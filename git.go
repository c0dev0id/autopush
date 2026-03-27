package main

import (
	"fmt"
	"os/exec"
	"strings"
)

// gitCmd runs a git command with -C repoDir and returns trimmed combined output.
func gitCmd(repoDir string, args ...string) (string, error) {
	all := append([]string{"-C", repoDir}, args...)
	out, err := exec.Command("git", all...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func getCurrentSHA(repoDir string) (string, error) {
	sha, err := gitCmd(repoDir, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("rev-parse HEAD: %v", err)
	}
	if len(sha) < 8 {
		return "", fmt.Errorf("unexpected SHA %q", sha)
	}
	return sha, nil
}

func getCurrentBranch(repoDir string) (string, error) {
	return gitCmd(repoDir, "rev-parse", "--abbrev-ref", "HEAD")
}

func getRepoRoot(dir string) (string, error) {
	return gitCmd(dir, "rev-parse", "--show-toplevel")
}

// push runs git push and reports whether anything was actually pushed.
// Returns (false, nil) when the remote is already up to date.
func push(repoDir string) (pushed bool, err error) {
	out, cmdErr := exec.Command("git", "-C", repoDir, "push").CombinedOutput()
	output := strings.TrimSpace(string(out))
	if cmdErr != nil {
		return false, fmt.Errorf("%s", output)
	}
	upToDate := strings.Contains(strings.ToLower(output), "everything up")
	return !upToDate, nil
}

func getRemoteURL(repoDir string) (string, error) {
	return gitCmd(repoDir, "remote", "get-url", "origin")
}

// parseGitHubOwnerRepo extracts the owner and repo name from an https or ssh
// GitHub remote URL.
func parseGitHubOwnerRepo(remoteURL string) (owner, repo string, err error) {
	s := strings.TrimSuffix(remoteURL, ".git")
	if after, ok := strings.CutPrefix(s, "https://github.com/"); ok {
		parts := strings.SplitN(after, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], nil
		}
	}
	if after, ok := strings.CutPrefix(s, "git@github.com:"); ok {
		parts := strings.SplitN(after, "/", 2)
		if len(parts) == 2 {
			return parts[0], parts[1], nil
		}
	}
	return "", "", fmt.Errorf("unrecognised GitHub remote URL: %q", remoteURL)
}
