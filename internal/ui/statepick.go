package ui

// The state picker: m on a Linear issue, or a click on its state mark in
// the list or its status in the drawer, opens a box over the deck with the
// issue's team states in Linear's order. ↑↓ and ↵, a state's key or a click
// moves the issue there; esc, m or a click outside keeps it.

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// markX is the screen column of an issue row's state mark.
const markX = 2

// pickBox is the state picker as drawn: its top left corner, its width,
// its lines (borders included) and which state its first state line is.
type pickBox struct {
	x, y, w int
	lines   []string
	first   int // the state on lines[1]
	shown   int // how many states show
}

// over draws the box over the screen lines, each w columns wide.
func (b pickBox) over(lines []string, w int) []string {
	out := append([]string(nil), lines...)
	for i, bl := range b.lines {
		y := b.y + i
		if y < 0 || y >= len(out) {
			continue
		}
		line := fit(out[y], w)
		out[y] = ansi.Cut(line, 0, b.x) + bl + ansi.Cut(line, b.x+b.w, w)
	}
	return out
}

// statePickBox lays out the open state picker: under the issue's row when
// it fits, else above it, scrolled so the selected state shows.
func (m Model) statePickBox() (pickBox, bool) {
	is, ok := m.issueByID(m.moveArmed)
	if m.moveArmed == "" || !ok {
		return pickBox{}, false
	}
	states := m.issueStates(is)
	if len(states) == 0 {
		return pickBox{}, false
	}
	// The header and its rule above, the footer and its rule below.
	top, bottom := 2, m.height-2
	shown := min(len(states), max(bottom-top-2, 1))
	h := shown + 2
	off := clamp(m.moveOff, 0, len(states)-shown)
	if m.moveSel < off {
		off = m.moveSel
	}
	if m.moveSel >= off+shown {
		off = m.moveSel - shown + 1
	}

	title := " move " + is.ID + " "
	nameW := ansi.StringWidth(title) + 2
	for _, s := range states {
		nameW = max(nameW, ansi.StringWidth(s.Name)+12)
	}
	w := min(nameW, max(m.width-markX-1, 10))
	x := markX
	if x+w > m.width {
		x = max(m.width-w, 0)
	}

	y := top
	l := m.layout()
	for i, r := range m.rows {
		if r.issue != nil && r.issue.ID == is.ID {
			if at := l.listTop + i - m.listOff; i >= m.listOff && at < l.listTop+l.listH {
				switch {
				case at+1+h <= bottom:
					y = at + 1
				case at-h >= top:
					y = at - h
				}
			}
			break
		}
	}

	inner := w - 2
	rule := func(s string) string { return ansi.Truncate(s+strings.Repeat("─", w), inner, "") }
	lines := []string{dim.Render("╭" + rule("─"+title) + "╮")}
	for i := off; i < off+shown; i++ {
		s := states[i]
		glyph, st := issueGlyph(s)
		key := " "
		if i < len(moveKeys) {
			key = moveKeys[i : i+1]
		}
		text := " " + bold.Render(key) + " " + st.Render(glyph+" "+s.Name)
		if s.ID == is.State.ID {
			text += " " + dim.Render("(now)")
		}
		text = fit(text, inner)
		if i == m.moveSel {
			text = highlight(text, m.light)
		}
		lines = append(lines, dim.Render("│")+text+dim.Render("│"))
	}
	more := ""
	if shown < len(states) {
		more = fmt.Sprintf("─ %d–%d of %d ", off+1, off+shown, len(states))
	}
	lines = append(lines, dim.Render("╰"+rule(more)+"╯"))
	return pickBox{x: x, y: y, w: w, lines: lines, first: off, shown: shown}, true
}

// statePickKey handles a key while the picker is open.
func (m *Model) statePickKey(msg tea.KeyPressMsg) tea.Cmd {
	is, ok := m.issueByID(m.moveArmed)
	if !ok {
		m.moveArmed = ""
		return nil
	}
	n := len(m.issueStates(is))
	s := msg.String()
	switch s {
	case "up", "ctrl+p":
		m.moveSel = max(m.moveSel-1, 0)
	case "down", "ctrl+n":
		m.moveSel = min(m.moveSel+1, n-1)
	case "home", "pgup":
		m.moveSel = 0
	case "end", "pgdown":
		m.moveSel = n - 1
	case "enter":
		return m.pickState(is, m.moveSel)
	default:
		// A state's key picks it; any other key closes the picker.
		if k := strings.Index(moveKeys, s); len(s) == 1 && k >= 0 && k < n {
			return m.pickState(is, k)
		}
		m.moveArmed = ""
		return nil
	}
	m.keepPickSel()
	return nil
}

// keepPickSel scrolls the picker so its selected state shows.
func (m *Model) keepPickSel() {
	if b, ok := m.statePickBox(); ok {
		m.moveOff = b.first
	}
}

// pickState closes the picker and moves the issue to its team's state n.
func (m *Model) pickState(is deck.LinearIssue, n int) tea.Cmd {
	m.moveArmed = ""
	return m.moveIssue(is, n)
}

// statePickClick handles a click while the picker is open: on a state it
// picks it, on the border it does nothing, and anywhere else it closes the
// picker.
func (m *Model) statePickClick(mouse tea.Mouse) tea.Cmd {
	b, ok := m.statePickBox()
	is, have := m.issueByID(m.moveArmed)
	if !ok || !have || mouse.X < b.x || mouse.X >= b.x+b.w || mouse.Y < b.y || mouse.Y >= b.y+len(b.lines) {
		m.moveArmed = ""
		return nil
	}
	if i := mouse.Y - b.y - 1; i >= 0 && i < b.shown && mouse.X > b.x && mouse.X < b.x+b.w-1 {
		return m.pickState(is, b.first+i)
	}
	return nil
}
