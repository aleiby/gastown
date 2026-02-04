# Local Build State

Note: When applying PRs from forks, cherry-pick the specific commits (`gh pr view <PR#> --json commits`) rather than merging the branch, which may include unrelated commits.

## Base
upstream/main: 13461161 (v0.5.0+214)

## Applied PRs

| PR | Commit | Description |
|----|--------|-------------|
| #1178 | 60efa615 | fix(boot): detect stale molecule progress for idle Deacon |
| #1179 | 167b5666 | fix(sling): skip nudge when self-slinging |
| #1185 | 15db89f0 | feat(doctor): add stale-beads-redirect check |
| #1185 | 3a5cade3 | feat(doctor): extend stale-beads-redirect to verify redirect topology |
| #1186 | 2192791a | fix(formula): add missing skipped_count variable declaration |
| #1187 | e3828b82 | fix(test): add --adopt flag to integration tests using local paths |
| #1188 | 2cef2be5 | fix(doctor): allow identity anchor CLAUDE.md at town root |
| #1193 | ce1c6b90 | fix(boot): skip IDLE_CHECK when Deacon is in await-signal backoff |
| #1195 | 8575b49a | feat(dashboard): show convoy titles alongside IDs |
| #1196 | 3d1a95f2 | fix(web): remove os.Executable() to prevent fork bomb in tests |
| #1198 | 337cc765 | fix(witness): add await-signal backoff to prevent tight loop when idle |
| #1199 | cc8f71f6 | docs: add PR branch naming guidance to CONTRIBUTING.md |
| #1200 | dc0664d9 | feat(doctor): distinguish fixed vs unfixed in --fix output |
| #1200 | b5cbeb00 | fix(doctor): show wrench icon for fixed items in streaming output |
| #1203 | 0b19d1d4 | fix(await-signal): handle empty stdout when querying agent bead |

## Build from Source (Linux)

Building requires CGO and these system dependencies:

```bash
# Debian/Ubuntu
sudo apt-get install -y gcc g++ libzstd-dev libicu-dev

# Build and install (ldflags required for version info and BuiltProperly check)
VERSION=$(git describe --tags --always --dirty | sed 's/^v//') && \
COMMIT=$(git rev-parse --short HEAD) && \
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ") && \
go generate ./... && \
go install -ldflags "-X github.com/steveyegge/gastown/internal/cmd.Version=$VERSION -X github.com/steveyegge/gastown/internal/cmd.Commit=$COMMIT -X github.com/steveyegge/gastown/internal/cmd.BuildTime=$BUILD_TIME -X github.com/steveyegge/gastown/internal/cmd.BuiltProperly=1" ./cmd/gt
```

### Pre-Build Checklist

1. **Push all commits first** (including UPDATE-NOTES.md updates)
2. **Sync mayor/rig to origin/main**: `git -C ~/gt/gastown/mayor/rig pull`
3. **Sync your build workspace** to the same origin/main commit (clean working tree)
4. **Verify no "dirty" flag** in `git describe --tags --always --dirty`

origin/main is the source of truth. The stale binary check compares `gt version` against
`~/gt/gastown/mayor/rig` HEAD - if your binary was built from a different commit, you'll
get warnings.

Binary installs to `~/go/bin/gt` (ensure this is in your PATH). Remove any stale
binaries from other locations (e.g., `rm ~/.local/bin/gt`) that might shadow it.
