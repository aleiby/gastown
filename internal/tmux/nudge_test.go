package tmux

import (
	"fmt"
	"strings"
	"testing"
)

func TestMakeSentinel(t *testing.T) {
	s := makeSentinel()
	if !strings.HasPrefix(s, "§") || !strings.HasSuffix(s, "§") {
		t.Errorf("sentinel should be bookended with section signs, got %q", s)
	}
	inner := s[len("§") : len(s)-len("§")]
	if len(inner) > 4 {
		t.Errorf("sentinel inner should be <= 4 chars, got %d: %q", len(inner), inner)
	}
	s2 := makeSentinel()
	if s == s2 {
		t.Errorf("two sentinels should differ, both were %q", s)
	}
}

func TestFindSentinelFromEnd(t *testing.T) {
	sentinel := "§TEST§"
	lines := []string{
		"some output",
		"more output",
		"❯ " + sentinel + "hello world",
		"",
	}

	lineIdx, linesFromBottom, found := findSentinelFromEnd(lines, sentinel)
	if !found {
		t.Fatal("sentinel not found")
	}
	if lineIdx != 2 {
		t.Errorf("expected lineIdx 2, got %d", lineIdx)
	}
	if linesFromBottom != 1 {
		t.Errorf("expected linesFromBottom 1, got %d", linesFromBottom)
	}
}

func TestFindSentinelFromEnd_NotFound(t *testing.T) {
	lines := []string{"no", "sentinel", "here"}
	_, _, found := findSentinelFromEnd(lines, "§NOPE§")
	if found {
		t.Error("should not find sentinel that isn't there")
	}
}

func TestDetectContinuationPrefix_MultiLine(t *testing.T) {
	deleted := "line one\n  line two\n  line three"

	prefix := detectContinuationPrefix(deleted)
	if prefix != "  " {
		t.Errorf("expected continuation prefix '  ', got %q", prefix)
	}
}

func TestDetectContinuationPrefix_SingleLine(t *testing.T) {
	deleted := "hello world"

	prefix := detectContinuationPrefix(deleted)
	if prefix != "" {
		t.Errorf("expected empty prefix for single line, got %q", prefix)
	}
}

func TestDetectContinuationPrefix_DifferentPrefix(t *testing.T) {
	deleted := "if True:\n... alpha\n... beta"

	prefix := detectContinuationPrefix(deleted)
	if prefix != "... " {
		t.Errorf("expected continuation prefix '... ', got %q", prefix)
	}
}

func TestDetectContinuationPrefix_TabPrefix(t *testing.T) {
	deleted := "line one\n\tline two\n\tline three"

	prefix := detectContinuationPrefix(deleted)
	if prefix != "\t" {
		t.Errorf("expected tab prefix, got %q", prefix)
	}
}

func TestExtractOriginalInput_SingleLine(t *testing.T) {
	original := "output line 1\noutput line 2\n❯ hello world"
	cleared := "output line 1\noutput line 2\n❯ "

	result := extractOriginalInput(original, cleared, 0)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestExtractOriginalInput_MultiLine(t *testing.T) {
	original := "output\n❯ line one\n  line two\n  line three"
	cleared := "output\n❯ "

	result := extractOriginalInput(original, cleared, 0)
	if result != "line one\nline two\nline three" {
		t.Errorf("expected 'line one\\nline two\\nline three', got %q", result)
	}
}

func TestExtractOriginalInput_Empty(t *testing.T) {
	original := "output\n❯ "
	cleared := "output\n❯ "

	result := extractOriginalInput(original, cleared, 0)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractOriginalInput_LeadingWhitespace(t *testing.T) {
	original := "output\n❯    leading spaces"
	cleared := "output\n❯ "

	result := extractOriginalInput(original, cleared, 0)
	if result != "   leading spaces" {
		t.Errorf("expected '   leading spaces', got %q", result)
	}
}

func TestExtractOriginalInput_TrailingWhitespace(t *testing.T) {
	original := "output\n❯ trailing spaces   "
	cleared := "output\n❯ "

	result := extractOriginalInput(original, cleared, 0)
	if result != "trailing spaces   " {
		t.Errorf("expected 'trailing spaces   ', got %q", result)
	}
}

func TestExtractOriginalInput_NoDiff(t *testing.T) {
	result := extractOriginalInput("same content", "same content", 0)
	if result != "" {
		t.Errorf("expected empty string when no diff, got %q", result)
	}
}

func TestExtractOriginalInput_DifferentTUI(t *testing.T) {
	original := "Python 3.11\n>>> if True:\n... alpha = 1\n... beta = 2"
	cleared := "Python 3.11\n>>> "

	result := extractOriginalInput(original, cleared, 0)
	if result != "if True:\nalpha = 1\nbeta = 2" {
		t.Errorf("expected 'if True:\\nalpha = 1\\nbeta = 2', got %q", result)
	}
}

func TestExtractOriginalInput_EmptyContinuationLine(t *testing.T) {
	original := "output\n❯ first\n\n  third"
	cleared := "output\n❯ "

	result := extractOriginalInput(original, cleared, 0)
	if result != "first\n\nthird" {
		t.Errorf("expected 'first\\n\\nthird', got %q", result)
	}
}

func TestExtractOriginalInput_ManyLines(t *testing.T) {
	var lines []string
	lines = append(lines, "output")
	lines = append(lines, "❯ L01")
	for i := 2; i <= 20; i++ {
		lines = append(lines, "  "+strings.Repeat("L", 1)+string(rune('0'+i/10))+string(rune('0'+i%10)))
	}
	original := strings.Join(lines, "\n")
	cleared := "output\n❯ "

	result := extractOriginalInput(original, cleared, 0)
	resultLines := strings.Split(result, "\n")
	if len(resultLines) != 20 {
		t.Fatalf("expected 20 lines, got %d", len(resultLines))
	}
	if resultLines[0] != "L01" {
		t.Errorf("line 0: expected 'L01', got %q", resultLines[0])
	}
	for i := 1; i < len(resultLines); i++ {
		if strings.HasPrefix(resultLines[i], "  ") {
			t.Errorf("line %d still has continuation prefix: %q", i, resultLines[i])
		}
	}
}

func TestCommonStringPrefix(t *testing.T) {
	tests := []struct {
		a, b, expected string
	}{
		{"hello", "help", "hel"},
		{"abc", "abc", "abc"},
		{"abc", "xyz", ""},
		{"", "abc", ""},
		{"abc", "", ""},
		{"  line1", "  line2", "  line"},
	}
	for _, tt := range tests {
		result := commonStringPrefix(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("commonStringPrefix(%q, %q) = %q, want %q", tt.a, tt.b, result, tt.expected)
		}
	}
}

func TestCommonStringPrefix_UTF8(t *testing.T) {
	a := "  🌟 line two"
	b := "  🎯 line three"
	result := commonStringPrefix(a, b)
	if result != "  " {
		t.Errorf("expected '  ' (emoji bytes should not be split), got %q", result)
	}
}

func TestTrimToNonContent(t *testing.T) {
	tests := []struct {
		input, expected string
	}{
		{"  ", "  "},
		{"  line", "  "},
		{"...", "..."},
		{"...abc", "..."},
		{"> ", "> "},
		{"> text", "> "},
		{"", ""},
		{"abc", ""},
	}
	for _, tt := range tests {
		result := trimToNonContent(tt.input)
		if result != tt.expected {
			t.Errorf("trimToNonContent(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

// --- Cycle detector tests ---

func TestCycleDetector_NoCycle(t *testing.T) {
	d := newCycleDetector(4)
	if d.Check("a") {
		t.Error("first state should not be a cycle")
	}
	if d.Check("b") {
		t.Error("second distinct state should not be a cycle")
	}
	if d.Check("c") {
		t.Error("third distinct state should not be a cycle")
	}
}

func TestCycleDetector_DetectsCycle(t *testing.T) {
	d := newCycleDetector(4)
	d.Check("state-A")
	d.Check("state-B")
	if !d.Check("state-A") {
		t.Error("should detect cycle when state-A reappears")
	}
}

func TestCycleDetector_WindowEviction(t *testing.T) {
	d := newCycleDetector(2) // only remember last 2
	d.Check("old")
	d.Check("newer")
	d.Check("newest") // "old" should be evicted

	if d.Check("old") {
		t.Error("evicted state should not trigger cycle detection")
	}
}

func TestCycleDetector_ConvergenceNotCycle(t *testing.T) {
	// Convergence (same state twice in a row) is handled by the caller
	// comparing prev == cur. The detector only sees distinct non-prev states.
	d := newCycleDetector(4)
	d.Check("a")
	d.Check("b")
	d.Check("c")
	// "d" is new, not a cycle
	if d.Check("d") {
		t.Error("new state should not be a cycle")
	}
}

// --- lastNLines tests ---

func TestLastNLines(t *testing.T) {
	tests := []struct {
		input    string
		n        int
		expected string
	}{
		{"a\nb\nc\nd", 2, "c\nd"},
		{"a\nb\nc\nd", 4, "a\nb\nc\nd"},
		{"a\nb\nc\nd", 10, "a\nb\nc\nd"}, // fewer lines than n
		{"a\nb\nc\nd", 0, "a\nb\nc\nd"},  // n=0 means no trimming
		{"single", 1, "single"},
		{"a\nb", 1, "b"},
		{"", 5, ""},
	}
	for _, tt := range tests {
		result := lastNLines(tt.input, tt.n)
		if result != tt.expected {
			t.Errorf("lastNLines(%q, %d) = %q, want %q", tt.input, tt.n, result, tt.expected)
		}
	}
}

// --- Status bar disambiguation tests ---

func TestExtractOriginalInput_StatusBarHunk(t *testing.T) {
	// Input and status bar both change. The input hunk is before the status
	// bar hunk. A separator line (>32 bytes) breaks them into separate hunks.
	sep := strings.Repeat("─", 40) // 40 × 3 bytes = 120 bytes UTF-8
	original := "output\n❯ my input\n" + sep + "\n  ctrl+t to hide tasks"
	cleared := "output\n❯ \n" + sep + "\n  ctrl+t · ctrl+g to edit"

	result := extractOriginalInput(original, cleared, 0)
	if result != "my input" {
		t.Errorf("expected 'my input', got %q", result)
	}
}

func TestExtractOriginalInput_ThreeCandidates(t *testing.T) {
	// Three change regions separated by large EQUAL regions.
	// Candidate 0: header change (noise)
	// Candidate 1: user input
	// Candidate 2: status bar change
	// With no continuation prefix, fallback picks second-to-last = candidate 1.
	sep := strings.Repeat("=", 40) // 40 bytes, above threshold
	original := "header old\n" + sep + "\n❯ typed input\n" + sep + "\nstatus old"
	cleared := "header new\n" + sep + "\n❯ \n" + sep + "\nstatus new"

	result := extractOriginalInput(original, cleared, 0)
	if result != "typed input" {
		t.Errorf("expected 'typed input', got %q", result)
	}
}

func TestExtractOriginalInput_MultiLineWithStatusBar(t *testing.T) {
	// Multi-line input (has continuation prefix) + status bar change.
	// The candidate with a continuation prefix should be selected regardless
	// of position, even when the status bar hunk is also present.
	sep := strings.Repeat("─", 40)
	original := "output\n❯ line one\n  line two\n  line three\n" + sep + "\n  status old"
	cleared := "output\n❯ \n" + sep + "\n  status new"

	result := extractOriginalInput(original, cleared, 0)
	if result != "line one\nline two\nline three" {
		t.Errorf("expected 'line one\\nline two\\nline three', got %q", result)
	}
}

// --- Trimmed extraction tests ---

func TestExtractOriginalInput_Trimmed(t *testing.T) {
	// Large capture with input near the bottom. Trimming with captureN > 0
	// should still correctly extract the input from the last few lines.
	var outputLines []string
	for i := 0; i < 1000; i++ {
		outputLines = append(outputLines, fmt.Sprintf("output line %d", i))
	}
	outputLines = append(outputLines, "❯ hello world")
	original := strings.Join(outputLines, "\n")

	outputLines[len(outputLines)-1] = "❯ "
	cleared := strings.Join(outputLines, "\n")

	// captureN=5, so trim to last 25 lines (5+20 margin)
	result := extractOriginalInput(original, cleared, 5)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestExtractOriginalInput_TrimmedMultiLine(t *testing.T) {
	// Multi-line input in a large capture, extracted with trimming.
	var outputLines []string
	for i := 0; i < 500; i++ {
		outputLines = append(outputLines, fmt.Sprintf("output line %d", i))
	}
	outputLines = append(outputLines, "❯ first line")
	outputLines = append(outputLines, "  second line")
	outputLines = append(outputLines, "  third line")
	original := strings.Join(outputLines, "\n")

	cleared := strings.Join(outputLines[:500], "\n") + "\n❯ "

	result := extractOriginalInput(original, cleared, 10)
	if result != "first line\nsecond line\nthird line" {
		t.Errorf("expected 'first line\\nsecond line\\nthird line', got %q", result)
	}
}

func TestExtractOriginalInput_StatusBarOnlyChange(t *testing.T) {
	// Only the status bar changes — no user input was typed.
	// Should return "" since there's nothing to restore.
	sep := strings.Repeat("─", 40)
	original := "output\n❯ \n" + sep + "\n  status old"
	cleared := "output\n❯ \n" + sep + "\n  status new"
	result := extractOriginalInput(original, cleared, 0)
	if result != "" {
		t.Errorf("expected empty string (no input), got %q", result)
	}
}

func TestExtractOriginalInput_InputAfterStatusBar(t *testing.T) {
	// Status bar is ABOVE the input (layout variant).
	// Input hunk is the last candidate, not second-to-last.
	sep := strings.Repeat("─", 40)
	original := "status old\n" + sep + "\n❯ my input"
	cleared := "status new\n" + sep + "\n❯ "
	result := extractOriginalInput(original, cleared, 0)
	if result != "my input" {
		t.Errorf("expected 'my input', got %q", result)
	}
}

func TestExtractOriginalInput_ThreeChangesInputLast(t *testing.T) {
	// Three change regions: header, status, and input (last).
	// The old heuristic (second-to-last) would pick the status hunk.
	sep := strings.Repeat("=", 40)
	original := "header old\n" + sep + "\nstatus old\n" + sep + "\n❯ user input"
	cleared := "header new\n" + sep + "\nstatus new\n" + sep + "\n❯ "
	result := extractOriginalInput(original, cleared, 0)
	if result != "user input" {
		t.Errorf("expected 'user input', got %q", result)
	}
}

func TestExtractOriginalInput_LeadingSpacesWithStatusBar(t *testing.T) {
	// Input with leading spaces + status bar change.
	// Input is before the status bar in normal layout.
	sep := strings.Repeat("─", 40)
	original := "output\n❯    leading spaces\n" + sep + "\n  ctrl+t to hide tasks"
	cleared := "output\n❯ \n" + sep + "\n  ctrl+t · ctrl+g to edit"
	result := extractOriginalInput(original, cleared, 0)
	if result != "   leading spaces" {
		t.Errorf("expected '   leading spaces', got %q", result)
	}
}

// =============================================================================
// ZFC compliance: Multi-client TUI tests
//
// These tests verify extractOriginalInput works with TUI clients other than
// Claude Code. Each test uses realistic prompt/continuation patterns from a
// specific client. If a change to the extraction algorithm breaks any of these,
// the change likely violates ZFC (Zero Framework Cognition) — it probably
// introduced a client-specific assumption.
// =============================================================================

func TestExtractOriginalInput_PythonREPL(t *testing.T) {
	// Python REPL uses ">>> " for prompts and "... " for continuation.
	// When all continuation lines share the same indentation level, the
	// common prefix includes both "... " and the shared indentation.
	// This is ZFC-correct: the algorithm strips the maximal common non-content
	// prefix without client-specific knowledge of where "prompt" ends and
	// "indentation" begins.
	original := "Python 3.12.0\n>>> for i in range(3):\n...     print(i)\n...     total += i"
	cleared := "Python 3.12.0\n>>> "

	result := extractOriginalInput(original, cleared, 0)
	expected := "for i in range(3):\nprint(i)\ntotal += i"
	if result != expected {
		t.Errorf("PythonREPL: expected %q, got %q", expected, result)
	}
}

func TestExtractOriginalInput_PythonREPL_SingleLine(t *testing.T) {
	original := "Python 3.12.0\n>>> x = 42"
	cleared := "Python 3.12.0\n>>> "

	result := extractOriginalInput(original, cleared, 0)
	if result != "x = 42" {
		t.Errorf("PythonREPL single line: expected %q, got %q", "x = 42", result)
	}
}

func TestExtractOriginalInput_BashPrompt(t *testing.T) {
	// Bash with "$ " prompt, single line (no continuation).
	original := "user@host:~\n$ ls -la /tmp"
	cleared := "user@host:~\n$ "

	result := extractOriginalInput(original, cleared, 0)
	if result != "ls -la /tmp" {
		t.Errorf("Bash: expected %q, got %q", "ls -la /tmp", result)
	}
}

func TestExtractOriginalInput_BashMultiLine(t *testing.T) {
	// Bash multi-line with ">" continuation prefix.
	original := "user@host:~\n$ for f in *.go; do\n> echo $f\n> done"
	cleared := "user@host:~\n$ "

	result := extractOriginalInput(original, cleared, 0)
	expected := "for f in *.go; do\necho $f\ndone"
	if result != expected {
		t.Errorf("Bash multiline: expected %q, got %q", expected, result)
	}
}

func TestExtractOriginalInput_IPython(t *testing.T) {
	// IPython uses "In [N]: " for prompts and "   ...: " for continuation.
	// The common prefix of the continuation lines ("   ...:     ") includes
	// shared indentation. The trailing empty continuation line becomes ""
	// after prefix stripping and is removed by TrimRight.
	original := "In [1]: def hello():\n   ...:     return 'world'\n   ...:     "
	cleared := "In [1]: "

	result := extractOriginalInput(original, cleared, 0)
	expected := "def hello():\nreturn 'world'"
	if result != expected {
		t.Errorf("IPython: expected %q, got %q", expected, result)
	}
}

func TestExtractOriginalInput_FishShell(t *testing.T) {
	// Fish shell uses "> " for prompts (varies by theme).
	original := "Welcome to fish\n> git status --short"
	cleared := "Welcome to fish\n> "

	result := extractOriginalInput(original, cleared, 0)
	if result != "git status --short" {
		t.Errorf("Fish: expected %q, got %q", "git status --short", result)
	}
}

func TestExtractOriginalInput_FishMultiLine(t *testing.T) {
	// Fish continuation lines are indented with spaces.
	original := "Welcome to fish\n> for f in *.go\n      echo $f\n  end"
	cleared := "Welcome to fish\n> "

	result := extractOriginalInput(original, cleared, 0)
	// With 2 continuation lines sharing "  " as common non-content prefix
	// (both start with spaces), the prefix is detected and stripped.
	lines := strings.Split(result, "\n")
	if lines[0] != "for f in *.go" {
		t.Errorf("Fish multiline line 0: expected %q, got %q", "for f in *.go", lines[0])
	}
	if len(lines) != 3 {
		t.Fatalf("Fish multiline: expected 3 lines, got %d: %q", len(lines), result)
	}
}

func TestExtractOriginalInput_ZshPrompt(t *testing.T) {
	// Zsh with "% " prompt.
	original := "last login info\n% echo hello"
	cleared := "last login info\n% "

	result := extractOriginalInput(original, cleared, 0)
	if result != "echo hello" {
		t.Errorf("Zsh: expected %q, got %q", "echo hello", result)
	}
}

// =============================================================================
// ZFC compliance: Negative assertions
//
// These tests verify that the extraction algorithm does NOT depend on any
// specific client's artifacts. If extractOriginalInput produces different
// results based on prompt character choice, it has a ZFC violation.
// =============================================================================

func TestExtractOriginalInput_PromptCharAgnostic(t *testing.T) {
	// The same input extracted with different prompt characters must produce
	// identical results. The prompt char is in the EQUAL region of the diff
	// and should not affect extraction.
	prompts := []string{"❯ ", "$ ", "% ", "> ", "→ ", "# ", "λ "}
	for _, prompt := range prompts {
		original := "output\n" + prompt + "hello world"
		cleared := "output\n" + prompt
		result := extractOriginalInput(original, cleared, 0)
		if result != "hello world" {
			t.Errorf("prompt %q: expected 'hello world', got %q", prompt, result)
		}
	}
}

func TestExtractOriginalInput_PromptCharAgnostic_MultiLine(t *testing.T) {
	// Multi-line extraction should also be prompt-char-agnostic.
	// The continuation prefix "  " is the same regardless of prompt char.
	prompts := []string{"❯ ", "$ ", ">>> "}
	for _, prompt := range prompts {
		original := "output\n" + prompt + "line one\n  line two\n  line three"
		cleared := "output\n" + prompt
		result := extractOriginalInput(original, cleared, 0)
		expected := "line one\nline two\nline three"
		if result != expected {
			t.Errorf("prompt %q multiline: expected %q, got %q", prompt, expected, result)
		}
	}
}

func TestExtractOriginalInput_NoSeparatorDependence(t *testing.T) {
	// The algorithm must not depend on specific separator patterns.
	// Different separators (or none) should not change input extraction.
	separators := []string{
		strings.Repeat("─", 40),
		strings.Repeat("=", 40),
		strings.Repeat("━", 40),
		strings.Repeat("-", 80),
	}
	for _, sep := range separators {
		original := "output\n❯ my input\n" + sep + "\nstatus info"
		cleared := "output\n❯ \n" + sep + "\nstatus changed"
		result := extractOriginalInput(original, cleared, 0)
		if result != "my input" {
			t.Errorf("separator %q: expected 'my input', got %q", sep[:4]+"...", result)
		}
	}
}

// =============================================================================
// Extension point tests
//
// These verify that small, well-named extraction helpers work correctly in
// isolation. Each function has a clear contract and can be extended without
// touching the main extraction algorithm.
// =============================================================================

func TestDetectContinuationPrefix_IPythonDots(t *testing.T) {
	// IPython continuation: "   ...: " prefix + shared indentation.
	// When all continuation lines share the same indent level, the common
	// prefix includes both the IPython continuation marker and the indent.
	deleted := "def foo():\n   ...:     return 1\n   ...:     pass"
	prefix := detectContinuationPrefix(deleted)
	if prefix != "   ...:     " {
		t.Errorf("expected '   ...:     ', got %q", prefix)
	}
}

func TestDetectContinuationPrefix_PipePrefix(t *testing.T) {
	// Some TUIs use "| " for continuation (e.g., SQL editors).
	deleted := "SELECT *\n| FROM users\n| WHERE id = 1"
	prefix := detectContinuationPrefix(deleted)
	if prefix != "| " {
		t.Errorf("expected '| ', got %q", prefix)
	}
}

func TestTrimToNonContent_ColonPrefix(t *testing.T) {
	// Colon is a valid TUI punctuation character.
	result := trimToNonContent(":  content")
	if result != ":  " {
		t.Errorf("expected ':  ', got %q", result)
	}
}

func TestTrimToNonContent_MixedPunctuation(t *testing.T) {
	// Combination of allowed prompt chars.
	result := trimToNonContent("..> text")
	if result != "..> " {
		t.Errorf("expected '..> ', got %q", result)
	}
}
