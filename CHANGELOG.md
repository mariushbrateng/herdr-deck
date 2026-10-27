# Changelog

What changed in each herdr-deck release, newest first. The deck shows this
file too: press `w` for What's new.

The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and versions follow [Semantic Versioning](https://semver.org/).

## [Unreleased]

### Added

- herdr-projects 1.x: the deck reads the work board (`herdr-projects list
  --json`, `details <id>` and the task folders) when the projects root has
  one, and the toggle opens it from any pane. Each task shows its stage in
  its own colour and glyph, its PR, worktree, review findings, time and
  tokens used, the Linear ID its title starts with, and a red ⚠ when it
  touches a critical area (review tier, the repo's AGENTS.md or
  INVARIANTS.md, or paths such as migrations and auth). `P` then `1`–`4`
  gives a task a priority of the deck's own (1 Urgent … 4 Low); the lists
  sort by priority, then stage. `R` opens a task's report or review page.
  0.2 projects work as before.
- The Linear view, where the deck starts when a Linear API key is set: your
  open Linear issues by state (under way, to do, backlog), sorted by
  priority, each with the board task whose title starts with its ID (its
  ID, stage and PR). The drawer shows the issue: state, priority, labels,
  project, description and comments. `m`, or a click on an issue's state
  mark or its status in the drawer, opens a picker with its team's states
  (↑↓ and ↵, a state's key, or a click) and moves it there in Linear; `P`
  sets its priority. When Linear refuses a change, as with a read-only
  key, the deck says so in red. `V` switches to the tasks.
- GitHub and Review views, next to Linear in the header's view tabs, each
  with its count: your open pull requests, and the ones you are asked to
  review, by repo. Each shows its review (approved, waiting, changes
  requested), CI (passed, running, failed) and whether it can merge
  (ready, blocked, behind, conflict, draft) in green, yellow and red. `←`
  and `→` (or a click on a tab) switch between Linear, GitHub and Review;
  `↵` or `g` opens the PR, and the drawer shows its PR tab. The new
  `github.repos` setting picks the repos. Read through `gh` every minute.

## [0.1.9] - 2026-10-20

### Added

- Task text shows its inline Markdown: *italic*, **bold**, `code`,
  ~~strikethrough~~, <u>underline</u> and links in task titles (list
  rows, the card, the project switcher), Next lines and one-line notes,
  without the markers. A link shows its text and opens with a click
  where the terminal supports OSC 8 links. Markers inside words, like
  `snake_case_names` or `2*3*4`, stay as written.

### Changed

- The drawer's Overview tab opens with the dev links: the localhost link
  chips moved from the Dev section to the top, and each shows its port,
  `[3 Frontend :5181 ●]`. The rest of the Dev section (a line per service
  with its state and log) is gone; the list's DEV column still shows each
  service's state. A thread without dev links gets one dim line in their
  place saying why: `not started · u starts it`, `starting…`, `dev exited
  · see the log`, `no .config/dev.json`, `invalid .config/dev.json · !
  shows why`, `worktree not found` or `ports not known yet`.
- A row whose threads have no PR, or that has no thread, uses the first
  GitHub pull link in its task, for example a task list a script writes
  from review requests: the PR column shows its number, and the PR tab,
  Overview's PR section and the card show it as for a thread PR, read
  through `gh` by owner, repo and number. A thread's own PR always wins.

## [0.1.8] - 2026-10-09

### Added

- An Impact tab in the drawer: what a thread's branch did to its
  repository's shape. It opens on *Look here first*: imports that go
  upward against the layers or skip one, new cycles, new third-party
  dependencies, new env, HTTP, SQL and exec touchpoints, new imports
  between packages and API changes, riskiest first, each with its file and
  line. Below it, the changed packages are boxes nested in their folders
  (unchanged ones counted), with each changed import numbered on its
  boxes' borders in its colour (green added, yellow changed, red removed)
  and listed in a legend. It reads Go and TypeScript straight from git
  objects, without a checkout, and caches every file by its content, so a
  reload costs almost nothing. A repository can declare its layers in
  `.config/dev.json` under `x-herdr-deck.architecture`; the new `[arch]`
  settings turn the tab off (`enabled`) or count test files (`tests`).
- Select a box on the Impact canvas: `tab`, then `j`/`k` through
  *Look here first* and the arrows or `hjkl` from box to box, or a click.
  The box's imports are drawn as lines in their colours (`┄` for a
  removed one), routed around unrelated boxes, and its detail lists them
  with their files and lines, its API changes, touchpoints and files.
  `↵` shows the line in the diff preview.
- Select an edge: `n` and `N` (or `]` and `[`) step through the selected
  box's imports, or click a line. The line turns heavy, and the main view
  shows its code: the hunk with the import line marked (the base side for
  a removed one), with the names a changed import gained and lost; `tab`
  steps through the files that import it.
- `esc` steps back: from an edge to its box, then to the list. While a
  box or edge is selected, `n`, `tab` and `]`/`[` take these new
  meanings; `esc` gives them back.
- `O` opens the Impact view in a herdr pane of its own, zoomed to the
  whole tab (`herdr-deck arch`); `q` closes it.

### Changed

- The mouse wheel scrolls the view under the pointer (the code view or
  the Impact tab) and never moves the selected row, box or edge behind it.

### Fixed

- On macOS, `U U` no longer fails with "operation not permitted" when a
  dev server exits just as it stops it: a process group with only
  exited processes left counts as stopped.

## [0.1.7] - 2026-10-07

### Added

- Drag the rule between the list and the drawer to resize the drawer, or
  press `+` and `-`. The list keeps at least three rows and the drawer its
  card, tab bar and two lines. The height is a share of the pane, so it
  follows a resize, and it is kept for the next start (in the deck's state
  folder); `z` cycles back to it. The new `ui.drawer_height` setting
  (default `0.5`) is the height until you drag.
- A scrollbar at the right edge of a diff preview that is longer than the
  pane, and of a full view that overflows (a report, What's new, help,
  settings). Drag its thumb to scroll, or press on the track to jump there;
  it follows the keys and the wheel too.
- The deck reads the shared dev manifest, `.config/dev.json`
  ([spec](https://github.com/FredricW/herdr-deck/blob/main/docs/dev-manifest.md)):
  the worktree's own, else the main checkout's. Ports come from fixed numbers or the project's state file,
  links and commands can use `$WORKTREE`, `$REPO`, `$DIRNAME`, `$BRANCH`,
  `$PORT_<name>` and `$env(…)`, and the drawer's *Dev* section shows each
  service on its own line with its dot, port, state and log.
- `u` runs the manifest's `dev` command, or starts the default group's
  services in `needs` order, waiting for what each one needs to get ready.
  `U` pressed twice stops them: the `stop` command, then every process with
  a run record, dependents first. Run records and logs live in the shared
  `~/.local/state/dev-manifest/` folder, so other tools see what the deck
  started.
- A PR tab in the drawer, after Commits, on rows whose thread has a pull
  request: what it is (title, author, branches, labels, reviewers, size),
  its status (state, why it is not merging, checks with the failing ones
  as chips, review decision, auto-merge), its description as Markdown,
  and its conversation in time order (comments, reviews and review
  threads; apps' comments and resolved threads folded to one line). `↵` or
  a click opens a comment on GitHub.
- A click on a row's PR number in the list shows its PR tab; a second
  click opens the PR on GitHub.

### Changed

- The deck now prefers `.config/dev.json` to `.herdr-deck/dev.json`. A
  legacy file still works, with a note in the `!` sources view, but any
  `.config/dev.json` (the worktree's or the main checkout's) comes first,
  and a broken manifest is reported instead of falling back to another
  file. Logs of what `u` starts moved from the deck's own state folder to
  the shared one. An `up` command started by an older deck keeps running
  but is not tracked: stop it yourself, since `U` does not see it.
- Overview's *PR* section is a short summary with a line to the PR tab;
  the *Review* section moved into the PR tab's comments.

## [0.1.6] - 2026-10-05

### Changed

- The drawer's tab bar has a blank line above and below it and a column
  of padding on either side, and inactive tabs have a much quieter
  background, just off the terminal's, so the active tab stands out.
- The drawer's header card shows the thread's repo and branch on its
  second line; the thread id, its title and the pane moved into the
  *Thread* section.
- The diff preview is calmer: added and removed lines get a fainter green
  and red tint, so the code's colours dominate; a blank line separates
  hunks and a commit's files; and in the split layout the side without a
  line shows a dim `╱╱╱` hatching instead of blank space.

## [0.1.5] - 2026-10-05

### Added

- A split layout for the diff preview: `S` (or `|`) shows the old file on
  the left and the new one on the right, with faint line numbers on each
  side and removed and added lines paired up, and switches back to
  unified. `diff.layout = "split"` (or the settings page, `--diff-layout`,
  `$HERDR_DECK_DIFF_LAYOUT`) starts with it. A pane under 100 columns shows
  unified and says so in the preview's header.
- The drawer reads each open thread PR from GitHub through your `gh`, so the
  *PR* section says why it is not merging: every check (failing ones as
  chips, running and queued ones too), behind its base, conflicts, draft,
  reviews still asked for, changes requested and auto-merge. A new
  *Review* section lists the PR's unresolved review threads with their
  file, line, author and first comment; `↵` or a click opens one on GitHub.
  `c` (or a check chip) shows a failed check's failing step's log, full
  height, read on demand. Without `gh`, or logged out, the deck keeps
  herdr-projects' ticker data and says so in `!`. `github.enabled = false`
  turns it off.
- Linear issues in the drawer show their title and assignee, one line
  each: `[1 ABC-123 Fix login · in progress · ana]`, shortened to fit a
  narrow drawer.
- A spec for a shared, tool-neutral dev manifest, `.config/dev.json`
  (`docs/dev-manifest.md` in the repo), and an agent skill,
  `skills/dev-manifest`, that writes and validates one for a repository.
  The deck still reads `.herdr-deck/dev.json` for now.

### Changed

- With a Linear API key, bare Linear IDs open Linear's own URL, so
  `linear.workspace` is no longer needed; the open-link action asks Linear
  for the workspace too.

## [0.1.4] - 2026-10-04

### Added

- A Commits tab in the drawer, between Files and Log: the thread branch's
  commits since its base, newest first, with short sha, subject, age and
  `+N −M`, under a `● uncommitted · N files` row that leads to Files.
  `↵`, a digit or a click previews a commit as `git show` does, with a
  syntax-coloured diff. `space`, `→` or `l` in the focused drawer (or a
  click on `▸`) expands a commit to the files it changed, and `↵` on one
  previews that file's change; `←` or `h` collapses it. `g` opens the
  commit in the PR on GitHub.
- `ui.folded_lists` in the config file (also on the settings page, as
  `--ui-folded-lists` and `$HERDR_DECK_UI_FOLDED_LISTS`) names the lists
  that start folded, by heading in any case: `["Backlog", "Resolved"]` by
  default, `[]` for none.

### Changed

- The Files tab previews by default, as Commits does: `↵`, a digit or a
  click shows a file's diff in the deck instead of opening the diff tool.
  The diff tool is now `d` in the focused Files or Commits tab (the file
  or commit under the cursor, so `d d` from the list opens the first file
  there), and `a` opens the whole diff in it.
- A list heading now has to match a folded list's name whole, so a list
  such as "Backlog later" no longer starts folded.
- Settings now live in one table per area of the config file (`[ui]`,
  `[projects]`, `[linear]`, `[figma]`, `[browser]`, `[updates]`,
  `[editor]`, `[diff]`), and each is named by its dotted path, such as
  `linear.workspace`. That name gives its flag and environment variable
  (`--diff-command`, `$HERDR_DECK_DIFF_COMMAND`), and every setting now
  has a flag. The earlier keys, flags (`--diff-tool`, `--editor`, …) and
  environment variables still work, and the `!` view notes each earlier
  key in the file. Run `herdr-deck config migrate` to see the move and
  `herdr-deck config migrate --write` to make it (the old file is kept as
  `config.toml.bak`); saving on the settings page also moves a setting
  into its table, comments and all.
- A stray argument, such as a mistyped command, is now an error instead
  of being ignored.

## [0.1.3] - 2026-10-03

### Added

- `p` opens a project picker over the deck: every project under the
  projects root, with the threads waiting on you in each and how long they
  have waited. Inbox items are progress updates, not needs: each project
  shows them as a dim `✉ N updates`. Projects with a waiting thread come
  first; paused ones are dim and archived ones show after `tab`. Type to
  filter. `↵` on a project focuses its coordinator, or starts one with
  `herdr-projects open` when none runs; on a waiting thread, it focuses that
  thread's pane, in whichever workspace it is.
- When threads in other projects wait on you, the list ends with a line
  such as `● 2 other projects need you`; click it or press `p` to see them.
  Their threads never join this project's Needs you group.
- The drawer has a header card (the title, the status as a glyph and a
  word, the percent with a progress bar) and three tabs: Overview
  (Next, PR with commenters and failing checks, Note, Links as numbered
  chips, Dev, Thread), Files (the changed files as a diffstat with a
  bar per file, every file listed) and Log (the thread's timeline from
  its thread file and its inbox items, handled ones included). The
  active tab is dark text on blue. `[` and `]` switch tabs; `tab` moves
  the focus into the drawer, where `j`/`k` and `↵` pick a chip, file or
  event. The drawer takes half of the pane.
- A diff preview: on the Files tab, `v` shows the file under the cursor's
  diff where the task list is, while `j`/`k` in the drawer move between
  files. The diff is against the merge-base, uncommitted changes and
  untracked files included, with its code coloured by the file's language,
  a green `+` and red `−` gutter on tinted lines and dim hunk headers.
  `J`/`K` scroll it a line, `pgup`/`pgdn` a page, the wheel three lines;
  `v` or `esc` brings the list back as you left it.

### Changed

- Inbox items no longer get rows under Needs you, which now holds only
  threads waiting on you. A thread with unhandled items has a yellow title
  and shows them, marked `✉`, in its Log; items about no thread only count
  in the header.
- `d` focuses the Files tab instead of opening a chooser; `d 3` still
  opens file 3 and `d d` the whole diff, and `t` on the tab switches list
  and tree. With one changed file, `d` no longer opens it at once; use
  `d 1`.
- A thread's report (`r`), What's new (`w`) and a task's notes in the
  Overview render their Markdown the way glow does: headings, bold, code,
  lists, quotes and highlighted code blocks, wrapped to the pane and styled
  for a dark or a light terminal.

## [0.1.2] - 2026-10-03

### Changed

- The Files section colours each file by how it changed: a status letter
  `A` added (green), `M` modified (yellow), `D` deleted (red), `R` renamed
  (cyan) and `?` untracked (faint green). Line counts are green and red;
  an added file shows only `+N` and a deleted one only `-M`.
- `d t` switches the Files section between the list and a folder tree, with
  folder names and their summed counts muted so the files stand out.
  `diff_view = "tree"` in the config file (or the settings page) starts in
  the tree.

### Fixed

- The settings page scrolls to keep an invalid value's error in view.

## [0.1.1] - 2026-10-03

### Added

- A Files section in the drawer lists the files the selected thread changed,
  with `+N -M` counts. `d` opens the diff tool on one file or on the whole
  diff.
- A settings page (`s`) shows every setting, its value and where it comes
  from, and edits the config file in place. Most changes apply at once.
- What's new (`w`) shows this changelog in the deck. After an update the
  deck says once which version it now runs, and while `↑` shows, it lists
  what the newer version brings.
- Releases carry prebuilt binaries for macOS and Linux (arm64 and amd64)
  with a `SHA256SUMS` file. Installing a release through herdr downloads
  the binary instead of building it, so Go is only needed for other commits.
- The README shows the deck in short demo GIFs.

### Changed

- Release notes on GitHub come from this changelog.

## [0.1.0] - 2026-10-03

The first release.

### Added

- A status pane for a herdr-projects project: its tasks and threads in one
  list, with what needs you pinned on top, and a drawer with details about
  the selected row. Long lists such as Backlog fold.
- Live status from herdr-projects and herdr: each thread's state and
  activity, its pane and agent, the inbox, and pull requests with their
  review state, comments and failing checks.
- Numbered links in the drawer for Linear, Figma, Notion, GitHub and
  localhost. A digit opens one; `l f n g o` open the first of a kind.
- Linear issue status next to each issue ID, when a Linear API key is set.
- Dev servers from each worktree's dev manifest, with a dot per port.
  `u` starts a thread's dev servers.
- Keys to focus a thread's herdr pane (`↵`), open its worktree in the
  editor (`e`), read its report (`r`), resize the drawer (`z`) and see
  sources that could not be read (`!`).
- Web links open in a browser tab that already shows them, on macOS.
- A herdr plugin: a deck opens next to each coordinator by itself, a key
  toggles it anywhere, and Figma links and selected Linear IDs open from
  any herdr pane.
- `herdr-deck update` updates the plugin. The header shows `↑` when a newer
  version exists, and running decks restart in place on the new binary.
- A config file, `~/.config/herdr-deck/config.toml`, for the Linear
  workspace, editor, refresh interval, update check and more.
