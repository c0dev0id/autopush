# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`autopush` watches one or more local git repositories for new commits, pushes
them automatically, and monitors the resulting GitHub Actions workflows until
they settle. Status is reported to stdout and to the terminal/tmux window title.
OpenBSD is the primary target platform.

Key design constraint: the GitHub API is only queried while a workflow is
in-flight. Once every run reaches a terminal state, polling stops and the tool
idles until the next commit. A newer commit cancels any in-flight workflow watch.

## Commands

The file watcher (`watch.go`) uses kqueue(2), so the package only compiles on
a BSD or macOS. On Linux, `go build`, `go vet` and `go test` all fail in
`watch.go` with `undefined: unix.Kqueue` -- that is expected, not a bug to fix.
Cross-compile instead:

```sh
GOOS=openbsd go build ./...              # type-check the binary
GOOS=openbsd go vet ./...                # lint (covers test files too)
GOOS=openbsd go test -c -o /dev/null .   # compile the unit tests
GOOS=openbsd go test -tags integration -c -o /dev/null .   # compile the integration test
```

The unit tests cannot be executed on Linux; run them on a kqueue platform.

On OpenBSD/macOS:

```sh
go build ./...                 # or: make build
go vet ./...
go test ./...                  # or: make test (unit tests, shell out to real git)
go test -run TestReposInDir -v .   # single test
make install                   # build + copy to $HOME/.bin/autopush
make integration               # see below
```

`make integration` (`go test -tags integration -run TestIntegration -v -timeout 30m .`)
drives the built binary against a real GitHub repository and the live Actions
API: default `git@github.com:c0dev0id/autopush-test.git`, overridable with
`AUTOPUSH_TEST_REPO`. It needs a token and ssh push access, and it
**force-resets the remote branch to its root commit** before and after every
scenario. Without a token it skips itself.

Usage:

```sh
./autopush                 # daemon, watch current dir
./autopush /path/to/repo   # daemon, watch given repo(s)
./autopush -1              # oneshot: push, wait for CI, exit with status
./autopush -r              # also watch/push submodules (recursive)
./autopush -p              # pull --rebase before each push
./autopush -p 30           # also pull --rebase every 30s
./autopush -a              # watch every usable repo in the current dir
./autopush -c "make build && make install"   # run this in the repo root after it pulled or pushed changes
./autopush -arp60          # short flags bundle; a number after p is its interval
```

Provide a token for workflow monitoring via `GITHUB_TOKEN`, `GH_TOKEN`, or a
`.gh_token` file in the repository root or `$HOME`. Without a token, pushes
still happen; workflow checks are skipped.

Oneshot exit codes: `0` ok / no workflow, `1` error, `2` CI failed.

## Code Layout

Single `package main`, one file per responsibility:

- **main.go** — flag pre-scans and parsing, repo resolution, daemon vs oneshot
  dispatch, signal handling, per-repo goroutines, token lookup, `-c` command execution.
- **watch.go** — kqueue(2) watcher on a single file (`COMMIT_EDITMSG`).
- **git.go** — all `git` subprocess wrappers, workflow-file detection,
  submodule listing, repo usability checks (`repoUnusableReason`), GitHub
  remote-URL parsing.
- **github.go** — GitHub Actions REST polling and terminal-state detection.
- **notify.go** — stdout output (colored on a TTY) and window-title updates,
  with aggregate OK/FAIL state across all watched repos.
- **git_test.go / main_test.go** — unit tests. `git_test.go` builds real
  repositories (with bare-repo upstreams) in `t.TempDir()` and shells out to
  `git`; it skips when `git` is not on `PATH`.
- **integration_test.go** — behind the `integration` build tag; see above.

## Architecture Notes

- **Repo selection.** Paths given on the command line are only checked for
  being a repository on a branch (detached HEAD is fatal). Repositories found
  by `-a` and submodules found by `-r` additionally go through
  `repoUnusableReason`: they must have an upstream tracking branch whose
  remote-tracking ref has been fetched, and the tracking remote must be an ssh
  URL. Anything failing a check is skipped with an orange warning. This is why
  an https-only repo works when passed explicitly but is skipped by `-a`.

- **Commit detection.** `watch.go` registers `COMMIT_EDITMSG` with kqueue
  (`EVFILT_VNODE`, write/delete/rename/attrib) and re-opens the file after git
  swaps it in. A 30s polling ticker in `runDaemon` is a safety net and the only
  mechanism when `COMMIT_EDITMSG` does not exist at startup. Detection drives
  `doCheck`, which compares `HEAD` against the last pushed SHA.

- **Push & recovery.** `push` reports whether anything was actually sent
  (distinguishing "everything up-to-date"). On failure the error is surfaced and
  the repo is marked FAIL; the watcher keeps running, so once the user resolves
  the problem and commits again, the next event retries. With `-p`, a
  `git pull --rebase` runs first and the SHA is re-read (rebase rewrites it);
  the pull is skipped, not failed, while the working tree is dirty.

- **Change command.** `-c <command>` runs `sh -c <command>` in the repo root,
  synchronously, once per sync cycle: after a pull that moved `HEAD` or a push
  that sent commits (also after a push failure that followed such a pull). Its
  output is passed through unprefixed. A non-zero exit is reported as a red
  line but does not change the repo's OK/FAIL state or the oneshot exit code,
  which describe push and CI only.

- **Workflow observation.** `watchWorkflows` runs under a `context.CancelFunc`
  stored in `doCheck`; a newer commit cancels the previous watch before starting
  a new one. It waits up to `appearTimeout` for runs tied to the pushed SHA, then
  polls every `pollInterval` until all runs are `completed`. If no run ever
  appears for the SHA, it falls back to reporting the latest run of each
  workflow and monitoring any that are still in progress.

- **Notification.** `setXTitle` writes terminal escape sequences to stdout: the
  xterm title (`\033]0;…\007`) and the screen/tmux window-title
  (`\033k…\033\\`). There is no tmux variable or `status-right` integration —
  to show status in tmux, let the window title through (e.g. keep
  `automatic-rename` off while autopush owns the title). On exit the daemon
  restores `automatic-rename on`. `setRepoStatus` must only be called at terminal
  states (push done, CI settled), never for transient "pushing…"/"pulling…".

## Conventions

- **Flag parsing.** Two pre-scans rewrite `os.Args` before `flag.Parse()`
  because Go's `flag` can express neither POSIX bundles nor a flag that is both
  a bool and takes a value: `expandBundledFlags` splits `-arp60` into
  `-a -r -p=60`, then `extractPullInterval` strips the value from `-p`. A new
  short flag must be added to the `known` map in `expandBundledFlags` (and to
  `TestExpandBundledFlags`), otherwise any bundle containing it passes through
  untouched and `flag.Parse` rejects it. Flags that take a value (`-c`) are
  deliberately not in that map: they cannot be bundled and must stand alone.
- **Status text drives colour.** `colorLine` colours a line by substring of the
  status: "failed" red, "passed" green, "skipped" orange. Keep those words out
  of neutral messages. The integration test also matches on `CI passed`,
  `FAILED` and `pushed` in the output.
- OpenBSD is primary. Use kqueue for file watching — no `inotify` or other
  Linux-isms. Only the Go stdlib and `golang.org/x/sys/unix` are depended on.
