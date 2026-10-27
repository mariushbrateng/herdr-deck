// Package config reads the deck's settings file and resolves each setting
// from, in order: the command-line flag, the environment variable, the
// config file and the built-in default.
//
// The file is $XDG_CONFIG_HOME/herdr-deck/config.toml, else
// ~/.config/herdr-deck/config.toml (on macOS too); --config or
// $HERDR_DECK_CONFIG names another. Only the settings page writes it, one
// key at a time (Save), and `herdr-deck config migrate` (MigrateFile),
// which moves earlier flat keys into their tables. A missing file is fine; a file that cannot be
// parsed, an unknown key or a bad value never stops the deck: they come
// back as problems for the Sources view (or stderr) and the setting falls
// back to the next source.
package config

import (
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/FredricW/herdr-deck/internal/deck"
	"github.com/FredricW/herdr-deck/internal/launch"
)

// Environment variables: HERDR_DECK_<TABLE>_<KEY> for each setting, except
// HERDR_PROJECTS_ROOT, which is herdr-projects' own. Spec.EnvAliases are
// the earlier names that still work.
const (
	EnvPath                = "HERDR_DECK_CONFIG"
	EnvRefreshInterval     = "HERDR_DECK_UI_REFRESH_INTERVAL"
	EnvFoldedLists         = "HERDR_DECK_UI_FOLDED_LISTS"
	EnvDrawerHeight        = "HERDR_DECK_UI_DRAWER_HEIGHT"
	EnvProjectsRoot        = "HERDR_PROJECTS_ROOT"
	EnvLinearWorkspace     = deck.EnvLinearWorkspace
	EnvLinearStatus        = "HERDR_DECK_LINEAR_STATUS"
	EnvLinearAPIKeyCommand = "HERDR_DECK_LINEAR_API_KEY_COMMAND"
	EnvGitHubEnabled       = "HERDR_DECK_GITHUB_ENABLED"
	EnvGitHubRepos         = "HERDR_DECK_GITHUB_REPOS"
	EnvFigmaDesktop        = "HERDR_DECK_FIGMA_DESKTOP"
	EnvReuseTabs           = "HERDR_DECK_BROWSER_REUSE_TABS"
	EnvUpdateCheck         = "HERDR_DECK_UPDATES_CHECK"
	EnvAutoRestart         = "HERDR_DECK_UPDATES_AUTO_RESTART"
	EnvEditorCommand       = "HERDR_DECK_EDITOR_COMMAND"
	EnvEditorTerminal      = "HERDR_DECK_EDITOR_TERMINAL"
	EnvDiffCommand         = "HERDR_DECK_DIFF_COMMAND"
	EnvDiffTerminal        = "HERDR_DECK_DIFF_TERMINAL"
	EnvDiffView            = "HERDR_DECK_DIFF_VIEW"
	EnvDiffLayout          = "HERDR_DECK_DIFF_LAYOUT"
	EnvArchEnabled         = "HERDR_DECK_ARCH_ENABLED"
	EnvArchTests           = "HERDR_DECK_ARCH_TESTS"
	// EnvLinearAPIKey is internal/source/linear's: the key itself, which
	// wins over linear.api_key_command.
	EnvLinearAPIKey = "LINEAR_API_KEY"
)

// Default editor and diff tool commands. The diff tool is hunk when it is
// installed, else git diff, which pages itself in a terminal. Both diffs
// include uncommitted changes.
const (
	DefaultEditor    = "code {path}"
	DefaultDiffTool  = "hunk diff {base} -- {file}"
	FallbackDiffTool = "git -C {path} diff --merge-base {base} -- {file}"
)

// Placeholders each command may use.
var (
	EditorPlaceholders = []string{"path"}
	DiffPlaceholders   = []string{"path", "base", "file"}
)

// The Files section's views: a flat list, or the files under their
// folders.
const (
	DiffViewList = "list"
	DiffViewTree = "tree"
)

// DiffViews are diff_view's values, in the order the settings page cycles
// them.
var DiffViews = []string{DiffViewList, DiffViewTree}

// The diff preview's layouts: one column with - and + lines, or the old
// file on the left and the new one on the right.
const (
	DiffLayoutUnified = "unified"
	DiffLayoutSplit   = "split"
)

// DiffLayouts are diff.layout's values, in the order the settings page
// cycles them.
var DiffLayouts = []string{DiffLayoutUnified, DiffLayoutSplit}

// DefaultFoldedLists are the lists that start folded unless ui.folded_lists
// says otherwise: TASKS.md's Backlog and the deck's own Resolved group.
var DefaultFoldedLists = []string{"Backlog", "Resolved"}

// NoLists is how flags, environment variables and the settings page write
// an empty ui.folded_lists: no list starts folded.
const NoLists = "none"

// Drawer height default and allowed range, as a share of the pane below
// the header.
const (
	DefaultDrawerHeight = 0.5
	MinDrawerHeight     = 0.2
	MaxDrawerHeight     = 0.8
)

// Refresh interval default and allowed range.
const (
	DefaultRefreshInterval = 5 * time.Second
	MinRefreshInterval     = time.Second
	MaxRefreshInterval     = 10 * time.Minute
)

// File is config.toml as written: one table per area. Every key is
// optional: a nil field was not set. New keys are added as new optional
// fields, so an older deck reading a newer file only reports the keys it
// does not know.
type File struct {
	UI       *UI       `toml:"ui"`
	Projects *Projects `toml:"projects"`
	Linear   *Linear   `toml:"linear"`
	GitHub   *GitHub   `toml:"github"`
	Figma    *Figma    `toml:"figma"`
	Browser  *Browser  `toml:"browser"`
	Updates  *Updates  `toml:"updates"`
	Editor   *Program  `toml:"editor"`
	Diff     *Diff     `toml:"diff"`
	Arch     *Arch     `toml:"arch"`
	// Old names, by Spec.Key, the earlier flat key (Spec.Old) the file sets
	// a setting under, such as linear_workspace. Shadowed are the settings
	// the file also sets in their table: that value wins and the flat
	// key's is ignored.
	Old      map[string]string `toml:"-"`
	Shadowed map[string]bool   `toml:"-"`
}

// UI is the [ui] table: how the deck's list behaves and reloads.
type UI struct {
	RefreshInterval *Duration `toml:"refresh_interval"`
	// FoldedLists are the list headings that start folded; [] folds none.
	FoldedLists *[]string `toml:"folded_lists"`
	// DrawerHeight is the drawer's share of the pane until it is dragged.
	DrawerHeight *float64 `toml:"drawer_height"`
}

// Projects is the [projects] table.
type Projects struct {
	Root *string `toml:"root"`
}

// Linear is the [linear] table.
type Linear struct {
	Workspace *string `toml:"workspace"`
	// Status turns the Linear issue status next to IDs on or off.
	Status *bool `toml:"status"`
	// APIKeyCommand prints the Linear API key, e.g. `op read …`. The key
	// itself never goes in this file.
	APIKeyCommand *string `toml:"api_key_command"`
}

// GitHub is the [github] table.
type GitHub struct {
	// Enabled turns the deck's own PR reads through gh on or off.
	Enabled *bool `toml:"enabled"`
	// Repos are the repos, "owner/name", the GitHub and Review views
	// list open PRs from; [] lists them from every repo.
	Repos *[]string `toml:"repos"`
}

// Figma is the [figma] table.
type Figma struct {
	Desktop *bool `toml:"desktop"`
}

// Browser is the [browser] table.
type Browser struct {
	ReuseTabs *bool `toml:"reuse_tabs"`
}

// Updates is the [updates] table.
type Updates struct {
	Check       *bool `toml:"check"`
	AutoRestart *bool `toml:"auto_restart"`
}

// Program is a table such as [editor]: a command line with placeholders,
// split into argv without a shell, and whether it is a terminal program
// that opens in a new herdr pane next to the deck.
type Program struct {
	Command  *string `toml:"command"`
	Terminal *bool   `toml:"terminal"`
}

// Diff is the [diff] table: the diff tool, and the Files tab's view.
type Diff struct {
	Program
	View   *string `toml:"view"`
	Layout *string `toml:"layout"`
}

// Arch is the [arch] table: the Impact tab and how it reads a branch.
type Arch struct {
	Enabled *bool `toml:"enabled"`
	// Tests counts test files in the package graphs.
	Tests *bool `toml:"tests"`
}

// Duration is a TOML string such as "5s" or "1m30s".
type Duration struct{ time.Duration }

// UnmarshalText parses a Go duration string.
func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	if err != nil {
		return fmt.Errorf("%q is not a duration such as \"5s\"", b)
	}
	d.Duration = v
	return nil
}

// Path returns the config file path and whether it was named explicitly:
// flag, else $HERDR_DECK_CONFIG, else $XDG_CONFIG_HOME/herdr-deck/config.toml,
// else ~/.config/herdr-deck/config.toml. A relative $XDG_CONFIG_HOME is
// ignored, as the XDG spec says. It returns "" when no home is known.
func Path(flag string, getenv func(string) string) (path string, named bool) {
	if flag != "" {
		return expandHome(flag, getenv), true
	}
	if p := getenv(EnvPath); p != "" {
		return expandHome(p, getenv), true
	}
	if x := getenv("XDG_CONFIG_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "herdr-deck", "config.toml"), false
	}
	home := homeDir(getenv)
	if home == "" {
		return "", false
	}
	return filepath.Join(home, ".config", "herdr-deck", "config.toml"), false
}

// StateDir is where the deck keeps state that outlives it, such as the
// version it last ran: $XDG_STATE_HOME/herdr-deck, else
// ~/.local/state/herdr-deck. A relative $XDG_STATE_HOME is ignored. It
// returns "" when no home is known.
func StateDir(getenv func(string) string) string { return stateDir(getenv, "herdr-deck") }

// DevStateDir is the state folder every tool that reads dev manifests
// shares, for run records and logs (docs/dev-manifest.md, section 10):
// $XDG_STATE_HOME/dev-manifest, else ~/.local/state/dev-manifest (on macOS
// too). It returns "" when no home is known.
func DevStateDir(getenv func(string) string) string { return stateDir(getenv, "dev-manifest") }

func stateDir(getenv func(string) string, name string) string {
	if x := getenv("XDG_STATE_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, name)
	}
	home := homeDir(getenv)
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "state", name)
}

// CacheDir is where the deck keeps throwaway state, such as the last update
// check: $XDG_CACHE_HOME/herdr-deck, else ~/.cache/herdr-deck (on macOS
// too). It returns "" when no home is known.
func CacheDir(getenv func(string) string) string {
	if x := getenv("XDG_CACHE_HOME"); x != "" && filepath.IsAbs(x) {
		return filepath.Join(x, "herdr-deck")
	}
	home := homeDir(getenv)
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".cache", "herdr-deck")
}

// Load reads the file at path. A missing file gives an empty File and no
// problems, unless it was named explicitly. A file that does not parse
// gives an empty File. Each key decodes on its own, so a bad value or an
// unknown key is reported and skipped without losing the rest. A setting's
// earlier flat key (Spec.Old) is read too, unless its table sets it.
func Load(path string, named bool) (File, []string) {
	f := File{Old: map[string]string{}, Shadowed: map[string]bool{}}
	if path == "" {
		return f, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && !named {
			return f, nil
		}
		return f, []string{"config: " + shortErr(path, err)}
	}
	var raw map[string]toml.Primitive
	md, err := toml.Decode(string(b), &raw)
	if err != nil {
		return File{Old: map[string]string{}, Shadowed: map[string]bool{}}, []string{"config: " + path + ": " + parseErr(err) + "; the file is ignored"}
	}
	dec := f.decoders(md)
	// Tables decoded one key at a time, so a bad value or an unknown key in
	// them is reported and skipped on its own.
	tables := map[string]map[string]func(toml.Primitive) error{}
	old := map[string]string{} // flat key → Spec.Key
	for _, sp := range Specs {
		table, name := splitKey(sp.Key)
		if tables[table] == nil {
			tables[table] = map[string]func(toml.Primitive) error{}
		}
		tables[table][name] = dec[sp.Key]
		if sp.Old != "" {
			old[sp.Old] = sp.Key
		}
	}
	var problems []string
	for _, k := range md.Keys() {
		if _, ok := tables[k[0]]; ok || len(k) != 1 {
			continue // below, or inside a table
		}
		key, ok := old[k[0]]
		if !ok {
			// An unknown table is one problem, not one per key in it.
			problems = append(problems, fmt.Sprintf("config: %s: unknown key %q, ignored", path, k[0]))
			continue
		}
		f.Old[key] = k[0]
		if md.IsDefined(strings.Split(key, ".")...) {
			f.Shadowed[key] = true
			continue
		}
		if err := dec[key](raw[k[0]]); err != nil {
			problems = append(problems, fmt.Sprintf("config: %s: %s: %s; ignored", path, k[0], valueErr(err)))
		}
	}
	for _, name := range slices.Sorted(maps.Keys(tables)) {
		p, ok := raw[name]
		if !ok {
			continue
		}
		problems = append(problems, decodeTable(md, p, path, name, tables[name])...)
	}
	return f, problems
}

// decoders decode each setting's value, by Spec.Key, into f.
func (f *File) decoders(md toml.MetaData) map[string]func(toml.Primitive) error {
	return map[string]func(toml.Primitive) error{
		KeyRefreshInterval:     into(md, &f.UI, func(t *UI) **Duration { return &t.RefreshInterval }),
		KeyFoldedLists:         into(md, &f.UI, func(t *UI) **[]string { return &t.FoldedLists }),
		KeyDrawerHeight:        into(md, &f.UI, func(t *UI) **float64 { return &t.DrawerHeight }),
		KeyProjectsRoot:        into(md, &f.Projects, func(t *Projects) **string { return &t.Root }),
		KeyLinearWorkspace:     into(md, &f.Linear, func(t *Linear) **string { return &t.Workspace }),
		KeyLinearStatus:        into(md, &f.Linear, func(t *Linear) **bool { return &t.Status }),
		KeyLinearAPIKeyCommand: into(md, &f.Linear, func(t *Linear) **string { return &t.APIKeyCommand }),
		KeyGitHubEnabled:       into(md, &f.GitHub, func(t *GitHub) **bool { return &t.Enabled }),
		KeyGitHubRepos:         into(md, &f.GitHub, func(t *GitHub) **[]string { return &t.Repos }),
		KeyFigmaDesktop:        into(md, &f.Figma, func(t *Figma) **bool { return &t.Desktop }),
		KeyReuseTabs:           into(md, &f.Browser, func(t *Browser) **bool { return &t.ReuseTabs }),
		KeyUpdateCheck:         into(md, &f.Updates, func(t *Updates) **bool { return &t.Check }),
		KeyAutoRestart:         into(md, &f.Updates, func(t *Updates) **bool { return &t.AutoRestart }),
		KeyEditorCommand:       into(md, &f.Editor, func(t *Program) **string { return &t.Command }),
		KeyEditorTerminal:      into(md, &f.Editor, func(t *Program) **bool { return &t.Terminal }),
		KeyDiffCommand:         into(md, &f.Diff, func(t *Diff) **string { return &t.Command }),
		KeyDiffTerminal:        into(md, &f.Diff, func(t *Diff) **bool { return &t.Terminal }),
		KeyDiffView:            into(md, &f.Diff, func(t *Diff) **string { return &t.View }),
		KeyDiffLayout:          into(md, &f.Diff, func(t *Diff) **string { return &t.Layout }),
		KeyArchEnabled:         into(md, &f.Arch, func(t *Arch) **bool { return &t.Enabled }),
		KeyArchTests:           into(md, &f.Arch, func(t *Arch) **bool { return &t.Tests }),
	}
}

// into decodes a value into a fresh V and, only when that works, points
// the field of the table *t that field picks at it, creating the table
// first.
func into[T, V any](md toml.MetaData, t **T, field func(*T) **V) func(toml.Primitive) error {
	return func(p toml.Primitive) error {
		var v V
		if err := md.PrimitiveDecode(p, &v); err != nil {
			return err
		}
		if *t == nil {
			*t = new(T)
		}
		*field(*t) = &v
		return nil
	}
}

// decodeTable decodes the table p, called name, one key at a time with
// fields, and returns a problem for each key that is unknown or does not
// decode, and for a name that is not a table.
func decodeTable(md toml.MetaData, p toml.Primitive, path, name string, fields map[string]func(toml.Primitive) error) []string {
	// A dotted ui.key has no type of its own for ui, so look at the value.
	var v any
	if err := md.PrimitiveDecode(p, &v); err != nil {
		return []string{fmt.Sprintf("config: %s: %s: %s; ignored", path, name, valueErr(err))}
	}
	if _, ok := v.(map[string]any); !ok {
		return []string{fmt.Sprintf("config: %s: %s is not a table; ignored", path, name)}
	}
	var sub map[string]toml.Primitive
	if err := md.PrimitiveDecode(p, &sub); err != nil {
		return []string{fmt.Sprintf("config: %s: %s: %s; ignored", path, name, valueErr(err))}
	}
	var problems []string
	for _, k := range slices.Sorted(maps.Keys(sub)) {
		key := name + "." + k
		dec, ok := fields[k]
		if !ok {
			problems = append(problems, fmt.Sprintf("config: %s: unknown key %q, ignored", path, key))
			continue
		}
		if err := dec(sub[k]); err != nil {
			problems = append(problems, fmt.Sprintf("config: %s: %s: %s; ignored", path, key, valueErr(err)))
		}
	}
	return problems
}

// Flags are the command-line values.
type Flags struct {
	Config string
	// Values are the setting flags given, by Spec.Key. Register fills it
	// from a flag.FlagSet.
	Values map[string]FlagValue
}

// FlagValue is one setting flag as given: the flag's name, with its
// dashes, and its text ("true" for a bare switch).
type FlagValue struct {
	Name, Text string
}

// Set records that the flag name gave key the value text. An empty name
// is the setting's own flag.
func (fl *Flags) Set(key, name, text string) {
	if name == "" {
		sp, _ := SpecFor(key)
		name = sp.Flag
	}
	if fl.Values == nil {
		fl.Values = map[string]FlagValue{}
	}
	fl.Values[key] = FlagValue{Name: name, Text: text}
}

// Settings are the resolved values the deck runs with.
type Settings struct {
	// Path is the config file read, or "" when none could be located.
	Path            string
	LinearWorkspace string
	RefreshInterval time.Duration
	// ProjectsRoot is the herdr-projects root; "" when no home is known.
	ProjectsRoot string
	// Editor opens a worktree ({path}); Diff shows a worktree's changes
	// ({path}, {base}, optional {file}).
	Editor launch.Command
	Diff   launch.Command
	// DiffView is the Files section's view when the deck starts:
	// DiffViewList or DiffViewTree.
	DiffView string
	// DiffLayout is the diff preview's layout when the deck starts:
	// DiffLayoutUnified or DiffLayoutSplit.
	DiffLayout string
	// ReuseTabs opens a web link by focusing a browser tab that already
	// shows it, where the browser allows (launch.Browser).
	ReuseTabs bool
	// FigmaDesktop opens Figma links in the Figma desktop app: the deck's
	// own, and those herdr hands the plugin (a Ctrl+click, the open-link
	// action).
	FigmaDesktop bool
	// UpdateCheck shows "update available" in the header; AutoRestart
	// restarts the deck when its binary is replaced.
	UpdateCheck bool
	AutoRestart bool
	// LinearStatus shows each Linear issue's status next to its ID.
	LinearStatus bool
	// GitHubEnabled has the deck read thread PRs from GitHub through gh
	// (internal/source/github), beyond herdr-projects' ticker.
	GitHubEnabled bool
	// GitHubRepos are the repos, "owner/name", the GitHub and Review
	// views list open PRs from; empty lists them from every repo.
	GitHubRepos []string
	// FoldedLists are the headings of the lists that start folded, matched
	// without regard to case. It is never nil; empty folds none.
	FoldedLists []string
	// DrawerHeight is the drawer's default share of the pane below the
	// header, MinDrawerHeight to MaxDrawerHeight. A dragged height, kept in
	// the state folder (DrawerFile), wins over it.
	DrawerHeight float64
	// ArchEnabled shows the Impact tab (internal/source/arch); ArchTests
	// counts test files in its package graphs.
	ArchEnabled bool
	ArchTests   bool
	// LinearAPIKeyCommand is the argv of the command that prints the Linear
	// API key, or nil. $LINEAR_API_KEY wins over it (internal/source/linear).
	LinearAPIKeyCommand []string
	// Problems are one line each about the file or a bad value that was
	// skipped. They never stop the deck.
	Problems []string
	// Notes are one line each about earlier names the file still uses,
	// for the Sources view. They need no action until the next save.
	Notes []string
	// Values are each setting's effective value as text and where it came
	// from, keyed by Spec.Key, for the settings page.
	Values map[string]Value
}

// Resolve reads the config file and settles every setting: flag > env >
// file > default. A bad flag value is an error, since the user just typed
// it; a bad environment or file value is a problem and the next source is
// used. lookPath finds a program on $PATH (exec.LookPath); it picks the
// default diff tool.
func Resolve(fl Flags, getenv func(string) string, lookPath func(string) (string, error)) (Settings, error) {
	s := Settings{Values: map[string]Value{}}
	var named bool
	s.Path, named = Path(fl.Config, getenv)
	f, problems := Load(s.Path, named)
	s.Problems = problems
	r := resolver{s: &s, fl: fl, f: f, getenv: getenv}
	var err error

	if s.RefreshInterval, err = resolve(r, KeyRefreshInterval, reader[time.Duration]{
		parse: func(t string) (time.Duration, string, error) {
			d, err := parseInterval(strings.TrimSpace(t))
			return d, d.String(), err
		},
		file: func() (time.Duration, string, bool) {
			if f.UI == nil || f.UI.RefreshInterval == nil {
				return 0, "", false
			}
			d := f.UI.RefreshInterval.Duration
			if err := checkInterval(d); err != nil {
				r.problem(KeyRefreshInterval, fmt.Sprintf("%v; using %v", err, DefaultRefreshInterval))
				return 0, "", false
			}
			return d, d.String(), true
		},
		def: func() (time.Duration, string) { return DefaultRefreshInterval, DefaultRefreshInterval.String() },
	}); err != nil {
		return s, err
	}

	if s.FoldedLists, err = resolve(r, KeyFoldedLists, reader[[]string]{
		parse: func(t string) ([]string, string, error) {
			l, err := ParseList(t)
			return l, ListText(l), err
		},
		file: func() ([]string, string, bool) {
			if f.UI == nil || f.UI.FoldedLists == nil {
				return nil, "", false
			}
			l, err := cleanList(*f.UI.FoldedLists)
			if err != nil {
				r.problem(KeyFoldedLists, err.Error()+"; ignored")
				return nil, "", false
			}
			return l, ListText(l), true
		},
		def: func() ([]string, string) {
			return append([]string{}, DefaultFoldedLists...), ListText(DefaultFoldedLists)
		},
	}); err != nil {
		return s, err
	}

	if s.DrawerHeight, err = resolve(r, KeyDrawerHeight, reader[float64]{
		parse: func(t string) (float64, string, error) {
			v, err := parseFraction(strings.TrimSpace(t))
			return v, FractionText(v), err
		},
		file: func() (float64, string, bool) {
			if f.UI == nil || f.UI.DrawerHeight == nil {
				return 0, "", false
			}
			v := *f.UI.DrawerHeight
			if err := checkFraction(v); err != nil {
				r.problem(KeyDrawerHeight, fmt.Sprintf("%v; using %v", err, FractionText(DefaultDrawerHeight)))
				return 0, "", false
			}
			return v, FractionText(v), true
		},
		def: func() (float64, string) { return DefaultDrawerHeight, FractionText(DefaultDrawerHeight) },
	}); err != nil {
		return s, err
	}

	if s.ProjectsRoot, err = resolve(r, KeyProjectsRoot, reader[string]{
		parse: func(t string) (string, string, error) {
			// A relative path is relative to the working directory.
			p, err := filepath.Abs(expandHome(strings.TrimSpace(t), getenv))
			return p, p, err
		},
		file: func() (string, string, bool) {
			if f.Projects == nil || f.Projects.Root == nil {
				return "", "", false
			}
			if p := expandHome(strings.TrimSpace(*f.Projects.Root), getenv); filepath.IsAbs(p) {
				return p, p, true
			}
			r.problem(KeyProjectsRoot, fmt.Sprintf("%q is not an absolute path; ignored", *f.Projects.Root))
			return "", "", false
		},
		def: func() (string, string) {
			if home := homeDir(getenv); home != "" {
				p := filepath.Join(home, ".herdr-projects")
				return p, p
			}
			return "", ""
		},
	}); err != nil {
		return s, err
	}
	if s.LinearWorkspace, err = resolve(r, KeyLinearWorkspace, reader[string]{
		parse: func(t string) (string, string, error) { return t, t, nil },
		file: func() (string, string, bool) {
			if f.Linear == nil || f.Linear.Workspace == nil {
				return "", "", false
			}
			w := strings.TrimSpace(*f.Linear.Workspace)
			return w, w, true
		},
		def: func() (string, string) { return "", "" },
	}); err != nil {
		return s, err
	}
	if s.LinearStatus, err = resolveBool(r, KeyLinearStatus, func(f File) *bool {
		if f.Linear == nil {
			return nil
		}
		return f.Linear.Status
	}, true); err != nil {
		return s, err
	}
	// The command takes no placeholders and runs as written, without a
	// shell. Problems never quote it, in case a key was pasted there.
	if s.LinearAPIKeyCommand, err = resolve(r, KeyLinearAPIKeyCommand, reader[[]string]{
		parse: func(t string) ([]string, string, error) {
			argv, err := launch.ParseCommand(t)
			if err != nil {
				return nil, "", errors.New("the command does not parse")
			}
			return argv, strings.TrimSpace(t), nil
		},
		file: func() ([]string, string, bool) {
			if f.Linear == nil || f.Linear.APIKeyCommand == nil || strings.TrimSpace(*f.Linear.APIKeyCommand) == "" {
				return nil, "", false
			}
			argv, err := launch.ParseCommand(*f.Linear.APIKeyCommand)
			if err != nil {
				r.problem(KeyLinearAPIKeyCommand, "the command does not parse; ignored")
				return nil, "", false
			}
			return argv, strings.TrimSpace(*f.Linear.APIKeyCommand), true
		},
		def: func() ([]string, string) { return nil, "" },
	}); err != nil {
		return s, err
	}
	if getenv(EnvLinearAPIKey) != "" {
		v := s.Values[KeyLinearAPIKeyCommand]
		v.Note = "$" + EnvLinearAPIKey + " is set and wins over the command"
		s.Values[KeyLinearAPIKeyCommand] = v
	}

	if s.GitHubEnabled, err = resolveBool(r, KeyGitHubEnabled, func(f File) *bool {
		if f.GitHub == nil {
			return nil
		}
		return f.GitHub.Enabled
	}, true); err != nil {
		return s, err
	}
	if s.GitHubRepos, err = resolve(r, KeyGitHubRepos, reader[[]string]{
		parse: func(t string) ([]string, string, error) {
			l, err := parseRepos(t)
			return l, ListText(l), err
		},
		file: func() ([]string, string, bool) {
			if f.GitHub == nil || f.GitHub.Repos == nil {
				return nil, "", false
			}
			l, err := cleanList(*f.GitHub.Repos)
			if err == nil {
				err = checkRepos(l)
			}
			if err != nil {
				r.problem(KeyGitHubRepos, err.Error()+"; ignored")
				return nil, "", false
			}
			return l, ListText(l), true
		},
		def: func() ([]string, string) { return []string{}, NoLists },
	}); err != nil {
		return s, err
	}
	if s.FigmaDesktop, err = resolveBool(r, KeyFigmaDesktop, func(f File) *bool {
		if f.Figma == nil {
			return nil
		}
		return f.Figma.Desktop
	}, false); err != nil {
		return s, err
	}
	if s.ReuseTabs, err = resolveBool(r, KeyReuseTabs, func(f File) *bool {
		if f.Browser == nil {
			return nil
		}
		return f.Browser.ReuseTabs
	}, true); err != nil {
		return s, err
	}
	if s.UpdateCheck, err = resolveBool(r, KeyUpdateCheck, func(f File) *bool {
		if f.Updates == nil {
			return nil
		}
		return f.Updates.Check
	}, true); err != nil {
		return s, err
	}
	if s.AutoRestart, err = resolveBool(r, KeyAutoRestart, func(f File) *bool {
		if f.Updates == nil {
			return nil
		}
		return f.Updates.AutoRestart
	}, true); err != nil {
		return s, err
	}

	if s.Editor, err = r.program(KeyEditorCommand, KeyEditorTerminal, f.Editor, EditorPlaceholders, DefaultEditor, false); err != nil {
		return s, err
	}
	def := DefaultDiffTool
	if _, err := lookPath("hunk"); err != nil {
		def = FallbackDiffTool
	}
	var diff *Program
	if f.Diff != nil {
		diff = &f.Diff.Program
	}
	if s.Diff, err = r.program(KeyDiffCommand, KeyDiffTerminal, diff, DiffPlaceholders, def, true); err != nil {
		return s, err
	}
	if s.DiffView, err = resolveChoice(r, KeyDiffView, DiffViews, func() *string {
		if f.Diff == nil {
			return nil
		}
		return f.Diff.View
	}); err != nil {
		return s, err
	}
	if s.DiffLayout, err = resolveChoice(r, KeyDiffLayout, DiffLayouts, func() *string {
		if f.Diff == nil {
			return nil
		}
		return f.Diff.Layout
	}); err != nil {
		return s, err
	}

	if s.ArchEnabled, err = resolveBool(r, KeyArchEnabled, func(f File) *bool {
		if f.Arch == nil {
			return nil
		}
		return f.Arch.Enabled
	}, true); err != nil {
		return s, err
	}
	if s.ArchTests, err = resolveBool(r, KeyArchTests, func(f File) *bool {
		if f.Arch == nil {
			return nil
		}
		return f.Arch.Tests
	}, false); err != nil {
		return s, err
	}

	for k, text := range f.values() {
		v := s.Values[k]
		v.File, v.InFile = text, true
		s.Values[k] = v
	}
	if f.UI != nil && f.UI.FoldedLists != nil && !listFitsText(*f.UI.FoldedLists) {
		v := s.Values[KeyFoldedLists]
		v.HandEdit = true
		s.Values[KeyFoldedLists] = v
	}
	for _, sp := range Specs {
		old, ok := f.Old[sp.Key]
		if !ok {
			continue
		}
		v := s.Values[sp.Key]
		v.Old, v.Shadowed = old, f.Shadowed[sp.Key]
		s.Values[sp.Key] = v
		if v.Shadowed {
			s.Notes = append(s.Notes, fmt.Sprintf("config: %s is set twice, as %s and the earlier %s; %s wins", sp.Key, sp.Key, old, sp.Key))
		} else {
			s.Notes = append(s.Notes, fmt.Sprintf("config: %s is renamed to %s; saving it on the settings page, or `herdr-deck config migrate`, moves it", old, sp.Key))
		}
	}
	// The Sources view is narrow: show the file as ~/… where it fits.
	if home := homeDir(getenv); home != "" && strings.HasPrefix(s.Path, home+string(filepath.Separator)) {
		short := "~" + strings.TrimPrefix(s.Path, home)
		for i, p := range s.Problems {
			s.Problems[i] = strings.ReplaceAll(p, s.Path, short)
		}
	}
	return s, nil
}

// resolver is what settling one setting needs.
type resolver struct {
	s      *Settings
	fl     Flags
	f      File
	getenv func(string) string
}

// reader reads one setting's value from each source. parse reads a flag
// or environment value, returning the value and its text; file returns the
// file's value when it is set and valid, reporting a bad one itself; def
// is the default.
type reader[T any] struct {
	parse func(string) (T, string, error)
	file  func() (T, string, bool)
	def   func() (T, string)
}

// resolve settles the setting key: flag > env > file > default. A bad
// flag value is an error; a bad environment value is a problem and the
// next source is used.
func resolve[T any](r resolver, key string, rd reader[T]) (T, error) {
	sp, _ := SpecFor(key)
	if fv, ok := r.fl.Values[key]; ok {
		v, text, err := rd.parse(fv.Text)
		if err != nil {
			return v, fmt.Errorf("%s: %w", fv.Name, err)
		}
		r.note(sp, text, FromFlag, fv.Name)
		return v, nil
	}
	if name, e := envOf(sp, r.getenv); e != "" {
		v, text, err := rd.parse(e)
		if err == nil {
			r.note(sp, text, FromEnv, name)
			return v, nil
		}
		r.s.Problems = append(r.s.Problems, fmt.Sprintf("$%s: %v; ignored", name, err))
	}
	if v, text, ok := rd.file(); ok {
		r.note(sp, text, FromFile, "")
		return v, nil
	}
	v, text := rd.def()
	r.note(sp, text, FromDefault, "")
	return v, nil
}

// resolveBool settles an on/off setting whose file value file picks.
func resolveBool(r resolver, key string, file func(File) *bool, def bool) (bool, error) {
	return resolve(r, key, reader[bool]{
		parse: parseBool,
		file: func() (bool, string, bool) {
			if b := file(r.f); b != nil {
				return *b, strconv.FormatBool(*b), true
			}
			return false, "", false
		},
		def: func() (bool, string) { return def, strconv.FormatBool(def) },
	})
}

// resolveChoice settles a setting that is one of choices, the first being
// its default; file gives the file's value, nil when it has none.
func resolveChoice(r resolver, key string, choices []string, file func() *string) (string, error) {
	return resolve(r, key, reader[string]{
		parse: func(t string) (string, string, error) {
			t = strings.TrimSpace(t)
			return t, t, checkChoice(t, choices)
		},
		file: func() (string, string, bool) {
			p := file()
			if p == nil {
				return "", "", false
			}
			v := strings.TrimSpace(*p)
			if err := checkChoice(v, choices); err != nil {
				r.problem(key, err.Error()+"; ignored")
				return "", "", false
			}
			return v, v, true
		},
		def: func() (string, string) { return choices[0], choices[0] },
	})
}

func parseBool(t string) (bool, string, error) {
	b, err := strconv.ParseBool(strings.TrimSpace(t))
	if err != nil {
		return false, "", fmt.Errorf("%q is not true or false", t)
	}
	return b, strconv.FormatBool(b), nil
}

// envOf returns the first of sp's environment variables that is set, the
// current name before its aliases, and its value.
func envOf(sp Spec, getenv func(string) string) (name, value string) {
	if sp.Env == "" {
		return "", ""
	}
	for _, n := range append([]string{sp.Env}, sp.EnvAliases...) {
		if v := getenv(n); v != "" {
			return n, v
		}
	}
	return "", ""
}

// note records a setting's effective value and source for the settings
// page; name is the flag or environment variable it came from.
func (r resolver) note(sp Spec, text string, from Source, name string) {
	v := Value{Text: text, Source: from}
	if name != "" && name != sp.Flag && name != sp.Env {
		v.Via = name
	}
	r.s.Values[sp.Key] = v
}

// problem reports a bad file value of key, under the name the file uses.
func (r resolver) problem(key, msg string) {
	name := key
	if old, ok := r.f.Old[key]; ok && !r.f.Shadowed[key] {
		name = old
	}
	r.s.Problems = append(r.s.Problems, fmt.Sprintf("config: %s: %s: %s", r.s.Path, name, msg))
}

// program settles a command and its terminal option as one setting: the
// highest source with a valid command wins, and terminal comes from that
// source or a higher one, else the default. So --editor-command "zed
// {path}" is not opened in a pane because the file said terminal = true
// for nvim.
func (r resolver) program(cmdKey, termKey string, file *Program, allowed []string, def string, defTerm bool) (launch.Command, error) {
	cmdSpec, _ := SpecFor(cmdKey)
	termSpec, _ := SpecFor(termKey)
	var term *bool
	termFrom, termVia := FromDefault, ""
	setTerm := func(b *bool, from Source, via string) {
		if term == nil && b != nil {
			term, termFrom, termVia = b, from, via
		}
	}
	pick := func(argv []string, text string, from Source, via string) launch.Command {
		c := launch.Command{Argv: argv, Terminal: defTerm}
		if term != nil {
			c.Terminal = *term
		}
		r.note(cmdSpec, strings.TrimSpace(text), from, via)
		r.note(termSpec, strconv.FormatBool(c.Terminal), termFrom, termVia)
		return c
	}

	if fv, ok := r.fl.Values[termKey]; ok {
		b, _, err := parseBool(fv.Text)
		if err != nil {
			return launch.Command{}, fmt.Errorf("%s: %w", fv.Name, err)
		}
		setTerm(&b, FromFlag, fv.Name)
	}
	if fv, ok := r.fl.Values[cmdKey]; ok {
		argv, err := launch.ParseCommand(fv.Text, allowed...)
		if err != nil {
			return launch.Command{}, fmt.Errorf("%s: %w", fv.Name, err)
		}
		return pick(argv, fv.Text, FromFlag, fv.Name), nil
	}

	if name, v := envOf(termSpec, r.getenv); v != "" {
		if b, _, err := parseBool(v); err == nil {
			setTerm(&b, FromEnv, name)
		} else {
			r.s.Problems = append(r.s.Problems, fmt.Sprintf("$%s: %v; ignored", name, err))
		}
	}
	if name, v := envOf(cmdSpec, r.getenv); v != "" {
		argv, err := launch.ParseCommand(v, allowed...)
		if err == nil {
			return pick(argv, v, FromEnv, name), nil
		}
		r.s.Problems = append(r.s.Problems, fmt.Sprintf("$%s: %v; ignored", name, err))
	}

	if file != nil {
		setTerm(file.Terminal, FromFile, "")
		if file.Command != nil {
			argv, err := launch.ParseCommand(*file.Command, allowed...)
			if err == nil {
				return pick(argv, *file.Command, FromFile, ""), nil
			}
			r.problem(cmdKey, err.Error()+"; ignored")
		}
	}

	argv, err := launch.ParseCommand(def, allowed...)
	if err != nil {
		panic("config: bad default " + cmdKey + ": " + err.Error())
	}
	return pick(argv, def, FromDefault, ""), nil
}

// ParseList reads comma-separated names such as "Backlog, Resolved", or
// NoLists for none. It never returns nil.
func ParseList(text string) ([]string, error) {
	text = strings.TrimSpace(text)
	if strings.EqualFold(text, NoLists) {
		return []string{}, nil
	}
	l, err := cleanList(strings.Split(text, ","))
	if err == nil && len(l) == 0 {
		err = fmt.Errorf("%q names no list; %s folds none", text, NoLists)
	}
	return l, err
}

// cleanList trims each name and drops repeats, ignoring case. An empty
// name is an error.
func cleanList(names []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, n := range names {
		n = strings.TrimSpace(n)
		if n == "" {
			return nil, errors.New("a list name is empty")
		}
		if k := strings.ToLower(n); !seen[k] {
			seen[k] = true
			out = append(out, n)
		}
	}
	return out, nil
}

// listFitsText says whether ParseList(ListText(l)) gives l back: no name
// holds a comma or is NoLists.
func listFitsText(l []string) bool {
	for _, n := range l {
		if strings.Contains(n, ",") || (len(l) == 1 && strings.EqualFold(strings.TrimSpace(n), NoLists)) {
			return false
		}
	}
	return true
}

// parseRepos reads github.repos as ParseList does, each name an
// "owner/name".
func parseRepos(text string) ([]string, error) {
	l, err := ParseList(text)
	if err == nil {
		err = checkRepos(l)
	}
	return l, err
}

// checkRepos says which name is not a repo's "owner/name".
func checkRepos(l []string) error {
	for _, n := range l {
		if !deck.ValidRepo(n) {
			return fmt.Errorf("%q is not a repo such as owner/name", n)
		}
	}
	return nil
}

// ListText is a list as ParseList reads it.
func ListText(l []string) string {
	if len(l) == 0 {
		return NoLists
	}
	return strings.Join(l, ", ")
}

func checkChoice(v string, choices []string) error {
	for _, c := range choices {
		if v == c {
			return nil
		}
	}
	return fmt.Errorf("%q is not %s", v, strings.Join(choices, " or "))
}

func parseInterval(s string) (time.Duration, error) {
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("%q is not a duration such as \"5s\"", s)
	}
	return d, checkInterval(d)
}

// parseFraction reads a drawer height such as 0.5.
func parseFraction(s string) (float64, error) {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("%q is not a number such as 0.5", s)
	}
	return v, checkFraction(v)
}

func checkFraction(v float64) error {
	if math.IsNaN(v) {
		return errors.New("NaN is not a number such as 0.5")
	}
	if v < MinDrawerHeight || v > MaxDrawerHeight {
		return fmt.Errorf("%s is outside %s–%s", FractionText(v), FractionText(MinDrawerHeight), FractionText(MaxDrawerHeight))
	}
	return nil
}

// FractionText writes a drawer height as the file holds it: 0.5, 0.35.
func FractionText(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func checkInterval(d time.Duration) error {
	if d < MinRefreshInterval || d > MaxRefreshInterval {
		return fmt.Errorf("%v is outside %v–%v", d, MinRefreshInterval, MaxRefreshInterval)
	}
	return nil
}

// homeDir is $HOME, else the OS's idea of it, else "".
func homeDir(getenv func(string) string) string {
	if h := getenv("HOME"); h != "" {
		return h
	}
	h, _ := os.UserHomeDir()
	return h
}

// expandHome turns a leading "~/" (or a lone "~") into the home folder.
func expandHome(p string, getenv func(string) string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home := homeDir(getenv)
	if home == "" {
		return p
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~"))
}

// shortErr drops the operation from a *PathError so the line reads
// "<path>: <reason>".
func shortErr(path string, err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return path + ": " + pe.Err.Error()
	}
	return err.Error()
}

// valueErr is why one value did not decode, without the library's prefix.
func valueErr(err error) string {
	msg := err.Error()
	if i := strings.LastIndex(msg, ": "); i >= 0 && strings.HasPrefix(msg, "toml:") {
		msg = msg[i+2:]
	}
	return msg
}

// parseErr is a TOML error on one line, with its position.
func parseErr(err error) string {
	var pe toml.ParseError
	if errors.As(err, &pe) {
		return fmt.Sprintf("line %d: %s", pe.Position.Line, pe.Message)
	}
	return strings.ReplaceAll(err.Error(), "\n", " ")
}
