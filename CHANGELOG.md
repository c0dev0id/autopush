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
