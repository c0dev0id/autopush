package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestIsSSHRemote(t *testing.T) {
	cases := map[string]bool{
		"git@github.com:me/x.git":       true,
		"ssh://git@github.com/me/x.git": true,
		"https://github.com/me/x.git":   false,
		"http://example.com/x.git":      false,
		"git://github.com/me/x.git":     false,
		"":                              false,
	}
	for url, want := range cases {
		if got := isSSHRemote(url); got != want {
			t.Errorf("isSSHRemote(%q) = %v, want %v", url, got, want)
		}
	}
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	all := append([]string{"-C", dir}, args...)
	if out, err := exec.Command("git", all...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// initRepo creates a repo with one commit and returns the current branch name.
func initRepo(t *testing.T, dir string) string {
	t.Helper()
	git(t, dir, "init", "-q")
	git(t, dir, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "init")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--abbrev-ref", "HEAD").CombinedOutput()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return string(out[:len(out)-1])
}

func run(t *testing.T, name string, args ...string) {
	t.Helper()
	if out, err := exec.Command(name, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v\n%s", name, args, err, out)
	}
}

// track gives dir a real upstream by pushing to a bare repo under bareParent,
// so @{upstream} resolves, then rewrites the origin URL to finalURL to exercise
// the ssh/non-ssh classification independently of the push target.
func track(t *testing.T, dir, bareParent, finalURL string) {
	t.Helper()
	b := initRepo(t, dir)
	bare := filepath.Join(bareParent, filepath.Base(dir)+".git")
	run(t, "git", "init", "-q", "--bare", bare)
	git(t, dir, "remote", "add", "origin", bare)
	git(t, dir, "push", "-q", "-u", "origin", b)
	git(t, dir, "remote", "set-url", "origin", finalURL)
}

// mk creates an empty subdirectory of parent and returns its path.
func mk(t *testing.T, parent, name string) string {
	t.Helper()
	p := filepath.Join(parent, name)
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReposInDir(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	root := t.TempDir()
	bares := t.TempDir()

	track(t, mk(t, root, "ok_ssh"), bares, "git@github.com:me/ok_ssh.git")
	track(t, mk(t, root, "ok_sshurl"), bares, "ssh://git@github.com/me/ok_sshurl.git")
	track(t, mk(t, root, "https_only"), bares, "https://github.com/me/https_only.git")

	// tracking configured but never fetched -> @{upstream} does not resolve
	noFetch := mk(t, root, "no_fetch")
	b := initRepo(t, noFetch)
	git(t, noFetch, "remote", "add", "origin", "git@github.com:me/no_fetch.git")
	git(t, noFetch, "config", "branch."+b+".remote", "origin")
	git(t, noFetch, "config", "branch."+b+".merge", "refs/heads/"+b)

	noTrack := mk(t, root, "no_track")
	initRepo(t, noTrack)
	git(t, noTrack, "remote", "add", "origin", "git@github.com:me/no_track.git")

	detached := mk(t, root, "detached")
	initRepo(t, detached)
	git(t, detached, "checkout", "-q", "--detach", "HEAD")

	mk(t, root, "plaindir") // no .git

	got, err := reposInDir(root)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, len(got))
	for i, p := range got {
		names[i] = filepath.Base(p)
	}
	sort.Strings(names)
	want := []string{"ok_ssh", "ok_sshurl"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("reposInDir included %v, want %v", names, want)
	}
}

func TestPullReportsMovedHEAD(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	bares := t.TempDir()
	a := mk(t, t.TempDir(), "a")
	bare := filepath.Join(bares, "a.git")
	track(t, a, bares, bare)
	branch, err := getCurrentBranch(a)
	if err != nil {
		t.Fatal(err)
	}

	b := filepath.Join(t.TempDir(), "b")
	run(t, "git", "clone", "-q", "-b", branch, bare, b)
	git(t, b, "-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "remote")
	git(t, b, "push", "-q", "origin", branch)

	changed, err := pull(a)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("remote ahead: changed = false, want true")
	}

	changed, err = pull(a)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("nothing new: changed = true, want false")
	}
}
