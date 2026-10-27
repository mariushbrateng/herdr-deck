package fake

import (
	"time"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// Mine is a sample of the Linear view's issues, matching the sample
// board's task titles, with the workflow states of their team.
func Mine(now time.Time) *deck.Mine {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	st := func(id, name, typ string, pos float64) deck.WorkflowState {
		return deck.WorkflowState{ID: id, Name: name, Type: typ, Position: pos}
	}
	backlog, todo := st("s1", "New", "backlog", 0), st("s2", "Todo", "unstarted", 0)
	doing, inReview := st("s3", "In Progress", "started", 0), st("s4", "In Review", "started", 1)
	done, canceled := st("s5", "Done", "completed", 0), st("s6", "Canceled", "canceled", 0)
	issue := func(id, title string, s deck.WorkflowState, prio int, updated time.Duration) deck.LinearIssue {
		return deck.LinearIssue{
			UUID: "uuid-" + id, ID: id, Title: title, URL: "https://linear.app/acme/issue/" + id,
			State: s, Priority: prio, TeamKey: "ENG", Team: "Engineering", Updated: ago(updated),
		}
	}
	guard := issue("ENG-791", "Guard på migrations", doing, 1, 20*time.Minute)
	guard.Project = "Platform hardening"
	guard.Labels = []deck.Label{{Name: "Backend", Color: "#4ea7fc"}, {Name: "Critical", Color: "#eb5757"}}
	guard.Description = "Make CI fail when two **alembic heads** exist.\n\n- check in `make test`\n- document it in AGENTS.md"
	guard.Comments = []deck.Comment{
		{Author: "Ana Berg", At: ago(26 * time.Hour), Body: "Should this also block deploys?"},
		{Author: "Alex", At: ago(3 * time.Hour), Body: "Only CI for now; deploys after."},
	}
	chunks := issue("ENG-1201", "Stop losing chunks on reconnect", doing, 2, time.Hour)
	chunks.Labels = []deck.Label{{Name: "Bug", Color: "#eb5757"}}
	chunks.Description = "Recordings lose the last chunk when the socket reconnects."
	stats := issue("ENG-914", "Restrict org admin to own org in stats", inReview, 2, 2*time.Hour)
	seed := issue("ENG-890", "Improve seeding data for local development and testing", doing, 3, 5*time.Hour)
	skill := issue("ENG-1597", "Lag skill for ukentlig bruksstatistikk", todo, 0, 30*time.Hour)
	spec := issue("ENG-1025", "Lag speck for enterprise admin og org admin", todo, 2, 50*time.Hour)
	keep := issue("ENG-1632", "Behold metadata om hva en generering brukte som input", backlog, 0, 70*time.Hour)
	return &deck.Mine{
		Issues: []deck.LinearIssue{guard, chunks, stats, seed, skill, spec, keep},
		States: map[string][]deck.WorkflowState{"ENG": {backlog, todo, doing, inReview, done, canceled}},
		Read:   now,
	}
}
