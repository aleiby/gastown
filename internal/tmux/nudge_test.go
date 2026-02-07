package tmux

import (
	"strings"
	"testing"
)

func TestMakeSentinel(t *testing.T) {
	s := makeSentinel()
	if !strings.HasPrefix(s, "§") || !strings.HasSuffix(s, "§") {
		t.Errorf("sentinel should be bookended with section signs, got %q", s)
	}
	// Total should be 6 chars: § + 4 hash chars + §
	inner := s[len("§") : len(s)-len("§")]
	if len(inner) > 4 {
		t.Errorf("sentinel inner should be <= 4 chars, got %d: %q", len(inner), inner)
	}
	// Two sentinels should be different (nanosecond resolution)
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

func TestExtractOriginalInput_SingleLine(t *testing.T) {
	sentinel := "§ABCD§"

	// BEFORE: terminal output ending with prompt + sentinel + user input
	before := "output line 1\noutput line 2\n❯ " + sentinel + "hello world"

	// AFTER: same terminal output, prompt is now empty (input was cleared)
	after := "output line 1\noutput line 2\n❯ "

	result := extractOriginalInput(before, after, sentinel)
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestExtractOriginalInput_MultiLine(t *testing.T) {
	sentinel := "§ABCD§"

	// Multi-line input with continuation prefix
	before := "output\n❯ " + sentinel + "line one\n  line two\n  line three"
	after := "output\n❯ "

	result := extractOriginalInput(before, after, sentinel)
	if result != "line one\nline two\nline three" {
		t.Errorf("expected 'line one\\nline two\\nline three', got %q", result)
	}
}

func TestExtractOriginalInput_Empty(t *testing.T) {
	sentinel := "§ABCD§"

	// No user input — sentinel is right at cursor with nothing after
	before := "output\n❯ " + sentinel
	after := "output\n❯ "

	result := extractOriginalInput(before, after, sentinel)
	if result != "" {
		t.Errorf("expected empty string, got %q", result)
	}
}

func TestExtractOriginalInput_SentinelNotInDiff(t *testing.T) {
	result := extractOriginalInput("same content", "same content", "§NOPE§")
	if result != "" {
		t.Errorf("expected empty string when sentinel not found, got %q", result)
	}
}
