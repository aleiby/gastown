# Reliable Nudge Delivery with Input Preservation

**Date**: 2026-02-05
**Updated**: 2026-02-06
**Status**: Design Revised
**Issue**: gt-lmm1z (NudgeManager), gt-9og (nudge delivery failures)

## Problem Statement

Nudges need to be 100% reliable with low latency (max handful of seconds), but they use the same input method as the overseer (human) - the command line text input field. If the overseer is typing when a nudge arrives, the nudge text gets appended to their partial input, resulting in garbled instructions.

### Core Tension

1. **Reliability**: Nudges must always be delivered
2. **Non-interruption**: Should not corrupt overseer's in-progress typing
3. **Cross-agent compatibility**: Must work with Claude Code, OpenCode, Codex, Gemini, Amp, etc.
4. **ZFC compliance**: No agent-specific regex parsing of content

## Solution: Sentinel/Clear/Inject Protocol

### Key Insight

Instead of Ctrl-C (which sends SIGINT and risks interrupting the agent or triggering exit), use **Home+Ctrl-K** to clear input line-by-line. This sends no signals, carries no exit risk, and works reliably in terminal TUI applications.

A **sentinel string** re-inserted on each line during clearing provides exact content extraction and reliable boundary detection without prompt parsing, diff algorithms, or format-specific assumptions. The content after the sentinel is the raw user input; suffix comparison against adjacent lines in the previous capture determines when the input boundary has been crossed.

### Why Not Ctrl-C?

The previous design used Ctrl-C to clear input. This has fundamental problems:

1. **Agent interruption**: Ctrl-C sends SIGINT. If the agent is thinking/processing, this triggers "Interrupted" and stops the agent's current work — catastrophic in Gas Town.
2. **Double Ctrl-C exit**: Two Ctrl-C's in quick succession exits Claude Code. Even with the space-prefix mitigation (`" " "C-c"`), timing windows can cause exit.
3. **Space-prefix failure**: The `space + Ctrl-C` safety pattern doesn't clear large collapsed paste blocks (`[Pasted text #N +X lines]`).
4. **Input restoration impossible**: After Ctrl-C clears input, restoring it requires a second Ctrl-C to clear the garbled state — which risks the double-Ctrl-C exit.

### Why Home+Ctrl-K?

**Tested extensively** (see Appendix A):
- **Home**: Goes to beginning of current visual line (no signal, no side effects)
- **Ctrl-K**: Kills from cursor to end of visual line (no signal, no side effects)
- Each Home+Ctrl-K pair clears one visual line, working bottom-up
- Extra Home+Ctrl-K on an empty input field is a no-op (safe to overshoot)
- No risk of agent interruption, session exit, or signal interference

**Limitations discovered through testing:**
- Home goes to beginning of **current visual line only**, not beginning of entire multi-line input
- Ctrl-K kills to end of **current visual line only** — no "kill to end of buffer" equivalent
- Atomic batching (multiple keys in one tmux send-keys call) does not work — the TUI needs separate calls with ~50ms delay between them
- Up/Down arrows navigate within multi-line input but also enter command history at boundaries
- Ctrl-Home, Ctrl-End, Shift-selection combos are not supported in ink-based TUIs
- Cursor may be on any line of multi-line input, not necessarily the bottom line

### Protocol Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│  PRE-CHECKS                                                             │
│  - Pane in blocking mode? (copy mode, etc.) → defer                     │
│  - Large paste placeholder detected? → defer                            │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  IDENTIFY current line                                                  │
│  Home, insert §XXXX§, wait 50ms, capture                               │
│  Find sentinel → extract content after sentinel → collect[0]            │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CLEAR UPWARD (preceding lines)                                         │
│  loop:                                                                  │
│    Home, Ctrl-K, Backspace, Home, insert §XXXX§, wait 50ms, capture    │
│    content = text after sentinel                                        │
│    verify: prev_capture[sentinel_line - 1] ends with content            │
│    if no match → boundary reached, switch direction                     │
│    if match → prepend to collected lines, continue                      │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CLEAR DOWNWARD (following lines)                                       │
│  loop:                                                                  │
│    Home, Ctrl-K, Ctrl-K, insert §XXXX§, wait 50ms, capture             │
│    content = text after sentinel                                        │
│    verify: prev_capture[sentinel_line + 1] ends with content            │
│    if no match → boundary reached, done clearing                        │
│    if match → append to collected lines, continue                       │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  INJECT nudge                                                           │
│  Home, Ctrl-K (clear final sentinel)                                    │
│  send-keys -l "nudge message"                                           │
│  wait 50ms                                                              │
│  Send Escape (vim mode compat) + Enter                                  │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  RESTORE original user input (future)                                   │
│  Original input was collected during CLEAR phase                        │
│  Inject via send-keys -l after agent finishes processing nudge          │
│  (Requires coordination to detect agent completion)                     │
└─────────────────────────────────────────────────────────────────────────┘
```

## Sentinel Design

### Format

```
§XXXX§
```

Where `XXXX` is a 4-character base32-encoded hash of the current nanosecond timestamp. Total length: 6 characters. The `§` bookends (U+00A7, 2 bytes each) are chosen because they virtually never appear in normal input or terminal output.

```go
func makeSentinel() string {
    h := sha256.Sum256([]byte(fmt.Sprintf("%d", time.Now().UnixNano())))
    encoded := base32.StdEncoding.EncodeToString(h[:3])
    encoded = strings.TrimRight(encoded, "=")
    if len(encoded) > 4 {
        encoded = encoded[:4]
    }
    return fmt.Sprintf("§%s§", encoded)
}
```

### Why Insert After Home?

The sentinel is inserted **after** sending Home, which places it at the beginning of the current visual line. This prevents a line-wrap edge case:

If inserted at the cursor position (end of input), a long line near the terminal width could wrap when the sentinel is appended. `tmux capture-pane` outputs visual lines with newlines at wrap boundaries, which would split the sentinel across two lines and break detection.

Inserting after Home guarantees the sentinel is at the **start** of a visual line, where wrapping cannot occur (nothing precedes it on that line).

### Finding the Sentinel

Search backward from the end of the full capture. The most recent occurrence is always the one we inserted (avoids false matches from sentinel text that might exist in scrollback history).

```go
func findSentinelFromEnd(capture, sentinel string) (lineIdx, linesFromBottom int, found bool) {
    lines := strings.Split(capture, "\n")
    for i := len(lines) - 1; i >= 0; i-- {
        if strings.Contains(lines[i], sentinel) {
            return i, len(lines) - 1 - i, true
        }
    }
    return 0, 0, false
}
```

### Sentinel Lifecycle

1. Inserted via Home + sentinel on each line to identify content
2. Found in capture to extract content (everything after sentinel on that line)
3. Cleared by Home+Ctrl-K before navigating to the next line
4. Re-inserted on the next line after navigation (Backspace up or Ctrl-K down)
5. Final sentinel cleared before nudge injection

## Boundary Detection via Suffix Comparison

### How It Works

After each sentinel insertion, the content after the sentinel is compared with the adjacent line from the previous capture. If the adjacent line **ends with** the sentinel content, the new line is still within the input field. If not (or if content is empty), we've crossed the boundary.

This eliminates the need for convergence detection — the algorithm knows exactly when to stop in each direction.

### Why Suffix Comparison Works

- TUI formatting adds **prefixes** (prompt `>`, indentation spaces) to input lines
- The sentinel is inserted after Home, so content after sentinel is the raw input text
- Raw input text always appears as a **suffix** of the formatted line
- Non-input lines (separators, agent output, status) have entirely different content that won't suffix-match

### Clearing Mechanics

Each line requires one sentinel-per-line iteration: insert sentinel, capture, extract content, clear + navigate. The protocol handles lines in both directions from the cursor:

| Input Lines | Iterations (up + down) | Approximate Duration |
|------------|----------------------|----------------------|
| 0 (empty)  | 1 (identify only)    | ~100ms               |
| 1          | 1 + 2 boundary checks | ~300ms              |
| 3          | 3 + 2 boundary checks | ~500ms              |
| 5          | 5 + 2 boundary checks | ~700ms              |

Each iteration involves ~6 tmux send-keys calls + 1 capture + 50ms delay.

### Safety: Abort on External Input

If the capture changes in unexpected ways between iterations (content appears that wasn't in the previous capture's adjacent lines), someone is typing. Abort and let the daemon retry later.

## Edge Cases

| Condition | Detection | Response |
|-----------|-----------|----------|
| Copy mode | `#{pane_in_mode} == 1` | Defer to next cycle |
| Large paste placeholder | `[Pasted text #N +X lines]` in last 50 lines | Defer |
| User typing during clear | Unexpected content in captures | Abort, daemon retries |
| Cursor mid-input | Sentinel on interior line, input above and below | Bidirectional clearing (up then down) |
| Empty input (common case) | Identify step finds empty content | Fast path: clear sentinel + inject |
| Sentinel not found | Not in capture | Return ErrSentinelNotFound |
| Input field at terminal width | Sentinel inserted after Home (line start) | No wrap possible |
| Vim mode enabled | Ctrl-K is a digraph key in insert mode | Needs detection/alternate path (future) |
| Empty line in multi-line input | Content after sentinel is empty | May trigger premature boundary detection (see Open Questions) |

## Input Collection via Sentinel-Per-Line

### Key Insight

Instead of observing what disappears between captures (which requires diffing), **re-insert the sentinel on each line** before clearing it. The content after the sentinel is the exact input text for that line, with TUI-added prefixes (prompt characters, indentation) excluded automatically.

Boundary detection uses **suffix comparison**: the content after the sentinel on the new line should appear as a suffix of the adjacent line from the previous capture. If not, we've left the input field — no format-specific parsing needed.

### Three Operations

| Operation | Keys | Effect |
|-----------|------|--------|
| **Identify** | `C-a, {sentinel}` | Read current line content |
| **Delete + go up** | `C-a, C-k, BSpace, C-a, {sentinel}` | Kill content, backspace joins with line above, home, re-sentinel |
| **Delete + go down** | `C-a, C-k, C-k, {sentinel}` | Kill content, kill newline (joins with line below), sentinel on now-current line |

### Walkthrough: Cursor on Line 2 of 3-line Input

**Step 1: Identify current line** — `C-a, {sentinel}`

```
Capture A:
  57: ---------
  58:  > Line 1
  59:    §XXXX§Line 2
  60:    Line 3
  61: ---------
```

A[59]: content after sentinel = `"Line 2"`. Collect it.

```
collected = ["Line 2"]
```

**Step 2: Delete current + go up** — `C-a, C-k, BSpace, C-a, {sentinel}`

```
Capture B:
  10:  * Thinking...
  11:
  12: ---------
  13:  > §XXXX§Line 1
  14:    Line 3
  15: ---------
  16:
  17:  Status: Updated.
```

B[13]: content after sentinel = `"Line 1"`.
Verify: does A[sentinel_line - 1] = A[58] = `" > Line 1"` end with `"Line 1"`? **Yes** → prepend.

```
collected = ["Line 1", "Line 2"]
```

**Step 3: Delete current + go up** — `C-a, C-k, BSpace, C-a, {sentinel}`

```
Capture C:
  10: ---------
  11:  > §XXXX§
  12:    Line 3
  13: ---------
```

C[11]: content after sentinel = `""` (empty).
Verify: does B[sentinel_line - 1] = B[12] = `"---------"` end with `""`?
Empty is a trivial suffix, but `"---------"` is clearly a separator — **boundary reached**. Switch direction.

**Step 4: Delete current + go down** — `C-a, C-k, C-k, {sentinel}`

```
Capture D:
  10: ---------
  11:  > §XXXX§Line 3
  12: ---------
```

D[11]: content after sentinel = `"Line 3"`.
Verify: does C[sentinel_line + 1] = C[12] = `"   Line 3"` end with `"Line 3"`? **Yes** → append.

```
collected = ["Line 1", "Line 2", "Line 3"]
```

**Step 5: Delete current + go down** — `C-a, C-k, C-k, {sentinel}`

```
Capture E:
  10: ---------
  11:  > §XXXX§
  12: ---------
```

E[11]: content after sentinel = `""` (empty).
Verify: does D[sentinel_line + 1] = D[12] = `"---------"` end with `""`?
Separator — **boundary reached**. Done clearing.

**Step 6: Inject nudge** — `C-a, C-k, {nudge}`

Clear the final sentinel and inject the nudge message.

### Suffix Comparison for Boundary Detection

The content after the sentinel on each line is the **raw user input** without TUI formatting. The adjacent line from the previous capture has TUI formatting (prompt `>`, indentation, etc.). The suffix comparison bridges this:

```
Adjacent line:     " > Line 1"     (TUI-formatted)
After sentinel:    "Line 1"        (raw input)
Suffix match:      ✓ " > Line 1" ends with "Line 1"
```

When we cross the input boundary into a separator or agent output:

```
Adjacent line:     "---------"     (non-input)
After sentinel:    ""              (empty — nothing after sentinel)
Suffix match:      Trivially yes, but empty content = boundary
```

The boundary check is: content after sentinel must be **non-empty** AND appear as a suffix of the adjacent line. Empty content always signals a boundary.

### Implementation

```go
func clearAndCollect(sentinel string) ([]string, error) {
    var collected []string

    // Step 1: Identify current line
    sendKeys("Home")
    sendKeys("-l", sentinel)
    time.Sleep(50 * time.Millisecond)
    prev := capture()
    sentLine := findSentinelLine(prev, sentinel)
    content := extractAfterSentinel(prev, sentLine, sentinel)
    if content != "" {
        collected = append(collected, content)
    }

    // Step 2: Clear upward
    for i := 0; i < maxIters; i++ {
        adjLine := getLine(prev, sentLine - 1)
        sendKeys("Home"); sendKeys("C-k"); sendKeys("BSpace")
        sendKeys("Home"); sendKeys("-l", sentinel)
        time.Sleep(50 * time.Millisecond)
        cur := capture()
        sentLine = findSentinelLine(cur, sentinel)
        content = extractAfterSentinel(cur, sentLine, sentinel)

        if content == "" || !strings.HasSuffix(adjLine, content) {
            break // boundary reached
        }
        collected = append([]string{content}, collected...) // prepend
        prev = cur
    }

    // Step 3: Clear downward
    for i := 0; i < maxIters; i++ {
        adjLine := getLine(prev, sentLine + 1)
        sendKeys("Home"); sendKeys("C-k"); sendKeys("C-k")
        sendKeys("-l", sentinel)
        time.Sleep(50 * time.Millisecond)
        cur := capture()
        sentLine = findSentinelLine(cur, sentinel)
        content = extractAfterSentinel(cur, sentLine, sentinel)

        if content == "" || !strings.HasSuffix(adjLine, content) {
            break // boundary reached
        }
        collected = append(collected, content) // append
        prev = cur
    }

    // Step 4: Clean up final sentinel
    sendKeys("Home"); sendKeys("C-k")

    return collected, nil
}
```

### Comparison with Previous Approaches

| Approach | Pros | Cons |
|----------|------|------|
| **Sentinel-per-line** (current) | Exact content, reliable boundaries, handles cursor mid-input, no diffing | More keystrokes per line (~6 vs ~2), more captures |
| Convergence + deletion tracking | Fewer operations per line | Can't distinguish input from non-input boundaries |
| Myers diff (original PR #1212) | Full before/after comparison | Requires Ctrl-C, complex diff logic, fragmentation issues |

### Open Questions

- **Empty input lines**: An empty line within multi-line input would produce `""` after the sentinel, triggering boundary detection prematurely. Needs testing to determine if this is a real scenario.
- **Backspace behavior**: Does Backspace at the beginning of a line reliably join with the line above in Claude Code's ink TUI? Needs testing.
- **Line wrapping**: If a logical input line wraps across visual lines, each visual line gets its own sentinel iteration. The suffix comparison should still work, but reconstruction needs to detect and join wrapped lines.

## Timing

| Constant | Value | Purpose |
|----------|-------|---------|
| `sentinelInsertDelay` | 50ms | Wait after sentinel insertion for TUI to render |
| `clearIterDelay` | 50ms | Wait after each Home+Ctrl-K for TUI to render |
| `nudgeInjectDelay` | 50ms | Wait after injecting nudge text before Enter |

**Protocol overhead**: ~70ms fixed (sentinel insertion + full capture) + ~60ms per input line cleared.

## Alternatives Considered

### Ctrl-C Based Clearing (Previous Design)

See "Why Not Ctrl-C?" above. The Ctrl-C approach was implemented in PR #1212 with a Myers diff verification algorithm. While the diff algorithm worked well for detecting gap typing and after-nudge typing, the fundamental Ctrl-C problems (agent interruption, exit risk, restoration impossibility) made it unsuitable.

The Myers diff implementation remains available if needed for other purposes, but is not required for the Home+Ctrl-K protocol.

### Ctrl-U (Unix Line Kill)

Tested. Only clears content on the current visual line — same limitation as Ctrl-K but without the ability to pair with Home for line-by-line clearing. Requires backspace fallback for anything beyond a single short line.

### Ctrl+S (Claude Code Stash)

Claude Code has a stash feature (save/restore input). Blocked by XON/XOFF flow control conflict on many systems. Could work if XON/XOFF is disabled globally (`stty -ixon`), but is Claude Code specific.

### Hooks-Based Side Channel

Claude Code hooks (PostToolUse, UserPromptSubmit) can inject `<system-reminder>` content without touching the terminal. Works for content delivery during active agent work, but cannot wake idle agents. Not a complete solution on its own, but complementary — hooks could deliver nudge content while a minimal terminal poke (just Enter) wakes idle agents.

### MCP Server Side Channel

An MCP server in the daemon could provide `send_nudge` / `check_inbox` tools for structured inter-agent communication over HTTP. Completely eliminates terminal collision. Requires MCP infrastructure setup and agent polling. See separate design document for MCP integration plan.

## Files

| File | Purpose |
|------|---------|
| `internal/tmux/nudge.go` | Core delivery protocol |
| `internal/tmux/diff.go` | Myers diff (retained, not used by new protocol) |
| `internal/tmux/nudge_test.go` | Unit tests |
| `internal/tmux/diff_test.go` | Diff algorithm tests |

## References

- [Myers' O(ND) Difference Algorithm](http://www.xmailserver.org/diff2.pdf) - Original paper (retained for reference)
- Gas Town issue #1216 - Design analysis and test results

## Related Issues

- gt-lmm1z: NudgeManager implementation (P1)
- gt-9og: Original nudge delivery bug report (P1)
- gt-7c1xj: Nudge priority levels (deferred, can layer on top)

## Appendix A: Test Results

Testing was performed against a live Claude Code session (`gt-gastown-crew-amos`) using custom Go test programs that exercised various clearing strategies via tmux.

### Clearing Strategy Comparison (5-line input, 264 chars)

| Strategy | Ops | Duration | Success Rate |
|----------|-----|----------|-------------|
| **Home + Ctrl-K per line** | **15** | **258ms** | **3/3** |
| Ctrl-U fast (no delay) | 270 | 598ms | 3/3 |
| Ctrl-U slow (30ms delay) | 250 | 1.75s | 3/3 |
| End + Ctrl-U | 390-540 | 1.18-1.35s | 1/3 |
| Home + Shift-End + Delete | 80 | 1.35s | 0/3 |

### Atomic vs Separate Calls

All atomic strategies (multiple keys in one `tmux send-keys` call) failed 100%. Claude Code's ink-based TUI requires separate `tmux send-keys` calls with real inter-keystroke delay to process each input event.

### Navigation Key Behavior in Claude Code

| Key | Behavior | Useful for clearing? |
|-----|----------|---------------------|
| Home | Beginning of current visual line | Yes (per-line positioning) |
| Ctrl-A | Same as Home | No advantage |
| End | End of current visual line | Not needed |
| Up | Navigates within multi-line input; history at top boundary | Not for clearing |
| Down | Navigates within multi-line input; history at bottom boundary | Not for clearing |
| Ctrl-Home | Inconsistent behavior | No |
| Ctrl-End | Not supported | No |
| Shift-End/Home | Partial selection, unreliable | No |

### Home+Ctrl-K Tuning (10-line input)

| Delay | Iterations | Duration | Success |
|-------|-----------|----------|---------|
| 10ms | 10-11 | 166ms | 3/3 |
| 30ms | 10 | 353ms | 3/3 |
| 50ms | 10 | 581ms | 3/3 |

The 10ms delay works for clearing but is too fast for convergence detection captures. 50ms provides reliable TUI rendering for capture-based convergence.
