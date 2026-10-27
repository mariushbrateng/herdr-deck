package live

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/fsnotify/fsnotify"
)

// boardFiles are the files of a herdr-projects 1.x task folder whose change
// is a change on the board. Its logs change all the time and say nothing
// the deck shows.
var boardFiles = map[string]bool{"task.json": true, "closing-note.md": true}

// WatchBoard calls changed once after a task of the herdr-projects 1.x
// board in tasksDir (<home>/data/tasks) changes, as Watch does for a 0.2
// project folder. New repos and tasks are watched when they appear.
func WatchBoard(ctx context.Context, tasksDir string, debounce time.Duration, changed func()) error {
	w, err := fsnotify.NewWatcher()
	if err != nil {
		return err
	}
	if err := w.Add(tasksDir); err != nil {
		w.Close()
		return err
	}
	repos, _ := os.ReadDir(tasksDir)
	for _, repo := range repos {
		if !repo.IsDir() {
			continue
		}
		dir := filepath.Join(tasksDir, repo.Name())
		_ = w.Add(dir)
		tasks, _ := os.ReadDir(dir)
		for _, t := range tasks {
			if t.IsDir() {
				_ = w.Add(filepath.Join(dir, t.Name()))
			}
		}
	}
	depth := func(name string) int {
		rel, err := filepath.Rel(tasksDir, name)
		if err != nil {
			return -1
		}
		n := 1
		for _, c := range rel {
			if c == filepath.Separator {
				n++
			}
		}
		return n
	}

	go func() {
		defer w.Close()
		timer := time.NewTimer(debounce)
		timer.Stop()
		for {
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case ev, ok := <-w.Events:
				if !ok {
					return
				}
				d := depth(ev.Name)
				if ev.Has(fsnotify.Create) && (d == 1 || d == 2) {
					// A new repo or task folder.
					if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
						_ = w.Add(ev.Name)
					}
					timer.Reset(debounce)
					continue
				}
				if d == 3 && boardFiles[filepath.Base(ev.Name)] || ev.Has(fsnotify.Remove) && d <= 2 {
					timer.Reset(debounce)
				}
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
			case <-timer.C:
				changed()
			}
		}
	}()
	return nil
}
