package config

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveValuesAndSources(t *testing.T) {
	getenv, home := env(t, map[string]string{
		EnvEditorCommand:   "nvim {path}",
		EnvEditorTerminal:  "true",
		EnvRefreshInterval: "soon", // bad: the file's value is used
		EnvLinearAPIKey:    "lin_api_never_shown",
	})
	write(t, filepath.Join(home, ".config", "herdr-deck", "config.toml"), `
[linear]
workspace = "acme"
api_key_command = "op read op://Vault/Linear/key"

[figma]
desktop = true

[editor]
command = "zed {path}"

[diff]
terminal = false
view = "tree"

[ui]
refresh_interval = "30s"
folded_lists = ["In progress", " backlog ", "Backlog"]
drawer_height = 0.35
`)
	no := false
	s, err := Resolve(fls(KeyUpdateCheck, &no, KeyDiffCommand, "git diff {base}"), getenv, noHunk)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Value{
		KeyLinearWorkspace:     {Text: "acme", Source: FromFile},
		KeyLinearStatus:        {Text: "true", Source: FromDefault},
		KeyLinearAPIKeyCommand: {Text: "op read op://Vault/Linear/key", Source: FromFile, Note: "$LINEAR_API_KEY is set and wins over the command"},
		KeyEditorCommand:       {Text: "nvim {path}", Source: FromEnv},
		KeyEditorTerminal:      {Text: "true", Source: FromEnv},
		// The flag gives the command, so the file's terminal is not used.
		KeyDiffCommand:     {Text: "git diff {base}", Source: FromFlag},
		KeyDiffTerminal:    {Text: "true", Source: FromDefault},
		KeyDiffView:        {Text: "tree", Source: FromFile},
		KeyDiffLayout:      {Text: "unified", Source: FromDefault},
		KeyFoldedLists:     {Text: "In progress, backlog", Source: FromFile},
		KeyUpdateCheck:     {Text: "false", Source: FromFlag},
		KeyAutoRestart:     {Text: "true", Source: FromDefault},
		KeyReuseTabs:       {Text: "true", Source: FromDefault},
		KeyGitHubEnabled:   {Text: "true", Source: FromDefault},
		KeyGitHubRepos:     {Text: "none", Source: FromDefault},
		KeyArchEnabled:     {Text: "true", Source: FromDefault},
		KeyArchTests:       {Text: "false", Source: FromDefault},
		KeyFigmaDesktop:    {Text: "true", Source: FromFile},
		KeyRefreshInterval: {Text: "30s", Source: FromFile},
		KeyDrawerHeight:    {Text: "0.35", Source: FromFile},
		KeyProjectsRoot:    {Text: filepath.Join(home, ".herdr-projects"), Source: FromDefault},
	}
	for _, sp := range Specs {
		got := s.Values[sp.Key]
		got.File, got.InFile = "", false
		if got != want[sp.Key] {
			t.Errorf("%s = %+v, want %+v", sp.Key, got, want[sp.Key])
		}
	}
	// The file's own values show even where a flag or env var hides them.
	for k, f := range map[string]string{KeyEditorCommand: "zed {path}", KeyDiffTerminal: "false", KeyRefreshInterval: "30s"} {
		if v := s.Values[k]; !v.InFile || v.File != f {
			t.Errorf("%s file value = %q, %v; want %q", k, v.File, v.InFile, f)
		}
	}
	if v := s.Values[KeyUpdateCheck]; v.InFile {
		t.Errorf("updates.check is not in the file, got %q", v.File)
	}
	if len(s.Values) != len(Specs) {
		t.Errorf("%d values for %d specs", len(s.Values), len(Specs))
	}
	for _, v := range s.Values {
		if v.Text == "lin_api_never_shown" || v.Note == "lin_api_never_shown" {
			t.Error("the API key shows in a value")
		}
	}
}

func TestResolveDefaultSources(t *testing.T) {
	getenv, _ := env(t, nil)
	s, err := Resolve(Flags{}, getenv, withHunk)
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range Specs {
		if v := s.Values[sp.Key]; v.Source != FromDefault {
			t.Errorf("%s comes from %v, want the default", sp.Key, v.Source)
		}
	}
	if got := s.Values[KeyDiffCommand].Text; got != DefaultDiffTool {
		t.Errorf("diff.command = %q, want %q", got, DefaultDiffTool)
	}
	if got := s.Values[KeyLinearWorkspace].Text; got != "" {
		t.Errorf("linear.workspace = %q, want none", got)
	}
}

func TestSpecs(t *testing.T) {
	seen := map[string]bool{}
	for _, sp := range Specs {
		if seen[sp.Key] {
			t.Errorf("%s twice", sp.Key)
		}
		seen[sp.Key] = true
		if sp.Help == "" || sp.Default == "" {
			t.Errorf("%s has no help or default", sp.Key)
		}
	}
	tables := map[string]bool{}
	for _, tb := range Tables {
		tables[tb.Name] = true
	}
	// Every name is derived from the dotted key, and no two settings share
	// one, old or new.
	names := map[string]string{}
	claim := func(sp Spec, name string) {
		if other, ok := names[name]; ok {
			t.Errorf("%s and %s both use %s", other, sp.Key, name)
		}
		names[name] = sp.Key
	}
	for _, sp := range Specs {
		if !tables[sp.Table()] {
			t.Errorf("%s: table %q is not in Tables", sp.Key, sp.Table())
		}
		snake := strings.ReplaceAll(sp.Key, ".", "_")
		if want := "--" + strings.ReplaceAll(snake, "_", "-"); sp.Flag != want {
			t.Errorf("%s: flag %s, want %s", sp.Key, sp.Flag, want)
		}
		wantEnv := "HERDR_DECK_" + strings.ToUpper(snake)
		if sp.Key == KeyProjectsRoot {
			wantEnv = "HERDR_PROJECTS_ROOT" // herdr-projects' own
		}
		if sp.Env != wantEnv {
			t.Errorf("%s: env %s, want %s", sp.Key, sp.Env, wantEnv)
		}
		for _, n := range append(append([]string{sp.Flag, sp.Env, sp.Old}, sp.FlagAliases...), sp.EnvAliases...) {
			if n != "" {
				claim(sp, n)
			}
		}
		if sp.Old != "" && strings.Contains(sp.Old, ".") {
			t.Errorf("%s: old key %s is not a flat key", sp.Key, sp.Old)
		}
	}
	for _, sp := range Specs {
		if (sp.Kind == KindChoice) != (len(sp.Choices) > 0) {
			t.Errorf("%s: kind %v with choices %q", sp.Key, sp.Kind, sp.Choices)
		}
	}
	if err := Check(KeyDiffView, "tree"); err != nil {
		t.Errorf("Check(diff.view, tree) = %v", err)
	}
	if err := Check(KeyDiffView, "grid"); err == nil || err.Error() != `"grid" is not list or tree` {
		t.Errorf("Check(diff.view, grid) = %v", err)
	}
	for text, want := range map[string]string{
		"Backlog, Done": "",
		"none":          "",
		" , ":           "a list name is empty",
		"Backlog,,Done": "a list name is empty",
	} {
		got := ""
		if err := Check(KeyFoldedLists, text); err != nil {
			got = err.Error()
		}
		if got != want {
			t.Errorf("Check(ui.folded_lists, %q) = %q, want %q", text, got, want)
		}
	}
	for text, want := range map[string]string{
		"acme/webapp, acme/deploy": "",
		"none":                     "",
		"webapp":                   `"webapp" is not a repo such as owner/name`,
		"acme/web repo:evil/x":     `"acme/web repo:evil/x" is not a repo such as owner/name`,
	} {
		got := ""
		if err := Check(KeyGitHubRepos, text); err != nil {
			got = err.Error()
		}
		if got != want {
			t.Errorf("Check(github.repos, %q) = %q, want %q", text, got, want)
		}
	}
	s := resolveFile(t, "[github]\nrepos = [\"acme/web\", \"nope\"]\n", Flags{}, nil)
	if len(s.GitHubRepos) != 0 || !strings.Contains(strings.Join(s.Problems, "\n"), `github.repos: "nope" is not a repo such as owner/name; ignored`) {
		t.Errorf("a bad repo in the file: repos %q, problems %q", s.GitHubRepos, s.Problems)
	}
	sp, _ := SpecFor(KeyEditorCommand)
	for _, tt := range []struct {
		v    Value
		want string
	}{
		{Value{Source: FromFlag}, "--editor-command"},
		{Value{Source: FromFlag, Via: "--editor"}, "--editor"},
		{Value{Source: FromEnv}, "$" + EnvEditorCommand},
		{Value{Source: FromEnv, Via: "HERDR_DECK_EDITOR"}, "$HERDR_DECK_EDITOR"},
		{Value{Source: FromFile}, ""},
	} {
		if got := sp.Override(tt.v); got != tt.want {
			t.Errorf("Override(%+v) = %q, want %q", tt.v, got, tt.want)
		}
	}
}
