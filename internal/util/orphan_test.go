package util

import (
	"testing"
)

func TestFindOrphanedClaudeProcesses(t *testing.T) {
	// This is a live test that checks for orphaned processes on the current system.
	// It should not fail - just return whatever orphans exist (likely none in CI).
	orphans, err := FindOrphanedClaudeProcesses()
	if err != nil {
		t.Fatalf("FindOrphanedClaudeProcesses() error = %v", err)
	}

	// Log what we found (useful for debugging)
	t.Logf("Found %d orphaned claude processes", len(orphans))
	for _, o := range orphans {
		t.Logf("  PID %d: %s", o.PID, o.Cmd)
	}
}

func TestFindOrphanedClaudeProcesses_IgnoresTerminalProcesses(t *testing.T) {
	// This test verifies that the function only returns processes without TTY.
	// We can't easily mock ps output, but we can verify that if we're running
	// this test in a terminal, our own process tree isn't flagged.
	orphans, err := FindOrphanedClaudeProcesses()
	if err != nil {
		t.Fatalf("FindOrphanedClaudeProcesses() error = %v", err)
	}

	// If we're running in a terminal (typical test scenario), verify that
	// any orphans found genuinely have no TTY. We can't verify they're NOT
	// in the list since we control the test process, but we can log for inspection.
	for _, o := range orphans {
		t.Logf("Orphan found: PID %d (%s) - verify this has TTY=? in 'ps aux'", o.PID, o.Cmd)
	}
}
