package board

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

const agentsMD = "# Agent notes\n\n## Review tiers\n\n### Critical areas — always require thorough human review\n\n" +
	"- **Authentication & authorization** — `webapp/api_server/authz`, `authenticate_user`, route auth.\n" +
	"- **Payment data / PCI** — anything with `card_vault`.\n" +
	"- **Client / mobile API** — the `/client` sub-app and `api_external`.\n\n" +
	"### Standard areas — a quick review is acceptable\n\n- Frontend copy (`frontend/public/locales/**`).\n"

const invariantsMD = "# Invariants\n\n## Upload durability (CRITICAL)\n\nCode on `main`: `api_server/services/ws_upload_service.py` and\n`frontend/src/hooks/usePolling.ts`.\n\n" +
	"## Architecture (STANDARD)\n\n`api_server/routes/x.py`\n"

func TestCritical(t *testing.T) {
	repo := t.TempDir()
	os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte(agentsMD), 0o644)
	os.WriteFile(filepath.Join(repo, "INVARIANTS.md"), []byte(invariantsMD), 0o644)
	r := &Rules{}
	tests := []struct {
		paths []string
		want  []string
	}{
		{[]string{"frontend/public/locales/nb/common.json"}, nil},
		{[]string{"webapp/api_server/authz/grants.py"}, []string{"AGENTS.md: Authentication & authorization", "touches auth"}},
		{[]string{"webapp/billing/card_vault.py"}, []string{"AGENTS.md: Payment data / PCI"}},
		{[]string{"webapp/api_server/services/ws_upload_service.py"}, []string{"INVARIANTS.md: Upload durability"}},
		{[]string{"api_server/routes/x.py"}, nil},
		{[]string{"supabase/migrations/2024_init.sql"}, []string{"touches migrations"}},
		{[]string{"src/author.go", "docs/dbg.md"}, nil},
		{[]string{"webapp/client/routes.py"}, []string{"AGENTS.md: Client / mobile API"}},
		// `/client` is the sub-app's folder, not every file named client.
		{[]string{"frontend/src/api/client.ts"}, nil},
		// A document about auth changes no auth.
		{[]string{"docs/auth.md"}, nil},
	}
	for _, tt := range tests {
		if got := r.Critical(repo, tt.paths); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Critical(%q) = %q, want %q", tt.paths, got, tt.want)
		}
	}
	// Without a repo folder only the default words count.
	if got := (&Rules{}).Critical("", []string{"db/schema.sql"}); !reflect.DeepEqual(got, []string{"touches db"}) {
		t.Errorf("default rules: %q", got)
	}
}

func TestFrontend(t *testing.T) {
	wt := t.TempDir()
	os.MkdirAll(filepath.Join(wt, "frontend", "src"), 0o755)
	os.WriteFile(filepath.Join(wt, "frontend", "package.json"), []byte("{}"), 0o644)
	if got := Frontend(wt, []string{"webapp/x.py"}); got != "" {
		t.Errorf("backend change: %q", got)
	}
	if got := Frontend(wt, []string{"webapp/x.py", "frontend/src/App.tsx"}); got != "frontend" {
		t.Errorf("frontend change: %q", got)
	}
	root := t.TempDir()
	os.WriteFile(filepath.Join(root, "package.json"), []byte("{}"), 0o644)
	if got := Frontend(root, []string{"src/page.tsx"}); got != "." {
		t.Errorf("root frontend: %q", got)
	}
}
