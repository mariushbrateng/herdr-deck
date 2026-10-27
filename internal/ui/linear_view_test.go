package ui

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FredricW/herdr-deck/internal/deck"
	"github.com/FredricW/herdr-deck/internal/source/fake"
)

// mineSnap is the sample board with the sample Linear issues.
func mineSnap() deck.Snapshot {
	snap := fake.Board(now)
	snap.Mine = fake.Mine(now)
	return snap
}

// linearModel is the deck on the Linear view, with the changes it makes
// recorded in set.
type issueChanges struct {
	states []string
	prios  []string
	err    error
}

func linearModel(t *testing.T, snap deck.Snapshot, w, h int) (Model, *issueChanges) {
	t.Helper()
	ch := &issueChanges{}
	m, _ := newModelWith(t, snap, w, h, func(o *Options) {
		o.LinearView = true
		o.SetIssueState = func(is deck.LinearIssue, id string) error {
			ch.states = append(ch.states, is.ID+"→"+id)
			return ch.err
		}
		o.SetIssuePriority = func(is deck.LinearIssue, p int) error {
			ch.prios = append(ch.prios, is.ID+"→"+string(rune('0'+p)))
			return ch.err
		}
	})
	return m, ch
}

// selectIssue puts the cursor on the issue id.
func selectIssue(t *testing.T, m Model, id string) Model {
	t.Helper()
	for i, r := range m.rows {
		if r.issue != nil && r.issue.ID == id {
			m.cursor, m.moved = i, true
			m.ensureVisible()
			return m
		}
	}
	t.Fatalf("no row for %s", id)
	return m
}

func TestLinearGolden(t *testing.T) {
	for _, w := range []int{100, 60} {
		m, _ := linearModel(t, mineSnap(), w, 50)
		m = selectIssue(t, m, "ENG-791")
		golden(t, "linear-"+strconv.Itoa(w), m)
	}
}

func TestLinearRows(t *testing.T) {
	m, _ := linearModel(t, mineSnap(), 100, 50)
	var got []string
	for _, r := range m.rows {
		switch {
		case r.kind == rowHeading:
			got = append(got, "# "+r.list)
		case r.issue != nil:
			got = append(got, r.issue.ID)
		}
	}
	// By state, under way first; by priority within, none last.
	want := "# In Progress,ENG-791,ENG-1201,ENG-890,# In Review,ENG-914,# Todo,ENG-1025,ENG-1597,# New,ENG-1632"
	if s := strings.Join(got, ","); s != want {
		t.Errorf("rows = %s\nwant   %s", s, want)
	}
	s := screen(m)
	for _, want := range []string{
		"ISSUE", "TASK", "TASK STATUS",
		"◐ P1 ENG-791 Guard på migrations",
		"WEB-5   ◐ 60% Writing",
		"WEB-4   ★ approve · 1 min…  #2412",
		"◌    ENG-1632 Behold metadata",
		"no task",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
}

func TestLinearTasks(t *testing.T) {
	snap := mineSnap()
	// A second, newer task for ENG-791 that is done, and one that is
	// open: the open one shows, newest first, with the others counted.
	done := snap.Threads[1]
	done.ID, done.Created = "WEB-11", now
	done.Board = &deck.BoardTask{ID: "webapp/11", Phase: deck.PhaseMerged}
	done.Status = deck.StatusDone
	other := snap.Threads[1]
	other.ID, other.Created = "DCK-3", snap.Threads[1].Created.Add(-time.Hour)
	other.Title = "ENG-7910 is another issue"
	snap.Threads = append(snap.Threads, done, other)
	m, _ := linearModel(t, snap, 100, 50)
	m = selectIssue(t, m, "ENG-791")
	r, _ := m.selected()
	if len(r.threads) != 2 || r.threads[0].ID != "WEB-5" || r.threads[1].ID != "WEB-11" {
		t.Fatalf("threads = %v", threadIDs(r.threads))
	}
	s := screen(m)
	if !strings.Contains(s, "WEB-5   ◐ 60% Writing … +1") {
		t.Errorf("no +1 for the second task:\n%s", s)
	}
	for _, want := range []string{"task     WEB-5 ◐ 60% Writing the alembic head check · shown in the list", "WEB-11 ✓ merged"} {
		if !strings.Contains(s, want) {
			t.Errorf("drawer lacks %q:\n%s", want, s)
		}
	}
}

func threadIDs(ts []deck.Thread) []string {
	var out []string
	for _, t := range ts {
		out = append(out, t.ID)
	}
	return out
}

func TestLinearDrawer(t *testing.T) {
	m, o := newModelWith(t, mineSnap(), 100, 60, func(o *Options) { o.LinearView = true })
	m = selectIssue(t, m, "ENG-791")
	m, _ = press(m, keys("z")...) // the drawer at full height
	s := screen(m)
	for _, want := range []string{
		"Guard på migrations", "◐ In Progress",
		"ENG-791 · P1 Urgent · Engineering · Platform hardening",
		"── Issue ──",
		"status   In Progress",
		"prio     P1 Urgent",
		"labels   ● Backend  ● Critical",
		"project  Platform hardening",
		"task     WEB-5",
		"── Description ──",
		"Make CI fail when two alembic heads exist.",
		"• check in",
		"── Comments ──  2",
		"Ana Berg · 1d ago",
		"Should this also block deploys?",
		"Alex · 3h ago",
		"── Links ──",
		"[1 ENG-791]",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("drawer lacks %q:\n%s", want, s)
		}
	}
	// Without SetIssueState and SetIssuePriority the keys say so.
	if strings.Contains(s, "m moves it") {
		t.Error("a hint for a key that is off")
	}
	m, _ = press(m, keys("m")...)
	if !strings.Contains(m.Status(), "off") {
		t.Errorf("status = %q", m.Status())
	}
	// The issue's link opens, and l opens it too.
	m, _ = press(m, keys("1")...)
	m, _ = press(m, keys("l")...)
	if len(o.urls) != 2 || o.urls[0] != "https://linear.app/acme/issue/ENG-791" {
		t.Errorf("opened %v", o.urls)
	}
}

func TestLinearPR(t *testing.T) {
	m, _ := linearModel(t, mineSnap(), 100, 60)
	m = selectIssue(t, m, "ENG-914")
	s := screen(m)
	for _, want := range []string{"── PR ──", "#2412"} {
		if !strings.Contains(s, want) {
			t.Errorf("drawer lacks %q:\n%s", want, s)
		}
	}
	m, o := newModelWith(t, mineSnap(), 100, 60, func(o *Options) { o.LinearView = true })
	m = selectIssue(t, m, "ENG-914")
	m, _ = press(m, keys("g")...)
	if len(o.urls) != 1 || o.urls[0] != "https://github.com/acme/webapp/pull/2412" {
		t.Errorf("g opened %v", o.urls)
	}
}

func TestLinearMove(t *testing.T) {
	m, ch := linearModel(t, mineSnap(), 100, 60)
	m = selectIssue(t, m, "ENG-1025")
	m, _ = press(m, keys("m")...)
	s := screen(m)
	for _, want := range []string{"╭─ move ENG-1025 ", "│ 1 ◌ New", "│ 2 ○ Todo (now)", "│ 3 ◐ In Progress", "│ 4 ◇ In Review", "│ 5 ✓ Done", "│ 6 ✕ Canceled"} {
		if !strings.Contains(s, want) {
			t.Errorf("drawer lacks %q:\n%s", want, s)
		}
	}
	m, _ = press(m, keys("3")...)
	if strings.Join(ch.states, ",") != "ENG-1025→s3" {
		t.Fatalf("states = %v", ch.states)
	}
	// The row moved under In Progress at once, the cursor with it.
	if r, _ := m.selected(); r.issue == nil || r.issue.ID != "ENG-1025" || r.list != "In Progress" {
		t.Errorf("selected %+v", r.list)
	}
	if !strings.Contains(m.Status(), "ENG-1025: In Progress in Linear") {
		t.Errorf("status = %q", m.Status())
	}
	// Its own state, a digit past the states and esc change nothing.
	m, _ = press(m, keys("m3")...)
	m, _ = press(m, keys("m9")...)
	m, _ = press(m, keys("m")...)
	m, _ = press(m, esc)
	if len(ch.states) != 1 {
		t.Errorf("states = %v", ch.states)
	}
	// A refusal is shown.
	ch.err = errors.New("moving ENG-1025: Linear refused")
	m, _ = press(m, keys("m1")...)
	if !strings.Contains(m.Status(), "Linear refused") {
		t.Errorf("status = %q", m.Status())
	}
}

func TestLinearPriority(t *testing.T) {
	m, ch := linearModel(t, mineSnap(), 100, 60)
	m = selectIssue(t, m, "ENG-1597")
	m, _ = press(m, keys("P1")...)
	if strings.Join(ch.prios, ",") != "ENG-1597→1" {
		t.Fatalf("prios = %v", ch.prios)
	}
	// It sorts first in Todo now.
	var todo []string
	for _, r := range m.rows {
		if r.issue != nil && r.list == "Todo" {
			todo = append(todo, r.issue.ID)
		}
	}
	if strings.Join(todo, ",") != "ENG-1597,ENG-1025" {
		t.Errorf("Todo = %v", todo)
	}
	if !strings.Contains(m.Status(), "P1 Urgent") {
		t.Errorf("status = %q", m.Status())
	}
	m, _ = press(m, keys("P5")...)
	m, _ = press(m, keys("P0")...)
	if strings.Join(ch.prios, ",") != "ENG-1597→1,ENG-1597→0" {
		t.Errorf("prios = %v", ch.prios)
	}
}

func TestLinearToggle(t *testing.T) {
	m, _ := linearModel(t, mineSnap(), 100, 50)
	m, _ = press(m, keys("V")...)
	s := screen(m)
	if !strings.Contains(s, "WORK") || !strings.Contains(s, "P1 Urgent") {
		t.Errorf("V does not show the tasks:\n%s", s)
	}
	// P on a task is the deck's own priority again.
	if r, _ := m.selected(); r.issue != nil {
		t.Error("an issue row on the tasks")
	}
	m, _ = press(m, keys("V")...)
	if s := screen(m); !strings.Contains(s, "ISSUE") {
		t.Errorf("V does not come back:\n%s", s)
	}
	// Folding a state works as on the tasks.
	m.cursor, m.moved = 0, true
	m, _ = press(m, keys(" ")...)
	if s := screen(m); !strings.Contains(s, "+ In Progress (3)") {
		t.Errorf("no fold:\n%s", s)
	}
}

func TestLinearEmpty(t *testing.T) {
	for _, c := range []struct {
		mine *deck.Mine
		want string
	}{
		{nil, "Linear is off"},
		{&deck.Mine{Off: "no Linear API key; set $LINEAR_API_KEY"}, "no Linear API key"},
		{&deck.Mine{}, "Reading your Linear issues"},
		{&deck.Mine{Problem: "cannot reach Linear: timed out"}, "cannot reach Linear"},
		{&deck.Mine{Read: now}, "No open Linear issues"},
	} {
		snap := fake.Board(now)
		snap.Mine = c.mine
		m, _ := linearModel(t, snap, 100, 30)
		if s := screen(m); !strings.Contains(s, c.want) {
			t.Errorf("%+v: screen lacks %q:\n%s", c.mine, c.want, s)
		}
	}
}

func TestLinearMoveManyStates(t *testing.T) {
	snap := mineSnap()
	var many []deck.WorkflowState
	for i := range 16 {
		many = append(many, deck.WorkflowState{ID: "x" + strconv.Itoa(i), Name: "State " + strconv.Itoa(i), Type: "started", Position: float64(i)})
	}
	snap.Mine.States["ENG"] = many
	m, ch := linearModel(t, snap, 100, 60)
	m = selectIssue(t, m, "ENG-1025")
	m, _ = press(m, keys("m")...)
	if s := screen(m); !strings.Contains(s, "9 ◐ State 8") || !strings.Contains(s, "a ◐ State 9") || !strings.Contains(s, "g ◐ State 15") {
		t.Errorf("drawer lacks the lettered states:\n%s", s)
	}
	m, _ = press(m, keys("c")...)
	if strings.Join(ch.states, ",") != "ENG-1025→x11" {
		t.Errorf("states = %v", ch.states)
	}
}

func TestLinearSameNameTwoTeams(t *testing.T) {
	snap := mineSnap()
	// Infra's In Progress sits at another position than Engineering's, past
	// Engineering's In Review: it still joins Engineering's In Progress group.
	snap.Mine.Issues = append(snap.Mine.Issues, deck.LinearIssue{
		UUID: "u-inf", ID: "INF-112", Title: "Slett test-brukere", TeamKey: "INF",
		State: deck.WorkflowState{ID: "i3", Name: "In Progress", Type: "started", Position: 5},
	})
	m, _ := linearModel(t, snap, 100, 50)
	heads := map[string]int{}
	for _, r := range m.rows {
		if r.kind == rowHeading {
			heads[r.list]++
		}
		if r.issue != nil && r.issue.ID == "INF-112" && r.list != "In Progress" {
			t.Errorf("INF-112 under %s", r.list)
		}
	}
	if heads["In Progress"] != 1 {
		t.Errorf("headings = %v", heads)
	}
}

// at is the screen column and line where text first shows, after the
// line holding after (or anywhere when after is "").
func at(t *testing.T, m Model, after, text string) (int, int) {
	t.Helper()
	seen := after == ""
	for y, l := range strings.Split(screen(m), "\n") {
		if !seen {
			seen = strings.Contains(l, after)
			continue
		}
		if i := strings.Index(l, text); i >= 0 {
			return ansi.StringWidth(l[:i]), y
		}
	}
	t.Fatalf("%q is not on the screen:\n%s", text, screen(m))
	return 0, 0
}

func TestLinearStatePicker(t *testing.T) {
	m, ch := linearModel(t, mineSnap(), 100, 60)
	m = selectIssue(t, m, "ENG-1025")
	// ↓ and ↵ from its own state, Todo, pick the next one.
	m, _ = press(m, keys("m")...)
	if !strings.Contains(screen(m), "↑↓ ↵ pick") {
		t.Errorf("no picker help in the footer:\n%s", screen(m))
	}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyDown}, tea.KeyPressMsg{Code: tea.KeyEnter})
	if strings.Join(ch.states, ",") != "ENG-1025→s3" {
		t.Fatalf("states = %v", ch.states)
	}
	if strings.Contains(screen(m), "move ENG-1025") {
		t.Error("the picker stays open after a pick")
	}

	// A click on a row's state mark opens it on that row's issue, and a
	// click on a state picks it.
	_, y := at(t, m, "ISSUE", "ENG-1201")
	m, _ = press(m, click(markX, y))
	if r, _ := m.selected(); r.issue == nil || r.issue.ID != "ENG-1201" || m.moveArmed != "ENG-1201" {
		t.Fatalf("a click on the mark did not open the picker on ENG-1201:\n%s", screen(m))
	}
	x, y := at(t, m, "move ENG-1201", "5 ✓ Done")
	m, _ = press(m, click(x, y))
	if strings.Join(ch.states, ",") != "ENG-1025→s3,ENG-1201→s5" {
		t.Fatalf("states = %v", ch.states)
	}

	// A click elsewhere closes it, and the click on the title does not.
	m = selectIssue(t, m, "ENG-791")
	m, _ = press(m, keys("m")...)
	x, y = at(t, m, "", "move ENG-791")
	m, _ = press(m, click(x, y))
	if m.moveArmed == "" {
		t.Error("a click on the border closed the picker")
	}
	m, _ = press(m, click(90, 1))
	if m.moveArmed != "" || len(ch.states) != 2 {
		t.Errorf("a click outside: open %q, states %v", m.moveArmed, ch.states)
	}

	// The drawer's status opens it too.
	x, y = at(t, m, "── Issue ──", "In Progress")
	m, _ = press(m, click(x, y))
	if m.moveArmed != "ENG-791" {
		t.Fatalf("a click on the drawer's status did not open the picker:\n%s", screen(m))
	}

	// A refusal shows in the footer and stays in the drawer.
	ch.err = errors.New("moving ENG-791: Linear refused the change: the API key from $LINEAR_API_KEY may not change issues")
	m, _ = press(m, keys("1")...)
	if !strings.Contains(m.Status(), "could not change ENG-791") || m.failed != m.Status() {
		t.Errorf("status = %q", m.Status())
	}
	m, _ = press(m, esc)
	if s := screen(m); !strings.Contains(s, "refused  moving ENG-791: Linear refused") {
		t.Errorf("the drawer does not show the refusal:\n%s", s)
	}
	// It goes when a change to the issue goes through.
	ch.err = nil
	m, _ = press(m, keys("m4")...)
	if strings.Contains(screen(m), "refused  moving") {
		t.Error("the refusal stays after a change went through")
	}
}

func TestLinearStatePickerScrolls(t *testing.T) {
	snap := mineSnap()
	var many []deck.WorkflowState
	for i := range 16 {
		many = append(many, deck.WorkflowState{ID: "x" + strconv.Itoa(i), Name: "State " + strconv.Itoa(i), Type: "started", Position: float64(i)})
	}
	snap.Mine.States["ENG"] = many
	// Room for 8 states: the box scrolls with the selection.
	m, ch := linearModel(t, snap, 80, 14)
	m = selectIssue(t, m, "ENG-1025")
	m, _ = press(m, keys("m")...)
	if s := screen(m); !strings.Contains(s, "1–8 of 16") {
		t.Fatalf("no scroll note:\n%s", s)
	}
	for range 12 {
		m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyDown})
	}
	if s := screen(m); !strings.Contains(s, "d ◐ State 12") || strings.Contains(s, "State 0 ") {
		t.Errorf("the box does not follow the selection:\n%s", s)
	}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if strings.Join(ch.states, ",") != "ENG-1025→x12" {
		t.Errorf("states = %v", ch.states)
	}
}
