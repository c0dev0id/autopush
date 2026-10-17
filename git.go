package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// gitCmd runs a git command with -C repoDir and returns trimmed combined output.
func gitCmd(repoDir string, args ...string) (string, error) {
	all := append([]string{"-C", repoDir}, args...)
	out, err := exec.Command("git", all...).CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func getGitDir(repoDir string) (string, error) {
	return gitCmd(repoDir, "rev-parse", "--absolute-git-dir")
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

func isWorkspaceClean(repoDir string) bool {
	out, err := gitCmd(repoDir, "status", "--porcelain")
	return err == nil && out == ""
}

// pull runs git pull --rebase and returns any error.
func pull(repoDir string) error {
	out, err := exec.Command("git", "-C", repoDir, "pull", "--rebase").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", strings.TrimSpace(string(out)))
	}
	return nil
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

// hasWorkflows reports whether the repo contains any GitHub Actions workflow files.
func hasWorkflows(repoDir string) bool {
	for _, pattern := range []string{"*.yml", "*.yaml"} {
		matches, err := filepath.Glob(filepath.Join(repoDir, ".github", "workflows", pattern))
		if err == nil && len(matches) > 0 {
			return true
		}
	}
	return false
}

// listSubmodules returns the absolute paths of all submodules (recursive).
func listSubmodules(repoDir string) ([]string, error) {
	out, err := gitCmd(repoDir, "submodule", "foreach", "--recursive", "--quiet", "echo $displaypath")
	if err != nil || out == "" {
		return nil, err
	}
	var paths []string
	for _, rel := range strings.Split(out, "\n") {
		rel = strings.TrimSpace(rel)
		if rel != "" {
			paths = append(paths, filepath.Join(repoDir, rel))
		}
	}
	return paths, nil
}

func getRemoteURL(repoDir string) (string, error) {
	return remoteURL(repoDir, "origin")
}

func remoteURL(repoDir, name string) (string, error) {
	return gitCmd(repoDir, "remote", "get-url", name)
}

// getBranchRemote returns the remote the given branch tracks, or "" if the
// branch has no upstream configured.
func getBranchRemote(repoDir, branch string) (string, error) {
	return gitCmd(repoDir, "config", "--get", "branch."+branch+".remote")
}

// isSSHRemote reports whether url is an SSH remote. HTTPS remotes are excluded
// because they usually cannot be pushed without extra credential handling.
func isSSHRemote(url string) bool {
	return strings.HasPrefix(url, "git@") || strings.HasPrefix(url, "ssh://")
}

// repoUnusableReason validates that a repository can be pulled and pushed by
// autopush without manual intervention. It returns an empty string when the
// repo is usable, otherwise a human-readable reason to warn about and skip.
// Transient conditions (e.g. a dirty working tree) are not checked here: those
// resolve on their own and the repo should still be watched.
func repoUnusableReason(repoDir string) string {
	if _, err := getRepoRoot(repoDir); err != nil {
		return "not a usable git repository"
	}
	branch, err := getCurrentBranch(repoDir)
	if err != nil || branch == "HEAD" {
		return "detached HEAD (not on a branch)"
	}
	remote, err := getBranchRemote(repoDir, branch)
	if err != nil || remote == "" {
		return "no upstream tracking branch set for " + branch
	}
	if !upstreamRefExists(repoDir) {
		return "upstream for " + branch + " not fetched (remote-tracking ref missing)"
	}
	url, err := remoteURL(repoDir, remote)
	if err != nil || url == "" {
		return "tracking remote '" + remote + "' has no URL"
	}
	if !isSSHRemote(url) {
		return "tracking remote '" + remote + "' is not ssh (" + url + ")"
	}
	return ""
}

// upstreamRefExists reports whether the current branch's @{upstream} resolves
// to a commit. It is false when tracking is configured but the remote-tracking
// ref was never fetched or has been deleted on the remote.
func upstreamRefExists(repoDir string) bool {
	out, err := gitCmd(repoDir, "rev-parse", "--verify", "--quiet", "@{upstream}")
	return err == nil && out != ""
}

// reposInDir returns the immediate subdirectories of dir that are git
// repositories usable by autopush, without recursing. A directory without a
// .git entry is not a repository and is skipped silently. A directory that is
// a repository but cannot be cleanly pulled and pushed (detached HEAD, no
// upstream, non-ssh remote, ...) is skipped with a warning explaining why.
func reposInDir(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var repos []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if _, err := os.Stat(filepath.Join(path, ".git")); err != nil {
			continue
		}
		if reason := repoUnusableReason(path); reason != "" {
			notify(e.Name(), "skipped (-a): "+reason)
			continue
		}
		repos = append(repos, path)
	}
	return repos, nil
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
