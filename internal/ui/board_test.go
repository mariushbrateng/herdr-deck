package ui

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/FredricW/herdr-deck/internal/deck"
	"github.com/FredricW/herdr-deck/internal/source/fake"
)

func TestBoardGolden(t *testing.T) {
	for _, w := range []int{100, 60} {
		m, _ := newModel(t, fake.Board(now), w, 40)
		golden(t, "board-"+strconv.Itoa(w), m)
	}
}

func TestBoardRows(t *testing.T) {
	m, _ := newModel(t, fake.Board(now), 100, 60)
	s := screen(m)
	for _, want := range []string{
		// What waits on the user is pinned on top, in priority order.
		"Needs you",
		"● P1 ⚠ ENG-1201 Stop losing chunks",
		"★ P2 ⚠ ENG-914 Restrict org admin",
		"approve · 1 minor",
		"◆ P2 ENG-1597",
		"plan ready",
		"✕ P3 ENG-890",
		// Then the lists by priority.
		"P1 Urgent",
		"◐ P1 ⚠ ENG-791 Guard på migrations",
		"60% Writing the a",
		"◇ P3 ENG-1009",
		"◌ P4 ENG-1100",
		"queued #1",
		"No priority",
		"○ ENG-1025",
		// Finished tasks start folded.
		"+ Resolved (2)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
}

func TestBoardDrawer(t *testing.T) {
	m, o := newModel(t, fake.Board(now), 100, 60)
	// The cursor starts on the first task that needs the user; move to
	// the one ready to approve.
	m, _ = press(m, keys("j")...)
	if got := selectedTitle(m); !strings.Contains(got, "ENG-914") {
		t.Fatalf("selected %q", got)
	}
	s := screen(m)
	for _, want := range []string{
		"── Board ──",
		"★ ready to approve",
		"P2 High",
		"⚠ AGENTS.md: Permissions model",
		"frontend/ changed",
		"Pass · 1 minor, 1 trivial · gate major",
		"F1 minor The stats query still sorts in Python.",
		"1h 05m · 412k tokens",
		"── Worktree ──",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("drawer lacks %q:\n%s", want, s)
		}
	}
	m, _ = press(m, keys("R")...)
	if len(o.urls) != 1 || o.urls[0] != "http://127.0.0.1:52797/webapp/4/review.html" {
		t.Errorf("R opened %v", o.urls)
	}
}

func TestBoardPriorityKeys(t *testing.T) {
	var set []string
	setPrio := func(t deck.Thread, p int) error {
		set = append(set, t.Board.ID+"="+strconv.Itoa(p))
		if p == 3 {
			return errors.New("disk full")
		}
		return nil
	}
	m, _ := newModelWith(t, fake.Board(now), 100, 40, func(o *Options) {
		o.SetPriority = setPrio
		o.Relist = func(threads []deck.Thread) []deck.TaskList {
			// One list is enough to see the priority land.
			l := deck.TaskList{Name: "All"}
			for _, th := range threads {
				l.Tasks = append(l.Tasks, deck.Task{Title: th.Title, Threads: []string{th.ID}})
			}
			return []deck.TaskList{l}
		}
	})
	// The cursor is on WEB-9 (P1): P 4 makes it Low.
	m = runPrio(m, "P4")
	if len(set) != 1 || set[0] != "webapp/9=4" {
		t.Fatalf("set %v", set)
	}
	if !strings.Contains(m.status, "WEB-9: P4 Low") {
		t.Errorf("status %q", m.status)
	}
	if !strings.Contains(screen(m), "P4 ⚠ ENG-1201") {
		t.Errorf("the row does not show P4:\n%s", screen(m))
	}
	// Any other key ends the chord without setting anything.
	m = runPrio(m, "Px")
	if len(set) != 1 {
		t.Errorf("Px set %v", set)
	}
	m = runPrio(m, "P3")
	if !strings.Contains(m.status, "could not set the priority of WEB-9: disk full") {
		t.Errorf("status %q", m.status)
	}
}

// runPrio presses keys and delivers the priority's result to the model.
func runPrio(m Model, s string) Model {
	for _, k := range keys(s) {
		next, cmd := m.Update(k)
		m = next.(Model)
		if cmd != nil {
			if msg, ok := cmd().(prioritySetMsg); ok {
				next, _ := m.Update(msg)
				m = next.(Model)
			}
		}
	}
	return m
}
