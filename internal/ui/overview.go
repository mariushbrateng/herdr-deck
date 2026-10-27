package ui

import (
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FredricW/herdr-deck/internal/deck"
	"github.com/FredricW/herdr-deck/internal/markdown"
)

// noWorkspace says why a bare Linear ID has no link.
const noWorkspace = "no Linear workspace: set linear.workspace in the config file, --linear-workspace or $" + deck.EnvLinearWorkspace

// overview is the Overview tab: the localhost links as chips on top (or,
// for a thread without them, one dim line saying why), then titled
// sections in a fixed order, Next, PR, Note, Links and Thread, the empty
// ones left out. A blank line goes between them only when they all fit in
// h lines.
func (m Model) overview(d *drawer, r row, links []deck.Link, h int) {
	t, hasThread := r.thread()
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

	type sec struct {
		name  string
		style lipgloss.Style
		body  func()
		note  string // dim after the heading
	}
	var secs []sec
	if hasThread && len(t.Next) > 0 {
		st := bold
		if t.Status == deck.StatusNeedsYou {
			st = needsStyle
		}
		secs = append(secs, sec{name: "Next", style: st, body: func() {
			for _, n := range t.Next {
				g := inlineWords(n, plain, d.light)
				g.items = append([]item{span("→", bold)}, g.items...)
				g.hang = 2
				d.flow(g)
			}
		}})
	}
	if hasPR {
		secs = append(secs, sec{name: "PR", style: bold, body: func() { m.prSection(d, *pr, links, prLink, narrow) }})
	}
	if r.task != nil && r.task.Notes != "" {
		secs = append(secs, sec{name: "Note", style: bold, body: func() {
			// Each line is a note of its own, so each renders on its own:
			// as one document, Markdown would join them into a paragraph.
			// A line of inline Markdown only renders like the task's
			// title (link text without its URL, literal 2*3*4); a list
			// item or other block goes through glamour.
			for _, n := range strings.Split(r.task.Notes, "\n") {
				if markdown.IsInline(n) {
					d.flow(inlineWords(n, plain, d.light))
				} else {
					d.markdown(n)
				}
			}
		}})
	}
	// Linear issues with a title get a line each; the other links share
	// a wrapping line of chips.
	chipped := func(l deck.Link) bool { return l.Kind != deck.LinkLocalhost && !titled(l) }
	if g, unlinked := m.chips(d, links, prLink, chipped); len(g.items) > 0 || hasTitled(links, prLink) {
		secs = append(secs, sec{name: "Links", style: bold, body: func() {
			for i, l := range links {
				if i != prLink && titled(l) {
					d.flow(group{items: []item{m.issueChip(d, l, i)}})
				}
			}
			g, _ := m.chips(d, links, prLink, chipped)
			if len(g.items) > 0 {
				d.flow(g)
			}
			// With a key, Linear gives the URLs once it answers.
			if unlinked && !m.snap.LinearKey {
				d.flow(words(noWorkspace, dim))
			}
		}})
	}
	if hasThread && t.Board != nil {
		// On a board the task's own state comes first.
		secs = append([]sec{{name: "Board", style: phaseStyle(t.Board.Phase).Bold(true), body: func() { m.boardSection(d, t) }}}, secs...)
	}
	if hasThread {
		name := "Thread"
		if t.Board != nil {
			name = "Worktree"
		}
		secs = append(secs, sec{name: name, style: bold, body: func() { m.threadSection(d, r, t) }})
	}

	local := func(l deck.Link) bool { return l.Kind == deck.LinkLocalhost }
	hasDev := slices.ContainsFunc(links, local)
	why := ""
	if hasThread && !hasDev {
		why = devReason(t)
	}
	blocks := len(secs)
	if hasDev || why != "" {
		blocks++
	}

	build := func(blanks bool) {
		d.lines, d.stops, d.sections, d.given = nil, nil, nil, 0
		if n := r.updates; n > 0 {
			noun := "updates"
			if n == 1 {
				noun = "update"
			}
			text := fmt.Sprintf("✉ %d %s · see Log", n, noun)
			d.lines = append(d.lines, dline{
				text:  fit(" "+inboxStyle.Render(text), d.width),
				zones: []zone{{x0: 1, x1: 1 + ansi.StringWidth(text), act: action{kind: actTab, n: int(tabLog)}}},
			})
		}
		full := m.effectiveSize() == sizeFull
		if hasDev {
			if blanks && full {
				d.line("")
			}
			g, _ := m.chips(d, links, prLink, local)
			d.flow(g)
		} else if why != "" {
			if blanks && full {
				d.line("")
			}
			d.flow(words(why, dim))
		}
		for i, s := range secs {
			if blanks && (i > 0 || full || hasDev || why != "") {
				d.line("")
			}
			d.sections = append(d.sections, section{name: s.name, at: len(d.lines)})
			head := " " + dim.Render("── ") + s.style.Render(s.name) + dim.Render(" ──")
			if s.note != "" {
				head += "  " + dim.Render(s.note)
			}
			d.line(head)
			s.body()
		}
		if blanks && full && blocks > 0 {
			d.line("")
		}
	}
	build(false)
	if len(d.lines)+blocks+1 <= h {
		build(true)
	}
}

// devReason says in a few words why a thread has no localhost links: a
// missing worktree or a problem `!` explains first, then the dev command
// a tool started, then the source's note. "" when there is nothing to say.
func devReason(t deck.Thread) string {
	running := slices.ContainsFunc(t.DevServers, func(s deck.DevServer) bool { return s.Running })
	switch note := t.DevNote; {
	case note == deck.DevNoWorktree || strings.HasSuffix(note, deck.DevSeeSources):
		return note
	case t.DevUp != nil && t.DevUp.Alive:
		return "starting…"
	case t.DevUp != nil && !running:
		return "dev exited · see the log"
	case note == deck.DevNotStarted:
		return note + " · u starts it"
	default:
		return note
	}
}

// flow adds groups that wrap at the drawer's width, from column 1.
func (d *drawer) flow(groups ...group) {
	lw := d.labelW
	d.labelW = 0
	d.field("", false, groups...)
	d.labelW = lw
}

// chips are the links that keep returns true as one wrapping group of
// chips, `[1 ABC-1246 done]`, leaving out the PR's own (prLink), which
// the PR section shows. unlinked says a Linear ID lacks its URL.
func (m Model) chips(d *drawer, links []deck.Link, prLink int, keep func(deck.Link) bool) (g group, unlinked bool) {
	g.sep = " "
	prev := deck.LinkKind(-1)
	for i, l := range links {
		if i == prLink || !keep(l) {
			continue
		}
		g.items = append(g.items, m.chip(d, l, i, l.Kind != prev))
		prev = l.Kind
		unlinked = unlinked || l.Kind == deck.LinkLinear && l.URL == ""
	}
	return g, unlinked
}

// chip is one link as a chip: dim brackets, the digit bold, the label in
// its kind's colour, a Linear issue's state in its state colour. The kind
// word leads only the first chip of a run of one kind (never Linear's).
func (m Model) chip(d *drawer, l deck.Link, i int, first bool) item {
	var b strings.Builder
	b.WriteString(dim.Render("["))
	if i < 9 {
		b.WriteString(bold.Render(fmt.Sprint(i+1)) + " ")
	}
	st := chipStyle(l.Kind)
	closed := l.Issue != nil && issueClosed(*l.Issue)
	if first && l.Kind != deck.LinkLinear && l.Kind != deck.LinkLocalhost {
		b.WriteString(st.Render(l.Kind.String()) + " ")
	}
	label := l.Label
	if l.Kind == deck.LinkGitHub {
		label = strings.TrimPrefix(label, "PR ")
	}
	b.WriteString(st.Render(label))
	if l.Issue != nil {
		b.WriteString(" " + issueStyle(*l.Issue).Render(shortState(l.Issue.State)))
	}
	if l.Kind == deck.LinkLocalhost {
		if p := linkPort(l); p != "" {
			b.WriteString(" " + dim.Render(":"+p))
		}
		b.WriteString(" " + devDot(!l.Down))
	}
	b.WriteString(dim.Render("]"))
	text := b.String()
	style := plain
	if closed || l.Kind == deck.LinkLocalhost && l.Down {
		style = dim
	}
	it := item{text: text, style: style, act: action{kind: actLink, n: i}, stop: true}
	it.sel = d.nextStop() || m.choosing == l.Kind
	return it
}

// linkPort is the port in a localhost link's URL, unless its label shows
// it already (`:5181 web`, `localhost:5181/docs`).
func linkPort(l deck.Link) string {
	u, err := url.Parse(l.URL)
	if err != nil {
		return ""
	}
	p := u.Port()
	if p == "" {
		return ""
	}
	// :80 is not shown by a label with :8080.
	for rest := l.Label; ; {
		i := strings.Index(rest, ":"+p)
		if i < 0 {
			return p
		}
		rest = rest[i+1+len(p):]
		if rest == "" || rest[0] < '0' || rest[0] > '9' {
			return ""
		}
	}
}

// titled reports whether l is a Linear link whose issue has a title, which
// the Links section shows on a line of its own.
func titled(l deck.Link) bool {
	return l.Kind == deck.LinkLinear && l.Issue != nil && l.Issue.Title != ""
}

func hasTitled(links []deck.Link, prLink int) bool {
	for i, l := range links {
		if i != prLink && titled(l) {
			return true
		}
	}
	return false
}

// minTitle is the fewest columns a cut title keeps; with less room the
// chip leaves the title out.
const minTitle = 8

// issueChip is a Linear issue as a one-line chip, `[1 ABC-123 Fix login ·
// in progress · ana]`, shortened to fit the drawer: the assignee first
// becomes their initials, then the title is cut, then left out, and last
// the assignee goes.
func (m Model) issueChip(d *drawer, l deck.Link, i int) item {
	is := *l.Issue
	avail := max(d.width-1, 1)
	head := "["
	if i < 9 {
		head += fmt.Sprint(i+1) + " "
	}
	state := shortState(is.State)
	who := is.Assignee
	title := is.Title
	width := func() int {
		w := ansi.StringWidth(head+l.Label) + 3 + ansi.StringWidth(state) + 1 // " · " and "]"
		if title != "" {
			w += 1 + ansi.StringWidth(title)
		}
		if who != "" {
			w += 3 + ansi.StringWidth(who)
		}
		return w
	}
	if width() > avail && who != "" && is.AssigneeInitials != "" {
		who = is.AssigneeInitials
	}
	if over := width() - avail; over > 0 {
		if keep := ansi.StringWidth(title) - over; keep >= minTitle {
			title = strings.TrimRight(ansi.Truncate(title, keep-1, ""), " ") + "…"
		} else {
			title = ""
		}
	}
	if width() > avail {
		who = ""
	}

	var b strings.Builder
	b.WriteString(dim.Render("["))
	if i < 9 {
		b.WriteString(bold.Render(fmt.Sprint(i+1)) + " ")
	}
	b.WriteString(chipStyle(l.Kind).Render(l.Label))
	if title != "" {
		b.WriteString(" " + plain.Render(title))
	}
	b.WriteString(dim.Render(" · ") + issueStyle(is).Render(state))
	if who != "" {
		b.WriteString(dim.Render(" · ") + dim.Render(who))
	}
	b.WriteString(dim.Render("]"))
	style := plain
	if issueClosed(is) {
		style = dim
	}
	it := item{text: b.String(), style: style, act: action{kind: actLink, n: i}, stop: true}
	it.sel = d.nextStop() || m.choosing == l.Kind
	return it
}

// chipStyle colours a chip's label: Linear blue, Figma magenta, GitHub
// magenta like the PR column, the rest plain.
func chipStyle(k deck.LinkKind) lipgloss.Style {
	switch k {
	case deck.LinkLinear:
		return lipgloss.NewStyle().Foreground(colBlue)
	case deck.LinkFigma, deck.LinkGitHub:
		return reviewStyle
	}
	return plain
}

// prSection is a short summary of the PR: its chip with its state, review
// and comments, its checks, why it is not merging, and a line pointing to
// the PR tab, which has the rest.
func (m Model) prSection(d *drawer, pr deck.PullRequest, links []deck.Link, prLink int, narrow bool) {
	var g group
	g.sep = " "
	if prLink >= 0 {
		g.items = append(g.items, m.chip(d, links[prLink], prLink, false))
	} else {
		g.items = append(g.items, span(fmt.Sprintf("#%d", pr.Number), reviewStyle))
	}
	add := func(text string, st lipgloss.Style) {
		g.items = append(g.items, span("·", dim), span(text, st))
	}
	if s := strings.ToLower(pr.State); s != "" {
		if pr.Live && pr.Draft && pr.State == "OPEN" {
			s = "draft"
		}
		g.items = append(g.items, span(s, plain))
	}
	if pr.State == "OPEN" && pr.Review != "" {
		add(reviewText(pr), reviewTextStyle(pr.Review))
	}
	if pr.Live && pr.State == "OPEN" && pr.AutoMerge {
		add("auto-merge on", okStyle)
	}
	if n := pr.Comments; n > 0 {
		switch {
		case narrow:
			add(fmt.Sprintf("%dc", n), plain)
		case len(pr.Commenters) > 0:
			add(fmt.Sprintf("%d %s (%s)", n, plural(n, "comment"), strings.Join(pr.Commenters, ", ")), plain)
		default:
			add(fmt.Sprintf("%d %s", n, plural(n, "comment")), plain)
		}
	}
	d.flow(g)
	switch {
	case pr.Live && len(pr.Checks) > 0 && pr.State == "OPEN":
		d.flow(m.checksGroup(d, pr))
	case pr.State != "":
		if n := len(pr.FailingChecks); n > 0 {
			d.flow(words(fmt.Sprintf("✕ %d failing: %s", n, strings.Join(pr.FailingChecks, ", ")), failStyle))
		} else if pr.State == "OPEN" {
			d.flow(words("✓ no failing checks", okStyle))
		}
	}
	if text, st := mergeReason(pr); text != "" {
		d.flow(words(text, st))
	}
	text := "→ PR tab: description and comments"
	if current, outdated := threadCounts(pr); pr.Live && pr.State == "OPEN" && current+outdated > 0 {
		text = fmt.Sprintf("→ PR tab: %d unresolved review %s, description and comments", current+outdated, plural(current+outdated, "thread"))
		if narrow {
			text = fmt.Sprintf("→ PR tab: %d unresolved", current+outdated)
		}
	} else if narrow {
		text = "→ PR tab"
	}
	it := item{text: text, style: dim, act: action{kind: actTab, n: int(tabPR)}, stop: true}
	it.sel = d.nextStop()
	d.flow(group{items: []item{it}})
}

// reviewTextStyle colours a review decision: required magenta, approved
// green, changes requested red.
func reviewTextStyle(review string) lipgloss.Style {
	switch review {
	case "APPROVED":
		return okStyle
	case "CHANGES_REQUESTED":
		return failStyle
	}
	return reviewStyle
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// ago is an age in words: "now", "1m ago".
func ago(d time.Duration) string {
	if a := ageText(d); a != "now" {
		return a + " ago"
	}
	return "just now"
}

// threadSection is the metadata the card has no room for, under dim
// labels: the thread (or all of a task's threads), the pane and its
// agent, the full branch, report, base and the dev log.
func (m Model) threadSection(d *drawer, r row, t deck.Thread) {
	narrow := d.width < wideMin
	if len(r.threads) > 1 {
		for _, th := range r.threads {
			g := group{sep: " ", items: []item{span(th.ID, bold)}}
			g.items = append(g.items, inlineWords(th.Title, plain, d.light).items...)
			st, ss := statusPill(th)
			g.items = append(g.items, span("·", dim), span(st, ss))
			d.field("thread", false, g)
		}
	} else {
		g := group{sep: " ", items: []item{span(t.ID, plain)}}
		if r.task != nil && t.Title != "" && t.Title != r.task.Title {
			g.items = append(g.items, span("·", dim))
			g.items = append(g.items, inlineWords(t.Title, dim, d.light).items...)
		}
		d.field("thread", false, g)
	}
	if items := m.paneValue(t); len(items) > 0 {
		d.field("pane", false, group{sep: " ", items: items})
	}
	if t.Branch != "" {
		d.field("branch", false, words(t.Branch, plain))
	}
	if t.Report != "" {
		g := group{sep: " ", items: []item{span(reportName(t), plain)}}
		if !t.LastReportChange.IsZero() {
			g.items = append(g.items, span("·", dim), span("changed "+m.clock(t.LastReportChange, false), dim))
		}
		hint := "r shows it"
		if narrow {
			hint = "r"
		}
		g.items = append(g.items, span("·", dim), span(hint, dim))
		d.field("report", false, g)
	}
	if !narrow {
		base := t.Base
		mb := ""
		if dt, df, ok := m.diff(); ok && dt.ID == t.ID {
			if df.Base != "" {
				base = df.Base
			}
			mb = df.MergeBase
		}
		if base != "" {
			g := words(base, plain)
			if mb != "" {
				g.items = append(g.items, span("·", dim), span("merge-base "+shortSHA(mb), dim))
			}
			d.field("base", false, g)
		}
	}
	if t.DevUp != nil {
		d.field("log", false, words(tilde(t.DevUp.Log), dim))
	}
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// paneValue is the thread's herdr pane and its agent's live state; "closed"
// when herdr was read and does not show the recorded pane.
func (m Model) paneValue(t deck.Thread) []item {
	switch {
	case t.Pane != nil:
		items := []item{span(t.Pane.ID, plain)}
		if st := t.Pane.AgentStatus; st != "" && st != "unknown" {
			style := dim
			switch st {
			case "blocked":
				style = needsStyle
			case "working":
				style = workStyle
			}
			items = append(items, span("·", dim), span(agentName(t.Pane.Agent)+" "+st, style))
		}
		return items
	case t.PaneID == "":
		return nil
	case m.snap.Herdr && t.Status != deck.StatusDone:
		return []item{span(t.PaneID, plain), span("closed", dim)}
	}
	return []item{span(t.PaneID, plain)}
}

// clock is a time of day in the deck's zone, with the weekday when it is
// not today (unless sameDay says the caller shows the day already).
func (m Model) clock(t time.Time, sameDay bool) string {
	lt := t.In(m.loc())
	now := m.opt.Now().In(m.loc())
	if sameDay || lt.YearDay() == now.YearDay() && lt.Year() == now.Year() {
		return lt.Format("15:04")
	}
	return lt.Format("Mon 15:04")
}
