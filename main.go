package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

func main() {
	oneshot := flag.Bool("1", false, "push once, wait for workflow, exit with status")
	recursive := flag.Bool("r", false, "also watch and push submodules")
	doPull := flag.Bool("p", false, "pull --rebase before each push")
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

	if *oneshot {
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
		os.Exit(exitCode)
	}

	for _, r := range repos {
		go runDaemon(r.root, r.name, *doPull)
	}
	select {}
}

func runDaemon(repoRoot, repoName string, doPull bool) {
	branch, _ := getCurrentBranch(repoRoot)
	notify(repoName, fmt.Sprintf("watching [%s]", branch))

	commitMsgPath := filepath.Join(repoRoot, ".git", "COMMIT_EDITMSG")
	watcher, err := NewWatcher(commitMsgPath, repoName)
	if err != nil {
		notify(repoName, "cannot start watcher: "+err.Error())
		return
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
			return
		}
		token := githubToken()
		if token == "" {
			notify(repoName, "workflow check disabled: github token not set")
			return
		}
		remoteURL, err := getRemoteURL(repoRoot)
		if err != nil {
			notify(repoName, "workflow check disabled: cannot read remote URL")
			return
		}
		owner, repo, err := parseGitHubOwnerRepo(remoteURL)
		if err != nil {
			notify(repoName, "workflow check disabled: remote is not a GitHub repo")
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
		return 1
	}

	if doPull {
		notify(repoName, "pulling...")
		if err := pull(repoRoot); err != nil {
			notify(repoName, "pull failed: "+err.Error())
			return 1
		}
		sha, err = getCurrentSHA(repoRoot)
		if err != nil {
			notify(repoName, "error after pull: "+err.Error())
			return 1
		}
	}

	notify(repoName, "pushing "+sha[:8]+"...")
	pushed, err := push(repoRoot)
	if err != nil {
		notify(repoName, "push failed: "+err.Error())
		return 1
	}
	if pushed {
		notify(repoName, "pushed "+sha[:8])
	} else {
		notify(repoName, "up to date")
	}

	if !hasWorkflows(repoRoot) {
		notify(repoName, "no workflows configured")
		return 0
	}
	token := githubToken()
	if token == "" {
		return 0
	}
	remoteURL, err := getRemoteURL(repoRoot)
	if err != nil {
		return 0
	}
	owner, repo, err := parseGitHubOwnerRepo(remoteURL)
	if err != nil {
		return 0
	}

	return watchWorkflows(context.Background(), owner, repo, sha, token, repoName, true)
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
