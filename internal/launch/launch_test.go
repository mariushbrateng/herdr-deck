package launch

import (
	"errors"
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"code {path}", []string{"code", "{path}"}},
		{"  zed   --new  ", []string{"zed", "--new"}},
		{`open -a "Sublime Text" {path}`, []string{"open", "-a", "Sublime Text", "{path}"}},
		{`nvim '+cd {path}' x\ y`, []string{"nvim", "+cd {path}", "x y"}},
		{`a "say \"hi\" \n" b''`, []string{"a", `say "hi" \n`, "b"}},
		{`--folder={path}`, []string{"--folder={path}"}},
	}
	for _, tt := range tests {
		got, err := Split(tt.in)
		if err != nil || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Split(%q) = %q, %v; want %q", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{"", "   ", `zed '{path}`, `zed "x`, `zed \`} {
		if got, err := Split(bad); err == nil {
			t.Errorf("Split(%q) = %q, want an error", bad, got)
		}
	}
}

func TestParseCommandPlaceholders(t *testing.T) {
	if _, err := ParseCommand("zed {path}", "path"); err != nil {
		t.Error(err)
	}
	if _, err := ParseCommand("zed {file}", "path"); err == nil {
		t.Error("{file} is not an editor placeholder")
	}
}

func TestEditorArgv(t *testing.T) {
	c := Command{Argv: []string{"zed", "--folder={path}"}}
	if got, want := EditorArgv(c, "/w/t 1"), []string{"zed", "--folder=/w/t 1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("EditorArgv = %q, want %q", got, want)
	}
	c = Command{Argv: []string{"code", "-n"}}
	if got, want := EditorArgv(c, "/w"), []string{"code", "-n", "/w"}; !reflect.DeepEqual(got, want) {
		t.Errorf("EditorArgv without placeholder = %q, want the path appended: %q", got, want)
	}
}

func TestDiffArgv(t *testing.T) {
	c := Command{Argv: []string{"hunk", "diff", "{base}", "--", "{file}"}}
	if got, want := DiffArgv(c, "/w", "main", "a b.go"), []string{"hunk", "diff", "main", "--", "a b.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DiffArgv with file = %q, want %q", got, want)
	}
	if got, want := DiffArgv(c, "/w", "main", ""), []string{"hunk", "diff", "main"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DiffArgv without file = %q, want %q", got, want)
	}
	if got, want := DiffArgv(c, "/w", "main", "old.go", "new.go"), []string{"hunk", "diff", "main", "--", "old.go", "new.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DiffArgv with a rename = %q, want %q", got, want)
	}
	if got, want := DiffArgv(Command{Argv: []string{"tool", "--file={file}"}}, "/w", "main", "a.go", "b.go"), []string{"tool", "--file=a.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DiffArgv with {file} in an argument = %q, want %q", got, want)
	}
	c = Command{Argv: []string{"git", "-C", "{path}", "diff", "--merge-base", "{base}"}}
	if got, want := DiffArgv(c, "/w", "origin/main", ""), []string{"git", "-C", "/w", "diff", "--merge-base", "origin/main"}; !reflect.DeepEqual(got, want) {
		t.Errorf("DiffArgv = %q, want %q", got, want)
	}
}

func TestCommitArgv(t *testing.T) {
	cases := []struct {
		argv, want []string
	}{
		{[]string{"hunk", "diff", "{base}", "--", "{file}"}, []string{"hunk", "diff", "abc^", "abc"}},
		{[]string{"git", "-C", "{path}", "diff", "--merge-base", "{base}", "--", "{file}"}, []string{"git", "-C", "/w", "diff", "--merge-base", "abc^", "abc"}},
		{[]string{"tool", "{file}", "--rev={base}"}, []string{"tool", "--rev=abc^"}},
		{[]string{"tool", "--", "{file}", "{base}"}, []string{"tool", "abc^", "abc"}},
	}
	for _, c := range cases {
		if got := CommitArgv(Command{Argv: c.argv}, "/w", "abc"); !reflect.DeepEqual(got, c.want) {
			t.Errorf("CommitArgv(%q) = %q, want %q", c.argv, got, c.want)
		}
	}
	// One file, a rename's two paths.
	c := Command{Argv: []string{"hunk", "diff", "{base}", "--", "{file}"}}
	if got, want := CommitArgv(c, "/w", "abc", "old.go", "new.go"), []string{"hunk", "diff", "abc^", "abc", "--", "old.go", "new.go"}; !reflect.DeepEqual(got, want) {
		t.Errorf("CommitArgv with files = %q, want %q", got, want)
	}
	c = Command{Argv: []string{"tool", "{file}", "{base}"}}
	if got, want := CommitArgv(c, "/w", "abc", "a.go", "b.go"), []string{"tool", "a.go", "b.go", "abc^", "abc"}; !reflect.DeepEqual(got, want) {
		t.Errorf("CommitArgv with {file} first = %q, want %q", got, want)
	}
}

func TestRunner(t *testing.T) {
	var started, paned []string
	var dir string
	r := Runner{
		Start: func(argv []string, d string) error { started, dir = argv, d; return nil },
		Pane:  func(argv []string, d string) error { paned, dir = argv, d; return nil },
	}
	if err := r.Run(Command{}, []string{"code", "/w"}, "/w"); err != nil || started == nil || paned != nil || dir != "/w" {
		t.Errorf("desktop run: started %q paned %q dir %q err %v", started, paned, dir, err)
	}
	started = nil
	if err := r.Run(Command{Terminal: true}, []string{"nvim", "/w"}, "/w"); err != nil || paned == nil || started != nil {
		t.Errorf("terminal run: started %q paned %q err %v", started, paned, err)
	}
	r.Pane = nil
	if err := r.Run(Command{Terminal: true}, []string{"nvim", "/w"}, "/w"); !errors.Is(err, ErrNoHerdr) {
		t.Errorf("terminal run without herdr = %v, want ErrNoHerdr", err)
	}
}

func TestShellLine(t *testing.T) {
	got := ShellLine([]string{"nvim", "/w/my thread", "it's", `a\b`, "a-b_c.go"})
	if want := `nvim '/w/my thread' 'it'\''s' 'a'\\'b' a-b_c.go`; got != want {
		t.Errorf("ShellLine = %s, want %s", got, want)
	}
}

func TestCanOpen(t *testing.T) {
	cases := []struct {
		goos string
		env  map[string]string
		want bool
	}{
		{"darwin", nil, true},
		{"windows", nil, true},
		{"linux", map[string]string{"DISPLAY": ":0"}, true},
		{"linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, true},
		{"freebsd", map[string]string{"DISPLAY": ":0"}, true},
		{"linux", nil, false},
		{"linux", map[string]string{"DISPLAY": ":0", "SSH_CONNECTION": "10.0.0.2 42186 10.0.0.1 22"}, false},
		{"darwin", map[string]string{"SSH_CONNECTION": "10.0.0.2 42186 10.0.0.1 22"}, false},
		{"darwin", map[string]string{"SSH_TTY": "/dev/ttys003"}, false},
	}
	for _, c := range cases {
		getenv := func(k string) string { return c.env[k] }
		if got := CanOpen(c.goos, getenv); got != c.want {
			t.Errorf("CanOpen(%s, %v) = %v, want %v", c.goos, c.env, got, c.want)
		}
	}
}
