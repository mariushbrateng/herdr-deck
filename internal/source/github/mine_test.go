package github

import (
	"context"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// mineGh answers the GitHub views' searches with mine and a PR's full
// read with full.
type mineGh struct {
	mu         sync.Mutex
	mine, full []byte
	err        error
	calls      [][]string
}

func (f *mineGh) run(_ context.Context, args []string, _ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, args)
	if f.err != nil {
		return nil, f.err
	}
	if strings.Contains(strings.Join(args, " "), "DeckMine") {
		return f.mine, nil
	}
	return f.full, nil
}

func (f *mineGh) searches() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, a := range f.calls {
		if strings.Contains(strings.Join(a, " "), "DeckMine") {
			n++
		}
	}
	return n
}

func TestApplyMineReadsInTheBackground(t *testing.T) {
	f := &mineGh{mine: fixture(t, "mine.json")}
	clock := now
	r := &Reader{Run: f.run, Now: func() time.Time { return clock }}
	r.SetRepos([]string{"acme/webshop", " acme/deploy ", "not a repo", "acme/x repo:evil/y"})

	var s deck.Snapshot
	r.ApplyMine(&s)
	if s.PRs == nil || !s.PRs.Read.IsZero() || len(s.PRs.Mine) > 0 {
		t.Fatalf("first ApplyMine = %+v, want an empty pending answer", s.PRs)
	}
	r.Wait()
	args := strings.Join(f.calls[0], "\n")
	for _, want := range []string{
		"mine=is:pr is:open archived:false author:@me repo:acme/webshop repo:acme/deploy sort:updated-desc",
		"review=is:pr is:open archived:false review-requested:@me repo:acme/webshop repo:acme/deploy sort:updated-desc",
	} {
		if !strings.Contains(args, want) {
			t.Errorf("gh args lack %q:\n%s", want, args)
		}
	}
	if strings.Contains(args, "evil") {
		t.Error("an invalid repo reached the search")
	}

	s = deck.Snapshot{}
	r.ApplyMine(&s)
	got := s.PRs
	if !got.Read.Equal(now) || !slices.Equal(got.Repos, []string{"acme/webshop", "acme/deploy"}) {
		t.Errorf("Read %v, Repos %v", got.Read, got.Repos)
	}
	if len(got.Mine) != 2 || len(got.Review) != 1 {
		t.Fatalf("%d mine, %d to review; want 2 and 1 (the empty hit left out)", len(got.Mine), len(got.Review))
	}
	a := got.Mine[0]
	if a.Repo != "acme/webshop" || a.Title != "Checkout: keep the basket on reload" || a.Author != "alex" ||
		a.PR.Number != 2320 || a.PR.URL != prURL || a.PR.Review != "REVIEW_REQUIRED" || a.PR.MergeState != "BLOCKED" ||
		!a.PR.Live || a.CIState() != deck.CIPassed || !a.PR.CheckedAt.Equal(now) {
		t.Errorf("first PR = %+v", a)
	}
	b := got.Mine[1]
	if !b.PR.Draft || b.PR.Mergeable != "CONFLICTING" || b.PR.Review != "" || b.CIState() != deck.CINone {
		t.Errorf("draft PR = %+v", b)
	}
	c := got.Review[0]
	if c.Author != "ghost" || c.PR.Review != "CHANGES_REQUESTED" || c.CIState() != deck.CIRunning {
		t.Errorf("PR to review = %+v", c)
	}

	// Fresh for a minute, then read again.
	clock = now.Add(30 * time.Second)
	r.ApplyMine(&s)
	r.Wait()
	if f.searches() != 1 {
		t.Fatalf("%d searches within the minute, want 1", f.searches())
	}
	clock = now.Add(MineTTL)
	r.ApplyMine(&s)
	r.Wait()
	if f.searches() != 2 {
		t.Fatalf("%d searches after the minute, want 2", f.searches())
	}

	// Other repos read again at once.
	r.SetRepos([]string{"acme/webshop"})
	r.ApplyMine(&s)
	r.Wait()
	if f.searches() != 3 || strings.Contains(strings.Join(f.calls[len(f.calls)-1], " "), "acme/deploy") {
		t.Fatalf("new repos: %d searches, last %q", f.searches(), f.calls[len(f.calls)-1])
	}
}

func TestApplyMineLaysTheFullReadOver(t *testing.T) {
	f := &mineGh{mine: fixture(t, "mine.json"), full: fixture(t, "prs.json")}
	clock := now
	r := &Reader{Run: f.run, Now: func() time.Time { return clock }}
	var s deck.Snapshot
	r.ApplyMine(&s)
	r.Wait()

	// The drawer shows the first PR: it is read in full, a little later.
	clock = now.Add(10 * time.Second)
	r.Focus(prURL)
	r.Wait()
	s = deck.Snapshot{}
	r.ApplyMine(&s)
	p := s.PRs.Mine[0]
	if len(p.PR.Checks) == 0 || p.PR.Base != "main" || !p.PR.AutoMerge || len(p.PR.ReviewRequests) != 2 {
		t.Fatalf("full read not laid over: %+v", p.PR)
	}
	// The full read is newer: its checks (one failed) say the CI and its
	// merge state wins.
	if p.CIState() != deck.CIFailed || p.PR.MergeState != "BEHIND" || !p.PR.CheckedAt.Equal(clock) {
		t.Errorf("CI %q, merge state %q, checked %v", p.CIState(), p.PR.MergeState, p.PR.CheckedAt)
	}
	if s.PRs.Mine[1].PR.Checks != nil {
		t.Error("another PR got the full read")
	}

	// A newer list keeps its own state, and the full read's extras.
	clock = now.Add(MineTTL + 20*time.Second)
	r.ApplyMine(&s)
	r.Wait()
	s = deck.Snapshot{}
	r.ApplyMine(&s)
	p = s.PRs.Mine[0]
	if p.CIState() != deck.CIPassed || p.PR.MergeState != "BLOCKED" || len(p.PR.Checks) == 0 {
		t.Errorf("newer list: CI %q, merge state %q, %d checks", p.CIState(), p.PR.MergeState, len(p.PR.Checks))
	}
}

func TestApplyMineFailures(t *testing.T) {
	for _, tc := range []struct {
		name         string
		err          error
		off, problem string
	}{
		{"missing", &exec.Error{Name: "gh", Err: exec.ErrNotFound}, "gh is not installed (https://cli.github.com)", ""},
		{"logged out", &RunError{Code: 4, Stderr: "please run:  gh auth login"}, "gh is not logged in; run gh auth login", ""},
		{"network", &RunError{Code: 1, Stderr: "error connecting to api.github.com"}, "", "gh: error connecting to api.github.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &mineGh{err: tc.err}
			clock := now
			r := &Reader{Run: f.run, Now: func() time.Time { return clock }}
			var s deck.Snapshot
			r.ApplyMine(&s)
			r.Wait()
			s = deck.Snapshot{}
			r.ApplyMine(&s)
			if s.PRs.Off != tc.off || s.PRs.Problem != tc.problem {
				t.Errorf("off %q, problem %q", s.PRs.Off, s.PRs.Problem)
			}
			if missing := len(s.Missing) > 0; missing != (tc.problem != "") {
				t.Errorf("Missing = %q", s.Missing)
			}
			// It backs off: no second try at once.
			r.Wait()
			if f.searches() != 1 {
				t.Errorf("%d tries, want 1 until the back-off ends", f.searches())
			}
		})
	}
}
