package ui

// The Linear view (V): the open Linear issues assigned to the user,
// grouped by their state and sorted by priority, each with the board task
// whose title starts with its ID; and the drawer that shows the selected
// issue (state, priority, labels, project, description, comments). m, or a
// click on an issue's state, opens the state picker (statepick.go), and P
// sets its priority, in Linear.

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// issueFailure is a change Linear refused, for the issue's drawer.
type issueFailure struct{ id, msg string }

// issueSetMsg reports a change made in Linear.
type issueSetMsg struct {
	id, what string
	err      error
}

// buildIssueRows lays out the Linear view: a list per state, under way
// first, then to do, then the backlog, each sorted by priority (none
// last), then by when Linear last saw a change.
func buildIssueRows(snap deck.Snapshot, folds map[string]bool, folded []string) []row {
	if snap.Mine == nil {
		return nil
	}
	tasks := issueTasks(snap.Threads)
	issues := slices.Clone(snap.Mine.Issues)
	// One group per state name, though teams have their own states: a
	// name's group goes where its earliest state of the furthest kind
	// would.
	type rank struct {
		order int
		pos   float64
	}
	ranks := map[string]rank{}
	for _, is := range issues {
		r := rank{deck.StateOrder(is.State.Type), is.State.Position}
		if have, ok := ranks[is.State.Name]; !ok || r.order > have.order || r.order == have.order && r.pos < have.pos {
			ranks[is.State.Name] = r
		}
	}
	slices.SortStableFunc(issues, func(a, b deck.LinearIssue) int {
		ra, rb := ranks[a.State.Name], ranks[b.State.Name]
		return cmp.Or(
			rb.order-ra.order,
			cmp.Compare(ra.pos, rb.pos),
			strings.Compare(a.State.Name, b.State.Name),
			deck.PriorityRank(a.Priority)-deck.PriorityRank(b.Priority),
			b.Updated.Compare(a.Updated),
			strings.Compare(a.ID, b.ID),
		)
	})
	var rows []row
	for i := 0; i < len(issues); {
		name := issues[i].State.Name
		j := i
		for j < len(issues) && issues[j].State.Name == name {
			j++
		}
		isFolded, ok := folds[name]
		if !ok {
			isFolded = startsFolded(name, folded)
		}
		if len(rows) > 0 {
			rows = append(rows, row{kind: rowGap, key: "gap:" + name})
		}
		rows = append(rows, row{kind: rowHeading, list: name, key: "head:" + name, count: j - i, folded: isFolded, foldable: true})
		if !isFolded {
			for k := i; k < j; k++ {
				is := issues[k]
				rows = append(rows, row{kind: rowWork, list: name, key: "issue:" + is.ID, issue: &is, threads: tasks[is.ID]})
			}
		}
		i = j
	}
	return rows
}

// issueTasks are the threads (board tasks) by the Linear ID their title
// starts with, the one the row shows first: one still open before a
// resolved one, then the newest.
func issueTasks(threads []deck.Thread) map[string][]deck.Thread {
	out := map[string][]deck.Thread{}
	for _, t := range threads {
		if id := deck.TitleIssueID(t.Title); id != "" {
			out[id] = append(out[id], t)
		}
	}
	for _, ts := range out {
		slices.SortStableFunc(ts, func(a, b deck.Thread) int {
			return cmp.Or(
				boolRank(threadOpen(b))-boolRank(threadOpen(a)),
				b.Created.Compare(a.Created),
				deck.IssueNumber(b.ID)-deck.IssueNumber(a.ID),
			)
		})
	}
	return out
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// threadOpen says whether a thread is still being worked on: not
// resolved, and on a board not done, merged or failed.
func threadOpen(t deck.Thread) bool {
	if t.Board != nil {
		switch t.Board.Phase {
		case deck.PhaseDone, deck.PhaseMerged, deck.PhaseFailed:
			return false
		}
		return true
	}
	return t.Status != deck.StatusDone
}

// issueLinks are an issue row's links: the issue's own first, then its
// tasks'.
func issueLinks(r row) []deck.Link {
	links := []deck.Link{{Kind: deck.LinkLinear, Label: r.issue.ID, URL: r.issue.URL}}
	for _, t := range r.threads {
		for _, l := range t.Links {
			links = deck.AppendLink(links, l)
		}
	}
	slices.SortStableFunc(links, func(a, b deck.Link) int { return int(a.Kind) - int(b.Kind) })
	return links
}

// issueGlyph is a state's mark: under way half full, in review a
// diamond, to do empty, the backlog dotted.
func issueGlyph(s deck.WorkflowState) (string, lipgloss.Style) {
	st := issueStyle(deck.Issue{State: s.Name, StateType: s.Type})
	switch s.Type {
	case "started":
		if strings.Contains(strings.ToLower(s.Name), "review") {
			return "◇", st
		}
		return "◐", st
	case "unstarted":
		return "○", st
	case "backlog", "triage":
		return "◌", dim
	case "completed":
		return "✓", st
	case "canceled":
		return "✕", st
	}
	return "·", dim
}

// prioBadge is an issue's priority in the list, three columns wide so
// the IDs line up: `P1 ` to `P4 `, blank for none.
func prioBadge(p int) string {
	if p < 1 || p > 4 {
		return "   "
	}
	return prioStyle(p).Render(fmt.Sprintf("P%d", p)) + " "
}

// issueColumnTitles are the Linear view's column titles.
func (m Model) issueColumnTitles(w int) string {
	c := columns(w)
	s := "    " + pad("ISSUE", c.work) + "  "
	if c.wide {
		s += pad("TASK", c.thread) + "  "
		s += pad("TASK STATUS", c.status) + "  "
		s += pad("PR", c.pr) + "  "
	} else {
		s += pad("TASK", c.status) + "  "
	}
	return s + pad("LINKS", c.links) + " " + "DEV"
}

// issueLine is an issue's row: its state's mark, priority, ID and title,
// then its task's ID and status, the PR, links and dev servers.
func (m Model) issueLine(r row, sel bool, w int) string {
	mark, markSt := " ", plain
	if sel {
		mark = "▸"
		if m.dfocus {
			markSt = dim
		}
	}
	is := r.issue
	c := columns(w)
	glyph, gst := issueGlyph(is.State)
	work := prioBadge(is.Priority) + chipStyle(deck.LinkLinear).Render(is.ID) + " " + inline(is.Title, plain, m.light)
	cells := []cell{
		{text: " "},
		{text: mark, style: markSt},
		{text: glyph, style: gst},
		{text: " "},
		{text: work, raw: true, width: c.work},
		{text: "  "},
	}
	t, hasTask := r.thread()
	id, status, sst := "", "no task", dim
	if hasTask {
		id = t.ID
		if t.Board != nil {
			status, sst = phaseGlyph(t.Board.Phase)+" "+boardStatus(t), phaseStyle(t.Board.Phase)
		} else {
			status, sst = statusGlyph(t.Status)+" "+rowStatus(t, false), statusStyle(t.Status)
		}
	}
	if !c.wide && hasTask {
		status = id + " " + status
	}
	// The other tasks' count stays when the status is cut.
	if n := len(r.threads) - 1; n > 0 {
		more := fmt.Sprintf(" +%d", n)
		status = ansi.Truncate(status, max(c.status-len(more), 1), "…") + more
	}
	if c.wide {
		cells = append(cells, cell{text: id, style: dim, width: c.thread}, cell{text: "  "})
	}
	cells = append(cells, cell{text: status, style: sst, width: c.status}, cell{text: "  "})
	if c.wide {
		text, pst := "", reviewStyle
		if pr, own, ok := r.pr(); ok && pr.Number > 0 {
			text = prText(*pr, own)
			if own && len(pr.FailingChecks) > 0 {
				pst = failStyle
			}
		}
		cells = append(cells, cell{text: text, style: pst, width: c.pr}, cell{text: "  "})
	}
	cells = append(cells, badgeCells(issueLinks(r), c.links)...)
	cells = append(cells, cell{text: " "})
	cells = append(cells, devCells(r, c.dev)...)
	return m.finish(renderCells(cells), sel, w)
}

// emptyIssues says why the Linear view has no rows.
func (m Model) emptyIssues() string {
	mine := m.snap.Mine
	switch {
	case mine == nil:
		return "Linear is off (linear.status in the settings). V shows the tasks."
	case mine.Off != "":
		return "Linear: " + mine.Off + ". V shows the tasks."
	case mine.Problem != "" && mine.Read.IsZero():
		return "Linear: " + mine.Problem + ". V shows the tasks."
	case mine.Read.IsZero():
		return "Reading your Linear issues…"
	}
	return "No open Linear issues are assigned to you. V shows the tasks."
}

// issueCard is the drawer's card for an issue: its title and state, then
// its ID, priority, team and project, with the task's PR at the right.
func (m Model) issueCard(r row, w int) []string {
	is := r.issue
	glyph, st := issueGlyph(is.State)
	line1 := spread(" "+inline(is.Title, bold, m.light), st.Render(glyph+" "+is.State.Name)+" ", w)
	parts := []string{is.ID}
	if is.Priority > 0 {
		parts = append(parts, fmt.Sprintf("P%d %s", is.Priority, deck.PriorityName(is.Priority)))
	}
	if is.Team != "" {
		parts = append(parts, is.Team)
	}
	if is.Project != "" {
		parts = append(parts, is.Project)
	}
	bar := ""
	if pr, _, ok := r.pr(); ok {
		bar = prBrief(*pr, w < wideMin) + " "
	}
	return []string{line1, spread(" "+dim.Render(strings.Join(parts, " · ")), bar, w)}
}

// issueOverview is the drawer's content for an issue: its fields, the
// linked tasks and PR, the description, the comments and the links.
func (m Model) issueOverview(d *drawer, r row, links []deck.Link) {
	is := r.issue
	narrow := d.width < wideMin
	pr, _, hasPR := r.pr()
	prLink := -1
	if hasPR {
		for i, l := range links {
			if l.Kind == deck.LinkGitHub && l.URL == pr.URL {
				prLink = i
			}
		}
	}
	heading := func(name string, st lipgloss.Style, note string) {
		if len(d.lines) > 0 {
			d.line("")
		}
		d.sections = append(d.sections, section{name: name, at: len(d.lines)})
		head := " " + dim.Render("── ") + st.Render(name) + dim.Render(" ──")
		if note != "" {
			head += "  " + dim.Render(note)
		}
		d.line(head)
	}

	heading("Issue", bold, "")
	_, sst := issueGlyph(is.State)
	sg := group{sep: " ", items: []item{span(is.State.Name, sst)}}
	if m.opt.SetIssueState != nil {
		// A click on the state opens the picker, as m does.
		sg.items[0].act = action{kind: actState}
		if !narrow {
			sg.items = append(sg.items, span("·", dim), span("m or a click picks another", dim))
		}
	}
	d.field("status", false, sg)
	if m.issueFail.id == is.ID {
		d.styledField("refused", failStyle, false, words(m.issueFail.msg, failStyle))
	}
	pg := group{sep: " ", items: []item{span("none", dim)}}
	if is.Priority > 0 {
		pg.items = []item{span(fmt.Sprintf("P%d %s", is.Priority, deck.PriorityName(is.Priority)), prioStyle(is.Priority))}
	}
	if m.opt.SetIssuePriority != nil && !narrow {
		pg.items = append(pg.items, span("·", dim), span("P then 1–4 sets it, 0 clears", dim))
	}
	d.field("prio", false, pg)
	if len(is.Labels) > 0 {
		g := group{sep: "  "}
		for _, l := range is.Labels {
			st := plain
			if strings.HasPrefix(l.Color, "#") && len(l.Color) == 7 {
				st = lipgloss.NewStyle().Foreground(lipgloss.Color(l.Color))
			}
			g.items = append(g.items, span("● "+l.Name, st))
		}
		d.field("labels", false, g)
	}
	if is.Project != "" {
		d.field("project", false, words(is.Project, plain))
	}
	if is.Team != "" {
		d.field("team", false, words(is.Team, dim))
	}
	if len(r.threads) == 0 {
		d.field("task", false, words("no board task starts with "+is.ID, dim))
	} else {
		var groups []group
		for i, t := range r.threads {
			g := group{sep: " ", items: []item{span(t.ID, bold)}}
			if t.Board != nil {
				g.items = append(g.items, span(phaseGlyph(t.Board.Phase)+" "+boardStatus(t), phaseStyle(t.Board.Phase)))
			} else {
				g.items = append(g.items, span(statusGlyph(t.Status)+" "+rowStatus(t, false), statusStyle(t.Status)))
			}
			if i == 0 && len(r.threads) > 1 && !narrow {
				g.items = append(g.items, span("· shown in the list", dim))
			}
			groups = append(groups, g)
		}
		d.field("task", false, groups...)
	}
	if !is.Updated.IsZero() {
		d.field("updated", false, words(ago(m.opt.Now().Sub(is.Updated)), dim))
	}

	if hasPR {
		heading("PR", bold, "")
		m.prSection(d, *pr, links, prLink, narrow)
	}

	heading("Description", bold, "")
	if is.Description == "" {
		d.flow(words("no description", dim))
	} else {
		d.markdown(is.Description)
	}

	if len(is.Comments) > 0 {
		heading("Comments", bold, fmt.Sprint(len(is.Comments)))
		for i, c := range is.Comments {
			if i > 0 {
				d.line("")
			}
			g := group{sep: " ", items: []item{span(c.Author, bold)}}
			if !c.At.IsZero() {
				g.items = append(g.items, span("· "+ago(m.opt.Now().Sub(c.At)), dim))
			}
			d.flow(g)
			d.markdown(c.Body)
		}
	}

	keep := func(l deck.Link) bool { return l.Kind != deck.LinkLocalhost }
	if g, _ := m.chips(d, links, prLink, keep); len(g.items) > 0 {
		heading("Links", bold, "")
		g, _ := m.chips(d, links, prLink, keep)
		d.flow(g)
	}
}

// moveKeys pick a state in the picker: the digits, then letters, as a
// team may have more than nine states (some have 16). m is not one: it
// closes the picker again.
const moveKeys = "123456789abcdefghijklnopqrstuvwxyz"

// issueStates are the states an issue can move to: its team's.
func (m Model) issueStates(is deck.LinearIssue) []deck.WorkflowState {
	if m.snap.Mine == nil {
		return nil
	}
	return m.snap.Mine.States[is.TeamKey]
}

// selectedIssue is the issue on the selected row.
func (m Model) selectedIssue() (deck.LinearIssue, bool) {
	r, ok := m.selected()
	if !ok || r.issue == nil {
		return deck.LinearIssue{}, false
	}
	return *r.issue, true
}

// armMove opens the state picker on the selected issue, on its state.
func (m *Model) armMove() {
	is, ok := m.selectedIssue()
	switch {
	case !ok:
		m.status = "m moves a Linear issue; V shows them"
	case m.opt.SetIssueState == nil:
		m.status = "moving issues is off"
	case len(m.issueStates(is)) == 0:
		m.status = "Linear gave no states for " + is.TeamKey
	default:
		m.moveArmed = is.ID
		m.moveSel, m.moveOff = 0, 0
		for i, s := range m.issueStates(is) {
			if s.ID == is.State.ID {
				m.moveSel = i
			}
		}
		m.setMode(modeRow)
		m.tab = tabOverview
	}
}

// armIssuePriority starts the P chord on an issue.
func (m *Model) armIssuePriority(is deck.LinearIssue) {
	if m.opt.SetIssuePriority == nil {
		m.status = "setting Linear priorities is off"
		return
	}
	m.issuePrioArmed = is.ID
	m.status = "priority for " + is.ID + ": 1 urgent · 2 high · 3 medium · 4 low · 0 none · esc keeps it"
}

// issueKey handles a key while the state picker is open, or finishes the
// P chord on an issue. It reports whether it took the key.
func (m *Model) issueKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.moveArmed != "" {
		return m.statePickKey(msg), true
	}
	prioID := m.issuePrioArmed
	if prioID == "" {
		return nil, false
	}
	m.issuePrioArmed = ""
	s := msg.String()
	is, ok := m.issueByID(prioID)
	if !ok || len(s) != 1 {
		return nil, true
	}
	n := int(s[0] - '0')
	if n < 0 || n > 4 {
		return nil, true
	}
	set := m.opt.SetIssuePriority
	m.changeIssue(is.ID, func(c *deck.LinearIssue) { c.Priority = n })
	what := "no priority"
	if n > 0 {
		what = fmt.Sprintf("P%d %s", n, deck.PriorityName(n))
	}
	return func() tea.Msg { return issueSetMsg{id: is.ID, what: what, err: set(is, n)} }, true
}

// moveIssue moves the issue to its team's state n, at once in the list
// and then in Linear.
func (m *Model) moveIssue(is deck.LinearIssue, n int) tea.Cmd {
	states := m.issueStates(is)
	if n < 0 || n >= len(states) {
		return nil
	}
	to := states[n]
	if to.ID == is.State.ID {
		m.status = is.ID + " is " + to.Name + " already"
		return nil
	}
	if m.issueFail.id == is.ID {
		m.issueFail = issueFailure{}
	}
	set := m.opt.SetIssueState
	m.changeIssue(is.ID, func(c *deck.LinearIssue) { c.State = to })
	return func() tea.Msg { return issueSetMsg{id: is.ID, what: to.Name, err: set(is, to.ID)} }
}

func (m Model) issueByID(id string) (deck.LinearIssue, bool) {
	if m.snap.Mine == nil {
		return deck.LinearIssue{}, false
	}
	for _, is := range m.snap.Mine.Issues {
		if is.ID == id {
			return is, true
		}
	}
	return deck.LinearIssue{}, false
}

// changeIssue changes an issue in the snapshot at once, before Linear's
// next answer confirms it, and keeps the cursor on it, wherever it moves.
func (m *Model) changeIssue(id string, change func(*deck.LinearIssue)) {
	snap := m.snap
	mine := *snap.Mine
	mine.Issues = slices.Clone(mine.Issues)
	for i := range mine.Issues {
		if mine.Issues[i].ID == id {
			change(&mine.Issues[i])
		}
	}
	snap.Mine = &mine
	m.moved = true
	m.SetSnapshot(snap)
	for i, r := range m.rows {
		if r.issue != nil && r.issue.ID == id {
			m.cursor = i
			m.ensureVisible()
			break
		}
	}
}
