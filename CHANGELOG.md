# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- When a pushed commit triggers no GitHub Actions workflow within 60 s, display the last run of each workflow (name, date, time, commit SHA, status). If any run is still in progress, monitor it to completion and report the result.
- `-1` flag for oneshot mode: push once, wait for the workflow to finish, then exit.
  Exit codes: 0 = success or no workflow, 1 = application/connectivity error, 2 = workflow failed.
- Repository name prefix on every output line, e.g. `(myrepo) [09:39:38] CI passed`, so multiple autopush terminals are easy to distinguish.
- Multiple repository support: pass one or more directory arguments to watch them all concurrently (`autopush dir1 dir2`). Works with `-1` too — all repos are pushed in parallel and the exit code is the worst outcome across all of them.
- `-r` flag: automatically discover and watch git submodules (recursive) alongside the parent repo. Submodules are validated the same way as `-a` repositories: those not in a pushable state (typically detached HEAD) are skipped with a warning instead of failing repeatedly at push time.
- `-p` flag: run `git pull --rebase` before each push, so remote-ahead situations are resolved automatically. The pull is skipped when the working tree is dirty, since a rebase cannot run there; the push still carries any committed work.
- `-a` flag: watch every usable git repository in the current directory, without recursing into subdirectories. Combinable with `-r` and `-p`. A repository is included only when it is on a branch, has an upstream tracking branch whose remote-tracking ref has been fetched, and that remote is an ssh URL. Repositories that fail a check (detached HEAD, no upstream, upstream not fetched, https-only remote, broken repository) are skipped with a warning naming the reason; https-only repositories can still be watched by passing their path explicitly.
- Skip warnings are printed in orange when output goes to a terminal.
- `-c <command>` flag: run a shell command from the repository root after autopush pulled or pushed changes, for example `-c "make build && make install"`. It runs once per push cycle, after the push; its output is shown as-is and a non-zero exit is reported.

### Changed
- Push status is reported only after the push completes. The tool no longer prints a "pushing..." line before knowing the result, which previously contradicted the following "up to date" line when the remote was already current.

### Fixed
- `.gh_token` is looked up in the root of each watched repository, then in `$HOME`. It was previously read relative to the working directory, so a repository given by path never had its own token file found.
