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

## Architecture: Separation of Concerns

The protocol separates two independent problems:

1. **Clearing**: Remove whatever is in the input field. Uses C-a+Ctrl-K convergence — simple, fast, proven reliable. No sentinel needed.
2. **Content capture**: Identify what was in the input field before clearing, for later restoration. Uses Myers diff on before/after full captures — preserves formatting, handles empty lines, already implemented.

This separation avoids the complications of trying to capture content during clearing (sentinel-per-line word wrap issues, empty line boundary detection ambiguity).

## Solution: Capture/Clear/Inject Protocol

### Why Not Ctrl-C?

The previous design used Ctrl-C to clear input. This has fundamental problems:

1. **Agent interruption**: Ctrl-C sends SIGINT. If the agent is thinking/processing, this triggers "Interrupted" and stops the agent's current work — catastrophic in Gas Town.
2. **Double Ctrl-C exit**: Two Ctrl-C's in quick succession exits Claude Code. Even with the space-prefix mitigation (`" " "C-c"`), timing windows can cause exit.
3. **Space-prefix failure**: The `space + Ctrl-C` safety pattern doesn't clear large collapsed paste blocks (`[Pasted text #N +X lines]`).
4. **Input restoration impossible**: After Ctrl-C clears input, restoring it requires a second Ctrl-C to clear the garbled state — which risks the double-Ctrl-C exit.

### Why C-a+Ctrl-K?

**Tested extensively** (see Appendix A):
- **C-a**: Goes to beginning of current visual line (no signal, no side effects)
- **Ctrl-K**: Kills from cursor to end of visual line (no signal, no side effects)
- Each C-a+Ctrl-K pair clears one visual line
- Extra C-a+Ctrl-K on an empty input field is a no-op (safe to overshoot)
- No risk of agent interruption, session exit, or signal interference

**Why C-a over Home**: C-a is the readline/emacs convention for "beginning of line" and works reliably across all CLI agents. Home depends on terminal escape sequences (`\e[H`, `\e[1~`) which vary by terminal emulator and can be intercepted or misinterpreted. Since we send keys via `tmux send-keys`, C-a is delivered as the raw control character — no escape sequence ambiguity. See **Cross-Agent Compatibility** section below for details.

**Limitations discovered through testing:**
- C-a goes to beginning of **current visual line only**, not beginning of entire multi-line input
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
│  SENTINEL + CAPTURE BEFORE                                              │
│  1. C-a (go to beginning of current line)                               │
│  2. Insert sentinel: §XXXX§ (4-char hash + bookends, 6 chars total)    │
│  3. Wait 50ms for TUI to render                                        │
│  4. Full capture (tmux capture-pane -p -S -)                            │
│  5. Find sentinel → compute N (lines from sentinel to bottom)           │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CLEAR (convergence loop)                                               │
│  prev = captureLast(N+2)                                                │
│  loop (max 30 iterations):                                              │
│    C-a + Ctrl-K                                                         │
│    wait 50ms                                                            │
│    cur = captureLast(N+2)                                                │
│    if cur == prev → break (converged, input is clear)                   │
│    if len(cur) > len(prev) + threshold → abort (external input)         │
│    prev = cur                                                           │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CAPTURE AFTER                                                          │
│  Full capture (tmux capture-pane -p -S -)                               │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  INJECT nudge message                                                   │
│  send-keys -l "nudge message"                                           │
│  wait 50ms                                                              │
│  Send Escape (vim mode compat) + Enter                                  │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  DIFF + EXTRACT original input                                          │
│  Myers diff BEFORE vs AFTER captures                                    │
│  Deleted content = original user input (with formatting)                │
│  Store for future restoration                                           │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  RESTORE original user input (future)                                   │
│  Inject via send-keys -l after agent finishes processing nudge          │
│  (Requires coordination to detect agent completion)                     │
└─────────────────────────────────────────────────────────────────────────┘
```

## Sentinel Design

### Purpose

The sentinel serves a single purpose: **determine the capture window size N** for the convergence loop. By finding which line the cursor is on, we know how many lines from the bottom to capture during convergence checks, isolating the input area from agent output above.

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

### Why Insert After C-a?

The sentinel is inserted **after** sending C-a, which places it at the beginning of the current visual line. This prevents a line-wrap edge case:

If inserted at the cursor position (end of input), a long line near the terminal width could wrap when the sentinel is appended. `tmux capture-pane` outputs visual lines with newlines at wrap boundaries, which would split the sentinel across two lines and break detection.

Inserting after C-a guarantees the sentinel is at the **start** of a visual line, where wrapping cannot occur (nothing precedes it on that line).

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

1. Inserted once before the initial full capture (C-a + sentinel)
2. Found in the capture to establish cursor line position and compute N
3. Cleared naturally by the first C-a+Ctrl-K iteration (it's at the beginning of the line being cleared)
4. No cleanup step needed — the convergence loop removes it

## Convergence Detection (Clearing)

### How It Works

After each C-a+Ctrl-K, capture the last N+2 lines of the pane (where N was determined by the sentinel position, +2 for margin). Compare with the previous capture byte-for-byte. When the capture stops changing, clearing is complete.

### Why Convergence Works

- Each C-a+Ctrl-K removes content from the input area, changing the capture
- When nothing remains to clear, C-a+Ctrl-K is a no-op and the capture is unchanged
- The capture window (last N+2 lines) is isolated from agent output changes above the input field
- No format-specific parsing needed — pure byte comparison

### Clearing Mechanics (Tested)

Each input line requires **2 C-a+Ctrl-K iterations**: one kills the text content, one kills the newline. For M lines of input, expect 2M iterations before convergence, plus 1 final iteration to confirm.

| Input Lines | Iterations to Converge | Duration (50ms delay) |
|------------|----------------------|----------------------|
| 0 (empty)  | 2                    | ~230ms               |
| 1          | 2                    | ~220ms               |
| 3          | 6                    | ~470ms               |
| 5          | 10                   | ~690ms               |
| 10         | 20                   | ~1.2s                |

### Safety: Abort on External Input

If the capture **grows** between iterations (beyond a small threshold for status bar updates), someone is typing or navigating. Abort the clear operation and let the daemon retry later.

```go
if len(cur) > len(prev) + 50 {
    return ErrExternalInput
}
```

## Content Capture via Myers Diff

### Why Diff?

After clearing, we need to know what was in the input field to restore it later. The clearing process is intentionally simple (just C-a+Ctrl-K convergence) and doesn't track content. Instead, we take full captures **before** and **after** clearing and diff them.

### Why Myers Diff Works Here

With C-a+Ctrl-K clearing (unlike Ctrl-C), the before/after captures have clean differences:
- **No "Interrupted" messages** — C-a+Ctrl-K sends no signals
- **No agent state changes** — no SIGINT means the agent keeps its current state
- **Minimal noise** — only the input area changes, plus possible status bar updates and agent output during the ~1s clearing window

The diff between BEFORE and AFTER identifies:
- **Deleted content**: The original user input (including formatting, empty lines, indentation)
- **Inserted content**: Any agent output that appeared during clearing (separate diff hunk, above the input area)
- **Unchanged content**: Everything else (agent history, separators, status)

### Finding the Original Input

The deleted content from the diff is the original input. The sentinel helps locate it: in the BEFORE capture, the sentinel marks the cursor line. Deleted content near the sentinel position is the input text.

```go
// After clearing is complete:
afterCapture := captureFull()

// Diff before vs after
diffs := MyersDiff([]byte(beforeCapture), []byte(afterCapture))

// Find deleted content near the sentinel position
originalInput := extractDeletedNearSentinel(diffs, sentinel)
```

### Formatting Preservation

Myers diff operates on the raw byte stream of the captures. This means:
- **Empty lines** in the input are captured as part of the deleted region
- **Multi-line input** is captured as a contiguous deleted block
- **TUI formatting** (prompt characters, indentation) is included in the deleted content

The TUI formatting prefix needs to be stripped from the deleted content to recover the raw user input. This is the one format-dependent operation — but it only needs to handle the prefix of each line (prompt `>`, indentation), not parse the full TUI output. The sentinel position helps identify which deleted lines are input vs. other changes.

### Open Questions

- **How reliably does Myers diff isolate the input?** Needs testing with real before/after captures from the C-a+Ctrl-K clearing process to verify clean hunk boundaries.
- **TUI prefix stripping**: How much formatting does the TUI add to input lines? Is it consistent enough to strip mechanically, or does it vary by line position?
- **Agent output during clearing**: If the agent produces output during the ~1s clearing window, it appears as inserted content in the diff. Does this interfere with identifying the deleted input?

## Edge Cases

| Condition | Detection | Response |
|-----------|-----------|----------|
| Copy mode | `#{pane_in_mode} == 1` | Defer to next cycle |
| Large paste placeholder | `[Pasted text #N +X lines]` in last 50 lines | Defer |
| User typing during clear | Capture grows between convergence iterations | Abort, daemon retries |
| Cursor mid-input | Sentinel on interior line | Convergence clears all lines regardless of initial cursor position |
| Empty input (common case) | Sentinel cleared in 1 iter, convergence in 2 | Fast path: ~230ms total |
| Empty lines in input | Part of the deleted region in diff | Captured naturally by Myers diff |
| Sentinel not found | Not in capture | Return ErrSentinelNotFound |
| Input field at terminal width | Sentinel inserted after C-a (line start) | No wrap possible |
| Agent output during clear | Appears as inserted content in diff | Separate hunk from deleted input |
| Vim mode enabled | Ctrl-K is a digraph key in insert mode | Needs detection/alternate path (future) |

## Cross-Agent Compatibility

### Why C-a Over Home

The protocol uses C-a (Ctrl-A) rather than Home for "go to beginning of line". Both work identically in Claude Code, but C-a is the better choice for cross-agent portability:

| Factor | C-a | Home |
|--------|-----|------|
| Delivery mechanism | Raw control character (0x01) | Terminal escape sequence (`\e[H` or `\e[1~`) |
| readline/emacs convention | Yes (standard) | Terminal-dependent |
| tmux send-keys | `send-keys C-a` — unambiguous | `send-keys Home` — tmux translates to escape sequence |
| tmux prefix conflict | None — `send-keys` bypasses prefix handling | N/A |

### Per-Agent Analysis

**Claude Code** (ink-based TUI): C-a and Home are equivalent — both go to beginning of current visual line. C-a is the native readline binding.

**Gemini CLI** (ink-based TUI): Uses readline keybindings. C-a works for beginning of line. Home behavior depends on terminal escape sequence support.

**OpenCode** (Bubble Tea TUI): Distinguishes C-a (beginning of current line) from Home (beginning of input buffer). This means Home has different semantics in OpenCode — it navigates to the start of the entire multi-line input, not just the current line. For our protocol (clearing line by line), we want current-line behavior, making C-a the correct choice.

**Codex CLI** (ink-based TUI): Home key has known environment-sensitive issues — GitHub issue reports of Home not working correctly in certain terminal configurations. C-a avoids these escape sequence problems entirely.

**Amp** (Bubble Tea TUI): Ctrl+K is sometimes intercepted by IDE integrations (VS Code terminal). C-a is less likely to be intercepted since it's a basic readline binding rather than a "kill" command that IDEs might capture.

### tmux send-keys and C-a

A common concern: if the user's tmux prefix is C-a (the default), doesn't `send-keys C-a` trigger the prefix? No — `tmux send-keys` explicitly sends the key to the target pane, bypassing prefix handling. The prefix key is only intercepted when the user physically presses it in that terminal. Programmatic `send-keys` always delivers to the pane.

## Timing

| Constant | Value | Purpose |
|----------|-------|---------|
| `sentinelInsertDelay` | 50ms | Wait after sentinel insertion for TUI to render |
| `clearIterDelay` | 50ms | Wait after each C-a+Ctrl-K for TUI to render |
| `nudgeInjectDelay` | 50ms | Wait after injecting nudge text before Enter |

**Protocol overhead**: ~70ms fixed (sentinel + captures) + ~60ms per input line cleared + diff computation (negligible for typical capture sizes).

## Alternatives Considered

### Ctrl-C Based Clearing (Previous Design, PR #1212)

See "Why Not Ctrl-C?" above. The Ctrl-C approach was implemented with a Myers diff verification algorithm. While the diff algorithm worked well for detecting gap typing and after-nudge typing, the fundamental Ctrl-C problems (agent interruption, exit risk, restoration impossibility) made it unsuitable.

The current design retains Myers diff for content capture but replaces Ctrl-C with C-a+Ctrl-K for clearing.

### Sentinel-Per-Line Content Collection

An alternative approach where the sentinel is re-inserted on each line during clearing, extracting content as a side effect. Each iteration:
- Insert sentinel → capture → extract content after sentinel
- Delete line → navigate to adjacent line (Backspace up, Ctrl-K down)
- Repeat, using suffix comparison for boundary detection

**Rejected because:**
- Inserting sentinel at the start of lines near terminal width causes word wrapping, creating phantom lines in the collected output
- Empty input lines produce empty content after sentinel, making boundary detection ambiguous (empty input line vs. actual boundary)
- More keystrokes per line (~6 vs ~2), slower than simple convergence
- Conflates clearing and capture, adding complexity to both

### Convergence + Deletion Tracking

Observe what disappears between consecutive convergence captures to reconstruct input. Simpler than sentinel-per-line but can't reliably distinguish input boundaries from non-input content without format-specific parsing.

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
| `internal/tmux/diff.go` | Myers diff for content capture |
| `internal/tmux/nudge_test.go` | Unit tests |
| `internal/tmux/diff_test.go` | Diff algorithm tests |

## References

- [Myers' O(ND) Difference Algorithm](http://www.xmailserver.org/diff2.pdf) - Original paper
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
| **C-a + Ctrl-K per line** | **15** | **258ms** | **3/3** |
| Ctrl-U fast (no delay) | 270 | 598ms | 3/3 |
| Ctrl-U slow (30ms delay) | 250 | 1.75s | 3/3 |
| End + Ctrl-U | 390-540 | 1.18-1.35s | 1/3 |
| Home + Shift-End + Delete | 80 | 1.35s | 0/3 |

### Atomic vs Separate Calls

All atomic strategies (multiple keys in one `tmux send-keys` call) failed 100%. Claude Code's ink-based TUI requires separate `tmux send-keys` calls with real inter-keystroke delay to process each input event.

### Navigation Key Behavior in Claude Code

| Key | Behavior | Useful for clearing? |
|-----|----------|---------------------|
| C-a | Beginning of current visual line | **Yes** (primary choice, cross-agent compatible) |
| Home | Beginning of current visual line | Equivalent in Claude Code, but less portable |
| End | End of current visual line | Not needed |
| Up | Navigates within multi-line input; history at top boundary | Not for clearing |
| Down | Navigates within multi-line input; history at bottom boundary | Not for clearing |
| Ctrl-Home | Inconsistent behavior | No |
| Ctrl-End | Not supported | No |
| Shift-End/Home | Partial selection, unreliable | No |

### C-a+Ctrl-K Tuning (10-line input)

| Delay | Iterations | Duration | Success |
|-------|-----------|----------|---------|
| 10ms | 10-11 | 166ms | 3/3 |
| 30ms | 10 | 353ms | 3/3 |
| 50ms | 10 | 581ms | 3/3 |

The 10ms delay works for clearing but is too fast for convergence detection captures. 50ms provides reliable TUI rendering for capture-based convergence.
