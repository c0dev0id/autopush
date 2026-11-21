# Decisions

## Commit detection with kqueue on COMMIT_EDITMSG

git writes `.git/COMMIT_EDITMSG` on every commit, so a kqueue(2) vnode watch on
that file notices commits without polling. A 30-second ticker covers missed
events and repositories where the file does not exist yet. OpenBSD is the
primary platform, so kqueue is used directly; inotify and other Linux
interfaces are out.

## Minimal dependencies

Only the Go standard library and `golang.org/x/sys/unix`. Before writing a
helper, check whether the standard library already provides it (`flag.BoolFunc`,
`net/url`, `os.UserHomeDir`, `log.Fatalf` replaced hand-written code).

## GitHub is polled only while runs are in flight

Polling stops once every run has completed and resumes with the next push, to
stay within API rate limits. A newer commit cancels the current watch.

## One goroutine per repository for git and the change command

The watcher, the 30-second ticker and the `-p N` ticker are cases of a single
select loop in `runDaemon`. A periodic pull on its own goroutine let `-c`
commands overlap, ran them twice in one cycle, and raced on `index.lock`. The
cost: a commit made while the `-c` command runs is pushed when it finishes.
Pulling during a build would rewrite files under it.

## The change command

- `sh -c`, not `$SHELL`: the command behaves the same for every user, and `&&`
  and pipes work in any POSIX sh.
- Runs once per cycle in which a pull moved HEAD or a push sent commits,
  including after a push failure that followed such a pull.
- A non-zero exit is reported but changes neither the OK/FAIL state nor the
  oneshot exit code. Those describe push and CI; a later "CI passed" would
  overwrite a build failure anyway.
- Daemon mode defers it until the workflow watch has started, so a long build
  does not delay CI reporting. Oneshot runs it before the CI wait, which
  overlaps with CI running on GitHub.
- `-c` takes a value and is therefore not part of short-flag bundles.

## Flag parsing

`-p` is a `flag.BoolFunc`, so `-p` and `-p=N` parse natively. The flag package
handles neither POSIX bundles (`-arp60`) nor a value after a space on a bool
flag (`-p 30`); `expandBundledFlags` and `joinPullInterval` rewrite those
before parsing.

## Change detection from SHAs, not git output

`pull` compares HEAD before and after, because git translates "Already up to
date". `push` matches "Everything up-to-date", which git's `transport.c` marks
as stable plumbing output that is never translated.

## Ask git which remote a push goes to

CI is watched on the repository the push went to.
`git for-each-ref --format=%(push:remotename)` returns git's own choice
(`pushRemote`, `pushDefault`, tracking remote) and `git remote get-url --push`
its push URL. Reimplementing git's order would drift from it.

## Repository validation for -a and -r

Repositories found automatically are watched only when they are on a branch,
have a fetched upstream and an ssh remote; https remotes usually need
credential handling autopush does not do. A path given explicitly is trusted
beyond the detached-HEAD check.

## Untracked files do not block the pull

A rebase runs with untracked files present, and `-c` commands often leave build
output behind. The check before pulling uses
`git status --porcelain --untracked-files=no`.

## Token lookup

Environment first (`GITHUB_TOKEN`, `GH_TOKEN`), then `.gh_token` in the
repository root, then in the home directory from `os.UserHomeDir`. Never the
working directory: a daemon watching other paths must not pick up an unrelated
file.

## Status in the window title

autopush writes the xterm title and the screen/tmux window-name escape
sequences; there is no tmux variable or `status-right` integration. tmux
applies the window name only with `allow-rename on` and then turns
`automatic-rename` off for that window, so autopush turns it back on when it
exits. The status changes only at terminal states (push done, CI settled),
never for transient steps such as "pulling...".

## Tests run git without the user's configuration

`isolateGit` hides global, system and environment-supplied git configuration.
A `url.<base>.insteadOf` rewrite made `TestReposInDir` fail on machines that
had one.

## API query strings use fmt.Sprintf

The values are a hex SHA and GitHub owner and repository names, which cannot
contain characters that need escaping; `url.Values` would add code without
effect.
