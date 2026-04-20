# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).

## [Unreleased]

### Added
- `-1` flag for oneshot mode: push once, wait for the workflow to finish, then exit.
  Exit codes: 0 = success or no workflow, 1 = application/connectivity error, 2 = workflow failed.
