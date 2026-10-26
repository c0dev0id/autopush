package main

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestGithubToken(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_TOKEN", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	repo := t.TempDir()

	if got := githubToken(repo); got != "" {
		t.Fatalf("no token anywhere: got %q, want empty", got)
	}

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
	dir := t.TempDir()
	runOnChange(dir, "test", "echo one > marker && echo two >> marker")
	got, err := os.ReadFile(filepath.Join(dir, "marker"))
	if err != nil {
		t.Fatalf("command did not run in the repo root: %v", err)
	}
	if string(got) != "one\ntwo\n" {
		t.Errorf("marker = %q, want %q", got, "one\ntwo\n")
	}
}
