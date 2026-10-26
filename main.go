package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	// Both run before flag.Parse. expandBundledFlags splits POSIX-style
	// bundles (-arp, -rap60) into separate tokens; extractPullInterval then
	// strips the optional interval from -p so flag.Parse sees -p as a bool.
	os.Args = expandBundledFlags(os.Args)
	pullInterval := extractPullInterval()

	oneshot := flag.Bool("1", false, "push once, wait for workflow, exit with status")
	recursive := flag.Bool("r", false, "also watch and push submodules that are in a pushable state")
	doPull := flag.Bool("p", false, "pull --rebase before each push; -p N also pulls every N seconds")
	allRepos := flag.Bool("a", false, "watch every usable git repo in the current directory (on a branch, ssh upstream)")
	onChange := flag.String("c", "", "shell command to run in the repo root after it pulled or pushed changes")
	flag.Parse()

	dirs := flag.Args()
	if *allRepos {
		found, err := reposInDir(".")
		if err != nil {
			fatalf("cannot scan current directory: %v", err)
		}
		if len(found) == 0 {
			fatalf("no usable git repositories (on a branch with an ssh upstream) found in the current directory")
		}
		dirs = append(dirs, found...)
	}
	if len(dirs) == 0 {
		dirs = []string{"."}
	}

	type repoInfo struct {
		root string
		name string
	}

	seen := make(map[string]bool)
	repos := make([]repoInfo, 0, len(dirs))

	addRepo := func(root, name string) {
		if seen[root] {
			return
		}
		seen[root] = true
		repos = append(repos, repoInfo{root, name})
	}

	for _, dir := range dirs {
		repoRoot, err := getRepoRoot(dir)
		if err != nil {
			fatalf("not a git repository (%s): %v", dir, err)
		}
		branch, err := getCurrentBranch(repoRoot)
		if err != nil {
			fatalf("cannot determine branch (%s): %v", repoRoot, err)
		}
		if branch == "HEAD" {
			fatalf("detached HEAD in %s -- check out a branch first", repoRoot)
		}
		addRepo(repoRoot, filepath.Base(repoRoot))

		if *recursive {
			subPaths, err := listSubmodules(repoRoot)
			if err != nil {
				fatalf("cannot list submodules (%s): %v", repoRoot, err)
			}
			for _, sp := range subPaths {
				if reason := repoUnusableReason(sp); reason != "" {
					notify(filepath.Base(sp), "skipped (-r): "+reason)
					continue
				}
				subRoot, _ := getRepoRoot(sp)
				addRepo(subRoot, filepath.Base(subRoot))
			}
		}
	}

	repoNames := make([]string, len(repos))
	for i, r := range repos {
		repoNames[i] = r.name
	}
	initTmuxState(repoNames)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)

	if *oneshot {
		go func() {
			<-sigCh
			restoreTmuxRename()
			os.Exit(1)
		}()
		var (
			wg       sync.WaitGroup
			mu       sync.Mutex
			exitCode int
		)
		for _, r := range repos {
			wg.Add(1)
			go func(r repoInfo) {
				defer wg.Done()
				if code := runOneshot(r.root, r.name, *doPull, *onChange); code != 0 {
					mu.Lock()
					if code > exitCode {
						exitCode = code
					}
					mu.Unlock()
				}
			}(r)
		}
		wg.Wait()
		restoreTmuxRename()
		os.Exit(exitCode)
	}

	for _, r := range repos {
		go runDaemon(r.root, r.name, *doPull, pullInterval, *onChange)
	}
	<-sigCh
	restoreTmuxRename()
}

func runDaemon(repoRoot, repoName string, doPull bool, pullInterval int, onChange string) {
	branch, _ := getCurrentBranch(repoRoot)
	notify(repoName, fmt.Sprintf("watching [%s]", branch))

	gitDir, err := getGitDir(repoRoot)
	if err != nil {
		gitDir = filepath.Join(repoRoot, ".git")
	}
	commitMsgPath := filepath.Join(gitDir, "COMMIT_EDITMSG")
	watcher, err := NewWatcher(commitMsgPath, repoName)
	if err != nil {
		notify(repoName, "cannot start watcher: "+err.Error())
		setRepoStatus(repoName, true)
		return
	}

	if pullInterval > 0 {
		go func() {
			t := time.NewTicker(time.Duration(pullInterval) * time.Second)
			defer t.Stop()
			for range t.C {
				if !isWorkspaceClean(repoRoot) {
					notify(repoName, "can't pull, workspace not clean")
					continue
				}
				changed, err := pull(repoRoot)
				if err != nil {
					notify(repoName, "periodic pull failed: "+err.Error())
					continue
				}
				if changed {
					runOnChange(repoRoot, repoName, onChange)
				}
			}
		}()
	}

	var (
		lastPushedSHA  string
		cancelWorkflow context.CancelFunc
	)

	doCheck := func() {
		sha, err := getCurrentSHA(repoRoot)
		if err != nil || sha == lastPushedSHA {
			return
		}

		if cancelWorkflow != nil {
			cancelWorkflow()
			cancelWorkflow = nil
		}

		changed := false
		// A dirty tree cannot be rebased; skip the pull (not a failure) and let
		// the push carry the committed work. Prevents re-failing every tick.
		if doPull && isWorkspaceClean(repoRoot) {
			notify(repoName, "pulling...")
			changed, err = pull(repoRoot)
			if err != nil {
				notify(repoName, "pull failed: "+err.Error())
				setRepoStatus(repoName, true)
				return
			}
			// rebase may have rewritten local SHAs; re-read before push
			sha, err = getCurrentSHA(repoRoot)
			if err != nil || sha == lastPushedSHA {
				return
			}
		}

		pushed, err := push(repoRoot)
		if err != nil {
			notify(repoName, "push failed: "+err.Error())
			setRepoStatus(repoName, true)
			if changed {
				runOnChange(repoRoot, repoName, onChange)
			}
			return
		}

		lastPushedSHA = sha
		if !pushed {
			notify(repoName, "up to date")
		} else {
			notify(repoName, "pushed "+sha[:8])
		}
		if changed || pushed {
			runOnChange(repoRoot, repoName, onChange)
		}

		if !hasWorkflows(repoRoot) {
			notify(repoName, "no workflows configured")
			setRepoStatus(repoName, false)
			return
		}
		token := githubToken(repoRoot)
		if token == "" {
			notify(repoName, "workflow check disabled: github token not set")
			setRepoStatus(repoName, false)
			return
		}
		remoteURL, err := getRemoteURL(repoRoot)
		if err != nil {
			notify(repoName, "workflow check disabled: cannot read remote URL")
			setRepoStatus(repoName, false)
			return
		}
		owner, repo, err := parseGitHubOwnerRepo(remoteURL)
		if err != nil {
			notify(repoName, "workflow check disabled: remote is not a GitHub repo")
			setRepoStatus(repoName, false)
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancelWorkflow = cancel
		go func() { watchWorkflows(ctx, owner, repo, sha, token, repoName, false) }()
	}

	doCheck()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-watcher.C:
			doCheck()
		case <-ticker.C:
			doCheck()
		}
	}
}

func runOneshot(repoRoot, repoName string, doPull bool, onChange string) int {
	sha, err := getCurrentSHA(repoRoot)
	if err != nil {
		notify(repoName, "error: cannot get current SHA: "+err.Error())
		setRepoStatus(repoName, true)
		return 1
	}

	changed := false
	if doPull && isWorkspaceClean(repoRoot) {
		notify(repoName, "pulling...")
		changed, err = pull(repoRoot)
		if err != nil {
			notify(repoName, "pull failed: "+err.Error())
			setRepoStatus(repoName, true)
			return 1
		}
		sha, err = getCurrentSHA(repoRoot)
		if err != nil {
			notify(repoName, "error after pull: "+err.Error())
			setRepoStatus(repoName, true)
			return 1
		}
	}

	pushed, err := push(repoRoot)
	if err != nil {
		notify(repoName, "push failed: "+err.Error())
		setRepoStatus(repoName, true)
		if changed {
			runOnChange(repoRoot, repoName, onChange)
		}
		return 1
	}
	if pushed {
		notify(repoName, "pushed "+sha[:8])
	} else {
		notify(repoName, "up to date")
	}
	if changed || pushed {
		runOnChange(repoRoot, repoName, onChange)
	}

	if !hasWorkflows(repoRoot) {
		notify(repoName, "no workflows configured")
		setRepoStatus(repoName, false)
		return 0
	}
	token := githubToken(repoRoot)
	if token == "" {
		setRepoStatus(repoName, false)
		return 0
	}
	remoteURL, err := getRemoteURL(repoRoot)
	if err != nil {
		setRepoStatus(repoName, false)
		return 0
	}
	owner, repo, err := parseGitHubOwnerRepo(remoteURL)
	if err != nil {
		setRepoStatus(repoName, false)
		return 0
	}

	return watchWorkflows(context.Background(), owner, repo, sha, token, repoName, true)
}

// extractPullInterval scans os.Args for -p N or -p=N, strips the numeric
// value from os.Args (so flag.Parse sees -p as a plain bool), and returns
// the interval in seconds. Returns 0 if -p has no numeric argument.
func extractPullInterval() int {
	filtered := os.Args[:1:1]
	interval := 0
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if val, ok := strings.CutPrefix(arg, "-p="); ok {
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				interval = n
				filtered = append(filtered, "-p")
				continue
			}
		} else if arg == "-p" && i+1 < len(os.Args) {
			if n, err := strconv.Atoi(os.Args[i+1]); err == nil && n > 0 {
				interval = n
				i++ // consume the number
				filtered = append(filtered, "-p")
				continue
			}
		}
		filtered = append(filtered, arg)
	}
	os.Args = filtered
	return interval
}

// expandBundledFlags splits POSIX-style bundled short flags into separate
// tokens, e.g. -arp becomes -a -r -p and -rap60 becomes -r -a -p=60. Only the
// known single-char flags are bundled; p may carry a trailing numeric value.
// Anything that is not a clean bundle (long flags, -p=N, unknown chars, paths)
// is passed through untouched so flag.Parse handles or reports it.
func expandBundledFlags(args []string) []string {
	known := map[byte]bool{'1': true, 'r': true, 'a': true, 'p': true}
	out := args[:1:1]
	for _, arg := range args[1:] {
		if len(arg) <= 2 || arg[0] != '-' || arg[1] == '-' || strings.Contains(arg, "=") {
			out = append(out, arg)
			continue
		}
		if expanded, ok := expandBundle(arg[1:], known); ok {
			out = append(out, expanded...)
		} else {
			out = append(out, arg)
		}
	}
	return out
}

// expandBundle expands the characters of a single bundle (without its leading
// dash). Trailing digits immediately after p are emitted as -p=<n>. Returns
// false if the bundle contains an unknown flag or a misplaced digit.
func expandBundle(body string, known map[byte]bool) ([]string, bool) {
	var out []string
	for i := 0; i < len(body); i++ {
		c := body[i]
		if !known[c] {
			return nil, false
		}
		if c == 'p' {
			if rest := body[i+1:]; rest != "" {
				if _, err := strconv.Atoi(rest); err == nil {
					return append(out, "-p="+rest), true
				}
			}
		}
		out = append(out, "-"+string(c))
	}
	return out, true
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "autopush: "+format+"\n", args...)
	os.Exit(1)
}

func githubToken(repoRoot string) string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	paths := []string{
		filepath.Join(repoRoot, ".gh_token"),
		filepath.Join(os.Getenv("HOME"), ".gh_token"),
	}
	for _, path := range paths {
		if b, err := os.ReadFile(path); err == nil {
			if t := strings.TrimSpace(string(b)); t != "" {
				return t
			}
		}
	}
	return ""
}

// runOnChange runs command through sh -c in repoRoot with its output passed
// through, and reports a non-zero exit. A no-op when command is empty.
func runOnChange(repoRoot, repoName, command string) {
	if command == "" {
		return
	}
	notify(repoName, "running: "+command)
	cmd := exec.Command("sh", "-c", command)
	cmd.Dir = repoRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		notify(repoName, "command failed: "+err.Error())
		return
	}
	notify(repoName, "command ok")
}
