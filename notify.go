package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	ansiRed   = "\033[31m"
	ansiGreen = "\033[32m"
	ansiReset = "\033[0m"
)

var titleState struct {
	mu    sync.Mutex
	repos []string
	fails map[string]bool
}

func initTmuxState(repos []string) {
	titleState.mu.Lock()
	defer titleState.mu.Unlock()
	titleState.repos = repos
	titleState.fails = make(map[string]bool, len(repos))
}

// setRepoStatus records the OK/FAIL result for a repo and updates the window
// title. Only call this at terminal points (push/CI settled), not for
// transient states like "pushing..." or "pulling...".
func setRepoStatus(repoName string, failed bool) {
	titleState.mu.Lock()
	defer titleState.mu.Unlock()
	if titleState.fails == nil {
		return
	}
	titleState.fails[repoName] = failed
	anyFailed := false
	for _, r := range titleState.repos {
		if titleState.fails[r] {
			anyFailed = true
			break
		}
	}
	state := "OK"
	if anyFailed {
		state = "FAIL"
	}
	first := titleState.repos[0]
	label := fmt.Sprintf("%s (%s)", state, first)
	if len(titleState.repos) > 1 {
		label = fmt.Sprintf("%s (%s+)", state, first)
	}
	setXTitle(label)
}

func colorLine(line, status string) string {
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return line
	}
	s := strings.ToLower(status)
	switch {
	case strings.Contains(s, "failed"):
		return ansiRed + line + ansiReset
	case strings.Contains(s, "passed"):
		return ansiGreen + line + ansiReset
	}
	return line
}

func notify(repoName, status string) {
	ts := time.Now().Format("15:04:05")
	line := fmt.Sprintf("(%s) [%s] %s", repoName, ts, status)
	fmt.Println(colorLine(line, status))
}

func setXTitle(title string) {
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return
	}
	fmt.Printf("\033]0;%s\007\033k%s\033\\", title, title)
}

func restoreTmuxRename() {
	if os.Getenv("TMUX") == "" {
		return
	}
	exec.Command("tmux", "set-window-option", "automatic-rename", "on").Run()
}

