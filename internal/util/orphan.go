//go:build !windows

package util

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

// minOrphanAge is the minimum age (in seconds) a process must be before
// we consider it orphaned. This prevents race conditions with newly spawned
// processes and avoids killing legitimate short-lived subagents.
const minOrphanAge = 60

// parseEtime parses ps etime format into seconds.
// Format: [[DD-]HH:]MM:SS
// Examples: "01:23" (83s), "01:02:03" (3723s), "2-01:02:03" (176523s)
func parseEtime(etime string) (int, error) {
	var days, hours, minutes, seconds int

	// Check for days component (DD-HH:MM:SS)
	if idx := strings.Index(etime, "-"); idx != -1 {
		d, err := strconv.Atoi(etime[:idx])
		if err != nil {
			return 0, fmt.Errorf("parsing days: %w", err)
		}
		days = d
		etime = etime[idx+1:]
	}

	// Split remaining by colons
	parts := strings.Split(etime, ":")
	switch len(parts) {
	case 2: // MM:SS
		m, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, fmt.Errorf("parsing minutes: %w", err)
		}
		s, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, fmt.Errorf("parsing seconds: %w", err)
		}
		minutes, seconds = m, s
	case 3: // HH:MM:SS
		h, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, fmt.Errorf("parsing hours: %w", err)
		}
		m, err := strconv.Atoi(parts[1])
		if err != nil {
			return 0, fmt.Errorf("parsing minutes: %w", err)
		}
		s, err := strconv.Atoi(parts[2])
		if err != nil {
			return 0, fmt.Errorf("parsing seconds: %w", err)
		}
		hours, minutes, seconds = h, m, s
	default:
		return 0, fmt.Errorf("unexpected etime format: %s", etime)
	}

	return days*86400 + hours*3600 + minutes*60 + seconds, nil
}

// OrphanedProcess represents a claude process running without a controlling terminal.
type OrphanedProcess struct {
	PID int
	Cmd string
}

// FindOrphanedClaudeProcesses finds claude/codex processes without a controlling terminal.
// These are typically subagent processes spawned by Claude Code's Task tool that didn't
// clean up properly after completion.
//
// Detection is based on TTY column: processes with TTY "?" have no controlling terminal.
// This is safer than process tree walking because:
// - Legitimate terminal sessions always have a TTY (pts/*)
// - Orphaned subagents have no TTY (?)
// - Won't accidentally kill user's personal claude instances in terminals
//
// Additionally, processes must be older than minOrphanAge seconds to be considered
// orphaned. This prevents race conditions with newly spawned processes.
func FindOrphanedClaudeProcesses() ([]OrphanedProcess, error) {
	// Use ps to get PID, TTY, command, and elapsed time for all processes
	// TTY "?" indicates no controlling terminal
	// etime is elapsed time in [[DD-]HH:]MM:SS format (portable across Linux/macOS)
	out, err := exec.Command("ps", "-eo", "pid,tty,comm,etime").Output()
	if err != nil {
		return nil, fmt.Errorf("listing processes: %w", err)
	}

	var orphans []OrphanedProcess
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 4 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue // Header line or invalid PID
		}

		tty := fields[1]
		cmd := fields[2]
		etimeStr := fields[3]

		// Only look for claude/codex processes without a TTY
		if tty != "?" {
			continue
		}

		// Match claude or codex command names
		cmdLower := strings.ToLower(cmd)
		if cmdLower != "claude" && cmdLower != "claude-code" && cmdLower != "codex" {
			continue
		}

		// Skip processes younger than minOrphanAge seconds
		// This prevents killing newly spawned subagents and reduces false positives
		age, err := parseEtime(etimeStr)
		if err != nil {
			continue
		}
		if age < minOrphanAge {
			continue
		}

		orphans = append(orphans, OrphanedProcess{
			PID: pid,
			Cmd: cmd,
		})
	}

	return orphans, nil
}

// CleanupOrphanedClaudeProcesses finds and kills orphaned claude/codex processes.
// Returns the list of killed processes and any error encountered.
func CleanupOrphanedClaudeProcesses() ([]OrphanedProcess, error) {
	orphans, err := FindOrphanedClaudeProcesses()
	if err != nil {
		return nil, err
	}

	if len(orphans) == 0 {
		return nil, nil
	}

	var killed []OrphanedProcess
	var lastErr error

	for _, orphan := range orphans {
		// Send SIGTERM for graceful shutdown
		if err := syscall.Kill(orphan.PID, syscall.SIGTERM); err != nil {
			// Process may have already exited, which is fine
			if err != syscall.ESRCH {
				lastErr = fmt.Errorf("killing PID %d: %w", orphan.PID, err)
			}
			continue
		}
		killed = append(killed, orphan)
	}

	return killed, lastErr
}
