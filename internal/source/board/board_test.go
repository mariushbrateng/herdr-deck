package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// fakeCLI answers `list --json` and `details <id>` from testdata.
func fakeCLI(t *testing.T, calls *[]string) RunFunc {
	return func(_ context.Context, _ string, args ...string) ([]byte, error) {
		*calls = append(*calls, strings.Join(args, " "))
		switch args[0] {
		case "list":
			return os.ReadFile("testdata/list.json")
		case "details":
			data, err := os.ReadFile("testdata/details-" + strings.ReplaceAll(args[1], "/", "-") + ".json")
			if err != nil {
				return nil, errors.New("no such task")
			}
			return data, nil
		}
		t.Fatalf("unexpected command %v", args)
		return nil, nil
	}
}

func TestReadFromCLI(t *testing.T) {
	var calls []string
	prio := &Priorities{Path: filepath.Join(t.TempDir(), PriorityFile)}
	if err := prio.Set("webapp/7", 1); err != nil {
		t.Fatal(err)
	}
	r := Reader{Home: "testdata/home", Run: fakeCLI(t, &calls), Priorities: prio, LinearWorkspace: "acme"}
	snap := r.Read(context.Background())

	if len(snap.Missing) != 0 {
		t.Errorf("Missing = %q", snap.Missing)
	}
	if calls[0] != "list --json" {
		t.Errorf("first call %q, want list --json", calls[0])
	}
	var ids []string
	for _, th := range snap.Threads {
		ids = append(ids, th.ID)
	}
	// Priority first (webapp/7 is P1), then phase: the plan waits on the
	// user, then work under way, then finished.
	want := []string{"webapp/7", "agent-plugins/1", "WEB-5", "WEB-2"}
	if !reflect.DeepEqual(ids, want) {
		t.Fatalf("thread order %q, want %q", ids, want)
	}
	byID := map[string]deck.Thread{}
	for _, th := range snap.Threads {
		byID[th.ID] = th
	}

	w := byID["WEB-5"]
	if w.Board.Phase != deck.PhaseWorking || w.Status != deck.StatusWorking || w.Activity != "Writing the guard" {
		t.Errorf("WEB-5: phase %v status %v activity %q", w.Board.Phase, w.Status, w.Activity)
	}
	if w.Worktree != "/wt/webapp-5" || w.Branch != "work/5-guard" || w.Base != "origin/main" || w.PaneID != "wA:p2" || w.Repo != "/repos/webapp" {
		t.Errorf("WEB-5 worktree %q branch %q base %q pane %q repo %q", w.Worktree, w.Branch, w.Base, w.PaneID, w.Repo)
	}
	if w.Board.Progress == nil || *w.Board.Progress != 40 || w.Board.ETA == nil || *w.Board.ETA != 12 {
		t.Errorf("WEB-5 progress %v eta %v", w.Board.Progress, w.Board.ETA)
	}
	if w.Report != "Closing note for 5\n" {
		t.Errorf("WEB-5 report %q, want its closing note", w.Report)
	}
	if len(w.Links) != 1 || w.Links[0].Label != "ENG-791" || w.Links[0].URL == "" {
		t.Errorf("WEB-5 links %+v, want the title's Linear issue", w.Links)
	}

	d := byID["WEB-2"]
	if d.Board.Phase != deck.PhaseMerged || d.Status != deck.StatusDone {
		t.Errorf("WEB-2 phase %v status %v, want merged", d.Board.Phase, d.Status)
	}
	if d.PR == nil || d.PR.Number != 41 || d.PR.State != "MERGED" {
		t.Errorf("WEB-2 PR %+v", d.PR)
	}
	if d.Board.Review == nil || !d.Board.Review.Pass || d.Board.Review.Tally() != "1 minor, 1 trivial" {
		t.Errorf("WEB-2 review %+v", d.Board.Review)
	}
	if d.Board.Elapsed.Minutes() != 10 || d.Board.Tokens != 1234 || d.Board.Context != 194930 {
		t.Errorf("WEB-2 elapsed %v tokens %d context %d", d.Board.Elapsed, d.Board.Tokens, d.Board.Context)
	}
	if !strings.HasSuffix(d.Board.ReportFile, "reports/webapp/2/report.md") || !strings.HasPrefix(d.Report, "# Report") {
		t.Errorf("WEB-2 report file %q, text %q", d.Board.ReportFile, d.Report)
	}
	var kinds []string
	for _, l := range d.Links {
		kinds = append(kinds, l.Kind.String()+" "+l.Label)
	}
	// The report's bare WEB-5 is a board key, not a Linear issue.
	if want := []string{"Linear ENG-890", "Figma Design", "GitHub PR #41"}; !reflect.DeepEqual(kinds, want) {
		t.Errorf("WEB-2 links %q, want %q", kinds, want)
	}

	p := byID["agent-plugins/1"]
	if p.Board.Phase != deck.PhasePlan || p.Status != deck.StatusNeedsYou {
		t.Errorf("plan task: phase %v status %v", p.Board.Phase, p.Status)
	}

	var lists []string
	for _, l := range snap.TaskLists {
		lists = append(lists, l.Name)
	}
	if want := []string{"P1 Urgent", "No priority", ListDone}; !reflect.DeepEqual(lists, want) {
		t.Errorf("lists %q, want %q", lists, want)
	}
	if !reflect.DeepEqual(snap.Project.Repos, []string{"agent-plugins", "webapp"}) {
		t.Errorf("repos %q", snap.Project.Repos)
	}
}

func TestReadRepoFilter(t *testing.T) {
	var calls []string
	r := Reader{Home: "testdata/home", Run: fakeCLI(t, &calls), Repo: "agent-plugins"}
	snap := r.Read(context.Background())
	if len(snap.Threads) != 1 || snap.Threads[0].ID != "agent-plugins/1" {
		t.Fatalf("threads %+v", snap.Threads)
	}
}

func TestReadWithoutCLI(t *testing.T) {
	r := Reader{Home: "testdata/home", Run: func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("not found")
	}}
	snap := r.Read(context.Background())
	if len(snap.Missing) != 1 || !strings.Contains(snap.Missing[0], "from their folders") {
		t.Errorf("Missing = %q", snap.Missing)
	}
	// webapp/8 is archived, so only 2 and 5 are read from their folders.
	var ids []string
	for _, th := range snap.Threads {
		ids = append(ids, th.ID)
	}
	if want := []string{"webapp/5", "webapp/2"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("threads %q, want %q", ids, want)
	}
	if snap.Threads[0].Board.Phase != deck.PhaseWorking || snap.Threads[1].Board.Phase != deck.PhaseMerged {
		t.Errorf("phases %v %v", snap.Threads[0].Board.Phase, snap.Threads[1].Board.Phase)
	}
}

func TestPhaseOf(t *testing.T) {
	tests := []struct {
		stage, state string
		merged       bool
		want         deck.Phase
	}{
		{"todo", "idle", false, deck.PhaseTodo},
		{"queued", "idle", false, deck.PhaseQueued},
		{"working", "working", false, deck.PhaseWorking},
		{"working", "blocked", false, deck.PhaseAsks},
		{"working", "error", false, deck.PhaseFailed},
		{"plan", "idle", false, deck.PhasePlan},
		{"reviewing", "idle", false, deck.PhaseReview},
		{"approval", "idle", false, deck.PhaseApprove},
		{"merging", "idle", false, deck.PhaseMerging},
		{"failed", "error", false, deck.PhaseFailed},
		{"done", "done", false, deck.PhaseDone},
		{"done", "done", true, deck.PhaseMerged},
		{"something-new", "working", false, deck.PhaseWorking},
		// Several attempts under way are work, not a question; pick is.
		{"attempts", "working", false, deck.PhaseWorking},
		{"tried", "idle", false, deck.PhaseWorking},
		{"pick", "idle", false, deck.PhaseAsks},
		{"checking", "idle", false, deck.PhaseChecking},
		{"progress", "working", false, deck.PhaseWorking},
		{"starting", "idle", false, deck.PhaseWorking},
		{"setup", "idle", false, deck.PhaseWorking},
		{"pushing", "idle", false, deck.PhaseWorking},
		{"stopped", "idle", false, deck.PhaseFailed},
		{"nosummary", "idle", false, deck.PhaseAsks},
		{"reviewneeds", "idle", false, deck.PhaseAsks},
		{"after", "idle", false, deck.PhaseQueued},
	}
	if got := PhaseOf("attempts", "working", false).Status(); got != deck.StatusWorking {
		t.Errorf("attempts: status %v, want working (not under Needs you)", got)
	}
	for _, tt := range tests {
		if got := PhaseOf(tt.stage, tt.state, tt.merged); got != tt.want {
			t.Errorf("PhaseOf(%q, %q, %v) = %v, want %v", tt.stage, tt.state, tt.merged, got, tt.want)
		}
	}
}

func TestLinearID(t *testing.T) {
	for title, want := range map[string]string{
		"ENG-1025 Lag speck":      "ENG-1025",
		"[ENG-7] Fix":             "ENG-7",
		"Kartlegg arkitekturen":   "",
		"Fix ENG-3 later":         "",
		"ENG-12: Guard migration": "ENG-12",
	} {
		if got := LinearID(title); got != want {
			t.Errorf("LinearID(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestPriorities(t *testing.T) {
	p := &Priorities{Path: filepath.Join(t.TempDir(), "state", PriorityFile)}
	m, err := p.Load()
	if err != nil || len(m) != 0 {
		t.Fatalf("empty Load = %v, %v", m, err)
	}
	if err := p.Set("a/1", 2); err != nil {
		t.Fatal(err)
	}
	if err := p.Set("a/2", 4); err != nil {
		t.Fatal(err)
	}
	if err := p.Set("a/2", 0); err != nil {
		t.Fatal(err)
	}
	if err := p.Set("a/3", 5); err == nil {
		t.Error("Set 5 did not fail")
	}
	m, _ = p.Load()
	if !reflect.DeepEqual(m, map[string]int{"a/1": 2}) {
		t.Errorf("Load = %v", m)
	}
	if PriorityList(1) != "P1 Urgent" || PriorityList(4) != "P4 Low" || PriorityList(0) != "No priority" {
		t.Errorf("names %q %q %q", PriorityList(1), PriorityList(4), PriorityList(0))
	}
}
