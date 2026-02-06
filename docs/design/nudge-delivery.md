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

A **sentinel string** inserted at the cursor position before the initial capture provides an anchor point that eliminates the need for prompt detection, diff algorithms, or format-specific parsing. The sentinel tells us exactly which line the cursor is on, giving us the optimal capture window size for convergence detection.

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
- Up arrow navigates command history, not input lines
- Ctrl-Home, Ctrl-End, Shift-selection combos are not supported in ink-based TUIs

### Protocol Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│  PRE-CHECKS                                                             │
│  - Pane in blocking mode? (copy mode, etc.) → defer                     │
│  - Large paste placeholder detected? → defer                            │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  SENTINEL                                                               │
│  1. Home (go to beginning of current line — avoids line-wrap issues)    │
│  2. Insert sentinel: §XXXX§ (4-char hash + bookends, 6 chars total)    │
│  3. Wait 50ms for TUI to render                                        │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CAPTURE (one-time full scrollback capture)                             │
│  tmux capture-pane -p -S -                                              │
│  Find sentinel searching backward from end of capture                   │
│  Compute N = lines from sentinel to bottom of capture                   │
│  N is the capture window for all subsequent convergence checks          │
└────────────────────────┬────────────────────────────────────────────────┘
                         ▼
┌─────────────────────────────────────────────────────────────────────────┐
│  CLEAR (convergence loop)                                               │
│  prev = captureLast(N)                                                  │
│  loop (max 30 iterations):                                              │
│    Home + Ctrl-K                                                        │
│    wait 50ms                                                            │
│    cur = captureLast(N)                                                  │
│    if cur == prev → break (converged, input is clear)                   │
│    if len(cur) > len(prev) + threshold → abort (external input)         │
│    prev = cur                                                           │
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
│  (Future) RESTORE original user input                                   │
│  The original input is known from the initial full capture              │
│  (everything on the sentinel's line after the sentinel)                 │
│  plus content on preceding input lines.                                 │
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

1. Inserted before the initial full capture
2. Found in the capture to establish cursor line position
3. Cleared naturally by the first Home+Ctrl-K iteration (it's at the beginning of the line being cleared)
4. No cleanup step needed

## Convergence Detection

### How It Works

After each Home+Ctrl-K, capture the last N lines of the pane (where N was determined by the sentinel position). Compare with the previous capture byte-for-byte. When the capture stops changing, clearing is complete.

### Why Convergence Works

- Each Home+Ctrl-K removes content from the input area, changing the capture
- When nothing remains to clear, Home+Ctrl-K is a no-op and the capture is unchanged
- The capture window (last N lines) is isolated from agent output changes above the input field
- No format-specific parsing needed — pure byte comparison

### Clearing Mechanics (Tested)

Each input line requires **2 Home+Ctrl-K iterations**: one kills the text content, one kills the newline. For M lines of input, expect 2M iterations before convergence, plus 1 final iteration to confirm.

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

## Edge Cases

| Condition | Detection | Response |
|-----------|-----------|----------|
| Copy mode | `#{pane_in_mode} == 1` | Defer to next cycle |
| Large paste placeholder | `[Pasted text #N +X lines]` in last 50 lines | Defer |
| User typing during clear | Capture grows between iterations | Abort, daemon retries |
| User pressing Up/Down | Content changes dramatically (history nav) | Abort, daemon retries |
| Empty input (common case) | Sentinel cleared in 1 iter, convergence in 2 | Fast path: ~230ms total |
| Sentinel not found | Not in capture | Return ErrSentinelNotFound |
| Input field at terminal width | Sentinel inserted after Home (line start) | No wrap possible |
| Agent output during clear | Output is above capture window (N lines from bottom) | Not visible in convergence captures |
| Vim mode enabled | Ctrl-K is a digraph key in insert mode | Needs detection/alternate path (future) |

## Input Restoration (Future)

The initial full capture contains the user's original input. After the sentinel is found, everything on the sentinel's line **after** the sentinel is the bottom line of the user's input. Lines above (between the previous separator and the sentinel line) are the preceding input lines.

Because Home only reaches the current (bottom) visual line, and there is no keystroke that navigates to the beginning of multi-line input in Claude Code's ink TUI, the original input must be extracted from the capture data.

After the nudge is delivered and the agent processes it, the original input can be restored via `send-keys -l`. This requires knowing when the agent has finished processing — a separate coordination problem left for future work.

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
| Up | Command history navigation | No (leaves input field) |
| Down | Command history navigation | No (leaves input field) |
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
