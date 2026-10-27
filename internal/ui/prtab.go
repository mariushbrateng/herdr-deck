package ui

import (
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FredricW/herdr-deck/internal/deck"
	"github.com/FredricW/herdr-deck/internal/markdown"
)

// The PR tab: the thread PR's status, what it is, its description and its
// conversation. The status comes from the GitHub reader's regular read;
// the rest (deck.PRDetail) is read while the tab shows the PR.

// prTab fills the PR tab for the row's PR: its thread's, or the one its
// task links.
func (m Model) prTab(d *drawer, r row, links []deck.Link) {
	p, _, ok := r.pr()
	if !ok {
		return
	}
	pr := *p
	prLink := -1
	for i, l := range links {
		if l.Kind == deck.LinkGitHub && l.URL == pr.URL {
			prLink = i
		}
	}
	det := pr.Detail
	heading := func(name, note string) {
		if len(d.lines) > 0 {
			d.line("")
		}
		d.sections = append(d.sections, section{name: name, at: len(d.lines)})
		head := " " + dim.Render("── ") + bold.Render(name) + dim.Render(" ──")
		if note != "" {
			head += "  " + dim.Render(note)
		}
		d.line(head)
	}

	// About first, with no heading: the tab opens on what the PR is.
	title := group{sep: " ", hang: 2}
	if prLink >= 0 {
		title.items = append(title.items, m.chip(d, links[prLink], prLink, false))
	} else {
		title.items = append(title.items, span(fmt.Sprintf("#%d", pr.Number), reviewStyle))
	}
	if det != nil {
		title.items = append(title.items, words(det.Title, bold).items...)
	}
	d.flow(title)
	if det == nil {
		d.flow(m.detailMissing(pr))
	} else {
		who := group{sep: " ", items: []item{span(det.Author, plain)}}
		if !det.Created.IsZero() {
			who.items = append(who.items, span("· opened "+ago(m.opt.Now().Sub(det.Created)), dim))
		}
		if !det.Updated.IsZero() {
			who.items = append(who.items, span("· updated "+ago(m.opt.Now().Sub(det.Updated)), dim))
		}
		d.field("author", false, who)
		if det.Head != "" {
			d.field("branch", false, group{sep: " ", items: []item{span(m.shortBranch(det.Head), plain), span("→", dim), span(det.Base, plain)}})
		}
		if len(det.Labels) > 0 {
			g := group{sep: " "}
			for _, l := range det.Labels {
				g.items = append(g.items, span(l, workStyle))
			}
			d.field("labels", false, g)
		}
		if g := reviewersGroup(pr, det); len(g.items) > 0 {
			d.field("reviews", false, g)
		}
		if det.ChangedFiles > 0 || det.Additions+det.Deletions > 0 {
			d.field("changes", false, group{sep: " ", items: []item{
				span(fmt.Sprintf("+%d", det.Additions), okStyle), span(fmt.Sprintf("−%d", det.Deletions), failStyle),
				span(fmt.Sprintf("· %d %s", det.ChangedFiles, plural(det.ChangedFiles, "file")), dim),
			}})
		}
	}

	heading("Status", "")
	d.flow(prStateGroup(pr))
	if text, st := mergeReason(pr); text != "" {
		d.flow(words(text, st))
	}
	switch {
	case pr.Live && len(pr.Checks) > 0:
		d.flow(m.checksGroup(d, pr))
	case r.pull != nil && r.pull.CIState() != deck.CINone:
		// The GitHub views' list has the checks taken together only.
		g, text, st := ciState(*r.pull)
		d.flow(group{sep: " ", items: []item{span(g+" checks "+text, st), span("· reading them…", dim)}})
	case pr.Live && pr.State == "OPEN":
		d.flow(words("no checks", dim))
	case len(pr.FailingChecks) > 0:
		d.flow(words(fmt.Sprintf("✕ %d failing: %s", len(pr.FailingChecks), strings.Join(pr.FailingChecks, ", ")), failStyle))
	}
	if !pr.CheckedAt.IsZero() {
		d.flow(words("checked "+ago(m.opt.Now().Sub(pr.CheckedAt)), dim))
	}

	if det == nil {
		// The regular read has the unresolved review threads already.
		if len(pr.Threads) > 0 {
			heading("Review threads", fmt.Sprintf("%d unresolved", len(pr.Threads)))
			for i, t := range pr.Threads {
				if i > 0 {
					d.line("")
				}
				m.comment(d, pr, deck.PRComment{Kind: deck.CommentThread, Author: t.Author, Body: t.Body, URL: t.URL,
					At: t.At, Path: t.Path, Line: t.Line, Outdated: t.Outdated, Replies: t.Replies}, threadComment+i)
			}
		}
		return
	}

	heading("Description", "")
	if det.Body == "" {
		d.flow(words("No description.", dim))
	} else {
		d.markdown(det.Body)
	}

	heading("Comments", commentsNote(det))
	if det.Earlier > 0 {
		d.flow(words(fmt.Sprintf("… %d earlier on GitHub", det.Earlier), dim))
	}
	if len(det.Comments) == 0 {
		d.flow(words("No comments yet.", dim))
	}
	for i, c := range det.Comments {
		if i > 0 {
			d.line("")
		}
		m.comment(d, pr, c, i)
	}
}

// threadComment offsets the indexes of review threads shown before the
// detail is read (pr.Threads), so actComment tells them from comments.
const threadComment = 1 << 20

// prStateGroup is the PR's state, review decision and auto-merge in
// words: `open · review required · auto-merge on`.
func prStateGroup(pr deck.PullRequest) group {
	g := group{sep: " "}
	add := func(text string, st lipgloss.Style) {
		if len(g.items) > 0 {
			g.items = append(g.items, span("·", dim))
		}
		g.items = append(g.items, span(text, st))
	}
	if s := strings.ToLower(pr.State); s != "" {
		if pr.Live && pr.Draft && pr.State == "OPEN" {
			s = "draft"
		}
		st := plain
		switch pr.State {
		case "MERGED":
			st = okStyle
		case "CLOSED":
			st = failStyle
		}
		add(s, st)
	}
	if pr.State == "OPEN" && pr.Review != "" {
		add(reviewText(pr), reviewTextStyle(pr.Review))
	}
	if pr.Live && pr.State == "OPEN" && pr.AutoMerge {
		add("auto-merge on", okStyle)
	}
	if len(g.items) == 0 {
		add("state not known yet", dim)
	}
	return g
}

// detailMissing says why the PR tab has no description and comments: not
// read yet, a failed read, or GitHub not read at all.
func (m Model) detailMissing(pr deck.PullRequest) group {
	switch {
	case pr.DetailNote != "":
		return words(pr.DetailNote, warnStyle)
	case !pr.Live || m.opt.DetailPR == nil:
		return words("No description or comments: the deck does not read GitHub here (see !); g opens the PR", dim)
	}
	return words("reading the description and comments…", dim)
}

// reviewersGroup is who reviewed and where they stand, then who is asked
// and has not answered: `✓ alex  ✕ sam  ◌ frontend`.
func reviewersGroup(pr deck.PullRequest, det *deck.PRDetail) group {
	g := group{sep: "  "}
	for _, r := range det.Reviewers {
		glyph, st := reviewGlyph(r.State)
		g.items = append(g.items, span(st.Render(glyph)+" "+r.Login, plain))
	}
	for _, r := range pr.ReviewRequests {
		g.items = append(g.items, span(reviewStyle.Render("◌")+" "+r+dim.Render(" asked"), plain))
	}
	return g
}

// reviewGlyph is a review state's glyph and colour.
func reviewGlyph(state string) (string, lipgloss.Style) {
	switch state {
	case "APPROVED":
		return "✓", okStyle
	case "CHANGES_REQUESTED":
		return "✕", failStyle
	case "DISMISSED":
		return "–", dim
	}
	return "○", dim
}

// reviewWords is a review state in words.
func reviewWords(state string) string {
	switch state {
	case "APPROVED":
		return "approved"
	case "CHANGES_REQUESTED":
		return "requested changes"
	case "DISMISSED":
		return "review dismissed"
	}
	return "reviewed"
}

// commentsNote is the Comments heading's note: how many, with the
// unresolved review threads.
func commentsNote(det *deck.PRDetail) string {
	if len(det.Comments) == 0 {
		return ""
	}
	open := 0
	for _, c := range det.Comments {
		if c.Kind == deck.CommentThread && !c.Resolved {
			open++
		}
	}
	note := fmt.Sprintf("%d", len(det.Comments)+det.Earlier)
	if open > 0 {
		note += fmt.Sprintf(" · %d unresolved", open)
	}
	return note
}

// folded says whether comment c shows as one dim line: an app's comment
// or a resolved review thread, until the user expands it.
func (m Model) folded(pr deck.PullRequest, c deck.PRComment, i int) (foldable, folded bool) {
	if !c.Bot && !c.Resolved {
		return false, false
	}
	return true, !m.unfolded[commentKey(pr, c, i)]
}

func commentKey(pr deck.PullRequest, c deck.PRComment, i int) string {
	if c.URL != "" {
		return c.URL
	}
	return fmt.Sprintf("%s#%d", pr.URL, i)
}

// comment adds comment i: a line with who, what and when that opens it on
// GitHub, then its body as Markdown. An app's comment and a resolved
// thread fold to one dim line, `▸` in its third column (clear of the
// cursor's), with the start of the body.
func (m Model) comment(d *drawer, pr deck.PullRequest, c deck.PRComment, i int) {
	foldable, folded := m.folded(pr, c, i)
	when := ""
	if !c.At.IsZero() {
		when = ago(m.opt.Now().Sub(c.At))
	}
	var head []string // plain parts after the lead
	lead := bold.Render(c.Author)
	switch c.Kind {
	case deck.CommentReview:
		glyph, st := reviewGlyph(c.State)
		lead += " " + st.Render(glyph+" "+reviewWords(c.State))
		if c.Inline > 0 {
			head = append(head, fmt.Sprintf("%d on files", c.Inline))
		}
	case deck.CommentThread:
		where := c.Path
		if c.Line > 0 {
			where += fmt.Sprintf(":%d", c.Line)
		}
		lead = plain.Render(where) + dim.Render(" · ") + lead
		if c.Replies > 0 {
			head = append(head, fmt.Sprintf("+%d %s", c.Replies, plural(c.Replies, "reply")))
		}
		if c.Outdated {
			head = append(head, "outdated")
		}
		if c.Resolved {
			head = append(head, "resolved")
		}
	}
	if c.Bot {
		head = append([]string{"bot"}, head...)
	}
	if when != "" {
		head = append(head, when)
	}
	text := " "
	open := action{kind: actComment, n: i}
	var zones []zone
	if foldable {
		glyph := " ▾ "
		if folded {
			glyph = " ▸ "
		}
		text += dim.Render(glyph)
		zones = append(zones, zone{x0: 1, x1: 4, act: action{kind: actFold, n: i}})
	}
	x0 := ansi.StringWidth(text)
	if folded {
		text += dim.Render(ansi.Strip(lead) + " · " + strings.Join(head, " · "))
		if snippet := firstText(c.Body); snippet != "" {
			text += dim.Render(": " + snippet)
		}
	} else {
		text += lead
		if len(head) > 0 {
			text += dim.Render(" · " + strings.Join(head, " · "))
		}
	}
	zones = append(zones, zone{x0: x0, x1: d.width, act: open})
	d.stopLine(text, open, zones)
	if folded || c.Body == "" {
		return
	}
	for _, l := range markdown.Render(c.Body, d.width-4, !d.light) {
		d.lines = append(d.lines, dline{text: "   " + ansi.Truncate(l, d.width-3, "…")})
	}
}

// linkDef is a Markdown link definition, `[vc]: #…`, which apps put on
// top of their comments.
var linkDef = regexp.MustCompile(`^\[[^\]]+\]:`)

// firstText is the first line of Markdown src with words, plain: HTML,
// tables and link definitions, which apps' comments start with, left out.
func firstText(src string) string {
	for _, l := range strings.Split(src, "\n") {
		l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#>-*"))
		switch {
		case l == "", strings.HasPrefix(l, "<"), strings.HasPrefix(l, "|"), linkDef.MatchString(l):
			continue
		}
		return l
	}
	return ""
}

// openComment opens the PR tab's comment i on GitHub, or the PR when the
// comment has no link.
func (m *Model) openComment(i int) tea.Cmd {
	pr, ok := m.prOf()
	if ok && i >= threadComment && i-threadComment < len(pr.Threads) {
		t := pr.Threads[i-threadComment]
		url := t.URL
		if url == "" {
			url = pr.URL + "/files"
		}
		return m.openURL(url, "the review thread on "+t.Path)
	}
	if !ok || pr.Detail == nil || i < 0 || i >= len(pr.Detail.Comments) {
		return nil
	}
	c := pr.Detail.Comments[i]
	url := c.URL
	if url == "" {
		url = pr.URL
	}
	return m.openURL(url, c.Author+"'s comment on GitHub")
}

// toggleComment folds or unfolds the PR tab's comment i, when it folds.
func (m *Model) toggleComment(i int) {
	pr, ok := m.prOf()
	if !ok || pr.Detail == nil || i < 0 || i >= len(pr.Detail.Comments) {
		return
	}
	c := pr.Detail.Comments[i]
	if foldable, folded := m.folded(pr, c, i); foldable {
		m.unfolded[commentKey(pr, c, i)] = folded
	}
}

// prOnlyKey handles the PR tab's own keys in the focused drawer: space,
// → or l unfold the comment under the cursor, ← or h fold it.
func (m *Model) prOnlyKey(msg tea.KeyPressMsg) bool {
	if m.mode != modeRow || m.curTab() != tabPR || !m.dfocus {
		return false
	}
	s := msg.String()
	switch s {
	case "space", "right", "l", "left", "h":
	default:
		return false
	}
	l := m.layout()
	if l.drawer == nil || m.dcur >= len(l.drawer.stops) {
		return true
	}
	a := l.drawer.stops[m.dcur].act
	pr, ok := m.prOf()
	if a.kind != actComment || !ok || pr.Detail == nil || a.n >= len(pr.Detail.Comments) {
		return true
	}
	c := pr.Detail.Comments[a.n]
	foldable, folded := m.folded(pr, c, a.n)
	switch {
	case !foldable:
	case s == "space":
		m.toggleComment(a.n)
	case s == "right" || s == "l":
		if folded {
			m.toggleComment(a.n)
		}
	case !folded:
		m.toggleComment(a.n)
	}
	return true
}

// showPRTab selects list row i and shows its PR tab, as a click on the
// list's PR column does; a second click, with the tab already showing
// that row's PR, opens the PR on GitHub.
func (m *Model) showPRTab(i int) tea.Cmd {
	again := i == m.cursor && m.mode == modeRow && m.curTab() == tabPR && m.size != sizeHidden
	if i != m.cursor {
		m.dcur = 0
	}
	m.cursor, m.moved = i, true
	m.choosing, m.dfocus = noKind, false
	if again {
		if pr, ok := m.prOf(); ok {
			return m.openURL(pr.URL, fmt.Sprintf("#%d on GitHub", pr.Number))
		}
		return nil
	}
	m.switchTab(tabPR)
	if m.size == sizeHidden {
		m.size = sizeNormal
	}
	m.ensureVisible()
	return nil
}
