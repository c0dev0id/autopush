package main

import (
	"time"

	"golang.org/x/sys/unix"
)

// Watcher watches a single file for changes using kqueue(2). C receives a
// signal whenever the file is written, renamed, or deleted (and re-appears).
type Watcher struct {
	kq   int
	path string
	fd   int // open fd registered with kqueue; -1 when not watching
	C    chan struct{}
}

// NewWatcher creates a kqueue watcher for path and starts the event loop.
func NewWatcher(path, repoName string) (*Watcher, error) {
	kq, err := unix.Kqueue()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		kq:   kq,
		path: path,
		fd:   -1,
		C:    make(chan struct{}, 1),
	}
	if !w.addFile() {
		notify(repoName, "COMMIT_EDITMSG not found, falling back to polling")
	}
	go w.loop()
	return w, nil
}

// addFile opens the watched path and registers it with the kqueue.
// Returns true if the file was successfully registered.
func (w *Watcher) addFile() bool {
	fd, err := unix.Open(w.path, unix.O_RDONLY|unix.O_NONBLOCK, 0)
	if err != nil {
		return false
	}
	var ev unix.Kevent_t
	unix.SetKevent(&ev, fd, unix.EVFILT_VNODE, unix.EV_ADD|unix.EV_CLEAR|unix.EV_ENABLE)
	ev.Fflags = unix.NOTE_WRITE | unix.NOTE_DELETE | unix.NOTE_RENAME | unix.NOTE_ATTRIB
	if _, err := unix.Kevent(w.kq, []unix.Kevent_t{ev}, nil, nil); err != nil {
		unix.Close(fd)
		return false
	}
	w.fd = fd
	return true
}

func (w *Watcher) loop() {
	events := make([]unix.Kevent_t, 8)
	for {
		n, err := unix.Kevent(w.kq, nil, events, nil)
		if err != nil {
			time.Sleep(500 * time.Millisecond)
			continue
		}

		needRewatch := false
		for i := range n {
			if events[i].Fflags&(unix.NOTE_DELETE|unix.NOTE_RENAME) != 0 {
				needRewatch = true
			}
		}

		select {
		case w.C <- struct{}{}:
		default:
		}

		if needRewatch {
			if w.fd >= 0 {
				unix.Close(w.fd)
				w.fd = -1
			}
			// Brief pause: let git finish renaming the temp file into place
			time.Sleep(200 * time.Millisecond)
			w.addFile()
		}
	}
}
