package github

// The GitHub and Review views' data: gh's user's open pull requests in
// the repos the deck follows, and the ones they are asked to review, read
// with two searches in one request a minute. The PR the drawer shows is
// read in full as any other (Focus, Detail), and that read is laid over
// the list's.

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// MineTTL is how long the GitHub views' PRs are shown before they are
// read again.
const MineTTL = time.Minute

// mineFirst is how many PRs each search gives at most.
const mineFirst = "50"

// mineFields is what the views show of each PR. Both searches together
// cost one point of GitHub's GraphQL budget (checked 2026-10-26).
const mineFields = `number title url updatedAt state isDraft mergeable mergeStateStatus reviewDecision
repository { nameWithOwner } author { __typename login }
commits(last: 1) { nodes { commit { statusCheckRollup { state } } } }`

// mineSearch is the search for the open PRs who matches (author:@me,
// review-requested:@me) in repos, or in every repo without any; the most
// recently changed first.
func mineSearch(who string, repos []string) string {
	q := []string{"is:pr", "is:open", "archived:false", who}
	for _, r := range repos {
		q = append(q, "repo:"+r)
	}
	return strings.Join(append(q, "sort:updated-desc"), " ")
}

// mineQuery is gh's arguments for both searches.
func mineQuery(repos []string) []string {
	search := func(alias, v string) string {
		return alias + ": search(query: $" + v + ", type: ISSUE, first: " + mineFirst + ") { nodes { ... on PullRequest { ...deckMine } } }"
	}
	q := "query DeckMine($mine: String!, $review: String!) { rateLimit { remaining resetAt } " +
		search("mine", "mine") + " " + search("review", "review") +
		" } fragment deckMine on PullRequest { " + strings.Join(strings.Fields(mineFields), " ") + " }"
	return []string{"api", "graphql",
		"-f", "mine=" + mineSearch("author:@me", repos),
		"-f", "review=" + mineSearch("review-requested:@me", repos),
		"-f", "query=" + q}
}

type gqlMine struct {
	Data *struct {
		Mine   *gqlSearch `json:"mine"`
		Review *gqlSearch `json:"review"`
	} `json:"data"`
}

type gqlSearch struct {
	Nodes []gqlMinePR `json:"nodes"`
}

type gqlMinePR struct {
	Number           int        `json:"number"`
	Title            string     `json:"title"`
	URL              string     `json:"url"`
	UpdatedAt        time.Time  `json:"updatedAt"`
	State            string     `json:"state"`
	IsDraft          bool       `json:"isDraft"`
	Mergeable        string     `json:"mergeable"`
	MergeStateStatus string     `json:"mergeStateStatus"`
	ReviewDecision   string     `json:"reviewDecision"`
	Author           *gqlAuthor `json:"author"`
	Repository       struct {
		NameWithOwner string `json:"nameWithOwner"`
	} `json:"repository"`
	Commits struct {
		Nodes []struct {
			Commit struct {
				StatusCheckRollup *struct {
					State string `json:"state"`
				} `json:"statusCheckRollup"`
			} `json:"commit"`
		} `json:"nodes"`
	} `json:"commits"`
}

// pr is n as the views show it, read at; ok is false for a search hit
// that is no PR, or one without a GitHub https URL.
func (n gqlMinePR) pr(at time.Time) (deck.MyPR, bool) {
	ref, ok := parsePR(n.URL)
	if n.Number <= 0 || !ok || !strings.HasPrefix(n.URL, "https://") {
		return deck.MyPR{}, false
	}
	p := deck.MyPR{
		Repo:    n.Repository.NameWithOwner,
		Title:   strings.TrimSpace(n.Title),
		Updated: n.UpdatedAt,
		PR: deck.PullRequest{
			URL: ref.URL, Number: n.Number, State: n.State, Review: n.ReviewDecision,
			CheckedAt: at, Live: true, Draft: n.IsDraft,
			MergeState: n.MergeStateStatus, Mergeable: n.Mergeable,
		},
	}
	if p.Repo == "" {
		p.Repo = ref.Owner + "/" + ref.Repo
	}
	p.Author, _ = n.Author.login()
	for _, c := range n.Commits.Nodes {
		if r := c.Commit.StatusCheckRollup; r != nil {
			p.CI = r.State
		}
	}
	return p, true
}

// mineCache is what the last read of the GitHub views gave.
type mineCache struct {
	mine, review []deck.MyPR
	at           time.Time
	inflight     bool
	retryAt      time.Time
	fails        int
	// off says why gh cannot be used at all, problem why the last read
	// failed otherwise.
	off, problem string
	// stale asks for a read at once: the repos changed.
	stale bool
}

// SetRepos sets the repos the GitHub views follow, "owner/name"; none
// follows every repo. Anything else is left out. A change reads them
// again on the next ApplyMine.
func (r *Reader) SetRepos(repos []string) {
	var ok []string
	for _, repo := range repos {
		if repo = strings.TrimSpace(repo); deck.ValidRepo(repo) {
			ok = append(ok, repo)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !slices.Equal(ok, r.repos) {
		r.repos = ok
		r.mine.mine, r.mine.review, r.mine.at = nil, nil, time.Time{}
		r.mine.stale = true
	}
}

// ApplyMine sets snap.PRs from the cache, with the full reads of the PRs
// the drawer showed laid over them, and starts a background read when it
// is older than MineTTL. It never waits on the network.
func (r *Reader) ApplyMine(snap *deck.Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c := &r.mine
	now := r.now()
	var want []prRef
	snap.PRs = &deck.MyPRs{
		Mine:    r.lay(c.mine, now, &want),
		Review:  r.lay(c.review, now, &want),
		Repos:   slices.Clone(r.repos),
		Read:    c.at,
		Problem: c.problem,
		Off:     c.off,
	}
	if c.problem != "" {
		snap.Missing = append(snap.Missing, "GitHub PRs: "+c.problem)
	}
	r.want(want)
	fresh := !c.at.IsZero() && now.Sub(c.at) < MineTTL && !c.stale
	if fresh || c.inflight || now.Before(c.retryAt) && !c.stale {
		return
	}
	c.inflight = true
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		r.FetchMine(context.Background())
		if r.OnUpdate != nil {
			r.OnUpdate()
		}
	}()
}

// lay copies prs with the cache's full reads over them, and adds the PR
// the drawer shows to want when its full read is due. The caller holds
// r.mu.
func (r *Reader) lay(prs []deck.MyPR, now time.Time, want *[]prRef) []deck.MyPR {
	out := slices.Clone(prs)
	for i := range out {
		p := &out[i]
		e := r.cache[p.PR.URL]
		if e != nil && e.pr != nil {
			layFull(p, e.pr, e.at)
		}
		ref, ok := parsePR(p.PR.URL)
		switch {
		case !ok:
		case p.PR.URL == r.detail && r.needDetail(e, p.PR.URL):
			*want = append(*want, ref)
		case p.PR.URL == r.focus && (e == nil || e.stale || now.Sub(e.at) >= r.ttl(p.PR.URL)):
			*want = append(*want, ref)
		}
	}
	return out
}

// layFull lays d, the PR read in full at, over p: what only a full read
// has always, and what the list shows too when the full read is newer.
func layFull(p *deck.MyPR, d *pullData, at time.Time) {
	pr := &p.PR
	pr.Base, pr.AutoMerge, pr.ReviewRequests = d.Base, d.AutoMerge, d.Requests
	pr.Checks, pr.Threads, pr.Detail = d.Checks, d.Threads, d.Detail
	if !at.After(pr.CheckedAt) {
		return
	}
	pr.State, pr.Review, pr.Draft = d.State, d.Review, d.Draft
	pr.MergeState, pr.Mergeable = d.MergeState, d.Mergeable
	pr.FailingChecks = d.Failing()
	pr.CheckedAt = at
	p.CI = deck.Rollup(d.Checks)
}

// FetchMine asks GitHub for the GitHub views' PRs now and stores them. A
// failure is kept for Snapshot.Missing and delays the next try.
func (r *Reader) FetchMine(ctx context.Context) {
	r.mu.Lock()
	repos := slices.Clone(r.repos)
	r.mu.Unlock()
	mine, review, err := r.readMine(ctx, repos)
	r.mu.Lock()
	defer r.mu.Unlock()
	c := &r.mine
	c.inflight = false
	if !slices.Equal(repos, r.repos) {
		return // the repos changed meanwhile: stale asks again
	}
	c.stale = false
	now := r.now()
	if err == nil {
		c.mine, c.review, c.at = mine, review, now
		c.off, c.problem, c.fails = "", "", 0
		return
	}
	var re *readError
	if !errors.As(err, &re) {
		re = &readError{msg: err.Error()}
	}
	c.off, c.problem = "", ""
	switch {
	case re.off:
		c.off = strings.TrimPrefix(re.msg, "off, ")
		c.retryAt = now.Add(offBackoff)
	case !re.retryAt.IsZero():
		c.problem = re.msg
		c.retryAt = re.retryAt
	default:
		c.problem = re.msg
		c.fails++
		c.retryAt = now.Add(min(minBackoff<<min(c.fails-1, 4), maxBackoff))
	}
}

func (r *Reader) readMine(ctx context.Context, repos []string) (mine, review []deck.MyPR, err error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	out, runErr := r.run(ctx, mineQuery(repos), maxOutput)
	if runErr != nil {
		if e := r.ghError(runErr); e.off || len(out) == 0 {
			return nil, nil, e
		}
	}
	var resp gqlResponse
	var data gqlMine
	if json.Unmarshal(out, &resp) != nil || json.Unmarshal(out, &data) != nil {
		if runErr != nil {
			return nil, nil, r.ghError(runErr)
		}
		return nil, nil, &readError{msg: "gh's answer is not GraphQL JSON"}
	}
	for _, e := range resp.Errors {
		if e.Type == "RATE_LIMITED" {
			return nil, nil, r.rateLimited(resp)
		}
	}
	if data.Data == nil || data.Data.Mine == nil || data.Data.Review == nil {
		switch {
		case len(resp.Errors) > 0:
			return nil, nil, &readError{msg: oneLine(resp.Errors[0].Message)}
		case runErr != nil:
			return nil, nil, r.ghError(runErr)
		}
		return nil, nil, &readError{msg: "GitHub answered without data"}
	}
	at := r.now()
	if d := resp.Data; d != nil && d.RateLimit != nil && d.RateLimit.Remaining < lowBudget {
		if reset := d.RateLimit.ResetAt; reset.After(at) && reset.Before(at.Add(2*time.Hour)) {
			r.mu.Lock()
			r.mine.retryAt = reset
			r.mu.Unlock()
		}
	}
	read := func(s *gqlSearch) []deck.MyPR {
		var out []deck.MyPR
		for _, n := range s.Nodes {
			if p, ok := n.pr(at); ok {
				out = append(out, p)
			}
		}
		return out
	}
	return read(data.Data.Mine), read(data.Data.Review), nil
}
