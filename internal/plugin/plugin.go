// Package plugin holds the commands herdr runs for the herdr-deck plugin
// (herdr-plugin.toml): the toggle action, and the hook that opens a deck
// next to a herdr-projects coordinator when its agent starts.
//
// herdr runs them with the plugin folder as working directory and passes
// the context in HERDR_* environment variables: HERDR_PANE_ID is the pane an
// event is about, HERDR_PLUGIN_CONTEXT_JSON names the focused pane for an
// action.
package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/FredricW/herdr-deck/internal/config"
	"github.com/FredricW/herdr-deck/internal/project"
	"github.com/FredricW/herdr-deck/internal/source/board"
	"github.com/FredricW/herdr-deck/internal/source/herdr"
)

// Env is what a hook knows about where it runs.
type Env struct {
	Getenv func(string) string
	Root   string // the herdr-projects root, as configured
	// Links and OpenURL are for the open-link action.
	Links   LinkSettings
	OpenURL func(url string) error
}

// Run runs the plugin command named by args[0]: "toggle", "agent-detected"
// or "open-link" (which takes optional text to open).
func Run(ctx context.Context, h Host, env Env, args []string) error {
	if len(args) > 1 && args[0] == "open-link" {
		return OpenLink(ctx, h, env, args[1:])
	}
	if len(args) != 1 {
		return errors.New("usage: herdr-deck plugin toggle|agent-detected|open-link [text]")
	}
	switch args[0] {
	case "open-link":
		return OpenLink(ctx, h, env, nil)
	case "toggle":
		return Toggle(ctx, h, env, focusedPane(env.Getenv))
	case "agent-detected":
		ev, err := parseEvent(env.Getenv("HERDR_PLUGIN_EVENT_JSON"))
		if err != nil {
			return err
		}
		if ev.PaneID == "" {
			ev.PaneID = env.Getenv("HERDR_PANE_ID")
		}
		return withLock(env.Getenv("HERDR_PLUGIN_STATE_DIR"), func() error {
			_, err := AgentDetected(ctx, h, env, ev)
			return err
		})
	}
	return fmt.Errorf("unknown plugin command %q", args[0])
}

// Event is the part of herdr's pane_agent_detected event the hook reads.
type Event struct {
	PaneID   string `json:"pane_id"`
	Agent    string `json:"agent"`
	Released bool   `json:"released"`
}

func parseEvent(s string) (Event, error) {
	var wrap struct {
		Data Event `json:"data"`
	}
	if s == "" {
		return Event{}, nil
	}
	if err := json.Unmarshal([]byte(s), &wrap); err != nil {
		return Event{}, fmt.Errorf("event: %w", err)
	}
	return wrap.Data, nil
}

// focusedPane is the pane an action was invoked on.
func focusedPane(getenv func(string) string) string {
	var c struct {
		Focused string `json:"focused_pane_id"`
	}
	if json.Unmarshal([]byte(getenv("HERDR_PLUGIN_CONTEXT_JSON")), &c) == nil && c.Focused != "" {
		return c.Focused
	}
	return getenv("HERDR_PANE_ID")
}

// AgentDetected opens a deck next to the agent's pane when that agent is a
// herdr-projects coordinator and its workspace has no deck yet. It returns
// the new deck's pane id, or "" when it opened none.
//
// A coordinator is an agent whose working directory is a project folder,
// <root>/<slug> with a PROJECT.md. Thread agents run in worktrees or under
// <root>/<slug>/threads/, so they never get a deck of their own.
func AgentDetected(ctx context.Context, h Host, env Env, ev Event) (string, error) {
	if ev.PaneID == "" || ev.Agent == "" || ev.Released {
		return "", nil
	}
	st, err := h.Snapshot(ctx)
	if err != nil {
		return "", err
	}
	pane, ok := findPane(st, ev.PaneID)
	if !ok {
		return "", nil
	}
	slug, ok := coordinatorSlug(pane.Cwd, env.Root)
	if !ok {
		return "", nil
	}
	for _, p := range st.Panes {
		if p.WorkspaceID == pane.WorkspaceID && p.Tokens[Token] != "" {
			return "", nil
		}
	}
	// Never focus it: the coordinator is just starting and has the user's
	// attention.
	return open(ctx, h, env, pane.ID, slug, false)
}

// Toggle closes the deck when it is the focused pane, focuses the deck in
// the focused pane's tab, or else opens a focused deck to the right of the
// focused pane. The deck shows the project the pane works in: its folder
// under the projects root, else its herdr-projects hp_project token, else
// $HERDR_DECK_PROJECT.
func Toggle(ctx context.Context, h Host, env Env, focused string) error {
	if focused == "" {
		return errors.New("toggle: no focused pane")
	}
	st, err := h.Snapshot(ctx)
	if err != nil {
		return err
	}
	pane, ok := findPane(st, focused)
	if !ok {
		return fmt.Errorf("toggle: pane %s not found", focused)
	}
	if pane.Tokens[Token] != "" {
		return closeDeck(ctx, h, pane.ID)
	}
	for _, p := range st.Panes {
		if p.TabID == pane.TabID && p.Tokens[Token] != "" {
			return h.Focus(ctx, p.ID)
		}
	}
	slug := paneSlug(pane, env)
	if board.Use(env.Root, slug) {
		// herdr-projects 1.x: any tab's deck shows the board.
		slug = board.Slug
	}
	if slug == "" {
		_ = h.Notify(ctx, "Deck: no project here", "Toggle it in a herdr-projects project's pane, or set "+project.EnvProject+".")
		return fmt.Errorf("toggle: pane %s is not in a herdr-projects project", pane.ID)
	}
	_, err = open(ctx, h, env, pane.ID, slug, true)
	return err
}

// closeDeck closes a deck and hands focus back to the pane it sat next to.
// When that fails, herdr picks the pane to focus.
func closeDeck(ctx context.Context, h Host, deck string) error {
	next, _ := h.LeftOf(ctx, deck)
	if err := h.Close(ctx, deck); err != nil {
		return err
	}
	if next != "" {
		_ = h.Focus(ctx, next)
	}
	return nil
}

// MarkSelf sets Token on the deck's own pane when herdr started the deck as
// this plugin's pane. Errors are ignored: the mark only helps the hooks
// find the deck.
func MarkSelf(ctx context.Context, h Host, getenv func(string) string, slug string) {
	if getenv("HERDR_PLUGIN_ID") != ID || getenv("HERDR_PLUGIN_ENTRYPOINT_ID") != Entrypoint {
		return
	}
	if pane := getenv("HERDR_PANE_ID"); pane != "" {
		_ = h.Mark(ctx, pane, slug)
	}
}

// open opens a deck for slug right of target, marks it and sizes its split.
// focus says whether the deck takes focus from target.
func open(ctx context.Context, h Host, env Env, target, slug string, focus bool) (string, error) {
	vars := map[string]string{project.EnvProject: slug}
	if env.Root != "" {
		vars[config.EnvProjectsRoot] = env.Root
	}
	// A deck pane need not inherit the hook's environment, so a config file
	// named there is passed on; the default path the deck finds itself.
	if c := env.Getenv(config.EnvPath); c != "" {
		vars[config.EnvPath] = c
	}
	cwd := filepath.Join(env.Root, slug)
	if slug == board.Slug {
		cwd = env.Root
	}
	deck, err := h.Open(ctx, Open{Target: target, Cwd: cwd, Env: vars, Focus: focus})
	if err != nil {
		return "", err
	}
	if deck == "" {
		return "", errors.New("plugin.pane.open returned no pane id")
	}
	if err := h.Mark(ctx, deck, slug); err != nil {
		return deck, err
	}
	// A deck too narrow or too wide is still a deck: sizing is best effort.
	if l, err := h.Layout(ctx, deck); err == nil {
		if path, width, ok := deckSplit(l, deck); ok {
			_ = h.SetRatio(ctx, deck, path, ratio(width))
		}
	}
	return deck, nil
}

func findPane(st herdr.State, id string) (herdr.Pane, bool) {
	for _, p := range st.Panes {
		if p.ID == id {
			return p, true
		}
	}
	return herdr.Pane{}, false
}

// coordinatorSlug is the project whose folder cwd is: <root>/<slug> itself,
// not a folder under it, holding a PROJECT.md. herdr reports physical paths,
// so both sides have their symlinks resolved first.
func coordinatorSlug(cwd, root string) (string, bool) {
	if cwd == "" || root == "" {
		return "", false
	}
	cwd, root = realPath(cwd), realPath(root)
	if filepath.Dir(cwd) != root {
		return "", false
	}
	slug := filepath.Base(cwd)
	if strings.HasPrefix(slug, ".") {
		return "", false
	}
	if _, err := os.Stat(filepath.Join(cwd, "PROJECT.md")); err != nil {
		return "", false
	}
	return slug, true
}

// paneSlug is the project a pane works in, or "".
func paneSlug(p herdr.Pane, env Env) string {
	noEnv := func(string) string { return "" }
	if p.Cwd != "" && env.Root != "" {
		if s, err := project.Slug("", noEnv, realPath(p.Cwd), realPath(env.Root)); err == nil {
			return s
		}
	}
	if s := p.Tokens["hp_project"]; s != "" {
		return s
	}
	return env.Getenv(project.EnvProject)
}

func realPath(p string) string {
	p = filepath.Clean(p)
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
