package ui

// The view tabs (Linear, GitHub, Review), which ← and → switch between
// and V leaves for the tasks; and the GitHub and Review views: the user's
// open pull requests, and the ones they are asked to review, by repo,
// each with its review, CI and merge state. The drawer shows the selected
// PR's PR tab, read in full while it shows.

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// viewKind is what the list shows.
type viewKind int

const (
	viewTasks  viewKind = iota // the board's tasks
	viewLinear                 // the Linear issues assigned to the user
	viewGitHub                 // the user's open PRs
	viewReview                 // the open PRs the user is asked to review
)

// viewTabs are the view tabs in the header's order, which ← and → step
// through.
var viewTabs = []viewKind{viewLinear, viewGitHub, viewReview}

func (v viewKind) String() string {
	switch v {
	case viewLinear:
		return "Linear"
	case viewGitHub:
		return "GitHub"
	case viewReview:
		return "Review"
	}
	return "Tasks"
}

// toggleView switches between the view tabs, on the one last shown, and
// the tasks.
func (m *Model) toggleView() {
	if m.view == viewTasks {
		m.showView(m.lastTab)
		return
	}
	m.showView(viewTasks)
}

// stepView shows the view tab delta steps along; the ends stay put.
func (m *Model) stepView(delta int) {
	i := slices.Index(viewTabs, m.view) + delta
	if i < 0 || i >= len(viewTabs) {
		return
	}
	m.showView(viewTabs[i])
}

// showView makes the list show v from its top.
func (m *Model) showView(v viewKind) {
	if v != viewTasks {
		m.lastTab = v
	}
	if v == m.view {
		return
	}
	m.view = v
	m.moved, m.cursor, m.listOff = false, 0, 0
	m.dfocus = false
	m.tab = tabOverview
	m.setMode(modeRow)
	m.SetSnapshot(m.snap)
}

// viewCount is the number a view tab shows after its name, or -1 before
// its source has answered.
func (m Model) viewCount(v viewKind) int {
	switch v {
	case viewLinear:
		if mine := m.snap.Mine; mine != nil && !mine.Read.IsZero() {
			return len(mine.Issues)
		}
	case viewGitHub, viewReview:
		if prs := m.snap.PRs; prs != nil && !prs.Read.IsZero() {
			return len(viewPRs(prs, v))
		}
	}
	return -1
}

// viewTabsText is the header's view tabs, styled as the drawer's tabs
// (without their padding in a narrow header), and each one's columns from
// the start of the text.
func (m Model) viewTabsText() (string, []zone) {
	var b strings.Builder
	var zones []zone
	x := 0
	padding := " "
	if m.width < wideMin {
		padding = ""
	}
	for i, v := range viewTabs {
		label := padding + v.String()
		if n := m.viewCount(v); n >= 0 {
			label += " (" + strconv.Itoa(n) + ")"
		}
		label += padding
		if i > 0 {
			b.WriteString(" ")
			x++
		}
		b.WriteString(tabStyle(v == m.view, true, m.light).Render(label))
		w := ansi.StringWidth(label)
		zones = append(zones, zone{x0: x, x1: x + w, act: action{n: int(v)}})
		x += w
	}
	return b.String(), zones
}

// viewTabsAt is the header column the view tabs start at: after the
// project's name, which a narrow header leaves out.
func (m Model) viewTabsAt() int {
	if m.width < wideMin {
		return 1
	}
	return 1 + ansi.StringWidth(m.snap.Project.Title()) + 2
}

// viewTabAt is the view tab at header column x, or viewTasks for none.
func (m Model) viewTabAt(x int) viewKind {
	if m.view == viewTasks {
		return viewTasks
	}
	_, zones := m.viewTabsText()
	at := m.viewTabsAt()
	for _, z := range zones {
		if x >= at+z.x0 && x < at+z.x1 {
			return viewKind(z.act.n)
		}
	}
	return viewTasks
}

// viewPRs are the PRs view v lists.
func viewPRs(prs *deck.MyPRs, v viewKind) []deck.MyPR {
	if v == viewReview {
		return prs.Review
	}
	return prs.Mine
}

// linearID is a Linear issue ID anywhere in a PR's title, as commit and
// PR titles carry it: "fix(chat): … (ENG-1692)".
var linearID = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,5}-\d+\b`)

// buildPullRows lays out the GitHub or Review view: a list per repo, in
// the order github.repos names them (else by name), each with the most
// recently changed PR first, as GitHub gives them.
func buildPullRows(snap deck.Snapshot, v viewKind, folds map[string]bool, folded []string) []row {
	if snap.PRs == nil {
		return nil
	}
	issues := map[string]deck.LinearIssue{}
	if snap.Mine != nil {
		for _, is := range snap.Mine.Issues {
			issues[is.ID] = is
		}
	}
	prs := viewPRs(snap.PRs, v)
	byRepo := map[string][]deck.MyPR{}
	var repos []string
	for _, p := range prs {
		if _, ok := byRepo[p.Repo]; !ok {
			repos = append(repos, p.Repo)
		}
		byRepo[p.Repo] = append(byRepo[p.Repo], p)
	}
	rank := func(repo string) int {
		if i := slices.IndexFunc(snap.PRs.Repos, func(r string) bool { return strings.EqualFold(r, repo) }); i >= 0 {
			return i
		}
		return len(snap.PRs.Repos)
	}
	slices.SortStableFunc(repos, func(a, b string) int {
		if d := rank(a) - rank(b); d != 0 {
			return d
		}
		return strings.Compare(a, b)
	})
	var rows []row
	for _, repo := range repos {
		ps := byRepo[repo]
		isFolded, ok := folds[repo]
		if !ok {
			isFolded = startsFolded(repo, folded)
		}
		if len(rows) > 0 {
			rows = append(rows, row{kind: rowGap, key: "gap:" + repo})
		}
		rows = append(rows, row{kind: rowHeading, list: repo, key: "head:" + repo, count: len(ps), folded: isFolded, foldable: true})
		if isFolded {
			continue
		}
		for _, p := range ps {
			links := []deck.Link{{Kind: deck.LinkGitHub, Label: fmt.Sprintf("#%d", p.PR.Number), URL: p.PR.URL}}
			for _, id := range linearID.FindAllString(p.Title, -1) {
				if is, ok := issues[id]; ok && is.URL != "" {
					links = deck.AppendLink(links, deck.Link{Kind: deck.LinkLinear, Label: id, URL: is.URL})
				}
			}
			slices.SortStableFunc(links, func(a, b deck.Link) int { return int(a.Kind) - int(b.Kind) })
			rows = append(rows, row{kind: rowWork, list: repo, key: "pr:" + p.PR.URL, pull: &p, links: links})
		}
	}
	return rows
}

// pullCols are the GitHub views' column widths: at 72 columns the
// review, CI and merge state in words, below that a glyph each.
type pullCols struct {
	wide                         bool
	work, review, ci, merge, lnk int
}

func pullColumns(w int) pullCols {
	if w >= wideMin {
		c := pullCols{wide: true, review: 10, ci: 9, merge: 10, lnk: 5}
		c.work = max(w-4-2-c.review-2-c.ci-2-c.merge-2-c.lnk, 6)
		return c
	}
	c := pullCols{review: 1, ci: 1, merge: 1, lnk: 5}
	c.work = max(w-4-2-5-2-c.lnk, 6)
	return c
}

// pullColumnTitles are the GitHub views' column titles.
func (m Model) pullColumnTitles(w int) string {
	c := pullColumns(w)
	s := "    " + pad("PR", c.work) + "  "
	if c.wide {
		s += pad("REVIEW", c.review) + "  " + pad("CI", c.ci) + "  " + pad("MERGE", c.merge) + "  "
	} else {
		s += "R C M  "
	}
	return s + "LINKS"
}

// reviewState is a PR's review decision as a glyph and words: approved
// green, changes requested red, waiting for a review yellow, none needed
// dim.
func reviewState(pr deck.PullRequest) (glyph, text string, st lipgloss.Style) {
	switch pr.Review {
	case "APPROVED":
		return "✓", "approved", okStyle
	case "CHANGES_REQUESTED":
		return "✕", "changes", failStyle
	case "REVIEW_REQUIRED":
		return "◌", "waiting", warnStyle
	}
	return "–", "none", dim
}

// ciState is a PR's checks taken together: passed green, running yellow,
// failed red, none dim.
func ciState(p deck.MyPR) (glyph, text string, st lipgloss.Style) {
	switch p.CIState() {
	case deck.CIPassed:
		return "✓", "passed", okStyle
	case deck.CIRunning:
		return "◐", "running", warnStyle
	case deck.CIFailed:
		return "✕", "failed", failStyle
	}
	return "–", "none", dim
}

// mergeState says whether a PR can merge: ready green, a conflict red,
// behind its base or blocked (by reviews or checks) yellow, a draft or
// not worked out yet dim.
func mergeState(pr deck.PullRequest) (glyph, text string, st lipgloss.Style) {
	switch {
	case pr.Mergeable == "CONFLICTING" || pr.MergeState == "DIRTY":
		return "✕", "conflict", failStyle
	case pr.Draft || pr.MergeState == "DRAFT":
		return "◇", "draft", dim
	case pr.MergeState == "BEHIND":
		return "↓", "behind", warnStyle
	case pr.MergeState == "BLOCKED":
		return "◌", "blocked", warnStyle
	case pr.Mergeable == "MERGEABLE":
		return "✓", "ready", okStyle
	}
	return "…", "checking", dim
}

// pullGlyph is a PR's mark: a draft's diamond, red when something must be
// fixed (a conflict, failed checks, changes requested), green when it can
// merge, else yellow while it waits.
func pullGlyph(p deck.MyPR) (string, lipgloss.Style) {
	pr := p.PR
	_, merge, _ := mergeState(pr)
	switch {
	case pr.Draft:
		return "◇", dim
	case merge == "conflict" || p.CIState() == deck.CIFailed || pr.Review == "CHANGES_REQUESTED":
		return "●", failStyle
	case merge == "ready" && p.CIState() != deck.CIRunning:
		return "●", okStyle
	}
	return "◐", warnStyle
}

// pullLine is a PR's row: its mark, number and title (a draft's dim, a
// PR to review with its author), its review, CI and merge state, and its
// links.
func (m Model) pullLine(r row, sel bool, w int) string {
	mark, markSt := " ", plain
	if sel {
		mark = "▸"
		if m.dfocus {
			markSt = dim
		}
	}
	p := r.pull
	c := pullColumns(w)
	glyph, gst := pullGlyph(*p)
	work := chipStyle(deck.LinkGitHub).Render(fmt.Sprintf("#%d", p.PR.Number)) + " "
	if p.PR.Draft {
		work += dim.Render("draft") + " "
	}
	if m.view == viewReview {
		work += dim.Render(p.Author) + " "
	}
	titleSt := plain
	if p.PR.Draft {
		titleSt = dim
	}
	work += inline(p.Title, titleSt, m.light)
	cells := []cell{
		{text: " "},
		{text: mark, style: markSt},
		{text: glyph, style: gst},
		{text: " "},
		{text: work, raw: true, width: c.work},
		{text: "  "},
	}
	for i, state := range []func() (string, string, lipgloss.Style){
		func() (string, string, lipgloss.Style) { return reviewState(p.PR) },
		func() (string, string, lipgloss.Style) { return ciState(*p) },
		func() (string, string, lipgloss.Style) { return mergeState(p.PR) },
	} {
		g, text, st := state()
		width := []int{c.review, c.ci, c.merge}[i]
		if c.wide {
			cells = append(cells, cell{text: g + " " + text, style: st, width: width}, cell{text: "  "})
		} else {
			cells = append(cells, cell{text: g, style: st}, cell{text: " "})
		}
	}
	if !c.wide {
		cells = append(cells, cell{text: " "})
	}
	cells = append(cells, pullLinkCells(r.links, c.lnk)...)
	return m.finish(renderCells(cells), sel, w)
}

// pullLinkCells is a PR row's LINKS column: G for the PR, then L when its
// title names a Linear issue assigned to the user.
func pullLinkCells(links []deck.Link, width int) []cell {
	var cells []cell
	used := 0
	for _, k := range []deck.LinkKind{deck.LinkGitHub, deck.LinkLinear} {
		if !slices.ContainsFunc(links, func(l deck.Link) bool { return l.Kind == k }) {
			continue
		}
		text := k.Badge()
		if used > 0 {
			text = " " + text
		}
		if used+ansi.StringWidth(text) > width {
			break
		}
		st := linkStyle(k)
		if k == deck.LinkGitHub {
			st = bold.Inherit(reviewStyle)
		}
		cells = append(cells, cell{text: text, style: st})
		used += ansi.StringWidth(text)
	}
	return append(cells, cell{text: strings.Repeat(" ", max(width-used, 0))})
}

// emptyPulls says why a GitHub view has no rows.
func (m Model) emptyPulls() string {
	prs := m.snap.PRs
	where := "in any repo"
	if prs != nil && len(prs.Repos) > 0 {
		where = "in " + strings.Join(prs.Repos, ", ")
	}
	switch {
	case prs == nil:
		return "GitHub is off (github.enabled in the settings). V shows the tasks."
	case prs.Off != "":
		return "GitHub: " + prs.Off + ". V shows the tasks."
	case prs.Problem != "" && prs.Read.IsZero():
		return "GitHub: " + prs.Problem + ". V shows the tasks."
	case prs.Read.IsZero():
		return "Reading your pull requests…"
	case m.view == viewReview:
		return "Nobody has asked you to review an open pull request " + where + "."
	}
	return "You have no open pull requests " + where + "."
}

// pullCard is the drawer's card for a PR: its title, with its review, CI
// and merge state at the right; then its repo, number, author and when it
// changed.
func (m Model) pullCard(r row, w int) []string {
	p := r.pull
	var right []string
	for _, state := range [](func() (string, string, lipgloss.Style)){
		func() (string, string, lipgloss.Style) { return reviewState(p.PR) },
		func() (string, string, lipgloss.Style) { return ciState(*p) },
		func() (string, string, lipgloss.Style) { return mergeState(p.PR) },
	} {
		if g, text, st := state(); text != "none" {
			right = append(right, st.Render(g+" "+text))
		}
	}
	line1 := spread(" "+inline(p.Title, bold, m.light), strings.Join(right, "  ")+" ", w)
	parts := []string{p.Repo, fmt.Sprintf("#%d", p.PR.Number), p.Author}
	if p.PR.Draft {
		parts = append(parts, "draft")
	}
	if !p.Updated.IsZero() {
		parts = append(parts, "updated "+ago(m.opt.Now().Sub(p.Updated)))
	}
	return []string{line1, " " + dim.Render(ansi.Truncate(strings.Join(parts, " · "), max(w-2, 1), "…"))}
}
