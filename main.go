package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	oneshot := flag.Bool("1", false, "push once, wait for workflow, exit with status")
	flag.Parse()

	dir := "."
	if args := flag.Args(); len(args) > 0 {
		dir = args[0]
	}

	repoRoot, err := getRepoRoot(dir)
	if err != nil {
		fatalf("not a git repository: %v", err)
	}

	branch, err := getCurrentBranch(repoRoot)
	if err != nil {
		fatalf("cannot determine branch: %v", err)
	}
	if branch == "HEAD" {
		fatalf("detached HEAD -- check out a branch first")
	}

	if *oneshot {
		os.Exit(runOneshot(repoRoot))
	}

	notify(fmt.Sprintf("watching %s [%s]", filepath.Base(repoRoot), branch))

	commitMsgPath := filepath.Join(repoRoot, ".git", "COMMIT_EDITMSG")
	watcher, err := NewWatcher(commitMsgPath)
	if err != nil {
		fatalf("cannot start watcher: %v", err)
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

		// Cancel any in-flight workflow watcher for the previous commit.
		if cancelWorkflow != nil {
			cancelWorkflow()
			cancelWorkflow = nil
		}

		notify("pushing " + sha[:8] + "...")
		pushed, err := push(repoRoot)
		if err != nil {
			notify("push failed: " + err.Error())
			return
		}

		lastPushedSHA = sha
		if !pushed {
			notify("up to date")
		} else {
			notify("pushed " + sha[:8])
		}

		token := githubToken()
		if token == "" {
			notify("workflow check disabled: github token not set")
			return
		}
		remoteURL, err := getRemoteURL(repoRoot)
		if err != nil {
			notify("workflow check disabled: cannot read remote URL")
			return
		}
		owner, repo, err := parseGitHubOwnerRepo(remoteURL)
		if err != nil {
			notify("workflow check disabled: remote is not a GitHub repo")
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		cancelWorkflow = cancel
		go func() { watchWorkflows(ctx, owner, repo, sha, token, false) }()
	}

	doCheck() // check immediately on startup for any unpushed commits

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

func runOneshot(repoRoot string) int {
	sha, err := getCurrentSHA(repoRoot)
	if err != nil {
		notify("error: cannot get current SHA: " + err.Error())
		return 1
	}

	notify("pushing " + sha[:8] + "...")
	pushed, err := push(repoRoot)
	if err != nil {
		notify("push failed: " + err.Error())
		return 1
	}
	if pushed {
		notify("pushed " + sha[:8])
	} else {
		notify("up to date")
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

	return watchWorkflows(context.Background(), owner, repo, sha, token, true)
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
