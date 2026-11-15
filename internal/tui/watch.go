package tui

import (
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// settle is how long the files must stay quiet before a change is reported.
// One save is often several writes.
const settle = 100 * time.Millisecond

// watch reports on the returned channel whenever one of files is written,
// created or replaced. It watches each file's directory, not the file: most
// editors save by renaming a new file over the old one, and a watch on the
// old file would not outlive it.
func watch(files []string) (changed <-chan struct{}, stop func(), err error) {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, nil, err
	}
	watched := make(map[string]bool, len(files))
	for _, file := range files {
		abs, err := filepath.Abs(file)
		if err == nil {
			err = w.Add(filepath.Dir(abs))
		}
		if err != nil {
			_ = w.Close()
			return nil, nil, err
		}
		watched[abs] = true
	}

	out := make(chan struct{}, 1)
	notify := func() {
		select {
		case out <- struct{}{}:
		default: // a change is already waiting to be picked up
		}
	}
	go func() {
		var quiet *time.Timer
		for {
			select {
			case event, ok := <-w.Events:
				switch {
				case !ok:
					return
				case !watched[event.Name] || event.Op == fsnotify.Chmod:
				case quiet == nil:
					quiet = time.AfterFunc(settle, notify)
				default:
					quiet.Reset(settle)
				}
			case _, ok := <-w.Errors:
				// Drained so the watcher never blocks; a missed event costs
				// one press of `r`.
				if !ok {
					return
				}
			}
		}
	}()
	return out, func() { _ = w.Close() }, nil
}
