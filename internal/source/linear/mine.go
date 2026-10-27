package linear

// The Linear view's data: the open issues assigned to the key's user, in
// full (description, labels, project, the newest comments), and the
// workflow states of the teams, read in one request; and the two changes
// the view makes, an issue's state and its priority.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// MineTTL is how long the Linear view's issues are shown before they are
// fetched again.
const MineTTL = time.Minute

// mineStates are the state types the Linear view lists: what is under
// way, to do and in the backlog. Done, canceled and duplicate issues
// (canceled too in Linear's types) and triage are left out.
var mineStates = []string{"started", "unstarted", "backlog"}

// mineQuery asks for the issues and the teams' states. With 50 issues, 10
// labels and 10 comments each, and 20 teams of 50 states, it costs about
// 3,500 of the 10,000 complexity points a query may cost.
const mineQuery = `query DeckMine($types: [String!]) {
  viewer {
    assignedIssues(first: 50, filter: { state: { type: { in: $types } } }) {
      nodes {
        id identifier title url priority description updatedAt
        state { id name type position }
        team { key name }
        project { name }
        labels(first: 10) { nodes { name color } }
        comments(first: 10, orderBy: createdAt) { nodes { body createdAt user { displayName } } }
      }
    }
  }
  teams(first: 20) { nodes { key states(first: 50) { nodes { id name type position } } } }
  organization { urlKey }
}`

type gqlMine struct {
	Viewer struct {
		AssignedIssues struct {
			Nodes []gqlFullIssue `json:"nodes"`
		} `json:"assignedIssues"`
	} `json:"viewer"`
}

type gqlState struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Type     string  `json:"type"`
	Position float64 `json:"position"`
}

func (s gqlState) state() deck.WorkflowState {
	return deck.WorkflowState{ID: s.ID, Name: s.Name, Type: s.Type, Position: s.Position}
}

type gqlFullIssue struct {
	ID          string    `json:"id"`
	Identifier  string    `json:"identifier"`
	Title       string    `json:"title"`
	URL         string    `json:"url"`
	Priority    float64   `json:"priority"`
	Description string    `json:"description"`
	UpdatedAt   time.Time `json:"updatedAt"`
	State       *gqlState `json:"state"`
	Team        *struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	} `json:"team"`
	Project *struct {
		Name string `json:"name"`
	} `json:"project"`
	Labels struct {
		Nodes []struct {
			Name  string `json:"name"`
			Color string `json:"color"`
		} `json:"nodes"`
	} `json:"labels"`
	Comments struct {
		Nodes []struct {
			Body      string    `json:"body"`
			CreatedAt time.Time `json:"createdAt"`
			User      *struct {
				DisplayName string `json:"displayName"`
			} `json:"user"`
		} `json:"nodes"`
	} `json:"comments"`
}

type gqlTeams struct {
	Nodes []struct {
		Key    string `json:"key"`
		States struct {
			Nodes []gqlState `json:"nodes"`
		} `json:"states"`
	} `json:"nodes"`
}

// issue is n as the Linear view shows it; ok is false without a state.
func (n gqlFullIssue) issue() (deck.LinearIssue, bool) {
	if n.State == nil || n.ID == "" {
		return deck.LinearIssue{}, false
	}
	is := deck.LinearIssue{
		UUID:        n.ID,
		ID:          n.Identifier,
		Title:       strings.TrimSpace(n.Title),
		URL:         n.URL,
		State:       n.State.state(),
		Priority:    int(n.Priority),
		Description: strings.TrimSpace(n.Description),
		Updated:     n.UpdatedAt,
	}
	if !strings.HasPrefix(is.URL, "https://") {
		is.URL = "" // only ever open Linear's own https pages
	}
	if n.Team != nil {
		is.TeamKey, is.Team = n.Team.Key, n.Team.Name
	}
	if n.Project != nil {
		is.Project = n.Project.Name
	}
	for _, l := range n.Labels.Nodes {
		is.Labels = append(is.Labels, deck.Label{Name: l.Name, Color: l.Color})
	}
	for _, c := range n.Comments.Nodes {
		author := "Linear" // an integration's comment has no user
		if c.User != nil && c.User.DisplayName != "" {
			author = c.User.DisplayName
		}
		is.Comments = append(is.Comments, deck.Comment{Author: author, At: c.CreatedAt, Body: strings.TrimSpace(c.Body)})
	}
	slices.SortStableFunc(is.Comments, func(a, b deck.Comment) int { return a.At.Compare(b.At) })
	return is, true
}

// mineCache is what the last fetch of the Linear view gave.
type mineCache struct {
	issues   []deck.LinearIssue
	states   map[string][]deck.WorkflowState
	at       time.Time
	inflight bool
	retryAt  time.Time
	problem  string
	stale    bool // a change was made: fetch again at once
}

// ApplyMine sets snap.Mine from the cache, and starts a background fetch
// when it is older than MineTTL. It never waits on the network.
func (r *Reader) ApplyMine(snap *deck.Snapshot) {
	if r.Key == nil || !r.Key.Configured() {
		snap.Mine = &deck.Mine{Off: "no Linear API key; set $" + EnvAPIKey + " or linear.api_key_command in the config file"}
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	c := &r.mine
	snap.Mine = &deck.Mine{
		Issues:  slices.Clone(c.issues),
		States:  c.states,
		Read:    c.at,
		Problem: c.problem,
	}
	if c.problem != "" {
		snap.Missing = append(snap.Missing, "Linear issues: "+c.problem)
	}
	now := r.now()
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

// FetchMine asks Linear for the Linear view's issues now and stores them.
// A failure is kept for Snapshot.Missing and delays the next try.
func (r *Reader) FetchMine(ctx context.Context) {
	issues, states, err := r.fetchMine(ctx)
	r.mu.Lock()
	defer r.mu.Unlock()
	c := &r.mine
	c.inflight, c.stale = false, false
	now := r.now()
	if err == nil {
		c.issues, c.states, c.at, c.problem = issues, states, now, ""
		return
	}
	var fe *fetchError
	if !errors.As(err, &fe) {
		fe = &fetchError{msg: err.Error()}
	}
	c.problem = fe.msg
	switch {
	case !fe.retryAt.IsZero():
		c.retryAt = fe.retryAt
	case fe.auth:
		c.retryAt = now.Add(authBackoff)
	default:
		c.retryAt = now.Add(errorBackoff)
	}
}

func (r *Reader) fetchMine(ctx context.Context) ([]deck.LinearIssue, map[string][]deck.WorkflowState, error) {
	key, source, err := r.key(ctx)
	if err != nil {
		return nil, nil, err
	}
	body, err := json.Marshal(map[string]any{"query": mineQuery, "variables": map[string]any{"types": mineStates}})
	if err != nil {
		return nil, nil, err
	}
	data, err := r.post(ctx, key, body)
	if err != nil {
		return nil, nil, r.keyError(err, key, source)
	}
	var mine gqlMine
	if raw := data["viewer"]; len(raw) == 0 || string(raw) == "null" {
		return nil, nil, &fetchError{msg: "Linear gave no issues for the key's user"}
	} else if err := json.Unmarshal(raw, &mine.Viewer); err != nil {
		return nil, nil, &fetchError{msg: "Linear's issues are not in the shape the deck reads"}
	}
	var issues []deck.LinearIssue
	for _, n := range mine.Viewer.AssignedIssues.Nodes {
		if is, ok := n.issue(); ok {
			issues = append(issues, is)
		}
	}
	states := map[string][]deck.WorkflowState{}
	var teams gqlTeams
	if json.Unmarshal(data["teams"], &teams) == nil {
		for _, t := range teams.Nodes {
			var ss []deck.WorkflowState
			for _, s := range t.States.Nodes {
				ss = append(ss, s.state())
			}
			SortStates(ss)
			states[t.Key] = ss
		}
	}
	var org gqlOrganization
	if json.Unmarshal(data["organization"], &org) == nil && validURLKey.MatchString(org.URLKey) {
		r.mu.Lock()
		r.urlKey = org.URLKey
		r.mu.Unlock()
	}
	return issues, states, nil
}

// SortStates puts states in Linear's order: by kind, then by position.
func SortStates(ss []deck.WorkflowState) {
	slices.SortStableFunc(ss, func(a, b deck.WorkflowState) int {
		if d := deck.StateOrder(a.Type) - deck.StateOrder(b.Type); d != 0 {
			return d
		}
		switch {
		case a.Position < b.Position:
			return -1
		case a.Position > b.Position:
			return 1
		}
		return 0
	})
}

// key gets the API key, as an error fit for the Sources view when there
// is none.
func (r *Reader) key(ctx context.Context) (key, source string, err error) {
	key, source, err = r.Key.Get(ctx)
	if err != nil {
		if errors.Is(err, errNoKey) {
			return "", "", &fetchError{msg: "no API key; set $" + EnvAPIKey + " or linear.api_key_command in the config file"}
		}
		// The command does not run again by itself: say how to retry.
		return "", "", &fetchError{msg: err.Error() + "; restart the deck to try again", retryAt: farFuture}
	}
	return key, source, nil
}

// keyError is err from post without the key in it, naming the key's
// source when Linear refused it.
func (r *Reader) keyError(err error, key, source string) error {
	var fe *fetchError
	if !errors.As(err, &fe) {
		return &fetchError{msg: redact(err.Error(), key)}
	}
	fe.msg = redact(fe.msg, key)
	if fe.auth {
		r.Key.Invalidate()
		fe.msg = "Linear refused the API key from " + source + " (" + fe.msg + ")"
	}
	return fe
}

// SetState moves the issue to the workflow state stateID.
func (r *Reader) SetState(ctx context.Context, is deck.LinearIssue, stateID string) error {
	err := r.update(ctx, is, map[string]any{"stateId": stateID}, func(c *deck.LinearIssue) {
		for _, s := range r.mine.states[c.TeamKey] {
			if s.ID == stateID {
				c.State = s
			}
		}
	})
	if err != nil {
		return fmt.Errorf("moving %s: %w", is.ID, err)
	}
	return nil
}

// SetPriority gives the issue Linear priority p, 0–4.
func (r *Reader) SetPriority(ctx context.Context, is deck.LinearIssue, p int) error {
	if p < 0 || p > 4 {
		return fmt.Errorf("priority %d is not 0–4", p)
	}
	err := r.update(ctx, is, map[string]any{"priority": p}, func(c *deck.LinearIssue) { c.Priority = p })
	if err != nil {
		return fmt.Errorf("setting the priority of %s: %w", is.ID, err)
	}
	return nil
}

// update runs issueUpdate with input, applies change to the cached issue
// when Linear says it succeeded, and asks for a fresh fetch.
func (r *Reader) update(ctx context.Context, is deck.LinearIssue, input map[string]any, change func(*deck.LinearIssue)) error {
	if is.UUID == "" {
		return errors.New("Linear has not given this issue")
	}
	key, source, err := r.key(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{
		"query":     "mutation DeckUpdate($id: String!, $input: IssueUpdateInput!) { issueUpdate(id: $id, input: $input) { success } }",
		"variables": map[string]any{"id": is.UUID, "input": input},
	})
	if err != nil {
		return err
	}
	gr, err := r.postResponse(ctx, key, body)
	var fe *fetchError
	if errors.As(err, &fe) && fe.denied {
		// The key reads, so it stays: it may only not write.
		return errors.New(writeRefused(source, redact(fe.msg, key)))
	}
	if err != nil {
		return r.keyError(err, key, source)
	}
	var res struct {
		Success bool `json:"success"`
	}
	if json.Unmarshal(gr.Data["issueUpdate"], &res) != nil || !res.Success {
		if len(gr.Errors) > 0 {
			msg := "Linear: " + redact(oneLine(gr.Errors[0].Message), key)
			if gr.Errors[0].forbidden() {
				return errors.New(writeRefused(source, msg))
			}
			return errors.New(msg)
		}
		return errors.New("Linear did not make the change")
	}
	r.mu.Lock()
	for i := range r.mine.issues {
		if r.mine.issues[i].UUID == is.UUID {
			change(&r.mine.issues[i])
		}
	}
	r.mine.stale = true
	r.mu.Unlock()
	return nil
}

// writeRefused says Linear would not let the key from source change an
// issue, with Linear's reason.
func writeRefused(source, reason string) string {
	return "Linear refused the change: the API key from " + source + " may not change issues (" + reason + ")"
}
