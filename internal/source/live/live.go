// Package live reads every source of one project into a deck.Snapshot and
// watches the project folder for changes.
package live

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"time"

	"github.com/FredricW/herdr-deck/internal/deck"
	"github.com/FredricW/herdr-deck/internal/source/board"
	"github.com/FredricW/herdr-deck/internal/source/dev"
	"github.com/FredricW/herdr-deck/internal/source/github"
	"github.com/FredricW/herdr-deck/internal/source/herdr"
	"github.com/FredricW/herdr-deck/internal/source/linear"
	"github.com/FredricW/herdr-deck/internal/source/projects"
	"github.com/FredricW/herdr-deck/internal/source/tasks"
)

// Source reads one project: herdr-projects' files and thread list through
// Projects, and TASKS.md through the tasks parser.
type Source struct {
	Projects projects.Reader
	// LinearWorkspace is the slug bare Linear IDs link into. Empty leaves
	// them without a URL, and Read names that in Snapshot.Missing, unless
	// Linear has a key and gives the URLs itself.
	LinearWorkspace string
	// Herdr lays herdr's live panes and agent states over the threads; nil
	// leaves the deck on herdr-projects' data alone.
	Herdr *herdr.Reader
	// Dev reads each worktree's dev servers and probes their ports; nil
	// leaves threads without them.
	Dev *dev.Reader
	// Linear adds each Linear link's issue status; nil leaves IDs without
	// one.
	Linear *linear.Reader
	// GitHub reads thread PRs from GitHub through gh, beyond the ticker's
	// summary, and the user's own open PRs for the GitHub views; nil
	// leaves PRs as herdr-projects' ticker saw them.
	GitHub *github.Reader
	// Roster reads every project under the projects root for the project
	// picker; nil leaves Snapshot.Projects empty.
	Roster *projects.Roster
	// Board, when set, reads a herdr-projects 1.x board in place of the
	// 0.2 project folder (Projects) and its TASKS.md.
	Board *board.Reader
	// Now stamps Snapshot.ReadAt; nil means time.Now.
	Now func() time.Time
}

// New returns a Source for the project slug under root.
func New(root, slug, linearWorkspace string) Source {
	return Source{Projects: projects.New(root, slug), LinearWorkspace: linearWorkspace}
}

// Read returns what can be read of the project now. Like projects.Reader.Read
// it never fails: unreadable sources are named in Snapshot.Missing. A missing
// TASKS.md is not a missing source; the project simply has no tasks yet.
func (s Source) Read(ctx context.Context) deck.Snapshot {
	if s.Board != nil {
		return s.readBoard(ctx)
	}
	snap := s.Projects.Read(ctx)
	dir := s.Projects.Dir()
	threadText := tasks.ThreadFiles(filepath.Join(dir, "threads"))

	f, err := tasks.Load(dir, tasks.Options{LinearWorkspace: s.LinearWorkspace, ThreadText: threadText})
	switch {
	case err == nil:
		snap.TaskLists = f.Lists
	case !errors.Is(err, fs.ErrNotExist):
		snap.Missing = append(snap.Missing, "TASKS.md: "+short(err).Error())
	}

	// A thread's own brief and report give its links too, so a thread no
	// task names still offers them.
	for i := range snap.Threads {
		t := &snap.Threads[i]
		for _, l := range tasks.ScrapeAll(threadText(t.ID), s.LinearWorkspace) {
			t.Links = deck.AppendLink(t.Links, l)
		}
	}

	if s.Linear != nil {
		s.Linear.Apply(&snap)
		s.Linear.ApplyMine(&snap)
	}
	// With a key, Linear gives the URLs; until its first answer arrives
	// the IDs wait for them rather than missing a workspace.
	if s.LinearWorkspace == "" && !snap.LinearKey {
		if id := unlinkedLinearID(snap); id != "" {
			snap.Missing = append(snap.Missing, "Linear: no workspace set, so "+id+" and other bare IDs cannot open; set linear.workspace in the config file, --linear-workspace or $"+deck.EnvLinearWorkspace+", or a Linear API key")
		}
	}

	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	snap.ReadAt = now()
	if s.Herdr != nil {
		s.Herdr.Apply(ctx, &snap, snap.ReadAt)
	}
	if s.GitHub != nil {
		// After herdr, which may resolve a thread: those are not read.
		s.GitHub.Apply(&snap)
		s.GitHub.ApplyMine(&snap)
	}
	if s.Dev != nil {
		// After herdr: its workspace port token is the fallback port.
		s.Dev.Apply(ctx, &snap)
	}
	if s.Roster != nil {
		snap.Projects = s.Roster.Read(snap)
		if s.Herdr != nil {
			s.Herdr.ApplyProjects(snap.Projects, snap.Project.Slug, snap.ReadAt)
		}
	}
	return snap
}

// readBoard reads the herdr-projects 1.x board and lays the live sources
// over it as Read does for a 0.2 project.
func (s Source) readBoard(ctx context.Context) deck.Snapshot {
	b := *s.Board
	b.LinearWorkspace = s.LinearWorkspace
	snap := b.Read(ctx)
	if s.Linear != nil {
		s.Linear.Apply(&snap)
		s.Linear.ApplyMine(&snap)
	}
	now := time.Now
	if s.Now != nil {
		now = s.Now
	}
	snap.ReadAt = now()
	if s.Herdr != nil {
		s.Herdr.Apply(ctx, &snap, snap.ReadAt)
	}
	if s.GitHub != nil {
		s.GitHub.Apply(&snap)
		s.GitHub.ApplyMine(&snap)
	}
	if s.Dev != nil {
		s.Dev.Apply(ctx, &snap)
	}
	return snap
}

// unlinkedLinearID returns the first Linear ID in snap that has no URL
// anywhere in it, or "".
func unlinkedLinearID(snap deck.Snapshot) string {
	var all []deck.Link
	for _, list := range snap.TaskLists {
		for _, t := range list.Tasks {
			all = append(all, t.Links...)
		}
	}
	for _, t := range snap.Threads {
		all = append(all, t.Links...)
	}
	linked := map[string]bool{}
	for _, l := range all {
		if l.Kind == deck.LinkLinear && l.URL != "" {
			linked[l.Label] = true
		}
	}
	for _, l := range all {
		if l.Kind == deck.LinkLinear && l.URL == "" && !linked[l.Label] {
			return l.Label
		}
	}
	return ""
}

// short drops the path from a file error; the note already names the file.
func short(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}
