# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`autopush` is a Git automation daemon that watches a local repository for new commits, automatically pushes them, and monitors GitHub Actions CI workflows. It targets OpenBSD as the primary platform.

## Intended Behavior (from spec)

The core loop (from README):
1. Detect new commits in the watched git repo
2. Run `git push`; on failure — watch the git directory and wait for the user to resolve the issue manually, then retry once the repo is clean and new commits appear
3. On successful push — check if GitHub Actions workflows are configured; if yes, poll workflow status until completion, then report success or failure (with a link to the failed workflow)
4. Notify via: stdout, X-window title, tmux status bar

Key design constraint: GitHub API is only queried while a workflow is in-flight. Once a workflow settles (pass or fail), polling stops and the tool goes idle.

## Architecture Notes

The tool has four logical responsibilities that should stay separated:

- **Commit detection** — watching the git directory for new commits (e.g. `.git/refs/heads/<branch>` mtime or reading `COMMIT_EDITMSG`)
- **Push & error recovery** — running `git push`, detecting failure modes (detached HEAD, unpulled remote commits, etc.), and re-watching for a clean state
- **Workflow observation** — querying the GitHub API for workflow runs associated with the pushed commit SHA; polling until terminal state
- **Notification** — writing status to stdout, updating the X window title (`xdotool` or direct `DISPLAY`/`xprop`), and updating the tmux status bar (`tmux set-option -g status-right`)

## Commands

```sh
go build ./...          # build
go vet ./...            # lint
./autopush              # run in current directory
./autopush /path/to/repo
```

Set `GITHUB_TOKEN` or `GH_TOKEN` in the environment for workflow monitoring.

For tmux status bar display, add `#{@autopush}` to `status-right` in `.tmux.conf`.

## Platform

OpenBSD is the primary target. Follow the global CLAUDE.md for port/package conventions if packaging is needed. Avoid Linux-isms (e.g. `inotify`; use `kqueue` for filesystem watching instead).
