# Features

- **Commit detection**: kqueue(2) watch on `COMMIT_EDITMSG`, with a 30-second
  poll as a safety net.
- **Automatic push**: pushes the current branch when HEAD changes and reports
  "pushed" or "up to date".
- **Pull before push (`-p`)**: `git pull --rebase` before each push; skipped
  while tracked files have uncommitted changes.
- **Periodic pull (`-p N`)**: also pulls every N seconds. A pull that brings in
  commits starts a normal sync cycle.
- **Change command (`-c`)**: runs a shell command in the repository root once
  per cycle in which a pull moved HEAD or a push sent commits.
- **CI monitoring**: follows the GitHub Actions runs for the pushed commit on
  the repository the push went to; reports success, or each failed run with a
  link. When the commit triggered no run, shows the latest run of each workflow
  and follows those still in progress.
- **Idle when settled**: the GitHub API is queried only while runs are in
  progress. A newer commit cancels the current watch.
- **Multiple repositories**: any number of paths. `-a` adds every usable
  repository in the current directory, `-r` adds submodules. Repositories found
  this way must be on a branch with a fetched upstream and an ssh remote.
- **Oneshot mode (`-1`)**: push once, wait for CI, exit `0` (ok or no
  workflow), `1` (error) or `2` (CI failed). With several repositories the
  worst result wins.
- **Status display**: output lines prefixed with repository and time, colored
  on a terminal; aggregate `OK`/`FAIL` in the xterm title and the screen/tmux
  window name.
- **Token lookup**: `GITHUB_TOKEN`, `GH_TOKEN`, `.gh_token` in the repository
  root, `~/.gh_token`. Without a token, pushing still works.
