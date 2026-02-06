package tmux

import (
	"testing"
)

func TestTailLines(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		n        int
		expected []string
	}{
		{
			name:     "empty data",
			data:     "",
			n:        5,
			expected: nil,
		},
		{
			name:     "zero lines requested",
			data:     "line1\nline2\n",
			n:        0,
			expected: nil,
		},
		{
			name:     "single line no newline",
			data:     "only line",
			n:        5,
			expected: []string{"only line"},
		},
		{
			name:     "single line with newline",
			data:     "only line\n",
			n:        5,
			expected: []string{"only line"},
		},
		{
			name:     "multiple lines request more",
			data:     "line1\nline2\nline3\n",
			n:        10,
			expected: []string{"line1", "line2", "line3"},
		},
		{
			name:     "multiple lines request fewer",
			data:     "line1\nline2\nline3\nline4\nline5\n",
			n:        2,
			expected: []string{"line4", "line5"},
		},
		{
			name:     "exact match",
			data:     "line1\nline2\nline3\n",
			n:        3,
			expected: []string{"line1", "line2", "line3"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tailLines([]byte(tt.data), tt.n)

			if len(result) != len(tt.expected) {
				t.Errorf("expected %d lines, got %d", len(tt.expected), len(result))
				return
			}

			for i, line := range result {
				if string(line) != tt.expected[i] {
					t.Errorf("line %d: expected %q, got %q", i, tt.expected[i], string(line))
				}
			}
		})
	}
}

func TestHasPastedTextPlaceholder(t *testing.T) {
	tests := []struct {
		name     string
		data     string
		maxLines int
		expected bool
	}{
		{
			name:     "no placeholder",
			data:     "line1\nline2\nline3\n",
			maxLines: 50,
			expected: false,
		},
		{
			name:     "placeholder present",
			data:     "some text\n[Pasted text #3 +47 lines]\nmore text\n",
			maxLines: 50,
			expected: true,
		},
		{
			name:     "placeholder with different numbers",
			data:     "prefix\n[Pasted text #123 +999 lines]\nsuffix\n",
			maxLines: 50,
			expected: true,
		},
		{
			name:     "similar but not matching",
			data:     "This is [Pasted text] but no numbers\n",
			maxLines: 50,
			expected: false,
		},
		{
			name:     "placeholder outside scan range",
			data:     "[Pasted text #1 +10 lines]\nline2\nline3\nline4\nline5\n",
			maxLines: 2,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasPastedTextPlaceholder([]byte(tt.data), tt.maxLines)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}


