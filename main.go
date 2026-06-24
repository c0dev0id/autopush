package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

func main() {
	// Must run before flag.Parse: strips the optional interval from -p N and
	// rewrites os.Args so flag.Parse sees -p as a plain bool.
	pullInterval := extractPullInterval()

	oneshot := flag.Bool("1", false, "push once, wait for workflow, exit with status")
	recursive := flag.Bool("r", false, "also watch and push submodules")
	doPull := flag.Bool("p", false, "pull --rebase before each push; -p N also pulls every N seconds")
	flag.Parse()

	dirs := flag.Args()
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
				subRoot, err := getRepoRoot(sp)
				if err != nil {
					fatalf("submodule not a git repository (%s): %v", sp, err)
				}
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
				if code := runOneshot(r.root, r.name, *doPull); code != 0 {
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
		go runDaemon(r.root, r.name, *doPull, pullInterval)
	}
	<-sigCh
	restoreTmuxRename()
}

func runDaemon(repoRoot, repoName string, doPull bool, pullInterval int) {
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
				if err := pull(repoRoot); err != nil {
					notify(repoName, "periodic pull failed: "+err.Error())
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

		if doPull {
			notify(repoName, "pulling...")
			if err := pull(repoRoot); err != nil {
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

		notify(repoName, "pushing "+sha[:8]+"...")
		pushed, err := push(repoRoot)
		if err != nil {
			notify(repoName, "push failed: "+err.Error())
			setRepoStatus(repoName, true)
			return
		}

		lastPushedSHA = sha
		if !pushed {
			notify(repoName, "up to date")
		} else {
			notify(repoName, "pushed "+sha[:8])
		}

		if !hasWorkflows(repoRoot) {
			notify(repoName, "no workflows configured")
			setRepoStatus(repoName, false)
			return
		}
		token := githubToken()
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

func runOneshot(repoRoot, repoName string, doPull bool) int {
	sha, err := getCurrentSHA(repoRoot)
	if err != nil {
		notify(repoName, "error: cannot get current SHA: "+err.Error())
		setRepoStatus(repoName, true)
		return 1
	}

	if doPull {
		notify(repoName, "pulling...")
		if err := pull(repoRoot); err != nil {
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

	notify(repoName, "pushing "+sha[:8]+"...")
	pushed, err := push(repoRoot)
	if err != nil {
		notify(repoName, "push failed: "+err.Error())
		setRepoStatus(repoName, true)
		return 1
	}
	if pushed {
		notify(repoName, "pushed "+sha[:8])
	} else {
		notify(repoName, "up to date")
	}

	if !hasWorkflows(repoRoot) {
		notify(repoName, "no workflows configured")
		setRepoStatus(repoName, false)
		return 0
	}
	token := githubToken()
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
		if strings.HasPrefix(arg, "-p=") {
			if n, err := strconv.Atoi(strings.TrimPrefix(arg, "-p=")); err == nil && n > 0 {
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

func fatalf(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "autopush: "+format+"\n", args...)
	os.Exit(1)
}

func githubToken() string {
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t
	}
	for _, path := range []string{".gh_token", filepath.Join(os.Getenv("HOME"), ".gh_token")} {
		if b, err := os.ReadFile(path); err == nil {
			if t := strings.TrimSpace(string(b)); t != "" {
				return t
			}
		}
	}
	return ""
}
