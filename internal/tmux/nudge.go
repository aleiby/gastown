package tmux

import (
	"bytes"
	"regexp"
	"time"
)

// Timing constants for the Clear/Inject/Verify protocol.
const (
	// nudgeClearDelayMs is the time to wait after Ctrl-C for input to clear.
	nudgeClearDelayMs = 50

	// nudgeInjectDelayMs is the time to wait after injecting text before verification.
	nudgeInjectDelayMs = 50

	// nudgePastePlaceholderLines is how many lines to scan for paste placeholder.
	nudgePastePlaceholderLines = 50
)

// pastedTextPlaceholderRe matches Claude Code's large paste placeholder pattern.
// Example: "[Pasted text #3 +47 lines]"
var pastedTextPlaceholderRe = regexp.MustCompile(`\[Pasted text #\d+ \+\d+ lines\]`)

// nudgeSessionReliable implements the Clear/Inject/Verify protocol.
//
// Protocol:
// 1. Check if pane is in blocking mode (copy mode, etc.)
// 2. Capture BEFORE (full scrollback as byte stream)
// 3. Clear input with Ctrl-C
// 4. Inject nudge text (NO Enter yet)
// 5. Capture AFTER
// 6. Run Myers diff and find the nudge hunk
// 7. Verify clean delivery (no gap typing, no after-nudge typing)
// 8. If clean, send Enter
// 9. If not clean, return error (caller can retry later)
//
// Detection uses Myers diff algorithm to compare BEFORE and AFTER:
// - Handles multiple disjoint changes (scrolling, new output, input changes, ads)
// - Absorbs spurious character-level matches (threshold: 4 bytes)
// - Extracts: original input, gap typing, after-nudge typing
// - Clean delivery = no gap typing AND no after-nudge typing
//
// ============================================================================
// WARNING: NEVER SEND A SECOND CTRL-C IN THIS FUNCTION!
// ============================================================================
// Two Ctrl-C's in quick succession exits Claude Code. This was learned the
// hard way - retry logic that sent Ctrl-C to "clear and retry" would kill
// agent sessions. If anything goes wrong after the first Ctrl-C, we return
// an error and let the caller retry later (daemon retries on next 2-second
// pass). DO NOT add retry logic that sends another Ctrl-C!
// ============================================================================
//
// Returns nil on success, error on failure.
func (t *Tmux) nudgeSessionReliable(session, message string) error {
	// Pre-check: is pane in blocking mode?
	if t.IsPaneInMode(session) {
		return ErrPaneInMode
	}

	// Step 1: Capture BEFORE (full scrollback as byte stream)
	before, err := t.capturePaneFull(session)
	if err != nil {
		return err
	}

	// Check for large paste placeholder
	if hasPastedTextPlaceholder(before, nudgePastePlaceholderLines) {
		return ErrPastePlaceholder
	}

	// Step 2: Clear input with Ctrl-C
	// WARNING: This is the ONLY Ctrl-C we send. See warning in function header.
	//
	// Send space + Ctrl-C atomically to break any "double Ctrl-C = exit" detection
	// window from previous Ctrl-C's (e.g., daemon retry queue delivering multiple nudges).
	// The space is harmless - Ctrl-C clears the input line anyway.
	// Sending both in one tmux call ensures they arrive together.
	if _, err := t.run("send-keys", "-t", session, " ", "C-c"); err != nil {
		return err
	}
	time.Sleep(nudgeClearDelayMs * time.Millisecond)

	// Step 3: Inject nudge text (NO Enter yet)
	if err := t.SendKeysLiteral(session, message); err != nil {
		return err
	}
	time.Sleep(nudgeInjectDelayMs * time.Millisecond)

	// Step 4: Capture AFTER (as byte stream)
	after, err := t.capturePaneFull(session)
	if err != nil {
		return err
	}

	// Step 5: Run Myers diff and find the nudge
	nudgeBytes := []byte(message)

	// Quick check: is the nudge even in AFTER?
	if bytes.Index(after, nudgeBytes) < 0 {
		return ErrNudgeNotFound
	}

	// Compute diff and find the nudge hunk
	diffs := MyersDiff(before, after)
	original, gapTyping, afterNudgeTyping, found := FindNudgeInDiff(before, after, nudgeBytes, diffs)
	if !found {
		return ErrNudgeNotFound
	}

	// Step 6: Verify clean delivery
	if !IsCleanDelivery(gapTyping, afterNudgeTyping) {
		// User was typing during injection - caller should retry later
		// Note: original contains what user had typed before Ctrl-C
		// Future enhancement: could log this for debugging
		_ = original // Currently unused, but available for restoration
		return ErrUserTyping
	}

	// Clean delivery - send Enter
	// Send Escape first for vim mode compatibility
	_, _ = t.run("send-keys", "-t", session, "Escape")
	time.Sleep(100 * time.Millisecond)

	if err := t.SendKeysRaw(session, "Enter"); err != nil {
		return err
	}

	// Future enhancement: Input restoration
	// If original is non-empty, the user had typed something before the nudge arrived.
	// We could restore it after the nudge is processed:
	//   toRestore := TextToRestore(original, gapTyping, afterNudgeTyping)
	//   if len(toRestore) > 0 { t.SendKeysLiteral(session, string(toRestore)) }
	// For now, we just deliver the nudge cleanly.

	// Wake the pane for detached sessions
	t.WakePaneIfDetached(session)
	return nil
}

// capturePaneFull captures the full scrollback of a pane.
func (t *Tmux) capturePaneFull(session string) ([]byte, error) {
	content, err := t.CapturePaneAll(session)
	if err != nil {
		return nil, err
	}
	return []byte(content), nil
}

// tailLines extracts the last n lines from data efficiently.
// Works backwards from the end of the buffer.
func tailLines(data []byte, n int) [][]byte {
	if len(data) == 0 || n <= 0 {
		return nil
	}

	// Count lines from the end
	var lines [][]byte
	end := len(data)

	// Skip trailing newline if present
	if end > 0 && data[end-1] == '\n' {
		end--
	}

	for i := end - 1; i >= 0 && len(lines) < n; i-- {
		if data[i] == '\n' {
			lines = append(lines, data[i+1:end])
			end = i
		}
	}

	// Don't forget the first line (no leading newline)
	if end > 0 && len(lines) < n {
		lines = append(lines, data[0:end])
	}

	// Reverse to get correct order
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	return lines
}

// hasPastedTextPlaceholder checks if the capture contains a large paste placeholder.
// Scans the last maxLines lines for the placeholder pattern.
func hasPastedTextPlaceholder(data []byte, maxLines int) bool {
	lines := tailLines(data, maxLines)
	for _, line := range lines {
		if pastedTextPlaceholderRe.Match(line) {
			return true
		}
	}
	return false
}


