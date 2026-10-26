//go:build integration

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// These tests drive autopush against a real private GitHub repository and the
// live GitHub Actions API. They push commits, trigger workflows, and wait for
// CI to settle, so they are slow and require network access and a token. They
// are excluded from the normal build by the integration tag; run them with
// `make integration`.
//
// The target repository is reset hard to its initial (root) commit before the
// suite starts, before every scenario, and once more on cleanup, so an aborted
// run that leaves the remote dirty cannot break the next run.

const defaultTestRepo = "git@github.com:c0dev0id/autopush-test.git"

const (
	testUserName  = "autopush-integration"
	testUserEmail = "claude@textmail.me"
)

const passingWorkflow = `name: passing
on:
  push:
    branches: ["**"]
jobs:
  ok:
    runs-on: ubuntu-latest
    steps:
      - run: echo ok
`

const failingWorkflow = `name: failing
on:
  push:
    branches: ["**"]
jobs:
  boom:
    runs-on: ubuntu-latest
    steps:
      - run: exit 1
`

type harness struct {
	t          *testing.T
	bin        string
	repoURL    string
	dir        string
	branch     string
	initialSHA string
	token      string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	token := githubToken(".")
	if token == "" {
		t.Skip("no github token; set GITHUB_TOKEN/GH_TOKEN or provide a .gh_token")
	}
	repoURL := os.Getenv("AUTOPUSH_TEST_REPO")
	if repoURL == "" {
		repoURL = defaultTestRepo
	}

	root := t.TempDir()
	bin := filepath.Join(root, "autopush")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build autopush: %v\n%s", err, out)
	}

	dir := filepath.Join(root, "repo")
	if out, err := exec.Command("git", "clone", repoURL, dir).CombinedOutput(); err != nil {
		t.Fatalf("clone %s: %v\n%s", repoURL, err, out)
	}

	h := &harness{t: t, bin: bin, repoURL: repoURL, dir: dir, token: token}
	configIdentity(t, dir)
	h.branch = h.git("rev-parse", "--abbrev-ref", "HEAD")
	roots := strings.Fields(h.git("rev-list", "--max-parents=0", "HEAD"))
	if len(roots) != 1 {
		t.Fatalf("expected a single root commit, got %d: %v", len(roots), roots)
	}
	h.initialSHA = roots[0]

	h.resetRemote()
	t.Cleanup(h.resetRemote)
	return h
}

// mustGit runs a git command in dir and fails the test on error.
func mustGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := gitCmd(dir, args...)
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return out
}

func configIdentity(t *testing.T, dir string) {
	t.Helper()
	mustGit(t, dir, "config", "user.name", testUserName)
	mustGit(t, dir, "config", "user.email", testUserEmail)
}

func (h *harness) git(args ...string) string {
	h.t.Helper()
	return mustGit(h.t, h.dir, args...)
}

// resetRemote returns both the working clone and the remote branch to the
// initial commit, discarding any commits and files a previous run left behind.
func (h *harness) resetRemote() {
	h.t.Helper()
	h.git("checkout", h.branch)
	h.git("reset", "--hard", h.initialSHA)
	h.git("clean", "-fdxq")
	h.git("push", "--force", "origin", h.branch)
}

func (h *harness) commitWorkflow(name, content string) {
	h.t.Helper()
	wfDir := filepath.Join(h.dir, ".github", "workflows")
	if err := os.MkdirAll(wfDir, 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wfDir, name), []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	h.git("add", "-A")
	h.git("commit", "-q", "-m", "integration: add "+name)
}

// commitOnRemote clones the repo separately, adds a commit, and pushes it, so
// the main working clone becomes behind the remote without knowing about it.
func (h *harness) commitOnRemote() {
	h.t.Helper()
	tmp := h.t.TempDir()
	if out, err := exec.Command("git", "clone", h.repoURL, tmp).CombinedOutput(); err != nil {
		h.t.Fatalf("clone for remote commit: %v\n%s", err, out)
	}
	configIdentity(h.t, tmp)
	if err := os.WriteFile(filepath.Join(tmp, "remote.txt"), []byte(time.Now().String()), 0o644); err != nil {
		h.t.Fatal(err)
	}
	mustGit(h.t, tmp, "add", "-A")
	mustGit(h.t, tmp, "commit", "-q", "-m", "integration: remote-ahead commit")
	mustGit(h.t, tmp, "push", "origin", h.branch)
}

func (h *harness) runAutopush(args ...string) (string, int) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, h.bin, append(args, h.dir)...)
	cmd.Env = append(os.Environ(), "GITHUB_TOKEN="+h.token)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	out := buf.String()
	h.t.Logf("autopush %v:\n%s", args, out)

	var ee *exec.ExitError
	switch {
	case err == nil:
		return out, 0
	case errors.As(err, &ee):
		return out, ee.ExitCode()
	default:
		h.t.Fatalf("run autopush %v: %v", args, err)
		return out, -1
	}
}

func TestIntegration(t *testing.T) {
	h := newHarness(t)

	t.Run("passing workflow", func(t *testing.T) {
		h.resetRemote()
		h.commitWorkflow("passing.yml", passingWorkflow)
		out, code := h.runAutopush("-1")
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
		if !strings.Contains(out, "CI passed") {
			t.Errorf("output missing %q:\n%s", "CI passed", out)
		}
	})

	t.Run("failing workflow", func(t *testing.T) {
		h.resetRemote()
		h.commitWorkflow("failing.yml", failingWorkflow)
		out, code := h.runAutopush("-1")
		if code != 2 {
			t.Errorf("exit code = %d, want 2", code)
		}
		if !strings.Contains(out, "FAILED") {
			t.Errorf("output missing %q:\n%s", "FAILED", out)
		}
	})

	t.Run("remote ahead resolved with -p", func(t *testing.T) {
		h.resetRemote()
		h.commitOnRemote()

		if err := os.WriteFile(filepath.Join(h.dir, "local.txt"), []byte("local"), 0o644); err != nil {
			t.Fatal(err)
		}
		h.git("add", "-A")
		h.git("commit", "-q", "-m", "integration: local commit")

		out, code := h.runAutopush("-1", "-p")
		if code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
		if !strings.Contains(out, "pushed") {
			t.Errorf("expected a push after rebase:\n%s", out)
		}
	})
}
