package linear

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// rt answers every request with answer, recording the requests.
type rt struct {
	mu     sync.Mutex
	bodies []map[string]any
	auth   []string
	answer func(body map[string]any) (int, string)
}

func (f *rt) RoundTrip(req *http.Request) (*http.Response, error) {
	var body map[string]any
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return nil, err
	}
	f.mu.Lock()
	f.bodies = append(f.bodies, body)
	f.auth = append(f.auth, req.Header.Get("Authorization"))
	f.mu.Unlock()
	code, text := f.answer(body)
	return &http.Response{StatusCode: code, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(text)), Request: req}, nil
}

const mineAnswer = `{"data": {
  "viewer": {"assignedIssues": {"nodes": [
    {"id": "u1", "identifier": "ENG-791", "title": " Guard på migrations ", "url": "https://linear.app/acme/issue/ENG-791",
     "priority": 1, "description": "Fail CI on **two heads**.", "updatedAt": "2026-10-22T08:00:00Z",
     "state": {"id": "s3", "name": "In Progress", "type": "started", "position": 2},
     "team": {"key": "ENG", "name": "Engineering"}, "project": {"name": "Hardening"},
     "labels": {"nodes": [{"name": "Backend", "color": "#4ea7fc"}]},
     "comments": {"nodes": [
       {"body": "newer", "createdAt": "2026-10-21T10:00:00Z", "user": {"displayName": "Ana"}},
       {"body": "older", "createdAt": "2026-10-20T10:00:00Z", "user": null}]}},
    {"id": "u2", "identifier": "INF-112", "title": "Slett test-brukere", "url": "javascript:alert(1)",
     "priority": 0, "description": null, "updatedAt": "2026-10-21T08:00:00Z",
     "state": {"id": "i1", "name": "Todo", "type": "unstarted", "position": 1},
     "team": {"key": "INF", "name": "Infra"}, "project": null,
     "labels": {"nodes": []}, "comments": {"nodes": []}},
    {"id": "u3", "identifier": "ENG-1", "title": "no state", "state": null}
  ]}},
  "teams": {"nodes": [{"key": "ENG", "states": {"nodes": [
    {"id": "s5", "name": "Done", "type": "completed", "position": 0},
    {"id": "s3", "name": "In Progress", "type": "started", "position": 2},
    {"id": "s1", "name": "New", "type": "backlog", "position": 0},
    {"id": "s4", "name": "In Review", "type": "started", "position": 3},
    {"id": "s2", "name": "Todo", "type": "unstarted", "position": 1}]}}]},
  "organization": {"urlKey": "acme"}
}}`

func mineReader(f *rt) *Reader {
	r := NewReader(func(k string) string {
		if k == EnvAPIKey {
			return testKey
		}
		return ""
	}, nil)
	r.HTTP = &http.Client{Transport: f}
	r.Now = func() time.Time { return time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC) }
	return r
}

func TestFetchMine(t *testing.T) {
	f := &rt{answer: func(map[string]any) (int, string) { return 200, mineAnswer }}
	r := mineReader(f)
	var snap deck.Snapshot
	r.ApplyMine(&snap)
	if snap.Mine == nil || !snap.Mine.Read.IsZero() {
		t.Fatalf("before the first answer: %+v", snap.Mine)
	}
	r.Wait()
	snap = deck.Snapshot{}
	r.ApplyMine(&snap)
	mine := snap.Mine
	if mine.Problem != "" || mine.Read.IsZero() {
		t.Fatalf("mine = %+v", mine)
	}
	if len(mine.Issues) != 2 {
		t.Fatalf("issues = %+v", mine.Issues)
	}
	g := mine.Issues[0]
	if g.UUID != "u1" || g.ID != "ENG-791" || g.Title != "Guard på migrations" || g.Priority != 1 ||
		g.State.Name != "In Progress" || g.State.ID != "s3" || g.TeamKey != "ENG" || g.Team != "Engineering" ||
		g.Project != "Hardening" || g.Description != "Fail CI on **two heads**." || g.URL != "https://linear.app/acme/issue/ENG-791" {
		t.Errorf("issue = %+v", g)
	}
	if len(g.Labels) != 1 || g.Labels[0] != (deck.Label{Name: "Backend", Color: "#4ea7fc"}) {
		t.Errorf("labels = %+v", g.Labels)
	}
	// Oldest first; an integration's comment has no user.
	if len(g.Comments) != 2 || g.Comments[0].Body != "older" || g.Comments[0].Author != "Linear" || g.Comments[1].Author != "Ana" {
		t.Errorf("comments = %+v", g.Comments)
	}
	if u := mine.Issues[1].URL; u != "" {
		t.Errorf("a URL that is not https was kept: %q", u)
	}
	var names []string
	for _, s := range mine.States["ENG"] {
		names = append(names, s.Name)
	}
	if got := strings.Join(names, ","); got != "New,Todo,In Progress,In Review,Done" {
		t.Errorf("states = %s", got)
	}
	if f.auth[0] != testKey {
		t.Errorf("Authorization = %q", f.auth[0])
	}
	vars, _ := f.bodies[0]["variables"].(map[string]any)
	if types, _ := json.Marshal(vars["types"]); string(types) != `["started","unstarted","backlog"]` {
		t.Errorf("types = %s", types)
	}
	if q, _ := f.bodies[0]["query"].(string); !strings.Contains(q, "assignedIssues") {
		t.Errorf("query = %s", q)
	}
	// Within MineTTL nothing is fetched again.
	r.ApplyMine(&deck.Snapshot{})
	r.Wait()
	if len(f.bodies) != 1 {
		t.Errorf("fetched %d times", len(f.bodies))
	}
	if ws, _ := r.Workspace(context.Background()); ws != "acme" {
		t.Errorf("workspace = %q", ws)
	}
}

func TestMineWithoutKey(t *testing.T) {
	r := NewReader(func(string) string { return "" }, nil)
	var snap deck.Snapshot
	r.ApplyMine(&snap)
	if snap.Mine == nil || !strings.Contains(snap.Mine.Off, "no Linear API key") {
		t.Fatalf("mine = %+v", snap.Mine)
	}
}

func TestMineRefusedKey(t *testing.T) {
	f := &rt{answer: func(map[string]any) (int, string) { return 401, `{"errors":[{"message":"bad ` + testKey + `"}]}` }}
	r := mineReader(f)
	r.FetchMine(context.Background())
	var snap deck.Snapshot
	r.ApplyMine(&snap)
	if !strings.Contains(snap.Mine.Problem, "refused the API key") {
		t.Errorf("problem = %q", snap.Mine.Problem)
	}
	if len(snap.Missing) != 1 || strings.Contains(strings.Join(snap.Missing, " "), testKey) {
		t.Errorf("missing = %q", snap.Missing)
	}
	// It waits before trying again.
	r.ApplyMine(&deck.Snapshot{})
	r.Wait()
	if len(f.bodies) != 1 {
		t.Errorf("fetched %d times", len(f.bodies))
	}
}

func TestSetStateAndPriority(t *testing.T) {
	var mutations []map[string]any
	f := &rt{answer: func(body map[string]any) (int, string) {
		if q, _ := body["query"].(string); strings.HasPrefix(q, "mutation") {
			mutations = append(mutations, body["variables"].(map[string]any))
			return 200, `{"data":{"issueUpdate":{"success":true}}}`
		}
		return 200, mineAnswer
	}}
	r := mineReader(f)
	r.FetchMine(context.Background())
	var snap deck.Snapshot
	r.ApplyMine(&snap)
	is := snap.Mine.Issues[0]
	if err := r.SetState(context.Background(), is, "s4"); err != nil {
		t.Fatal(err)
	}
	if err := r.SetPriority(context.Background(), is, 3); err != nil {
		t.Fatal(err)
	}
	if len(mutations) != 2 {
		t.Fatalf("mutations = %v", mutations)
	}
	in, _ := json.Marshal(mutations[0])
	if string(in) != `{"id":"u1","input":{"stateId":"s4"}}` {
		t.Errorf("state mutation = %s", in)
	}
	in, _ = json.Marshal(mutations[1])
	if string(in) != `{"id":"u1","input":{"priority":3}}` {
		t.Errorf("priority mutation = %s", in)
	}
	// The cache shows the change at once and is fetched again.
	snap = deck.Snapshot{}
	r.ApplyMine(&snap)
	if g := snap.Mine.Issues[0]; g.State.Name != "In Review" || g.Priority != 3 {
		t.Errorf("after the change: %+v", g)
	}
	r.Wait()
	if n := len(f.bodies); n != 4 {
		t.Errorf("requests = %d, want a fetch after the changes", n)
	}
	if err := r.SetPriority(context.Background(), is, 5); err == nil {
		t.Error("priority 5 was sent")
	}
}

func TestSetStateRefused(t *testing.T) {
	f := &rt{answer: func(body map[string]any) (int, string) {
		return 200, `{"data":{"issueUpdate":null},"errors":[{"message":"Forbidden: key ` + testKey + ` has no write access"}]}`
	}}
	r := mineReader(f)
	err := r.SetState(context.Background(), deck.LinearIssue{UUID: "u1", ID: "ENG-791"}, "s4")
	if err == nil || !strings.Contains(err.Error(), "no write access") || strings.Contains(err.Error(), testKey) {
		t.Errorf("err = %v", err)
	}
	if err := r.SetState(context.Background(), deck.LinearIssue{ID: "ENG-9"}, "s4"); err == nil {
		t.Error("an issue Linear has not given was changed")
	}
}

func TestSetStateNoWriteAccess(t *testing.T) {
	for _, c := range []struct {
		name   string
		status int
		answer string
	}{
		{"forbidden code", 400, `{"data":null,"errors":[{"message":"Invalid scope: write required","extensions":{"code":"FORBIDDEN","type":"forbidden"}}]}`},
		{"forbidden beside data", 200, `{"data":{"issueUpdate":null},"errors":[{"message":"Invalid scope: write required","extensions":{"code":"FORBIDDEN"}}]}`},
		{"HTTP 403", 403, `{}`},
	} {
		f := &rt{answer: func(map[string]any) (int, string) { return c.status, c.answer }}
		r := mineReader(f)
		err := r.SetState(context.Background(), deck.LinearIssue{UUID: "u1", ID: "ENG-791"}, "s4")
		if err == nil || !strings.Contains(err.Error(), "may not change issues") || strings.Contains(err.Error(), testKey) {
			t.Errorf("%s: err = %v", c.name, err)
		}
	}
}
