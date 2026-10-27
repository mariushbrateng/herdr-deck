package config

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/FredricW/herdr-deck/internal/launch"
)

// Source is where a setting's effective value comes from.
type Source int

const (
	FromDefault Source = iota
	FromFile
	FromEnv
	FromFlag
)

func (s Source) String() string {
	switch s {
	case FromFile:
		return "file"
	case FromEnv:
		return "env"
	case FromFlag:
		return "flag"
	}
	return "default"
}

// Value is a setting's effective value as text and where it came from.
type Value struct {
	// Text is the value as the file would hold it: "true", "5s", a
	// command line. "" is unset (no default).
	Text   string
	Source Source
	// Note says what else decides the setting, such as $LINEAR_API_KEY
	// winning over the key command.
	Note string
	// File is the value the config file holds, valid or not, when InFile;
	// a flag or env var may hide it.
	File   string
	InFile bool
	// HandEdit says the file's value cannot be edited as text without
	// changing it, such as a list name with a comma; edit the file.
	HandEdit bool
	// Via is the flag or environment variable the value came from when it
	// is an alias (an earlier name), else "".
	Via string
	// Old is the earlier flat key the file sets this setting under, such
	// as linear_workspace; saving moves it into its table. Shadowed says
	// the file sets both, and the table's value wins.
	Old      string
	Shadowed bool
}

// values are the values the file sets, as text, by Spec.Key: as the file
// holds them, even when they do not validate.
func (f File) values() map[string]string {
	out := map[string]string{}
	str := func(k string, v *string) {
		if v != nil {
			out[k] = strings.TrimSpace(*v)
		}
	}
	boolean := func(k string, v *bool) {
		if v != nil {
			out[k] = strconv.FormatBool(*v)
		}
	}
	if t := f.UI; t != nil {
		if t.RefreshInterval != nil {
			out[KeyRefreshInterval] = t.RefreshInterval.String()
		}
		if t.FoldedLists != nil {
			out[KeyFoldedLists] = ListText(*t.FoldedLists)
		}
		if t.DrawerHeight != nil {
			out[KeyDrawerHeight] = FractionText(*t.DrawerHeight)
		}
	}
	if t := f.Projects; t != nil {
		str(KeyProjectsRoot, t.Root)
	}
	if t := f.Linear; t != nil {
		str(KeyLinearWorkspace, t.Workspace)
		boolean(KeyLinearStatus, t.Status)
		str(KeyLinearAPIKeyCommand, t.APIKeyCommand)
	}
	if t := f.GitHub; t != nil {
		boolean(KeyGitHubEnabled, t.Enabled)
		if t.Repos != nil {
			out[KeyGitHubRepos] = ListText(*t.Repos)
		}
	}
	if t := f.Figma; t != nil {
		boolean(KeyFigmaDesktop, t.Desktop)
	}
	if t := f.Browser; t != nil {
		boolean(KeyReuseTabs, t.ReuseTabs)
	}
	if t := f.Updates; t != nil {
		boolean(KeyUpdateCheck, t.Check)
		boolean(KeyAutoRestart, t.AutoRestart)
	}
	if t := f.Editor; t != nil {
		str(KeyEditorCommand, t.Command)
		boolean(KeyEditorTerminal, t.Terminal)
	}
	if t := f.Diff; t != nil {
		str(KeyDiffCommand, t.Command)
		boolean(KeyDiffTerminal, t.Terminal)
		str(KeyDiffView, t.View)
		str(KeyDiffLayout, t.Layout)
	}
	if t := f.Arch; t != nil {
		boolean(KeyArchEnabled, t.Enabled)
		boolean(KeyArchTests, t.Tests)
	}
	return out
}

// Kind is how a setting is written and edited.
type Kind int

const (
	KindBool     Kind = iota // true or false; the page toggles it
	KindWord                 // a word such as a workspace slug
	KindPath                 // an absolute path; ~/ is the home folder
	KindDuration             // a Go duration such as "5s"
	KindCommand              // a command line, split without a shell
	KindChoice               // one of Spec.Choices; the page cycles them
	KindList                 // names, written as a TOML array; edited comma-separated
	KindFraction             // a share such as 0.5, written as a TOML float
)

// Setting keys, as Spec.Key and Settings.Values name them: the dotted
// path of the key in the file, table.key.
const (
	KeyRefreshInterval     = "ui.refresh_interval"
	KeyFoldedLists         = "ui.folded_lists"
	KeyDrawerHeight        = "ui.drawer_height"
	KeyProjectsRoot        = "projects.root"
	KeyLinearWorkspace     = "linear.workspace"
	KeyLinearStatus        = "linear.status"
	KeyLinearAPIKeyCommand = "linear.api_key_command"
	KeyGitHubEnabled       = "github.enabled"
	KeyGitHubRepos         = "github.repos"
	KeyFigmaDesktop        = "figma.desktop"
	KeyReuseTabs           = "browser.reuse_tabs"
	KeyUpdateCheck         = "updates.check"
	KeyAutoRestart         = "updates.auto_restart"
	KeyEditorCommand       = "editor.command"
	KeyEditorTerminal      = "editor.terminal"
	KeyDiffCommand         = "diff.command"
	KeyDiffTerminal        = "diff.terminal"
	KeyDiffView            = "diff.view"
	KeyDiffLayout          = "diff.layout"
	KeyArchEnabled         = "arch.enabled"
	KeyArchTests           = "arch.tests"
)

// Spec describes one setting: its names everywhere and how the settings
// page edits it.
type Spec struct {
	// Key is the setting's dotted path in the file, such as
	// linear.workspace: its one name in docs, notes and errors.
	Key  string
	Kind Kind
	// Flag and Env override the file. Flag is --<table>-<key> and Env is
	// HERDR_DECK_<TABLE>_<KEY>, except where another tool owns the name.
	Flag string
	Env  string
	// FlagAliases, EnvAliases and Old are earlier names that still work:
	// hidden flags, environment variables, and a flat key in the file that
	// saving moves into the table.
	FlagAliases []string
	EnvAliases  []string
	Old         string
	// Default is the default as the docs and flag help show it.
	Default string
	// Help is one line about the setting.
	Help string
	// Placeholders a command may use.
	Placeholders []string
	// Choices are a KindChoice setting's values, the default first.
	Choices []string
	// Restart says a running deck only picks a change up when it starts.
	Restart bool
}

// Table is the TOML table the setting sits in.
func (sp Spec) Table() string {
	t, _ := splitKey(sp.Key)
	return t
}

// Tables are the file's tables in the settings page's order, with a few
// words about each for its heading.
var Tables = []struct{ Name, Title string }{
	{"ui", "the list, the drawer and how often it reloads"},
	{"projects", "herdr-projects"},
	{"linear", "Linear links and status"},
	{"github", "pull requests"},
	{"figma", "Figma links"},
	{"browser", "web links"},
	{"updates", "new versions of the deck"},
	{"editor", "what e opens"},
	{"diff", "what d opens, and the Files tab"},
	{"arch", "the Impact tab: what a branch did to the shape"},
}

// Specs are every setting, by table, in the settings page's order.
var Specs = []Spec{
	{Key: KeyRefreshInterval, Kind: KindDuration, Flag: "--ui-refresh-interval", Env: EnvRefreshInterval,
		FlagAliases: []string{"--refresh-interval"}, EnvAliases: []string{"HERDR_DECK_REFRESH_INTERVAL"}, Old: "refresh_interval",
		Default: DefaultRefreshInterval.String(),
		Help:    "how often the deck reloads when no file change says to; 1s to 10m"},
	{Key: KeyFoldedLists, Kind: KindList, Flag: "--ui-folded-lists", Env: EnvFoldedLists,
		Default: ListText(DefaultFoldedLists),
		Help:    "list headings that start folded, any case, comma-separated; " + NoLists + " folds none. The deck's own groups are Resolved and Other threads"},
	{Key: KeyDrawerHeight, Kind: KindFraction, Flag: "--ui-drawer-height", Env: EnvDrawerHeight,
		Default: FractionText(DefaultDrawerHeight),
		Help:    "the drawer's share of the pane, " + FractionText(MinDrawerHeight) + " to " + FractionText(MaxDrawerHeight) + ", until you drag the rule above it or press + -; the dragged height is kept"},
	{Key: KeyProjectsRoot, Kind: KindPath, Flag: "--projects-root", Env: EnvProjectsRoot, Old: "projects_root", Restart: true,
		Default: "~/.herdr-projects",
		Help:    "the herdr-projects root; ~/ is your home folder"},
	{Key: KeyLinearWorkspace, Kind: KindWord, Flag: "--linear-workspace", Env: EnvLinearWorkspace, Old: "linear_workspace",
		Default: "none",
		Help:    "Linear workspace that bare IDs such as ABC-123 link into; not needed with a Linear API key"},
	{Key: KeyLinearStatus, Kind: KindBool, Flag: "--linear-status", Env: EnvLinearStatus, Old: "linear_status",
		Default: "true",
		Help:    "show each Linear issue's state next to its ID (needs an API key)"},
	{Key: KeyLinearAPIKeyCommand, Kind: KindCommand, Flag: "--linear-api-key-command", Env: EnvLinearAPIKeyCommand, Old: "linear_api_key_command",
		Default: "none",
		Help:    "a command that prints the Linear API key, such as op read …; never the key itself. $" + EnvLinearAPIKey + " wins over it"},
	{Key: KeyGitHubEnabled, Kind: KindBool, Flag: "--github-enabled", Env: EnvGitHubEnabled,
		Default: "true",
		Help:    "read thread PRs from GitHub through gh: why a PR is not merging, failing check logs, review threads"},
	{Key: KeyGitHubRepos, Kind: KindList, Flag: "--github-repos", Env: EnvGitHubRepos,
		Default: NoLists,
		Help:    "repos the GitHub and Review views list your open PRs from, owner/name, comma-separated; " + NoLists + " lists them from every repo"},
	{Key: KeyFigmaDesktop, Kind: KindBool, Flag: "--figma-desktop", Env: EnvFigmaDesktop, Old: "figma_desktop",
		Default: "false",
		Help:    "open Figma links in the Figma desktop app instead of the browser"},
	{Key: KeyReuseTabs, Kind: KindBool, Flag: "--browser-reuse-tabs", Env: EnvReuseTabs,
		FlagAliases: []string{"--reuse-browser-tabs"}, EnvAliases: []string{"HERDR_DECK_REUSE_BROWSER_TABS"}, Old: "reuse_browser_tabs",
		Default: "true",
		Help:    "open a web link in a browser tab that already shows it (macOS)"},
	{Key: KeyUpdateCheck, Kind: KindBool, Flag: "--updates-check", Env: EnvUpdateCheck,
		FlagAliases: []string{"--update-check"}, EnvAliases: []string{"HERDR_DECK_UPDATE_CHECK"}, Old: "update_check",
		Default: "true",
		Help:    "show ↑ <version> in the header when a newer deck exists"},
	{Key: KeyAutoRestart, Kind: KindBool, Flag: "--updates-auto-restart", Env: EnvAutoRestart,
		FlagAliases: []string{"--auto-restart"}, EnvAliases: []string{"HERDR_DECK_AUTO_RESTART"}, Old: "auto_restart",
		Default: "true",
		Help:    "restart the deck in place when its binary is replaced"},
	{Key: KeyEditorCommand, Kind: KindCommand, Flag: "--editor-command", Env: EnvEditorCommand, Placeholders: EditorPlaceholders,
		FlagAliases: []string{"--editor"}, EnvAliases: []string{"HERDR_DECK_EDITOR"},
		Default: DefaultEditor,
		Help:    "what e opens a worktree with; {path} is the folder"},
	{Key: KeyEditorTerminal, Kind: KindBool, Flag: "--editor-terminal", Env: EnvEditorTerminal,
		Default: "false",
		Help:    "the editor is a terminal program: open it in a new herdr pane"},
	{Key: KeyDiffCommand, Kind: KindCommand, Flag: "--diff-command", Env: EnvDiffCommand, Placeholders: DiffPlaceholders,
		FlagAliases: []string{"--diff-tool"}, EnvAliases: []string{"HERDR_DECK_DIFF_TOOL"},
		Default: DefaultDiffTool + " (with hunk), else " + FallbackDiffTool,
		Help:    "shows a worktree's changes: {path}, {base} and an optional {file}"},
	{Key: KeyDiffTerminal, Kind: KindBool, Flag: "--diff-terminal", Env: EnvDiffTerminal,
		Default: "true",
		Help:    "the diff tool is a terminal program: open it in a new herdr pane"},
	{Key: KeyDiffView, Kind: KindChoice, Flag: "--diff-view", Env: EnvDiffView, Choices: DiffViews, Old: "diff_view",
		Default: DiffViewList,
		Help:    "the Files section's view at start: list, or tree (files under their folders); d t switches"},
	{Key: KeyDiffLayout, Kind: KindChoice, Flag: "--diff-layout", Env: EnvDiffLayout, Choices: DiffLayouts,
		Default: DiffLayoutUnified,
		Help:    "the diff preview's layout at start: unified, or split (old left, new right); S switches"},
	{Key: KeyArchEnabled, Kind: KindBool, Flag: "--arch-enabled", Env: EnvArchEnabled, Restart: true,
		Default: "true",
		Help:    "show the Impact tab: the package graphs of a thread's branch and its base, compared"},
	{Key: KeyArchTests, Kind: KindBool, Flag: "--arch-tests", Env: EnvArchTests, Restart: true,
		Default: "false",
		Help:    "count test files in the Impact tab's package graphs"},
}

// SpecFor returns the spec of key.
func SpecFor(key string) (Spec, bool) {
	for _, s := range Specs {
		if s.Key == key {
			return s, true
		}
	}
	return Spec{}, false
}

// Override names the flag or environment variable v came from, the alias
// when one was used, or "" when it came from the file or the default.
func (sp Spec) Override(v Value) string {
	switch v.Source {
	case FromFlag:
		if v.Via != "" {
			return v.Via
		}
		return sp.Flag
	case FromEnv:
		if v.Via != "" {
			return "$" + v.Via
		}
		return "$" + sp.Env
	}
	return ""
}

// Check validates text as a value for key, the way Resolve reads it from
// the file.
func Check(key, text string) error {
	sp, ok := SpecFor(key)
	if !ok {
		return fmt.Errorf("unknown setting %q", key)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return fmt.Errorf("%s needs a value", key)
	}
	switch sp.Kind {
	case KindBool:
		if _, err := strconv.ParseBool(text); err != nil {
			return fmt.Errorf("%q is not true or false", text)
		}
	case KindWord:
		if strings.ContainsAny(text, " \t/") {
			return fmt.Errorf("%q is not a single word such as acme", text)
		}
	case KindPath:
		if text != "~" && !strings.HasPrefix(text, "~/") && !filepath.IsAbs(text) {
			return fmt.Errorf("%q is not an absolute path; ~/ works", text)
		}
	case KindDuration:
		if _, err := parseInterval(text); err != nil {
			return err
		}
	case KindCommand:
		if _, err := launch.ParseCommand(text, sp.Placeholders...); err != nil {
			return err
		}
	case KindChoice:
		return checkChoice(text, sp.Choices)
	case KindList:
		if _, err := ParseList(text); err != nil {
			return err
		}
		if key == KeyGitHubRepos {
			if _, err := parseRepos(text); err != nil {
				return err
			}
		}
	case KindFraction:
		if _, err := parseFraction(text); err != nil {
			return err
		}
	}
	return nil
}
