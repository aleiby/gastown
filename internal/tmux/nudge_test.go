package tmux

import (
	"strings"
	"testing"
)

func TestFindInputField_Basic(t *testing.T) {
	sep := strings.Repeat("─", 40)
	capture := "output\n" + sep + "\n❯\u00a0hello\n" + sep + "\nstatus"

	fieldLines, paneWidth, ok := findInputField(capture)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if paneWidth != 40 {
		t.Errorf("expected paneWidth 40, got %d", paneWidth)
	}
	if len(fieldLines) != 1 || fieldLines[0] != "❯\u00a0hello" {
		t.Errorf("unexpected fieldLines: %q", fieldLines)
	}
}

func TestFindInputField_NoSeparators(t *testing.T) {
	_, _, ok := findInputField("just some text\nno separators here")
	if ok {
		t.Error("expected ok=false when no separators")
	}
}

func TestFindInputField_OneSeparator(t *testing.T) {
	sep := strings.Repeat("─", 40)
	_, _, ok := findInputField("output\n" + sep + "\ntext")
	if ok {
		t.Error("expected ok=false with only one separator")
	}
}

func TestFindInputField_MultipleSeparatorPairs(t *testing.T) {
	sep := strings.Repeat("─", 40)
	// Four separators — should use the last two
	capture := sep + "\nold field\n" + sep + "\nmiddle\n" + sep + "\n❯\u00a0current input\n" + sep + "\nstatus"

	fieldLines, _, ok := findInputField(capture)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if len(fieldLines) != 1 || fieldLines[0] != "❯\u00a0current input" {
		t.Errorf("expected last field, got %q", fieldLines)
	}
}

func TestExtractOriginalInput_SingleLine(t *testing.T) {
	sep := strings.Repeat("─", 40)
	capture := "output\n" + sep + "\n❯\u00a0hello world\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestExtractOriginalInput_SingleLineRegularSpace(t *testing.T) {
	sep := strings.Repeat("─", 40)
	// Regular space instead of NO-BREAK SPACE
	capture := "output\n" + sep + "\n❯ hello world\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestExtractOriginalInput_MultiLine(t *testing.T) {
	sep := strings.Repeat("─", 40)
	capture := "output\n" + sep + "\n❯\u00a0line one\n  line two\n  line three\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	if result != "line one\nline two\nline three" {
		t.Errorf("expected 'line one\\nline two\\nline three', got %q", result)
	}
}

func TestExtractOriginalInput_Empty(t *testing.T) {
	sep := strings.Repeat("─", 40)
	// Empty prompt — just the prompt character with nothing after
	capture := "output\n" + sep + "\n❯\u00a0\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractOriginalInput_NoSeparators(t *testing.T) {
	result := extractOriginalInput("just some text without separators")
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractOriginalInput_LeadingWhitespace(t *testing.T) {
	sep := strings.Repeat("─", 40)
	capture := "output\n" + sep + "\n❯\u00a0   leading spaces\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	if result != "   leading spaces" {
		t.Errorf("expected '   leading spaces', got %q", result)
	}
}

func TestExtractOriginalInput_PromptMimicking(t *testing.T) {
	sep := strings.Repeat("─", 40)
	// User typed "❯ mimics prompt" — the TUI shows "❯ ❯ mimics prompt"
	// After stripping TUI prompt prefix "❯ ", we get "❯ mimics prompt"
	capture := "output\n" + sep + "\n❯\u00a0❯ mimics prompt\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	if result != "❯ mimics prompt" {
		t.Errorf("expected '❯ mimics prompt', got %q", result)
	}
}

func TestExtractOriginalInput_EmptyContinuation(t *testing.T) {
	sep := strings.Repeat("─", 40)
	// Empty line between content lines (user pressed Enter twice)
	// tmux trims trailing spaces, so the empty continuation is just ""
	capture := "output\n" + sep + "\n❯\u00a0first\n\n  third\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	if result != "first\n\nthird" {
		t.Errorf("expected 'first\\n\\nthird', got %q", result)
	}
}

func TestExtractOriginalInput_WrappedLine(t *testing.T) {
	// Pane width 25 → available width 21 (paneWidth - 4)
	// First line content exactly 21 chars → fullLine → next is visual wrap (join)
	sep := strings.Repeat("─", 25)
	content := strings.Repeat("x", 21) // exactly fills available width
	capture := "output\n" + sep + "\n❯\u00a0" + content + "\n  rest\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	expected := content + "rest"
	if result != expected {
		t.Errorf("expected wrapped join %q, got %q", expected, result)
	}
}

func TestExtractOriginalInput_WrappedLineTrimmedSpace(t *testing.T) {
	// Pane width 25 → available width 21 (paneWidth - 4)
	// Original content: "xxxx...x " (20 x's + trailing space = 21 chars, fills availWidth)
	// tmux capture-pane trims the trailing space → captured as 20 chars
	// fullLine: 20 >= 21-1=20 → true (detected as full despite trim)
	// Padding restores the trimmed space: 21 - 20 = 1 space added before join
	sep := strings.Repeat("─", 25)
	content := strings.Repeat("x", 20) // 20 chars (tmux trimmed the trailing space)
	capture := "output\n" + sep + "\n❯\u00a0" + content + "\n  more\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	expected := content + " more" // space restored by padding
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExtractOriginalInput_WrappedThenNewline(t *testing.T) {
	// Pane width 25 → available width 21 (paneWidth - 4)
	// Line 1: exactly 21 chars → full → join with line 2
	// Line 2: "wrap" (4 chars, < 20) → not full → newline before line 3
	// Line 3: "third"
	sep := strings.Repeat("─", 25)
	content := strings.Repeat("A", 21)
	capture := "output\n" + sep + "\n❯\u00a0" + content + "\n  wrap\n  third\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	expected := content + "wrap\nthird"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExtractOriginalInput_WordWrapped(t *testing.T) {
	// Pane width 25 → available width 21 (paneWidth - 4)
	// Content: "aaaa bbbbb ccccc ddddd" (22 chars) — exceeds availWidth by 1.
	// Claude Code word-wraps at the last space before the limit:
	//   Field[0]: "aaaa bbbbb ccccc" (16 chars — consumed space + "ddddd" = 22 > 21)
	//   Field[1]: "ddddd" (5 chars)
	// Word-wrap detection: 16 + 1 + 5 = 22 > 21 → visual wrap → join with consumed space
	sep := strings.Repeat("─", 25)
	capture := "output\n" + sep + "\n❯\u00a0aaaa bbbbb ccccc\n  ddddd\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	expected := "aaaa bbbbb ccccc ddddd" // word wrap: restore the single consumed space
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestExtractOriginalInput_ContinuationPrefix(t *testing.T) {
	sep := strings.Repeat("─", 40)
	// User input that starts with spaces — the continuation prefix "  " gets stripped
	// but the user's actual leading spaces remain since the TUI adds "  " on top
	// For line j>0: content = TrimPrefix(line, "  ")
	// If user typed "  hello" on a new line, TUI shows "    hello" (2 prefix + 2 user)
	capture := "output\n" + sep + "\n❯\u00a0first\n    indented\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	// "    indented" → TrimPrefix("  ") → "  indented"
	if result != "first\n  indented" {
		t.Errorf("expected 'first\\n  indented', got %q", result)
	}
}

func TestExtractOriginalInput_EmptyField(t *testing.T) {
	sep := strings.Repeat("─", 40)
	// No lines between separators (adjacent separators)
	capture := "output\n" + sep + "\n" + sep + "\nstatus"

	result := extractOriginalInput(capture)
	if result != "" {
		t.Errorf("expected empty string for empty field, got %q", result)
	}
}
