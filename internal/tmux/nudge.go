package tmux

import (
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

// Timing constants for the Clear/Inject/Restore protocol.
const (
	// nudgeClearDelayMs is the time to wait after Ctrl-U for clear to take effect.
	nudgeClearDelayMs = 50

	// nudgeInjectDelayMs is the time to wait after injecting nudge text.
	nudgeInjectDelayMs = 100

	// nudgeEnterDelayMs is the time to wait after pressing Enter for submission.
	nudgeEnterDelayMs = 200

	// nudgePastePlaceholderLines is how many lines to scan for paste placeholder.
	nudgePastePlaceholderLines = 50
)

// pastedTextPlaceholderRe matches Claude Code's large paste placeholder pattern.
// Example: "[Pasted text #3 +47 lines]"
var pastedTextPlaceholderRe = regexp.MustCompile(`\[Pasted text #\d+ \+\d+ lines\]`)

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

// extractDeletedInput finds the last DELETE hunk in the diff between BEFORE
// and AFTER captures. This represents the input that was cleared by Ctrl-U.
// Strips TUI prompt prefixes ("❯ " on first line, "  " on continuation lines).
func extractDeletedInput(beforeCapture, afterCapture string) string {
	diffs := MyersDiff([]byte(beforeCapture), []byte(afterCapture))
	hunks := GroupHunks(diffs)

	// The last DELETE hunk is the input that was cleared (closest to bottom of pane)
	for i := len(hunks) - 1; i >= 0; i-- {
		h := hunks[i]
		if len(h.Deleted) == 0 {
			continue
		}

		deleted := string(h.Deleted)

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
		if result != "" {
			return result
		}
	}

	return ""
}

// nudgeWithProtocol implements the Clear/Inject/Restore protocol.
//
// Protocol flow:
// 1. PRE-CHECKS: Copy mode? Paste placeholder?
// 2. CAPTURE BEFORE: Full pane capture
// 3. CLEAR: Ctrl-U to clear input
// 4. CAPTURE AFTER + DIFF: Extract original input from deleted content
// 5. INJECT: send-keys -l message + Enter
// 6. RESTORE: Restore original input
func (t *Tmux) nudgeWithProtocol(session, message string) error {
	// Pre-checks
	if t.IsPaneInCopyMode(session) {
		return ErrPaneBlocked
	}
	if t.detectPastePlaceholder(session) {
		return ErrPasteDetected
	}

	// Capture BEFORE state
	beforeCapture, err := t.CapturePaneAll(session)
	if err != nil {
		return fmt.Errorf("nudge: capture before: %w", err)
	}

	// Clear input with Ctrl-U
	if err := t.SendKeysRaw(session, "C-u"); err != nil {
		return fmt.Errorf("nudge: clear: %w", err)
	}
	time.Sleep(nudgeClearDelayMs * time.Millisecond)

	// Capture AFTER state and diff to extract original input
	afterCapture, err := t.CapturePaneAll(session)
	if err != nil {
		return fmt.Errorf("nudge: capture after: %w", err)
	}
	originalInput := extractDeletedInput(beforeCapture, afterCapture)

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
