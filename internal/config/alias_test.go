package config

import (
	"bytes"
	"flag"
	"path/filepath"
	"strings"
	"testing"
)

// samples are two values for every setting: text as a flag or environment
// variable gives it, its TOML literal and Value.Text, and another value's
// literal and text, for the file to hide.
var samples = map[string]struct{ text, lit, shown, otherLit, other string }{
	KeyRefreshInterval:     {"30s", `"30s"`, "30s", `"20s"`, "20s"},
	KeyFoldedLists:         {"Later", `["Later"]`, "Later", `["Icebox"]`, "Icebox"},
	KeyDrawerHeight:        {"0.4", "0.4", "0.4", "0.6", "0.6"},
	KeyProjectsRoot:        {"/work/projects", `"/work/projects"`, "/work/projects", `"/file/root"`, "/file/root"},
	KeyLinearWorkspace:     {"acme", `"acme"`, "acme", `"globex"`, "globex"},
	KeyLinearStatus:        {"false", "false", "false", "true", "true"},
	KeyLinearAPIKeyCommand: {"op read x", `"op read x"`, "op read x", `"pass linear"`, "pass linear"},
	KeyGitHubEnabled:       {"false", "false", "false", "true", "true"},
	KeyGitHubRepos:         {"acme/web", `["acme/web"]`, "acme/web", `["acme/api"]`, "acme/api"},
	KeyFigmaDesktop:        {"true", "true", "true", "false", "false"},
	KeyReuseTabs:           {"false", "false", "false", "true", "true"},
	KeyUpdateCheck:         {"false", "false", "false", "true", "true"},
	KeyAutoRestart:         {"false", "false", "false", "true", "true"},
	KeyEditorCommand:       {"zed {path}", `"zed {path}"`, "zed {path}", `"vi {path}"`, "vi {path}"},
	KeyEditorTerminal:      {"true", "true", "true", "false", "false"},
	KeyDiffCommand:         {"tig", `"tig"`, "tig", `"lazygit"`, "lazygit"},
	KeyDiffTerminal:        {"false", "false", "false", "true", "true"},
	KeyDiffView:            {"tree", `"tree"`, "tree", `"list"`, "list"},
	KeyDiffLayout:          {"split", `"split"`, "split", `"unified"`, "unified"},
	KeyArchEnabled:         {"false", "false", "false", "true", "true"},
	KeyArchTests:           {"true", "true", "true", "false", "false"},
}

func TestSamplesCoverSpecs(t *testing.T) {
	for _, sp := range Specs {
		if _, ok := samples[sp.Key]; !ok {
			t.Errorf("no sample for %s", sp.Key)
		}
	}
}

// resolveFile resolves with the config file body under a fresh home.
func resolveFile(t *testing.T, body string, fl Flags, vars map[string]string) Settings {
	t.Helper()
	getenv, home := env(t, vars)
	write(t, filepath.Join(home, ".config", "herdr-deck", "config.toml"), body)
	s, err := Resolve(fl, getenv, noHunk)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// tableLine is key's line in its own table.
func tableLine(key, lit string) string {
	table, name := splitKey(key)
	return "[" + table + "]\n" + name + " = " + lit + "\n"
}

// An earlier flat key reads as the setting, with a note; the table's key
// wins over it.
func TestOldFileKeys(t *testing.T) {
	for _, sp := range Specs {
		if sp.Old == "" {
			continue
		}
		smp := samples[sp.Key]
		t.Run(sp.Old, func(t *testing.T) {
			s := resolveFile(t, sp.Old+" = "+smp.lit+"\n", Flags{}, nil)
			v := s.Values[sp.Key]
			if v.Text != smp.shown || v.Source != FromFile || v.Old != sp.Old || v.Shadowed || len(s.Problems) != 0 {
				t.Errorf("%s = %+v, problems %q; want %q from the file under %s", sp.Key, v, s.Problems, smp.shown, sp.Old)
			}
			if len(s.Notes) != 1 || !strings.Contains(s.Notes[0], sp.Old+" is renamed to "+sp.Key) {
				t.Errorf("notes = %q, want one about the rename", s.Notes)
			}

			// Both: the table's value wins, and the old one is ignored.
			s = resolveFile(t, sp.Old+" = "+smp.lit+"\n"+tableLine(sp.Key, smp.otherLit), Flags{}, nil)
			v = s.Values[sp.Key]
			if v.Text != smp.other || v.Source != FromFile || !v.Shadowed || len(s.Problems) != 0 {
				t.Errorf("both set: %s = %+v, problems %q; want the table's %q", sp.Key, v, s.Problems, smp.other)
			}
			if len(s.Notes) != 1 || !strings.Contains(s.Notes[0], sp.Key+" wins") {
				t.Errorf("both set: notes = %q", s.Notes)
			}

			// A bad old value is named as the file writes it.
			s = resolveFile(t, sp.Old+" = {}\n", Flags{}, nil)
			if len(s.Problems) != 1 || !strings.Contains(s.Problems[0], ": "+sp.Old+": ") {
				t.Errorf("bad value: problems = %q, want one naming %s", s.Problems, sp.Old)
			}
		})
	}
}

// Every flag and its earlier names set the setting, beating the
// environment and the file; an earlier name never beats the flag itself.
func TestFlagsAndAliases(t *testing.T) {
	for _, sp := range Specs {
		smp := samples[sp.Key]
		for _, name := range append([]string{sp.Flag}, sp.FlagAliases...) {
			t.Run(name, func(t *testing.T) {
				fs := flag.NewFlagSet("t", flag.ContinueOnError)
				var fl Flags
				fl.Register(fs)
				if err := fs.Parse([]string{name + "=" + smp.text}); err != nil {
					t.Fatal(err)
				}
				s := resolveFile(t, tableLine(sp.Key, smp.otherLit), fl, map[string]string{sp.Env: smp.text + "x"})
				v := s.Values[sp.Key]
				want := Value{Text: smp.shown, Source: FromFlag}
				if name != sp.Flag {
					want.Via = name
				}
				if v.Text != want.Text || v.Source != want.Source || v.Via != want.Via {
					t.Errorf("%s = %+v, want %+v", sp.Key, v, want)
				}
				if got := sp.Override(v); got != name {
					t.Errorf("Override = %q, want %q", got, name)
				}
			})
		}
		for _, alias := range sp.FlagAliases {
			for _, args := range [][]string{{sp.Flag + "=" + smp.text, alias + "=x"}, {alias + "=x", sp.Flag + "=" + smp.text}} {
				fs := flag.NewFlagSet("t", flag.ContinueOnError)
				var fl Flags
				fl.Register(fs)
				if err := fs.Parse(args); err != nil {
					t.Fatal(err)
				}
				if got := fl.Values[sp.Key]; got.Name != sp.Flag || got.Text != smp.text {
					t.Errorf("%q gave %+v, want %s's value", args, got, sp.Flag)
				}
			}
		}
	}
}

// A bad value through an earlier flag name is an error naming that flag.
func TestFlagAliasErrorNamesIt(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	var fl Flags
	fl.Register(fs)
	if err := fs.Parse([]string{"--refresh-interval", "soon", "--auto-restart=maybe"}); err != nil {
		t.Fatal(err)
	}
	getenv, _ := env(t, nil)
	if _, err := Resolve(fl, getenv, noHunk); err == nil || !strings.HasPrefix(err.Error(), "--refresh-interval: ") {
		t.Errorf("Resolve = %v, want an error naming --refresh-interval", err)
	}
	delete(fl.Values, KeyRefreshInterval)
	if _, err := Resolve(fl, getenv, noHunk); err == nil || !strings.HasPrefix(err.Error(), "--auto-restart: ") {
		t.Errorf("Resolve = %v, want an error naming --auto-restart", err)
	}
}

// A switch's flag alone means true.
func TestBoolFlagsStandAlone(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	var fl Flags
	fl.Register(fs)
	if err := fs.Parse([]string{"--figma-desktop", "--diff-terminal=false"}); err != nil {
		t.Fatal(err)
	}
	s := resolveFile(t, "", fl, nil)
	if !s.FigmaDesktop || s.Diff.Terminal {
		t.Errorf("FigmaDesktop, Diff.Terminal = %v, %v; want true, false", s.FigmaDesktop, s.Diff.Terminal)
	}
}

// Every environment variable and its earlier names set the setting over
// the file; the current name wins over an earlier one.
func TestEnvAndAliases(t *testing.T) {
	for _, sp := range Specs {
		smp := samples[sp.Key]
		for _, name := range append([]string{sp.Env}, sp.EnvAliases...) {
			t.Run(name, func(t *testing.T) {
				s := resolveFile(t, tableLine(sp.Key, smp.otherLit), Flags{}, map[string]string{name: smp.text})
				v := s.Values[sp.Key]
				want := Value{Text: smp.shown, Source: FromEnv}
				if name != sp.Env {
					want.Via = name
				}
				if v.Text != want.Text || v.Source != want.Source || v.Via != want.Via || len(s.Problems) != 0 {
					t.Errorf("%s = %+v, problems %q; want %+v", sp.Key, v, s.Problems, want)
				}
				if got := sp.Override(v); got != "$"+name {
					t.Errorf("Override = %q, want $%s", got, name)
				}
			})
		}
		for _, alias := range sp.EnvAliases {
			s := resolveFile(t, "", Flags{}, map[string]string{sp.Env: smp.text, alias: "junk {nope}"})
			if v := s.Values[sp.Key]; v.Text != smp.shown || v.Via != "" {
				t.Errorf("$%s and $%s: %s = %+v, want $%s's", sp.Env, alias, sp.Key, v, sp.Env)
			}
		}
	}
}

// Help lists each setting's flag once and leaves the earlier names out.
func TestPrintDefaultsHidesAliases(t *testing.T) {
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	var fl Flags
	hidden := fl.Register(fs)
	var b bytes.Buffer
	fs.SetOutput(&b)
	PrintDefaults(fs, hidden)
	out := b.String()
	for _, sp := range Specs {
		// flag prints names with one dash.
		if one := "  " + sp.Flag[1:]; !strings.Contains(out, one+" ") && !strings.Contains(out, one+"\n") {
			t.Errorf("help lacks %s", sp.Flag)
		}
		if !strings.Contains(out, sp.Key+" in the config file") {
			t.Errorf("help does not name %s", sp.Key)
		}
		for _, a := range sp.FlagAliases {
			if one := "  " + a[1:]; strings.Contains(out, one+" ") || strings.Contains(out, one+"\n") {
				t.Errorf("help shows the earlier %s", a)
			}
		}
	}
}
