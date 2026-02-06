# Reliable Nudge Delivery with Input Preservation

**Date**: 2026-02-05
**Updated**: 2026-02-05
**Status**: Implementation In Progress
**Issue**: gt-lmm1z (NudgeManager), gt-9og (nudge delivery failures)

## Problem Statement

Nudges need to be 100% reliable with low latency (max handful of seconds), but they use the same input method as the overseer (human) - the command line text input field. If the overseer is typing when a nudge arrives, the nudge text gets appended to their partial input, resulting in garbled instructions.

### Core Tension

1. **Reliability**: Nudges must always be delivered
2. **Non-interruption**: Should not corrupt overseer's in-progress typing
3. **Cross-agent compatibility**: Must work with Claude Code, OpenCode, Codex, Gemini, Amp, etc.
4. **ZFC compliance**: No agent-specific regex parsing of content

## Solution: Clear/Inject/Verify Protocol

### Key Insight

Instead of trying to detect if the user is typing (unreliable), we:
1. **Always clear** the input field before injecting (guarantees no garbling)
2. **Capture before/after** to detect changes
3. **Use diff algorithm** to identify what changed and verify clean delivery
4. **Restore** the user's input after the nudge is delivered (future enhancement)

### Critical Safety Constraint

```
============================================================================
WARNING: NEVER SEND TWO CTRL-C's IN QUICK SUCCESSION!
============================================================================
Two Ctrl-C's in rapid succession exits Claude Code. This was learned the
hard way - retry logic that sent Ctrl-C to "clear and retry" would kill
agent sessions.

If anything goes wrong after the first Ctrl-C, we return an error and let
the daemon retry on the next 2-second pass. DO NOT add retry logic that
sends another Ctrl-C within the same function call!
============================================================================
```

### Protocol Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│  PRE-CHECKS                                                             │
│  - Pane in blocking mode? (copy mode, etc.) → defer                     │
│  - Large paste placeholder detected? → defer                            │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CAPTURE BEFORE (full scrollback as byte stream)                        │
│  tmux capture-pane -p -S -                                              │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CLEAR: Ctrl-C (this is the ONLY Ctrl-C we send!)                       │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  INJECT: send-keys -l "nudge message" (NO Enter yet)                    │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CAPTURE AFTER (full scrollback as byte stream)                         │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  DIFF & VERIFY                                                          │
│  - Run Myers diff on BEFORE vs AFTER                                    │
│  - Group diff operations into hunks                                     │
│  - Find hunk containing the nudge (iterate backward)                    │
│  - Extract: original input, gap typing, after-nudge typing              │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
              ┌─────────────────────┐
              │  Clean delivery?    │
              │  (no gap/after text)│
              └────────┬────────────┘
                  YES  │  NO
                       │   │
          ┌────────────┘   └──────────────┐
          ▼                               ▼
   ┌─────────────┐                 ┌──────────────┐
   │ Send Escape │                 │ Return error │
   │ Send Enter  │                 │ (daemon will │
   │ Restore     │                 │  retry next  │
   │ (future)    │                 │  pass)       │
   └─────────────┘                 └──────────────┘
```

## Myers Diff Algorithm

### Why Myers?

We need to compare BEFORE and AFTER captures to:
1. Detect if user was typing during injection (gap typing)
2. Identify the original user input to restore
3. Handle multiple changes (e.g., "Interrupted" messages, UI updates, ads)

Line-based comparison fails because:
- Terminal output can change between captures (Interrupted messages, agent output)
- Prompt characters are multi-byte (❯ is 3 bytes)
- We need to work with raw byte streams, not logical lines

### Algorithm Overview

Myers diff finds the shortest edit script to transform one sequence into another.
Complexity: O(ND) where N is sequence length and D is edit distance.

For similar sequences (our case - BEFORE and AFTER share most content), this is very fast.

### Implementation Structure

```go
// Diff operation types
type DiffOp int
const (
    DiffEqual DiffOp = iota  // Content unchanged
    DiffDelete               // Content in BEFORE but not AFTER
    DiffInsert               // Content in AFTER but not BEFORE
)

type Diff struct {
    Op   DiffOp
    Data []byte
}

// Core functions
func MyersDiff(a, b []byte) []Diff           // Returns list of diff operations
func FindNudgeInDiff(...) (original, gap, afterNudge []byte, found bool)
func IsCleanDelivery(gap, afterNudge []byte) bool
func TextToRestore(original, gap, afterNudge []byte) []byte
```

### Algorithm: Position-Based Nudge Finding

The key insight is that character-level Myers diff can fragment logical units (like our nudge text) across many small diff operations due to spurious character matches. For example, comparing:
- `"I'm starting to type some instructio"`
- `"ns for you t[from crew/holden] Reliability test 7"`

Myers finds matches like 's', 'r', 'o', 't', etc. scattered throughout, producing 40+ fragmented diff operations instead of one clean delete+insert.

**Solution**: Instead of looking for the nudge within diff hunks, we:
1. Find the nudge position directly in AFTER (simple `bytes.Index`)
2. Use the diff operations to map positions between BEFORE and AFTER
3. Identify the "changed region" containing the nudge, absorbing small Equals
4. Extract what was deleted (original input) and what was inserted around the nudge

**Critical: Small Equal Absorption**

Character-level Myers produces many tiny Equal operations (1-3 chars) when comparing unrelated text that happens to share some characters. These would fragment the input area into dozens of tiny "hunks".

Solution: Only treat Equal operations >= 4 bytes as hunk boundaries. Smaller Equals are absorbed into the current hunk. This threshold works because:
- Spurious character matches are typically 1-2 bytes
- Real content boundaries (separator lines, status lines) are much larger
- The prompt `❯ ` is 4 bytes but appears within larger Equal blocks in practice

```go
const minEqualToBreakHunk = 4
```

```
FindNudgeInDiff Algorithm:

1. Find nudge in AFTER at position P
   nudgePos = bytes.Index(after, nudge)

2. Walk through diff operations, tracking positions in both sequences:
   beforePos = 0, afterPos = 0

   For each diff:
   - DiffEqual (>= 4 bytes): beforePos += len, afterPos += len (hunk boundary)
   - DiffEqual (< 10 bytes): absorbed into current hunk
   - DiffDelete: beforePos += len (only advances in BEFORE)
   - DiffInsert: afterPos += len  (only advances in AFTER)

3. When we encounter Delete/Insert operations:
   Mark the start of a "hunk" (changed region)
   Track hunkBeforeStart and hunkAfterStart
   Continue collecting operations until significant Equal (>= 4 bytes)

4. When we hit significant DiffEqual (>= 4 bytes):
   Mark the end: hunkBeforeEnd, hunkAfterEnd
   Check if nudgePos is within [hunkAfterStart, hunkAfterEnd)

5. If nudge is in this hunk, extract:
   original    = BEFORE[hunkBeforeStart:hunkBeforeEnd]   // What was replaced
   changedArea = AFTER[hunkAfterStart:hunkAfterEnd]      // What replaced it

   Within changedArea, find the nudge:
   beforeNudge = changedArea[0 : nudgePos-hunkAfterStart]        // Gap typing
   afterNudge  = changedArea[nudgePos-hunkAfterStart+nudgeLen:]  // After-nudge typing
```

### Optimizations

1. **Common prefix skip**: O(n) scan from start, early exit at first difference
2. **Common suffix skip**: O(n) scan from end, early exit at first difference
3. **Diff only the middle**: If BEFORE and AFTER share 10KB prefix, only diff the remaining portion
4. **Position-based finding**: O(n) walk through diffs, not dependent on hunk structure

### Example

**BEFORE capture:**
```
● Received "no restore" test. Standing by.

❯ [from gastown/crew/holden] Reliability test 1
  ⎿  Interrupted · What should Claude do instead?

❯ [from gastown/crew/holden] Reliability test 3

● Received reliability tests 1 and 3.

─────────────────────────────────────────────────────────
❯ I'm starting to type some instructio
─────────────────────────────────────────────────────────
  ⏵⏵ bypass permissions on (shift+tab to cycle)
```

**AFTER capture:**
```
● Received "no restore" test. Standing by.

❯ [from gastown/crew/holden] Reliability test 1
  ⎿  Interrupted · What should Claude do instead?

❯ [from gastown/crew/holden] Reliability test 3

● Received reliability tests 1 and 3.

─────────────────────────────────────────────────────────
❯ ns for you t[from gastown/crew/holden] Reliability test 7
─────────────────────────────────────────────────────────
  ⏵⏵ bypass permissions on (shift+tab to cycle)   AD: DRINK OVALTINE!!
```

**Diff hunks (iterating backward):**

1. **Hunk 2** (suffix - the ad):
   - Deleted: ``
   - Inserted: `   AD: DRINK OVALTINE!!`
   - No nudge here, continue...

2. **Hunk 1** (input area - contains nudge):
   - Deleted: `I'm starting to type some instructio`
   - Inserted: `ns for you t[from gastown/crew/holden] Reliability test 7`
   - **Found nudge!**

**Extraction:**
- Original input: `I'm starting to type some instructio`
- Gap typing (inserted before nudge): `ns for you t`
- After-nudge typing: `` (empty)
- **Text to restore**: `I'm starting to type some instructions for you t`

### Clean Delivery Check

For the current implementation, we verify clean delivery by checking:
- Gap typing is empty (no text typed between Ctrl-C and nudge)
- After-nudge typing is empty (no text typed after nudge injection)

If either is non-empty, we return `ErrUserTyping` and let the daemon retry on next pass.

## Edge Cases

| Condition | Detection | Response |
|-----------|-----------|----------|
| Copy mode | `#{pane_in_mode} == 1` | Defer to next cycle |
| Large paste placeholder | `[Pasted text #N +X lines]` in last 50 lines | Defer |
| User typing during inject | Gap/after-nudge text in diff | Return error, daemon retries |
| "Interrupted" message | Appears as separate diff hunk | Ignored (not the nudge hunk) |
| Agent output between captures | Appears as diff hunks before nudge | Ignored |
| Nudge doesn't appear | Not found in any hunk | Return ErrNudgeNotFound |
| UI changes (ads, status) | Appear as hunks after nudge | Ignored |

## Why Ctrl-C?

- More universal than Ctrl-U (which operates on readline "chunks")
- Sends SIGINT which most terminal apps interpret as "cancel current input"
- Less likely to be remapped than Ctrl-U
- Works regardless of vim mode or other configurations

**Note**: Ctrl-C may trigger "Interrupted" UI in some agents (Claude Code). This is handled correctly - the Interrupted message appears as a separate diff hunk, not in the input area.

### Preventing Double Ctrl-C Exit

Claude Code exits when it receives two Ctrl-C's in quick succession. This can happen when:
- Daemon retry queue delivers multiple pending nudges rapidly
- Manual Ctrl-C followed by a nudge delivery

**Solution**: Send a space character atomically before Ctrl-C:

```go
// Send space + Ctrl-C atomically in one tmux call
t.run("send-keys", "-t", session, " ", "C-c")
```

The space breaks any "double Ctrl-C = exit" detection window from previous Ctrl-C's. It's harmless because Ctrl-C clears the input line anyway. Sending both in one tmux call ensures they arrive together.

## Timing

| Constant | Value | Purpose |
|----------|-------|---------|
| `nudgeClearDelayMs` | 50ms | Wait after Ctrl-C for input to clear |
| `nudgeInjectDelayMs` | 50ms | Wait after injecting text before capture |

## Files

| File | Purpose |
|------|---------|
| `internal/tmux/nudge.go` | Core delivery protocol |
| `internal/tmux/diff.go` | Myers diff implementation (new) |
| `internal/tmux/nudge_test.go` | Unit tests |
| `internal/tmux/diff_test.go` | Diff algorithm tests (new) |

## Future Enhancements

1. **Input restoration**: After clean delivery, restore `original + gap + afterNudge` via send-keys
2. **Multi-line input**: Test and handle multi-line user input
3. **Performance tuning**: Profile diff algorithm on large scrollback captures

## References

- [Myers' O(ND) Difference Algorithm](http://www.xmailserver.org/diff2.pdf) - Original paper
- [Neil Fraser: Diff Strategies](https://neil.fraser.name/writing/diff/) - Optimization techniques
- [sergi/go-diff](https://github.com/sergi/go-diff) - Reference Go implementation

## Related Issues

- gt-lmm1z: NudgeManager implementation (P1)
- gt-9og: Original nudge delivery bug report (P1)
- gt-7c1xj: Nudge priority levels (deferred, can layer on top)
