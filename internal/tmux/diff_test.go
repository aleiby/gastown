package tmux

import (
	"bytes"
	"testing"
)

func TestMyersDiff_Empty(t *testing.T) {
	diffs := MyersDiff(nil, nil)
	if len(diffs) != 0 {
		t.Fatalf("expected no diffs, got %d", len(diffs))
	}
}

func TestMyersDiff_InsertOnly(t *testing.T) {
	diffs := MyersDiff(nil, []byte("hello"))
	if len(diffs) != 1 || diffs[0].Op != DiffInsert || string(diffs[0].Data) != "hello" {
		t.Fatalf("expected single insert of 'hello', got %v", diffs)
	}
}

func TestMyersDiff_DeleteOnly(t *testing.T) {
	diffs := MyersDiff([]byte("hello"), nil)
	if len(diffs) != 1 || diffs[0].Op != DiffDelete || string(diffs[0].Data) != "hello" {
		t.Fatalf("expected single delete of 'hello', got %v", diffs)
	}
}

func TestMyersDiff_Equal(t *testing.T) {
	diffs := MyersDiff([]byte("same"), []byte("same"))
	if len(diffs) != 1 || diffs[0].Op != DiffEqual || string(diffs[0].Data) != "same" {
		t.Fatalf("expected single equal of 'same', got %v", diffs)
	}
}

func TestMyersDiff_CommonPrefixSuffix(t *testing.T) {
	a := []byte("hello world goodbye")
	b := []byte("hello earth goodbye")
	diffs := MyersDiff(a, b)

	// Should have: equal "hello " + delete "world" + insert "earth" + equal " goodbye"
	// Verify the diff transforms a into b
	result := applyDiffs(a, diffs)
	if !bytes.Equal(result, b) {
		t.Fatalf("applying diffs to %q should produce %q, got %q", a, b, result)
	}
}

func TestMyersDiff_Transforms(t *testing.T) {
	tests := []struct {
		name string
		a, b string
	}{
		{"prepend", "world", "hello world"},
		{"append", "hello", "hello world"},
		{"replace middle", "abcdef", "abXYef"},
		{"multiline delete", "line1\nline2\nline3", "line1\nline3"},
		{"multiline insert", "line1\nline3", "line1\nline2\nline3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diffs := MyersDiff([]byte(tt.a), []byte(tt.b))
			result := applyDiffs([]byte(tt.a), diffs)
			if string(result) != tt.b {
				t.Errorf("applying diffs to %q should produce %q, got %q", tt.a, tt.b, result)
			}
		})
	}
}

func TestGroupHunks_Simple(t *testing.T) {
	diffs := []Diff{
		{DiffEqual, []byte("prefix ")},
		{DiffDelete, []byte("old")},
		{DiffInsert, []byte("new")},
		{DiffEqual, []byte(" suffix")},
	}
	hunks := GroupHunks(diffs)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk, got %d", len(hunks))
	}
	if string(hunks[0].Deleted) != "old" {
		t.Errorf("expected deleted 'old', got %q", hunks[0].Deleted)
	}
	if string(hunks[0].Inserted) != "new" {
		t.Errorf("expected inserted 'new', got %q", hunks[0].Inserted)
	}
}

func TestGroupHunks_AbsorbsSmallEquals(t *testing.T) {
	// Simulates multi-line input where a newline (1 byte) is a spurious EQUAL match
	diffs := []Diff{
		{DiffEqual, []byte("big prefix that is long enough")},
		{DiffDelete, []byte("line1")},
		{DiffEqual, []byte("\n")}, // 1 byte — should be absorbed
		{DiffDelete, []byte("line2")},
		{DiffEqual, []byte("big suffix that is long enough")},
	}
	hunks := GroupHunks(diffs)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk (small equal absorbed), got %d", len(hunks))
	}
	// The absorbed newline appears in both Deleted and Inserted
	if string(hunks[0].Deleted) != "line1\nline2" {
		t.Errorf("expected deleted 'line1\\nline2', got %q", hunks[0].Deleted)
	}
}

func TestGroupHunks_BreaksOnSignificantEqual(t *testing.T) {
	diffs := []Diff{
		{DiffDelete, []byte("first")},
		{DiffEqual, []byte("long enough separator")}, // >= 4 bytes — breaks hunk
		{DiffDelete, []byte("second")},
	}
	hunks := GroupHunks(diffs)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks (significant equal breaks), got %d", len(hunks))
	}
	if string(hunks[0].Deleted) != "first" {
		t.Errorf("hunk 0 deleted: expected 'first', got %q", hunks[0].Deleted)
	}
	if string(hunks[1].Deleted) != "second" {
		t.Errorf("hunk 1 deleted: expected 'second', got %q", hunks[1].Deleted)
	}
}

func TestGroupHunks_ExactThreshold(t *testing.T) {
	// 4 bytes is exactly minEqualToBreakHunk — should break
	diffs := []Diff{
		{DiffDelete, []byte("a")},
		{DiffEqual, []byte("1234")}, // exactly 4 bytes
		{DiffDelete, []byte("b")},
	}
	hunks := GroupHunks(diffs)
	if len(hunks) != 2 {
		t.Fatalf("expected 2 hunks (4 bytes = threshold), got %d", len(hunks))
	}

	// 3 bytes is below threshold — should absorb
	diffs = []Diff{
		{DiffDelete, []byte("a")},
		{DiffEqual, []byte("123")}, // 3 bytes — below threshold
		{DiffDelete, []byte("b")},
	}
	hunks = GroupHunks(diffs)
	if len(hunks) != 1 {
		t.Fatalf("expected 1 hunk (3 bytes < threshold), got %d", len(hunks))
	}
}

// applyDiffs applies a diff sequence to produce the target from the source.
func applyDiffs(source []byte, diffs []Diff) []byte {
	var result []byte
	for _, d := range diffs {
		switch d.Op {
		case DiffEqual, DiffInsert:
			result = append(result, d.Data...)
		case DiffDelete:
			// skip
		}
	}
	return result
}
