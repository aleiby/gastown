package tmux

import (
	"bytes"
	"testing"
)

func TestCommonPrefixLen(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected int
	}{
		{
			name:     "identical",
			a:        "hello",
			b:        "hello",
			expected: 5,
		},
		{
			name:     "no common prefix",
			a:        "abc",
			b:        "xyz",
			expected: 0,
		},
		{
			name:     "partial prefix",
			a:        "hello world",
			b:        "hello there",
			expected: 6, // "hello "
		},
		{
			name:     "a is prefix of b",
			a:        "hello",
			b:        "hello world",
			expected: 5,
		},
		{
			name:     "b is prefix of a",
			a:        "hello world",
			b:        "hello",
			expected: 5,
		},
		{
			name:     "empty a",
			a:        "",
			b:        "hello",
			expected: 0,
		},
		{
			name:     "empty b",
			a:        "hello",
			b:        "",
			expected: 0,
		},
		{
			name:     "both empty",
			a:        "",
			b:        "",
			expected: 0,
		},
		{
			name:     "unicode prefix",
			a:        "❯ hello",
			b:        "❯ world",
			expected: 4, // "❯ " is 4 bytes
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := commonPrefixLen([]byte(tt.a), []byte(tt.b))
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestCommonSuffixLen(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected int
	}{
		{
			name:     "identical",
			a:        "hello",
			b:        "hello",
			expected: 5,
		},
		{
			name:     "no common suffix",
			a:        "abc",
			b:        "xyz",
			expected: 0,
		},
		{
			name:     "partial suffix",
			a:        "hello world",
			b:        "brave world",
			expected: 6, // " world"
		},
		{
			name:     "a is suffix of b",
			a:        "world",
			b:        "hello world",
			expected: 5,
		},
		{
			name:     "b is suffix of a",
			a:        "hello world",
			b:        "world",
			expected: 5,
		},
		{
			name:     "empty a",
			a:        "",
			b:        "hello",
			expected: 0,
		},
		{
			name:     "empty b",
			a:        "hello",
			b:        "",
			expected: 0,
		},
		{
			name:     "newline suffix",
			a:        "line1\nline2\n",
			b:        "different\nline2\n",
			expected: 7, // "\nline2\n"
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := commonSuffixLen([]byte(tt.a), []byte(tt.b))
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

func TestMyersDiff_TrivialCases(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected []Diff
	}{
		{
			name:     "both empty",
			a:        "",
			b:        "",
			expected: nil,
		},
		{
			name:     "a empty - pure insert",
			a:        "",
			b:        "hello",
			expected: []Diff{{DiffInsert, []byte("hello")}},
		},
		{
			name:     "b empty - pure delete",
			a:        "hello",
			b:        "",
			expected: []Diff{{DiffDelete, []byte("hello")}},
		},
		{
			name:     "identical",
			a:        "hello",
			b:        "hello",
			expected: []Diff{{DiffEqual, []byte("hello")}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := MyersDiff([]byte(tt.a), []byte(tt.b))
			if !diffsEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", formatDiffs(tt.expected), formatDiffs(result))
			}
		})
	}
}

func TestMyersDiff_SimpleEdits(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
		// We verify by reconstructing b from a using the diff
	}{
		{
			name: "single char insert",
			a:    "ac",
			b:    "abc",
		},
		{
			name: "single char delete",
			a:    "abc",
			b:    "ac",
		},
		{
			name: "single char replace",
			a:    "abc",
			b:    "adc",
		},
		{
			name: "prefix insert",
			a:    "world",
			b:    "hello world",
		},
		{
			name: "suffix insert",
			a:    "hello",
			b:    "hello world",
		},
		{
			name: "middle insert",
			a:    "helloworld",
			b:    "hello world",
		},
		{
			name: "prefix delete",
			a:    "hello world",
			b:    "world",
		},
		{
			name: "suffix delete",
			a:    "hello world",
			b:    "hello",
		},
		{
			name: "middle delete",
			a:    "hello world",
			b:    "helloworld",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := MyersDiff([]byte(tt.a), []byte(tt.b))
			reconstructed := applyDiffs([]byte(tt.a), diffs)
			if !bytes.Equal(reconstructed, []byte(tt.b)) {
				t.Errorf("reconstruction failed:\n  a: %q\n  b: %q\n  got: %q\n  diffs: %v",
					tt.a, tt.b, string(reconstructed), formatDiffs(diffs))
			}
		})
	}
}

func TestMyersDiff_ComplexEdits(t *testing.T) {
	tests := []struct {
		name string
		a    string
		b    string
	}{
		{
			name: "complete replacement",
			a:    "hello",
			b:    "world",
		},
		{
			name: "multiple edits",
			a:    "the quick brown fox",
			b:    "a slow red fox",
		},
		{
			name: "with newlines",
			a:    "line1\nline2\nline3\n",
			b:    "line1\nmodified\nline3\n",
		},
		{
			name: "unicode content",
			a:    "❯ hello world",
			b:    "❯ goodbye world",
		},
		{
			name: "nudge scenario - clean",
			a:    "prefix\n❯ \nsuffix",
			b:    "prefix\n❯ [NUDGE]\nsuffix",
		},
		{
			name: "nudge scenario - with gap typing",
			a:    "prefix\n❯ original input\nsuffix",
			b:    "prefix\n❯ gap[NUDGE]\nsuffix",
		},
		{
			name: "nudge scenario - with interrupted message",
			a:    "output\n───\n❯ user input\n───\nstatus",
			b:    "output\nInterrupted\n───\n❯ [NUDGE]\n───\nstatus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := MyersDiff([]byte(tt.a), []byte(tt.b))
			reconstructed := applyDiffs([]byte(tt.a), diffs)
			if !bytes.Equal(reconstructed, []byte(tt.b)) {
				t.Errorf("reconstruction failed:\n  a: %q\n  b: %q\n  got: %q\n  diffs: %v",
					tt.a, tt.b, string(reconstructed), formatDiffs(diffs))
			}
		})
	}
}

func TestMyersDiff_LargeCommonPrefix(t *testing.T) {
	// Simulate large scrollback with small change at end
	prefix := bytes.Repeat([]byte("scrollback line\n"), 100)
	a := append(append([]byte(nil), prefix...), []byte("❯ original input\n")...)
	b := append(append([]byte(nil), prefix...), []byte("❯ [NUDGE]\n")...)

	diffs := MyersDiff(a, b)
	reconstructed := applyDiffs(a, diffs)
	if !bytes.Equal(reconstructed, b) {
		t.Errorf("reconstruction failed for large prefix scenario")
	}

	// Verify that the diff is efficient (should have large Equal block for prefix)
	if len(diffs) < 1 || diffs[0].Op != DiffEqual {
		t.Errorf("expected first diff to be Equal (common prefix)")
	}
	if len(diffs[0].Data) < len(prefix) {
		t.Errorf("expected Equal block to contain at least the prefix")
	}
}

func TestGroupHunks(t *testing.T) {
	tests := []struct {
		name     string
		diffs    []Diff
		expected []Hunk
	}{
		{
			name:     "no diffs",
			diffs:    nil,
			expected: nil,
		},
		{
			name: "only equal",
			diffs: []Diff{
				{DiffEqual, []byte("hello")},
			},
			expected: nil,
		},
		{
			name: "single delete",
			diffs: []Diff{
				{DiffDelete, []byte("hello")},
			},
			expected: []Hunk{
				{Deleted: []byte("hello"), Inserted: nil},
			},
		},
		{
			name: "single insert",
			diffs: []Diff{
				{DiffInsert, []byte("hello")},
			},
			expected: []Hunk{
				{Deleted: nil, Inserted: []byte("hello")},
			},
		},
		{
			name: "delete and insert (replacement)",
			diffs: []Diff{
				{DiffDelete, []byte("old")},
				{DiffInsert, []byte("new")},
			},
			expected: []Hunk{
				{Deleted: []byte("old"), Inserted: []byte("new")},
			},
		},
		{
			name: "multiple hunks",
			diffs: []Diff{
				{DiffEqual, []byte("prefix")},
				{DiffDelete, []byte("old1")},
				{DiffInsert, []byte("new1")},
				{DiffEqual, []byte("middle")},
				{DiffDelete, []byte("old2")},
				{DiffInsert, []byte("new2")},
				{DiffEqual, []byte("suffix")},
			},
			expected: []Hunk{
				{Deleted: []byte("old1"), Inserted: []byte("new1")},
				{Deleted: []byte("old2"), Inserted: []byte("new2")},
			},
		},
		{
			name: "hunk at end",
			diffs: []Diff{
				{DiffEqual, []byte("prefix")},
				{DiffDelete, []byte("old")},
				{DiffInsert, []byte("new")},
			},
			expected: []Hunk{
				{Deleted: []byte("old"), Inserted: []byte("new")},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GroupHunks(tt.diffs)
			if !hunksEqual(result, tt.expected) {
				t.Errorf("expected %v, got %v", formatHunks(tt.expected), formatHunks(result))
			}
		})
	}
}

func TestFindNudgeHunk(t *testing.T) {
	tests := []struct {
		name           string
		hunks          []Hunk
		nudge          string
		wantOriginal   string
		wantBefore     string
		wantAfter      string
		wantFound      bool
	}{
		{
			name:      "no hunks",
			hunks:     nil,
			nudge:     "[NUDGE]",
			wantFound: false,
		},
		{
			name: "nudge not found",
			hunks: []Hunk{
				{Deleted: []byte("old"), Inserted: []byte("new")},
			},
			nudge:     "[NUDGE]",
			wantFound: false,
		},
		{
			name: "clean delivery - nudge only",
			hunks: []Hunk{
				{Deleted: []byte(""), Inserted: []byte("[NUDGE]")},
			},
			nudge:        "[NUDGE]",
			wantOriginal: "",
			wantBefore:   "",
			wantAfter:    "",
			wantFound:    true,
		},
		{
			name: "with original input",
			hunks: []Hunk{
				{Deleted: []byte("original input"), Inserted: []byte("[NUDGE]")},
			},
			nudge:        "[NUDGE]",
			wantOriginal: "original input",
			wantBefore:   "",
			wantAfter:    "",
			wantFound:    true,
		},
		{
			name: "with gap typing",
			hunks: []Hunk{
				{Deleted: []byte("original"), Inserted: []byte("gap[NUDGE]")},
			},
			nudge:        "[NUDGE]",
			wantOriginal: "original",
			wantBefore:   "gap",
			wantAfter:    "",
			wantFound:    true,
		},
		{
			name: "with after-nudge typing",
			hunks: []Hunk{
				{Deleted: []byte("original"), Inserted: []byte("[NUDGE]after")},
			},
			nudge:        "[NUDGE]",
			wantOriginal: "original",
			wantBefore:   "",
			wantAfter:    "after",
			wantFound:    true,
		},
		{
			name: "with gap and after-nudge typing",
			hunks: []Hunk{
				{Deleted: []byte("original"), Inserted: []byte("before[NUDGE]after")},
			},
			nudge:        "[NUDGE]",
			wantOriginal: "original",
			wantBefore:   "before",
			wantAfter:    "after",
			wantFound:    true,
		},
		{
			name: "nudge in second hunk - iterates backward",
			hunks: []Hunk{
				{Deleted: []byte("first"), Inserted: []byte("other change")},
				{Deleted: []byte("original"), Inserted: []byte("[NUDGE]")},
			},
			nudge:        "[NUDGE]",
			wantOriginal: "original",
			wantBefore:   "",
			wantAfter:    "",
			wantFound:    true,
		},
		{
			name: "nudge in first of three hunks - finds last match",
			hunks: []Hunk{
				{Deleted: []byte("a"), Inserted: []byte("[NUDGE]1")},
				{Deleted: []byte("b"), Inserted: []byte("change")},
				{Deleted: []byte("c"), Inserted: []byte("[NUDGE]2")},
			},
			nudge:        "[NUDGE]",
			wantOriginal: "c",       // Last hunk with nudge
			wantBefore:   "",
			wantAfter:    "2",
			wantFound:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			original, before, after, found := FindNudgeHunk(tt.hunks, []byte(tt.nudge))
			if found != tt.wantFound {
				t.Errorf("found: expected %v, got %v", tt.wantFound, found)
				return
			}
			if !found {
				return
			}
			if string(original) != tt.wantOriginal {
				t.Errorf("original: expected %q, got %q", tt.wantOriginal, string(original))
			}
			if string(before) != tt.wantBefore {
				t.Errorf("before: expected %q, got %q", tt.wantBefore, string(before))
			}
			if string(after) != tt.wantAfter {
				t.Errorf("after: expected %q, got %q", tt.wantAfter, string(after))
			}
		})
	}
}

func TestIsCleanDelivery(t *testing.T) {
	tests := []struct {
		name     string
		before   string
		after    string
		expected bool
	}{
		{
			name:     "both empty",
			before:   "",
			after:    "",
			expected: true,
		},
		{
			name:     "whitespace only",
			before:   "  ",
			after:    "\t\n",
			expected: true,
		},
		{
			name:     "gap typing",
			before:   "typed",
			after:    "",
			expected: false,
		},
		{
			name:     "after-nudge typing",
			before:   "",
			after:    "typed",
			expected: false,
		},
		{
			name:     "both have typing",
			before:   "gap",
			after:    "after",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsCleanDelivery([]byte(tt.before), []byte(tt.after))
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestTextToRestore(t *testing.T) {
	tests := []struct {
		name     string
		original string
		before   string
		after    string
		expected string
	}{
		{
			name:     "all empty",
			original: "",
			before:   "",
			after:    "",
			expected: "",
		},
		{
			name:     "only original",
			original: "hello",
			before:   "",
			after:    "",
			expected: "hello",
		},
		{
			name:     "all parts",
			original: "I'm starting to type some instructio",
			before:   "ns for you t",
			after:    "",
			expected: "I'm starting to type some instructions for you t",
		},
		{
			name:     "with after-nudge",
			original: "original",
			before:   "gap",
			after:    "after",
			expected: "originalgapafter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TextToRestore([]byte(tt.original), []byte(tt.before), []byte(tt.after))
			if string(result) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(result))
			}
		})
	}
}

func TestFindNudgeInDiff(t *testing.T) {
	tests := []struct {
		name           string
		before         string
		after          string
		nudge          string
		wantOriginal   string
		wantBefore     string
		wantAfter      string
		wantFound      bool
	}{
		{
			name:      "nudge not in after",
			before:    "hello",
			after:     "world",
			nudge:     "[NUDGE]",
			wantFound: false,
		},
		{
			// Use ">= 10 char" suffix so it properly breaks the hunk (matches real terminal behavior)
			name:         "clean insertion",
			before:       "prefix | suffix |",
			after:        "prefix [NUDGE] | suffix |",
			nudge:        "[NUDGE]",
			wantOriginal: "",
			wantBefore:   "",
			wantAfter:    " ", // The space between nudge and suffix is part of the insert
			wantFound:    true,
		},
		{
			name:         "replacement",
			before:       "prefix old | suffix |",
			after:        "prefix [NUDGE] | suffix |",
			nudge:        "[NUDGE]",
			wantOriginal: "old",
			wantBefore:   "",
			wantAfter:    "",
			wantFound:    true,
		},
		{
			name:         "with gap typing",
			before:       "prefix XXXXXXXX | suffix |",
			after:        "prefix zzz[NUDGE] | suffix |",
			nudge:        "[NUDGE]",
			wantOriginal: "XXXXXXXX",
			wantBefore:   "zzz",
			wantAfter:    "",
			wantFound:    true,
		},
		{
			name:         "with after-nudge typing",
			before:       "prefix XXXXXXXX | suffix |",
			after:        "prefix [NUDGE]yyy | suffix |",
			nudge:        "[NUDGE]",
			wantOriginal: "XXXXXXXX",
			wantBefore:   "",
			wantAfter:    "yyy",
			wantFound:    true,
		},
		{
			name:         "with both gap and after-nudge",
			before:       "prefix XXXXXXXX | suffix |",
			after:        "prefix zzz[NUDGE]yyy | suffix |",
			nudge:        "[NUDGE]",
			wantOriginal: "XXXXXXXX",
			wantBefore:   "zzz",
			wantAfter:    "yyy",
			wantFound:    true,
		},
		{
			name:         "nudge at start",
			before:       "old | content |",
			after:        "[NUDGE] | content |",
			nudge:        "[NUDGE]",
			wantOriginal: "old",
			wantBefore:   "",
			wantAfter:    "",
			wantFound:    true,
		},
		{
			name:         "nudge at end",
			before:       "content old",
			after:        "content [NUDGE]",
			nudge:        "[NUDGE]",
			wantOriginal: "old",
			wantBefore:   "",
			wantAfter:    "",
			wantFound:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := MyersDiff([]byte(tt.before), []byte(tt.after))
			original, before, after, found := FindNudgeInDiff(
				[]byte(tt.before), []byte(tt.after), []byte(tt.nudge), diffs)

			if found != tt.wantFound {
				t.Logf("Diffs: %v", formatDiffs(diffs))
				t.Errorf("found: expected %v, got %v", tt.wantFound, found)
				return
			}
			if !found {
				return
			}
			if string(original) != tt.wantOriginal {
				t.Logf("Diffs: %v", formatDiffs(diffs))
				t.Errorf("original: expected %q, got %q", tt.wantOriginal, string(original))
			}
			if string(before) != tt.wantBefore {
				t.Logf("Diffs: %v", formatDiffs(diffs))
				t.Errorf("before: expected %q, got %q", tt.wantBefore, string(before))
			}
			if string(after) != tt.wantAfter {
				t.Logf("Diffs: %v", formatDiffs(diffs))
				t.Errorf("after: expected %q, got %q", tt.wantAfter, string(after))
			}
		})
	}
}

func TestEndToEnd_NudgeScenario(t *testing.T) {
	// This test simulates the full nudge delivery scenario from the design doc
	before := `● Received "no restore" test. Standing by.

❯ [from crew/holden] Reliability test 1
  ⎿  Interrupted

❯ [from crew/holden] Reliability test 3

● Received tests 1 and 3.

─────────────────────────────────────────
❯ I'm starting to type some instructio
─────────────────────────────────────────
  ⏵⏵ bypass permissions on`

	after := `● Received "no restore" test. Standing by.

❯ [from crew/holden] Reliability test 1
  ⎿  Interrupted

❯ [from crew/holden] Reliability test 3

● Received tests 1 and 3.

─────────────────────────────────────────
❯ ns for you t[from crew/holden] Reliability test 7
─────────────────────────────────────────
  ⏵⏵ bypass permissions on   AD: DRINK OVALTINE!!`

	nudge := "[from crew/holden] Reliability test 7"

	// Compute diff
	diffs := MyersDiff([]byte(before), []byte(after))

	// Verify reconstruction
	reconstructed := applyDiffs([]byte(before), diffs)
	if !bytes.Equal(reconstructed, []byte(after)) {
		t.Fatalf("reconstruction failed")
	}

	// Find nudge using position-based approach (more robust than hunk-based)
	original, gap, afterNudge, found := FindNudgeInDiff([]byte(before), []byte(after), []byte(nudge), diffs)
	if !found {
		t.Fatalf("nudge %q not found in diff", nudge)
	}

	// Verify extraction
	expectedOriginal := "I'm starting to type some instructio"
	expectedGap := "ns for you t"
	expectedAfter := ""

	if string(original) != expectedOriginal {
		t.Errorf("original: expected %q, got %q", expectedOriginal, string(original))
	}
	if string(gap) != expectedGap {
		t.Errorf("gap: expected %q, got %q", expectedGap, string(gap))
	}
	if string(afterNudge) != expectedAfter {
		t.Errorf("afterNudge: expected %q, got %q", expectedAfter, string(afterNudge))
	}

	// Verify text to restore
	restore := TextToRestore(original, gap, afterNudge)
	expectedRestore := "I'm starting to type some instructions for you t"
	if string(restore) != expectedRestore {
		t.Errorf("restore: expected %q, got %q", expectedRestore, string(restore))
	}

	// This is NOT a clean delivery (has gap typing)
	if IsCleanDelivery(gap, afterNudge) {
		t.Errorf("expected non-clean delivery due to gap typing")
	}
}

func TestEndToEnd_CleanDelivery(t *testing.T) {
	// BEFORE has "❯ " (prompt with trailing space, empty input)
	// AFTER has "❯ [NUDGE]" (prompt with nudge in input field)
	// This is a clean delivery - only the nudge was inserted
	before := "output\n───\n❯ \n───\nstatus"
	after := "output\n───\n❯ [NUDGE]\n───\nstatus"
	nudge := "[NUDGE]"

	diffs := MyersDiff([]byte(before), []byte(after))
	hunks := GroupHunks(diffs)
	original, gap, afterNudge, found := FindNudgeHunk(hunks, []byte(nudge))

	if !found {
		t.Fatalf("nudge not found")
	}
	if string(original) != "" {
		t.Errorf("expected empty original, got %q", string(original))
	}
	if string(gap) != "" {
		t.Errorf("expected empty gap, got %q", string(gap))
	}
	if string(afterNudge) != "" {
		t.Errorf("expected empty afterNudge, got %q", string(afterNudge))
	}
	if !IsCleanDelivery(gap, afterNudge) {
		t.Errorf("expected clean delivery")
	}
}

// Helper functions

func diffsEqual(a, b []Diff) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Op != b[i].Op || !bytes.Equal(a[i].Data, b[i].Data) {
			return false
		}
	}
	return true
}

func hunksEqual(a, b []Hunk) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i].Deleted, b[i].Deleted) || !bytes.Equal(a[i].Inserted, b[i].Inserted) {
			return false
		}
	}
	return true
}

func formatDiffs(diffs []Diff) string {
	if diffs == nil {
		return "nil"
	}
	var buf bytes.Buffer
	buf.WriteString("[")
	for i, d := range diffs {
		if i > 0 {
			buf.WriteString(", ")
		}
		switch d.Op {
		case DiffEqual:
			buf.WriteString("=")
		case DiffDelete:
			buf.WriteString("-")
		case DiffInsert:
			buf.WriteString("+")
		}
		buf.WriteString(`"`)
		buf.Write(d.Data)
		buf.WriteString(`"`)
	}
	buf.WriteString("]")
	return buf.String()
}

func formatHunks(hunks []Hunk) string {
	if hunks == nil {
		return "nil"
	}
	var buf bytes.Buffer
	buf.WriteString("[")
	for i, h := range hunks {
		if i > 0 {
			buf.WriteString(", ")
		}
		buf.WriteString("{-")
		buf.Write(h.Deleted)
		buf.WriteString(" +")
		buf.Write(h.Inserted)
		buf.WriteString("}")
	}
	buf.WriteString("]")
	return buf.String()
}

// applyDiffs reconstructs the target from source using the diff operations.
func applyDiffs(source []byte, diffs []Diff) []byte {
	var result []byte
	for _, d := range diffs {
		switch d.Op {
		case DiffEqual, DiffInsert:
			result = append(result, d.Data...)
		case DiffDelete:
			// Don't add deleted content to result
		}
	}
	return result
}
