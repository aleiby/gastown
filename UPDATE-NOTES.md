# Updating Local gt Installation

This documents the process for updating our fork with upstream changes while preserving local fixes.

## Quick Reference

```bash
# 1. Fetch upstream
git fetch upstream

# 2. See what's new
git log --oneline upstream/main..HEAD   # our commits ahead
git log --oneline HEAD..upstream/main   # upstream commits we don't have

# 3. Check which local commits are already merged upstream
git log --oneline upstream/main --grep="#<PR-number>"

# 4. Create backup branch
git branch build-with-fixes-backup-$(date +%Y%m%d)

# 5. Create new branch from upstream and cherry-pick unmerged commits
git checkout -b build-with-fixes-vX.Y.Z vX.Y.Z
git cherry-pick <commit>  # for each unmerged commit

# 6. Build and test
go build ./...
go test ./...

# 7. Install
go install ./cmd/gt
```

**WARNING**: Always use `go install ./cmd/gt`, never `go build -o ~/bin/gt`.
The Mayor's session hook expects `gt` to be in `~/go/bin/`. If you install
to `~/bin/` instead, the Mayor will use a stale binary and miss role context.

## Our Local Commits (as of 2026-01-29)

Commits we maintain on top of upstream (v0.5.0):

| Commit | Description | PR # | Author | Notes |
|--------|-------------|------|--------|-------|
| a7158216 | fix(install): bootstrap file placement + crew/polecats | #411 | | Major install fix |
| eaa93c79 | fix(install): PRIME.md at redirect target | - | | Related install fix |
| 165d2d69 | fix: G115 lint error and sync formulas | - | | Lint fix |
| fa0b1401 | fix(seance): claude --resume from original cwd | #528 | | |
| b73b3f5a | docs: PR branch naming guidance | #530 | | |
| ab563460 | fix(doctor): allowed_prefixes check | #608 | | |
| 91394835 | feat(doctor): stale-beads-redirect check | #734 | | |
| f4173e5b | feat(doctor): stale-beads-redirect topology | #734 | | |
| f9079ac1 | fix(hooks): crew/polecats-level settings | #738 | | |
| bba5d963 | fix(handoff): prevent self-kill during gt handoff | #882 | | Updated 2026-01-25 (rebased) |
| d1caaaed | fix(mail): crew/polecat ambiguity in session lookup | #914 | | Supersedes #896 |
| e06d7f7b | fix(mail): filter by read status in ListUnread | #936 | | Cherry-picked 2026-01-24 |
| c4368134 | fix(molecule): use Dependencies from bd show | #901 | | Cherry-picked 2026-01-24, resolved conflicts |
| 1052333f | fix(mayor): run session from mayorDir instead of townRoot | #972 | | Cherry-picked 2026-01-25 |
| dbb2ce0f | fix(tmux): wake Claude in detached sessions by triggering SIGWINCH | #976 | | Cherry-picked 2026-01-25 |
| 0817ec7d | fix(startup): unify agent startup with beacon + instructions in CLI prompt | #977 | | Cherry-picked 2026-01-25 |
| 0e873e96 | fix(shutdown): prevent self-kill during gt shutdown --all | #981 | | Cherry-picked 2026-01-25 |
| d20850e4 | fix(unsling): update bead status to open after clearing hook | #1032 | | Cherry-picked 2026-01-27 |
| cd2aaf0d | fix(nudge): resolve short addresses by trying crew then polecat | #1034 | | Cherry-picked 2026-01-27 |
| c3bb1c4b | feat(doctor): add stale-agent-beads check for removed crew | #1036 | | Cherry-picked 2026-01-27 |
| a43f6f7e | fix(mail): mark messages as read when viewed via gt mail read | #1033 | | Cherry-picked 2026-01-27 |
| 37ef73ad | fix(session): remove redundant gt prime instruction | #1037 | | Cherry-picked 2026-01-28 |
| 0f8dbbad | fix(polecat): prevent orphaned hooked work during concurrent sling | #1081 | | Cherry-picked 2026-01-29 |
| b1096f83 | fix(convoy): query external rig databases for cross-rig tracking | #916 | | Cherry-picked 2026-01-29 |
| 52f1a405 | feat(deacon): add feed-stranded-convoys step to patrol | #1092 | PiotrTrzpil | Cherry-picked 2026-01-29 |
| e1dec183 | fix(patrol): use gt convoy commands instead of bd list | #1106 | | Cherry-picked 2026-01-29 |
| 951b8651 | fix(daemon): migrate errant .beads in town-level services | #1113 | | Cherry-picked 2026-01-29 |
| 924e9782 | fix(daemon): prevent deadlock in errant beads migration | #1113 | | Cherry-picked 2026-01-29 |
| a35093ac | feat(dog): add session management and delayed dispatch | #1015 | | Cherry-picked 2026-01-29 |
| c8f25a5d | feat(tmux): add C-b g keybinding for agent switcher menu | #1111 | groblegark | Cherry-picked 2026-01-29 |
| 56d15a14 | feat(doctor): add --slow flag to highlight slow checks | - | | Local, not yet PR'd |
| c7fe1936 | feat(doctor): add streaming output for real-time progress | - | | Local, not yet PR'd |
| 308ed91c | fix(doctor): update claude_settings_check to use working dirs | - | | Local, not yet PR'd |
| 9f91b79b | perf(status): parallelize beads pre-fetching (~2x faster) | - | | Local, not yet PR'd |
| 9e50166f | fix(settings): use settings.local.json in working directory | - | | Local, not yet PR'd |
| f6afa4d3 | fix(convoy): detect orphaned molecules as stranded | #1118 | | Cherry-picked 2026-01-29 |
| 68f508d2 | fix(deacon): update heartbeat on every startup, including resume | #1119 | | Cherry-picked 2026-01-30 |
| d55a114b | fix(boot): use flock instead of session check in AcquireLock | #1123 | | Cherry-picked 2026-01-30 |
| f251c40d | fix(boot): add idle detection to triage for alive-but-not-patrolling Deacon | #1125 | | Cherry-picked 2026-01-30, includes restart-failed reporting |
| b125ef1b | fix(boot): ensure Boot is ephemeral (fresh each tick) | #1126 | | Cherry-picked 2026-01-30 |
| 18bedfdd | test(dog): add tests for gt dog done command | - | | Cherry-picked 2026-01-30 (upstream) |
| 61756fa3 | feat(dog): add gt dog clear command to reset stuck dogs | #1127 | | Cherry-picked 2026-01-30 |
| 93988ce9 | feat(warrant): add gt warrant command for agent termination | #1127 | | Cherry-picked 2026-01-30 |

## PRs Recently Merged to Upstream

These were in our previous build but are now in v0.5.0:
- #729 - fix(daemon): spawn Deacon after kill
- #731 - fix(costs): add event to BeadsCustomTypes
- #779 - feat(doctor): auto-fix SessionHookCheck
- #850 - fix(hooks): allow feature branches
- #854 - fix(formula): clarify WITNESS_PING routing

## PRs to Watch

Open PRs that may get merged (check before update):
- #690 - fix(polecat): improve lifecycle handling (**tested, incomplete - respawn functions not wired**)
- #617 - fix(agents): FormatStartupNudge beacon (on local branch `pr/cli-prompt-propulsion-clean`)
- #847 - fix(refinery): role-specific runtime config
- #815 - feat(rig): --adopt flag
- #799 - feat(cmd): preflight/postflight commands
- #796 - feat(doctor): beads-sync worktree health

## PRs Likely Superseded

These can probably be closed:
- #524 - KillSessionWithProcesses (similar fixes merged upstream)
- #509 - --hook flag (upstream has 8c200d4a)

## Conflict-Prone Files

These files often have conflicts during rebase:
- `internal/polecat/manager_test.go` - Test changes, keep upstream version
- `internal/cmd/doctor.go` - Doctor check registrations
- `internal/cmd/seance.go` - Variable redeclaration issues

## Known Test Adjustments

When rebasing, you may need to:
- Remove `TestAddWithOptions_AgentsMDFallback` (tests old behavior our install fix changed)
- Fix `townRoot, _ :=` to `townRoot, _ =` in seance.go (variable already declared)

## Last Update

- **Date**: 2026-01-30
- **Version**: v0.5.0
- **Branch**: main
- **Head**: 93988ce9 (48 commits ahead of upstream)
- **Backup**: build-with-fixes-backup-20260122-2
