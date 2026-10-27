package ui

// The deck's view of a herdr-projects 1.x board task (deck.BoardTask): its
// phase in colour, its priority and critical marks, the Board section of
// the Overview, and the keys that act on it.

import (
	"fmt"
	"slices"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// The phases' colours follow herdr-projects' own (docs/colours.md):
// working amber, needs you red, ready to approve green, under review blue,
// done teal, queued pink, to do grey, the board's own check light grey. A merged task is the purple GitHub
// gives merged PRs, and a plan waiting for a yes is green like other work
// that waits for the user's approval.
var (
	phaseTodo    = dim
	phaseQueued  = lipgloss.NewStyle().Foreground(colMagenta).Faint(true)
	phaseWorking = lipgloss.NewStyle().Foreground(colYellow)
	phasePlan    = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	phaseAsks    = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	phaseReview  = lipgloss.NewStyle().Foreground(colBlue)
	// The board's own check is light grey there: plain text here, brighter
	// than to do's dim grey.
	phaseChecking = plain
	phaseApprove  = lipgloss.NewStyle().Foreground(colGreen).Bold(true)
	phaseMerging  = lipgloss.NewStyle().Foreground(colCyan)
	phaseFailed   = lipgloss.NewStyle().Foreground(colRed)
	phaseDone     = lipgloss.NewStyle().Foreground(colCyan)
	phaseMerged   = lipgloss.NewStyle().Foreground(colMagenta)
	critStyle     = lipgloss.NewStyle().Foreground(colRed).Bold(true)
	feStyle       = lipgloss.NewStyle().Foreground(colCyan)
)

func phaseStyle(p deck.Phase) lipgloss.Style {
	switch p {
	case deck.PhaseQueued:
		return phaseQueued
	case deck.PhaseWorking:
		return phaseWorking
	case deck.PhasePlan:
		return phasePlan
	case deck.PhaseAsks:
		return phaseAsks
	case deck.PhaseReview:
		return phaseReview
	case deck.PhaseChecking:
		return phaseChecking
	case deck.PhaseApprove:
		return phaseApprove
	case deck.PhaseMerging:
		return phaseMerging
	case deck.PhaseFailed:
		return phaseFailed
	case deck.PhaseDone:
		return phaseDone
	case deck.PhaseMerged:
		return phaseMerged
	}
	return phaseTodo
}

func phaseGlyph(p deck.Phase) string {
	switch p {
	case deck.PhaseQueued:
		return "◌"
	case deck.PhaseWorking:
		return "◐"
	case deck.PhasePlan:
		return "◆"
	case deck.PhaseAsks:
		return "●"
	case deck.PhaseReview:
		return "◇"
	case deck.PhaseChecking:
		return "◑"
	case deck.PhaseApprove:
		return "★"
	case deck.PhaseMerging:
		return "↻"
	case deck.PhaseFailed:
		return "✕"
	case deck.PhaseDone, deck.PhaseMerged:
		return "✓"
	}
	return "○"
}

// prioStyle colours a priority badge: Urgent red, High amber, Medium blue,
// Low grey.
func prioStyle(p int) lipgloss.Style {
	switch p {
	case 1:
		return lipgloss.NewStyle().Foreground(colRed).Bold(true)
	case 2:
		return lipgloss.NewStyle().Foreground(colYellow).Bold(true)
	case 3:
		return lipgloss.NewStyle().Foreground(colBlue)
	}
	return dim
}

// prioNames are the priorities as Linear names them; 1 is the most urgent.
var prioNames = []string{"none", "Urgent", "High", "Medium", "Low"}

func prioName(p int) string {
	if p < 0 || p >= len(prioNames) {
		return prioNames[0]
	}
	return prioNames[p]
}

// boardMarks is what goes before a board task's title in the WORK column:
// its priority (P1–P4) and ⚠ for a critical change, each with a space;
// "" for a task with neither. Rows of one list share a priority, so a
// list's rows line up.
func boardMarks(b *deck.BoardTask) string {
	var s string
	if b.Priority > 0 {
		s += prioStyle(b.Priority).Render(fmt.Sprintf("P%d", b.Priority)) + " "
	}
	if len(b.Critical) > 0 {
		s += critStyle.Render("⚠") + " "
	}
	return s
}

// boardStatus is the STATUS column of a board task: what the agent does
// now while it works, else the phase, with the review's tally once the
// work waits for approval.
func boardStatus(t deck.Thread) string {
	b := t.Board
	switch b.Phase {
	case deck.PhaseWorking:
		s := b.Phase.String()
		if b.Progress != nil {
			s = fmt.Sprintf("%d%%", clamp(*b.Progress, 0, 100))
		}
		if b.Msg != "" {
			s += " " + b.Msg
		}
		return s
	case deck.PhaseQueued:
		if b.Queue > 0 {
			return fmt.Sprintf("queued #%d", b.Queue)
		}
	case deck.PhaseApprove, deck.PhaseDone, deck.PhaseMerged, deck.PhaseFailed:
		word := b.Phase.String()
		if b.Phase == deck.PhaseApprove {
			word = "approve"
		}
		if b.Review != nil {
			if tally := shortTally(*b.Review); tally != "" {
				return word + " · " + tally
			}
		}
		return word
	}
	return b.Phase.String()
}

// boardPill is the card's status for a board task.
func boardPill(b *deck.BoardTask) (string, lipgloss.Style) {
	return phaseGlyph(b.Phase) + " " + b.Phase.String(), phaseStyle(b.Phase)
}

// boardSection is the Overview's Board section: where the task stands on
// the board and what it used.
func (m Model) boardSection(d *drawer, t deck.Thread) {
	b := t.Board
	narrow := d.width < wideMin

	st := phaseStyle(b.Phase)
	g := group{sep: " ", items: []item{span(phaseGlyph(b.Phase)+" "+b.Phase.String(), st)}}
	if b.Research {
		g.items = append(g.items, span("·", dim), span("research only", dim))
	}
	if b.Chat {
		g.items = append(g.items, span("·", dim), span("chat only", dim))
	}
	if b.Progress != nil && b.Phase == deck.PhaseWorking {
		g.items = append(g.items, span(progressBar(clamp(*b.Progress, 0, 100), st), plain))
		g.items[len(g.items)-1].raw = true
		g.items = append(g.items, span(fmt.Sprintf("%d%%", clamp(*b.Progress, 0, 100)), st))
		if b.ETA != nil && *b.ETA > 0 {
			g.items = append(g.items, span(fmt.Sprintf("~%d min left", *b.ETA), dim))
		}
	}
	d.field("stage", false, g)
	if b.Msg != "" {
		d.field("says", false, inlineWords(b.Msg, plain, d.light))
	}

	pg := group{sep: " ", items: []item{span(fmt.Sprintf("P%d %s", b.Priority, prioName(b.Priority)), prioStyle(b.Priority))}}
	if b.Priority == 0 {
		pg.items = []item{span("none", dim)}
	}
	if m.opt.SetPriority != nil {
		pg.items = append(pg.items, span("·", dim), span("P then 1–4 sets it, 0 clears", dim))
	}
	d.field("prio", false, pg)

	if len(b.Critical) > 0 {
		var groups []group
		for _, c := range b.Critical {
			groups = append(groups, group{sep: " ", items: []item{span("⚠", critStyle), span(c, critStyle)}})
		}
		d.field("risk", false, groups...)
	} else if b.Phase != deck.PhaseTodo && t.Worktree != "" {
		d.field("risk", false, words("no critical area touched", dim))
	}
	if b.Frontend != "" {
		hint := "u starts it"
		switch {
		case strings.HasPrefix(t.DevNote, "no ") && strings.Contains(t.DevNote, "dev.json"):
			// u runs the dev manifest; without one there is nothing to run.
			hint = t.DevNote + ": u cannot start it"
		case len(t.DevServers) > 0 && slices.ContainsFunc(t.DevServers, func(s deck.DevServer) bool { return s.Running }):
			hint = "running · o opens it"
		case narrow:
			hint = "u"
		}
		d.field("ui", false, group{sep: " ", items: []item{span(b.Frontend+"/", feStyle), span("changed", feStyle), span("·", dim), span(hint, dim)}})
	}

	if rv := b.Review; rv != nil {
		verdict, vst := "Review failed", failStyle
		if rv.Pass {
			verdict, vst = "Pass", okStyle
		}
		rg := group{sep: " ", items: []item{span(verdict, vst)}}
		if tally := rv.Tally(); tally != "" {
			rg.items = append(rg.items, span("·", dim), span(tally, plain))
		}
		if rv.Gate != "" {
			rg.items = append(rg.items, span("·", dim), span("gate "+rv.Gate, dim))
		}
		if rv.Round > 1 {
			rg.items = append(rg.items, span("·", dim), span(fmt.Sprintf("round %d", rv.Round), dim))
		}
		groups := []group{rg}
		for _, f := range rv.Findings {
			fg := group{sep: " ", hang: 2, items: []item{span(f.ID, dim), span(f.Severity, severityStyle(f.Severity))}}
			fg.items = append(fg.items, inlineWords(firstSentence(f.Subject), plain, d.light).items...)
			groups = append(groups, fg)
		}
		d.field("review", false, groups...)
	}

	var used []string
	if b.Elapsed > 0 {
		used = append(used, durationText(b.Elapsed))
	}
	if b.Tokens > 0 {
		used = append(used, tokensText(b.Tokens)+" tokens")
	}
	if b.Context > 0 {
		used = append(used, tokensText(b.Context)+" context")
	}
	if len(used) > 0 {
		d.field("used", false, words(strings.Join(used, " · "), plain))
	}
	agent := strings.TrimSpace(b.App + " " + b.Model)
	if agent != "" {
		d.field("agent", false, words(agent, dim))
	}
	if b.ReportURL != "" || b.ReportFile != "" || b.ReviewURL != "" {
		var items []item
		if b.ReportURL != "" || b.ReportFile != "" {
			items = append(items, span("report", plain))
		}
		if b.ReviewURL != "" {
			items = append(items, span("review page", plain))
		}
		hint := "R opens it"
		if len(items) > 1 {
			hint = "R opens the report"
		}
		items = append(items, span("·", dim), span(hint, dim))
		d.field("pages", false, group{sep: " ", items: items})
	}
	d.field("board", false, words(b.ID+" · "+b.Repo, dim))
}

func severityStyle(s string) lipgloss.Style {
	switch s {
	case "critical":
		return critStyle
	case "major":
		return failStyle
	case "minor":
		return warnStyle
	}
	return dim
}

// firstSentence cuts a finding's subject at its first sentence, at most
// 120 characters: the drawer lists findings, the review page explains them.
func firstSentence(s string) string {
	if i := strings.Index(s, ". "); i > 0 {
		s = s[:i+1]
	}
	return ansi.Truncate(s, 120, "…")
}

func durationText(d time.Duration) string {
	switch {
	case d >= time.Hour:
		return fmt.Sprintf("%dh %02dm", int(d.Hours()), int(d.Minutes())%60)
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%ds", int(d.Seconds()))
}

func tokensText(n int) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1000:
		return fmt.Sprintf("%dk", n/1000)
	}
	return fmt.Sprintf("%d", n)
}

// prioritySetMsg says how setting a priority went.
type prioritySetMsg struct {
	id   string
	prio int
	err  error
}

// armPriority starts the P chord on a board task: the next key, 1–4 or 0,
// gives it its priority.
func (m *Model) armPriority() {
	r, _ := m.selected()
	t, ok := r.thread()
	switch {
	case !ok || t.Board == nil:
		m.status = "priorities are for board tasks"
	case m.opt.SetPriority == nil:
		m.status = "setting priorities is off"
	default:
		m.prioArmed = t.ID
		m.status = "priority for " + t.ID + ": 1 urgent · 2 high · 3 medium · 4 low · 0 none · esc keeps it"
	}
}

// priorityKey finishes the P chord. It reports whether it took the key:
// any key ends the chord, and only 0–4 set the priority.
func (m *Model) priorityKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	id := m.prioArmed
	if id == "" {
		return nil, false
	}
	m.prioArmed = ""
	s := msg.String()
	if len(s) != 1 || s[0] < '0' || s[0] > '4' {
		return nil, true
	}
	prio := int(s[0] - '0')
	var t deck.Thread
	for _, th := range m.snap.Threads {
		if th.ID == id {
			t = th
		}
	}
	if t.Board == nil {
		return nil, true
	}
	set := m.opt.SetPriority
	// Show it at once; the reload that follows confirms it.
	m.setPriority(id, prio)
	return func() tea.Msg { return prioritySetMsg{id: id, prio: prio, err: set(t, prio)} }, true
}

// setPriority changes a task's priority in the snapshot and lays out the
// list again, keeping the cursor on the task.
func (m *Model) setPriority(id string, prio int) {
	snap := m.snap
	snap.Threads = append([]deck.Thread(nil), snap.Threads...)
	for i, th := range snap.Threads {
		if th.ID == id && th.Board != nil {
			b := *th.Board
			b.Priority = prio
			snap.Threads[i].Board = &b
		}
	}
	if m.opt.Relist != nil {
		snap.TaskLists = m.opt.Relist(snap.Threads)
	}
	m.SetSnapshot(snap)
	for i, r := range m.rows {
		if t, ok := r.thread(); ok && t.ID == id && r.kind == rowWork {
			m.cursor = i
			m.ensureVisible()
			break
		}
	}
}

// openBoardPage opens the selected board task's report page, else its
// review page, in the browser.
func (m *Model) openBoardPage() tea.Cmd {
	r, _ := m.selected()
	t, ok := r.thread()
	if !ok || t.Board == nil {
		m.status = "pages are for board tasks"
		return nil
	}
	b := t.Board
	// A page on disk opens without the board's page server.
	url := b.ReportURL
	if strings.HasSuffix(b.ReportFile, ".html") || url == "" && b.ReportFile != "" {
		url = "file://" + b.ReportFile
	}
	what := "the report of " + t.ID
	if url == "" {
		url, what = b.ReviewURL, "the review of "+t.ID
	}
	if url == "" {
		m.status = t.ID + " has no report or review page yet"
		return nil
	}
	return m.openOrCopy(url, what)
}

// boardSlug is the project slug of a deck on a herdr-projects 1.x board
// (board.Slug, which the UI does not import).
const boardSlug = "board"

// reportName is where the report r shows comes from: a 0.2 thread's
// threads/<id>.md, a board task's report file or else its closing note.
func reportName(t deck.Thread) string {
	switch {
	case t.Board == nil:
		return "threads/" + t.ID + ".md"
	case strings.HasSuffix(t.Board.ReportFile, ".md"):
		return tilde(t.Board.ReportFile)
	}
	return "closing note"
}

// shortTally is a review's findings for the STATUS column: the worst
// severity's count, "1 major"; "" with none. The drawer lists them all.
func shortTally(r deck.BoardReview) string {
	for _, sev := range []string{"critical", "major", "minor", "trivial"} {
		n := 0
		for _, f := range r.Findings {
			if f.Severity == sev {
				n++
			}
		}
		if n == 0 {
			continue
		}
		return fmt.Sprintf("%d %s", n, sev)
	}
	return ""
}
