package board

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// defaultWords are path words that make a change critical in any repo:
// a path segment or file name made of them, such as migrations/ or
// auth.ts, marks it.
var defaultWords = map[string]string{
	"auth": "auth", "authn": "auth", "authz": "auth", "authentication": "auth", "authorization": "auth", "oauth": "auth",
	"migration": "migrations", "migrations": "migrations", "alembic": "migrations",
	"db": "db", "database": "db",
	"security": "security", "secret": "secrets", "secrets": "secrets", "crypto": "security", "encryption": "security",
	"permission": "permissions", "permissions": "permissions", "rbac": "permissions",
}

// Rules says which changed paths are critical in a repo: the default path
// words, and the paths a repo's AGENTS.md names under its critical areas
// and its INVARIANTS.md under its CRITICAL sections. Each repo's files are
// read again when they change.
type Rules struct {
	mu    sync.Mutex
	repos map[string]repoRules
}

type repoRules struct {
	stamp string
	areas []area
}

// area is one path a repo's own files call critical, and what they call it.
type area struct {
	path string // e.g. "webapp/api_server/authz", "card_vault"
	name string // e.g. "Authentication & authorization"
	from string // AGENTS.md or INVARIANTS.md
}

// Critical returns why the paths changed in repo (its root folder) make
// the change critical, one reason per kind; nil when nothing does.
func (r *Rules) Critical(repo string, paths []string) []string {
	areas := r.areas(repo)
	seen := map[string]bool{}
	var reasons []string
	add := func(s string) {
		if !seen[s] {
			seen[s] = true
			reasons = append(reasons, s)
		}
	}
	for _, p := range paths {
		lp := strings.ToLower(filepath.ToSlash(p))
		for _, a := range areas {
			if hasPath(lp, strings.ToLower(a.path)) {
				add(a.from + ": " + a.name)
			}
		}
		if docFile(lp) {
			// A document about auth is not auth.
			continue
		}
		for _, w := range pathWords(lp) {
			if kind, ok := defaultWords[w]; ok {
				add("touches " + kind)
			}
		}
	}
	sort.SliceStable(reasons, func(i, j int) bool {
		// The repo's own rules first: they say more.
		return !strings.HasPrefix(reasons[i], "touches ") && strings.HasPrefix(reasons[j], "touches ")
	})
	return reasons
}

var wordSplit = regexp.MustCompile(`[/._\-]+`)

// pathWords are a path's words: its segments split at dots, dashes and
// underscores, so "supabase/migrations/2024_init.sql" gives "migrations".
func pathWords(p string) []string {
	return wordSplit.Split(p, -1)
}

// hasPath reports whether path contains area at segment boundaries: the
// area "api_server/authz" is in "webapp/api_server/authz/x.py", and the
// area "card_vault" in "a/card_vault.py", but "auth" is not in
// "author.go".
func hasPath(path, area string) bool {
	// `/client` names a sub-app: a folder, never a file such as client.ts.
	folder := strings.HasPrefix(area, "/") || strings.HasSuffix(strings.TrimRight(area, "*"), "/")
	area = strings.Trim(area, "/*")
	if area == "" {
		return false
	}
	for i := 0; ; {
		j := strings.Index(path[i:], area)
		if j < 0 {
			return false
		}
		j += i
		before := j == 0 || path[j-1] == '/'
		end := j + len(area)
		after := end < len(path) && path[end] == '/' || !folder && (end == len(path) || strings.ContainsRune("._", rune(path[end])))
		if before && after {
			return true
		}
		i = j + 1
	}
}

func (r *Rules) areas(repo string) []area {
	if repo == "" {
		return nil
	}
	files := []string{filepath.Join(repo, "AGENTS.md"), filepath.Join(repo, "INVARIANTS.md")}
	stamp := ""
	for _, f := range files {
		if fi, err := os.Stat(f); err == nil {
			stamp += fi.ModTime().Format(time.RFC3339Nano) + "|"
		} else {
			stamp += "-|"
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if have, ok := r.repos[repo]; ok && have.stamp == stamp {
		return have.areas
	}
	var areas []area
	if data, err := os.ReadFile(files[0]); err == nil {
		areas = append(areas, agentsAreas(string(data))...)
	}
	if data, err := os.ReadFile(files[1]); err == nil {
		areas = append(areas, invariantAreas(string(data))...)
	}
	if r.repos == nil {
		r.repos = map[string]repoRules{}
	}
	r.repos[repo] = repoRules{stamp: stamp, areas: areas}
	return areas
}

var (
	mdHeading = regexp.MustCompile(`^(#{1,6})\s+(.*)$`)
	codeSpan  = regexp.MustCompile("`([^`\\s()]+)`")
	boldName  = regexp.MustCompile(`^\s*[-*]\s+\*\*([^*]+)\*\*`)
	fileLike  = regexp.MustCompile(`\.[a-z]{1,4}$`)
)

// agentsAreas reads the paths under an AGENTS.md heading that says
// "Critical": each list item's bold name, and the code spans in it that
// look like paths or identifiers (`webapp/api_server/authz`,
// `card_vault`).
func agentsAreas(src string) []area {
	var areas []area
	in, level := false, 0
	for _, line := range strings.Split(src, "\n") {
		if m := mdHeading.FindStringSubmatch(line); m != nil {
			if in && len(m[1]) <= level {
				in = false
			}
			if strings.Contains(strings.ToLower(m[2]), "critical") {
				in, level = true, len(m[1])
			}
			continue
		}
		if !in {
			continue
		}
		name := ""
		if m := boldName.FindStringSubmatch(line); m != nil {
			name = strings.TrimSpace(m[1])
		}
		if name == "" {
			continue
		}
		for _, m := range codeSpan.FindAllStringSubmatch(line, -1) {
			p := m[1]
			if strings.ContainsAny(p, "/_") {
				areas = append(areas, area{path: p, name: name, from: "AGENTS.md"})
			}
		}
	}
	return areas
}

// invariantAreas reads the file paths an INVARIANTS.md names in its
// sections marked CRITICAL: `api_server/services/ws_upload_service.py`.
func invariantAreas(src string) []area {
	var areas []area
	name := ""
	for _, line := range strings.Split(src, "\n") {
		if m := mdHeading.FindStringSubmatch(line); m != nil {
			if len(m[1]) == 2 {
				name = ""
				if strings.Contains(m[2], "(CRITICAL)") {
					name = strings.TrimSpace(strings.Replace(m[2], "(CRITICAL)", "", 1))
				}
			}
			continue
		}
		if name == "" {
			continue
		}
		for _, m := range codeSpan.FindAllStringSubmatch(line, -1) {
			p := m[1]
			if strings.Contains(p, "/") && (fileLike.MatchString(p) || strings.HasSuffix(p, "/")) {
				areas = append(areas, area{path: p, name: name, from: "INVARIANTS.md"})
			}
		}
	}
	return areas
}

// frontendFolders are where a repo keeps its frontend, when not at its
// root.
var frontendFolders = []string{"frontend", "web", "client", "app", "ui"}

// frontendExt are the file kinds a frontend change shows in.
var frontendExt = map[string]bool{
	".tsx": true, ".jsx": true, ".ts": true, ".js": true, ".mjs": true, ".vue": true, ".svelte": true,
	".css": true, ".scss": true, ".html": true, ".json": true, ".svg": true,
}

// Frontend is the folder of the worktree whose frontend the changed paths
// touch: the first of frontendFolders with a package.json that a changed
// file is under, or "." when the worktree's own package.json is and a
// frontend file changed; "" when no frontend changed.
func Frontend(worktree string, paths []string) string {
	has := func(dir string) bool {
		_, err := os.Stat(filepath.Join(worktree, dir, "package.json"))
		return err == nil
	}
	for _, dir := range frontendFolders {
		if !has(dir) {
			continue
		}
		for _, p := range paths {
			if strings.HasPrefix(filepath.ToSlash(p), dir+"/") && frontendExt[filepath.Ext(p)] {
				return dir
			}
		}
	}
	if has(".") {
		for _, p := range paths {
			if frontendExt[filepath.Ext(p)] && !strings.HasPrefix(p, ".") {
				return "."
			}
		}
	}
	return ""
}

// docFile reports whether p is documentation, whose path words say what
// it is about rather than what it changes.
func docFile(p string) bool {
	switch filepath.Ext(p) {
	case ".md", ".mdx", ".rst", ".txt", ".adoc":
		return true
	}
	return false
}
