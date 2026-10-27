# herdr-deck — plan

A standalone Go + Charm TUI that runs in a split next to each herdr-projects
coordinator and shows, always up to date, what that project is doing: its
tasks, its threads and their status, links to Linear / Notion / Figma / GitHub,
and the dev servers its worktrees run.

Written 2026-10-02 from an investigation of herdr 0.9.3 and herdr-projects
0.2.34. Paths and line numbers below were true then; verify before relying on
them.

## Principles

- **Read-only towards herdr-projects.** Never fork it, never write its files.
  It is Elias Stravik's plugin (MIT) and releases often; we read its files and
  its one JSON command. The only writes we make are herdr pane/workspace
  metadata under our own `--source herdr-deck`, and opening URLs.
- **Standalone binary first, herdr plugin wrapper second.** `herdr-deck` must
  run in any terminal (`herdr-deck --project admin-rebuild`). The herdr plugin
  is a thin `herdr-plugin.toml` that opens it in a split.
- **Degrade, don't fail.** Any source can be missing (no ticker, no herdr
  socket, no manifest, odd TASKS.md). Show what is there, mark what is not.
- **Data layer separate from UI.** `internal/source/*` produces plain structs
  (`deck.Snapshot` and friends in `internal/deck`); `internal/ui` only renders
  them. Data code is unit-tested against fixture
  files in the shapes herdr-projects writes.

## Stack

- Go 1.27 or newer, module `github.com/FredricW/herdr-deck`.
- Bubble Tea v2 (model/update/view), Bubbles (list, table, viewport, help,
  key), Lip Gloss (layout/styling), imported from `charm.land/bubbletea/v2`,
  `charm.land/bubbles/v2` and `charm.land/lipgloss/v2`.
- bubblezone for clickable rows, only if Bubble Tea v2's own mouse handling
  isn't enough.
- `BurntSushi/toml` (or `pelletier/go-toml/v2`) for herdr-projects' TOML.
- glamour (`charm.land/glamour/v2`, the renderer glow uses) for Markdown:
  `internal/markdown` renders a thread's report and What's new with its
  dark or light style (the deck's background detection picks one), without
  the document margin, wrapped to the drawer's width and cached by source,
  width and style. If glamour fails, the plain text shows, wrapped. Task
  notes, Next items and inbox summaries stay plain one-line fields.
- chroma (`github.com/alecthomas/chroma/v2`, already in the binary through
  glamour) for the diff preview's syntax colours, in `internal/syntax`.
- No CGO. `go build` produces one binary.

## Data sources

All under the projects root (`$HERDR_PROJECTS_ROOT`, else
`~/.herdr-projects`), project folder `<root>/<slug>/`.

| What | Where | Notes |
|---|---|---|
| Projects | folders under the root with a `PROJECT.md`; `<slug>/.state/project.json` → `status` (`active`, `paused`, `archived`); `<slug>/.state/coordinator.json` → `pane_id` | For the project picker; read without running `herdr-projects list`. `herdr-projects --root <root> open <slug> --tab` starts a coordinator, only when the user picks a project with none. |
| Project settings | `<slug>/PROJECT.md` | `+++` TOML front matter: `name`, `goal`, `[[repos]] path`. |
| Threads | `herdr-projects --root <root> thread list <slug> --json` | The only JSON output. Each row has every TOML field plus `group`, `rank`, `next[]`, `report`. Fallback: read `<slug>/threads/t-*.toml` directly. |
| Thread status | fields `last_group`, `state_line` (e.g. `needs you · ~95%`), `activity`, `percent`, `status` | Updated by the herdr-projects ticker every ~15 s. |
| PRs | thread `pr`, `pr_state`, `pr_review`; `<slug>/.state/ticker.json` → `prs[thread id]` | `prs` holds `state`, `review_decision`, `failing_checks[]`, `comment_count`, `commenters[]`. Refreshed every 2 min. |
| Tasks | `<slug>/TASKS.md` | Written freely by the coordinator agent. Expected format is `## <List>` headings and `- [ ] Title (owner) · t-0007` lines with notes indented two spaces (herdr-projects `skill/COORDINATOR.md:78-81`, parser `src/tasks.rs:126`), but real files often use plain `- ` bullets with "thread t-0002" in prose. Parse loosely; see below. |
| Inbox | `<slug>/inbox/*.md`; the drawer's Log tab also reads `inbox/done/*.md` | `+++` TOML front matter: `id`, `kind`, `subject`, `created`, `summary`, `event`. Count + list of unhandled items from `inbox/`; handled items in `done/` are the thread history the Log tab shows. |
| Thread brief / report | `<slug>/threads/t-NNNN.task.md`, `t-NNNN.md` | Free text. Scrape for links. Report may start with `PR: <url>` and has a `## Next` section. |
| herdr live state | `session.snapshot` over the socket (the same JSON as `herdr api snapshot`) on every reload | Workspaces, panes, agents, tokens (`hp_project`, `hp_sub`, `hp_group`, `port`). A thread's pane has `hp_project` = slug and `hp_group` = `<slug>!1!<rank>!<thread id>`; the coordinator's `hp_group` is `<slug>!0!<pane id>`. Without tokens, the recorded `pane_id` counts when its `cwd` is the thread's worktree. |
| herdr events | Unix socket `$HERDR_SOCKET_PATH` (else `~/.config/herdr/herdr.sock`), newline-delimited JSON `{id, method, params}`; `events.subscribe {subscriptions:[{type:"pane.agent_status_changed"}, …]}` | Verified in milestone 5 (herdr 0.9.3): after `subscription_started` the connection stays open and streams `{"event","data"}` lines (event names use `_`: `pane_updated`). `pane.updated` fires for every pane each time the herdr-projects ticker rewrites tokens (~15 s) and carries the whole pane. `pane.agent_status_changed` needs a `pane_id`, so the deck subscribes once per agent pane, and again when panes come or go. One unknown pane id fails the whole subscribe and closes the connection. When the subscribe is refused the deck polls every 2.5 s. Reference round-trip: herdr-projects `src/runner.rs:279-291`. Full method list: `herdr api schema --json`. |
| Dev servers | The shared dev manifest `.config/dev.json` (docs/dev-manifest.md), looked up in the worktree, then the main checkout (the thread's `repo`), then the legacy `.herdr-deck/dev.json` in the same order; the first file found is the manifest and a broken one never falls through. Parsed and checked by `internal/source/dev/manifest` (schema/v1 plus the spec's rules; the dev-manifest skill's validator uses the same package). Ports: fixed, or `{state}` from the project's state file; `{base}` (the port store) is a note, not guessed, until phase 2. Services (declared, plus one per port no service lists) get a dot each: ready = main port (or `ready.port`) answers a TCP connect to 127.0.0.1 or [::1] (400 ms, parallel, cached 2 s) or `ready.http` returns 200–399; without ports, its run record's process is alive. `u` runs `dev`, `U U` `stop`, with run records and logs in `$XDG_STATE_HOME/dev-manifest/` (spec section 10). A repo without a manifest, a legacy one and port-store ports are notes in Sources; a broken manifest or state file is missing. |
| Fallback port | herdr workspace token `port` on the workspace whose `worktree.checkout_path` is the thread's worktree (set by a worktree plugin, e.g. from a hash of the branch) | Use only when no manifest port is found; marked `~`. |

**Never** call `herdr-projects context <slug>` without `--peek`: without it
the command marks inbox items as seen.

### herdr-projects 1.x: the board

herdr-projects 1.0 replaced the project folders with one work board of
tasks, kept in the same home folder (`~/.herdr-projects`). The deck reads
it with `internal/source/board` when `<root>/data/tasks` exists and the
slug names no 0.2 project folder (one with a `PROJECT.md`); `--project
<repo>` (or `$HERDR_DECK_PROJECT`) narrows it to one repo's tasks, and the
toggle opens it from any pane. 0.2 projects keep their reader.

| What | Where | Notes |
|---|---|---|
| Tasks | `herdr-projects list --json` (`--all` with archived ones) | Preferred. Each row: `id` (`repo/n`), `state`, `stage`, `title`, `msg`, `progress`, `eta`, `pr`, `review`, `reviewRound`, `queue`, `research`, `chat`. |
| One task | `herdr-projects details <id>` | `task.id` is the repo's key (`WEB-7`, the deck's thread id), `description`, `worktree`, `branch`, `report`, `review` (with its findings and page), `limits.time.usedMs`, `limits.tokens.used`. |
| Task folder | `<root>/data/tasks/<repo>/<n>/task.json`, `closing-note.md` | Only what the CLI leaves out: `link.pane`, `from` (the base), `root` (the repo), `usage.context`, `prState`/`prMerged`, `created`/`at`. When the CLI cannot run, every task is read from here. |
| Reports | `<root>/reports/<repo>/<n>/` | The report file the board names, else `index.html` or `report.md`. `R` opens it, `r` shows a Markdown report or the closing note. |
| Priority | `$XDG_STATE_HOME/herdr-deck/priorities.json` | The deck's own, by board id; the board never sees it. 1 Urgent, 2 High, 3 Medium, 4 Low, as Linear numbers them (decided with the user); `P` then `1`–`4` sets it, `0` clears. |

The board's stage maps to a phase with its own glyph and colour, after
herdr-projects' `docs/colours.md`: to do `○` grey, queued `◌` pink,
working `◐` amber, the board's own check `◑` light grey, plan ready `◆` and ready to approve `★` green, asks
you `●` and failed `✕` red, in review `◇` blue, merging `↻` and done `✓`
teal, merged `✓` purple. What waits on the user (plan, asks, approve,
failed) is pinned under Needs you. The lists are the priorities in use,
then No priority, then Resolved (folded); within a list, tasks sort by
phase (what waits on you first, finished work last), then newest first.

A task is marked critical (`⚠`) when its review tier or a finding is
critical, or its worktree's changes touch a path the repo's AGENTS.md
lists under a "Critical" heading or its INVARIANTS.md names in a
`(CRITICAL)` section, or a path word such as auth, migrations, alembic,
db, security, secrets or permissions. Its frontend folder (`frontend/`,
`web/`, … with a `package.json`, or the root) is noted when a frontend
file changed; `u` starts it through the repo's dev manifest.

### Loose task parsing

Accept both shapes. For each list item under a `##` heading:

- title = the line text minus checkbox, owner and thread suffix;
- done = `[x]`; list = the heading;
- thread ids = the threads the task belongs to: the ids in a strict
  `· t-0007` suffix, else every `t-\d{4}` on the bullet line, plus the
  suffix ids of sub-items in its notes (`  - [ ] Part · t-0005`). Other ids
  in the indented notes only mention another thread ("after t-0002 lands")
  and do not link it, or that thread's links would show on this task;
- links (from the line, its notes, and the task's threads' `.task.md`/`.md`,
  each file scraped on its own):
  - Linear: `\b[A-Z][A-Z0-9]{1,5}-\d+\b` → `https://linear.app/<workspace>/issue/<ID>`.
    There is no default workspace: it comes from `--linear-workspace` or
    `$HERDR_DECK_LINEAR_WORKSPACE`. With a Linear API key the URL comes from
    Linear instead (see "Linear issue status"). Without either, IDs still
    show but say a workspace must be set instead of opening. A branch name gives a ticket
    too (`dev/abc-123-users` → ABC-123). Word boundaries on both sides, so
    ABC-110 ≠ ABC-1100 and P-ABC-49 is not ABC-49. Bare IDs inside code
    (backtick spans, fenced blocks) and in a `## Remember` section are
    examples or lessons, so they do not link; a full Linear URL there still
    does.
  - Figma: `figma.com/(design|file|proto)/…` URLs; label them with the file
    name and `node-id`, and offer the `figma://` desktop rewrite.
  - Notion: `notion.so/…` URLs.
  - GitHub: PR/issue URLs, plus the thread's `pr` field.

Paragraph text that is not a list item is shown as the list's note, not as a
task.

## UI

One project per instance, narrow by default (~60–80 columns, full height).
A compact list on top and a detail drawer below; mockups of every state at
60 and 80 columns are in [docs/design/d-list-detail.md](design/d-list-detail.md)
(direction D, chosen 2026-10-02; the alternatives are in
[docs/design/](design/README.md)). The drawer was redesigned on 2026-10-03:
a header card and Overview, Files and Log tabs, mocked up with every state
in [docs/design/drawer/](design/drawer/README.md), whose *Decisions* section
lists what the user chose.

```
 Admin rebuild                           ◐ 2  ◇ 1  ○ 1  ✉ 0
────────────────────────────────────────────────────────────
    WORK                       STATUS           LINKS  DEV
  In progress
 ▸◐ Users page                 Building overv…  L4
  ◐ Templates page             Writing tests    L F    ●●●
  ◇ Document select for sum…   #2320 review     L F3   ●○

  + Backlog (6)
────────────────────────────────────────────────────────────
 Users page                                ◐ working  ~60%
 t-0002 · Building overview                     ▰▰▰▰▰▰▱▱▱▱
 Overview   Files 11   Log 7                      ↓ 2 more
 ── Note ──
 Phase 1 = layout + overview (ABC-1256); report on
 1257/1250 before starting them.
 ── Links ──
 [1 ABC-1246 done] [2 ABC-1256 in progress]
────────────────────────────────────────────────────────────
 1-9 link  l f n g o first  ↵ pane  [ ] tab  ? help
```

- **List.** One row per piece of work: a task joined with the threads it
  names, or a thread no task names. Rows are grouped under their TASKS.md
  list, with a blank line between two groups (folded ones too); the cursor
  and the mouse skip it. Columns at 80: work, thread, status, PR, links (`L4` = four Linear
  links), dev (one dot per dev.json port). At 60 the thread and PR columns
  fold into status.
- **Needs you on top.** Threads waiting on you move into a red *Needs you*
  group at the top of the list. A thread is waiting on
  you when herdr-projects says so, or when its agent has been `blocked` on a
  question or permission prompt for 30 s (milestone 5); an agent that is
  working, idle, done or waiting on its own sub-agents never counts. If the cursor has not
  been moved by hand, it jumps there so the drawer shows the thread's
  `next[]`.
- **Inbox items are news, not needs** (user, 2026-10-03). They never get
  rows. A thread with unhandled items (in `inbox/`, by `subject`) has its
  title in yellow, a folded list holding one shows a yellow `✉`, Overview
  starts with `✉ N updates · see Log`, and the items show in the thread's
  Log, marked `✉`. Items about no thread (routines, spaces) only count in
  the header's `✉ N`, which counts every unhandled item.
- **Folding.** `space` on a list heading folds or unfolds the list. A folded
  heading starts with `+` and shows its count (`+ Backlog (6)`); open
  headings have no marker. `▸` only ever marks the selected row. The lists
  named in `ui.folded_lists` start folded (default *Backlog* and *Resolved*;
  see Configuration), and a folded list still shows its threads' glyphs.
  Folding never hides a need, because those rows are pinned on top.
- **Drawer.** It shows the selected row, and takes half of the pane below
  the header (the list keeps the rest; it scrolls to keep the cursor in
  view). `z` cycles the drawer through that height, full height and hidden.
  Dragging the rule above it with the mouse, or `+` / `-`, resizes it
  (2026-10-05): the list keeps three rows, the drawer its card, tab bar and
  two lines. The height is a share of the pane, kept in the state folder
  (`drawer-height`), never in the config file; `ui.drawer_height` (default
  0.5, 0.2–0.8) is the height until the first drag.
  From the top:
  - **Header card**, two lines. Line 1: the row's title (bold); at the right
    a status pill, the status glyph plus a word (`● needs you`, `◐ working`,
    `◇ review`, `↻ landing`, `○ idle`, `○ no thread`, `✓ done`, `✉ inbox`),
    and the percent (`~95%`), both in the status colour. Line 2, dim: the
    thread id, the thread's own title when the row is a task (not at 60
    columns), the activity while working, the pane (not at 60), the owner
    of a task; at the right a ten-cell bar `▰▱` in the status colour, or,
    with no percent, the PR (`#2320` magenta, `✕ 2 failing` red or `✓`
    green). A task with several threads shows the most pressing one.
  - **Tab bar**, one line: ` Overview `, ` Files N `, ` Commits N `,
    ` Impact N `, ` Log N ` (` PR ` before Log with a PR) as plain
    labels with one space of padding on each side, on background colours,
    one space apart, no rule or brackets. The active tab is bold dark
    text (256-colour 234, 16 on a light terminal) on blue; inactive tabs plain text on dark grey (256-colour 237,
    254 on a light terminal). `Files N` counts changed files (`Files …`
    until git has answered once), `Commits N` the branch's commits,
    `Impact N` its Look here first items (`Impact ≡` for a pure move,
    `Impact …` before the first read; no tab with `arch.enabled =
    false`), `Log N` events. A tab with nothing
    behind it (no thread, a resolved thread's files) is dim, has no count
    and is skipped by keys and clicks. At the right end, dim, what is below
    the drawer's end: Overview's section names at 80 columns, else
    `↓ N more` lines.
  - **The tab's content**, which scrolls (`pgup`/`pgdn`, the wheel).

  The tab stays as the cursor moves through the list, except that the
  needs-you jump shows Overview, and a row without a thread shows Overview
  until the cursor is back on a thread. A list heading, or no
  row (an empty project), shows a card with the list's or project's name,
  no tabs, and the list's note, the goal, repos and folder as today.
- **Overview tab.** Titled sections, each a dim `── Name ──` rule with the
  name bold, in this order, empty ones left out, separated by a blank line
  only when they all fit:
  - *Next*: the thread's `next[]` lines after a bold `→`; the rule's name
    red while the thread needs you.
  - *PR*: `[5 #2320] open · review required · 2 comments (sam, alex)`, then
    `✕ N failing: lint, test` (red) or `✓ no failing checks` (green), and at
    full height `checked 1m ago` (ticker `last_pr_check`). Review text:
    required magenta, approved green, changes requested red. The PR's chip
    is here and never also in *Links*.
  - *Note*: the task's notes.
  - *Links*: every Linear, Figma, Notion and other GitHub link as a chip,
    `[1 ABC-1246 done]`: dim brackets, bold digit, label in the kind's
    colour, a Linear issue's state in its state colour. A run
    of one kind shows the kind word on its first chip only
    (`[2 Figma 598-48083] [3 1138-88367]`); a closed issue's chip is dim.
  - *Thread*: dim labels for metadata: `pane` with the agent's live state,
    `branch`, `report` (changed time, `r shows it`), `base` and merge-base,
    `log` while the deck runs an `up` command. A task with several threads
    lists each.

  Above the first section, without a heading, the manifest's localhost
  links are chips with their port and dot, `[6 Frontend :5174 ●]` (since
  2026-10-20; they used to sit in a *Dev* section with a line per server,
  between *Links* and *Thread*). A thread without them gets one dim line
  there saying why, the most relevant reason only: `worktree not found`
  or an invalid manifest (`· ! shows why`) first, then the dev command a
  tool started (`starting…`, `dev exited · see the log`), then
  `not started · u starts it`, `no .config/dev.json` or `ports not known
  yet`.

  Links are numbered `1`–`9` across sections in the order Linear, Figma,
  Notion, GitHub, localhost, wherever their chip sits; the numbered links
  replace a separate chooser.
- **Files tab.** The selected thread's changed files
  (`internal/source/diff`): its worktree against the merge-base with the
  thread's `base` (else `origin/HEAD`), `git diff --raw --numstat -z -M
  <merge-base>` (the raw records give each file's `deck.Change`) plus
  untracked files from `git ls-files --others --exclude-standard`. A total
  line (`11 files  +447 -68  vs origin/main`, the view and `t` at its
  right), then one row per file as a diffstat: digit, a status letter
  coloured by its change (`A` green, `M` yellow, `D` red, `R` cyan, `?`
  untracked faint green; faint when binary), the path (losing its start
  when long; a rename folded git-style, `pages/{members → users}/index.ts`),
  green `+N` / red `-M` counts (only `+N` for an added or untracked file,
  only `-M` for a deleted one, `binary` for a binary file), and a bar of 8
  cells at 80 columns, 5 at 60: cells ∝ added plus deleted lines, scaled to
  the thread's largest file, at least one cell for any change, green cells
  for added then red for deleted, the rest a dim `▁`; no bar for a binary
  file. Files stay in path order. `t` switches between this list and the
  folder tree (`internal/ui/filetree.go`): folders first, single-folder
  chains joined (`src/pages/users/`), folder names and their summed counts
  faint and without a bar; files numbered in display order. `diff.view`
  (list or tree, default list) sets the view at start; the toggle never
  writes the file. Every file is listed and the tab scrolls; files 1–9 take
  the digits, the rest show a dim `·` and open by click or the drawer
  cursor. Git runs off the UI goroutine (5 s timeout,
  `GIT_OPTIONAL_LOCKS=0`) when the selection moves to another thread and
  on every reload; answers are reused for 2 s. A missing worktree or base
  is a dim note.
- **Diff preview.** The Files tab's default action (t-0044): `↵`, a digit
  or a click on a file, or `v` for the file under the cursor, focuses the
  tab and shows that file's diff in the list's place: the column-titles
  line becomes a header (the path, losing its start when long, `+N −M`,
  `untracked`, and at the right the lines shown, `1–11/19`), the list's
  lines the diff. The drawer keeps Files, so `j`/`k` or a click pick
  another file; a new file
  starts at its top. `diff.Reader.ReadPatch` runs `git diff --no-color
  --no-ext-diff --no-textconv -M <merge-base> -- [old] <file>` with the
  Files tab's 5 s timeout and optional locks off, keeps 2000 lines (and
  4 MB) and counts the rest (`… K more lines`); an untracked file is read
  as all added (`@@ -0,0 +1,N @@`), a binary file is a one-line note, a
  pure rename "renamed, content unchanged". Answers are cached by file,
  merge-base and a hash of the file's content (64 entries). The read runs
  off the UI goroutine when the preview moves to another file and on every
  reload; `internal/syntax` colours the code lines in the same goroutine
  with chroma's lexer for the file name (named ANSI colours: keywords
  magenta, strings yellow, numbers and types cyan, functions and tags blue,
  comments dim), plain when no lexer matches. Added lines get a green bold
  `+`, removed a red bold `−`, both a true-colour tint (dark: 22,54,33 /
  64,26,31; light: 222,250,228 / 255,228,226) across the line; hunk headers
  and `\ No newline` notes are dim. Tabs are four columns, control
  characters `�`, long lines cut with `…`. `J`/`K` scroll a line,
  `pgup`/`pgdn` and `ctrl+u`/`ctrl+d` a page, `home`/`end`, the wheel over
  it three lines. A diff longer than the preview gets a scrollbar in its
  last column (t-0067): see Mouse below. The preview needs the Files tab focused at the normal
  drawer height on the thread it started on: `v`, `esc`, another tab, row
  or full view, or `z` ends it, and the list shows again with its cursor
  and scroll untouched.
- **Split layout** (t-0049). `S` (or `|`) switches the preview between
  unified and split for the session; `diff.layout` (`unified` | `split`,
  `--diff-layout`, `HERDR_DECK_DIFF_LAYOUT`, a cycling choice on the
  settings page) is the start. Split (`internal/ui/split.go`): old left,
  new right, each side a right-aligned line number from the hunk header
  in a fixed grey (256-colour 238 dark / 252 light, so the code
  dominates), the sign, the code (tinted as in unified, cut with `…` per
  side, a blank column before the `│` divider in 240 / 250). `pairLines` lays the patch out once
  per read, off the UI goroutine: each run of removed lines pairs with
  the added lines after it, filler on the shorter side; context on both
  sides; hunk headers and a commit's file rules span both; a
  `\ No newline` note goes on the side of the line before it (both after
  context). A file whose hunks all start at `-0,0` (added, untracked) shows
  only its new side across the width, one whose hunks all end at `+0,0`
  (deleted) only its old side. The split needs a preview of at least 100
  columns (`splitMinWidth`: two sides of 49, about 40 of code); narrower,
  it draws unified with `split needs ≥ 100 cols` in the header, and
  switches back on a resize. `prevOff` counts rows of the layout shown;
  a layout change maps it through the row that shows the same patch line,
  so the same hunk stays in view.
- **Commits tab** (t-0044). The thread branch's own commits:
  `diff.Reader.ReadCommits` runs `git log --numstat <merge-base>..HEAD`
  (100 at most, `rev-list --count` for the rest) in the worktree, against
  the Files tab's merge-base, newest first. A row is the digit (dim `·`
  after 9), the short sha dim, the subject, the age dim and `+N −M` green
  and red, the counts right-aligned in one column; a merge commit is dim
  with `⋔` and no counts; `+K more` ends a longer branch. With uncommitted
  changes (`git status --porcelain -z --untracked-files=all`) a first row
  `● uncommitted · N files  → Files` leads to the Files tab. The label
  counts the commits. A resolved thread or one without a worktree shows a
  dim note. The list is cached by HEAD and upstream sha (and for 2 s), so
  a reload re-runs only `rev-parse` and `status` until the branch moves;
  `merge-base HEAD @{upstream}` marks the commits the remote has. `↵` or a
  digit previews a commit in the diff preview: a header with the short
  sha, subject and counts, then author, date and body, then `git show
  --diff-merges=first-parent` file by file under `── path ──` rules, each
  coloured by its language (`ReadCommitPatch`, cached by sha). In the
  focused drawer `d` opens the commit in the diff tool (`launch.CommitArgv`:
  `{base}` = `sha^`, the sha inserted after a lone `{base}`, so `hunk diff
  sha^ sha`) and `g` the commit inside the PR on GitHub
  (`…/pull/N/commits/<sha>`, with tab reuse; an unpushed commit says so).
  From the list, `d` and `g` keep their meaning. In the focused drawer,
  `space`/`→`/`l` (or a click on the `▸` marker after the digit) expands a
  commit to its files and `space`/`←`/`h` collapses it (also from one of
  its files); several may be open. File rows are styled as on Files
  (status letter, path, `+N −M` with `−`, list or tree by `diff.view`),
  indented under the commit, unnumbered (the digits stay the commits').
  `ReadCommitFiles` (`git show --format= --raw --numstat -z -M
  --diff-merges=first-parent`, parsed as the Files tab's diff) runs off
  the UI goroutine on the first expand and is cached by sha. `↵`/click on a
  file previews `git show <sha> -- [old] <path>` (`ReadCommitFilePatch`),
  `d` opens it at that commit (`CommitArgv` with files). The cursor keeps
  its row (sha and path) when rows open, close or arrive above it.
- **Impact tab** (t-0072, from the exploration in
  [docs/research/architecture-tab.md](research/architecture-tab.md)).
  What the branch did to the repository's shape. `internal/source/arch`
  reads the merge-base (with the thread's base, as Files) and `HEAD` from
  git objects (`ls-tree`, one `cat-file --batch`; no checkout, optional
  locks off), parses Go with `go/parser` and TypeScript/JavaScript with an
  import scanner (comments blanked; relative paths, `index` files,
  tsconfig `paths`, workspace packages), builds each commit's package
  graph (a package is a folder), and diffs them: edges added, removed and
  changed (same importers, different uses), layer verdicts, component
  cycles, touchpoints (env, http, sql, exec, fs, flag), go.mod /
  package.json requirements, exported API, and declarations moved
  (matched by hash) so a move is not a change. Facts are cached by blob
  SHA and results by merge-base, head and the manifest's blob (16), so a
  reload costs `rev-parse HEAD`, `merge-base` and `rev-parse
  HEAD:.config/dev.json` until one of them moves. It runs off the
  UI goroutine like the Files read (30 s timeout). The repository's
  `.config/dev.json` at HEAD may declare `x-herdr-deck.architecture`:
  `layers` (ordered, `paths` globs, `closed`), `roots`, `language`;
  without layers they are inferred by depth and flag nothing. The tab
  shows a summary line, *Look here first* (5 rows at the normal height,
  12 at full: upward, cycle, skip, new dependency, touchpoints, new
  edges, API; each with its `file:line`), then the canvas (§12 of the
  research): impacted packages as boxes nested in their folders
  (unchanged ones counted as `⋯N`, plain-folder chains collapsed),
  packed in rows; titles coloured by change, borders neutral; changed
  edges between drawn boxes numbered on the source's bottom and the
  target's top border in their status colour (green added, yellow
  changed, red removed; `!` upward or skipping), counts (`▾N` `▴N`) past
  20; a legend beside the canvas from 110 columns, else below, with `─▸`
  for every status but removed `┄▸`. Uncommitted changes are left out.
  The focused tab's cursor stops on the findings (each selects its box),
  then the boxes in reading order, then the selected box's detail rows;
  on a box the arrows and hjkl step to the nearest box that way (along
  plus twice across, `canvas.step`). The selection redraws the same
  geometry with a heavy border, routes its edges (changed ones, then
  unchanged up to six) through free cells with a shortest-path search
  (crossing a border or line costs extra; borders and earlier lines stay
  whole; only the arrow point goes on the target's border; crossing into
  a box that is neither end nor holds one costs more than any route that
  avoids such boxes, so lines go around siblings through the gaps and
  through one only when nothing else connects), dims boxes
  at no end, and swaps the legend for the detail. Canvases are cached by
  result, width and selection (8). A click selects a box or finding and
  a second click acts. `↵` opens the site in the diff preview at its line
  (`previewGoto`: the patch line whose new, or old, number matches, three
  rows of context above). `O` runs `exec herdr-deck arch --project <slug>
  --thread <id>` (plus the deck's setting flags; its `HERDR_DECK_*`,
  `HERDR_PROJECTS_ROOT` and `XDG_*` variables go in the split's `env`)
  in a pane split below the deck, focused and zoomed (`pane.split`,
  `pane.zoom` mode on, `pane.send_input`); the pane
  (`ui.ImpactPane`) reads the thread from herdr-projects' list and opens
  files in the diff tool. Edges (t-0072, third PR): the detail's import
  rows are cursor stops (`actEdge`), and the edge under the cursor is the
  selected one (`Model.iedge`). Routes do not depend on it: each line
  cell records its edge, and the selected one is drawn heavy at render
  (`━┃┏┓┗┛`, `┅┇` removed, bold). A click hit-tests the cells, then the
  four neighbours, and selects the edge with its source box. `n`/`N`
  (and `]`/`[` on an edge) cycle edges, `tab`/`.` sites, `esc` steps back.
  The main view shows the site's code (`edgeview.go`): the file's patch
  cut to the hunk holding the import line (its old side for a removed
  edge), else `diff.Reader.ReadFileAt` (`git show rev:path`) around it
  with `import unchanged`; drawn with the preview's row renderer, the
  import row marked `▌`, read off the UI goroutine and cached by site. A
  changed edge's header lists the names gained and lost (`Edge.Gained`,
  `Lost`). The pane splits: canvas above, code below (beside from 180
  columns). The first call site of the imported name is not used yet:
  the site is the import.
- **Log tab.** The thread's timeline, newest first, one line per event:
  age (dim, right-aligned), a glyph in its colour, the text, and at 80
  columns the clock time (dim, with the weekday before today). Only what
  herdr-projects records: `created`, `launched_at` and `brief_seen_at` from
  the thread file, and inbox items whose `subject` is the thread, from
  `inbox/` and `inbox/done/` (read-only; read only files whose name holds
  the thread id, cached by name), by their `event`: `new report` `≡`,
  `waiting on you` and `blocked on a prompt` red `●`, `PR opened` / `PR
  updated` magenta `◇` with the PR's chip, `PR checks failing` red `✕`, `PR
  merged` green `✓`, `resolved` dim `✓`, `prompted its thread` (routine)
  dim `»`; created `+` and launched `▶` dim. An item still in `inbox/` ends
  in a yellow `✉`. At full height a dim rule heads each day, a dim `│`
  marks a gap of over an hour, and a dim last line names the sources. No
  git commits, no earlier working/idle changes (only `last_state_change` is
  kept), no percent history. A routine's item (`inbox/*-routine-*.md`)
  counts for the thread its summary names.
- **New reads for the drawer:** the thread's `percent` as a number (may be
  `null`), its `created`, `launched_at`, `brief_seen_at`,
  `last_state_change`, `last_report_change` and `resolved_reason`, and
  ticker.json's `commenters[]` and `last_pr_check`.
- **Missing sources.** `! N` in the header (yellow), and `!` shows a
  *Sources* view in the drawer. Stale rows are dim with `as of HH:MM`, and
  fallback ports are marked `~`.
- **Other projects** (milestone 8). `p` opens the project picker over the
  deck, only on demand: every project under the projects root (folders with
  a PROJECT.md, as `herdr-projects list` finds them), each with its name,
  status (`here` marks the deck's own; paused dim; archived only after
  `tab`), its thread counts and a dim `✉ N updates`, and under it the
  threads waiting on the user with their age. Inbox items are progress
  updates (a report, a PR opened or merged, resolved), not needs (user,
  2026-10-03): they never count, and an item about a waiting thread counts
  only through the thread. Projects with a waiting thread come first, then
  the rest by name. Typing filters by name or
  slug; the arrows move, `esc` clears the filter, then closes. `↵` on a
  project focuses its coordinator (`pane.focus`; herdr 0.9.3 switches to the
  pane's workspace and tab with it, verified on a scratch server), or, with
  none running, runs `herdr-projects --root <root> open <slug> --tab`, which
  starts one in the project's workspace and focuses it (`--tab` keeps it out
  of the deck's own pane; an archived project starts nothing). `↵` on a
  waiting thread focuses its pane. When threads in other projects wait on
  the user, the list's last line says `● N other projects need you`; a
  click on it opens the picker. Other projects' threads never join the
  Needs you group. Data: `projects.Roster` reads `threads/t-*.toml`, `inbox/`,
  `.state/project.json` and `.state/coordinator.json` of every project
  (never `thread list`, never `ticker.json`) at most every 20 s, on the
  load goroutine; herdr's reader lays the snapshot it already read over them
  on every reload (same 30 s blocked rule). Only the deck's own folder is
  watched. The deck's own project comes from its snapshot.
- **Threads no task names** (built in milestone 4) get their own rows under
  *Other threads* after TASKS.md's lists; resolved ones go to a *Resolved*
  list that starts folded like *Backlog*. A task naming several threads is
  one row showing the most pressing thread; the drawer lists them all.

Keys:

- `j`/`k` move (the drawer follows), `space` folds a list.
- `[`/`]` switch to the previous / next drawer tab, from the list or the
  drawer. `tab` moves the focus into the drawer: the list's `▸` turns dim
  and a drawer cursor (`▸` and the selection background) appears; inside
  the drawer `tab`/`shift+tab` switch tabs, `j`/`k` move the drawer cursor,
  `enter` acts on the item under it (Overview: opens the link; Files:
  previews the file, numbered or not; Commits: previews the commit; Log: a report event shows the report,
  a PR event opens the PR, other events focus the pane), and `esc` returns
  the focus to the list. A click in the drawer focuses it too.
- `1`–`9` open the drawer's numbered links on Overview and Log, and
  preview the numbered file or commit on Files and Commits (without a
  preview reader, a file opens in the diff tool). `l` Linear, `f` Figma,
  `n` Notion, `g` GitHub PR open the first link of that kind, on any tab. When there
  are several, the key highlights that kind's chips and waits: a digit opens
  one, `a` opens all of that kind, `d` opens a Figma link in the desktop
  app, the same letter again opens the first, and `esc` cancels. With
  `figma.desktop` set (milestone 8) every Figma link the deck opens, by
  key, digit or click, opens in the desktop app. `o` opens
  the first localhost link whose dev server is running, without a chooser
  (milestone 6): links to servers that are down open only by their digit.
- `enter`: on a thread row, focus its herdr pane (`herdr pane focus
  <pane_id>`).
- `e`: open the worktree in the configured editor, `code {path}` by
  default. Milestone 4 left `$VISUAL` out because a terminal editor would
  take over the deck's own pane; since the config file (below), an editor
  with `terminal = true` opens in a new herdr pane instead. `$VISUAL` and
  `$EDITOR` are still not read implicitly. `r`: the
  thread's report in the drawer at full height, under the thread's card,
  with `Report · esc returns` where the tab bar was. `!` sources, `?` help,
  `s` settings and `w` What's new replace the whole drawer (today's title
  rule, no card or tabs); `esc` returns to the tab you were on.
- `d`: focuses the drawer's Files tab (unhiding the drawer), so `d 3`
  previews file 3. In the focused Files or Commits tab, `d` opens the file
  or commit under the cursor in the diff tool, so `d d` from the list
  opens the first file there (t-0044: the preview became the default and
  the tool an explicit key in both tabs). `a` on Files or Commits opens
  the whole diff in the tool, `v` turns the preview on and off (see Files
  tab), `t` on Files switches list and tree. A click on a file previews
  it, on the total line opens the whole diff. The
  diff tool gets the merge-base commit as `{base}`. A rename passes both
  paths (a lone `{file}` argument becomes one per file) so git pairs them;
  an untracked file does not open, since git diff leaves it out.
- `u`: run the manifest's `up` command for the worktree, detached (own
  session and process group), logging to
  `$XDG_STATE_HOME/herdr-deck/logs/<slug>-<thread>.log` (else
  `~/.local/state/…`), with a `.pid` file beside it so a restarted deck
  still knows the command runs. Not started again while a manifest port
  answers or that pid is alive; the drawer's *Thread* section shows the
  log, Overview `starting…` while there are no links yet, and the DEV
  column a yellow `◐` until the ports answer.
- `p`: the project picker (see Other projects above).
- Open URLs with `open` (macOS) / `xdg-open`. Over SSH, or on Linux with no
  display, copy them with OSC 52 instead (herdr 0.9.5 has no API that opens
  a URL on the client; it forwards a pane's OSC 52 to the client).

Mouse: a click selects a row, a click on a tab switches to it, a click on a
chip, file or Log event opens it, and clicks on `! N` and on a list heading
work like their keys. A click on the card does nothing. The wheel scrolls the
list or the drawer's tab under the pointer.

Scrollbars (t-0067): the diff preview and full views (report, What's new,
help, settings, check logs) that overflow get a one-column scrollbar at the
right edge: a `│` track and `┃` thumb in fixed 256-colour greys (like the
split diff's line numbers; named colours have no shade this faint), the
thumb blue while dragged. The preview gives up its last column; a full view
two (a blank one, then the bar). A press on the thumb grabs it and a drag
scrolls; a press on the track jumps there and the drag goes on; a key or the
release lets go. A row's drawer tabs keep `↓ N more` instead (follow-up),
except the PR and Impact tabs, which are long like a report: their
content gets the scrollbar too, and the card and tab bar keep the full
width.

Colours are named ANSI colours, so the terminal theme applies (full table in
[docs/design/README.md](design/README.md#colours)). Needs you is bold red,
inbox items yellow, working cyan, review magenta, landing, passing checks and
listening ports green, and idle and metadata dim. The selected row (and the
link chooser's line) has a subtle grey background, ANSI 256 colour 237, or 254
on a light terminal, under the text's own colours. The drawer's active tab is
bold near-black text on blue, the one palette colour no status uses; inactive tabs
sit on the selection's grey. The full drawer colour table is in
[docs/design/drawer/](design/drawer/README.md#colours). The deck never marks
inbox items handled.

## herdr integration

Ship a `herdr-plugin.toml` in the repo root. Verified in milestone 7 against
herdr 0.9.3, on a separate headless herdr server (`HOME=<scratch> herdr
server` with its own `HERDR_SOCKET_PATH`: plugins, config and sockets then
live under that home, apart from the user's own herdr):

- `[[build]]`: `go build -ldflags "-X main.version=<version>" -o
  bin/herdr-deck ./cmd/herdr-deck`. argv has no variables, so the version is
  written twice in the manifest; a test fails when they differ.
- `[[panes]] id="deck" placement="split"`, command = `bin/herdr-deck`. herdr
  runs plugin commands in the plugin folder, so the hooks open the deck with
  `cwd` = the project folder and `HERDR_DECK_PROJECT` (and
  `HERDR_PROJECTS_ROOT`) in its environment. A plugin pane's `HERDR_PANE_ID`
  is its own pane.
- Deck panes are marked with the pane token `herdr_deck=<slug>` (source
  `herdr-deck`): the hook sets it right after opening, and the deck sets it
  on its own pane at start. herdr's state has no plugin-ownership field (a
  plugin pane only has the manifest title as `label`).
- `[[actions]] id="toggle"`, `contexts=["workspace"]` runs `herdr-deck
  plugin toggle`. On the focused pane (`HERDR_PLUGIN_CONTEXT_JSON`'s
  `focused_pane_id`): when it is a deck, close it and focus the pane left of
  it (`pane.neighbor` before the close, then `pane.focus`; herdr picks when
  that fails); else when its tab has a deck, focus that deck; else open a
  focused deck to its right for the project of its folder, its `hp_project`
  token, or `$HERDR_DECK_PROJECT`; with none it shows a herdr notification.
  The auto-open hook never takes the focus. Bind it
  with `[[keys.command]] type="plugin_action" command="herdr-deck.toggle"` —
  **ask the user before editing their herdr config**.
- `[[events]] on="pane.agent_detected"` runs `herdr-deck plugin
  agent-detected`. It fires when herdr sees an agent start in a pane, also
  one started by typing in an existing shell; `workspace.created` fires
  earlier, while the pane is still a shell, so it can't tell a coordinator.
  The hook gets the pane in `HERDR_PANE_ID` and
  `HERDR_PLUGIN_EVENT_JSON` = `{"event":"pane_agent_detected","data":{"pane_id",
  "workspace_id","agent"}}`. It opens a deck when the pane's cwd is exactly
  `<root>/<slug>` with a PROJECT.md (symlinks resolved: herdr reports
  physical paths) and no pane of the workspace has the deck token. Threads
  run in worktrees or `<slug>/threads/…`, so only coordinators match. A file
  lock in `HERDR_PLUGIN_STATE_DIR` keeps two hooks from both opening one.
- Sizing: `plugin.pane.open` takes no ratio, so the hook finds the new split
  in `layout.export`'s tree (its path is a list of booleans, `true` = second
  child) and calls `layout.set_split_ratio`. The deck gets 40 % of the
  split, 60–80 columns, and the left pane keeps at least 60; below that they
  share it evenly.
- `[[link_handlers]] id="figma"` sends Ctrl+clicked Figma URLs to the
  `open-link` action (`herdr-deck plugin open-link`), which opens the
  `figma://` rewrite when `figma.desktop` is set, else the URL, as the deck
  itself does. Verified in
  milestone 8 on herdr 0.9.3: a link handler only ever sees a URL. herdr
  turns text into a link only for `http://`/`https://` (`url_byte_range` in
  its `src/app/actions.rs`), or takes an OSC 8 hyperlink's URI, and only on
  a Ctrl+click. A bare `ABC-123` is never a link, so Linear IDs go through
  the same action on a selection instead: a `[[keys.command]] type =
  "plugin_action"` key passes the selection as `selected_text`, and the
  action opens its first link or Linear ID with the deck's scraper rules.
  With no Linear workspace it asks Linear for the API key's workspace
  (`organization { urlKey }`); with neither it opens nothing, shows a herdr notification and
  logs why. herdr re-reads a linked plugin's manifest on each click, so a
  new handler needs no relink and no `reload-config`; the binary must be
  rebuilt for a new subcommand.

Install with `herdr plugin install FredricW/herdr-deck`, or `herdr plugin
link <checkout>` for development (link never builds); see the README.

## Configuration

Settings live in `$XDG_CONFIG_HOME/herdr-deck/config.toml`, else
`~/.config/herdr-deck/config.toml` (macOS too, not Application Support);
`--config` or `$HERDR_DECK_CONFIG` names another file. Each setting comes
from the flag, else the environment variable, else the file, else the
default (`internal/config`). Only the settings page writes the file, and
never a secret.

- One TOML table per area (user, 2026-10-04): `[ui]` refresh_interval (Go
  duration, 1s–10m, default 5s), folded_lists, drawer_height (0.2–0.8,
  default 0.5); `[projects]` root;
  `[linear]` workspace, status, api_key_command; `[figma]` desktop;
  `[browser]` reuse_tabs (default true); `[updates]` check, auto_restart;
  `[editor]` command, terminal; `[diff]` command, terminal, view (list or
  tree, default list), layout (unified or split, default unified). The dotted path (`linear.workspace`) is the one name
  in docs, the settings page, notes and errors (`Spec.Key`). Flags are
  `--<table>-<key>` and env vars `HERDR_DECK_<TABLE>_<KEY>`; only
  `projects.root` keeps herdr-projects' `HERDR_PROJECTS_ROOT`, and
  `LINEAR_API_KEY` stays as it is. Every setting has a flag, registered
  from `config.Specs` (`Flags.Register`). The README's settings table is
  generated from the Specs by a test (`-update` rewrites it).
- Earlier names stay as aliases (`Spec.Old`, `FlagAliases`, `EnvAliases`):
  the flat file keys (`linear_workspace`, …, `diff_view`), the old flags
  (`--diff-tool`, `--editor`, `--refresh-interval`, …, hidden from help)
  and env vars (`HERDR_DECK_DIFF_TOOL`, `HERDR_DECK_EDITOR`, …). A current
  name always wins over an earlier one; in the file the table's value wins
  and the other is ignored. Each flat key in the file gives a dim note in
  the `!` view (`Settings.Notes`), and a bad value is reported under the
  name the file uses. Saving a setting on the settings page moves its
  flat key into the table with the comments above and after it (not the
  file's opening comment). `herdr-deck config migrate` moves them all: a
  dry run prints a diff, `--write` writes it and keeps `<file>.bak`.
- `[editor]` and `[diff]` are a command plus `terminal`. Commands are split
  into argv like a POSIX shell but never run through one. Placeholders:
  `{path}` (both; appended to the editor when absent), `{base}` and `{file}`
  (diff; without a file a lone `{file}` argument and a `--` before it are
  dropped). A terminal program opens in a new herdr pane below the deck
  (`pane.split` with `cwd` = the worktree, then `pane.send_input` types the
  quoted command and Enter); outside herdr the status line says it needs
  herdr. The source that gives a command also decides its `terminal`, or a
  higher one does.
- The diff tool (`config.Settings.Diff`, `launch.DiffArgv`) opens from the
  Files section (`d`, see UI). Default `hunk
  diff {base} -- {file}` (working tree against the base ref, uncommitted
  changes included) when `hunk` is on PATH, else `git -C {path} diff
  --merge-base {base} -- {file}`, which pages itself in the pane.
- A missing file is silent. A file that does not parse is ignored as a
  whole; an unknown key or bad value is skipped on its own. Either way the
  problem is listed in `Snapshot.Missing` (the `!` view) or, for the plugin
  commands, on stderr (herdr's plugin log). A bad flag value is an error.
- Plugin mode: the hooks resolve the projects root from the same file and
  pass `$HERDR_DECK_CONFIG` on to decks they open when it is set; the deck
  finds the default path itself.
- `[arch]`: `enabled` (default true; false leaves the Impact tab out) and
  `tests` (default false; count test files in its graphs), both read at
  start.
- `linear.status` (default true) and `linear.api_key_command`: see
  "Linear issue status" below.
- `ui.folded_lists` (`[ui] folded_lists`, flag `--ui-folded-lists`, env
  `$HERDR_DECK_UI_FOLDED_LISTS`; default `["Backlog", "Resolved"]`): the list headings
  that start folded, matched whole and without regard to case; `[]` folds
  none. One setting covers TASKS.md's lists and the deck's own groups
  (Resolved, Other threads), since the user sees them all as list headings
  and folds them the same way; Needs you never folds. The flag, env var and
  settings page take comma-separated names, `none` for an empty list
  (`KindList`; saved as a TOML array). A change applies at once to the
  lists the user has not folded or unfolded by hand this session; manual
  folds stay. Whole-heading matching replaced the earlier hard-coded
  "heading contains backlog" rule.
- `config.Load` decodes every table one key at a time (`decodeTable`), so
  a bad value or unknown key in one is skipped on its own.
- `updates.check` (the header's update hint) and `updates.auto_restart` (re-exec
  when the binary is replaced), both on by default. New keys are new
  optional fields, so an older deck only reports a newer file's keys as
  unknown.
- Settings page (`s`, `internal/ui/settings.go`): a full-height drawer view
  listing `config.Specs` by table (`config.Tables`), under their dotted
  names, each with its effective value
  and source (`Settings.Values`, recorded by `Resolve` as it settles each
  setting). `↵` toggles a boolean, cycles a choice (`KindChoice`, its values in
  `Spec.Choices`, such as `diff.view`) or edits a value in place (a list
  as comma-separated names), checked by
  `config.Check` as you type; `x` removes the key from the file. A save goes through `config.Save`: it patches only
  that key's line (comments, order and unknown keys stay; a new key goes
  after the last key of its table, a new table at the end), refuses a file
  that does not parse or an inline `editor = {…}` table, decodes the result
  to check only that key changed, and writes a temp file renamed over the
  old one (through a symlink, keeping the mode). A value set by a flag or
  env var still saves to the file; the page and status line say the
  override wins. After a save the deck resolves again and applies it at
  once: the UI takes the refresh interval, Figma desktop, the folded lists
  and the update hint, and `main` swaps an `atomic.Pointer[config.Settings]` that opening
  links, the editor and the diff tool, the Linear workspace and reader
  (rebuilt when the key command changes), the update check and
  auto-restart read each time.
  Only `projects.root` needs a restart, and the status line says so.
  `linear.api_key_command` is shown and edited as a command; the key it
  prints is never run, shown or written.

## Updates

Option B of the research doc (option A until 2026-10-03): releases carry
prebuilt binaries, and herdr's build step uses them when it can.

- `.github/workflows/release.yml` (a plain workflow, not GoReleaser): a
  `v*` tag push checks the tag against the manifest (`check-version.sh`),
  builds darwin and linux on arm64 and amd64 with `CGO_ENABLED=0` and
  `-X main.version=<tag>`, packs `herdr-deck_<version>_<os>_<arch>.tar.gz`
  (binary and README) with `SHA256SUMS`, and creates the GitHub Release with
  the tag's CHANGELOG.md section as its notes (`scripts/release-notes.sh`),
  falling back to `scripts/changelog.sh` (first-parent PRs and commits since
  the previous tag) for a tag the changelog does not cover. Only the publish job gets `contents: write`. Pull requests
  that touch the release files, and a manual run with `dry_run` on, only
  build and keep the files as a workflow artifact; a manual run with a tag
  and `dry_run` off publishes a tag that has no release yet. Artifact
  attestations are left for later.
- `herdr-plugin.toml`'s `[[build]]` runs `scripts/build.sh`. In a detached
  checkout with no local changes whose commit is the `v<manifest version>`
  tag (asked of origin, since herdr's shallow checkout has no tags) it
  downloads that release's archive for the machine, checks it against
  `SHA256SUMS`, checks that `--version` names the tag, and renames it to
  `bin/herdr-deck`. Anything else (a branch checkout such as the linked dev
  checkout, local changes, another commit, no release, no network, a
  mismatch) builds from source with `go build`, stamped with `git describe`
  or else the manifest's version. `HERDR_DECK_BUILD=source` forces a source
  build; `HERDR_DECK_DOWNLOAD_URL` points at another release host (tests).
  `scripts/test-build.sh` covers these cases against a fake release and a
  fake `go`, in CI's version job. `make build` is unchanged.
- Cutting a release: in a PR, bump `version` in `herdr-plugin.toml` and
  move CHANGELOG.md's Unreleased items into a `## [X.Y.Z] - date` section;
  merge, then push the `vX.Y.Z` tag on that merge commit.
  `check-version.sh` fails a tag without a CHANGELOG.md section (it skips
  the check in a checkout with no CHANGELOG.md, such as v0.1.0's).
- CHANGELOG.md (Keep a Changelog) is the one source of release notes. The
  deck embeds it (`go:embed` in the root package `herdrdeck`) and
  `internal/changelog` parses it into releases: a section it cannot read is
  left out and named as a problem (a test keeps the repo's file clean).
  `w` toggles What's new in the drawer: the releases newest first, each
  release's summary and sections rendered as Markdown (see Stack), the
  running one marked (a release build is its release; a build past one,
  a git describe or a bare commit, is Unreleased, which a release build
  does not show). `$XDG_STATE_HOME/herdr-deck/last-version` (else
  `~/.local/state/herdr-deck/`) records the last X.Y.Z a deck ran; a deck
  that starts on a newer one says `Updated to vX.Y.Z · w what's new` in the
  footer until the first key or click. The first run, a bare commit and a
  downgrade say nothing. While `↑` shows, the check also reads the newer
  version's CHANGELOG.md (`Updater.Changelog`: GitHub's raw host at the
  tag, or `git show <origin head>:CHANGELOG.md`, else the fetched origin
  branch, for a linked checkout, which is read again on the next check),
  once per newer version. What's new lists its Unreleased and its releases
  newer than the running one, without items this build's changelog already
  lists. Failures leave the list out, silently.

- `herdr-deck update [--check]` (`internal/update`) reads `herdr plugin list
  --plugin herdr-deck --json`. A GitHub install compares the newest `vX.Y.Z`
  tag (`git ls-remote --tags`) with the installed version and runs `herdr
  plugin install <repo> --ref <tag> --yes`. A linked checkout compares
  origin's default branch (`git ls-remote --symref origin HEAD`) with the
  installed commit; it pulls (`--ff-only`) only on that branch with no tracked
  changes, then builds to a temp file in `bin/` and renames it over
  `bin/herdr-deck`. `--check` only reports. "Installed" is the running
  binary when it is the plugin's `bin/herdr-deck`, else what that binary's
  `--version` says (the command may be typed in a shell running another
  build).
- The header hint (`↑ <tag or commit>`) comes from `update.Checker`: one
  cache file shared by all decks (`$XDG_CACHE_HOME/herdr-deck/update.json`,
  else `~/.cache/herdr-deck/`), so the remote is asked at most once an
  hour. The check runs off the UI goroutine at start and hourly; failures
  show only in Sources. Git never prompts (`GIT_TERMINAL_PROMPT=0`, ssh
  `BatchMode`).
- Restart in place (`internal/restart`): the binary path is resolved once
  at start. On each refresh tick the deck stats it; a file that differs
  (inode, size or mtime), has stayed the same for 2 s and answers
  `--version` makes Bubble Tea quit, then `syscall.Exec` runs it with the
  same args and env (same pid, so the herdr pane stays). If Exec fails the
  old deck starts again and the header says `↻ restart failed`. Not on
  Windows.

## Linear issue status

The drawer's Linear chips show each issue's state next to its ID (`1
ABC-123 in progress`), coloured by Linear's state type: started cyan (a
name with "review" magenta), triage yellow, unstarted and backlog plain,
completed and canceled dim, the ID too. The list's LINKS column keeps its
`L4` badge: six columns have no room for states or titles.

Titles, assignees and URLs (L1 in
[research/integrations-linear-github.md](research/integrations-linear-github.md),
built 2026-10-04): an issue Linear answered for gets a line of its own in
the Overview's *Links* section, `[1 ABC-123 Fix login · in progress · ana]`,
shortened to the drawer's width in this order: the assignee becomes their
initials, the title is cut (to no fewer than 8 columns), then left out,
and last the assignee goes. The other links keep sharing a line of chips.
A link without a URL takes the `url` Linear gives (https only); an ID
Linear has not answered for links into the key's workspace
(`organization { urlKey }`, asked in the same request), so
`linear.workspace` is only needed without a key. A URL the link was
written with stays, since it may point at a comment or another workspace.
With a key, the "no workspace" note and hint stay away while the first
answer is pending, and come back if Linear fails before naming the
workspace (a refused key, a failing key command). The open-link plugin action asks for the
workspace once when `linear.workspace` is unset. Still to do: the issue
description in a drawer view (L3), which needs an on-demand `issue(id:)`
fetch.

`internal/source/linear`:

- One GraphQL request (`https://api.linear.app/graphql`) per 50 IDs: per
  team key an aliased `issues(filter: { team: { key: { eq: $kN } }, number:
  { in: $nN } }, includeArchived: true)`, keys and numbers as variables,
  asking `identifier title url state { name type } team { key name }
  assignee { displayName initials }`, plus `organization { urlKey }`. A
  batch of 50 costs about 250 complexity points (object 1, field 0.1,
  times `first`; a query may cost 10,000, a key gets 3,000,000 an hour and
  2,500 requests, checked against Linear's docs 2026-10-04). A key with only
  *Read* permission is enough; a key limited to some teams just gets no
  nodes for the others, which then link through the workspace. An
  ID Linear does not know is just missing from the nodes and has no state.
  Not `issue(id:)`: it returns a non-null `Issue!`, so one unknown ID would
  null the whole response.
- `live.Source.Read` calls `Apply`, which lays the cache over every Linear
  link and starts a background fetch for IDs missing or older than 3
  minutes, one at a time. When it ends, `OnUpdate` sends a reload. A reload
  never waits on the network.
- Failures go to `Snapshot.Missing` as `Linear status: …`, and the next try
  waits: a minute after an error, 5 minutes after a refused key (HTTP
  401/403 or `AUTHENTICATION_ERROR`), until the reset time after a rate
  limit (HTTP 429 or `RATELIMITED`, `X-RateLimit-Requests-Reset`). Without
  a key the Sources view has a note, not a missing source.
- Key: `$LINEAR_API_KEY`, else `linear.api_key_command` from the config
  file (user's decision, 2026-10-03), split like the editor command and run
  without a shell, with a 30 s timeout, at most once per deck start; a
  refused key lets it run again. A failed command is not retried until the
  deck restarts. The key is sent as `Authorization: <key>`, kept in memory
  only and redacted from every error; a command's output is never echoed.
- Tests use a fake `http.RoundTripper` and command runner; they never call
  Linear.

## Linear view

Decided with the user on 2026-10-22 (board task DCK-2). With a Linear API
key the deck starts on the Linear view instead of the tasks, which the
user sees on the board anyway; `V` switches between the two, and the
tasks view with DCK-1's own priorities is unchanged.

- **List.** The open issues assigned to the key's user, in every team the
  key reaches (for example Engineering and Infra), with state type started, unstarted or
  backlog: done, canceled (duplicates are canceled too) and triage are left
  out. One group per state name (teams have their own states; a name's group takes the rank of its furthest kind and earliest position), in Linear's order (under way first, then
  to do, then the backlog; by state position within a type), each sorted
  by Linear priority (Urgent first, none last), then most recently
  changed. Groups fold like lists (`ui.folded_lists`).
- **Task column.** A board task belongs to an issue when its title starts
  with the issue's ID (`deck.TitleIssueID`: `ENG-1656 …`, `[ENG-1656] …`,
  word boundary, so ENG-16560 is another issue), whatever the team. With
  several, an open one comes first, then the newest; the row shows it, with
  `+N` for the others, and the drawer lists them all. The row's PR, links,
  dev dots and ↵ (task pane) come from it.
- **Drawer.** The tab bar has one tab, Linear: an Issue section (status,
  prio, labels in their colours, project, team, task, updated), the PR
  section when the task has one, the description (glamour), the newest 10
  comments, oldest first, and the links.
- **Changes.** `m`, or a click on the row's state mark or the drawer's
  status, opens the state picker (decided with the user on 2026-10-22,
  board task DCK-3): a box over the list, under the row (above it when it
  does not fit), with the team's states in Linear's order (a team can have 16;
  the box scrolls when the pane is short). `↑↓`/`↵`, a state's key (1–9,
  then a–z without m, so `m3` still works as a chord) or a click picks one;
  esc, m, another key or a click outside keeps it. The states keep the
  deck's own glyph colours, not Linear's (the user did not ask for them).
  `P` then `0`–`4` sets the Linear priority. Both show at once and reload;
  a refusal shows red in the footer and as a `refused` line in the drawer
  until a change to that issue goes through. A read-only key (HTTP 403 or a
  `FORBIDDEN` GraphQL error) says it may not change issues, and the key is
  not dropped, as it still reads.
- **Source** (`internal/source/linear/mine.go`): one request a minute,
  `viewer.assignedIssues(first: 50, filter: state type in …)` with labels
  (10), comments (10, `orderBy: createdAt`, which Linear returns newest
  first) and `teams(first: 20) { states }`, about 3,500 complexity points.
  Changes are `issueUpdate(id, input: { stateId | priority })`. Both were
  checked against Linear's schema (it validates before it authenticates)
  on 2026-10-22. Failures go to `Snapshot.Missing` as `Linear issues: …`,
  with the same backoff as the statuses. Without a key, `Snapshot.Mine.Off`
  says so and the deck starts on the tasks.
- The deck reaches Linear with its own API key, not the Linear connector of
  Claude: that connector's login lives in claude.ai and is reachable only
  through Claude, not from a standalone binary.

## GitHub and Review views

Decided with the user on 2026-10-26. Next to the Linear view, two more
view tabs: **GitHub**, the open PRs gh's user wrote, and **Review**, the
open PRs they are asked to review (`review-requested:@me`, so a team's
request counts too). The header shows the tabs after the project's name,
each with its count in parentheses (`GitHub (10)`), the shown one in the
drawer tabs' blue; a narrow header leaves the name out.

- **Keys.** `←`/`→` step through Linear, GitHub, Review (the ends stay
  put), and a click on a tab shows it. `V` goes to the tasks and back to
  the tab last shown; on the tasks the arrows do nothing. In the focused
  drawer the arrows stay the PR, Commits and Impact tabs' own.
- **Repos.** `github.repos` (a list, `owner/name`; none lists every repo).
  The user follows acme/webapp and acme/deploy. A name
  that is not `owner/name` is refused, so a setting can add nothing else
  to the search.
- **List.** One group per repo, in `github.repos`' order, the most recently
  changed PR first. A row: a mark that sums it up (green can merge, red
  something to fix, yellow waiting, ◇ a draft), the number and title (a
  draft's dim with `draft`; on Review, the author), then REVIEW (approved
  green, waiting yellow, changes red, none dim), CI (the head commit's
  rollup: passed green, running yellow, failed red, none dim), MERGE
  (ready green, blocked and behind yellow, conflict red, draft and not
  worked out yet dim) and LINKS: G for the PR, L for the Linear issue the
  title names when it is one of the Linear view's. Below 72 columns the
  three states are a glyph each. `↵` and `g` open the PR.
- **Drawer.** The PR tab only, as for a thread PR: the GitHub reader's
  Focus and Detail read the shown PR in full (checks, review threads,
  description, conversation), and `ApplyMine` lays that read over the
  list's; the newer one gives the review, merge state and CI.
- **Source** (`internal/source/github/mine.go`): one `gh api graphql` a
  minute with two aliased `search(type: ISSUE, first: 50)`, `is:pr is:open
  archived:false author:@me` and `review-requested:@me`, each with the
  repos and `sort:updated-desc`. It costs one point of the 5,000 an hour
  (checked 2026-10-26). Failures go to `Snapshot.Missing` as `GitHub PRs:
  …`, with the PR reads' back-off; gh missing or logged out says so in the
  view. Off with `github.enabled = false`.

## GitHub pull requests

The read-only part of the shortlist in
[docs/research/integrations-linear-github.md](research/integrations-linear-github.md):
G1 (why the PR is not merging), G2 (a failed check's log tail) and G3
(unresolved review threads). Writes (re-runs, merges) are not decided.
`internal/source/github`:

- Shells out to `gh` (the research doc's option A), so the deck never
  handles a token: one `gh api graphql --hostname <host>` per batch of up
  to 10 open thread PRs on a host, one aliased `repository { pullRequest }`
  each, plus `rateLimit`. It asks for state, draft, mergeable,
  mergeStateStatus, reviewDecision, base, auto-merge, review requests, the
  head commit's statusCheckRollup contexts (check runs and statuses, one
  per name, the latest run winning) and review threads (unresolved kept,
  outdated last, the first comment and a count). About 3 points a PR.
- `live.Source.Read` calls `Apply` after herdr's state, which lays the cache
  over each thread PR and queues a background read of the open PRs of
  unresolved threads that are missing or stale: after 45 s for the PR the
  drawer shows (the UI passes it through `Options.FocusPR`, and a stale one
  is read at once and first), after 3 minutes for the rest. A merged or
  closed PR is not read again. `OnUpdate` reloads the deck.
- The ticker and the deck both describe a PR. The newer one wins: the
  deck's read replaces the ticker's state, review and failing checks (and
  `CheckedAt`) when it is newer; a newer ticker that disagrees keeps its
  fields, drops the deck's checks and makes the deck read the PR again.
- Back-off: a minute after a failure, doubling up to 10 minutes; until
  `resetAt` after a rate limit (`RATE_LIMITED` or "rate limit" from gh);
  until the reset when fewer than 100 points remain. gh missing or logged
  out (exit code 4) is a Sources note and the PR keeps the ticker's data;
  other failures are `Snapshot.Missing` as `GitHub: …`.
- Logs (`Reader.Log`, behind `c` or a check chip): the job ID comes from
  the check's `detailsUrl` (`/actions/runs/<run>/job/<job>`); `gh api
  repos/{o}/{r}/actions/jobs/{job}/logs --allow-escape-sequences` (retried
  without the flag on an older gh) keeps the last 4 MB; `Tail` cuts from
  the failing step's `##[group]Run …` (its header group left out) to the
  step's end, drops timestamps, colour codes and group markers, and keeps
  200 lines. A finished job's tail is cached by job ID. Checks outside
  Actions open their page instead.
- The UI: the *PR* section's checks line and merge-state line
  (`mergeReason`) with a line to the PR tab, the PR tab, and the log view
  (`modeCheck`, full height like the report).
- The PR tab (2026-10-06), after Commits and only for a thread with a PR:
  first, with no heading, what the PR is (title chip, author, opened and
  updated, head → base, labels, reviewers and requests,
  additions/deletions/files; the user put it first on 2026-10-07), then
  *Status* (state, review decision, auto-merge, `mergeReason`, checks,
  `checked`), *Description* (the body through internal/markdown) and *Comments* (issue
  comments, reviews and review threads, resolved ones included, in time
  order; apps' comments and resolved threads fold to one line). The body
  and conversation (`deck.PRDetail`, `detailFields`: title, body, labels,
  `comments(last: 50)`, `reviews(last: 50)`) are asked for only for the PR
  the tab shows (`Reader.Detail`), as a second fragment in the same
  request, alone (a large or failing answer never costs the other PRs
  their read), so they refresh with the PR's own FocusTTL reads and share
  its back-off; a read without them keeps the last ones, and the tab
  coming back reads them again once older than FocusTTL. Empty
  comment-only reviews (GitHub makes one per thread reply) and the
  author's own review state are left out. Before the detail arrives, the
  tab lists the regular read's unresolved threads. A click on
  the list's PR number shows the tab, a second click opens GitHub; `g`
  still opens GitHub from anywhere. Overview's *Review* section moved into
  the tab. Settings: `[github] enabled`
  (default true); the intervals are constants.
- Tests use a fake gh runner with made-up fixtures (`testdata/`); they never
  call GitHub.

## Dev manifest

Spec, approved by the user on 2026-10-04 (decisions in its section 17):
[docs/dev-manifest.md](dev-manifest.md), with JSON Schemas in `schema/v1/`
(checked against the spec's examples by `go test ./schema`). A shared,
tool-neutral `.config/dev.json` (services, groups, commands, links) replaces
`.herdr-deck/dev.json`, which stays readable as a legacy fallback. Per-worktree
ports come from a port store shared by every tool
(`$XDG_STATE_HOME/dev-manifest/ports.json`), or from the project's own state
file. Detection from task runners only seeds a first manifest
(`herdr-deck dev init`) and warns about drift; it is never a runtime source.
An agent skill, `skills/dev-manifest` (linked into `~/.claude/skills`), writes
and validates manifests for existing repos until `herdr-deck dev init` and the
drift check exist; the skill then uses them.
The deck implements it in phases:

1. **Phase 1 (done).** Lookup with the legacy fallback, the parser and
   checker shared with the skill's validator, fixed and state-file ports,
   variables and `$env(…)`, services with readiness, `u` = `dev`
   (`commands.dev`, else the default group's services in `needs` order),
   `U U` = `stop` (`commands.stop`, then run records, dependents first),
   run records, logs and the lock in the shared state folder, and a line
   per service in the drawer.
2. **Phase 2.** The shared port store (`{ "base": N }` ports), `start` and
   `stop <service>`, and a picker for the manifest's other commands
   (terminal commands in a herdr pane).
3. **Phase 3.** `herdr-deck dev init` (seeding) and drift warnings in the
   sources view.

## Milestones

Each milestone is one thread / one PR, landed before the next starts unless
marked parallel.

1. **Skeleton.** `go mod init`, `cmd/herdr-deck`, Bubble Tea app with
   sections and fake data, key help, `--project` flag, `make build/test/lint`
   (golangci-lint if available, else `go vet`), README, CI-free.
2. **herdr-projects reader** (parallel with 3). `internal/source/projects`:
   PROJECT.md, `thread list --json` with TOML fallback, `ticker.json` PRs,
   inbox. Fixtures are a made-up project in the shapes herdr-projects
   writes. Tests.
   Done: the live group token maps to the deck status (`waiting-on-you` →
   needs you, `working` → working, `ready-for-review`/`landing` → review,
   `resolved` → done, `idle`/unknown → unknown). Unreadable sources are
   listed in `deck.Snapshot.Missing`; `Read` never returns an error.
3. **Tasks + links** (parallel with 2). `internal/source/tasks`: loose
   TASKS.md parser and link scraper as specified above, with tests covering
   both TASKS.md shapes (the strict format and a loose hand-written file).
4. **Live UI.** Replace the skeleton's sections with the list + drawer
   layout from the UI section (needs-you pinning, folding, numbered drawer
   links) and wire 2+3 into it. Refresh on file changes (fsnotify on the
   project folder) plus a 5 s tick. Link keys, digits and mouse open URLs.
   Done: `internal/source/live` combines the readers and watches the folder;
   `↵` and the drawer's `Dev` line are placeholders for milestones 5 and 6.
5. **herdr live state.** Snapshot + socket subscription (verify streaming
   first; fall back to polling). Pane focus on `enter`. "Needs you" from live
   agent state.
   Done: `internal/source/herdr` reads the snapshot on every reload and
   streams events that trigger reloads. "Needs you" from live state follows
   herdr-projects' own rule (`src/thread.rs`, `group`, row 4): an agent
   `blocked` for 30 s (`BLOCKED_DEBOUNCE_SECS`) makes its thread need the
   user, so the deck only runs ahead of herdr-projects' ~15 s ticker, never
   against it. A shorter block is ambiguous and the herdr-projects group
   stands. A `working` agent overrides a stale needs-you unless the thread
   failed; `idle`, `done` and `unknown` leave the group as it is. `enter`
   runs `pane.focus` over the socket.
   Fixed in t-0009: the threshold was 5 s, and a thread waiting on its own
   background /code-review showed under Needs you while herdr-projects kept
   it working. herdr 0.9.3 detects the agent's state from its screen
   (`herdr agent explain <pane>` names the rule; the Claude rules are in
   `~/.local/state/herdr/agent-detection/remote/claude.toml`). Claude's
   "Waiting for N background agents to finish" line is `working`
   (`background_agents_working`), and the snapshot has no field that says
   which rule fired, so a short `blocked` (a dialog that closes again) cannot
   be told from a real question in time. Hence the debounce.
6. **Dev servers.** (Since replaced by the shared dev manifest, above.) Reader for the `.herdr-deck/dev.json` manifest + port
   probe; per-thread localhost links with running dots; `port` token fallback.
   Done: `internal/source/dev` runs after herdr's reader (which sets each
   thread's `PortToken`); localhost links carry `Down` when their server does
   not listen; `Snapshot.Notes` holds Sources lines that are not missing data.
7. **herdr plugin.** `herdr-plugin.toml`, toggle action, auto-open event,
   split sizing. Document install in README.
   Done: `internal/plugin` holds the toggle action and the
   `pane.agent_detected` hook (`herdr-deck plugin toggle|agent-detected`);
   coordinators only, one deck per workspace, sized with
   `layout.set_split_ratio`. Details in "herdr integration" above.
   Later (user, 2026-10-03): the toggle focuses the deck it opens, focuses
   a deck that is not focused instead of closing it, and closes only a
   focused deck.
8. **Polish / later.** Done: the config file (see Configuration), with a
   configurable editor and diff tool, a blank line between the list's
   groups, browser tab reuse (`launch.Browser`: on macOS, AppleScript
   finds a tab of the default browser showing the same page, as `pageKey`
   defines it, and focuses it; else `open`/`xdg-open`; the README's
   "Browser tabs" has the rules), `u` starts dev servers (the manifest's
   `up`), Figma link handlers with the open-link action (see herdr
   integration), and Linear issue status next to IDs (see "Linear issue
   status" above), the GitHub PR reads (see "GitHub pull requests" above), the drawer's Files section, whose `d` opens the diff
   tool (see UI), the settings page (`s`, see Configuration),
   CHANGELOG.md with What's new (`w`, see Updates), and README GIFs that
   `make demo` renders with VHS from `docs/demo/*.tape` on `--fake` data
   (not in CI), and the project picker (`p`, see "Other projects" in UI):
   the user chose it on 2026-10-03 over the multi-project directions in
   [docs/design/multi-project/](design/multi-project/README.md), so the
   deck stays one project per instance. The Impact tab (t-0072, see UI),
   in two PRs: the reader with Look here first and the canvas at rest,
   then selection, lines, the detail and the separate pane.

## Definition of done (v1 = milestones 1–7)

- Running `herdr-deck` next to a project's coordinator shows its tasks
  (from a loose, non-strict TASKS.md too), its threads with live status and
  PR state, inbox count, and working localhost links for any running dev
  server.
- Every link kind opens from a key and from a mouse click.
- Opening a project with `herdr-projects open <slug>` gets a deck pane next to
  the coordinator without manual steps.
- `go test ./...` passes; no writes to anything under `~/.herdr-projects`.

## Open questions for the user

None. Settled:

- Name: it stays `herdr-deck`.
- The deck opens on its own next to coordinators only, never next to thread
  agents; the toggle action still opens one anywhere.
- `o` opens the first running localhost link directly, with no chooser.
- CI, releases and self-update: the recommendations in
  [docs/research/ci-releases-selfupdate.md](research/ci-releases-selfupdate.md)
  (its last section) are accepted.
