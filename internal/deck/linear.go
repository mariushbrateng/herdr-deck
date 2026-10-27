package deck

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Mine is the open Linear issues assigned to the API key's user, as
// Linear last gave them (internal/source/linear), with the workflow states
// of their teams so an issue can be moved to another.
type Mine struct {
	Issues []LinearIssue
	// States are the workflow states by team key, in Linear's order:
	// by kind (triage, backlog, unstarted, started, completed,
	// canceled), then by position.
	States map[string][]WorkflowState
	// Read is when Linear gave them; zero while the first answer is
	// pending.
	Read time.Time
	// Problem says why the last fetch failed, or "".
	Problem string
	// Off says why there is nothing to fetch (no API key), or "".
	Off string
}

// LinearIssue is one issue in full: what the deck's Linear view lists and
// its drawer shows.
type LinearIssue struct {
	// UUID is Linear's own id, which a change needs; ID is the
	// identifier, "ENG-1656".
	UUID  string
	ID    string
	Title string
	URL   string
	State WorkflowState
	// Priority is Linear's: 1 Urgent, 2 High, 3 Medium, 4 Low, 0 none.
	Priority    int
	TeamKey     string
	Team        string
	Project     string
	Labels      []Label
	Description string
	// Comments are the newest ones Linear gave, oldest first.
	Comments []Comment
	Updated  time.Time
}

// WorkflowState is one of a team's workflow states.
type WorkflowState struct {
	ID   string
	Name string
	// Type is Linear's kind of state: triage, backlog, unstarted,
	// started, completed or canceled.
	Type     string
	Position float64
}

// Label is an issue label.
type Label struct {
	Name  string
	Color string // "#rrggbb", or ""
}

// Comment is one comment on an issue.
type Comment struct {
	Author string
	At     time.Time
	Body   string
}

// StateOrder ranks a state type in Linear's order of work: triage first,
// canceled last.
func StateOrder(typ string) int {
	switch typ {
	case "triage":
		return 0
	case "backlog":
		return 1
	case "unstarted":
		return 2
	case "started":
		return 3
	case "completed":
		return 4
	case "canceled":
		return 5
	}
	return 6
}

// PriorityName is Linear's name for priority p.
func PriorityName(p int) string {
	switch p {
	case 1:
		return "Urgent"
	case 2:
		return "High"
	case 3:
		return "Medium"
	case 4:
		return "Low"
	}
	return "No priority"
}

// PriorityRank orders priorities most urgent first, with none last.
func PriorityRank(p int) int {
	if p <= 0 || p > 4 {
		return 5
	}
	return p
}

// titleID is a Linear issue ID at the start of a title, as board task
// titles carry it ("ENG-1656 Save …", "ENG-1656: …").
var titleID = regexp.MustCompile(`^\s*\[?([A-Z][A-Z0-9]{1,5}-\d+)\b`)

// TitleIssueID returns the Linear issue ID a title starts with, or "".
// "ENG-16560" is not ENG-1656: the ID ends at a word boundary.
func TitleIssueID(title string) string {
	m := titleID.FindStringSubmatch(title)
	if m == nil {
		return ""
	}
	return m[1]
}

// IssueNumber is the number of an issue ID, "ENG-1656" → 1656, or 0.
func IssueNumber(id string) int {
	_, n, ok := strings.Cut(id, "-")
	if !ok {
		return 0
	}
	v, _ := strconv.Atoi(n)
	return v
}
