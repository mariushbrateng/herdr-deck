package ui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/FredricW/herdr-deck/internal/deck"
	"github.com/FredricW/herdr-deck/internal/source/fake"
)

var (
	left  = tea.KeyPressMsg{Code: tea.KeyLeft}
	right = tea.KeyPressMsg{Code: tea.KeyRight}
	enter = tea.KeyPressMsg{Code: tea.KeyEnter}
)

// prSnap is the sample board with the sample Linear issues and PRs.
func prSnap() deck.Snapshot {
	snap := mineSnap()
	snap.PRs = fake.PRs(now)
	return snap
}

// githubModel is the deck on the GitHub view, with the PRs the reader
// was told to read in focus and detail.
func githubModel(t *testing.T, snap deck.Snapshot, w, h int) (Model, *opened, *[]string) {
	t.Helper()
	var watched []string
	m, o := newModelWith(t, snap, w, h, func(o *Options) {
		o.LinearView = true
		o.FocusPR = func(url string) { watched = append(watched, "focus "+url) }
		o.DetailPR = func(url string) { watched = append(watched, "detail "+url) }
	})
	m, _ = press(m, right)
	if m.view != viewGitHub {
		t.Fatalf("→ from Linear shows %v, want GitHub", m.view)
	}
	return m, o, &watched
}

func TestPullsGolden(t *testing.T) {
	for _, w := range []int{100, 60} {
		m, _, _ := githubModel(t, prSnap(), w, 40)
		golden(t, "pulls-"+strconv.Itoa(w), m)
	}
	m, _, _ := githubModel(t, prSnap(), 100, 30)
	m, _ = press(m, right)
	golden(t, "pulls-review-100", m)
}

func TestGitHubRows(t *testing.T) {
	m, _, _ := githubModel(t, prSnap(), 100, 40)
	var got []string
	for _, r := range m.rows {
		switch {
		case r.kind == rowHeading:
			got = append(got, "# "+r.list)
		case r.pull != nil:
			got = append(got, strconv.Itoa(r.pull.PR.Number))
		}
	}
	// By repo in github.repos' order; GitHub's order, newest first, within.
	want := "# acme/webshop,2412,2420,2398,2377,# acme/deploy,88,85"
	if s := strings.Join(got, ","); s != want {
		t.Errorf("rows = %s\nwant   %s", s, want)
	}
	s := screen(m)
	for _, want := range []string{
		"Linear (7)", "GitHub (6)", "Review (2)",
		"PR", "REVIEW", "CI", "MERGE", "LINKS",
		"#2412 Restrict org admin to own org in stats (ENG-914)",
		"✓ approved  ✓ passed   ✓ ready",
		"◌ waiting   ◐ running  ◌ blocked",
		"✕ changes   ✕ failed   ◌ blocked",
		"◌ waiting   ✓ passed   ✕ conflict",
		"#88 draft Deploy the worker",
		"– none      – none     ◇ draft",
		"✓ approved  ✓ passed   ↓ behind",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
}

func TestViewTabsArrows(t *testing.T) {
	m, _ := newModelWith(t, prSnap(), 100, 40, func(o *Options) { o.LinearView = true })
	step := func(msg tea.Msg, want viewKind) {
		t.Helper()
		m, _ = press(m, msg)
		if m.view != want {
			t.Fatalf("after %v the view is %v, want %v", msg, m.view, want)
		}
	}
	step(left, viewLinear) // the first tab stays put
	step(right, viewGitHub)
	step(right, viewReview)
	step(right, viewReview)
	step(left, viewGitHub)
	// V shows the tasks, where the arrows do nothing, and comes back to
	// the tab last shown.
	step(keys("V")[0], viewTasks)
	step(right, viewTasks)
	step(keys("V")[0], viewGitHub)
	// In the focused drawer the arrows are the PR tab's.
	step(tab, viewGitHub)
	if !m.dfocus {
		t.Fatal("tab did not focus the drawer")
	}
	step(right, viewGitHub)
	step(esc, viewGitHub)
	step(right, viewReview)
}

func TestViewTabsClick(t *testing.T) {
	m, _ := newModelWith(t, prSnap(), 100, 40, func(o *Options) { o.LinearView = true })
	line := strings.Split(screen(m), "\n")[0]
	x := strings.Index(line, "Review (2)")
	if x < 0 {
		t.Fatalf("no Review tab in %q", line)
	}
	m, _ = press(m, click(x+2, 0))
	if m.view != viewReview {
		t.Fatalf("a click on Review shows %v", m.view)
	}
	x = strings.Index(line, "Linear (7)")
	m, _ = press(m, click(x, 0))
	if m.view != viewLinear {
		t.Fatalf("a click on Linear shows %v", m.view)
	}
}

func TestGitHubDrawerAndLinks(t *testing.T) {
	m, o, watched := githubModel(t, prSnap(), 100, 40)
	r, _ := m.selected()
	if r.pull == nil || r.pull.PR.Number != 2412 {
		t.Fatalf("selected %+v, want #2412", r)
	}
	url := "https://github.com/acme/webshop/pull/2412"
	// The drawer shows the PR tab, so the reader reads it in full.
	if m.curTab() != tabPR {
		t.Errorf("drawer tab = %v, want PR", m.curTab())
	}
	if w := strings.Join(*watched, "\n"); !strings.Contains(w, "detail "+url) || !strings.Contains(w, "focus "+url) {
		t.Errorf("reader told %q", w)
	}
	s := screen(m)
	for _, want := range []string{"acme/webshop · #2412 · you · updated 20m ago", "── Status ──", "G L"} {
		if !strings.Contains(s, want) {
			t.Errorf("screen lacks %q:\n%s", want, s)
		}
	}
	// ↵ and g open the PR; 1 the Linear issue its title names.
	m, _ = press(m, enter)
	m, _ = press(m, keys("g")...)
	m, _ = press(m, keys("1")...)
	want := []string{url, url, "https://linear.app/acme/issue/ENG-914"}
	if strings.Join(o.urls, " ") != strings.Join(want, " ") {
		t.Errorf("opened %q, want %q", o.urls, want)
	}
}

func TestGitHubEmpty(t *testing.T) {
	for _, tc := range []struct {
		name string
		prs  *deck.MyPRs
		want string
	}{
		{"off", nil, "GitHub is off (github.enabled in the settings)"},
		{"gh", &deck.MyPRs{Off: "gh is not logged in; run gh auth login"}, "GitHub: gh is not logged in; run gh auth login."},
		{"pending", &deck.MyPRs{}, "Reading your pull requests…"},
		{"none", &deck.MyPRs{Read: now, Repos: []string{"acme/webshop"}}, "You have no open pull requests in acme/webshop."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			snap := mineSnap()
			snap.PRs = tc.prs
			m, _, _ := githubModel(t, snap, 100, 30)
			if s := screen(m); !strings.Contains(s, tc.want) {
				t.Errorf("screen lacks %q:\n%s", tc.want, s)
			}
		})
	}
}

func TestPullStateColours(t *testing.T) {
	green, yellow, red := okStyle.GetForeground(), warnStyle.GetForeground(), failStyle.GetForeground()
	for _, tc := range []struct {
		name string
		get  func() (string, string, lipgloss.Style)
		want any
	}{
		{"approved", func() (string, string, lipgloss.Style) { return reviewState(deck.PullRequest{Review: "APPROVED"}) }, green},
		{"waiting", func() (string, string, lipgloss.Style) {
			return reviewState(deck.PullRequest{Review: "REVIEW_REQUIRED"})
		}, yellow},
		{"changes", func() (string, string, lipgloss.Style) {
			return reviewState(deck.PullRequest{Review: "CHANGES_REQUESTED"})
		}, red},
		{"ci passed", func() (string, string, lipgloss.Style) { return ciState(deck.MyPR{CI: "SUCCESS"}) }, green},
		{"ci running", func() (string, string, lipgloss.Style) { return ciState(deck.MyPR{CI: "PENDING"}) }, yellow},
		{"ci failed", func() (string, string, lipgloss.Style) { return ciState(deck.MyPR{CI: "ERROR"}) }, red},
		{"ready", func() (string, string, lipgloss.Style) {
			return mergeState(deck.PullRequest{Mergeable: "MERGEABLE", MergeState: "CLEAN"})
		}, green},
		{"conflict", func() (string, string, lipgloss.Style) {
			return mergeState(deck.PullRequest{Mergeable: "CONFLICTING", MergeState: "DIRTY"})
		}, red},
	} {
		if _, _, st := tc.get(); st.GetForeground() != tc.want {
			t.Errorf("%s: colour %v, want %v", tc.name, st.GetForeground(), tc.want)
		}
	}
}
