package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// With CopyLinks a link goes on the clipboard with OSC 52 and is never
// opened; without it the same key opens the link.
func TestCopyLinksCopiesInsteadOfOpening(t *testing.T) {
	const want = "https://linear.app/acme/issue/ABC-1256"

	m, o := newModelWith(t, calm(), 80, 28, func(opt *Options) { opt.CopyLinks = true })
	m, cmd := press(m, keys("2")...)
	if len(o.urls) != 0 {
		t.Errorf("CopyLinks: opened %q, want nothing opened", o.urls)
	}
	if cmd == nil || cmd() != tea.SetClipboard(want)() {
		t.Errorf("CopyLinks: command does not set the clipboard to %q", want)
	}
	if !strings.Contains(m.Status(), "copied the link to ABC-1256") {
		t.Errorf("CopyLinks: status = %q", m.Status())
	}

	m, o = newModel(t, calm(), 80, 28)
	m, _ = press(m, keys("2")...)
	if len(o.urls) != 1 || o.urls[0] != want {
		t.Errorf("without CopyLinks: opened %q, want %q", o.urls, want)
	}
	if !strings.Contains(m.Status(), "opened ABC-1256") {
		t.Errorf("without CopyLinks: status = %q", m.Status())
	}
}
