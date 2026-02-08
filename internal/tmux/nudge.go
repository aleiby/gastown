package tmux

// ======================================================================
// DESIGN CONSTRAINT: Zero Framework Cognition (ZFC)
//
// The SYPHN nudge protocol's input extraction algorithm MUST remain
// client-agnostic. It works by diffing the pane before/after clearing,
// detecting continuation prefixes dynamically, and stripping them.
// NO hardcoded prompt strings, separator patterns, or client-specific
// heuristics may appear in the extraction path. Decisions that require
// interpretation belong in the AI layer, not in brittle pattern matching.
//
// HOW TO EXTEND:
// - Client-specific early-outs (e.g., paste detection) go in clearly
//   marked pre-check sections with NOTE comments (see
//   pastedTextPlaceholderRe for the pattern).
// - New heuristics in the extraction path MUST be tested against
//   multiple TUI clients (see TestExtractOriginalInput_PythonREPL,
//   TestExtractOriginalInput_BashPrompt, etc.).
// - If client-specific behavior is truly needed in the core path,
//   inject it as a parameter (e.g., a ClientHints struct), never bake
//   it into the algorithm directly.
// - Use trimToNonContent-style extension points: small, well-named
//   functions with clear contracts that signal "extend here."
//
// FAILURE CATEGORIES (do not conflate):
// - Algorithm bugs: wrong candidate selected, prefix stripping broken,
//   diff hunk fragmentation → fix in extractOriginalInput / GroupHunks
// - Transport artifacts: tmux expands tabs, -J joins lines, paste
//   collapsing, scrollback limits → NOT algorithm bugs. Do NOT "fix"
//   by adding client-specific code to the extraction path.
// ======================================================================

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

// Timing constants for the SYPHN protocol.
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
	// Each input line takes ~2 iterations (content + newline), so 200 supports ~100 lines.
	nudgeMaxClearIterations = 200

	// nudgeMinCaptureN is the minimum number of lines to capture for convergence.
	nudgeMinCaptureN = 5

	// nudgeDiffMarginLines is the extra margin added to captureN when trimming
	// captures before diffing. This ensures the diff region includes the full
	// input even after -J joins wrapped lines (which reduces line count).
	nudgeDiffMarginLines = 20
)

// pastedTextPlaceholderRe matches Claude Code's large paste placeholder pattern.
// Example: "[Pasted text #3 +47 lines]"
//
// NOTE: This is a client-specific early-out check. Other TUI clients may use
// different paste representations (or none). The nudge protocol still works
// without this check — it only prevents attempting a nudge during an active
// large paste, which would corrupt the pasted content. Additional client
// patterns can be added to the regex as needed.
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

// clearInput captures the original pane state, then uses a sentinel to locate
// the input region and convergence-clear it.
//
// Steps:
// 1. Full capture BEFORE touching anything → originalCapture (untouched layout)
// 2. C-a + insert sentinel §XXXX§
// 3. Full capture (with -J) → find sentinel → compute N (lines from bottom)
// 4. Convergence clear: C-a + Ctrl-K until captureLast(N+2) stabilizes
//
// The sentinel capture uses CapturePaneAll (with -J) separately from the
// original capture because inserting the sentinel may change word wrap.
// Using -J ensures N is in logical lines, consistent with the diff captures.
// The sentinel is NOT included in originalCapture, so the diff sees the
// original layout (correct wrap points).
func (t *Tmux) clearInput(session string) (originalCapture string, captureN int, err error) {
	// Capture the original pane state BEFORE inserting sentinel.
	// This preserves the original visual wrap positions for the diff.
	originalCapture, err = t.CapturePaneAll(session, 0)
	if err != nil {
		return "", 0, fmt.Errorf("capture original: %w", err)
	}

	// Insert sentinel to locate the input region
	sentinel := makeSentinel()
	if err := t.SendKeysRaw(session, "C-a"); err != nil {
		return "", 0, fmt.Errorf("send C-a: %w", err)
	}
	if err := t.SendKeysLiteral(session, sentinel); err != nil {
		return "", 0, fmt.Errorf("send sentinel: %w", err)
	}
	time.Sleep(nudgeSentinelDelayMs * time.Millisecond)

	// Full capture with -J for sentinel search. Must be full because the
	// cursor could be anywhere in a large multi-line input (e.g., user
	// navigated up to edit line 30 of a 100-line prompt). Uses -J so N
	// is in logical lines, consistent with the diff captures.
	sentinelCapture, err := t.CapturePaneAll(session, 0)
	if err != nil {
		return originalCapture, nudgeMinCaptureN, fmt.Errorf("capture after sentinel: %w", err)
	}

	lines := strings.Split(sentinelCapture, "\n")
	_, linesFromBottom, found := findSentinelFromEnd(lines, sentinel)
	if !found {
		// Sentinel not found — may be in vim NORMAL mode where C-a increments
		// a number and literal text is interpreted as commands.
		// Send Escape (ensure NORMAL) + "i" (enter INSERT mode) and retry.
		_ = t.SendKeysRaw(session, "Escape")
		time.Sleep(nudgeSentinelDelayMs * time.Millisecond)
		_ = t.SendKeysRaw(session, "i")
		time.Sleep(nudgeSentinelDelayMs * time.Millisecond)

		sentinel = makeSentinel() // fresh sentinel in case old one partially inserted
		if err := t.SendKeysRaw(session, "C-a"); err != nil {
			return originalCapture, nudgeMinCaptureN, fmt.Errorf("send C-a (vim retry): %w", err)
		}
		if err := t.SendKeysLiteral(session, sentinel); err != nil {
			return originalCapture, nudgeMinCaptureN, fmt.Errorf("send sentinel (vim retry): %w", err)
		}
		time.Sleep(nudgeSentinelDelayMs * time.Millisecond)

		sentinelCapture, err = t.CapturePaneAll(session, 0)
		if err != nil {
			return originalCapture, nudgeMinCaptureN, fmt.Errorf("capture after sentinel (vim retry): %w", err)
		}
		lines = strings.Split(sentinelCapture, "\n")
		_, linesFromBottom, found = findSentinelFromEnd(lines, sentinel)
		if !found {
			return originalCapture, nudgeMinCaptureN, fmt.Errorf("sentinel not found after vim mode retry")
		}
	}

	// N = lines from bottom where sentinel was found + margin
	captureN = linesFromBottom + 2
	if captureN < nudgeMinCaptureN {
		captureN = nudgeMinCaptureN
	}

	// Convergence clear: C-a + Ctrl-K until captureLast(N+2) stabilizes
	if err := t.convergenceClear(session, captureN); err != nil {
		return originalCapture, captureN, err
	}

	return originalCapture, captureN, nil
}

// cycleDetector tracks recent states to detect oscillation in a convergence loop.
// When a state is seen twice, the loop is cycling and should abort.
type cycleDetector struct {
	recent []string
	maxLen int
}

func newCycleDetector(maxLen int) *cycleDetector {
	return &cycleDetector{recent: make([]string, 0, maxLen), maxLen: maxLen}
}

// Check returns true if the state has been seen before (cycle detected).
// Otherwise records the state for future checks.
func (d *cycleDetector) Check(state string) bool {
	for _, seen := range d.recent {
		if state == seen {
			return true
		}
	}
	d.recent = append(d.recent, state)
	if len(d.recent) > d.maxLen {
		d.recent = d.recent[1:]
	}
	return false
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

	detector := newCycleDetector(8)

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

		if detector.Check(cur) {
			return ErrClearStalled
		}

		prev = cur
	}

	return fmt.Errorf("convergence clear exceeded %d iterations", nudgeMaxClearIterations)
}

// detectContinuationPrefix extracts the TUI's continuation prefix from the
// deleted content of a diff hunk.
//
// Line 0 of the deleted content has no prefix (the prompt is in the EQUAL region).
// Lines 1+ are continuation lines with the TUI's prefix (e.g., "  " for Claude
// Code, "... " for Python REPL). With 2+ non-empty continuation lines, the common
// prefix (trimmed to non-alphanumeric) gives the TUI prefix.
//
// ZFC-compliant: detected dynamically from diff output, no hardcoded constants.
func detectContinuationPrefix(deleted string) string {
	lines := strings.Split(deleted, "\n")
	if len(lines) < 2 {
		return "" // Single line, no continuation prefix to detect
	}

	// Collect non-empty continuation lines (lines 1+)
	var contLines []string
	for _, l := range lines[1:] {
		if len(l) > 0 {
			contLines = append(contLines, l)
		}
	}

	if len(contLines) >= 2 {
		prefix := contLines[0]
		for _, l := range contLines[1:] {
			prefix = commonStringPrefix(prefix, l)
		}
		// Trim to non-alphanumeric to avoid over-stripping when all
		// continuation lines share the same content prefix.
		return trimToNonContent(prefix)
	}

	// Only 1 continuation line: extract leading whitespace as prefix
	if len(contLines) == 1 {
		line := contLines[0]
		trimmed := strings.TrimLeft(line, " \t")
		return line[:len(line)-len(trimmed)]
	}

	return ""
}

// commonStringPrefix returns the common prefix of two strings.
// The result is truncated to the last complete UTF-8 rune boundary
// to avoid splitting multi-byte characters.
func commonStringPrefix(a, b string) string {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	end := 0
	for end < n {
		if a[end] != b[end] {
			break
		}
		end++
	}
	// Walk back to a UTF-8 rune boundary: continuation bytes have 10xxxxxx pattern
	for end > 0 && end < len(a) && a[end-1]&0xC0 == 0x80 {
		end--
	}
	// If we stopped on a leading byte without its full sequence, drop it too
	if end > 0 && a[end-1] >= 0x80 {
		// Check if the rune starting at this position is complete
		r := end - 1
		for r > 0 && a[r]&0xC0 == 0x80 {
			r--
		}
		// r is now at the start of the last rune — check if it's fully within [0, end)
		var runeLen int
		lead := a[r]
		switch {
		case lead < 0x80:
			runeLen = 1
		case lead&0xE0 == 0xC0:
			runeLen = 2
		case lead&0xF0 == 0xE0:
			runeLen = 3
		case lead&0xF8 == 0xF0:
			runeLen = 4
		default:
			runeLen = 1 // invalid, treat as single byte
		}
		if r+runeLen > end {
			end = r // Incomplete rune, truncate before it
		}
	}
	return a[:end]
}

// trimToNonContent trims a prefix string to only include non-content characters.
// This prevents over-stripping when all continuation lines share content prefix.
// Keeps ASCII whitespace and common TUI prompt punctuation (., >, |, :).
// Stops at ASCII alphanumeric or any non-ASCII byte (likely content: emoji, CJK, etc.).
func trimToNonContent(prefix string) string {
	for i := 0; i < len(prefix); i++ {
		c := prefix[i]
		if c == ' ' || c == '\t' || c == '.' || c == '>' || c == '|' || c == ':' {
			continue
		}
		// Stop at any content character: alphanumeric or non-ASCII
		return prefix[:i]
	}
	return prefix
}

// lastNLines returns the last n lines of s.
// If s has fewer than n lines, returns s unchanged.
// If n <= 0, returns s unchanged (no trimming).
func lastNLines(s string, n int) string {
	if n <= 0 {
		return s
	}
	count := 0
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			count++
			if count == n {
				return s[i+1:]
			}
		}
	}
	return s // fewer than n lines
}

// extractOriginalInput extracts the user's original input from the diff between
// the original capture (untouched) and the cleared capture.
//
// ZFC-compliant: works with ANY TUI client. No hardcoded prompt prefixes.
//
// Algorithm:
// 1. Trim captures to last N+margin lines (avoids diffing full scrollback)
// 2. Myers diff original vs cleared → GroupHunks
// 3. Search backward for last hunk with continuation prefix (the input region)
// 4. Detect continuation prefix dynamically from the deleted content
// 5. Strip detected prefix from continuation lines (1..N)
// 6. Line 0 needs no prefix stripping — the prompt prefix is in the EQUAL region
// 7. No TrimSpace — preserve all whitespace
//
// Visual wrapping is handled at the capture level: CapturePaneAll uses -J to
// join wrapped lines, so the diff sees logical lines, not visual rows.
//
// Known limitations:
// - If line 0 of the input is empty (user typed a leading newline), the \n
//   after the prompt exists in both captures and lands in the EQUAL region.
//   The diff can't distinguish "empty prompt" from "prompt + leading newline",
//   so the leading \n is lost.
// - CapturePaneAll strips trailing spaces from each line (to counteract -J
//   padding). This means trailing spaces in the user's input are not preserved
//   in the restored content. In practice, trailing spaces in terminal input
//   are rarely significant.
func extractOriginalInput(originalCapture, clearedCapture string, captureN int) string {
	// Trim captures to the input region before diffing. The input is always
	// near the bottom, so the top of the scrollback is identical between
	// original and cleared and contributes nothing to the diff. Trimming
	// dramatically reduces the Myers diff cost for large scrollback buffers.
	if captureN > 0 {
		trimLines := captureN + nudgeDiffMarginLines
		originalCapture = lastNLines(originalCapture, trimLines)
		clearedCapture = lastNLines(clearedCapture, trimLines)
	}

	diffs := MyersDiff([]byte(originalCapture), []byte(clearedCapture))
	hunks := GroupHunks(diffs)

	// Collect all hunks with deleted content as candidates.
	// The input hunk and the status bar hunk may both have deleted content.
	// We need to identify which is the input.
	type candidate struct {
		deleted    string
		inserted   string
		contPrefix string
	}
	var candidates []candidate
	for _, h := range hunks {
		if len(h.Deleted) == 0 {
			continue
		}
		d := string(h.Deleted)
		candidates = append(candidates, candidate{d, string(h.Inserted), detectContinuationPrefix(d)})
	}

	if len(candidates) == 0 {
		return ""
	}

	// Select the input hunk:
	// 1. Prefer the last hunk with a detected continuation prefix (multi-line input).
	//    Search backward since the input is near the bottom of the capture.
	// 2. Otherwise, pick the candidate with the smallest Inserted content.
	//    Input clearing produces an empty Inserted side (the prompt becomes "❯ ").
	//    Status bar changes have non-trivial Inserted content on both sides.
	// 3. If the best candidate's Inserted is >= its Deleted in length, it's
	//    likely a status bar swap with no actual input cleared → return "".
	var selected candidate
	for i := len(candidates) - 1; i >= 0; i-- {
		if candidates[i].contPrefix != "" {
			selected = candidates[i]
			break
		}
	}
	if selected.deleted == "" {
		bestIdx := 0
		for i := 1; i < len(candidates); i++ {
			if len(candidates[i].inserted) < len(candidates[bestIdx].inserted) {
				bestIdx = i
			}
		}
		selected = candidates[bestIdx]
		// If the Inserted side is at least as long as the Deleted side,
		// this is a symmetric change (e.g., status bar text swap), not
		// cleared input. Cleared input always has a much smaller Inserted.
		if len(selected.inserted) >= len(selected.deleted) {
			return ""
		}
	}

	// Strip continuation prefix from selected hunk
	lines := strings.Split(selected.deleted, "\n")
	var cleaned []string
	for j, line := range lines {
		if j == 0 {
			// Line 0: no prefix stripping needed.
			// The TUI prompt prefix (e.g., "❯ ") is in the EQUAL region.
			cleaned = append(cleaned, line)
		} else {
			if selected.contPrefix != "" && strings.HasPrefix(line, selected.contPrefix) {
				line = line[len(selected.contPrefix):]
			}
			cleaned = append(cleaned, line)
		}
	}

	result := strings.Join(cleaned, "\n")
	// Trim trailing newlines. Small EQUAL sections (e.g., the "\n" between
	// user input and a TUI separator line) get absorbed into the diff hunk,
	// leaving a spurious trailing newline in the deleted content.
	return strings.TrimRight(result, "\n")
}

// nudgeWithProtocol implements the SYPHN protocol.
//
// Protocol flow:
// 1. PRE-CHECKS: Copy mode? Paste placeholder?
// 2. CAPTURE ORIGINAL: Full capture of untouched pane state
// 3. SENTINEL + CLEAR: Insert sentinel to find N, convergence clear
// 4. CAPTURE CLEARED + DIFF: Extract original input from diff
// 5. INJECT: send-keys -l message + Enter
// 6. RESTORE: send-keys -l original input
//
// Delivery verification (deferred):
// Currently, Enter delivery is trusted without verification. A future
// enhancement could capture after Enter and verify the nudge text appears
// in the output, retrying on failure. This was previously attempted and
// removed (7346cc05) due to complexity — the TUI may transform the message
// (wrapping, styling) making exact string matching unreliable. A more robust
// approach would diff the pre/post-Enter captures and check that the inserted
// content contains a substring of the nudge message.
func (t *Tmux) nudgeWithProtocol(session, message string) error {
	// Pre-checks
	if t.IsPaneInCopyMode(session) {
		return ErrPaneBlocked
	}
	if t.detectPastePlaceholder(session) {
		return ErrPasteDetected
	}

	// Capture original → sentinel for N → convergence clear
	originalCapture, captureN, err := t.clearInput(session)
	if err != nil {
		return fmt.Errorf("nudge: %w", err)
	}

	// Capture cleared state with bounded -J capture. We know N from the
	// sentinel, so we only need captureN + margin lines. The original
	// capture is trimmed to match by extractOriginalInput.
	diffLines := captureN + nudgeDiffMarginLines
	clearedCapture, err := t.CapturePaneAll(session, diffLines)
	if err != nil {
		return fmt.Errorf("nudge: capture after clear: %w", err)
	}
	originalInput := extractOriginalInput(originalCapture, clearedCapture, captureN)

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
