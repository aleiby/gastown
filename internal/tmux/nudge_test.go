package tmux

import (
	"strings"
	"testing"
)

func TestMakeSentinel(t *testing.T) {
	s := makeSentinel()
	if !strings.HasPrefix(s, "\u00a7") || !strings.HasSuffix(s, "\u00a7") {
		t.Errorf("sentinel should be bookended with section signs, got %q", s)
	}
	// Two sentinels should be different (nanosecond resolution)
	s2 := makeSentinel()
	if s == s2 {
		t.Errorf("two sentinels should differ, both were %q", s)
	}
}

func TestFindSentinelFromEnd(t *testing.T) {
	sentinel := "\u00a7TEST\u00a7"
	lines := []string{
		"some output",
		"more output",
		"\u2757 " + sentinel + "hello world",
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
	_, _, found := findSentinelFromEnd(lines, "\u00a7NOPE\u00a7")
	if found {
		t.Error("should not find sentinel that isn't there")
	}
}

func TestExtractOriginalInput_SingleLine(t *testing.T) {
	sentinel := "\u00a7ABCD\u00a7"

	// BEFORE: terminal output ending with prompt + sentinel + user input
	before := "output line 1\noutput line 2\n\u2757 " + sentinel + "hello world"

	// AFTER: same terminal output, prompt is now empty (input was cleared)
	after := "output line 1\noutput line 2\n\u2757 "

	result := extractOriginalInput(before, after, sentinel)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestExtractOriginalInput_MultiLine(t *testing.T) {
	sentinel := "\u00a7ABCD\u00a7"

	// Multi-line input with continuation prefix
	before := "output\n\u2757 " + sentinel + "line one\n  line two\n  line three"
	after := "output\n\u2757 "

	result := extractOriginalInput(before, after, sentinel)
	if result != "line one\nline two\nline three" {
		t.Errorf("expected 'line one\\nline two\\nline three', got %q", result)
	}
}

func TestExtractOriginalInput_Empty(t *testing.T) {
	sentinel := "\u00a7ABCD\u00a7"

	// No user input — sentinel is right at cursor with nothing after
	before := "output\n\u2757 " + sentinel
	after := "output\n\u2757 "

	result := extractOriginalInput(before, after, sentinel)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractOriginalInput_SentinelNotInDiff(t *testing.T) {
	// If the sentinel isn't in any diff hunk, return empty
	result := extractOriginalInput("same content", "same content", "\u00a7NOPE\u00a7")
	if result != "" {
		t.Errorf("expected empty string when sentinel not found, got %q", result)
	}
}
