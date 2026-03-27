package main

import (
	"fmt"
	"os"
	"os/exec"
	"time"
)

// notify prints a timestamped status line and updates the X window title and
// tmux status bar.
func notify(status string) {
	ts := time.Now().Format("15:04:05")
	fmt.Printf("[%s] %s\n", ts, status)
	setXTitle("autopush: " + status)
	setTmuxStatus(status)
}

// setXTitle updates the title of the current terminal window via the standard
// OSC escape sequence. No-ops when stdout is not a character device.
func setXTitle(title string) {
	fi, err := os.Stdout.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return
	}
	fmt.Printf("\033]0;%s\007", title)
}

// setTmuxStatus writes the status string to the tmux session variable
// @autopush. Add #{@autopush} to status-right in .tmux.conf to display it.
func setTmuxStatus(status string) {
	if os.Getenv("TMUX") == "" {
		return
	}
	exec.Command("tmux", "set-option", "-gq", "@autopush", status).Run()
}
