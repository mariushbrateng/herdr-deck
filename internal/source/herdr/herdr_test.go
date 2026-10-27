package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// server is a fake herdr socket. handle answers each request line; it may
// write any number of lines back, and returning false closes the
// connection.
type server struct {
	path string
	mu   sync.Mutex
	reqs []request
}

type request struct {
	ID     string          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// shortDir is a temporary folder with a short path: Unix socket paths are
// limited to about 100 bytes, and t.TempDir adds the test's name, which
// a long $TMPDIR cannot spare.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "hd")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func newServer(t *testing.T, handle func(req request, w *bufio.Writer) bool) *server {
	t.Helper()
	s := &server{path: filepath.Join(shortDir(t), "s.sock")}
	ln, err := net.Listen("unix", s.path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				sc := bufio.NewScanner(conn)
				sc.Buffer(nil, 1<<20)
				w := bufio.NewWriter(conn)
				for sc.Scan() {
					var req request
					if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
						return
					}
					s.mu.Lock()
					s.reqs = append(s.reqs, req)
					s.mu.Unlock()
					keep := handle(req, w)
					w.Flush()
					if !keep {
						return
					}
				}
			}()
		}
	}()
	return s
}

func (s *server) requests() []request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]request(nil), s.reqs...)
}

func fixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/snapshot.json")
	if err != nil {
		t.Fatal(err)
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	b, _ = json.Marshal(v) // one line
	return b
}

// snapshotServer answers session.snapshot with the fixture and pane.focus
// with success.
func snapshotServer(t *testing.T) *server {
	snap := fixture(t)
	return newServer(t, func(req request, w *bufio.Writer) bool {
		switch req.Method {
		case "session.snapshot":
			w.Write(snap)
		case "pane.focus":
			w.WriteString(`{"id":"` + req.ID + `","result":{"type":"ok"}}`)
		default:
			w.WriteString(`{"id":"` + req.ID + `","error":{"code":"unknown","message":"no such method"}}`)
		}
		w.WriteString("\n")
		return true
	})
}

func TestSocketPath(t *testing.T) {
	env := map[string]string{EnvSocket: "/run/herdr.sock"}
	if got := SocketPath(func(k string) string { return env[k] }); got != "/run/herdr.sock" {
		t.Errorf("SocketPath with $%s = %q", EnvSocket, got)
	}
	got := SocketPath(func(string) string { return "" })
	if !strings.HasSuffix(got, filepath.Join(".config", "herdr", "herdr.sock")) {
		t.Errorf("default SocketPath = %q", got)
	}
}

func TestSnapshot(t *testing.T) {
	s := snapshotServer(t)
	st, err := Client{Socket: s.path}.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(st.Panes) != 4 {
		t.Fatalf("got %d panes, want 4", len(st.Panes))
	}
	p := st.Panes[3]
	if p.ID != "w2A:p1" || p.Agent != "claude" || p.AgentStatus != "working" || p.Tokens["hp_group"] != "herdr-deck!1!4!t-0006" {
		t.Errorf("thread pane = %+v", p)
	}
	if st.Panes[1].Agent != "" || st.Panes[1].Tokens != nil {
		t.Errorf("a shell pane has no agent or tokens: %+v", st.Panes[1])
	}
	wt := "/home/dev/worktrees/herdr-deck/hp-herdr-deck-t-0006-m5-herdr-live-state"
	if n := portToken(st, wt); n != 14437 {
		t.Errorf("port token of %s = %d, want 14437", wt, n)
	}
	if n := portToken(st, "/home/dev/src/herdr-deck"); n != 0 {
		t.Errorf("a workspace without a port token gave %d", n)
	}
}

func TestApplyPortToken(t *testing.T) {
	st := liveState()
	st.Workspaces = []Workspace{
		{ID: "w2A", Tokens: map[string]string{"port": "14437"}},
		{ID: "w2B", Tokens: map[string]string{"port": " 14438 "}},
		{ID: "w2C", Tokens: map[string]string{"port": "not a port"}},
		{ID: "w2D", Tokens: map[string]string{"port": "99999"}},
	}
	for i, wt := range []string{"/wt/t-0006/", "/wt/t-0007", "/wt/t-0008", "/wt/t-0005"} {
		st.Workspaces[i].Worktree = &Checkout{Path: wt}
	}
	snap := projectSnap()
	(&Reader{}).apply(&snap, st, t0)
	want := map[string]int{"t-0005": 0, "t-0006": 14437, "t-0007": 14438, "t-0008": 0, "t-0001": 0}
	for _, th := range snap.Threads {
		if th.PortToken != want[th.ID] {
			t.Errorf("%s: port token %d, want %d", th.ID, th.PortToken, want[th.ID])
		}
	}
}

func TestFocus(t *testing.T) {
	s := snapshotServer(t)
	c := Client{Socket: s.path}
	if err := c.Focus(context.Background(), "w2A:p1"); err != nil {
		t.Fatal(err)
	}
	reqs := s.requests()
	if len(reqs) != 1 || reqs[0].Method != "pane.focus" || string(reqs[0].Params) != `{"pane_id":"w2A:p1"}` {
		t.Fatalf("requests = %+v", reqs)
	}
	if err := c.call(context.Background(), "pane.nope", struct{}{}, nil); err == nil || !strings.Contains(err.Error(), "no such method") {
		t.Errorf("an error reply gives %v", err)
	}
}

func TestNoSocket(t *testing.T) {
	c := Client{Socket: filepath.Join(shortDir(t), "gone.sock")}
	if _, err := c.Snapshot(context.Background()); !errors.Is(err, ErrNoSocket) {
		t.Errorf("Snapshot without a socket: %v", err)
	}
	if err := c.Focus(context.Background(), "w1:p1"); !errors.Is(err, ErrNoSocket) {
		t.Errorf("Focus without a socket: %v", err)
	}
}

var t0 = time.Date(2026, 10, 2, 14, 0, 0, 0, time.UTC)

func projectSnap() deck.Snapshot {
	return deck.Snapshot{
		Project: deck.Project{Slug: "herdr-deck", Dir: "/root/herdr-deck"},
		Threads: []deck.Thread{
			{ID: "t-0005", Status: deck.StatusReview, PaneID: "w29:p1", Worktree: "/wt/t-0005"},
			{ID: "t-0006", Status: deck.StatusNeedsYou, StateLine: "needs you · ~40%", PaneID: "w2A:p1", Worktree: "/wt/t-0006"},
			{ID: "t-0007", Status: deck.StatusUnknown, PaneID: "w2B:p1", Worktree: "/wt/t-0007"},
			{ID: "t-0008", Status: deck.StatusNeedsYou, StateLine: "failed: no agent", PaneID: "w2C:p1", Worktree: "/wt/t-0008"},
			{ID: "t-0001", Status: deck.StatusDone, PaneID: "w25:p1", Worktree: "/wt/t-0001"},
		},
	}
}

func liveState() State {
	return State{Panes: []Pane{
		{ID: "w23:p1", Agent: "claude", AgentStatus: "idle", Cwd: "/root/herdr-deck",
			Tokens: map[string]string{"hp_project": "herdr-deck", "hp_group": "herdr-deck!0!w23:p1"}},
		// Matched by tokens although herdr-projects recorded another id.
		{ID: "w30:p1", Agent: "claude", AgentStatus: "working", Cwd: "/wt/t-0006/sub",
			Tokens: map[string]string{"hp_project": "herdr-deck", "hp_group": "herdr-deck!1!4!t-0006"}},
		// No tokens yet: matched by pane id and working directory.
		{ID: "w2B:p1", Agent: "claude", AgentStatus: "blocked", Cwd: "/wt/t-0007/"},
		{ID: "w2C:p1", Agent: "claude", AgentStatus: "working", Cwd: "/wt/t-0008"},
		// The recorded pane id, but someone else's directory.
		{ID: "w29:p1", Agent: "claude", AgentStatus: "blocked", Cwd: "/elsewhere"},
		// Another project's thread with the same thread id.
		{ID: "w40:p1", Agent: "claude", AgentStatus: "blocked",
			Tokens: map[string]string{"hp_project": "other", "hp_group": "other!1!1!t-0005"}},
	}}
}

func TestApplyMatchesPanes(t *testing.T) {
	r := &Reader{}
	snap := projectSnap()
	r.apply(&snap, liveState(), t0)

	if !snap.Herdr || snap.Project.PaneID != "w23:p1" {
		t.Errorf("Herdr %v, coordinator pane %q", snap.Herdr, snap.Project.PaneID)
	}
	want := map[string]string{"t-0005": "", "t-0006": "w30:p1", "t-0007": "w2B:p1", "t-0008": "w2C:p1", "t-0001": ""}
	for _, th := range snap.Threads {
		got := ""
		if th.Pane != nil {
			got = th.Pane.ID
		}
		if got != want[th.ID] {
			t.Errorf("%s: pane %q, want %q", th.ID, got, want[th.ID])
		}
	}
}

func TestApplyLiveStatus(t *testing.T) {
	r := &Reader{}
	snap := projectSnap()
	r.apply(&snap, liveState(), t0)
	got := func(s deck.Snapshot) map[string]deck.ThreadStatus {
		m := map[string]deck.ThreadStatus{}
		for _, th := range s.Threads {
			m[th.ID] = th.Status
		}
		return m
	}
	first := got(snap)
	for id, want := range map[string]deck.ThreadStatus{
		"t-0005": deck.StatusReview,  // no pane matched: the group stands
		"t-0006": deck.StatusWorking, // working agent overrides a stale needs-you
		"t-0007": deck.StatusUnknown, // blocked, but only just
		"t-0008": deck.StatusNeedsYou,
		"t-0001": deck.StatusDone,
	} {
		if first[id] != want {
			t.Errorf("at once, %s: status %v, want %v", id, first[id], want)
		}
	}

	if th := snap.Threads[1]; th.StateLine != "working" {
		t.Errorf("t-0006 overridden to working keeps state line %q", th.StateLine)
	}
	if th := snap.Threads[3]; th.StateLine != "failed: no agent" {
		t.Errorf("t-0008 not overridden, but its state line is now %q", th.StateLine)
	}

	// Still blocked after BlockedAfter: now it needs the user.
	snap = projectSnap()
	r.apply(&snap, liveState(), t0.Add(BlockedAfter))
	if s := got(snap)["t-0007"]; s != deck.StatusNeedsYou {
		t.Errorf("blocked for %v: status %v, want needs you", BlockedAfter, s)
	}
	if l := snap.Threads[2].StateLine; l != "needs you · blocked on a prompt" {
		t.Errorf("blocked t-0007's state line %q", l)
	}
	if p := snap.Threads[2].Pane; p == nil || !p.Since.Equal(t0) {
		t.Errorf("t-0007's pane %+v, want since %v", p, t0)
	}

	// Unblocked and blocked again: the wait starts over.
	st := liveState()
	st.Panes[2].AgentStatus = "working"
	snap = projectSnap()
	r.apply(&snap, st, t0.Add(6*time.Second))
	snap = projectSnap()
	r.apply(&snap, liveState(), t0.Add(7*time.Second))
	if s := got(snap)["t-0007"]; s == deck.StatusNeedsYou {
		t.Errorf("blocked again for 0s: status %v", s)
	}
}

func TestApplyWithoutHerdr(t *testing.T) {
	r := NewReader(filepath.Join(shortDir(t), "gone.sock"))
	snap := projectSnap()
	r.Apply(context.Background(), &snap, t0)
	if snap.Herdr || len(snap.Missing) != 1 || !strings.HasPrefix(snap.Missing[0], "herdr socket: not found") {
		t.Fatalf("Herdr %v, Missing %q", snap.Herdr, snap.Missing)
	}
	if snap.Threads[1].Status != deck.StatusNeedsYou || snap.Threads[1].Pane != nil {
		t.Errorf("threads changed without herdr: %+v", snap.Threads[1])
	}
}

func TestApplyFromSocket(t *testing.T) {
	s := snapshotServer(t)
	r := NewReader(s.path)
	snap := projectSnap()
	r.Apply(context.Background(), &snap, t0)
	if !snap.Herdr || len(snap.Missing) != 0 || snap.Threads[1].Pane == nil || snap.Threads[1].Pane.ID != "w2A:p1" {
		t.Fatalf("Herdr %v, Missing %q, t-0006 pane %+v", snap.Herdr, snap.Missing, snap.Threads[1].Pane)
	}
}

func TestWatchStreams(t *testing.T) {
	snap := fixture(t)
	s := newServer(t, func(req request, w *bufio.Writer) bool {
		switch req.Method {
		case "session.snapshot":
			w.Write(snap)
			w.WriteString("\n")
			return false
		case "events.subscribe":
			w.WriteString(`{"id":"` + req.ID + `","result":{"type":"subscription_started"}}` + "\n")
			w.WriteString(`{"event":"pane_updated","data":{"type":"pane_updated","pane":{}}}` + "\n")
			w.WriteString(`not json` + "\n")
			w.WriteString(`{"event":"pane_agent_status_changed","data":{"pane_id":"w2A:p1","agent_status":"blocked"}}` + "\n")
			w.Flush()
			time.Sleep(300 * time.Millisecond)
			w.WriteString(`{"event":"something_new","data":{"x":1}}` + "\n")
		}
		return true
	})
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Client{Socket: s.path}.Watch(ctx, WatchOptions{Debounce: 50 * time.Millisecond, Poll: time.Hour, Retry: time.Hour}, func() { calls.Add(1) })

	waitFor(t, func() bool { return calls.Load() >= 2 }, "two debounced calls")
	var sub request
	for _, r := range s.requests() {
		if r.Method == "events.subscribe" {
			sub = r
		}
	}
	var params struct {
		Subscriptions []map[string]string `json:"subscriptions"`
	}
	if err := json.Unmarshal(sub.Params, &params); err != nil {
		t.Fatal(err)
	}
	var panes []string
	for _, s := range params.Subscriptions {
		if s["type"] == "pane.agent_status_changed" {
			panes = append(panes, s["pane_id"])
		}
	}
	if strings.Join(panes, ",") != "w23:p1,w2A:p1" {
		t.Errorf("status subscriptions for %q, want the two agent panes", panes)
	}
}

func TestWatchPollsWhenRefused(t *testing.T) {
	snap := fixture(t)
	s := newServer(t, func(req request, w *bufio.Writer) bool {
		if req.Method == "session.snapshot" {
			w.Write(snap)
		} else {
			w.WriteString(`{"id":"` + req.ID + `","error":{"code":"invalid_request","message":"unknown subscription"}}`)
		}
		w.WriteString("\n")
		return false
	})
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Client{Socket: s.path}.Watch(ctx, WatchOptions{Debounce: 10 * time.Millisecond, Poll: 30 * time.Millisecond, Retry: time.Hour}, func() { calls.Add(1) })
	waitFor(t, func() bool { return calls.Load() >= 3 }, "polling calls")
}

func TestWatchWithoutSocket(t *testing.T) {
	var calls atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	Client{Socket: filepath.Join(shortDir(t), "gone.sock")}.Watch(ctx, WatchOptions{Debounce: 10 * time.Millisecond, Poll: 10 * time.Millisecond, Retry: 20 * time.Millisecond}, func() { calls.Add(1) })
	if n := calls.Load(); n != 0 {
		t.Errorf("changed was called %d times without a socket", n)
	}
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestWatchResubscribesWhenAPaneCloses(t *testing.T) {
	snap := fixture(t)
	var subs atomic.Int32
	s := newServer(t, func(req request, w *bufio.Writer) bool {
		switch req.Method {
		case "session.snapshot":
			w.Write(snap)
			w.WriteString("\n")
			return false
		case "events.subscribe":
			if subs.Add(1) == 1 {
				w.WriteString(`{"id":"` + req.ID + `","error":{"code":"pane_not_found","message":"pane w2A:p1 not found"}}` + "\n")
				return false
			}
			w.WriteString(`{"id":"` + req.ID + `","result":{"type":"subscription_started"}}` + "\n")
		}
		return true
	})
	var calls atomic.Int32
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go Client{Socket: s.path}.Watch(ctx, WatchOptions{Debounce: 10 * time.Millisecond, Poll: 10 * time.Millisecond, Retry: time.Hour}, func() { calls.Add(1) })
	waitFor(t, func() bool { return subs.Load() >= 2 }, "a second subscribe")
	time.Sleep(100 * time.Millisecond)
	if n := calls.Load(); n != 0 {
		t.Errorf("a closed pane made Watch poll: %d calls", n)
	}
}

// TestTimelines replays testdata/timelines: a thread's pane through a series
// of session.snapshot replies, with the status the deck must show after each.
// herdr-projects' group stays as the file says throughout.
func TestTimelines(t *testing.T) {
	files, err := filepath.Glob("testdata/timelines/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no timelines: %v", err)
	}
	statuses := map[string]deck.ThreadStatus{"working": deck.StatusWorking, "needs you": deck.StatusNeedsYou}
	for _, f := range files {
		t.Run(strings.TrimSuffix(filepath.Base(f), ".json"), func(t *testing.T) {
			b, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			var tl struct {
				Thread string
				Group  string
				Steps  []struct {
					At    string
					Note  string
					Want  string
					Reply struct {
						Result struct {
							Snapshot State
						}
					}
				}
			}
			if err := json.Unmarshal(b, &tl); err != nil {
				t.Fatal(err)
			}
			group, ok := statuses[tl.Group]
			if !ok {
				t.Fatalf("group %q", tl.Group)
			}
			r := &Reader{}
			for _, step := range tl.Steps {
				at, err := time.ParseDuration(step.At)
				want, ok := statuses[step.Want]
				if err != nil || !ok {
					t.Fatalf("step at %q wants %q", step.At, step.Want)
				}
				snap := deck.Snapshot{
					Project: deck.Project{Slug: "admin-rebuild", Dir: "/home/dev/.herdr-projects/admin-rebuild"},
					Threads: []deck.Thread{{ID: tl.Thread, Status: group, StateLine: tl.Group}},
				}
				r.apply(&snap, step.Reply.Result.Snapshot, t0.Add(at))
				th := snap.Threads[0]
				if th.Pane == nil {
					t.Fatalf("at %s: no pane matched", step.At)
				}
				if th.Status != want {
					t.Errorf("at %s (%s, %s): status %v, want %v", step.At, th.Pane.AgentStatus, step.Note, th.Status, want)
				}
			}
		})
	}
}

// Only a long block claims the user. Whatever else the agent does, and
// however long it has done it, herdr-projects' group stands (or a working
// agent says working).
func TestLiveStatusOnlyBlockedNeedsYou(t *testing.T) {
	long := t0.Add(-time.Hour)
	for _, c := range []struct {
		agent string
		group deck.ThreadStatus
		want  deck.ThreadStatus
	}{
		{"working", deck.StatusWorking, deck.StatusWorking},
		{"working", deck.StatusReview, deck.StatusReview},
		{"idle", deck.StatusWorking, deck.StatusWorking},
		{"idle", deck.StatusUnknown, deck.StatusUnknown},
		{"done", deck.StatusReview, deck.StatusReview},
		{"unknown", deck.StatusWorking, deck.StatusWorking},
		{"blocked", deck.StatusWorking, deck.StatusNeedsYou},
		{"blocked", deck.StatusReview, deck.StatusNeedsYou},
		{"blocked", deck.StatusDone, deck.StatusDone},
	} {
		th := deck.Thread{Status: c.group, Pane: &deck.Pane{AgentStatus: c.agent, Since: long}}
		if got := liveStatus(th, t0); got != c.want {
			t.Errorf("%s agent for an hour, group %v: %v, want %v", c.agent, c.group, got, c.want)
		}
	}
	th := deck.Thread{Status: deck.StatusWorking, Pane: &deck.Pane{AgentStatus: "blocked", Since: t0.Add(-BlockedAfter + time.Second)}}
	if got := liveStatus(th, t0); got != deck.StatusWorking {
		t.Errorf("blocked for %v: %v, want the group", BlockedAfter-time.Second, got)
	}
}

func TestApplyProjects(t *testing.T) {
	r := &Reader{}
	st := State{Panes: []Pane{
		{ID: "w40:p1", Agent: "claude", AgentStatus: "blocked",
			Tokens: map[string]string{"hp_project": "billing", "hp_group": "billing!1!1!t-0003"}},
		{ID: "w41:p1", Agent: "claude", AgentStatus: "idle",
			Tokens: map[string]string{"hp_project": "billing", "hp_group": "billing!0!w41:p1"}},
		// docs' recorded coordinator, without tokens or its folder as cwd.
		{ID: "w50:p1", Agent: "claude", AgentStatus: "idle", Cwd: "/elsewhere"},
		// A shell where search's recorded coordinator was: no agent, so none runs.
		{ID: "w60:p1", Cwd: "/root/search"},
	}}
	list := func() []deck.ProjectInfo {
		return []deck.ProjectInfo{
			{Project: deck.Project{Slug: "billing", Dir: "/root/billing"}, Threads: []deck.Thread{
				{ID: "t-0003", Status: deck.StatusWorking, Worktree: "/wt/billing-3"},
			}},
			{Project: deck.Project{Slug: "docs", Dir: "/root/docs"}, RecordedPane: "w50:p1"},
			{Project: deck.Project{Slug: "search", Dir: "/root/search"}, RecordedPane: "w60:p1"},
		}
	}

	// Without a state from Apply the list stays as it is.
	l := list()
	r.ApplyProjects(l, "", t0)
	if l[0].Threads[0].Pane != nil || l[1].PaneID != "" {
		t.Fatalf("applied without a state: %+v", l)
	}

	snap := deck.Snapshot{Project: deck.Project{Slug: "docs", Dir: "/root/docs"}}
	r.apply(&snap, st, t0)
	r.apply(&snap, st, t0.Add(BlockedAfter))
	l = list()
	r.ApplyProjects(l, "docs", t0.Add(BlockedAfter))
	th := l[0].Threads[0]
	if th.Pane == nil || th.Pane.ID != "w40:p1" || th.Status != deck.StatusNeedsYou || !th.Pane.Since.Equal(t0) {
		t.Errorf("billing t-0003: %+v pane %+v", th, th.Pane)
	}
	if l[0].PaneID != "w41:p1" || l[1].PaneID != "w50:p1" || l[2].PaneID != "" {
		t.Errorf("coordinators %q %q %q, want w41:p1 w50:p1 none", l[0].PaneID, l[1].PaneID, l[2].PaneID)
	}

	// A failed read forgets the state.
	r.Client = Client{}
	r.Apply(context.Background(), &snap, t0)
	l = list()
	r.ApplyProjects(l, "", t0)
	if l[0].PaneID != "" {
		t.Errorf("applied after a failed read: %+v", l[0])
	}
}

// OpenPane splits below the target, zooms the new pane when asked, then
// types the line and Enter into it.
func TestOpenPane(t *testing.T) {
	s := newServer(t, func(req request, w *bufio.Writer) bool {
		switch req.Method {
		case "pane.split":
			w.WriteString(`{"id":"` + req.ID + `","result":{"pane":{"pane_id":"w1:p9"}}}`)
		default:
			w.WriteString(`{"id":"` + req.ID + `","result":{"type":"ok"}}`)
		}
		w.WriteString("\n")
		return true
	})
	c := Client{Socket: s.path}
	if err := c.OpenPane(context.Background(), "w1:p2", "/src/wt", "exec herdr-deck arch", map[string]string{"HERDR_DECK_ARCH_TESTS": "true"}, true); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, r := range s.requests() {
		got = append(got, r.Method+" "+string(r.Params))
	}
	want := []string{
		`pane.split {"cwd":"/src/wt","direction":"down","env":{"HERDR_DECK_ARCH_TESTS":"true"},"focus":true,"target_pane_id":"w1:p2"}`,
		`pane.zoom {"mode":"on","pane_id":"w1:p9"}`,
		`pane.send_input {"keys":["Enter"],"pane_id":"w1:p9","text":"exec herdr-deck arch"}`,
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("requests:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	// Without zoom, as the editor opens: no pane.zoom.
	if err := c.OpenPane(context.Background(), "w1:p2", "/src/wt", "nvim .", nil, false); err != nil {
		t.Fatal(err)
	}
	for _, r := range s.requests()[3:] {
		if r.Method == "pane.zoom" {
			t.Error("zoomed without being asked")
		}
	}
}
