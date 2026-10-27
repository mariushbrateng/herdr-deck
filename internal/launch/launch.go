// Package launch opens URLs and folders in the user's apps, and runs the
// configured editor and diff tool, either as a desktop app or in a new
// herdr pane for a terminal program.
package launch

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"slices"
	"strings"
)

// URL opens url with the system's handler: `open` on macOS, else `xdg-open`.
// It returns once the handler has started, without waiting for it.
func URL(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return start(exec.Command(name, url))
}

// Start runs argv in dir and returns once it has started, without waiting
// for it.
func Start(argv []string, dir string) error {
	if len(argv) == 0 {
		return errors.New("empty command")
	}
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Dir = dir
	return start(cmd)
}

func start(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }() // reap the child; its exit status does not matter
	return nil
}

// Command is a configured program: an argv template whose arguments may hold
// placeholders such as {path}, and whether it is a terminal program that
// needs a pane of its own.
type Command struct {
	Argv     []string
	Terminal bool
}

// ErrNoHerdr means a terminal program was asked for outside herdr, where the
// deck has no pane to give it.
var ErrNoHerdr = errors.New("a terminal program needs herdr to open a pane; run the deck inside herdr")

// Runner starts commands. Both funcs are injectable so tests never launch
// anything.
type Runner struct {
	// Start runs a desktop program; nil means launch.Start.
	Start func(argv []string, dir string) error
	// Pane runs a terminal program in a new herdr pane next to the deck,
	// in dir; nil means there is no herdr, and Run returns ErrNoHerdr.
	Pane func(argv []string, dir string) error
}

// Run runs argv (already expanded from c) in dir, in a herdr pane when c is
// a terminal program.
func (r Runner) Run(c Command, argv []string, dir string) error {
	if len(argv) == 0 {
		return errors.New("empty command")
	}
	if c.Terminal {
		if r.Pane == nil {
			return ErrNoHerdr
		}
		return r.Pane(argv, dir)
	}
	if r.Start == nil {
		return Start(argv, dir)
	}
	return r.Start(argv, dir)
}

// EditorArgv is the editor command for path: {path} replaced, or path
// appended when no argument holds the placeholder.
func EditorArgv(c Command, path string) []string {
	argv := Expand(c.Argv, map[string]string{"path": path})
	if !uses(c.Argv, "path") {
		argv = append(argv, path)
	}
	return argv
}

// DiffArgv is the diff command for the worktree at path against base,
// limited to files when there are any ("" counts as none). An argument
// that is just {file} becomes one argument per file, so a rename can pass
// its old and new path and git pairs them; {file} inside a longer
// argument is the first file. Without files, an argument that is just
// {file} is dropped, and with it a "--" right before it.
func DiffArgv(c Command, path, base string, files ...string) []string {
	var fs []string
	for _, f := range files {
		if f != "" {
			fs = append(fs, f)
		}
	}
	first := ""
	if len(fs) > 0 {
		first = fs[0]
	}
	vars := map[string]string{"path": path, "base": base, "file": first}
	var argv []string
	for i, a := range c.Argv {
		switch {
		case a == "{file}":
			argv = append(argv, fs...)
		case a == "--" && len(fs) == 0 && i+1 < len(c.Argv) && c.Argv[i+1] == "{file}":
		default:
			argv = append(argv, Expand([]string{a}, vars)...)
		}
	}
	return argv
}

// CommitArgv is the diff command for one commit of the worktree at path,
// limited to files when there are any (a rename's old and new path): {base}
// is the commit's first parent (sha^), and the commit itself goes in right
// after an argument that is just {base}, so `hunk diff {base}` and `git
// diff --merge-base {base}` compare the parent with the commit rather
// than with the working tree. {file} works as in DiffArgv.
func CommitArgv(c Command, path, sha string, files ...string) []string {
	argv := DiffArgv(c, path, sha+"^", files...)
	i := slices.Index(c.Argv, "{base}")
	if i < 0 {
		return argv
	}
	n := 0
	for _, f := range files {
		if f != "" {
			n++
		}
	}
	// Where {base} landed: DiffArgv turns each {file} into n arguments
	// and, without files, drops a "--" right before one.
	at := 0
	for j, a := range c.Argv[:i] {
		switch {
		case a == "{file}":
			at += n
		case a == "--" && n == 0 && j+1 < len(c.Argv) && c.Argv[j+1] == "{file}":
		default:
			at++
		}
	}
	return slices.Insert(argv, at+1, sha)
}

var placeholder = regexp.MustCompile(`\{([a-z]+)\}`)

// Expand replaces each {name} in each argument with vars[name]. Unknown
// names are left as they are; Placeholders finds them beforehand.
func Expand(tmpl []string, vars map[string]string) []string {
	out := make([]string, len(tmpl))
	for i, a := range tmpl {
		out[i] = placeholder.ReplaceAllStringFunc(a, func(m string) string {
			if v, ok := vars[m[1:len(m)-1]]; ok {
				return v
			}
			return m
		})
	}
	return out
}

// Placeholders lists the placeholder names argv uses, in order, once each.
func Placeholders(argv []string) []string {
	var names []string
	seen := map[string]bool{}
	for _, a := range argv {
		for _, m := range placeholder.FindAllStringSubmatch(a, -1) {
			if !seen[m[1]] {
				seen[m[1]] = true
				names = append(names, m[1])
			}
		}
	}
	return names
}

func uses(argv []string, name string) bool {
	for _, n := range Placeholders(argv) {
		if n == name {
			return true
		}
	}
	return false
}

// Split parses a command line into argv the way a POSIX shell splits words,
// without running a shell: blanks separate words, '…' quotes literally,
// "…" quotes with \ escaping " and \, and \ outside quotes escapes the next
// character. Nothing is expanded.
func Split(s string) ([]string, error) {
	var argv []string
	var cur strings.Builder
	inWord := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case ' ', '\t', '\n':
			if inWord {
				argv = append(argv, cur.String())
				cur.Reset()
				inWord = false
			}
		case '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, errors.New("unterminated ' quote")
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
			inWord = true
		case '"':
			i++
			for ; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\') {
					i++
				}
				cur.WriteByte(s[i])
			}
			if i >= len(s) {
				return nil, errors.New("unterminated \" quote")
			}
			inWord = true
		case '\\':
			if i+1 >= len(s) {
				return nil, errors.New("trailing \\")
			}
			i++
			cur.WriteByte(s[i])
			inWord = true
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if inWord {
		argv = append(argv, cur.String())
	}
	if len(argv) == 0 {
		return nil, errors.New("empty command")
	}
	return argv, nil
}

// ParseCommand splits s and checks that it only uses the allowed
// placeholders.
func ParseCommand(s string, allowed ...string) ([]string, error) {
	argv, err := Split(s)
	if err != nil {
		return nil, err
	}
	for _, n := range Placeholders(argv) {
		ok := false
		for _, a := range allowed {
			ok = ok || n == a
		}
		if !ok {
			return nil, fmt.Errorf("unknown placeholder {%s}; use %s", n, braces(allowed))
		}
	}
	return argv, nil
}

func braces(names []string) string {
	b := make([]string, len(names))
	for i, n := range names {
		b[i] = "{" + n + "}"
	}
	return strings.Join(b, ", ")
}

// ShellLine quotes argv for the interactive shell of a new pane: words with
// anything but safe characters go in single quotes. A ' or \ is written
// outside the quotes, escaped with \: fish treats both as escapes inside
// single quotes, POSIX shells neither, so this reads the same in sh, bash,
// zsh and fish.
func ShellLine(argv []string) string {
	q := make([]string, len(argv))
	for i, a := range argv {
		q[i] = shellQuote(a)
	}
	return strings.Join(q, " ")
}

var safeWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellQuote(s string) string {
	if safeWord.MatchString(s) {
		return s
	}
	r := strings.NewReplacer(`'`, `'\''`, `\`, `'\\'`)
	return "'" + r.Replace(s) + "'"
}

// CanOpen reports whether URL can open a link on the screen the user looks
// at, on goos with the environment getenv. Over SSH (herdr --remote among
// others) the deck runs on another machine than the user's, and a Linux or
// BSD machine without $DISPLAY or $WAYLAND_DISPLAY has no screen; the deck
// then copies links instead.
func CanOpen(goos string, getenv func(string) string) bool {
	if getenv("SSH_CONNECTION") != "" || getenv("SSH_TTY") != "" {
		return false
	}
	switch goos {
	case "darwin", "windows":
		return true
	}
	return getenv("DISPLAY") != "" || getenv("WAYLAND_DISPLAY") != ""
}
