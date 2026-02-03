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

# After pushing to origin/main, update the mayor's rig clone to avoid stale binary warnings:
git -C ~/gt/gastown/mayor/rig pull
```

**Important:** Build `gt` only after ALL commits (including this file's updates) are pushed.
The stale binary warning compares against `~/gt/gastown/mayor/rig` (origin/main). If you build
before pushing UPDATE-NOTES.md changes, the binary will appear stale.
