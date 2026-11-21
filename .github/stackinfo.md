# Stack

| Area | Choice |
|---|---|
| Language | Go, `go 1.22` in `go.mod` |
| Dependencies | Go standard library; `golang.org/x/sys` v0.21.0 (`unix`, for kqueue) |
| External programs | `git`, `sh`; `tmux` optional |
| Remote API | GitHub REST API, `X-GitHub-Api-Version: 2022-11-28` |
| Tests | `go test`. Unit tests use local git repositories; the integration test (`-tags integration`) uses a real GitHub repository |

## Targets

| Platform | Status |
|---|---|
| OpenBSD | Primary target |
| FreeBSD, NetBSD, DragonFly BSD, macOS | Builds (kqueue available); not tested |
| Linux | Not supported: no kqueue |

The test helper `isolateGit` relies on `GIT_CONFIG_GLOBAL`, available since
git 2.32.
