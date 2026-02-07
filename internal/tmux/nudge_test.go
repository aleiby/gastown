package tmux

import (
	"testing"
)

func TestExtractDeletedInput_SingleLine(t *testing.T) {
	// BEFORE: terminal output ending with prompt + user input
	before := "output line 1\noutput line 2\n❯ hello world"

	// AFTER: same terminal output, prompt is now empty (input was cleared)
	after := "output line 1\noutput line 2\n❯ "

	result := extractDeletedInput(before, after)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestExtractDeletedInput_MultiLine(t *testing.T) {
	// Multi-line input with continuation prefix
	before := "output\n❯ line one\n  line two\n  line three"
	after := "output\n❯ "

	result := extractDeletedInput(before, after)
	if result != "line one\nline two\nline three" {
		t.Errorf("expected 'line one\\nline two\\nline three', got %q", result)
	}
}

func TestExtractDeletedInput_Empty(t *testing.T) {
	// No user input — nothing deleted
	before := "output\n❯ "
	after := "output\n❯ "

	result := extractDeletedInput(before, after)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractDeletedInput_NoDiff(t *testing.T) {
	result := extractDeletedInput("same content", "same content")
	if result != "" {
		t.Errorf("expected empty string when no diff, got %q", result)
	}
}
