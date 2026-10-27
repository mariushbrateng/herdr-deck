package fake

import (
	"strconv"
	"strings"
	"time"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// BoardSlug is the slug --fake shows the sample herdr-projects 1.x board
// for.
const BoardSlug = "board"

// Board is a herdr-projects 1.x board as the board source reads it: a
// task in every phase, with priorities, a critical change, a frontend
// change and review findings. The lists are grouped as the source groups
// them: by priority, then Resolved.
func Board(now time.Time) deck.Snapshot {
	pct := func(n int) *int { return &n }
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	linear := func(id string) deck.Link {
		return deck.Link{Kind: deck.LinkLinear, Label: id, URL: "https://linear.app/acme/issue/" + id}
	}
	pr := func(n int, state string) *deck.PullRequest {
		return &deck.PullRequest{URL: "https://github.com/acme/webapp/pull/" + strconv.Itoa(n), Number: n, State: state}
	}
	task := func(key, id, title string, phase deck.Phase, prio int) deck.Thread {
		repo, _, _ := strings.Cut(id, "/")
		t := deck.Thread{
			ID: key, Title: title, Status: phase.Status(), StateLine: phase.String(),
			Repo: "/src/" + repo, Created: ago(3 * time.Hour), Changed: ago(10 * time.Minute),
			Board: &deck.BoardTask{ID: id, Repo: repo, Phase: phase, Priority: prio, App: "claude", Model: "claude-opus-5-5"},
		}
		if phase != deck.PhaseTodo && phase != deck.PhaseQueued {
			t.Worktree = "/src/worktrees/" + repo + "-" + key
			t.Branch = "work/" + key
			t.Base = "origin/HEAD"
		}
		return t
	}

	asks := task("WEB-9", "webapp/9", "ENG-1201 Stop losing chunks on reconnect", deck.PhaseAsks, 1)
	asks.Board.Msg = "Should a gappy recording finalize as lossy or stay open?"
	asks.Activity = asks.Board.Msg
	asks.Board.Critical = []string{"INVARIANTS.md: Upload durability", "touches db"}
	asks.Board.Elapsed = 47 * time.Minute
	asks.Board.Context = 182000
	asks.Links = []deck.Link{linear("ENG-1201")}

	working := task("WEB-5", "webapp/5", "ENG-791 Guard på migrations", deck.PhaseWorking, 1)
	working.Board.Msg = "Writing the alembic head check"
	working.Activity = working.Board.Msg
	working.Board.Progress, working.Percent = pct(60), pct(60)
	working.Board.ETA = pct(8)
	working.Board.Critical = []string{"AGENTS.md: Database migrations", "touches migrations"}
	working.Board.Elapsed = 22 * time.Minute
	working.Links = []deck.Link{linear("ENG-791")}

	approve := task("WEB-4", "webapp/4", "ENG-914 Restrict org admin to own org in stats", deck.PhaseApprove, 2)
	approve.PR = pr(2412, "OPEN")
	approve.Board.Review = &deck.BoardReview{Pass: true, Gate: "major", Round: 1, Findings: []deck.Finding{
		{ID: "F1", Severity: "minor", Subject: "The stats query still sorts in Python. It could sort in SQL."},
		{ID: "F2", Severity: "trivial", Subject: "A comment names the old helper."},
	}}
	approve.Board.Critical = []string{"AGENTS.md: Permissions model"}
	approve.Board.Frontend = "frontend"
	approve.Board.ReportURL = "http://127.0.0.1:52797/webapp/4/review.html"
	approve.Board.Elapsed = time.Hour + 5*time.Minute
	approve.Board.Tokens = 412000
	approve.Links = []deck.Link{linear("ENG-914"), {Kind: deck.LinkGitHub, Label: "PR #2412", URL: approve.PR.URL}}

	plan := task("PLG-1", "agent-plugins/1", "ENG-1597 Skill for weekly usage statistics", deck.PhasePlan, 2)
	plan.Board.Msg = "Plan ready: three steps, one new skill"
	plan.Links = []deck.Link{linear("ENG-1597")}

	review := task("WEB-3", "webapp/3", "ENG-1009 Config av bruker-roller", deck.PhaseReview, 3)
	review.Board.Frontend = "frontend"
	review.Links = []deck.Link{linear("ENG-1009")}

	failed := task("WEB-6", "webapp/6", "ENG-890 Improve seeding data for local dev", deck.PhaseFailed, 3)
	failed.Board.Msg = "make test: 2 failing"
	failed.Board.Review = &deck.BoardReview{Gate: "major", Round: 2, Findings: []deck.Finding{
		{ID: "F1", Severity: "major", Subject: "The seed script drops the demo org on every run."},
	}}

	queued := task("WEB-8", "webapp/8", "ENG-1100 Export notes as PDF", deck.PhaseQueued, 4)
	queued.Board.Queue = 1
	todo := task("WEB-7", "webapp/7", "ENG-1025 Lag speck for enterprise admin og org admin", deck.PhaseTodo, 0)
	todo.Links = []deck.Link{linear("ENG-1025")}
	research := task("WEB-10", "webapp/10", "Kartlegg arkitekturen til chat-motoren", deck.PhaseTodo, 0)
	research.Board.Research = true

	merged := task("WEB-2", "webapp/2", "ENG-870 Show the customer's name on drafts", deck.PhaseMerged, 2)
	merged.PR = pr(2398, "MERGED")
	merged.Board.Review = &deck.BoardReview{Pass: true, Gate: "major", Findings: []deck.Finding{{ID: "F1", Severity: "minor", Subject: "Naming."}}}
	merged.Board.Elapsed = 38 * time.Minute
	done := task("WEB-1", "webapp/1", "Map how the MCP mock is used", deck.PhaseDone, 0)
	done.Board.Research = true
	done.Board.ReportFile = "/home/me/.herdr-projects/reports/webapp/1/index.html"
	done.Report = "WEB-1 done: the report is in reports/webapp/1/index.html. Nothing in the repo changed.\n"

	threads := []deck.Thread{asks, working, approve, plan, review, failed, queued, todo, research, merged, done}
	lists := []deck.TaskList{}
	index := map[string]int{}
	for _, t := range threads {
		name := "No priority"
		switch {
		case t.Status == deck.StatusDone:
			name = "Resolved"
		case t.Board.Priority == 1:
			name = "P1 Urgent"
		case t.Board.Priority == 2:
			name = "P2 High"
		case t.Board.Priority == 3:
			name = "P3 Medium"
		case t.Board.Priority == 4:
			name = "P4 Low"
		}
		i, ok := index[name]
		if !ok {
			i = len(lists)
			index[name] = i
			lists = append(lists, deck.TaskList{Name: name})
		}
		lists[i].Tasks = append(lists[i].Tasks, deck.Task{Title: t.Title, Done: t.Status == deck.StatusDone, Threads: []string{t.ID}})
	}
	return deck.Snapshot{
		Project:   deck.Project{Slug: BoardSlug, Name: "Board", Repos: []string{"webapp", "agent-plugins"}},
		Threads:   threads,
		TaskLists: lists,
		ReadAt:    now,
		Herdr:     true,
	}
}
