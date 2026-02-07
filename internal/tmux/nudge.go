package tmux

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
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
)

// tuiSeparatorPrefix is the prefix of Claude Code's TUI separator lines.
// Separator lines consist of box-drawing characters (U+2500) spanning the pane width.
const tuiSeparatorPrefix = "─────"

// tuiPromptPrefix is Claude Code's TUI prompt prefix: ❯ + NO-BREAK SPACE.
const tuiPromptPrefix = "❯\u00a0"

// tuiContinuationPrefix is the prefix Claude Code adds to continuation lines
// in multi-line input — both explicit newlines AND visual line wrapping.
const tuiContinuationPrefix = "  "

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

// findInputField locates the input field in a pane capture.
// Returns the field lines (between the last two separator lines) and the pane width.
func findInputField(capture string) (fieldLines []string, paneWidth int, ok bool) {
	lines := strings.Split(capture, "\n")
	var sepIndices []int
	for i, line := range lines {
		if strings.HasPrefix(line, tuiSeparatorPrefix) {
			sepIndices = append(sepIndices, i)
		}
	}
	if len(sepIndices) < 2 {
		return nil, 0, false
	}
	topSep := sepIndices[len(sepIndices)-2]
	botSep := sepIndices[len(sepIndices)-1]
	// Pane width = number of ─ characters in separator (each is 1 display column)
	paneWidth = utf8.RuneCountInString(lines[topSep])
	return lines[topSep+1 : botSep], paneWidth, true
}

// extractOriginalInput extracts the user's original input from a pane capture.
//
// Locates the input field between the TUI's separator lines, strips TUI prompt
// and continuation prefixes, then uses the pane width to distinguish visual
// line wrapping (joined back into one logical line) from explicit newlines.
//
// Claude Code adds "  " prefix to ALL continuation lines — both visual wraps
// and explicit newlines. We detect visual wraps using a word-wrap heuristic:
// if the current line's content + space + the first word of the next line would
// exceed availWidth, the TUI was forced to word-wrap here.
func extractOriginalInput(capture string) string {
	fieldLines, paneWidth, ok := findInputField(capture)
	if !ok || len(fieldLines) == 0 {
		return ""
	}

	// Claude Code uses paneWidth - 4 content chars per line:
	// 2 columns for prompt/continuation prefix + 2 columns right margin.
	availWidth := paneWidth - 4

	// Strip TUI prefixes
	type fieldEntry struct {
		content string
		width   int // display width of content
	}
	var entries []fieldEntry

	for j, line := range fieldLines {
		var content string
		if j == 0 {
			content = strings.TrimPrefix(line, tuiPromptPrefix)
			if content == line {
				// Try regular space variant
				content = strings.TrimPrefix(line, "❯ ")
			}
			// Handle empty prompt (just "❯" with no content)
			trimmed := strings.TrimSpace(content)
			if trimmed == "❯" || trimmed == "\u276f" {
				content = ""
			}
		} else {
			content = strings.TrimPrefix(line, tuiContinuationPrefix)
		}

		entries = append(entries, fieldEntry{
			content: content,
			width:   runewidth.StringWidth(content),
		})
	}

	// Reconstruct logical lines using word-wrap detection.
	//
	// Claude Code word-wraps: it breaks at the last space before the width
	// limit, consuming the space. To detect this, we check if the current
	// line's content + 1 (consumed space) + the first word of the next line
	// would exceed availWidth. If so, the TUI was forced to wrap here.
	//
	// When joining wrapped lines:
	// - Character wrap (width near availWidth): pad to availWidth to restore
	//   any trailing spaces that tmux capture-pane trimmed.
	// - Word wrap (width < availWidth-1): restore the single consumed space
	//   between words.
	var result strings.Builder
	for j, e := range entries {
		result.WriteString(e.content)
		if j < len(entries)-1 {
			if isVisualWrap(e.width, entries[j+1].content, availWidth) {
				if e.width >= availWidth-1 {
					// Character wrap: pad to restore tmux-trimmed trailing spaces
					if e.width < availWidth {
						result.WriteString(strings.Repeat(" ", availWidth-e.width))
					}
				} else {
					// Word wrap: restore the consumed space between words
					result.WriteByte(' ')
				}
			} else {
				result.WriteByte('\n')
			}
		}
	}

	return result.String()
}

// isVisualWrap determines if the break between two field lines is a visual
// word-wrap (should be joined) or an explicit newline (should be preserved).
//
// Word-wrap detection: if currentWidth + space + firstWordWidth > availWidth,
// the TUI couldn't fit the next word and was forced to wrap.
// Character-wrap detection: if currentWidth >= availWidth-1, the line fills
// the full available width (the -1 accounts for tmux trimming trailing spaces).
func isVisualWrap(currentWidth int, nextContent string, availWidth int) bool {
	// Character wrap: line fills the available width
	if currentWidth >= availWidth-1 {
		return true
	}

	// Empty next line is always an explicit newline (Enter pressed twice)
	if len(nextContent) == 0 {
		return false
	}

	// Word-wrap: check if current content + space + first word of next line
	// would exceed available width
	firstWordWidth := firstWordDisplayWidth(nextContent)
	return currentWidth+1+firstWordWidth > availWidth
}

// firstWordDisplayWidth returns the display width of the first word in content.
// A "word" is characters up to the first space (or end of string).
// If content starts with a space, returns 0 (indented explicit newline).
func firstWordDisplayWidth(content string) int {
	spaceIdx := strings.IndexByte(content, ' ')
	if spaceIdx == 0 {
		return 0 // starts with space — not a word-wrap continuation
	}
	if spaceIdx > 0 {
		return runewidth.StringWidth(content[:spaceIdx])
	}
	return runewidth.StringWidth(content)
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

// nudgeWithProtocol implements the Clear/Inject/Restore protocol.
//
// Protocol flow:
// 1. PRE-CHECKS: Copy mode? Paste placeholder?
// 2. CAPTURE BEFORE: Full capture to extract original input using TUI separators
// 3. CLEAR: C-a + Ctrl-K convergence loop until stable
// 4. INJECT: send-keys -l message + Enter
// 5. RESTORE: send-keys -l original input
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

	// Extract original input from BEFORE capture using separator-based extraction
	originalInput := extractOriginalInput(beforeCapture)

	// Compute captureN from field line count (+ margin for separators and status)
	fieldLines, _, ok := findInputField(beforeCapture)
	captureN := nudgeMinCaptureN
	if ok && len(fieldLines)+3 > captureN {
		captureN = len(fieldLines) + 3
	}

	// Convergence clear: C-a + Ctrl-K until stable
	if err := t.convergenceClear(session, captureN); err != nil {
		return fmt.Errorf("nudge: %w", err)
	}

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
