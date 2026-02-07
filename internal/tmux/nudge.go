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
	ErrClearStalled        = errors.New("input clearing stalled (oscillating state detected)")
	ErrNudgeDeliveryFailed = errors.New("nudge delivery failed after retries")
)

// Timing constants for the Clear/Inject/Restore protocol.
const (
	// nudgeSentinelDelayMs is the time to wait after inserting sentinel for TUI to render.
	nudgeSentinelDelayMs = 50

	// nudgeClearIterDelayMs is the time to wait after each C-a+Ctrl-K for TUI to render.
	nudgeClearIterDelayMs = 50

	// nudgeInjectDelayMs is the time to wait after injecting nudge text.
	nudgeInjectDelayMs = 100

	// nudgeEnterDelayMs is the time to wait after pressing Enter for submission.
	nudgeEnterDelayMs = 200

	// nudgePastePlaceholderLines is how many lines to scan for paste placeholder.
	nudgePastePlaceholderLines = 50

	// nudgeMaxClearIterations is the hard upper bound on convergence loop iterations.
	// Each input line takes ~2 iterations (content + newline), so 100 supports ~50 lines.
	nudgeMaxClearIterations = 100
)

// pastedTextPlaceholderRe matches Claude Code's large paste placeholder pattern.
// Example: "[Pasted text #3 +47 lines]"
var pastedTextPlaceholderRe = regexp.MustCompile(`\[Pasted text #\d+ \+\d+ lines\]`)

// makeSentinel generates a unique sentinel string like "§XXXX§".
// Uses sha256 of current nanosecond time, base32-encoded, 4 chars + bookends = 6 total.
func makeSentinel() string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
	encoded := base32.StdEncoding.EncodeToString(h[:3])
	encoded = strings.TrimRight(encoded, "=")
	if len(encoded) > 4 {
		encoded = encoded[:4]
	}
	return "§" + encoded + "§"
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

// clearInput implements the sentinel + capture + C-a+Ctrl-K convergence clear protocol.
//
// Steps:
// 1. C-a (go to beginning of current visual line)
// 2. Insert sentinel §XXXX§
// 3. Full capture → find sentinel → compute N (lines from sentinel to bottom)
// 4. Convergence loop: C-a + Ctrl-K until captureLast(N+2) stabilizes
//
// Returns BEFORE capture (with sentinel), sentinel string, and N.
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

	// Full capture to find sentinel
	capture, err := t.CapturePaneAll(session)
	if err != nil {
		return "", "", 0, fmt.Errorf("capture after sentinel: %w", err)
	}
	beforeCapture = capture

	// Find sentinel in capture
	lines := strings.Split(capture, "\n")
	_, linesFromBottom, found := findSentinelFromEnd(lines, sentinel)
	if !found {
		return beforeCapture, sentinel, 2, fmt.Errorf("sentinel not found in capture")
	}

	// N = lines from bottom where sentinel was found
	// Capture N+2 lines for the convergence loop (extra margin)
	captureN = linesFromBottom + 2

	// Convergence clear: C-a + Ctrl-K until captureLast(N+2) stabilizes
	if err := t.convergenceClear(session, captureN); err != nil {
		return beforeCapture, sentinel, captureN, err
	}

	return beforeCapture, sentinel, captureN, nil
}

// convergenceClear sends C-a + Ctrl-K in a loop until the last N lines of the
// pane stabilize. Each C-a+Ctrl-K clears one visual line. Multi-line input
// takes ~2 iterations per line (content + newline). Convergence means the
// input field is empty.
//
// Uses cycle detection to abort if the state oscillates (e.g., vim mode where
// Ctrl-K opens a digraph prompt that C-a dismisses, creating an infinite loop).
func (t *Tmux) convergenceClear(session string, captureN int) error {
	prev, err := t.CapturePane(session, captureN)
	if err != nil {
		return fmt.Errorf("initial capture: %w", err)
	}

	recentCaptures := make([]string, 0, 8)

	for i := 0; i < nudgeMaxClearIterations; i++ {
		// C-a (beginning of line) + Ctrl-K (kill to end of line)
		if err := t.SendKeysRaw(session, "C-a"); err != nil {
			return fmt.Errorf("send C-a: %w", err)
		}
		if err := t.SendKeysRaw(session, "C-k"); err != nil {
			return fmt.Errorf("send C-k: %w", err)
		}
		time.Sleep(nudgeClearIterDelayMs * time.Millisecond)

		cur, err := t.CapturePane(session, captureN)
		if err != nil {
			return fmt.Errorf("capture iteration %d: %w", i, err)
		}

		// Converged — nothing changed, input is clear
		if cur == prev {
			return nil
		}

		// Check for cycling (oscillating state)
		for _, seen := range recentCaptures {
			if cur == seen {
				return ErrClearStalled
			}
		}

		recentCaptures = append(recentCaptures, cur)
		if len(recentCaptures) > 8 {
			recentCaptures = recentCaptures[1:]
		}

		prev = cur
	}

	return fmt.Errorf("convergence clear exceeded %d iterations", nudgeMaxClearIterations)
}

// extractOriginalInput extracts the user's original input from the diff between
// BEFORE (with sentinel) and AFTER (cleared) captures.
//
// Finds the last hunk containing the sentinel (the input region), strips the
// sentinel and TUI prompt prefixes ("❯ " on first line, "  " on continuation).
func extractOriginalInput(beforeCapture, afterCapture, sentinel string) string {
	diffs := MyersDiff([]byte(beforeCapture), []byte(afterCapture))
	hunks := GroupHunks(diffs)

	// Search backward — the input is at the bottom of the capture
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
				line = strings.TrimPrefix(line, "❯ ")
			} else {
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

// nudgeWithProtocol implements the Clear/Inject/Restore protocol.
//
// Protocol flow:
// 1. PRE-CHECKS: Copy mode? Paste placeholder?
// 2. SENTINEL + CAPTURE BEFORE: C-a, insert sentinel, full capture, find N
// 3. CLEAR: C-a + Ctrl-K convergence loop until stable
// 4. CAPTURE AFTER + DIFF: Extract original input
// 5. INJECT: send-keys -l message + Enter
// 6. RESTORE: send-keys -l original input
func (t *Tmux) nudgeWithProtocol(session, message string) error {
	// Pre-checks
	if t.IsPaneInCopyMode(session) {
		return ErrPaneBlocked
	}
	if t.detectPastePlaceholder(session) {
		return ErrPasteDetected
	}

	// Sentinel + capture + convergence clear
	beforeCapture, sentinel, _, err := t.clearInput(session)
	if err != nil {
		return fmt.Errorf("nudge: %w", err)
	}

	// Capture AFTER state and diff to extract original input
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
