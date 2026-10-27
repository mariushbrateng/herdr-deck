package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/FredricW/herdr-deck/internal/source/herdr"
)

// fakeHost records what the hooks ask herdr to do. Open adds the deck pane
// to the state, split right of its target in a 120-column tab.
type fakeHost struct {
	st     herdr.State
	layout Layout
	opened []Open
	closed []string
	focus  []string
	left   map[string]string // LeftOf answers
	marks  map[string]string
	ratios []string
	notes  []string
	next   int
}

func (f *fakeHost) Snapshot(context.Context) (herdr.State, error) { return f.st, nil }

func (f *fakeHost) Open(_ context.Context, o Open) (string, error) {
	f.opened = append(f.opened, o)
	f.next++
	id := fmt.Sprintf("w9:p%d", 100+f.next)
	target, _ := findPane(f.st, o.Target)
	f.st.Panes = append(f.st.Panes, herdr.Pane{ID: id, WorkspaceID: target.WorkspaceID, TabID: target.TabID, Cwd: o.Cwd})
	f.layout = Layout{
		Root: Node{Type: "split", Direction: "right", Ratio: 0.5,
			First:  &Node{Type: "pane", PaneID: o.Target},
			Second: &Node{Type: "pane", PaneID: id}},
		Rects: map[string]Rect{o.Target: {Width: 100}, id: {X: 100, Width: 100}},
	}
	return id, nil
}

func (f *fakeHost) Close(_ context.Context, id string) error {
	f.closed = append(f.closed, id)
	return nil
}

func (f *fakeHost) Focus(_ context.Context, id string) error {
	f.focus = append(f.focus, id)
	return nil
}

func (f *fakeHost) LeftOf(_ context.Context, id string) (string, error) { return f.left[id], nil }

func (f *fakeHost) Mark(_ context.Context, id, slug string) error {
	if f.marks == nil {
		f.marks = map[string]string{}
	}
	f.marks[id] = slug
	for i, p := range f.st.Panes {
		if p.ID == id {
			f.st.Panes[i].Tokens = map[string]string{Token: slug}
		}
	}
	return nil
}

func (f *fakeHost) Layout(context.Context, string) (Layout, error) { return f.layout, nil }

func (f *fakeHost) SetRatio(_ context.Context, id string, path []bool, r float64) error {
	f.ratios = append(f.ratios, fmt.Sprintf("%s %v %.2f", id, path, r))
	return nil
}

func (f *fakeHost) Notify(_ context.Context, title, _ string) error {
	f.notes = append(f.notes, title)
	return nil
}

// projectsRoot makes a projects root with an admin-rebuild project and a
// folder without PROJECT.md.
func projectsRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, dir := range []string{"admin-rebuild/threads/t-0002", "not-a-project", ".state"} {
		if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "admin-rebuild", "PROJECT.md"), []byte("+++\nname = \"Admin rebuild\"\n+++\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

func noEnv(string) string { return "" }

func TestAgentDetectedOpensNextToCoordinator(t *testing.T) {
	root := projectsRoot(t)
	h := &fakeHost{st: herdr.State{Panes: []herdr.Pane{
		{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: filepath.Join(root, "admin-rebuild"), Agent: "claude"},
	}}}
	deck, err := AgentDetected(context.Background(), h, Env{Getenv: noEnv, Root: root}, Event{PaneID: "w1:p1", Agent: "claude"})
	if err != nil || deck == "" {
		t.Fatalf("AgentDetected = %q, %v", deck, err)
	}
	want := Open{Target: "w1:p1", Cwd: filepath.Join(root, "admin-rebuild"), Env: map[string]string{
		"HERDR_DECK_PROJECT": "admin-rebuild", "HERDR_PROJECTS_ROOT": root,
	}}
	if len(h.opened) != 1 || !reflect.DeepEqual(h.opened[0], want) {
		t.Fatalf("opened %+v, want %+v", h.opened, want)
	}
	if h.marks[deck] != "admin-rebuild" {
		t.Errorf("marks = %v", h.marks)
	}
	// The coordinator keeps focus.
	if h.opened[0].Focus || len(h.focus) != 0 {
		t.Errorf("deck focused: open %+v, focus %v", h.opened[0], h.focus)
	}
	// 200 columns: the deck gets 80, the coordinator 120.
	if want := []string{deck + " [] 0.60"}; !reflect.DeepEqual(h.ratios, want) {
		t.Errorf("ratios = %v, want %v", h.ratios, want)
	}
}

func TestAgentDetectedSkips(t *testing.T) {
	root := projectsRoot(t)
	coord := herdr.Pane{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: filepath.Join(root, "admin-rebuild"), Agent: "claude"}
	at := func(cwd string) []herdr.Pane { return []herdr.Pane{{ID: "w1:p1", WorkspaceID: "w1", Cwd: cwd}} }
	claude := Event{PaneID: "w1:p1", Agent: "claude"}
	tests := []struct {
		name  string
		panes []herdr.Pane
		ev    Event
	}{
		{name: "thread under threads/", panes: at(filepath.Join(root, "admin-rebuild/threads/t-0002")), ev: claude},
		{name: "thread in a worktree", panes: at("/src/webshop-worktrees/abc-123"), ev: claude},
		{name: "folder without PROJECT.md", panes: at(filepath.Join(root, "not-a-project")), ev: claude},
		{name: "hidden folder", panes: at(filepath.Join(root, ".state")), ev: claude},
		{name: "projects root itself", panes: at(root), ev: claude},
		{name: "workspace has a deck", panes: []herdr.Pane{coord, {ID: "w1:p7", WorkspaceID: "w1", TabID: "w1:t2", Tokens: map[string]string{Token: "admin-rebuild"}}}, ev: claude},
		{name: "agent released", panes: []herdr.Pane{coord}, ev: Event{PaneID: "w1:p1", Agent: "claude", Released: true}},
		{name: "no agent", panes: []herdr.Pane{coord}, ev: Event{PaneID: "w1:p1"}},
		{name: "pane gone", panes: nil, ev: claude},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &fakeHost{st: herdr.State{Panes: tt.panes}}
			deck, err := AgentDetected(context.Background(), h, Env{Getenv: noEnv, Root: root}, tt.ev)
			if err != nil || deck != "" || len(h.opened) != 0 {
				t.Fatalf("AgentDetected = %q, %v; opened %v", deck, err, h.opened)
			}
		})
	}
}

func TestAgentDetectedResolvesSymlinkedRoot(t *testing.T) {
	root := projectsRoot(t)
	link := filepath.Join(t.TempDir(), "projects")
	if err := os.Symlink(root, link); err != nil {
		t.Fatal(err)
	}
	real, _ := filepath.EvalSymlinks(root)
	h := &fakeHost{st: herdr.State{Panes: []herdr.Pane{
		{ID: "w1:p1", WorkspaceID: "w1", Cwd: filepath.Join(real, "admin-rebuild"), Agent: "claude"},
	}}}
	if deck, err := AgentDetected(context.Background(), h, Env{Getenv: noEnv, Root: link}, Event{PaneID: "w1:p1", Agent: "claude"}); err != nil || deck == "" {
		t.Fatalf("AgentDetected = %q, %v", deck, err)
	}
}

func TestToggle(t *testing.T) {
	root := projectsRoot(t)
	coord := herdr.Pane{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: filepath.Join(root, "admin-rebuild"), Agent: "claude"}
	deckPane := herdr.Pane{ID: "w1:p2", WorkspaceID: "w1", TabID: "w1:t1", Tokens: map[string]string{Token: "admin-rebuild"}}
	otherTab := herdr.Pane{ID: "w1:p3", WorkspaceID: "w1", TabID: "w1:t2", Cwd: filepath.Join(root, "admin-rebuild")}
	worktree := herdr.Pane{ID: "w2:p1", WorkspaceID: "w2", TabID: "w2:t1", Cwd: "/src/webshop-worktrees/abc-123", Tokens: map[string]string{"hp_project": "admin-rebuild"}}
	bare := herdr.Pane{ID: "w3:p1", WorkspaceID: "w3", TabID: "w3:t1", Cwd: "/src/elsewhere"}
	deckRightOfCoord := map[string]string{"w1:p2": "w1:p1"}

	tests := []struct {
		name       string
		panes      []herdr.Pane
		left       map[string]string
		focused    string
		env        map[string]string
		wantClose  []string
		wantFocus  []string // Focus calls after the deck opened or closed
		wantOpen   string   // the slug opened, or ""
		wantErr    bool
		wantNotice bool
	}{
		{name: "opens from the project folder", panes: []herdr.Pane{coord}, focused: "w1:p1", wantOpen: "admin-rebuild"},
		{name: "focuses the tab's deck", panes: []herdr.Pane{coord, deckPane}, focused: "w1:p1", wantFocus: []string{"w1:p2"}},
		{name: "closes a focused deck, focus goes left", panes: []herdr.Pane{coord, deckPane}, left: deckRightOfCoord, focused: "w1:p2", wantClose: []string{"w1:p2"}, wantFocus: []string{"w1:p1"}},
		{name: "closes a focused deck with nothing left of it", panes: []herdr.Pane{deckPane}, focused: "w1:p2", wantClose: []string{"w1:p2"}},
		{name: "a deck in another tab is not this tab's", panes: []herdr.Pane{deckPane, otherTab}, focused: "w1:p3", wantOpen: "admin-rebuild"},
		{name: "opens from hp_project", panes: []herdr.Pane{worktree}, focused: "w2:p1", wantOpen: "admin-rebuild"},
		{name: "opens from HERDR_DECK_PROJECT", panes: []herdr.Pane{bare}, focused: "w3:p1", env: map[string]string{"HERDR_DECK_PROJECT": "admin-rebuild"}, wantOpen: "admin-rebuild"},
		{name: "no project", panes: []herdr.Pane{bare}, focused: "w3:p1", wantErr: true, wantNotice: true},
		{name: "unknown pane", panes: []herdr.Pane{bare}, focused: "w9:p9", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &fakeHost{st: herdr.State{Panes: tt.panes}, left: tt.left}
			env := Env{Getenv: func(k string) string { return tt.env[k] }, Root: root}
			err := Toggle(context.Background(), h, env, tt.focused)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(h.closed, tt.wantClose) {
				t.Errorf("closed %v, want %v", h.closed, tt.wantClose)
			}
			if !reflect.DeepEqual(h.focus, tt.wantFocus) {
				t.Errorf("focus %v, want %v", h.focus, tt.wantFocus)
			}
			var opened string
			if len(h.opened) == 1 {
				opened = h.opened[0].Env["HERDR_DECK_PROJECT"]
				if h.opened[0].Target != tt.focused {
					t.Errorf("opened next to %s, want %s", h.opened[0].Target, tt.focused)
				}
				// A deck opened with the key takes focus.
				if !h.opened[0].Focus {
					t.Error("opened without focus")
				}
			}
			if opened != tt.wantOpen || len(h.opened) > 1 {
				t.Errorf("opened %+v, want %q", h.opened, tt.wantOpen)
			}
			if (len(h.notes) > 0) != tt.wantNotice {
				t.Errorf("notes = %v", h.notes)
			}
		})
	}
}

func TestRunReadsHerdrEnv(t *testing.T) {
	root := projectsRoot(t)
	h := &fakeHost{st: herdr.State{Panes: []herdr.Pane{
		{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: filepath.Join(root, "admin-rebuild"), Agent: "claude"},
	}}}
	vars := map[string]string{
		// The shape herdr 0.9.3 passes to an event hook.
		"HERDR_PLUGIN_EVENT_JSON": `{"event":"pane_agent_detected","data":{"type":"pane_agent_detected","pane_id":"w1:p1","workspace_id":"w1","agent":"claude"}}`,
		"HERDR_PLUGIN_STATE_DIR":  t.TempDir(),
	}
	env := Env{Getenv: func(k string) string { return vars[k] }, Root: root}
	if err := Run(context.Background(), h, env, []string{"agent-detected"}); err != nil || len(h.opened) != 1 {
		t.Fatalf("agent-detected: %v, opened %v", err, h.opened)
	}
	// The action's context names the focused pane; the deck is in its tab now.
	vars["HERDR_PLUGIN_CONTEXT_JSON"] = `{"workspace_id":"w1","tab_id":"w1:t1","focused_pane_id":"w1:p1","invocation_source":"keybinding"}`
	if err := Run(context.Background(), h, env, []string{"toggle"}); err != nil || len(h.focus) != 1 || h.focus[0] != h.st.Panes[1].ID {
		t.Fatalf("toggle: %v, focus %v", err, h.focus)
	}
	if err := Run(context.Background(), h, env, []string{"bogus"}); err == nil {
		t.Error("unknown command: no error")
	}
}

func TestMarkSelf(t *testing.T) {
	h := &fakeHost{}
	vars := map[string]string{"HERDR_PLUGIN_ID": ID, "HERDR_PLUGIN_ENTRYPOINT_ID": Entrypoint, "HERDR_PANE_ID": "w1:p2"}
	MarkSelf(context.Background(), h, func(k string) string { return vars[k] }, "admin-rebuild")
	if h.marks["w1:p2"] != "admin-rebuild" {
		t.Errorf("marks = %v", h.marks)
	}
	// Outside a herdr plugin pane the deck marks nothing.
	h = &fakeHost{}
	MarkSelf(context.Background(), h, func(k string) string { return map[string]string{"HERDR_PANE_ID": "w1:p1"}[k] }, "admin-rebuild")
	if len(h.marks) != 0 {
		t.Errorf("marks = %v", h.marks)
	}
}

// A deck opened under the lower of two stacked panes, as herdr 0.9.3's
// layout.export and pane.layout describe it.
const nestedTree = `{"type":"split","direction":"down","ratio":0.5,
 "first":{"type":"pane","pane_id":"w6:p1"},
 "second":{"type":"split","direction":"right","ratio":0.5,
  "first":{"type":"pane","pane_id":"w6:p3"},
  "second":{"type":"pane","pane_id":"w6:p4","label":"Deck"}}}`

func TestDeckSplit(t *testing.T) {
	var root Node
	if err := json.Unmarshal([]byte(nestedTree), &root); err != nil {
		t.Fatal(err)
	}
	l := Layout{Root: root, Rects: map[string]Rect{
		"w6:p1": {X: 0, Y: 0, Width: 174, Height: 24},
		"w6:p3": {X: 0, Y: 24, Width: 87, Height: 25},
		"w6:p4": {X: 87, Y: 24, Width: 87, Height: 25},
	}}
	path, width, ok := deckSplit(l, "w6:p4")
	if !ok || !reflect.DeepEqual(path, []bool{true}) || width != 174 {
		t.Fatalf("deckSplit = %v, %d, %v", path, width, ok)
	}
	if _, _, ok := deckSplit(l, "w6:p3"); ok {
		t.Error("a left pane has no deck split")
	}
}

func TestRatio(t *testing.T) {
	for _, tt := range []struct {
		width int
		deck  int // columns the deck gets
	}{
		{width: 100, deck: 50}, // too narrow for 60 + 60: halves
		{width: 120, deck: 60}, // 60 + 60
		{width: 174, deck: 70}, // 40 %
		{width: 200, deck: 80}, // 40 %
		{width: 300, deck: 80}, // capped
		{width: 0, deck: 0},    // no width: halves
		{width: 130, deck: 60}, // at least 60
	} {
		r := ratio(tt.width)
		got := tt.width - int(float64(tt.width)*r+0.5)
		if got != tt.deck {
			t.Errorf("ratio(%d) = %.3f gives the deck %d columns, want %d", tt.width, r, got, tt.deck)
		}
	}
}

func TestParseEvent(t *testing.T) {
	if ev, err := parseEvent(""); err != nil || ev != (Event{}) {
		t.Errorf("empty: %+v, %v", ev, err)
	}
	if _, err := parseEvent("{"); err == nil || !strings.Contains(err.Error(), "event") {
		t.Errorf("bad json: %v", err)
	}
}

func TestOpenPassesConfigPath(t *testing.T) {
	root := projectsRoot(t)
	h := &fakeHost{st: herdr.State{Panes: []herdr.Pane{
		{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: filepath.Join(root, "admin-rebuild"), Agent: "claude"},
	}}}
	getenv := func(k string) string {
		if k == "HERDR_DECK_CONFIG" {
			return "/etc/deck.toml"
		}
		return ""
	}
	if _, err := AgentDetected(context.Background(), h, Env{Getenv: getenv, Root: root}, Event{PaneID: "w1:p1", Agent: "claude"}); err != nil {
		t.Fatal(err)
	}
	if len(h.opened) != 1 || h.opened[0].Env["HERDR_DECK_CONFIG"] != "/etc/deck.toml" {
		t.Errorf("opened %+v, want HERDR_DECK_CONFIG passed on", h.opened)
	}
}

// On a herdr-projects 1.x home folder any pane's toggle opens the board's
// deck, started in the home folder; a 0.2 project folder beside it keeps
// its own deck.
func TestToggleBoard(t *testing.T) {
	root := projectsRoot(t)
	if err := os.MkdirAll(filepath.Join(root, "data", "tasks", "webapp"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, cwd, want, wantCwd string
	}{
		{"a pane anywhere", "/src/elsewhere", "board", root},
		{"the board's own pane", filepath.Join(root, "data", "coordinator"), "board", root},
		{"a 0.2 project folder", filepath.Join(root, "admin-rebuild"), "admin-rebuild", filepath.Join(root, "admin-rebuild")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := &fakeHost{st: herdr.State{Panes: []herdr.Pane{{ID: "w1:p1", WorkspaceID: "w1", TabID: "w1:t1", Cwd: tt.cwd}}}}
			env := Env{Getenv: func(string) string { return "" }, Root: root}
			if err := Toggle(context.Background(), h, env, "w1:p1"); err != nil {
				t.Fatal(err)
			}
			if len(h.opened) != 1 || h.opened[0].Env["HERDR_DECK_PROJECT"] != tt.want || h.opened[0].Cwd != tt.wantCwd {
				t.Errorf("opened %+v, want %q in %s", h.opened, tt.want, tt.wantCwd)
			}
		})
	}
}
