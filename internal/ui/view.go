package ui

import (
	"fmt"
	"math"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// frame is the pane's layout: where the list and drawer sit, so clicks and
// the wheel can find what is under the pointer.
type frame struct {
	listTop, listH     int // the list's first screen line and height
	drawerTop, drawerH int // the drawer's first content line and height
	drawer             *drawer
	// sepY is the rule above the drawer (its title rule in a full view),
	// or -1; headTop the card's first line and headN how many of the
	// card's and tab bar's lines show; tabY the tab bar's line, or -1.
	sepY, headTop, headN, tabY int
	bang                       zone // the header's `! N`, if shown
	// attn is the screen line saying other projects need the user, or 0.
	attn int
	// barred says the drawer's lines overflow it and leave the last
	// column to a scrollbar: a full view's (a report, What's new, …).
	barred bool
}

// drawerBar is the scrollbar of a full view's lines, scrolled to off.
func (f frame) drawerBar(off int) scrollbar {
	if f.drawer == nil {
		return scrollbar{}
	}
	s := scrollbar{total: len(f.drawer.lines), h: f.drawerH}
	s.off = clamp(off, 0, s.most())
	return s
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "herdr-deck · " + m.snap.Project.Title()
	return v
}

// effectiveSize is the drawer's size after the mode's needs: a report takes
// the full height, and a view asked for by key is never hidden.
func (m Model) effectiveSize() drawerSize {
	switch {
	case m.mode == modeReport || m.mode == modeSettings || m.mode == modeCheck:
		return sizeFull
	case m.mode != modeRow && m.size == sizeHidden:
		return sizeNormal
	}
	return m.size
}

// headSize is how many lines the drawer draws above its scrolling
// content: a row's card and tab bar (or the report's line) with a blank
// line on either side of it, a list heading's card, or none for a full
// view under its title rule.
func (m Model) headSize() int {
	r, ok := m.selected()
	switch {
	case (m.mode == modeReport || m.mode == modeCheck) && ok:
		return 5
	case m.mode != modeRow && m.mode != modeReport && m.mode != modeCheck:
		return 0
	case !ok || r.kind == rowHeading:
		return 2
	}
	return 5
}

// Limits on a dragged drawer: the list keeps its column titles and three
// rows, the drawer its rule, a row's card and tab bar (with their blank
// lines) and two lines of content.
const (
	minListRows   = 3
	minDrawerRows = 1 + 5 + 2
)

// drawerBlocks are how many lines the drawer takes at its normal size,
// its rule included, and the fewest and most a drag can give it; ok is
// false when the pane is too small for both limits, and the drawer then
// takes half of it.
func (m Model) drawerBlocks() (block, lo, hi int, ok bool) {
	body := max(m.height, 8) - 4 // header, rule, footer rule, footer
	lo, hi = minDrawerRows, body-1-minListRows
	if hi < lo {
		return max(body/2, 6), 0, 0, false
	}
	return clamp(int(math.Round(m.frac*float64(body))), lo, hi), lo, hi, true
}

func (m Model) drawerBlock() int {
	b, _, _, _ := m.drawerBlocks()
	return b
}

// layout works out the frame for the current size and state. At its normal
// size the drawer takes its share of the pane below the header (m.frac,
// half unless set or dragged): the rule above it, the card, the tab bar and
// the tab's content.
func (m Model) layout() frame {
	h := max(m.height, 8)
	f := frame{listTop: 3, sepY: -1, tabY: -1}
	body := h - 4 // header, rule, footer rule, footer
	head := m.headSize()
	switch m.effectiveSize() {
	case sizeHidden:
		f.listH = body - 1 // column titles
	case sizeFull:
		f.listTop, f.listH = 2, 0
		if head == 0 {
			f.sepY, f.headTop = 2, 3
		} else {
			f.headTop = 2
		}
		// Keep a line of content, as a short pane drops the head's
		// blank lines first.
		f.headN = min(head, max(h-3-f.headTop, 0))
		f.drawerTop = f.headTop + f.headN
		f.drawerH = h - 2 - f.drawerTop
	default:
		block := m.drawerBlock()
		f.listH = max(body-1-block, 1)
		block = body - 1 - f.listH
		f.sepY = f.listTop + f.listH
		f.headTop = f.sepY + 1
		f.headN = min(head, max(block-2, 0))
		f.drawerTop = f.headTop + f.headN
		f.drawerH = block - 1 - f.headN
	}
	if head == 5 && f.headN >= 3 && m.mode == modeRow {
		f.tabY = f.headTop + min(f.headN, 4) - 1 // see drawer.headLines
	}
	// Other projects' needs take the list's last line while it has two.
	if f.listH >= 2 && m.otherNeeds() > 0 {
		f.listH--
		f.attn = f.listTop + f.listH
	}
	if f.drawerH > 0 {
		// A full view that overflows leaves two columns to its
		// scrollbar: a blank one, so right-aligned text keeps clear of
		// it, and the bar. It is laid out that narrow first, as most
		// full views overflow, and again at the full width if it fits.
		// A row's drawer says what is below in its tab bar; the PR and
		// Impact tabs, long like a report, get a scrollbar too, beside
		// their content only: the card and tab bar keep the full width.
		r, ok := m.selected()
		switch {
		case m.width < barMinWidth:
		case m.mode != modeRow:
			f.drawer = m.drawerFor(m.width-2, f.drawerH)
			f.barred = f.drawerBar(0).shown()
		case ok && r.kind == rowWork && (m.curTab() == tabPR || m.curTab() == tabImpact):
			f.drawer = m.rowDrawer(r, rowLinks(r), m.width, m.width-2, f.drawerH)
			f.barred = f.drawerBar(0).shown()
		}
		if !f.barred {
			f.drawer = m.drawerFor(m.width, f.drawerH)
		}
	}
	f.bang = m.bangZone()
	return f
}

func (m Model) drawerFor(width, h int) *drawer {
	r, ok := m.selected()
	switch m.mode {
	case modeSources:
		return m.sourcesDrawer(width)
	case modeHelp:
		return m.helpDrawer(width)
	case modeSettings:
		return m.settingsDrawer(width)
	case modeNews:
		return m.newsDrawer(width)
	case modeReport:
		if ok {
			return m.reportDrawer(r, width)
		}
	case modeCheck:
		if ok {
			return m.checkDrawer(r, width)
		}
	}
	if !ok {
		return m.projectDrawer(row{}, width)
	}
	return m.rowDrawer(r, rowLinks(r), width, width, h)
}

func (m Model) render() string {
	w := m.width
	f := m.layout()
	rule := dim.Render(strings.Repeat("─", w))

	lines := []string{m.header(w), rule}
	if m.pick.open {
		lines = append(lines, m.pickerLines(w, m.pickH())...)
		lines = append(lines, rule, m.footer(w))
		return strings.Join(lines, "\n")
	}
	switch {
	case m.preview:
		lines = append(lines, m.previewLines(w, f.listH)...)
	case f.listH > 0 && m.edgeShown():
		lines = append(lines, m.shownEdgeLines(w, f.listH)...)
	case f.listH > 0 || m.effectiveSize() != sizeFull:
		lines = append(lines, dim.Render(fit(m.columnTitles(w), w)))
		lines = append(lines, m.listLines(w, f.listH)...)
	}
	if f.attn > 0 {
		lines = append(lines, attentionLine(m.otherNeeds(), w))
	}
	if f.drawer != nil {
		switch {
		case m.dragging && f.drawer.title != "":
			lines = append(lines, dragRule(f.drawer.title, w))
		case m.dragging && f.sepY >= 0:
			lines = append(lines, dragRule("", w))
		case f.drawer.title != "":
			lines = append(lines, drawerRule(f.drawer.title, w))
		case f.sepY >= 0:
			lines = append(lines, rule)
		}
		lines = append(lines, f.drawer.headLines(f.headN)...)
		dl := f.drawer.lines
		off := clamp(m.drawerOff, 0, max(len(dl)-f.drawerH, 0))
		body := make([]string, f.drawerH)
		for i := range body {
			if j := off + i; j < len(dl) {
				body[i] = dl[j].text
			}
		}
		if bar := f.drawerBar(off); f.barred && bar.shown() {
			body = withBar(body, bar, w, m.bar == barDrawer, m.light)
		}
		lines = append(lines, body...)
	}
	lines = append(lines, rule, m.footer(w))
	if b, ok := m.statePickBox(); ok {
		lines = b.over(lines, w)
	}
	return strings.Join(lines, "\n")
}

func drawerRule(title string, w int) string {
	t := "─ " + ansi.Truncate(title, max(w-4, 1), "…") + " "
	return bold.Render(t) + dim.Render(strings.Repeat("─", max(w-ansi.StringWidth(t), 0)))
}

// dragRule is the rule above the drawer while it is dragged: blue, with
// the drawer's title when it has one.
func dragRule(title string, w int) string {
	t := ""
	if title != "" {
		t = "━ " + ansi.Truncate(title, max(w-4, 1), "…") + " "
	}
	st := lipgloss.NewStyle().Foreground(colBlue)
	return st.Bold(true).Render(t) + st.Render(strings.Repeat("━", max(w-ansi.StringWidth(t), 0)))
}

// header is the project's name and its counts: needs you, working, review,
// idle, inbox and missing sources.
func (m Model) header(w int) string {
	narrow := w < wideMin
	var n [5]int
	for _, t := range m.snap.Threads {
		n[t.Status]++
	}
	var parts []string
	if c := n[deck.StatusNeedsYou]; c > 0 {
		s := fmt.Sprintf("● %d", c)
		if !narrow {
			s += " needs you"
		}
		parts = append(parts, needsStyle.Render(s))
	}
	for _, st := range []deck.ThreadStatus{deck.StatusWorking, deck.StatusReview, deck.StatusUnknown} {
		if c := n[st]; c > 0 {
			parts = append(parts, statusStyle(st).Render(fmt.Sprintf("%s %d", statusGlyph(st), c)))
		}
	}
	if len(parts) == 0 {
		parts = append(parts, dim.Render("nothing running"))
	}
	if c := len(m.snap.Inbox); c > 0 || len(parts) > 1 || n[deck.StatusNeedsYou]+n[deck.StatusWorking]+n[deck.StatusReview]+n[deck.StatusUnknown] > 0 {
		st := dim
		if c > 0 {
			st = inboxStyle
		}
		parts = append(parts, st.Render(fmt.Sprintf("✉ %d", c)))
	}
	if v := m.update.Available; v != "" {
		parts = append(parts, okStyle.Render("↑ "+v))
	}
	if m.opt.RestartFailed != "" {
		s := "↻"
		if !narrow {
			s += " restart failed"
		}
		parts = append(parts, warnStyle.Render(s))
	}
	// `! N` stays last: bangZone finds it at the right edge.
	if c := len(m.snap.Missing); c > 0 {
		parts = append(parts, warnStyle.Bold(true).Render(fmt.Sprintf("! %d", c)))
	}
	left := " " + bold.Render(m.snap.Project.Title())
	if m.view != viewTasks {
		// The view tabs, after the project's name where it fits.
		tabs, _ := m.viewTabsText()
		left = " " + tabs
		if !narrow {
			left = " " + bold.Render(m.snap.Project.Title()) + "  " + tabs
		}
	}
	return spread(left, strings.Join(parts, "  ")+" ", w)
}

// bangZone is where the header's `! N` sits, for clicks.
func (m Model) bangZone() zone {
	c := len(m.snap.Missing)
	if c == 0 {
		return zone{x0: -1, x1: -1}
	}
	wd := ansi.StringWidth(fmt.Sprintf("! %d", c))
	x1 := m.width - 1
	return zone{x0: x1 - wd, x1: x1}
}

// cols are the list's column widths.
type cols struct {
	wide                                 bool
	work, thread, status, pr, links, dev int
}

func columns(w int) cols {
	if w >= wideMin {
		c := cols{wide: true, thread: 6, status: 18, pr: 6, links: 6, dev: 4}
		c.work = w - 35 - c.status
		return c
	}
	c := cols{status: 15, links: 6, dev: 4}
	c.work = w - 19 - c.status
	if c.work < 12 {
		c.status = max(c.status-(12-c.work), 6)
		c.work = max(w-19-c.status, 6)
	}
	return c
}

func (m Model) columnTitles(w int) string {
	switch m.view {
	case viewLinear:
		return m.issueColumnTitles(w)
	case viewGitHub, viewReview:
		return m.pullColumnTitles(w)
	}
	c := columns(w)
	s := "    " + pad("WORK", c.work) + "  "
	if c.wide {
		s += pad("THREAD", c.thread) + "  "
	}
	s += pad("STATUS", c.status) + "  "
	if c.wide {
		s += pad("PR", c.pr) + "  "
	}
	return s + pad("LINKS", c.links) + " " + "DEV"
}

func (m Model) listLines(w, h int) []string {
	var lines []string
	switch {
	case !m.loaded:
		lines = append(lines, dim.Render("  Reading "+m.snap.Project.Title()+"…"))
	case len(m.rows) == 0:
		text := "Nothing yet. Rows appear when the coordinator writes TASKS.md or starts a thread."
		switch m.view {
		case viewLinear:
			text = m.emptyIssues()
		case viewGitHub, viewReview:
			text = m.emptyPulls()
		}
		msg := ansi.Wrap(text, max(w-4, 10), "")
		lines = append(lines, "")
		for _, l := range strings.Split(msg, "\n") {
			lines = append(lines, dim.Render("  "+l))
		}
	default:
		end := min(m.listOff+h, len(m.rows))
		firstList := true
		for i := m.listOff; i < end; i++ {
			r := m.rows[i]
			asOf := ""
			if r.kind == rowHeading && r.list != listNeedsYou && firstList {
				firstList = false
				if !m.snap.ThreadsAsOf.IsZero() {
					asOf = "as of " + m.snap.ThreadsAsOf.In(m.loc()).Format("15:04")
				}
			}
			lines = append(lines, m.rowLine(r, i == m.cursor, w, asOf))
		}
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	return lines[:h]
}

// cell is one column's text and style.
type cell struct {
	text  string
	style lipgloss.Style
	width int
	raw   bool // text is styled already: style is not applied
}

func (m Model) rowLine(r row, sel bool, w int, asOf string) string {
	if r.kind == rowGap {
		return ""
	}
	if r.kind == rowWork && r.issue != nil {
		return m.issueLine(r, sel, w)
	}
	if r.kind == rowWork && r.pull != nil {
		return m.pullLine(r, sel, w)
	}
	mark := " "
	markSt := plain
	if sel {
		mark = "▸"
		if m.dfocus {
			markSt = dim // the drawer has the focus
		}
	}
	var cells []cell
	switch r.kind {
	case rowHeading:
		text := r.list
		st := dim
		if r.list == listNeedsYou {
			st = needsStyle
		}
		if r.folded {
			text = fmt.Sprintf("+ %s (%d)", r.list, r.count)
		}
		cells = append(cells, cell{text: " "}, cell{text: mark, style: markSt}, cell{text: text, style: st})
		for _, g := range r.glyphs {
			cells = append(cells, cell{text: " " + statusGlyph(g), style: statusStyle(g)})
		}
		if r.folded && r.updates > 0 {
			cells = append(cells, cell{text: " ✉", style: inboxStyle})
		}
		left := renderCells(cells)
		if asOf != "" {
			return m.finish(spread(left, dim.Render(asOf)+" ", w), sel, w)
		}
		return m.finish(left, sel, w)
	}

	c := columns(w)
	t, hasThread := r.thread()
	glyph, gst := "·", dim
	switch {
	case r.task != nil && r.task.Done:
		glyph = "✓"
	case hasThread && t.Board != nil:
		glyph, gst = phaseGlyph(t.Board.Phase), phaseStyle(t.Board.Phase)
	case hasThread:
		glyph, gst = statusGlyph(t.Status), statusStyle(t.Status)
	}
	stale := !m.snap.ThreadsAsOf.IsZero() && hasThread
	workSt := plain
	if r.needsYou() {
		workSt = needsStyle
	} else if r.updates > 0 {
		// Unhandled inbox items about the row's threads: news to read in
		// its Log, not a need.
		workSt = inboxStyle
	} else if (r.task != nil && r.task.Done) || (hasThread && t.Status == deck.StatusDone) || stale {
		workSt = dim
	}
	work := inline(r.title(), workSt, m.light)
	if hasThread && t.Board != nil {
		work = boardMarks(t.Board) + work
	}
	cells = append(cells,
		cell{text: " "},
		cell{text: mark, style: markSt},
		cell{text: glyph, style: gst},
		cell{text: " "},
		cell{text: work, raw: true, width: c.work},
		cell{text: "  "},
	)
	if c.wide {
		id := ""
		if hasThread {
			id = t.ID
		}
		cells = append(cells, cell{text: id, style: dim, width: c.thread}, cell{text: "  "})
	}
	status, sst := "", dim
	if hasThread && t.Board != nil {
		status, sst = boardStatus(t), phaseStyle(t.Board.Phase)
		if !c.wide && t.PR != nil && t.PR.Number > 0 {
			status = fmt.Sprintf("#%d %s", t.PR.Number, status)
		}
	} else if hasThread {
		status, sst = rowStatus(t, !c.wide), statusStyle(t.Status)
		if stale {
			sst = dim
		}
	} else if r.task != nil && r.task.Done {
		status = "done"
	}
	pr, own, hasPR := r.pr()
	hasPR = hasPR && pr.Number > 0
	if hasPR && !own && !c.wide {
		// rowStatus put a thread PR there; a task's goes there too.
		status = strings.TrimSpace(fmt.Sprintf("#%d %s", pr.Number, status))
	}
	cells = append(cells, cell{text: status, style: sst, width: c.status}, cell{text: "  "})
	if c.wide {
		text, pst := "", reviewStyle
		if hasPR {
			text = prText(*pr, own)
			if own && len(pr.FailingChecks) > 0 {
				pst = failStyle
			}
		}
		cells = append(cells, cell{text: text, style: pst, width: c.pr}, cell{text: "  "})
	}
	cells = append(cells, badgeCells(rowLinks(r), c.links)...)
	cells = append(cells, cell{text: " "})
	cells = append(cells, devCells(r, c.dev)...)
	return m.finish(renderCells(cells), sel, w)
}

// prCell is where row r shows its PR number, `#2320`, as columns [x0,
// x1): the PR column at 80 columns, the start of STATUS at 60. ok is false
// when the row shows no PR.
func (m Model) prCell(r row) (x0, x1 int, ok bool) {
	pr, own, ok := r.pr()
	if !ok || pr.Number <= 0 || r.pull != nil {
		return 0, 0, false // a GitHub view's row is all PR
	}
	c := columns(m.width)
	if r.issue != nil && !c.wide {
		return 0, 0, false // the Linear view shows the PR only at 80 columns
	}
	text := fmt.Sprintf("#%d", pr.Number)
	x0 = 4 + c.work + 2 // mark, glyph and space; WORK and its gap
	width := c.status
	if c.wide {
		x0 += c.thread + 2 + c.status + 2
		width = c.pr
		text = prText(*pr, own)
	}
	return x0, x0 + min(ansi.StringWidth(text), width), true
}

// prText is the PR column's text: `#2320`, with ✕ when a thread PR's
// checks fail. A task's PR has no mark: the status ticker, which keeps
// the list's marks current, follows thread PRs only.
func prText(pr deck.PullRequest, own bool) string {
	s := fmt.Sprintf("#%d", pr.Number)
	if own && len(pr.FailingChecks) > 0 {
		s += "✕"
	}
	return s
}

// rowStatus is the STATUS column. At 60 columns it also carries the PR.
func rowStatus(t deck.Thread, narrow bool) string {
	var s string
	switch t.Status {
	case deck.StatusNeedsYou:
		s = "needs you"
	case deck.StatusWorking:
		s = t.Activity
		if s == "" {
			s = t.StateLine
		}
	case deck.StatusReview:
		if t.PR != nil {
			s = reviewText(*t.PR)
		}
		if s == "" {
			s = t.StateLine
		}
	case deck.StatusDone:
		s = "resolved"
	default:
		s = t.StateLine
	}
	if s == "" {
		s = statusWord(t.Status)
	}
	if narrow && t.PR != nil && t.PR.Number > 0 {
		short := s
		if t.Status == deck.StatusReview {
			short = shortReview(*t.PR)
		}
		s = fmt.Sprintf("#%d %s", t.PR.Number, short)
	}
	return s
}

// reviewText is a PR's state in words: "review required", "approved", ….
func reviewText(pr deck.PullRequest) string {
	switch pr.State {
	case "MERGED":
		return "merged"
	case "CLOSED":
		return "closed"
	}
	if pr.Review != "" {
		return strings.ReplaceAll(strings.ToLower(pr.Review), "_", " ")
	}
	return strings.ToLower(pr.State)
}

func shortReview(pr deck.PullRequest) string {
	switch {
	case pr.State == "MERGED":
		return "merged"
	case pr.Review == "APPROVED":
		return "approved"
	case pr.Review == "CHANGES_REQUESTED":
		return "changes"
	}
	return "review"
}

// badgeCells is the LINKS column: a letter per link kind, with a count when
// there are several (`L4 F3`). The PR has its own column, and localhost
// links show as DEV dots.
func badgeCells(links []deck.Link, width int) []cell {
	var n [5]int
	for _, l := range links {
		if l.Kind >= 0 && int(l.Kind) < len(n) {
			n[l.Kind]++
		}
	}
	var cells []cell
	used := 0
	for _, k := range []deck.LinkKind{deck.LinkLinear, deck.LinkFigma, deck.LinkNotion} {
		if n[k] == 0 {
			continue
		}
		text := k.Badge()
		if n[k] > 1 {
			text += fmt.Sprint(n[k])
		}
		if used > 0 {
			text = " " + text
		}
		if used+ansi.StringWidth(text) > width {
			break
		}
		cells = append(cells, cell{text: text, style: linkStyle(k)})
		used += ansi.StringWidth(text)
	}
	cells = append(cells, cell{text: strings.Repeat(" ", width-used)})
	return cells
}

// devCells is the DEV column: one dot per dev server, green when it is
// ready, yellow while it starts; a fallback port (herdr's port token) is
// marked ~. A server without a port that nothing started has none.
func devCells(r row, width int) []cell {
	var cells []cell
	used := 0
	for _, t := range r.threads {
		for _, s := range t.DevServers {
			if s.Fallback && used+2 <= width {
				cells = append(cells, cell{text: "~", style: dim})
				used++
			}
			if used >= width {
				break
			}
			switch {
			case s.Running:
				cells = append(cells, cell{text: "●", style: okStyle})
			case s.Starting:
				cells = append(cells, cell{text: "◐", style: warnStyle})
			case s.Port == 0 && s.Pending == "" && !s.Exited:
				continue // no port and nothing started it: no state to show
			default:
				cells = append(cells, cell{text: "○", style: dim})
			}
			used++
		}
	}
	return cells
}

func renderCells(cells []cell) string {
	var b strings.Builder
	for _, c := range cells {
		text := c.text
		if c.width > 0 {
			text = pad(text, c.width)
		}
		if !c.raw {
			text = c.style.Render(text)
		}
		b.WriteString(text)
	}
	return b.String()
}

// finish fits a row to the width; a selected row is highlighted across the
// whole width.
func (m Model) finish(s string, sel bool, w int) string {
	s = fit(s, w)
	if sel {
		return highlight(s, m.light)
	}
	return s
}

func (m Model) footer(w int) string {
	if m.status != "" {
		st := warnStyle
		if m.status == m.failed {
			st = failStyle.Bold(true)
		}
		return st.Render(fit(" "+m.status, w))
	}
	if m.notice != "" {
		return footerLine(okStyle.Render(" "+m.notice), m.opt.Version, w)
	}
	narrow := w < wideMin
	var hint string
	r, ok := m.selected()
	switch {
	case m.pick.open:
		hint = m.pickerFooter()
	case m.moveArmed != "":
		hint = "↑↓ ↵ pick  1-9 a-z the state's key  click one  esc keeps it"
		if narrow {
			hint = "↑↓ ↵ pick  esc keeps it"
		}
	case m.choosing != noKind:
		var nums []int
		for i, l := range m.links() {
			if l.Kind == m.choosing && i < 9 {
				nums = append(nums, i+1)
			}
		}
		span := ""
		if len(nums) > 0 {
			span = fmt.Sprint(nums[0])
			if len(nums) > 1 {
				span += fmt.Sprintf("-%d", nums[len(nums)-1])
			}
		}
		if m.desktop {
			hint = fmt.Sprintf("Figma desktop: %s open  f first  esc cancel", span)
		} else {
			hint = fmt.Sprintf("%s: %s open", m.choosing, span)
			if m.choosing == deck.LinkFigma {
				hint += "  d desktop"
			}
			hint += "  a all  " + kindKey(m.choosing) + " first  esc cancel"
		}
	case m.mode == modeSettings && m.set.editing:
		hint = "↵ save  esc cancel  empty removes it"
	case m.mode == modeSettings:
		hint = "↵ edit  x remove  j k move  esc back  q quit"
	case m.mode == modeReport:
		hint = "esc back  pgup pgdn scroll  ? help  q quit"
	case m.mode == modeCheck:
		hint = "esc back  pgup pgdn scroll  ↵ on GitHub  c next  q quit"
	case m.mode != modeRow:
		hint = "esc back  pgup pgdn scroll  z drawer  ? help  q quit"
	case !ok:
		hint = "? help  q quit"
	case r.kind == rowHeading:
		hint = "space fold  j k move  z drawer  ? help"
	default:
		hint = m.rowHint(r, narrow)
	}
	return footerLine(" "+hint, m.opt.Version, w)
}

// footerLine right-aligns the dim version after the key help. The help
// takes priority: the version is dropped when both do not fit with a
// two-column gap.
func footerLine(help, version string, w int) string {
	if version == "" || ansi.StringWidth(help)+2+ansi.StringWidth(version) > w {
		return fit(help, w)
	}
	return spread(help, dim.Render(version), w)
}

// spread puts left and right on one line of width w, right-aligning right and
// truncating left when they do not fit.
func spread(left, right string, w int) string {
	rw := ansi.StringWidth(right)
	room := w - rw - 1
	if right == "" {
		room = w
	}
	if room < 1 {
		return fit(left, w)
	}
	left = ansi.Truncate(left, room, "…")
	gap := w - ansi.StringWidth(left) - rw
	return left + strings.Repeat(" ", max(gap, 0)) + right
}

// fit truncates or pads s to exactly w columns.
func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-ansi.StringWidth(s), 0))
}

// pad truncates or pads text, plain or styled, to n columns.
func pad(s string, n int) string {
	if n <= 0 {
		return ""
	}
	return fit(s, n)
}

// rowHint is the footer's key help for a work row, by the drawer's tab and
// focus.
func (m Model) rowHint(r row, narrow bool) string {
	t, hasThread := r.thread()
	if r.pull != nil && !m.dfocus {
		if narrow {
			return "↵ g open  ←→ tabs  V tasks"
		}
		return "↵ g open the PR  tab drawer  ←→ tabs  V tasks  ? help"
	}
	if r.issue != nil && !m.dfocus {
		pane := ""
		if hasThread {
			pane = "  ↵ task pane"
		}
		if narrow {
			if hasThread {
				pane = "  ↵ pane"
			}
			return "m move  P prio  1-9 link" + pane + "  ←→ tabs  V tasks"
		}
		return "m move  P prio  1-9 link  l g first" + pane + "  ←→ tabs  V tasks  ? help"
	}
	tab := m.curTab()
	layout := "S split"
	if m.split {
		layout = "S unified"
	}
	if m.dfocus {
		switch tab {
		case tabFiles:
			view := "t tree"
			if m.tree {
				view = "t list"
			}
			switch {
			case m.preview && narrow:
				return "j k file  J K scroll  v off  d tool  esc list"
			case m.preview:
				return "j k file  J K scroll  " + layout + "  v off  d diff tool  a all  esc list"
			case narrow:
				return "j k file  ↵ preview  d tool  a all  " + view + "  esc list"
			}
			return "j k file  ↵ preview  d diff tool  a whole diff  " + view + "  esc list  ? help"
		case tabCommits:
			gh := ""
			if hasThread && t.PR != nil {
				gh = "  g GitHub"
			}
			switch {
			case m.preview && narrow:
				return "j k move  J K scroll  v off  esc list"
			case m.preview && gh == "":
				return "j k move  J K scroll  " + layout + "  v off  space files  d diff tool  esc list"
			case m.preview:
				return "j k move  J K scroll  " + layout + "  v off  d diff tool" + gh + "  esc list"
			case narrow:
				return "j k move  ↵ preview  space files  d tool  esc list"
			}
			return "j k move  ↵ preview  space files  d diff tool" + gh + "  esc list  ? help"
		case tabPR:
			if narrow {
				return "j k move  ↵ GitHub  space unfold  esc list"
			}
			return "j k move  ↵ on GitHub  space unfold  c check log  esc list  ? help"
		case tabImpact:
			if narrow {
				return "hjkl move  ↵ open  O pane  esc list"
			}
			return "j k h l move  ↵ open  O own pane  z full height  esc list  ? help"
		case tabLog:
			return "j k event  ↵ act  tab next tab  esc list  ? help"
		}
		return "j k link  ↵ open  tab next tab  esc list  ? help"
	}
	switch {
	case tab == tabFiles:
		view := "t tree"
		if m.tree {
			view = "t list"
		}
		if narrow {
			return "1-9 preview  a all  " + view + "  tab focus  [ ] tab"
		}
		return "1-9 preview file  a whole diff  " + view + "  tab focus  [ ] tab  ? help"
	case tab == tabCommits && narrow:
		return "1-9 commit  v preview  tab focus  [ ] tab"
	case tab == tabCommits:
		return "1-9 preview commit  v preview  tab focus  [ ] tab  ? help"
	case tab == tabImpact && narrow:
		return "tab select  O pane  z full  [ ] tab"
	case tab == tabImpact:
		return "tab select boxes  O own pane  z full height  [ ] tab  ? help"
	case tab == tabPR && narrow:
		return "g GitHub  c log  tab focus  [ ] tab  z drawer"
	case tab == tabPR:
		return "g on GitHub  c check log  pgdn scroll  tab focus  [ ] tab  ? help"
	case tab == tabLog && hasThread && t.PR != nil:
		return "1-9 link  g PR  r report  [ ] tab  z drawer  ? help"
	case tab == tabLog:
		return "r report  ↵ go to pane  [ ] tab  z drawer  ? help"
	case !hasThread:
		return "1-9 link  l f n g first  [ ] tab  z drawer  ? help"
	case r.needsYou() && narrow:
		return "↵ go to pane  1-9 link  [ ] tab  z drawer  ? help"
	case r.needsYou():
		return "↵ go to pane  1-9 link  [ ] tab  r report  z drawer  ? help"
	case narrow:
		return "1-9 link  o open  ↵ pane  [ ] tab  z drawer  ? help"
	}
	return "1-9 link  l f n g o first  ↵ pane  [ ] tab  z drawer  ? help"
}
