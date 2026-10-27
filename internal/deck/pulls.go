package deck

import (
	"regexp"
	"time"
)

// MyPRs is the open pull requests of gh's user in the repos the deck
// follows (github.repos): the ones they wrote, for the GitHub view, and
// the ones they are asked to review, for the Review view, as GitHub last
// gave them (internal/source/github).
type MyPRs struct {
	Mine, Review []MyPR
	// Repos are the repos asked about, "owner/name"; empty for every
	// repo.
	Repos []string
	// Read is when GitHub gave them; zero while the first answer is
	// pending.
	Read time.Time
	// Problem says why the last read failed, or "".
	Problem string
	// Off says why there is nothing to read (gh missing or logged out),
	// or "".
	Off string
}

// MyPR is one pull request in the GitHub or Review view.
type MyPR struct {
	Repo    string // "owner/name"
	Title   string
	Author  string // the login, "ghost" for a deleted account
	Updated time.Time
	// CI is the head commit's checks taken together, GitHub's rollup
	// state: SUCCESS, PENDING, EXPECTED, FAILURE or ERROR; "" without
	// checks. CIState says it in the deck's words.
	CI string
	// PR is the rest: number, URL, draft, review decision and merge
	// state, Live, and the checks and review threads once the drawer
	// has had the PR read in full.
	PR PullRequest
}

// The CI states a MyPR shows.
const (
	CINone    = ""
	CIPassed  = "passed"
	CIRunning = "running"
	CIFailed  = "failed"
)

// CIState is the PR's CI as one of the CI states.
func (p MyPR) CIState() string {
	switch p.CI {
	case "SUCCESS":
		return CIPassed
	case "PENDING", "EXPECTED":
		return CIRunning
	case "FAILURE", "ERROR":
		return CIFailed
	}
	return CINone
}

// Rollup is checks taken together as GitHub's rollup state: FAILURE when
// one failed, PENDING while one runs or waits, SUCCESS when all passed or
// were skipped, "" without checks.
func Rollup(checks []Check) string {
	if len(checks) == 0 {
		return ""
	}
	state := "SUCCESS"
	for _, c := range checks {
		switch c.State {
		case CheckFailed:
			return "FAILURE"
		case CheckRunning, CheckQueued:
			state = "PENDING"
		}
	}
	return state
}

// validRepo is "owner/name" as GitHub allows them.
var validRepo = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]*[A-Za-z0-9])?/[A-Za-z0-9._-]+$`)

// ValidRepo says whether repo is a GitHub repo's "owner/name", and
// nothing else: a setting can add nothing more to a search.
func ValidRepo(repo string) bool { return validRepo.MatchString(repo) }
