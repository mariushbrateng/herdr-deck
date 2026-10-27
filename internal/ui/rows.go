package ui

import (
	"sort"
	"strings"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// rowKind says what a list row shows.
type rowKind int

const (
	rowHeading rowKind = iota // a list heading, or the pinned Needs you group
	rowWork                   // a task with its threads, or a thread no task names
	rowGap                    // the blank line between two groups
)

// The lists the deck makes itself, next to TASKS.md's own.
const (
	listNeedsYou = "Needs you"
	listThreads  = "Other threads"
	listResolved = "Resolved"
)

// row is one line of the list. Gaps are rows too, so a row index is always a
// line of the list: scrolling and clicks need no other mapping.
type row struct {
	kind rowKind
	key  string // identifies the row across refreshes
	list string // the list the row is in, or the heading's own name

	// Headings.
	count    int                 // rows in the list
	folded   bool                // the list's rows are hidden
	foldable bool                // Needs you never folds
	glyphs   []deck.ThreadStatus // a folded list's open threads

	// Work rows. task is nil for a thread no task names. issue is set on
	// the Linear view's rows, whose threads are the board tasks whose
	// title starts with its ID, the one it shows first.
	issue *deck.LinearIssue
	// pull is set on the GitHub views' rows, with their links: the PR,
	// and the Linear issue its title names.
	pull    *deck.MyPR
	links   []deck.Link
	task    *deck.Task
	threads []deck.Thread
	// updates counts the unhandled inbox items about the row's threads
	// (on a folded heading, its rows'): they tint the row yellow.
	updates int
}

// title is the row's text in the WORK column and the drawer's title.
func (r row) title() string {
	switch {
	case r.kind == rowHeading:
		return r.list
	case r.kind == rowGap:
		return ""
	case r.issue != nil:
		return r.issue.Title
	case r.pull != nil:
		return r.pull.Title
	case r.task != nil:
		return r.task.Title
	case len(r.threads) > 0:
		t := r.threads[0]
		if t.Title != "" {
			return t.Title
		}
		return t.ID
	}
	return ""
}

// thread is the row's most pressing thread: the one whose state the row
// shows.
func (r row) thread() (deck.Thread, bool) {
	if len(r.threads) == 0 {
		return deck.Thread{}, false
	}
	best := r.threads[0]
	if r.issue != nil {
		return best, true
	}
	for _, t := range r.threads[1:] {
		if urgency(t.Status) < urgency(best.Status) {
			best = t
		}
	}
	return best, true
}

// pr is the row's PR, the one its PR column, PR tab and Overview show:
// its most pressing thread's, else another thread's, else the PR its
// task links (own is false then). The GitHub reader follows a task's PR
// only when no thread of the task has one, the same rule; only
// herdr-projects' status ticker follows thread PRs, so a task's PR shows
// no failing-checks mark in the list.
func (r row) pr() (pr *deck.PullRequest, own, ok bool) {
	if r.kind != rowWork {
		return nil, false, false
	}
	if r.pull != nil {
		return &r.pull.PR, true, true
	}
	if t, has := r.thread(); has && t.PR != nil {
		return t.PR, true, true
	}
	for _, t := range r.threads {
		if t.PR != nil {
			return t.PR, true, true
		}
	}
	if r.task != nil && r.task.PR != nil {
		return r.task.PR, false, true
	}
	return nil, false, false
}

// needsYou reports whether a thread of the row waits on the user: what
// pins it under Needs you. Inbox items never do; they tint the row.
func (r row) needsYou() bool {
	for _, t := range r.threads {
		if t.Status == deck.StatusNeedsYou {
			return true
		}
	}
	return false
}

// urgency orders statuses for picking a row's glyph: lower comes first.
func urgency(s deck.ThreadStatus) int {
	switch s {
	case deck.StatusNeedsYou:
		return 0
	case deck.StatusWorking:
		return 1
	case deck.StatusReview:
		return 2
	case deck.StatusUnknown:
		return 3
	}
	return 4
}

// startsFolded says whether list is one of the headings in folded (the
// ui.folded_lists setting), ignoring case.
func startsFolded(list string, folded []string) bool {
	for _, f := range folded {
		if strings.EqualFold(strings.TrimSpace(f), list) {
			return true
		}
	}
	return false
}

// inboxByThread counts the unhandled inbox items per thread id. Items
// about no thread (routines, spaces) count only in the header.
func inboxByThread(snap deck.Snapshot) map[string]int {
	n := map[string]int{}
	for _, it := range snap.Inbox {
		if it.Thread != "" {
			n[it.Thread]++
		}
	}
	return n
}

// buildRows lays out the list: the Needs you group (threads waiting on the
// user) pinned on top, then TASKS.md's lists in
// order, then the threads no task names, with a gap between two groups.
// folds holds the user's fold toggles by list name; lists without one start
// folded when folded names them.
func buildRows(snap deck.Snapshot, folds map[string]bool, folded []string) []row {
	byID := make(map[string]deck.Thread, len(snap.Threads))
	for _, t := range snap.Threads {
		byID[t.ID] = t
	}
	updates := inboxByThread(snap)

	type list struct {
		name string
		rows []row
	}
	var lists []list
	named := map[string]bool{}
	for li := range snap.TaskLists {
		tl := &snap.TaskLists[li]
		name := tl.Name
		if name == "" {
			name = "Tasks"
		}
		l := list{name: name}
		for ti := range tl.Tasks {
			task := &tl.Tasks[ti]
			r := row{kind: rowWork, list: name, task: task, key: "task:" + name + "/" + task.Title}
			for _, id := range task.Threads {
				named[id] = true
				if t, ok := byID[id]; ok {
					r.threads = append(r.threads, t)
					r.updates += updates[id]
				}
			}
			l.rows = append(l.rows, r)
		}
		lists = append(lists, l)
	}

	open, resolved := list{name: listThreads}, list{name: listResolved}
	if len(lists) == 0 {
		open.name = "Threads"
	}
	for _, t := range snap.Threads {
		if named[t.ID] {
			continue
		}
		r := row{kind: rowWork, threads: []deck.Thread{t}, key: "thread:" + t.ID, updates: updates[t.ID]}
		if t.Status == deck.StatusDone {
			r.list = resolved.name
			resolved.rows = append(resolved.rows, r)
		} else {
			r.list = open.name
			open.rows = append(open.rows, r)
		}
	}
	for _, l := range []list{open, resolved} {
		if len(l.rows) > 0 {
			lists = append(lists, l)
		}
	}

	// Pin what needs the user, wherever it is listed.
	var pinned []row
	for li := range lists {
		kept := lists[li].rows[:0:0]
		for _, r := range lists[li].rows {
			if r.needsYou() {
				pinned = append(pinned, r)
			} else {
				kept = append(kept, r)
			}
		}
		lists[li].rows = kept
	}

	var rows []row
	if len(pinned) > 0 {
		rows = append(rows, row{kind: rowHeading, list: listNeedsYou, key: "head:" + listNeedsYou, count: len(pinned)})
		rows = append(rows, pinned...)
	}
	for _, l := range lists {
		// A board's priority list whose tasks all wait on the user is
		// all under Needs you: its heading alone says nothing.
		if len(l.rows) == 0 && snap.Project.Slug == boardSlug {
			continue
		}
		isFolded, ok := folds[l.name]
		if !ok {
			isFolded = startsFolded(l.name, folded)
		}
		if len(rows) > 0 {
			rows = append(rows, row{kind: rowGap, key: "gap:" + l.name})
		}
		h := row{kind: rowHeading, list: l.name, key: "head:" + l.name, count: len(l.rows), folded: isFolded, foldable: true}
		if isFolded {
			for _, r := range l.rows {
				h.updates += r.updates
				for _, t := range r.threads {
					if t.Status != deck.StatusDone {
						h.glyphs = append(h.glyphs, t.Status)
					}
				}
			}
		}
		rows = append(rows, h)
		if !isFolded {
			rows = append(rows, l.rows...)
		}
	}
	return rows
}

// rowLinks are the links of a row in the drawer's order: Linear, Figma,
// Notion, GitHub, localhost, each kind in the order found. A task offers its
// own links and its threads'.
func rowLinks(r row) []deck.Link {
	if r.kind == rowWork && r.issue != nil {
		return issueLinks(r)
	}
	if r.kind == rowWork && r.pull != nil {
		return r.links
	}
	var links []deck.Link
	add := func(ls []deck.Link) {
		for _, l := range ls {
			links = deck.AppendLink(links, l)
		}
	}
	switch r.kind {
	case rowWork:
		if r.task != nil {
			add(r.task.Links)
		}
		for _, t := range r.threads {
			add(t.Links)
		}
	}
	sort.SliceStable(links, func(i, j int) bool { return links[i].Kind < links[j].Kind })
	return links
}
