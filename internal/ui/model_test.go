package ui

import (
	"context"
	"errors"
	"flag"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FredricW/herdr-deck/internal/deck"
	"github.com/FredricW/herdr-deck/internal/source/fake"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden from the current rendering")

// testVersion stands in for the build's version so the goldens do not
// depend on it.
const testVersion = "v9.9.9"

var now = time.Date(2026, 10, 2, 14, 41, 0, 0, time.UTC)

// opened records what the model asked to open; nothing is ever opened.
type opened struct{ urls, dirs, panes, devs, diffs []string }

func newModel(t *testing.T, snap deck.Snapshot, w, h int) (Model, *opened) {
	t.Helper()
	return newModelWith(t, snap, w, h, func(*Options) {})
}

// newModelWith is newModel with options changed by set.
func newModelWith(t *testing.T, snap deck.Snapshot, w, h int, set func(*Options)) (Model, *opened) {
	t.Helper()
	o := &opened{}
	opt := Options{
		Now:        func() time.Time { return now },
		Location:   time.UTC,
		OpenURL:    func(u string) error { o.urls = append(o.urls, u); return nil },
		OpenEditor: func(p string) error { o.dirs = append(o.dirs, p); return nil },
		FocusPane:  func(id string) error { o.panes = append(o.panes, id); return nil },
		StartDev: func(t deck.Thread) (string, error) {
			o.devs = append(o.devs, t.Worktree)
			return "started make dev for " + t.ID, nil
		},
		OpenDiff: func(path, base string, files []string) error {
			o.diffs = append(o.diffs, strings.TrimSpace(path+" "+base+" "+strings.Join(files, " ")))
			return nil
		},
		Version: testVersion,
	}
	set(&opt)
	m := New(snap, opt)
	m, _ = press(m, tea.WindowSizeMsg{Width: w, Height: h})
	return m, o
}

// press feeds messages to the model, running every command it returns until
// none is left, so an open link lands in opened.
func press(m Model, msgs ...tea.Msg) (Model, tea.Cmd) {
	var last tea.Cmd
	for _, msg := range msgs {
		next, cmd := m.Update(msg)
		m = next.(Model)
		last = cmd
		m = run(m, cmd)
	}
	return m, last
}

func run(m Model, cmd tea.Cmd) Model {
	if cmd == nil {
		return m
	}
	switch msg := cmd().(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			m = run(m, c)
		}
	case openedMsg, devUpMsg, diffMsg, patchMsg, commitsMsg, commitFilesMsg, checkLogMsg, archMsg, edgeViewMsg, issueSetMsg:
		next, _ := m.Update(msg)
		m = next.(Model)
	case settingsMsg, settingsSavedMsg:
		next, cmd := m.Update(msg)
		m = run(next.(Model), cmd)
	}
	return m
}

func keys(s string) []tea.Msg {
	var out []tea.Msg
	for _, r := range s {
		switch r {
		case ' ':
			out = append(out, tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
		default:
			out = append(out, tea.KeyPressMsg{Code: r, Text: string(r)})
		}
	}
	return out
}

var esc = tea.KeyPressMsg{Code: tea.KeyEscape}

// screen is the pane as plain text, without trailing spaces.
func screen(m Model) string {
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " ")
	}
	return strings.Join(lines, "\n")
}

// golden compares the plain-text screen with testdata/<name>.golden.
func golden(t *testing.T, name string, m Model) {
	t.Helper()
	lines := strings.Split(ansi.Strip(m.View().Content), "\n")
	if len(lines) != m.height {
		t.Errorf("%s: %d lines, want the pane height %d", name, len(lines), m.height)
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > m.width {
			t.Errorf("%s: line %d is %d wide, more than %d: %q", name, i, w, m.width, l)
		}
	}
	got := screen(m)
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/ui -update)", err)
	}
	if got+"\n" != string(want) {
		t.Errorf("%s differs from %s:\n%s", name, path, got)
	}
}

func fakeSnap() deck.Snapshot { return fake.Snapshot("admin-rebuild", now) }

// calm is the mockups' normal use: nothing needs the user.
func calm() deck.Snapshot {
	s := fakeSnap()
	s.Inbox = nil
	s.Threads[1].Status = deck.StatusWorking
	s.Threads[1].StateLine = "working · ~60%"
	sixty := 60
	s.Threads[1].Percent = &sixty
	s.Threads[1].Group = "working"
	s.Threads[1].Activity = "Building overview"
	s.Threads[1].Next = nil
	return s
}

func TestGolden(t *testing.T) {
	missing := calm()
	missing.Missing = []string{
		".state/ticker.json: no such file or directory",
		"thread list: herdr-projects: no session; showing threads/*.toml as last recorded",
	}
	missing.ThreadsAsOf = now.Add(-18 * time.Minute)

	// An unhandled inbox item about t-0004 tints its row and shows in its
	// Log; one about no thread is only counted in the header.
	tinted := calm()
	tinted.Inbox = []deck.InboxItem{{ID: "20261002T143900Z-pr-t-0004-10", Kind: "pr", Thread: "t-0004", Subject: "t-0004", Event: "PR checks failing", Created: now.Add(-2 * time.Minute)}}
	tinted.Threads[3].Log = append([]deck.LogEvent{{At: now.Add(-2 * time.Minute), Kind: deck.EventChecksFailing, Text: "checks failing: lint, test (ubuntu-latest)", Unhandled: true, Source: "inbox/20261002T143900Z-pr-t-0004-10.md"}}, tinted.Threads[3].Log...)
	tinted.Threads[3].PR.FailingChecks = []string{"lint", "test (ubuntu-latest)"}
	inboxOnly := calm()
	inboxOnly.Inbox = fakeSnap().Inbox[1:]

	cases := []struct {
		name string
		snap deck.Snapshot
		keys []tea.Msg
	}{
		{name: "normal", snap: calm()},
		{name: "needs-you", snap: fakeSnap()},
		{name: "needs-you-full", snap: fakeSnap(), keys: keys("z")},
		{name: "log", snap: fakeSnap(), keys: keys("]")},
		{name: "log-full", snap: fakeSnap(), keys: keys("]z")},
		{name: "review-full", snap: calm(), keys: keys("jjz")},
		{name: "tinted", snap: tinted, keys: keys("jj")},
		{name: "tinted-log", snap: tinted, keys: keys("jj[")},
		{name: "inbox-only", snap: inboxOnly},
		{name: "no-thread", snap: calm(), keys: keys("jjjjjj jjj")},
		{name: "drawer-focus", snap: calm(), keys: append([]tea.Msg{tea.KeyPressMsg{Code: tea.KeyTab}}, keys("j")...)},
		{name: "heading", snap: calm(), keys: keys("k")},
		{name: "chooser-linear", snap: calm(), keys: keys("l")},
		{name: "chooser-figma", snap: calm(), keys: keys("jjf")},
		{name: "empty", snap: deck.Snapshot{Project: fakeSnap().Project}},
		{name: "missing", snap: missing},
		{name: "backlog-open", snap: calm(), keys: keys("jjjjjj ")},
		{name: "report", snap: fakeSnap(), keys: keys("r")},
		{name: "help", snap: calm(), keys: keys("?")},
		{name: "drawer-hidden", snap: calm(), keys: keys("zz")},
		{name: "dev", snap: calm(), keys: keys("jj")},
		{name: "dev-fallback", snap: calm(), keys: keys("jjjj")},
		{name: "sources-notes", snap: calm(), keys: keys("!")},
		{name: "dev-not-started", snap: devNote(deck.DevNotStarted)},
		{name: "dev-no-manifest", snap: devNote("no .config/dev.json")},
		{name: "dev-starting", snap: starting(true)},
		{name: "dev-exited", snap: starting(false)},
		{name: "dev-services", snap: services(), keys: keys("jz")},
	}
	for _, c := range cases {
		for _, w := range []int{80, 60} {
			name := c.name + "-" + map[int]string{80: "80", 60: "60"}[w]
			t.Run(name, func(t *testing.T) {
				m, _ := newModel(t, deck.Snapshot{}, w, 28)
				m, _ = press(m, snapshotMsg(c.snap))
				m, _ = press(m, c.keys...)
				golden(t, name, m)
			})
		}
	}
}

// find returns the screen column and line where text starts.
func find(t *testing.T, m Model, text string) (int, int) {
	t.Helper()
	for y, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
		if i := strings.Index(l, text); i >= 0 {
			return ansi.StringWidth(l[:i]), y
		}
	}
	t.Fatalf("%q not on screen:\n%s", text, screen(m))
	return 0, 0
}

func click(x, y int) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func selectedTitle(m Model) string {
	r, _ := m.selected()
	return r.title()
}

// starting is calm with an up command the deck started for t-0002, whose
// state file is not written yet.
func starting(alive bool) deck.Snapshot {
	s := calm()
	s.Threads[1].DevUp = &deck.DevUp{Log: "/var/state/herdr-deck/logs/admin-rebuild-t-0002.log", Alive: alive}
	// Without Linear states the drawer has room for the log line.
	links := s.TaskLists[0].Tasks[0].Links
	for i := range links {
		links[i].Issue = nil
	}
	return s
}

// devNote is starting's snapshot without the up command: t-0002 has no dev
// links, and note says why.
func devNote(note string) deck.Snapshot {
	s := starting(false)
	s.Threads[1].DevUp = nil
	s.Threads[1].DevNote = note
	return s
}

// services gives Templates page a service in every state.
func services() deck.Snapshot {
	s := calm()
	for i := range s.Threads {
		if s.Threads[i].ID != "t-0003" {
			continue
		}
		srv := s.Threads[i].DevServers
		srv[1].Running, srv[1].Starting = false, true
		srv = append(srv, deck.DevServer{Name: "docs", Pending: "port store not supported yet"})
		s.Threads[i].DevServers = srv
	}
	return s
}

func TestDevUp(t *testing.T) {
	m, o := newModel(t, calm(), 80, 28)
	m, _ = press(m, keys("u")...)
	if len(o.devs) != 1 || o.devs[0] != "/src/worktrees/t-0002" || m.Status() != "started make dev for t-0002" {
		t.Fatalf("u: started %q, status %q", o.devs, m.Status())
	}
	m, _ = press(m, keys("jjj")...) // Templates page: no thread
	m, _ = press(m, keys("u")...)
	if len(o.devs) != 1 || m.Status() != "no thread on this row" {
		t.Errorf("u without a thread: started %q, status %q", o.devs, m.Status())
	}

	m, _ = newModel(t, calm(), 80, 28)
	m.opt.StartDev = func(deck.Thread) (string, error) { return "", errors.New("permission denied") }
	m, _ = press(m, keys("u")...)
	if m.Status() != "could not start dev servers: permission denied" {
		t.Errorf("u failing: status %q", m.Status())
	}
}

func TestMoveAndClamp(t *testing.T) {
	m, _ := newModel(t, calm(), 80, 28)
	if got := selectedTitle(m); got != "Users page" {
		t.Fatalf("start on %q, want the first row of work", got)
	}
	m, _ = press(m, keys("kkk")...)
	if m.cursor != 0 {
		t.Fatalf("k past the top: cursor %d", m.cursor)
	}
	m, _ = press(m, keys("jjjjjjjjjjjj")...)
	if got := selectedTitle(m); got != "Backlog" {
		t.Fatalf("j past the bottom: on %q, want the folded Backlog heading", got)
	}
}

// A Linear ID read without a workspace has no URL: the drawer says so and
// opening it names the flag and variable instead of opening a broken URL.
func TestLinearWithoutWorkspace(t *testing.T) {
	snap := calm()
	links := snap.TaskLists[0].Tasks[0].Links
	for i := range links {
		links[i].URL = ""
	}
	m, o := newModel(t, snap, 100, 28)
	if !strings.Contains(screen(m), "no Linear workspace") {
		t.Errorf("drawer has no workspace hint:\n%s", screen(m))
	}
	m, _ = press(m, keys("1")...)
	if len(o.urls) != 0 {
		t.Errorf("opened %q, want nothing", o.urls)
	}
	if !strings.Contains(m.Status(), "ABC-1246: no Linear workspace") || !strings.Contains(m.Status(), deck.EnvLinearWorkspace) {
		t.Errorf("status = %q", m.Status())
	}
}

func TestDigitsAndLetterKeysOpenLinks(t *testing.T) {
	m, o := newModel(t, calm(), 80, 28)
	m, _ = press(m, keys("2")...)
	if want := "https://linear.app/acme/issue/ABC-1256"; len(o.urls) != 1 || o.urls[0] != want {
		t.Fatalf("2 opened %q, want %q", o.urls, want)
	}
	if !strings.Contains(m.Status(), "opened ABC-1256") {
		t.Errorf("status = %q", m.Status())
	}
	m, _ = press(m, keys("9")...)
	if len(o.urls) != 1 || !strings.Contains(m.Status(), "no link 9") {
		t.Errorf("9 on a row with 4 links: opened %q, status %q", o.urls, m.Status())
	}
	m, _ = press(m, keys("f")...)
	if !strings.HasPrefix(m.Status(), "no Figma link") {
		t.Errorf("f without Figma links: status %q", m.Status())
	}

	// Document select: one PR, so g opens it at once.
	m, _ = press(m, keys("jjg")...)
	if got := o.urls[len(o.urls)-1]; got != "https://github.com/acme/webshop/pull/2320" {
		t.Errorf("g opened %q", got)
	}
	// Templates page: one localhost link, from its thread.
	press(m, keys("ko")...)
	if got := o.urls[len(o.urls)-1]; got != "http://localhost:5181" {
		t.Errorf("o opened %q", got)
	}
}

func TestLocalhostKeyOpensFirstRunning(t *testing.T) {
	snap := calm()
	// Document select: API docs is down; make Frontend down too, and add a
	// running link after them.
	th := &snap.Threads[3]
	th.Links[1].Down = true
	th.Links = append(th.Links, deck.Link{Kind: deck.LinkLocalhost, Label: "Storybook", URL: "http://localhost:6006"})
	m, o := newModel(t, snap, 80, 28)
	m, _ = press(m, keys("jjo")...)
	if len(o.urls) != 1 || o.urls[0] != "http://localhost:6006" || m.choosing != noKind {
		t.Fatalf("o opened %q (choosing %v), want the running Storybook link", o.urls, m.choosing)
	}

	// The fallback port does not listen: o says so and opens nothing, but
	// its digit still opens it.
	m, _ = press(m, keys("jjo")...)
	if len(o.urls) != 1 || !strings.Contains(m.Status(), "no dev server on this row is running") {
		t.Errorf("o with every server down: opened %q, status %q", o.urls, m.Status())
	}
	m, _ = press(m, keys("3")...)
	if got := o.urls[len(o.urls)-1]; got != "http://localhost:14437" {
		t.Errorf("3 opened %q", got)
	}
	m, _ = press(m, keys("ko")...)
	if !strings.HasPrefix(m.Status(), "no localhost link") {
		t.Errorf("o without localhost links: status %q", m.Status())
	}
}

// Dev links sit on top of Overview with their port, wrap whole at 60
// columns, and their digits and clicks open the link the chip shows.
func TestDevChips(t *testing.T) {
	snap := calm()
	th := &snap.Threads[2] // Templates page: Frontend is its link 3
	th.Links = append(th.Links,
		deck.Link{Kind: deck.LinkLocalhost, Label: "API docs", URL: "http://localhost:8011/docs", Down: true},
		deck.Link{Kind: deck.LinkLocalhost, Label: "Storybook components", URL: "http://127.0.0.1:6006"},
		deck.Link{Kind: deck.LinkLocalhost, Label: ":5441 db", URL: "http://localhost:5441"},
		deck.Link{Kind: deck.LinkLocalhost, Label: "Queue on queue.shop.test", URL: "https://queue.shop.test/abc-12"},
	)
	m, o := newModel(t, snap, 60, 40)
	m, _ = press(m, keys("jz")...)
	chips := []struct{ text, url string }{
		{"[3 Frontend :5181 ●]", "http://localhost:5181"},
		{"[4 API docs :8011 ○]", "http://localhost:8011/docs"},
		{"[5 Storybook components :6006 ●]", "http://127.0.0.1:6006"},
		{"[6 :5441 db ●]", "http://localhost:5441"},
		{"[7 Queue on queue.shop.test ●]", "https://queue.shop.test/abc-12"},
	}
	sc := screen(m)
	if strings.Contains(sc, "── Dev ──") {
		t.Errorf("Overview still has a Dev section:\n%s", sc)
	}
	_, top := find(t, m, chips[0].text)
	_, links := find(t, m, "── Links ──")
	if top > links {
		t.Errorf("dev chips at line %d, below Links at %d:\n%s", top, links, sc)
	}
	for i, c := range chips {
		x, y := find(t, m, c.text) // whole: a chip never breaks across lines
		m, _ = press(m, click(x+2, y))
		if got := o.urls[len(o.urls)-1]; got != c.url {
			t.Errorf("click on %s opened %q", c.text, got)
		}
		m, _ = press(m, keys(strconv.Itoa(i+3))...)
		if got := o.urls[len(o.urls)-1]; got != c.url {
			t.Errorf("%d opened %q, want %s", i+3, got, c.url)
		}
	}
}

func TestLinkPort(t *testing.T) {
	for _, tt := range []struct{ label, url, want string }{
		{"Frontend", "http://localhost:5181", "5181"},
		{":5181 web", "http://localhost:5181", ""},
		{"localhost:5181/docs", "http://localhost:5181/docs", ""},
		{":8080 proxy", "http://localhost:80/", "80"},
		{"~:80", "http://localhost:80", ""},
		{"Queue", "https://queue.shop.test/abc-12", ""},
	} {
		if got := linkPort(deck.Link{Label: tt.label, URL: tt.url}); got != tt.want {
			t.Errorf("linkPort(%q, %q) = %q, want %q", tt.label, tt.url, got, tt.want)
		}
	}
}

func TestChooser(t *testing.T) {
	m, o := newModel(t, calm(), 80, 28)

	m, _ = press(m, keys("l")...)
	if m.choosing != deck.LinkLinear || len(o.urls) != 0 {
		t.Fatalf("l with four Linear links: choosing %v, opened %q", m.choosing, o.urls)
	}
	if !strings.Contains(screen(m), "Linear: 1-4 open  a all  l first  esc cancel") {
		t.Errorf("footer does not ask for a digit:\n%s", screen(m))
	}
	m, _ = press(m, keys("3")...)
	if m.choosing != noKind || len(o.urls) != 1 || !strings.HasSuffix(o.urls[0], "ABC-1257") {
		t.Fatalf("3 in the chooser: choosing %v, opened %q", m.choosing, o.urls)
	}

	m, _ = press(m, keys("ll")...)
	if !strings.HasSuffix(o.urls[1], "ABC-1246") {
		t.Errorf("l l opened %q, want the first", o.urls[1])
	}
	m, _ = press(m, keys("la")...)
	if len(o.urls) != 6 {
		t.Errorf("l a opened %d links, want 2 + 4", len(o.urls))
	}
	m, _ = press(m, append(keys("l"), esc)...)
	if m.choosing != noKind || len(o.urls) != 6 {
		t.Errorf("esc: choosing %v, opened %d", m.choosing, len(o.urls))
	}
	// Another key closes the chooser and still does its own job.
	m, _ = press(m, keys("lj")...)
	if m.choosing != noKind || selectedTitle(m) != "Templates page" {
		t.Errorf("j in the chooser: choosing %v, on %q", m.choosing, selectedTitle(m))
	}

	// Figma: d then a digit opens the desktop app.
	m, _ = press(m, keys("jfd")...)
	if !strings.Contains(screen(m), "Figma desktop: 2-4 open") {
		t.Errorf("after f d the footer is:\n%s", screen(m))
	}
	m, _ = press(m, keys("3")...)
	if got := o.urls[len(o.urls)-1]; got != "figma://design/abc123/Document-select?node-id=1138-88367" {
		t.Errorf("f d 3 opened %q", got)
	}
	m, _ = press(m, keys("fd1")...)
	if !strings.Contains(m.Status(), "not a Figma link") {
		t.Errorf("f d 1 (a Linear link): status %q", m.Status())
	}
}

func TestNeedsYouPullsCursorUntilMoved(t *testing.T) {
	m, _ := newModel(t, calm(), 80, 28)
	m, _ = press(m, snapshotMsg(fakeSnap()))
	if got := selectedTitle(m); got != "Users page" || !m.rows[m.cursor].needsYou() {
		t.Fatalf("cursor on %q, want the needs-you row", got)
	}
	if m.rows[0].list != listNeedsYou {
		t.Fatalf("first row is %q, want the Needs you heading", m.rows[0].list)
	}

	// Moved by hand, the cursor stays on its row across refreshes, even
	// when a needs-you row appears above it.
	m, _ = newModel(t, calm(), 80, 28)
	m, _ = press(m, keys("jj")...)
	m, _ = press(m, snapshotMsg(fakeSnap()))
	if got := selectedTitle(m); got != "Document select for summary" {
		t.Fatalf("after refresh the cursor is on %q", got)
	}
}

func TestFolding(t *testing.T) {
	m, _ := newModel(t, calm(), 80, 28)
	if !strings.Contains(screen(m), "+ Backlog (3)") || strings.Contains(screen(m), "Settings page") {
		t.Fatalf("Backlog does not start folded:\n%s", screen(m))
	}
	m, _ = press(m, keys("jjjjjj ")...)
	if !strings.Contains(screen(m), "Settings page") {
		t.Fatalf("space did not unfold Backlog:\n%s", screen(m))
	}
	// The fold sticks across refreshes.
	m, _ = press(m, snapshotMsg(calm()))
	if !strings.Contains(screen(m), "Settings page") {
		t.Fatalf("refresh folded Backlog again")
	}
	// space on a heading folds it, with its threads' glyphs shown.
	m, _ = press(m, keys("kkkkkk ")...)
	if !strings.Contains(screen(m), "+ In progress (3) ◐ ◐ ◇") {
		t.Fatalf("folded In progress lacks its glyphs:\n%s", screen(m))
	}
	// space on a work row does nothing.
	before := len(m.rows)
	m, _ = press(m, keys("jj ")...)
	if len(m.rows) != before {
		t.Fatalf("space on a work row changed the list")
	}
}

// FoldedLists names the lists that start folded, ignoring case; an empty
// list folds none.
func TestFoldedListsOption(t *testing.T) {
	m, _ := newModelWith(t, calm(), 80, 40, func(o *Options) { o.FoldedLists = []string{} })
	if s := screen(m); strings.Contains(s, "+ Backlog") || !strings.Contains(s, "Settings page") {
		t.Errorf("an empty list still folds Backlog:\n%s", s)
	}
	m, _ = newModelWith(t, calm(), 80, 40, func(o *Options) { o.FoldedLists = []string{"IN PROGRESS"} })
	if s := screen(m); !strings.Contains(s, "+ In progress (3)") || strings.Contains(s, "+ Backlog") {
		t.Errorf("only In progress should start folded:\n%s", s)
	}
}

// A blank line separates two groups, never ends the list, and the cursor,
// the wheel and clicks all step over it.
func TestGapsBetweenGroups(t *testing.T) {
	m, _ := newModel(t, calm(), 80, 28)
	lines := strings.Split(screen(m), "\n")
	for _, heading := range []string{"On hold until Monday", "+ Backlog (3)"} {
		_, y := find(t, m, heading)
		if strings.TrimSpace(lines[y-1]) != "" {
			t.Errorf("no blank line above %q:\n%s", heading, screen(m))
		}
	}
	if m.rows[0].kind == rowGap || m.rows[len(m.rows)-1].kind == rowGap {
		t.Errorf("a gap starts or ends the list")
	}

	m, _ = press(m, keys("jj")...)
	if got := selectedTitle(m); got != "Document select for summary" {
		t.Fatalf("on %q, want the last row of In progress", got)
	}
	m, _ = press(m, keys("j")...)
	if got := selectedTitle(m); got != "On hold until Monday 2026-10-05" {
		t.Fatalf("j over the gap: on %q", got)
	}
	m, _ = press(m, keys("k")...)
	if got := selectedTitle(m); got != "Document select for summary" {
		t.Fatalf("k over the gap: on %q", got)
	}
	m, _ = press(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelDown})
	if got := selectedTitle(m); got != "On hold until Monday 2026-10-05" {
		t.Fatalf("wheel over the gap: on %q", got)
	}

	// A click on the gap does nothing; the rows below it still hit.
	_, y := find(t, m, "+ Backlog")
	m, _ = press(m, click(5, y-1))
	if got := selectedTitle(m); got != "On hold until Monday 2026-10-05" || strings.Contains(screen(m), "Settings page") {
		t.Fatalf("click on the gap: on %q", got)
	}
	x, y := find(t, m, "Subscriptions list")
	m, _ = press(m, click(x, y))
	if got := selectedTitle(m); got != "Subscriptions list" {
		t.Fatalf("click below a gap selected %q", got)
	}
}

// Scrolled, a click still maps to the row drawn under it, gaps included.
func TestClickInScrolledListWithGaps(t *testing.T) {
	m, _ := newModel(t, calm(), 60, 14)
	m, _ = press(m, keys("jjjjjj")...)
	if m.listOff == 0 {
		t.Fatalf("list did not scroll:\n%s", screen(m))
	}
	x, y := find(t, m, "Subscriptions list")
	m, _ = press(m, click(x, y))
	if got := selectedTitle(m); got != "Subscriptions list" {
		t.Fatalf("click in a scrolled list selected %q:\n%s", got, screen(m))
	}
}

func TestThreadsNoTaskNames(t *testing.T) {
	s := calm()
	s.Threads = append(s.Threads,
		deck.Thread{ID: "t-0009", Title: "Spike", Status: deck.StatusWorking},
		deck.Thread{ID: "t-0010", Title: "Old work", Status: deck.StatusDone},
	)
	m, _ := newModel(t, s, 80, 40)
	out := screen(m)
	for _, want := range []string{listThreads, "◐ Spike", "+ Resolved (1)"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Old work") {
		t.Errorf("resolved threads should start folded")
	}
}

// With FigmaDesktop every way of opening a Figma link opens the desktop
// app; other links still open in the browser.
func TestFigmaDesktopSetting(t *testing.T) {
	const web = "https://www.figma.com/design/abc123/Document-select?node-id=1138-88367"
	const app = "figma://design/abc123/Document-select?node-id=1138-88367"
	for _, desktop := range []bool{false, true} {
		// Document select: Linear link 1, Figma links 2-4.
		m, o := newModelWith(t, calm(), 80, 30, func(o *Options) { o.FigmaDesktop = desktop })
		m, _ = press(m, keys("jj")...)
		want := web
		if desktop {
			want = app
		}
		m, _ = press(m, keys("f3")...) // a digit in the chooser
		m, _ = press(m, keys("3")...)  // a digit straight away
		m, _ = press(m, keys("ff")...) // the first
		m, _ = press(m, keys("fa")...) // all three
		if len(o.urls) != 6 || o.urls[0] != want || o.urls[1] != want || !slices.Contains(o.urls[3:], want) {
			t.Fatalf("FigmaDesktop %v: opened %q, want %s", desktop, o.urls, want)
		}
		for _, u := range o.urls {
			if strings.HasPrefix(u, "figma://") != desktop {
				t.Errorf("FigmaDesktop %v: opened %q", desktop, u)
			}
		}
		x, y := find(t, m, "3 1138-88367")
		m, _ = press(m, click(x+2, y))
		if got := o.urls[len(o.urls)-1]; len(o.urls) != 7 || got != want {
			t.Errorf("FigmaDesktop %v: click opened %q, want %s", desktop, o.urls[6:], want)
		}
		press(m, keys("1")...)
		if got := o.urls[len(o.urls)-1]; !strings.HasPrefix(got, "https://linear.app/") {
			t.Errorf("FigmaDesktop %v: 1 (a Linear link) opened %q", desktop, got)
		}
	}
}

func TestMouse(t *testing.T) {
	m, o := newModel(t, calm(), 80, 28)

	x, y := find(t, m, "Templates page")
	m, _ = press(m, click(x, y))
	if got := selectedTitle(m); got != "Templates page" {
		t.Fatalf("click selected %q", got)
	}

	m, _ = press(m, keys("k")...)
	x, y = find(t, m, "3 ABC-1257")
	m, _ = press(m, click(x+2, y))
	if len(o.urls) != 1 || !strings.HasSuffix(o.urls[0], "ABC-1257") {
		t.Fatalf("click on drawer link 3 opened %q", o.urls)
	}

	x, y = find(t, m, "+ Backlog")
	m, _ = press(m, click(x, y))
	if !strings.Contains(screen(m), "Settings page") {
		t.Fatalf("click on the Backlog heading did not unfold it")
	}

	m, _ = press(m, tea.MouseWheelMsg{X: 5, Y: 5, Button: tea.MouseWheelUp})
	if got := selectedTitle(m); got != "On hold until Monday 2026-10-05" && got != "Subscriptions list" {
		t.Errorf("wheel up over the list: on %q", got)
	}
}

func TestClickOnMissingCount(t *testing.T) {
	s := calm()
	s.Missing = []string{"inbox: permission denied"}
	m, _ := newModel(t, s, 60, 28)
	if m.mode != modeSources {
		t.Fatalf("the Sources view did not come up by itself")
	}
	m, _ = press(m, esc)
	if m.mode != modeRow {
		t.Fatalf("esc did not return")
	}
	x, y := find(t, m, "! 1")
	m, _ = press(m, click(x, y))
	if m.mode != modeSources {
		t.Fatalf("click on ! 1 did not show Sources")
	}
	m, _ = press(m, keys("!")...)
	if m.mode != modeRow {
		t.Fatalf("! did not toggle Sources off")
	}
	// It comes up by itself only once.
	m, _ = press(m, snapshotMsg(s))
	if m.mode != modeRow {
		t.Fatalf("Sources came up a second time")
	}
}

func TestEditorPaneAndReport(t *testing.T) {
	m, o := newModel(t, fakeSnap(), 80, 28)
	m, _ = press(m, keys("e")...)
	if len(o.dirs) != 1 || o.dirs[0] != "/src/worktrees/t-0002" {
		t.Fatalf("e opened %q", o.dirs)
	}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyEnter})
	if len(o.panes) != 1 || o.panes[0] != "w1Z:p1" || !strings.Contains(m.Status(), "focused t-0002") {
		t.Errorf("enter: focused %q, status %q", o.panes, m.Status())
	}
	m, _ = press(m, keys("r")...)
	if m.mode != modeReport || !strings.Contains(screen(m), "Phase 1 is done") {
		t.Fatalf("r did not show the report:\n%s", screen(m))
	}
	m, _ = press(m, esc)
	if m.mode != modeRow {
		t.Fatalf("esc did not leave the report")
	}
	m, _ = press(m, keys("jjj")...) // Templates page: no report, no worktree
	m, _ = press(m, keys("r")...)
	if m.mode != modeRow || !strings.Contains(m.Status(), "no report") {
		t.Errorf("r without a report: mode %v, status %q", m.mode, m.Status())
	}
	m, _ = press(m, keys("e")...)
	if len(o.dirs) != 1 || !strings.Contains(m.Status(), "no worktree") {
		t.Errorf("e without a worktree: opened %q, status %q", o.dirs, m.Status())
	}
}

func TestDrawerSizes(t *testing.T) {
	m, _ := newModel(t, calm(), 80, 28)
	// Half of the pane: the list keeps 11 rows; the drawer its rule, the
	// card, the tab bar between blank lines and 6 lines of content.
	if l := m.layout(); l.drawerH != 6 || l.listH != 11 || l.headN != 5 || l.tabY != l.headTop+3 {
		t.Fatalf("normal: list %d, head %d, drawer %d", l.listH, l.headN, l.drawerH)
	}
	m, _ = press(m, keys("z")...)
	if l := m.layout(); l.listH != 0 || l.drawerH != 19 {
		t.Fatalf("full: list %d, drawer %d", l.listH, l.drawerH)
	}
	m, _ = press(m, keys("z")...)
	if l := m.layout(); l.drawerH != 0 || l.listH != 23 {
		t.Fatalf("hidden: list %d, drawer %d", l.listH, l.drawerH)
	}
	// ? still shows help in the hidden drawer's place.
	m, _ = press(m, keys("?")...)
	if l := m.layout(); l.drawerH == 0 {
		t.Fatalf("help with the drawer hidden: no drawer")
	}
	m, _ = press(m, keys("z")...)
	if m.size != sizeNormal {
		t.Fatalf("z does not cycle back to normal")
	}
}

// A short pane drops the blank lines around the tab bar before the bar,
// and a full view keeps its head and a line of content.
func TestShortPaneKeepsTabBar(t *testing.T) {
	for _, h := range []int{8, 9, 10, 11, 12} {
		m, _ := newModel(t, calm(), 80, h)
		// Under 11 rows the normal drawer has room for the card only.
		if h >= 11 {
			l := m.layout()
			lines := strings.Split(ansi.Strip(m.View().Content), "\n")
			if l.tabY < 0 || l.tabY >= len(lines) || !strings.Contains(lines[l.tabY], "Overview") {
				t.Errorf("%d rows: tab bar not on line %d:\n%s", h, l.tabY, screen(m))
				continue
			}
			x, y := find(t, m, "Log 7")
			if m, _ = press(m, click(x+1, y)); m.curTab() != tabLog {
				t.Errorf("%d rows: click on Log: %v", h, m.curTab())
			}
		}
		m, _ = press(m, keys("r")...)
		if l := m.layout(); l.drawerH < 1 || !strings.Contains(screen(m), "esc returns") {
			t.Errorf("%d rows: report drawer %d lines:\n%s", h, l.drawerH, screen(m))
		}
	}
}

func TestLongListScrolls(t *testing.T) {
	s := calm()
	for i := range 30 {
		s.TaskLists[0].Tasks = append(s.TaskLists[0].Tasks, deck.Task{Title: "Task " + string(rune('A'+i%26)) + string(rune('a'+i/26))})
	}
	m, _ := newModel(t, s, 60, 20)
	m, _ = press(m, keys(strings.Repeat("j", 25))...)
	if !strings.Contains(screen(m), "▸· "+selectedTitle(m)) {
		t.Fatalf("selected row %q scrolled out of view:\n%s", selectedTitle(m), screen(m))
	}
	golden(t, "scrolled-60", m)
}

func TestNarrowerThanDesign(t *testing.T) {
	for _, w := range []int{40, 50} {
		m, _ := newModel(t, fakeSnap(), w, 20)
		for i, l := range strings.Split(m.View().Content, "\n") {
			if ansi.StringWidth(l) > w {
				t.Errorf("width %d: line %d is %d wide", w, i, ansi.StringWidth(l))
			}
		}
	}
}

func TestRefreshCoalesces(t *testing.T) {
	loads := 0
	m := New(deck.Snapshot{Project: deck.Project{Slug: "p"}}, Options{
		Load: func(context.Context) deck.Snapshot { loads++; return calm() },
		Now:  func() time.Time { return now },
	})
	if !strings.Contains(screen(m), "Reading p…") {
		t.Errorf("before the first load:\n%s", screen(m))
	}
	if m.Init() == nil {
		t.Fatal("Init with Load returns no command")
	}
	// Init's load counts as running, so a refresh before it ends waits;
	// cmd stands for that first load.
	cmd := m.load()
	next, again := m.Update(RefreshMsg{})
	m = next.(Model)
	if again != nil || !m.pending {
		t.Fatal("a second RefreshMsg during a load started another")
	}
	next, follow := m.Update(cmd())
	m = next.(Model)
	if follow == nil || m.pending || !m.loading {
		t.Fatal("the pending reload did not start after the first finished")
	}
	next, _ = m.Update(follow())
	m = next.(Model)
	if loads != 2 || m.loading || m.Snapshot().Project.Name != "Admin rebuild" {
		t.Fatalf("loads %d, loading %v, project %+v", loads, m.loading, m.Snapshot().Project)
	}
	if _, cmd := m.Update(tickMsg{}); cmd == nil {
		t.Fatal("tick did not reload and re-arm")
	}
}

func TestQuit(t *testing.T) {
	for _, k := range []tea.KeyPressMsg{{Code: 'q', Text: "q"}, {Code: 'c', Mod: tea.ModCtrl}} {
		m, _ := newModel(t, calm(), 80, 28)
		_, cmd := m.Update(k)
		if cmd == nil {
			t.Fatalf("%v: no command", k)
		}
		if _, ok := cmd().(tea.QuitMsg); !ok {
			t.Fatalf("%v: command did not quit", k)
		}
	}
}

func TestDrawerScrollStaysInContent(t *testing.T) {
	s := fakeSnap()
	s.Threads[1].Report = strings.Repeat("line\n\n", 60)
	m, _ := newModel(t, s, 80, 28)
	m, _ = press(m, keys("r")...)
	for range 20 {
		m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	}
	l := m.layout()
	if most := len(l.drawer.lines) - l.drawerH; m.drawerOff != most {
		t.Fatalf("drawerOff = %d after paging past the end, want %d", m.drawerOff, most)
	}
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyPgUp})
	if m.drawerOff >= len(l.drawer.lines)-l.drawerH {
		t.Fatalf("pgup at the end did not scroll back")
	}
}

func TestReportClosesWhenRefreshMovesCursor(t *testing.T) {
	m, _ := newModel(t, fakeSnap(), 80, 28)
	m, _ = press(m, keys("r")...)
	if m.mode != modeReport {
		t.Fatal("r did not open the report")
	}
	// t-0002 is answered and t-0004 needs the user now; the untouched
	// cursor moves there.
	s := calm()
	s.Threads[3].Status = deck.StatusNeedsYou
	m, _ = press(m, snapshotMsg(s))
	if got := selectedTitle(m); got != "Document select for summary" {
		t.Fatalf("cursor on %q", got)
	}
	if m.mode != modeRow {
		t.Fatalf("the report stayed open on %q", selectedTitle(m))
	}
}

func TestClickAfterGrowingUsesDrawnOffset(t *testing.T) {
	m, o := newModel(t, calm(), 80, 28)
	m, _ = press(m, tea.KeyPressMsg{Code: tea.KeyPgDown})
	m, _ = press(m, tea.WindowSizeMsg{Width: 80, Height: 60})
	x, y := find(t, m, "1 ABC-1246")
	press(m, click(x, y))
	if len(o.urls) != 1 || !strings.HasSuffix(o.urls[0], "ABC-1246") {
		t.Fatalf("click on link 1 after growing opened %q", o.urls)
	}
}

func TestFocusPane(t *testing.T) {
	enter := tea.KeyPressMsg{Code: tea.KeyEnter}

	// With herdr's live state, the live pane wins over the recorded one.
	s := fakeSnap()
	s.Herdr = true
	s.Project.PaneID = "w1X:p1"
	s.Threads[1].Pane = &deck.Pane{ID: "w30:p2", AgentStatus: "blocked"}
	m, o := newModel(t, s, 80, 28)
	press(m, enter)
	if len(o.panes) != 1 || o.panes[0] != "w30:p2" {
		t.Fatalf("enter on t-0002 focused %q", o.panes)
	}

	s.Threads[1].Pane = nil
	// On the thread's own row, a closed pane is said, not focused.
	m, o = newModel(t, s, 80, 28)
	m, _ = press(m, enter)
	if len(o.panes) != 0 || !strings.Contains(m.Status(), "t-0002 has no open pane") {
		t.Fatalf("enter on a thread without a pane: focused %q, status %q", o.panes, m.Status())
	}

	// A failed focus says why.
	m = New(fakeSnap(), Options{FocusPane: func(string) error { return errors.New("pane gone") }})
	m, _ = press(m, enter)
	if !strings.Contains(m.Status(), "could not focus t-0002's pane w1Z:p1: pane gone") {
		t.Fatalf("failed focus: status %q", m.Status())
	}
}

func TestDrawerLivePane(t *testing.T) {
	s := fakeSnap()
	s.Herdr = true
	s.Threads[1].Pane = &deck.Pane{ID: "w1Z:p1", Agent: "claude", AgentStatus: "blocked"}
	m, _ := newModel(t, s, 80, 28)
	m, _ = press(m, keys("z")...)
	out := screen(m)
	for _, want := range []string{"pane     w1Z:p1 · claude blocked", "● needs you  ~95%"} {
		if !strings.Contains(out, want) {
			t.Errorf("drawer lacks %q:\n%s", want, out)
		}
	}

	s.Threads[1].Pane = nil
	m, _ = newModel(t, s, 80, 28)
	m, _ = press(m, keys("z")...)
	if out := screen(m); !strings.Contains(out, "w1Z:p1 closed") {
		t.Errorf("drawer does not say the pane is closed:\n%s", out)
	}
}

func TestSelectionHighlight(t *testing.T) {
	selLine := func(m Model) string {
		for _, l := range strings.Split(m.View().Content, "\n") {
			if strings.Contains(ansi.Strip(l), "▸") {
				return l
			}
		}
		t.Fatalf("no selected row:\n%s", screen(m))
		return ""
	}
	m, _ := newModel(t, fakeSnap(), 80, 28) // the cursor rests on t-0002, which needs the user
	l := selLine(m)
	if strings.Contains(l, "\x1b[7m") || strings.Contains(l, ";7m") {
		t.Errorf("the selected row is still reverse video: %q", l)
	}
	if !strings.Contains(l, "48;5;237m") {
		t.Errorf("the selected row has no dark grey background: %q", l)
	}
	if !strings.Contains(l, "31m") {
		t.Errorf("the selected row lost its red: %q", l)
	}
	if !strings.HasSuffix(l, "\x1b[m") || ansi.StringWidth(l) != 80 {
		t.Errorf("the highlight does not span the row: width %d, %q", ansi.StringWidth(l), l)
	}

	m, _ = press(m, tea.BackgroundColorMsg{Color: color.White})
	if l := selLine(m); !strings.Contains(l, "48;5;254m") || strings.Contains(l, "48;5;237m") {
		t.Errorf("on a light terminal the highlight is not light grey: %q", l)
	}

	// The link chooser highlights its kind's chips the same way.
	m, _ = press(m, keys("l")...)
	for _, l := range strings.Split(m.View().Content, "\n") {
		if strings.Contains(ansi.Strip(l), "[1 ABC-1246 done]") && !strings.Contains(l, "\x1b[48;5;254m\x1b[2m[") {
			t.Errorf("the chooser's chips are not highlighted:\n%q", l)
		}
	}
}

func TestFooterVersionYieldsToHelp(t *testing.T) {
	const help = "1-9 link  o open  ↵ pane  [ ] tab  z drawer  ? help"
	for _, tt := range []struct {
		name, version string
		shown         bool
	}{
		{"fits", testVersion, true},
		{"too long", "v1.2.3-45-gabcdef0", false},
		{"empty", "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := newModel(t, calm(), 60, 20)
			m.opt.Version = tt.version
			lines := strings.Split(screen(m), "\n")
			foot := lines[len(lines)-1]
			if !strings.Contains(foot, help) {
				t.Errorf("footer %q lost key help %q", foot, help)
			}
			if got := tt.version != "" && strings.HasSuffix(foot, tt.version); got != tt.shown {
				t.Errorf("footer %q: version shown = %v, want %v", foot, got, tt.shown)
			}
			if w := ansi.StringWidth(foot); w > 60 {
				t.Errorf("footer is %d wide", w)
			}
		})
	}
}

func TestRestartWhenBinaryChanges(t *testing.T) {
	calls := 0
	changed := false
	m := New(calm(), Options{
		Now:           func() time.Time { return now },
		BinaryChanged: func(context.Context) bool { calls++; return changed },
	})
	if m.Init() == nil {
		t.Fatal("Init with BinaryChanged returns no command")
	}
	cmd := m.checkBinary()
	if cmd == nil {
		t.Fatal("no binary check on the tick")
	}
	if again := m.checkBinary(); again != nil {
		t.Fatal("a second check started while one ran")
	}
	next, quit := m.Update(cmd())
	m = next.(Model)
	if quit != nil || m.Restart() {
		t.Fatal("an unchanged binary quit the deck")
	}
	changed = true
	cmd = m.checkBinary()
	next, quit = m.Update(cmd())
	m = next.(Model)
	if !m.Restart() || quit == nil {
		t.Fatal("a changed binary did not ask for a restart")
	}
	if _, ok := quit().(tea.QuitMsg); !ok {
		t.Fatal("the restart did not quit the program")
	}
	if calls != 2 {
		t.Errorf("BinaryChanged ran %d times, want 2", calls)
	}
	if m.checkBinary() != nil {
		t.Error("a check started after the restart was asked for")
	}
}

func TestUpdateHint(t *testing.T) {
	m, _ := newModel(t, calm(), 80, 28)
	m.opt.CheckUpdate = func(context.Context) Update { return Update{Available: "v0.2.0"} }
	if m.Init() == nil {
		t.Fatal("Init with CheckUpdate returns no command")
	}
	// Update, not press: the next check's hour-long tick must not run.
	next, _ := m.Update(m.checkUpdate()())
	m = next.(Model)
	header := strings.Split(screen(m), "\n")[0]
	if !strings.Contains(header, "↑ v0.2.0") {
		t.Errorf("header = %q, want the update hint", header)
	}
	m, _ = press(m, keys("!")...)
	if s := screen(m); !strings.Contains(s, "herdr-deck v0.2.0 is available: run `herdr-deck update`") {
		t.Errorf("Sources view does not explain the update:\n%s", s)
	}

	// A failed check is silent in the header and shows in Sources.
	m, _ = newModel(t, calm(), 80, 28)
	next, _ = m.Update(updateMsg{Problem: "git ls-remote: could not resolve host"})
	m = next.(Model)
	before, _ := newModel(t, calm(), 80, 28)
	if got, want := strings.Split(screen(m), "\n")[0], strings.Split(screen(before), "\n")[0]; got != want {
		t.Errorf("header with a failed check = %q, want it unchanged %q", got, want)
	}
	m, _ = press(m, keys("!")...)
	if s := screen(m); !strings.Contains(s, "update check: git ls-remote: could not resolve host") {
		t.Errorf("Sources view does not show the failed check:\n%s", s)
	}
}

func TestRestartFailedHint(t *testing.T) {
	m := New(calm(), Options{Now: func() time.Time { return now }, RestartFailed: "exec format error"})
	m, _ = press(m, tea.WindowSizeMsg{Width: 80, Height: 28})
	if header := strings.Split(screen(m), "\n")[0]; !strings.Contains(header, "↻ restart failed") {
		t.Errorf("header = %q, want the restart hint", header)
	}
	m, _ = press(m, keys("!")...)
	if s := screen(m); !strings.Contains(s, "restarting into it failed (exec format") {
		t.Errorf("Sources view does not say why the restart failed:\n%s", s)
	}
}

// A Linear link's status shows next to its ID, coloured by its type; a
// done issue is dim as a whole.
func TestLinearIssueStatus(t *testing.T) {
	m, _ := newModel(t, calm(), 80, 28)
	view := m.View().Content
	if !strings.Contains(view, workStyle.Render("in progress")) {
		t.Error("a started issue's state is not cyan")
	}
	if !strings.Contains(view, dim.Render("done")+dim.Render("]")) {
		t.Error("a done issue is not dim")
	}

	for _, tt := range []struct {
		issue deck.Issue
		style lipgloss.Style
		text  string
	}{
		{deck.Issue{State: "In Progress", StateType: "started"}, workStyle, "in progress"},
		{deck.Issue{State: "In Review", StateType: "started"}, reviewStyle, "in review"},
		{deck.Issue{State: "Triage", StateType: "triage"}, warnStyle, "triage"},
		{deck.Issue{State: "Todo", StateType: "unstarted"}, plain, "todo"},
		{deck.Issue{State: "Backlog", StateType: "backlog"}, plain, "backlog"},
		{deck.Issue{State: "Done", StateType: "completed"}, dim, "done"},
		{deck.Issue{State: "Canceled", StateType: "canceled"}, dim, "canceled"},
		{deck.Issue{State: "Waiting for QA sign-off", StateType: "started"}, workStyle, "waiting fo…"},
	} {
		if got := issueStyle(tt.issue).Render(shortState(tt.issue.State)); got != tt.style.Render(tt.text) {
			t.Errorf("%+v renders %q, want %q", tt.issue, got, tt.style.Render(tt.text))
		}
	}
}

// changes is a thread's diff with every kind of file, more than the Files
// section lists.
func changes() deck.Diff {
	return deck.Diff{
		Base:      "origin/main",
		MergeBase: "4b825dc",
		Files: []deck.DiffFile{
			{Path: "apps/admin/src/pages/users/UsersOverviewPage.tsx", Added: 214, Deleted: 12},
			{Path: "apps/admin/src/pages/users/columns.ts", Change: deck.ChangeAdded, Added: 48},
			{Path: "apps/admin/src/api/users.ts", Added: 31, Deleted: 9},
			{Path: "apps/admin/src/pages/users/index.ts", OldPath: "apps/admin/src/pages/members/index.ts", Change: deck.ChangeRenamed, Added: 1, Deleted: 1},
			{Path: "apps/admin/public/empty-state.png", Change: deck.ChangeAdded, Binary: true},
			{Path: "apps/admin/src/routes.tsx", Added: 6, Deleted: 2},
			{Path: "docs/users-page.md", Change: deck.ChangeAdded, Added: 19, Untracked: true},
			{Path: "package.json", Added: 1, Deleted: 1},
			{Path: "pnpm-lock.yaml", Added: 102, Deleted: 40},
			{Path: "scripts/seed-users.ts", Change: deck.ChangeAdded, Added: 25, Untracked: true},
			{Path: "tsconfig.json", Change: deck.ChangeDeleted, Deleted: 3},
		},
	}
}

// withDiffs gives the model a Diff that answers by worktree and counts its
// calls.
func withDiffs(diffs map[string]deck.Diff, calls *[]string) func(*Options) {
	return func(o *Options) {
		o.Diff = func(_ context.Context, t deck.Thread) deck.Diff {
			if calls != nil {
				*calls = append(*calls, t.ID)
			}
			return diffs[t.Worktree]
		}
	}
}

func TestFilesGolden(t *testing.T) {
	gone := deck.Diff{Base: "origin/main", Note: "the worktree is gone"}
	cases := []struct {
		name string
		diff deck.Diff
		tree bool
		keys []tea.Msg
	}{
		{name: "files", diff: changes(), keys: keys("d")},
		{name: "files-full", diff: changes(), keys: keys("dz")},
		{name: "files-note", diff: gone, keys: keys("d")},
		{name: "files-tree", diff: changes(), tree: true, keys: keys("d")},
		{name: "files-tree-full", diff: changes(), keys: keys("dtz")},
		{name: "files-cursor", diff: changes(), keys: keys("dzjjj")},
	}
	for _, c := range cases {
		for _, w := range []int{80, 60} {
			name := c.name + "-" + map[int]string{80: "80", 60: "60"}[w]
			t.Run(name, func(t *testing.T) {
				m, _ := newModelWith(t, deck.Snapshot{}, w, 28, func(o *Options) {
					withDiffs(map[string]deck.Diff{"/src/worktrees/t-0002": c.diff}, nil)(o)
					o.DiffTree = c.tree
				})
				m, _ = press(m, snapshotMsg(calm()))
				m, _ = press(m, c.keys...)
				golden(t, name, m)
			})
		}
	}
}

func TestFilesOpenDiffs(t *testing.T) {
	m, o := newModelWith(t, deck.Snapshot{}, 80, 40, withDiffs(map[string]deck.Diff{"/src/worktrees/t-0002": changes()}, nil))
	m, _ = press(m, snapshotMsg(calm()))
	m, _ = press(m, keys("zd")...) // full height: every file shows
	if !strings.Contains(screen(m), "11 files  +447 -68  vs origin/main") || !strings.Contains(screen(m), "tsconfig.json") {
		t.Fatalf("no Files tab:\n%s", screen(m))
	}
	// Without a preview, a digit opens the file in the diff tool; d in
	// the focused drawer the file under the cursor, a the whole diff.
	m, _ = press(m, keys("2")...)
	m, _ = press(m, keys("d")...)
	m, _ = press(m, keys("a")...)
	m, _ = press(m, esc)
	m, _ = press(m, keys("9")...)
	want := []string{
		"/src/worktrees/t-0002 4b825dc apps/admin/src/pages/users/columns.ts",
		"/src/worktrees/t-0002 4b825dc apps/admin/src/pages/users/columns.ts",
		"/src/worktrees/t-0002 4b825dc",
		"/src/worktrees/t-0002 4b825dc pnpm-lock.yaml",
	}
	if !slices.Equal(o.diffs, want) {
		t.Errorf("diffs %q, want %q", o.diffs, want)
	}
	if m.Status() != "opened the diff of pnpm-lock.yaml" {
		t.Errorf("status %q", m.Status())
	}
	// A click on a file opens it; on the total line, the whole diff.
	o.diffs = nil
	x, y := find(t, m, "routes.tsx")
	m, _ = press(m, click(x, y))
	_, y = find(t, m, "11 files")
	m, _ = press(m, click(40, y))
	if want := []string{"/src/worktrees/t-0002 4b825dc apps/admin/src/routes.tsx", "/src/worktrees/t-0002 4b825dc"}; !slices.Equal(o.diffs, want) {
		t.Errorf("clicks opened %q, want %q", o.diffs, want)
	}
	// A rename opens with both paths; an untracked file does not open.
	o.diffs = nil
	m, _ = press(m, keys("4")...)
	m, _ = press(m, keys("7")...)
	if want := []string{"/src/worktrees/t-0002 4b825dc apps/admin/src/pages/members/index.ts apps/admin/src/pages/users/index.ts"}; !slices.Equal(o.diffs, want) {
		t.Errorf("rename opened %q, want %q", o.diffs, want)
	}
	if m.Status() != "docs/users-page.md is untracked: git diff shows it once it is added" {
		t.Errorf("untracked status %q", m.Status())
	}
	// The digits are links again on Overview.
	_, _ = press(m, keys("[[1")...)
	if len(o.urls) != 1 {
		t.Errorf("1 on Overview opened %q", o.urls)
	}
}

// In the tree view the digits and clicks follow the tree's numbering, and
// t on the Files tab switches views.
func TestFilesTreeOpensByDisplayOrder(t *testing.T) {
	m, o := newModelWith(t, deck.Snapshot{}, 80, 40, withDiffs(map[string]deck.Diff{"/src/worktrees/t-0002": changes()}, nil))
	m, _ = press(m, snapshotMsg(calm()))
	m, _ = press(m, keys("z")...)
	m, _ = press(m, keys("dt")...)
	if !m.tree || m.curTab() != tabFiles {
		t.Fatalf("d t: tree %v, tab %v", m.tree, m.curTab())
	}
	if !strings.Contains(screen(m), "pages/users/") || !strings.Contains(screen(m), "t list") {
		t.Fatalf("no tree:\n%s", screen(m))
	}
	m, _ = press(m, keys("2")...)
	m, _ = press(m, keys("5")...)
	x, y := find(t, m, "routes.tsx")
	m, _ = press(m, click(x, y))
	want := []string{
		"/src/worktrees/t-0002 4b825dc apps/admin/src/api/users.ts",
		"/src/worktrees/t-0002 4b825dc apps/admin/src/pages/members/index.ts apps/admin/src/pages/users/index.ts",
		"/src/worktrees/t-0002 4b825dc apps/admin/src/routes.tsx",
	}
	if !slices.Equal(o.diffs, want) {
		t.Errorf("diffs %q, want %q", o.diffs, want)
	}
	// A click on a folder line opens nothing.
	o.diffs = nil
	x, y = find(t, m, "pages/users/")
	m, _ = press(m, click(x, y))
	if len(o.diffs) != 0 {
		t.Errorf("a folder click opened %q", o.diffs)
	}
	// t again is the list, numbered as git lists the files.
	m, _ = press(m, keys("t")...)
	m, _ = press(m, keys("2")...)
	if m.tree || len(o.diffs) != 1 || o.diffs[0] != "/src/worktrees/t-0002 4b825dc apps/admin/src/pages/users/columns.ts" {
		t.Errorf("back in the list: tree %v, diffs %q", m.tree, o.diffs)
	}
}

func TestFilesKeyEdges(t *testing.T) {
	one := deck.Diff{Base: "origin/main", MergeBase: "abc", Files: []deck.DiffFile{{Path: "a.go", Added: 1}}}
	none := deck.Diff{Base: "origin/main", MergeBase: "abc"}
	diffs := map[string]deck.Diff{"/src/worktrees/t-0002": one, "/src/worktrees/t-0003": none}
	snap := calm()
	snap.Threads[2].Worktree = "/src/worktrees/t-0003"
	m, o := newModelWith(t, deck.Snapshot{}, 80, 28, withDiffs(diffs, nil))
	m, _ = press(m, snapshotMsg(snap))
	// d focuses Files, even with one file; a digit opens it.
	m, _ = press(m, keys("d")...)
	if len(o.diffs) != 0 || !m.dfocus || m.curTab() != tabFiles {
		t.Errorf("d: diffs %q, focus %v, tab %v", o.diffs, m.dfocus, m.curTab())
	}
	m, _ = press(m, keys("1")...)
	if !slices.Equal(o.diffs, []string{"/src/worktrees/t-0002 abc a.go"}) {
		t.Errorf("one file: %q", o.diffs)
	}
	m, _ = press(m, esc)
	m, _ = press(m, keys("jd")...)
	if !strings.Contains(screen(m), "no changes against origin/main") {
		t.Errorf("no changes:\n%s", screen(m))
	}
	// Without herdr the opener fails, and the footer says why.
	m, _ = press(m, esc)
	m, _ = press(m, keys("k")...)
	m.opt.OpenDiff = func(string, string, []string) error { return errors.New("a terminal program needs herdr") }
	m, _ = press(m, keys("d1")...)
	if m.Status() != "could not open the diff of a.go: a terminal program needs herdr" {
		t.Errorf("status %q", m.Status())
	}
}

func TestFilesHiddenForResolvedAndInbox(t *testing.T) {
	var calls []string
	snap := fakeSnap()
	m, _ := newModelWith(t, deck.Snapshot{}, 80, 28, withDiffs(map[string]deck.Diff{"/src/worktrees/t-0002": changes()}, &calls))
	m, _ = press(m, snapshotMsg(snap))
	// The cursor rests on t-0002, which needs the user.
	if len(calls) != 1 || calls[0] != "t-0002" {
		t.Fatalf("calls %q", calls)
	}
	snap.Threads[1].Status = deck.StatusDone
	m, _ = press(m, snapshotMsg(snap))
	if strings.Contains(screen(m), "Files 11") {
		t.Errorf("a resolved thread shows its files:\n%s", screen(m))
	}
	if len(calls) != 1 {
		t.Errorf("read a resolved thread's diff: %q", calls)
	}
}

func TestDiffReadsFollowSelection(t *testing.T) {
	var calls []string
	snap := calm()
	snap.Threads[2].Worktree = "/src/worktrees/t-0003"
	diffs := map[string]deck.Diff{"/src/worktrees/t-0002": changes(), "/src/worktrees/t-0003": {Base: "origin/main"}}
	m, _ := newModelWith(t, deck.Snapshot{}, 80, 28, withDiffs(diffs, &calls))
	m, _ = press(m, snapshotMsg(snap))
	m, _ = press(m, keys("j")...) // t-0003
	m, _ = press(m, keys("k")...) // t-0002 again
	m, _ = press(m, keys("z")...) // no move: no read
	m, _ = press(m, snapshotMsg(snap))
	if want := []string{"t-0002", "t-0003", "t-0002", "t-0002"}; !slices.Equal(calls, want) {
		t.Errorf("calls %q, want %q", calls, want)
	}
	// A move while a read runs reads again when it ends, for the row
	// selected then.
	calls = nil
	next, cmd := m.Update(keys("j")[0])
	m = next.(Model)
	next, _ = m.Update(keys("j")[0])
	m = next.(Model)
	_ = run(m, cmd)
	if want := []string{"t-0003"}; !slices.Equal(calls, want) {
		t.Errorf("calls %q, want %q", calls, want)
	}
}

func TestDevReason(t *testing.T) {
	alive, gone := &deck.DevUp{Alive: true}, &deck.DevUp{}
	for _, c := range []struct {
		note string
		up   *deck.DevUp
		want string
	}{
		{deck.DevNoWorktree, gone, "worktree not found"},
		{"invalid .config/dev.json · ! shows why", alive, "invalid .config/dev.json · ! shows why"},
		{deck.DevNotStarted, alive, "starting…"},
		{"ports not known yet", gone, "dev exited · see the log"},
		{"ports not known yet · ! shows why", alive, "ports not known yet · ! shows why"},
		{deck.DevNotStarted, nil, "not started · u starts it"},
		{"no .config/dev.json", nil, "no .config/dev.json"},
		{"", nil, ""},
	} {
		if got := devReason(deck.Thread{DevNote: c.note, DevUp: c.up}); got != c.want {
			t.Errorf("devReason(%q, %+v) = %q, want %q", c.note, c.up, got, c.want)
		}
	}
	// A dev command that exits once its services run is not a failure.
	daemon := deck.Thread{DevUp: gone, DevServers: []deck.DevServer{{Name: "db", Running: true}}}
	if got := devReason(daemon); got != "" {
		t.Errorf("devReason with a running server = %q, want none", got)
	}
}
