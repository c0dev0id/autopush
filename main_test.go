package main

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestExpandBundledFlags(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"single bool untouched", []string{"prog", "-r"}, []string{"prog", "-r"}},
		{"bundle arp", []string{"prog", "-arp"}, []string{"prog", "-a", "-r", "-p"}},
		{"bundle with interval", []string{"prog", "-rap60"}, []string{"prog", "-r", "-a", "-p=60"}},
		{"oneshot bundle", []string{"prog", "-1a"}, []string{"prog", "-1", "-a"}},
		{"p not last, no digits", []string{"prog", "-par"}, []string{"prog", "-p", "-a", "-r"}},
		{"equals form untouched", []string{"prog", "-p=30"}, []string{"prog", "-p=30"}},
		{"long flag untouched", []string{"prog", "--help"}, []string{"prog", "--help"}},
		{"unknown char passes through", []string{"prog", "-xyz"}, []string{"prog", "-xyz"}},
		{"misplaced digit passes through", []string{"prog", "-p6r"}, []string{"prog", "-p6r"}},
		{"path arg untouched", []string{"prog", "-r", "some/path"}, []string{"prog", "-r", "some/path"}},
		{"value flag untouched", []string{"prog", "-c", "make build"}, []string{"prog", "-c", "make build"}},
		{"value flag not bundleable", []string{"prog", "-rc", "make"}, []string{"prog", "-rc", "make"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := expandBundledFlags(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("expandBundledFlags(%v) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

func TestJoinPullInterval(t *testing.T) {
	cases := []struct {
		in, want []string
	}{
		{[]string{"prog", "-p", "30", "dir"}, []string{"prog", "-p=30", "dir"}},
		{[]string{"prog", "-a", "-r", "-p", "60"}, []string{"prog", "-a", "-r", "-p=60"}},
		{[]string{"prog", "-p", "0"}, []string{"prog", "-p=0"}},
		{[]string{"prog", "-p", "dir"}, []string{"prog", "-p", "dir"}},
		{[]string{"prog", "-p"}, []string{"prog", "-p"}},
		{[]string{"prog", "-c", "30"}, []string{"prog", "-c", "30"}},
	}
	for _, c := range cases {
		if got := joinPullInterval(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("joinPullInterval(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestParsePullInterval(t *testing.T) {
	cases := []struct {
		in   string
		want int
		ok   bool
	}{
		{"true", 0, true},
		{"30", 30, true},
		{"0", 0, false},
		{"-5", 0, false},
		{"x", 0, false},
		{"false", 0, false},
	}
	for _, c := range cases {
		got, err := parsePullInterval(c.in)
		if got != c.want || (err == nil) != c.ok {
			t.Errorf("parsePullInterval(%q) = %d, %v; want %d, ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
}

func TestGithubToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()

	if got := githubToken(repo); got != "" {
		t.Fatalf("no token anywhere: got %q, want empty", got)
	}

	// With HOME unset, a .gh_token in the working directory must not be used.
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	if err := os.WriteFile(filepath.Join(cwd, ".gh_token"), []byte("cwd-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(cwd); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(wd) })
	t.Setenv("HOME", "")
	if got := githubToken(repo); got != "" {
		t.Errorf("HOME unset: got %q, want empty", got)
	}
	t.Setenv("HOME", home)

	if err := os.WriteFile(filepath.Join(home, ".gh_token"), []byte("home-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := githubToken(repo); got != "home-token" {
		t.Errorf("HOME fallback: got %q, want home-token", got)
	}

	if err := os.WriteFile(filepath.Join(repo, ".gh_token"), []byte("repo-token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := githubToken(repo); got != "repo-token" {
		t.Errorf("repo root before HOME: got %q, want repo-token", got)
	}

	t.Setenv("GH_TOKEN", "env-token")
	if got := githubToken(repo); got != "env-token" {
		t.Errorf("environment before files: got %q, want env-token", got)
	}
}

func TestRunOnChange(t *testing.T) {
	cases := []struct {
		name, command string
		marker        string // content the command leaves in ./marker, "" for none
		output        string // expected in what autopush prints, "" for nothing
	}{
		{"runs in the repo root", "echo one > marker && echo two >> marker", "one\ntwo\n", "command ok"},
		{"failure is reported", "echo x > marker; exit 3", "x\n", "command failed: exit status 3"},
		{"empty command is a no-op", "", "", ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			out := captureStdout(t, func() { runOnChange(dir, "test", c.command) })
			if c.output == "" && out != "" || !strings.Contains(out, c.output) {
				t.Errorf("output = %q, want %q", out, c.output)
			}
			got, _ := os.ReadFile(filepath.Join(dir, "marker"))
			if string(got) != c.marker {
				t.Errorf("marker = %q, want %q", got, c.marker)
			}
		})
	}
}

// captureStdout returns what fn writes to os.Stdout, including the output of
// child processes that inherit it.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	fn()
	os.Stdout = orig
	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestSyncHead(t *testing.T) {
	isolateGit(t)
	bares := t.TempDir()
	a := mk(t, t.TempDir(), "a")
	bare := filepath.Join(bares, "a.git")
	track(t, a, bares, bare)
	branch, err := getCurrentBranch(a)
	if err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "other")
	run(t, "git", "clone", "-q", "-b", branch, bare, other)
	for _, dir := range []string{a, other} {
		git(t, dir, "config", "user.email", "t")
		git(t, dir, "config", "user.name", "t")
	}
	// Distinct messages: an empty commit made in the same second on the same
	// parent would otherwise be identical, SHA included, on both sides.
	commit := func(dir, msg string) { git(t, dir, "commit", "-q", "--allow-empty", "-m", msg) }
	remoteCommit := func() {
		git(t, other, "pull", "-q", "--rebase")
		commit(other, "remote")
		git(t, other, "push", "-q")
	}

	steps := []struct {
		name                             string
		setup                            func()
		doPull                           bool
		wantChanged, wantPushed, wantErr bool
	}{
		{"nothing new", func() {}, false, false, false, false},
		{"local commit", func() { commit(a, "local") }, false, false, true, false},
		{"remote ahead, -p", remoteCommit, true, true, false, false},
		{"remote ahead and local commit, no -p", func() { remoteCommit(); commit(a, "local") }, false, false, false, true},
		{"same, retried with -p", func() {}, true, true, true, false},
	}
	for _, s := range steps {
		s.setup()
		var sha string
		var changed, pushed bool
		captureStdout(t, func() { sha, changed, pushed, err = syncHead(a, "test", s.doPull) })
		if changed != s.wantChanged || pushed != s.wantPushed || (err != nil) != s.wantErr {
			t.Fatalf("%s: changed=%v pushed=%v err=%v; want changed=%v pushed=%v err=%v",
				s.name, changed, pushed, err, s.wantChanged, s.wantPushed, s.wantErr)
		}
		if head, _ := getCurrentSHA(a); err == nil && sha != head {
			t.Errorf("%s: sha = %s, want HEAD %s", s.name, sha, head)
		}
	}
}
