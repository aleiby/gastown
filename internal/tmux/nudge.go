package tmux

import (
	"crypto/sha256"
	"encoding/base32"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Nudge delivery errors.
var (
	ErrPaneBlocked         = errors.New("pane is in copy mode or blocking state")
	ErrPasteDetected       = errors.New("large paste placeholder detected in input")
	ErrNudgeDeliveryFailed = errors.New("nudge delivery failed after retries")
)

// Timing constants for the Clear/Inject/Verify/Restore protocol.
const (
	// nudgeClearDelayMs is the time to wait after Ctrl-U for clear to take effect.
	nudgeClearDelayMs = 50

	// nudgeInjectDelayMs is the time to wait after injecting nudge text.
	nudgeInjectDelayMs = 100

	// nudgeEnterDelayMs is the time to wait after pressing Enter for submission.
	nudgeEnterDelayMs = 200

	// nudgeSentinelDelayMs is the time to wait after inserting sentinel.
	nudgeSentinelDelayMs = 50

	// nudgePastePlaceholderLines is how many lines to scan for paste placeholder.
	nudgePastePlaceholderLines = 50
)

// pastedTextPlaceholderRe matches Claude Code's large paste placeholder pattern.
// Example: "[Pasted text #3 +47 lines]"
var pastedTextPlaceholderRe = regexp.MustCompile(`\[Pasted text #\d+ \+\d+ lines\]`)

// makeSentinel generates a unique sentinel string like "§XXXX§".
// Uses sha256 of current nanosecond time + base32, 6 chars.
func makeSentinel() string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	encoded := base32.StdEncoding.EncodeToString(h[:])
	return "§" + encoded[:6] + "§"
}

// findSentinelFromEnd searches backward through lines for the sentinel.
// Returns the line index (from start), lines from bottom, and whether found.
func findSentinelFromEnd(lines []string, sentinel string) (lineIdx int, linesFromBottom int, found bool) {
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], sentinel) {
			return i, len(lines) - 1 - i, true
		}
	}
	return 0, 0, false
}

// clearInput implements the sentinel + capture + convergence clear protocol.
//
// Steps:
// 1. Insert sentinel character into input field (C-a positions cursor at start)
// 2. Capture full pane to find sentinel and determine input line count (N)
// 3. Run convergence clear loop (C-a + Ctrl-K until stable)
// 4. Return the BEFORE capture, sentinel, and N for later diff/restore
func (t *Tmux) clearInput(session string) (beforeCapture string, sentinel string, captureN int, err error) {
	sentinel = makeSentinel()

	// Move to start of input and insert sentinel
	if err := t.SendKeysRaw(session, "C-a"); err != nil {
		return "", "", 0, fmt.Errorf("send C-a: %w", err)
	}
	if err := t.SendKeysLiteral(session, sentinel); err != nil {
		return "", "", 0, fmt.Errorf("send sentinel: %w", err)
	}
	time.Sleep(nudgeSentinelDelayMs * time.Millisecond)

	// Capture full pane to find sentinel
	capture, err := t.CapturePaneAll(session)
	if err != nil {
		return "", "", 0, fmt.Errorf("capture after sentinel: %w", err)
	}
	beforeCapture = capture

	// Find sentinel in capture
	lines := strings.Split(capture, "\n")
	_, linesFromBottom, found := findSentinelFromEnd(lines, sentinel)
	if !found {
		// Sentinel not visible — clear what we can and bail
		_ = t.SendKeysRaw(session, "C-u")
		return beforeCapture, sentinel, 2, fmt.Errorf("sentinel not found in capture")
	}

	// N = lines from bottom where sentinel was found
	// Capture N+2 lines for the convergence loop (extra margin)
	captureN = linesFromBottom + 2

	// Clear the input with Ctrl-U (clears entire input line in Claude Code TUI)
	if err := t.SendKeysRaw(session, "C-u"); err != nil {
		return beforeCapture, sentinel, captureN, fmt.Errorf("send C-u: %w", err)
	}
	time.Sleep(nudgeClearDelayMs * time.Millisecond)

	return beforeCapture, sentinel, captureN, nil
}

// IsPaneInCopyMode checks if the pane is in copy mode or another blocking mode.
func (t *Tmux) IsPaneInCopyMode(session string) bool {
	inMode, err := t.run("display-message", "-t", session, "-p", "#{pane_in_mode}")
	return err == nil && inMode == "1"
}

// detectPastePlaceholder checks the last N lines for a large paste placeholder.
func (t *Tmux) detectPastePlaceholder(session string) bool {
	content, err := t.CapturePane(session, nudgePastePlaceholderLines)
	if err != nil {
		return false
	}
	return pastedTextPlaceholderRe.MatchString(content)
}

// extractOriginalInput extracts the user's original input from the diff between
// BEFORE (with sentinel) and AFTER (cleared) captures.
//
// Looks for DELETE hunks near the sentinel, strips the sentinel itself and
// TUI prompt prefixes ("❯ " on first line, "  " on continuation lines).
func extractOriginalInput(beforeCapture, afterCapture, sentinel string) string {
	diffs := MyersDiff([]byte(beforeCapture), []byte(afterCapture))
	hunks := GroupHunks(diffs)

	// Find the hunk that contains the sentinel — that's the input region
	for i := len(hunks) - 1; i >= 0; i-- {
		h := hunks[i]
		deleted := string(h.Deleted)
		if !strings.Contains(deleted, sentinel) {
			continue
		}

		// Remove sentinel
		deleted = strings.ReplaceAll(deleted, sentinel, "")

		// Strip TUI prompt prefixes line by line
		lines := strings.Split(deleted, "\n")
		var cleaned []string
		for j, line := range lines {
			if j == 0 {
				// First line: strip "❯ " prompt prefix
				line = strings.TrimPrefix(line, "❯ ")
			} else {
				// Continuation lines: strip "  " prefix (two spaces)
				line = strings.TrimPrefix(line, "  ")
			}
			cleaned = append(cleaned, line)
		}

		result := strings.Join(cleaned, "\n")
		result = strings.TrimSpace(result)
		return result
	}

	return ""
}

// nudgeWithProtocol implements the full Clear/Inject/Verify/Restore protocol.
//
// Protocol flow:
// 1. PRE-CHECKS: Copy mode? Paste placeholder?
// 2. SENTINEL + CAPTURE BEFORE: Insert sentinel, full capture, find sentinel, determine N
// 3. CLEAR (convergence): C-a + Ctrl-K until stable
// 4. CAPTURE AFTER + DIFF: Extract original input
// 5. INJECT + VERIFY (up to 3 attempts): send-keys + Enter, verify via convergence probe
// 6. RESTORE: Restore original input
func (t *Tmux) nudgeWithProtocol(session, message string) error {
	// Pre-checks
	if t.IsPaneInCopyMode(session) {
		return ErrPaneBlocked
	}
	if t.detectPastePlaceholder(session) {
		return ErrPasteDetected
	}

	// Clear input, get BEFORE capture
	beforeCapture, sentinel, _, err := t.clearInput(session)
	if err != nil {
		return fmt.Errorf("nudge: %w", err)
	}

	// AFTER capture + diff to extract original input
	afterCapture, err := t.CapturePaneAll(session)
	if err != nil {
		return fmt.Errorf("nudge: capture after clear: %w", err)
	}
	originalInput := extractOriginalInput(beforeCapture, afterCapture, sentinel)

	// Inject nudge + Enter
	if err := t.SendKeysLiteral(session, message); err != nil {
		return fmt.Errorf("nudge: inject: %w", err)
	}
	time.Sleep(nudgeInjectDelayMs * time.Millisecond)

	if err := t.SendKeysRaw(session, "Enter"); err != nil {
		return fmt.Errorf("nudge: enter: %w", err)
	}

	// Restore original input
	if originalInput != "" {
		time.Sleep(nudgeEnterDelayMs * time.Millisecond)
		_ = t.SendKeysLiteral(session, originalInput)
	}

	t.WakePaneIfDetached(session)
	return nil
}
