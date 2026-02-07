package tmux

import "bytes"

// DiffOp represents the type of diff operation.
type DiffOp int

const (
	DiffEqual DiffOp = iota
	DiffDelete
	DiffInsert
)

// Diff represents a single diff operation.
type Diff struct {
	Op   DiffOp
	Data []byte
}

// Hunk represents a contiguous region of changes.
// A hunk contains deletions from the source and insertions in the target.
type Hunk struct {
	Deleted  []byte // What was in the source (BEFORE)
	Inserted []byte // What's in the target (AFTER)
}

// minEqualToBreakHunk is the minimum size of an Equal section required to end a hunk.
// Small Equals (like single-character matches in character-level Myers diff) are
// absorbed into the hunk. This prevents fragmentation due to spurious matches
// between unrelated text that happens to share some characters.
const minEqualToBreakHunk = 4

// MyersDiff computes the diff between two byte slices using Myers' algorithm.
// Returns a slice of Diff operations that transform 'a' into 'b'.
//
// Optimizations:
// - Common prefix is skipped (not diffed)
// - Common suffix is skipped (not diffed)
// - Only the differing middle portion is processed by Myers
//
// Complexity: O(ND) where N is the length of the differing region
// and D is the edit distance. For similar sequences, this is very fast.
func MyersDiff(a, b []byte) []Diff {
	n, m := len(a), len(b)

	// Handle trivial cases
	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		return []Diff{{DiffInsert, b}}
	}
	if m == 0 {
		return []Diff{{DiffDelete, a}}
	}
	if bytes.Equal(a, b) {
		return []Diff{{DiffEqual, a}}
	}

	// Skip common prefix
	prefixLen := commonPrefixLen(a, b)
	prefix := a[:prefixLen]
	a = a[prefixLen:]
	b = b[prefixLen:]

	// Skip common suffix
	suffixLen := commonSuffixLen(a, b)
	suffix := a[len(a)-suffixLen:]
	a = a[:len(a)-suffixLen]
	b = b[:len(b)-suffixLen]

	// Compute diff of the middle part
	var middle []Diff
	if len(a) == 0 && len(b) == 0 {
		// Nothing left to diff
	} else if len(a) == 0 {
		middle = []Diff{{DiffInsert, b}}
	} else if len(b) == 0 {
		middle = []Diff{{DiffDelete, a}}
	} else {
		middle = computeMyersDiff(a, b)
	}

	// Combine prefix + middle + suffix
	var result []Diff
	if len(prefix) > 0 {
		result = append(result, Diff{DiffEqual, prefix})
	}
	result = append(result, middle...)
	if len(suffix) > 0 {
		result = append(result, Diff{DiffEqual, suffix})
	}

	return result
}

// commonPrefixLen returns the length of the common prefix of a and b.
func commonPrefixLen(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return i
		}
	}
	return n
}

// commonSuffixLen returns the length of the common suffix of a and b.
func commonSuffixLen(a, b []byte) int {
	n := len(a)
	m := len(b)
	max := n
	if m < max {
		max = m
	}
	for i := 0; i < max; i++ {
		if a[n-1-i] != b[m-1-i] {
			return i
		}
	}
	return max
}

// computeMyersDiff implements the core Myers algorithm.
// Assumes a and b are non-empty and have no common prefix/suffix.
func computeMyersDiff(a, b []byte) []Diff {
	n, m := len(a), len(b)
	max := n + m

	// v[k+max] stores the x coordinate of the furthest reaching point on diagonal k
	v := make([]int, 2*max+1)

	// trace stores the v array at each step for backtracking
	var trace [][]int

	// Find the shortest edit script
	for d := 0; d <= max; d++ {
		// Copy v for backtracking
		vCopy := make([]int, len(v))
		copy(vCopy, v)
		trace = append(trace, vCopy)

		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1+max] < v[k+1+max]) {
				x = v[k+1+max] // Move down (insert from b)
			} else {
				x = v[k-1+max] + 1 // Move right (delete from a)
			}
			y := x - k

			// Follow diagonal (matches)
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}

			v[k+max] = x

			if x >= n && y >= m {
				// Found the end, backtrack to get the diff
				return backtrack(trace, a, b, d, max)
			}
		}
	}

	// Should never reach here for valid inputs
	return nil
}

// backtrack reconstructs the diff from the trace.
func backtrack(trace [][]int, a, b []byte, d, max int) []Diff {
	n, m := len(a), len(b)
	x, y := n, m

	var ops []Diff

	for d > 0 {
		v := trace[d]
		k := x - y

		var prevK int
		if k == -d || (k != d && v[k-1+max] < v[k+1+max]) {
			prevK = k + 1 // Came from insert
		} else {
			prevK = k - 1 // Came from delete
		}

		prevX := v[prevK+max]
		prevY := prevX - prevK

		// Add diagonal matches (in reverse, we'll reverse later)
		for x > prevX && y > prevY {
			x--
			y--
			ops = append(ops, Diff{DiffEqual, a[x : x+1]})
		}

		// Add the edit operation
		if d > 0 {
			if k == prevK+1 {
				// Delete (moved right)
				x--
				ops = append(ops, Diff{DiffDelete, a[x : x+1]})
			} else {
				// Insert (moved down)
				y--
				ops = append(ops, Diff{DiffInsert, b[y : y+1]})
			}
		}

		d--
	}

	// Add remaining matches at the start
	for x > 0 && y > 0 {
		x--
		y--
		ops = append(ops, Diff{DiffEqual, a[x : x+1]})
	}

	// Handle any remaining deletes or inserts at the start
	for x > 0 {
		x--
		ops = append(ops, Diff{DiffDelete, a[x : x+1]})
	}
	for y > 0 {
		y--
		ops = append(ops, Diff{DiffInsert, b[y : y+1]})
	}

	// Reverse the ops
	for i, j := 0, len(ops)-1; i < j; i, j = i+1, j-1 {
		ops[i], ops[j] = ops[j], ops[i]
	}

	// Merge consecutive operations of the same type
	return mergeOps(ops)
}

// mergeOps merges consecutive diff operations of the same type.
func mergeOps(ops []Diff) []Diff {
	if len(ops) == 0 {
		return nil
	}

	var result []Diff
	current := Diff{ops[0].Op, append([]byte(nil), ops[0].Data...)}

	for i := 1; i < len(ops); i++ {
		if ops[i].Op == current.Op {
			current.Data = append(current.Data, ops[i].Data...)
		} else {
			result = append(result, current)
			current = Diff{ops[i].Op, append([]byte(nil), ops[i].Data...)}
		}
	}
	result = append(result, current)

	return result
}

// GroupHunks groups consecutive Delete/Insert operations into Hunks.
// Equal operations separate hunks.
func GroupHunks(diffs []Diff) []Hunk {
	var hunks []Hunk
	var current Hunk
	inHunk := false

	for _, d := range diffs {
		switch d.Op {
		case DiffEqual:
			if inHunk {
				hunks = append(hunks, current)
				current = Hunk{}
				inHunk = false
			}
		case DiffDelete:
			current.Deleted = append(current.Deleted, d.Data...)
			inHunk = true
		case DiffInsert:
			current.Inserted = append(current.Inserted, d.Data...)
			inHunk = true
		}
	}
	if inHunk {
		hunks = append(hunks, current)
	}

	return hunks
}
