package ui

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// The drawer's header card and tab bar (docs/design/drawer/README.md).

// rowDrawer is the selected row's drawer: the header card, the tab bar and
// the tab's content, h lines of which show from the drawer's offset. The
// card and tab bar take width columns, the content inner: fewer when it
// leaves room for a scrollbar.
func (m Model) rowDrawer(r row, links []deck.Link, width, inner, h int) *drawer {
	if r.kind == rowHeading {
		return m.projectDrawer(r, width)
	}
	d := newDrawer(inner, "")
	d.light = m.light
	d.card = m.card(r, width)
	if r.issue != nil {
		d.card = m.issueCard(r, width)
	}
	if r.pull != nil {
		d.card = m.pullCard(r, width)
	}
	tab := m.curTab()
	if m.dfocus {
		d.cur = m.dcur
	}
	t, hasThread := r.thread()
	switch tab {
	case tabFiles:
		m.filesTab(d)
	case tabCommits:
		m.commitsTab(d, r)
	case tabImpact:
		m.impactTab(d)
	case tabPR:
		m.prTab(d, r, links)
	case tabLog:
		m.logTab(d, t, links)
	default:
		if r.issue != nil {
			m.issueOverview(d, r, links)
		} else {
			m.overview(d, r, links, h)
		}
	}
	off := clamp(m.drawerOff, 0, max(len(d.lines)-h, 0))
	d.tabs, d.tabZones = m.tabBar(r, hasThread, tab, d.moreHint(tab, off, h), width)
	return d
}

// moreHint says what is below the drawer's end: Overview's section names
// at 80 columns, else how many lines.
func (d *drawer) moreHint(tab tabKind, off, h int) string {
	end := off + h
	below := len(d.lines) - end
	if below <= 0 {
		return ""
	}
	if (tab == tabOverview || tab == tabPR) && d.width >= wideMin {
		var names []string
		for _, s := range d.sections {
			if s.at >= end {
				names = append(names, s.name)
			}
		}
		if len(names) > 0 {
			if hint := "↓ " + strings.Join(names, " · "); ansi.StringWidth(hint) <= d.width/3 {
				return hint
			}
		}
	}
	return fmt.Sprintf("↓ %d more", below)
}

// tabStyle is a tab's label style: bold near-black on blue when active,
// plain on a grey just off the background otherwise, dim on it without
// data. Dark
// text reads on standard and pastel blues alike, where white is lost on a
// pastel one.
func tabStyle(active, enabled, light bool) lipgloss.Style {
	if active {
		fg := tabTextDark
		if light {
			fg = tabTextLight
		}
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.ANSIColor(fg)).Background(colBlue)
	}
	bg := tabIdleDark
	if light {
		bg = tabIdleLight
	}
	st := lipgloss.NewStyle().Background(lipgloss.ANSIColor(bg))
	if !enabled {
		st = st.Faint(true)
	}
	return st
}

// tabBar is the tab bar's line and its clickable tabs: each label with one
// space of padding on its background, one space apart, and the hint at
// the right, the whole bar one column in from either edge. PR is left out
// on a row without one, and Impact while it is off.
func (m Model) tabBar(r row, hasThread bool, active tabKind, hint string, width int) (string, []zone) {
	var b strings.Builder
	var zones []zone
	x := 1
	for tab := range numTabs {
		enabled := m.tabEnabled(r, tab)
		// PR shows only for a row with a PR, and Impact only while
		// it is on (arch.enabled); the others show dim.
		if tab == tabPR && !enabled || tab == tabImpact && m.opt.Arch == nil || r.issue != nil && tab != tabOverview || r.pull != nil && tab != tabPR {
			continue
		}
		label := tab.String()
		if r.issue != nil {
			label = "Linear"
		}
		if enabled {
			switch tab {
			case tabFiles:
				if _, d, ok := m.diff(); !ok {
					label += " …"
				} else if d.Note == "" {
					label += " " + strconv.Itoa(len(d.Files))
				}
			case tabCommits:
				if _, cs, ok := m.commits(); ok && cs.Note == "" {
					label += " " + strconv.Itoa(len(cs.List)+cs.More)
				} else if _, ok := m.commitThread(); ok {
					label += " …"
				}
			case tabImpact:
				label = m.impactLabel()
			case tabLog:
				if t, ok := r.thread(); ok && hasThread {
					label += " " + strconv.Itoa(len(t.Log))
				}
			}
		}
		text := " " + label + " "
		if tab > 0 {
			b.WriteString(" ")
			x++
		}
		b.WriteString(tabStyle(tab == active, enabled, m.light).Render(text))
		w := ansi.StringWidth(text)
		if enabled {
			zones = append(zones, zone{x0: x, x1: x + w, act: action{kind: actTab, n: int(tab)}})
		}
		x += w
	}
	right := ""
	if hint != "" {
		right = dim.Render(hint)
	}
	return " " + spread(b.String(), right, max(width-2, 1)) + " ", zones
}

// tabEnabled says whether the row has something behind tab: Files needs a
// worktree that is not resolved, Commits and Log a thread (Commits says
// why a resolved one or one without a worktree shows none).
func (m Model) tabEnabled(r row, tab tabKind) bool {
	if r.issue != nil {
		return tab == tabOverview
	}
	if r.pull != nil {
		return tab == tabPR
	}
	switch tab {
	case tabFiles:
		_, ok := m.diffThread()
		return ok && m.opt.Diff != nil
	case tabCommits:
		_, ok := r.thread()
		return r.kind == rowWork && ok && m.opt.Commits != nil
	case tabImpact:
		_, ok := m.diffThread()
		return ok && m.opt.Arch != nil
	case tabPR:
		_, _, ok := r.pr()
		return ok
	case tabLog:
		_, ok := r.thread()
		return r.kind == rowWork && ok
	}
	return true
}

// curTab is the tab the drawer shows: the chosen one, or Overview while
// the row has nothing behind it.
func (m Model) curTab() tabKind {
	r, ok := m.selected()
	if ok && r.kind == rowWork && r.pull != nil {
		return tabPR // a PR row has its PR tab only
	}
	if !ok || r.kind != rowWork || !m.tabEnabled(r, m.tab) {
		return tabOverview
	}
	return m.tab
}

// card is the header card: the title, the status pill and the percent on
// line 1; on line 2, dim, the thread's repo and branch and, when it fits,
// the activity while working (a task without a thread: its list), with the
// progress bar or the PR at the right.
func (m Model) card(r row, w int) []string {
	narrow := w < wideMin
	t, hasThread := r.thread()
	pill, pst := "○ no thread", dim
	switch {
	case hasThread && t.Board != nil:
		pill, pst = boardPill(t.Board)
	case hasThread:
		pill, pst = statusPill(t)
	case r.task != nil && r.task.Done:
		pill = "✓ done"
	}
	right := pst.Render(pill)
	pct, hasPct := percent(t)
	if hasThread && hasPct && t.Status != deck.StatusDone {
		right += "  " + pst.Render(fmt.Sprintf("~%d%%", pct))
	} else {
		hasPct = false
	}
	line1 := spread(" "+inline(r.title(), bold, m.light), right+" ", w)

	var parts []string
	var bar string
	switch {
	case hasThread:
		if t.Repo != "" {
			parts = append(parts, filepath.Base(t.Repo))
		}
		if b := m.shortBranch(t.Branch); b != "" {
			parts = append(parts, b)
		}
		switch {
		case hasPct:
			bar = progressBar(pct, pst)
		case t.PR != nil:
			bar = prBrief(*t.PR, narrow)
		}
	case r.task != nil && m.snap.Project.Slug != boardSlug:
		parts = append(parts, r.list, "TASKS.md")
	}
	if pr, own, ok := r.pr(); ok && !own && bar == "" {
		bar = prBrief(*pr, narrow)
	}
	if r.task != nil && r.task.Owner != "" {
		parts = append(parts, "owner "+r.task.Owner)
	}
	if bar != "" {
		bar += " "
	}
	// The activity goes last, and only whole: the repo and branch matter more.
	if hasThread && t.Board != nil && t.Board.Priority > 0 {
		parts = append([]string{fmt.Sprintf("P%d %s", t.Board.Priority, prioName(t.Board.Priority))}, parts...)
	}
	if hasThread && t.Status == deck.StatusWorking && t.Activity != "" {
		with := append(slices.Clone(parts), t.Activity)
		if 1+ansi.StringWidth(strings.Join(with, " · "))+1+ansi.StringWidth(bar) <= w {
			parts = with
		}
	}
	line2 := spread(" "+dim.Render(strings.Join(parts, " · ")), bar, w)
	return []string{line1, line2}
}

// shortBranch is the branch as the card shows it: without the
// `hp/<project>/` prefix herdr-projects gives every thread's branch, since
// the deck already shows which project it is.
func (m Model) shortBranch(b string) string {
	if slug := m.snap.Project.Slug; slug != "" {
		if rest, ok := strings.CutPrefix(b, "hp/"+slug+"/"); ok && rest != "" {
			return rest
		}
	}
	return b
}

// statusPill is the card's status: the list's glyph and a word, in the
// status colour.
func statusPill(t deck.Thread) (string, lipgloss.Style) {
	switch t.Status {
	case deck.StatusNeedsYou:
		return "● needs you", needsStyle
	case deck.StatusWorking:
		return "◐ working", workStyle
	case deck.StatusReview:
		if t.Group == "landing" {
			return "↻ landing", okStyle
		}
		return "◇ review", reviewStyle
	case deck.StatusDone:
		return "✓ done", dim
	}
	return "○ idle", dim
}

var percentRe = regexp.MustCompile(`~?(\d{1,3})%`)

// percent is the thread's own estimate: its percent field, else the one
// its state line carries ("needs you · ~95%").
func percent(t deck.Thread) (int, bool) {
	if t.Percent != nil {
		return clamp(*t.Percent, 0, 100), true
	}
	if m := percentRe.FindStringSubmatch(t.StateLine); m != nil {
		n, _ := strconv.Atoi(m[1])
		return clamp(n, 0, 100), true
	}
	return 0, false
}

// progressBar is ten cells, ▰ filled in the status colour, ▱ dim.
func progressBar(pct int, st lipgloss.Style) string {
	n := pct / 10
	return st.Render(strings.Repeat("▰", n)) + dim.Render(strings.Repeat("▱", 10-n))
}

// prBrief is the card's PR when there is no percent: `#2320` and what its
// checks say, once herdr-projects knows the PR's state.
func prBrief(pr deck.PullRequest, narrow bool) string {
	s := reviewStyle.Render(fmt.Sprintf("#%d", pr.Number))
	if pr.Number == 0 {
		s = reviewStyle.Render("PR")
	}
	switch n := len(pr.FailingChecks); {
	case pr.State == "":
	case n > 0 && narrow:
		s += " " + failStyle.Render(fmt.Sprintf("✕ %d", n))
	case n > 0:
		s += " " + failStyle.Render(fmt.Sprintf("✕ %d failing", n))
	default:
		s += " " + okStyle.Render("✓")
	}
	return s
}

// projectDrawer shows a list heading, or the project when there is no
// row: a card with its name and no tabs, then the list's note, the goal,
// repos and folder.
func (m Model) projectDrawer(r row, width int) *drawer {
	d := newDrawer(width, "")
	name, what := m.snap.Project.Title(), "project"
	if m.snap.Project.Slug != "" {
		what += " " + m.snap.Project.Slug
	}
	if r.kind == rowHeading && r.list != "" {
		name = r.list
		what = fmt.Sprintf("%d rows", r.count)
		if r.count == 1 {
			what = "1 row"
		}
		if r.list == listNeedsYou {
			what += " · threads waiting on you"
		} else {
			what += " · TASKS.md"
		}
	}
	d.card = []string{fit(" "+bold.Render(name), width), fit(" "+dim.Render(what), width)}
	if r.kind == rowHeading {
		for _, tl := range m.snap.TaskLists {
			if tl.Name == r.list && tl.Note != "" {
				var gs []group
				for _, p := range strings.Split(tl.Note, "\n") {
					gs = append(gs, words(p, plain))
				}
				d.field("Note", false, gs...)
			}
		}
		if r.folded {
			d.field("Folded", false, words(fmt.Sprintf("%d rows · space unfolds", r.count), dim))
		}
	}
	p := m.snap.Project
	if p.Goal != "" {
		d.field("Goal", false, words(p.Goal, plain))
	}
	var repos []group
	for _, repo := range p.Repos {
		repos = append(repos, words(tilde(repo), plain))
	}
	d.field("Repos", false, repos...)
	if p.Dir != "" {
		d.field("Folder", false, words(tilde(p.Dir), plain))
	}
	return d
}

// reportDrawer is the thread's report, rendered as Markdown, under its card, with a line saying
// how to return where the tab bar was.
func (m Model) reportDrawer(r row, width int) *drawer {
	t, _ := r.thread()
	d := newDrawer(width, "")
	d.card = m.card(r, width)
	d.light = m.light
	d.tabs = fit(" "+bold.Render("Report")+dim.Render(" · "+reportName(t)+" · esc returns"), width)
	d.markdown(t.Report)
	return d
}
