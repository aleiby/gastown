package util

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

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
func FindOrphanedClaudeProcesses() ([]OrphanedProcess, error) {
	// Use ps to get PID, TTY, and command for all processes
	// TTY "?" indicates no controlling terminal
	out, err := exec.Command("ps", "-eo", "pid,tty,comm").Output()
	if err != nil {
		return nil, fmt.Errorf("listing processes: %w", err)
	}

	var orphans []OrphanedProcess
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue // Header line or invalid PID
		}

		tty := fields[1]
		cmd := fields[2]

		// Only look for claude/codex processes without a TTY
		if tty != "?" {
			continue
		}

		// Match claude or codex command names
		cmdLower := strings.ToLower(cmd)
		if cmdLower != "claude" && cmdLower != "claude-code" && cmdLower != "codex" {
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
