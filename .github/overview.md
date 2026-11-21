# Overview

## Problem

Pushing after every commit and then checking CI in a browser interrupts work at
the terminal. Forgetting to push leaves work unpublished; checking CI by hand
means polling a web page.

## Solution

autopush runs next to the editor and watches one or more local git
repositories. When a new commit appears, it pushes it, follows the GitHub
Actions runs for that commit until they finish, and reports the result on
stdout and in the terminal or tmux window title. It queries GitHub only while
runs are in progress and is idle otherwise.

Optional behaviour:

- Rebase onto the remote before each push, and on a timer (`-p`, `-p N`).
- Run a shell command after commits were pulled or pushed (`-c`), for example
  to rebuild and install.
- Watch many repositories: several paths, every repository in a directory
  (`-a`), submodules (`-r`).
- Push once and exit with the CI result (`-1`), for scripts.

OpenBSD is the primary platform. autopush builds on any system with kqueue(2).
