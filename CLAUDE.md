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

```sh
go build ./...   # or: make build
go vet ./...     # lint
make install     # build + copy to $HOME/.bin/autopush

./autopush                 # daemon, watch current dir
./autopush /path/to/repo   # daemon, watch given repo(s)
./autopush -1              # oneshot: push, wait for CI, exit with status
./autopush -r              # also watch/push submodules (recursive)
./autopush -p              # pull --rebase before each push
./autopush -p 30           # also pull --rebase every 30s
```

Provide a token for workflow monitoring via `GITHUB_TOKEN`, `GH_TOKEN`, or a
`.gh_token` file in the repo or `$HOME`. Without a token, pushes still happen;
workflow checks are skipped.

Oneshot exit codes: `0` ok / no workflow, `1` error, `2` CI failed.

## Code Layout

Single `package main`, one file per responsibility:

- **main.go** — flag parsing, repo resolution, daemon vs oneshot dispatch,
  signal handling, per-repo goroutines, token lookup.
- **watch.go** — kqueue(2) watcher on a single file (`COMMIT_EDITMSG`).
- **git.go** — all `git` subprocess wrappers, workflow-file detection,
  submodule listing, GitHub remote-URL parsing.
- **github.go** — GitHub Actions REST polling and terminal-state detection.
- **notify.go** — stdout output (colored on a TTY) and window-title updates,
  with aggregate OK/FAIL state across all watched repos.

## Architecture Notes

- **Commit detection.** `watch.go` registers `COMMIT_EDITMSG` with kqueue
  (`EVFILT_VNODE`) and signals on write/rename/delete; it re-opens the file
  after git swaps it in. A 30s polling ticker in `runDaemon` is a safety net.
  Detection drives `doCheck`, which compares `HEAD` against the last pushed SHA.

- **Push & recovery.** `push` reports whether anything was actually sent
  (distinguishing "everything up-to-date"). On failure the error is surfaced and
  the repo is marked FAIL; the watcher keeps running, so once the user resolves
  the problem and commits again, the next event retries. With `-p`, a
  `git pull --rebase` runs first and the SHA is re-read (rebase rewrites it).

- **Workflow observation.** `watchWorkflows` runs under a `context.CancelFunc`
  stored in `doCheck`; a newer commit cancels the previous watch before starting
  a new one. It waits up to `appearTimeout` for runs tied to the pushed SHA, then
  polls every `pollInterval` until all runs are `completed`. If no run ever
  appears for the SHA, it falls back to reporting/monitoring the most recent runs.

- **Notification.** `setXTitle` writes terminal escape sequences to stdout: the
  xterm title (`\033]0;…\007`) and the screen/tmux window-title
  (`\033k…\033\\`). There is no tmux variable or `status-right` integration —
  to show status in tmux, let the window title through (e.g. keep
  `automatic-rename` off while autopush owns the title). On exit the daemon
  restores `automatic-rename on`. `setRepoStatus` must only be called at terminal
  states (push done, CI settled), never for transient "pushing…"/"pulling…".

## Conventions

- **Flag parsing gotcha:** `extractPullInterval()` runs before `flag.Parse()` and
  rewrites `os.Args` to support optional-value `-p N` (Go's `flag` can't express
  a flag that is both a bool and takes a value). Any new flag must survive this
  pre-scan.
- OpenBSD is primary. Use kqueue for file watching — no `inotify` or other
  Linux-isms. Only the Go stdlib and `golang.org/x/sys/unix` are depended on.
