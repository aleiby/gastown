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

// FindNudgeHunk searches hunks backward to find the one containing the nudge.
// Returns the original text (deleted), text before nudge (gap typing),
// text after nudge, and whether the nudge was found.
func FindNudgeHunk(hunks []Hunk, nudge []byte) (original, beforeNudge, afterNudge []byte, found bool) {
	for i := len(hunks) - 1; i >= 0; i-- {
		h := hunks[i]
		idx := bytes.Index(h.Inserted, nudge)
		if idx >= 0 {
			original = h.Deleted
			beforeNudge = h.Inserted[:idx]
			afterNudge = h.Inserted[idx+len(nudge):]
			found = true
			return
		}
	}
	return
}

// minEqualToBreakHunk is the minimum size of an Equal section required to end a hunk.
// Small Equals (like single-character matches in character-level Myers diff) are
// absorbed into the hunk. This prevents fragmentation due to spurious matches
// between unrelated text that happens to share some characters.
//
// For example, "I'm starting to type" and "ns for you t[nudge]" share characters
// like 's', 'r', 'o', 't' which Myers finds as matches. Without this threshold,
// these create many tiny hunks instead of one logical change.
//
// Value of 4 is chosen because:
// - Spurious matches are typically 1-2 bytes (single characters or short sequences)
// - Real content boundaries (separator lines, status lines) are much larger
// - The prompt "❯ " is 4 bytes but appears within larger Equal blocks in practice
const minEqualToBreakHunk = 4

// FindNudgeInDiff locates the nudge in AFTER and determines what it replaced in BEFORE.
// This is more robust than FindNudgeHunk because it doesn't rely on the nudge being
// contained within a single hunk (which can fail with character-level Myers diff).
//
// The algorithm handles multiple disjoint changes (scrolling, new output, input changes)
// by finding the specific hunk that contains the nudge position. Small Equal sections
// (< minEqualToBreakHunk bytes) are absorbed into hunks to prevent fragmentation from
// spurious character-level matches.
//
// Returns:
//   - original: text from BEFORE that was replaced by the nudge region
//   - beforeNudge: text in AFTER that appears before the nudge (gap typing)
//   - afterNudge: text in AFTER that appears after the nudge
//   - found: whether the nudge was located
func FindNudgeInDiff(before, after, nudge []byte, diffs []Diff) (original, beforeNudge, afterNudge []byte, found bool) {
	// Find nudge position in AFTER
	nudgePos := bytes.Index(after, nudge)
	if nudgePos < 0 {
		return nil, nil, nil, false
	}
	nudgeEnd := nudgePos + len(nudge)

	// Walk through diffs to find the hunk containing the nudge
	// A "hunk" is a sequence of operations between significant Equals (>= minEqualToBreakHunk)
	beforePos := 0
	afterPos := 0

	for i := 0; i < len(diffs); i++ {
		d := diffs[i]

		switch d.Op {
		case DiffEqual:
			beforePos += len(d.Data)
			afterPos += len(d.Data)

		case DiffDelete, DiffInsert:
			// Found start of a hunk - collect operations until significant Equal
			hunkBeforeStart := beforePos
			hunkAfterStart := afterPos

			// Collect operations, treating small Equals as part of the hunk
			for i < len(diffs) {
				op := diffs[i].Op
				dataLen := len(diffs[i].Data)

				// Significant Equal ends the hunk
				if op == DiffEqual && dataLen >= minEqualToBreakHunk {
					break
				}

				// Include this operation in the hunk
				switch op {
				case DiffEqual:
					// Small Equal - absorbed into hunk
					beforePos += dataLen
					afterPos += dataLen
				case DiffDelete:
					beforePos += dataLen
				case DiffInsert:
					afterPos += dataLen
				}
				i++
			}
			i-- // Back up so outer loop's i++ doesn't skip

			hunkBeforeEnd := beforePos
			hunkAfterEnd := afterPos

			// Check if the nudge is within this hunk's AFTER range
			if nudgePos >= hunkAfterStart && nudgePos < hunkAfterEnd {
				// Found the hunk containing the nudge!
				original = before[hunkBeforeStart:hunkBeforeEnd]
				changedInAfter := after[hunkAfterStart:hunkAfterEnd]

				// Find nudge within the hunk
				localNudgePos := nudgePos - hunkAfterStart
				localNudgeEnd := nudgeEnd - hunkAfterStart

				if localNudgeEnd > len(changedInAfter) {
					localNudgeEnd = len(changedInAfter)
				}

				beforeNudge = changedInAfter[:localNudgePos]
				afterNudge = changedInAfter[localNudgeEnd:]

				return original, beforeNudge, afterNudge, true
			}
		}
	}

	return nil, nil, nil, false
}

// IsCleanDelivery checks if a nudge was delivered cleanly.
// A clean delivery means no gap typing (before nudge) and no after-nudge typing.
func IsCleanDelivery(beforeNudge, afterNudge []byte) bool {
	return len(bytes.TrimSpace(beforeNudge)) == 0 && len(bytes.TrimSpace(afterNudge)) == 0
}

// TextToRestore returns the combined text that should be restored after nudge delivery.
// This is: original input + gap typing + after-nudge typing
func TextToRestore(original, beforeNudge, afterNudge []byte) []byte {
	result := make([]byte, 0, len(original)+len(beforeNudge)+len(afterNudge))
	result = append(result, original...)
	result = append(result, beforeNudge...)
	result = append(result, afterNudge...)
	return result
}
