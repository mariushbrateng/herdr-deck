// Package ui renders a deck.Snapshot as a Bubble Tea program: a compact list
// of the project's work on top and a drawer about the selected row below
// (docs/design/d-list-detail.md). It holds no data code: snapshots come from
// internal/source through Options.Load.
package ui

import (
	"context"
	"fmt"
	"time"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/FredricW/herdr-deck/internal/changelog"
	"github.com/FredricW/herdr-deck/internal/config"
	"github.com/FredricW/herdr-deck/internal/deck"
	"github.com/FredricW/herdr-deck/internal/source/arch"
)

// DefaultTick is how often the deck reloads when no file change says to.
const DefaultTick = 5 * time.Second

// Options wires the model to the outside world. Every field may be nil.
type Options struct {
	// Load reads a fresh snapshot. It runs off the UI goroutine, on start,
	// on every Tick and on RefreshMsg. Nil shows the initial snapshot only.
	Load func(context.Context) deck.Snapshot
	// Tick is the reload interval; zero means DefaultTick.
	Tick time.Duration
	// FocusPR tells the GitHub reader which PR the drawer shows ("" for
	// none), so it reads that one more often; CheckLog reads a failed
	// check's log tail, of the PR at prURL. Nil leaves them out.
	FocusPR  func(url string)
	CheckLog func(ctx context.Context, prURL string, c deck.Check) (deck.CheckLog, error)
	// DetailPR tells the GitHub reader which PR the PR tab shows ("" for
	// none), so it reads that PR's description and conversation. Nil
	// leaves them out.
	DetailPR func(url string)
	// OpenURL opens a link; OpenEditor opens a worktree folder. Tests
	// replace both so nothing is ever opened.
	OpenURL    func(url string) error
	OpenEditor func(path string) error
	// CopyLinks puts a link on the clipboard of the terminal the user sits
	// at (OSC 52, which herdr --remote and SSH carry back) instead of
	// opening it, for a deck on a machine with no screen of its own.
	CopyLinks bool
	// FigmaDesktop opens Figma links in the desktop app (their figma://
	// rewrite) wherever a key or click opens a link, not only after d.
	FigmaDesktop bool
	// FocusPane focuses a herdr pane by id, switching herdr to its
	// workspace and tab. Tests replace it so no real pane is ever focused.
	FocusPane func(paneID string) error
	// OpenProject starts the coordinator of the project slug and focuses it
	// (`herdr-projects open`), for the project picker when none runs. Tests
	// replace it so no coordinator is ever started.
	OpenProject func(slug string) error
	// SetPriority gives a herdr-projects 1.x board task the deck's own
	// priority, 1–4, or none with 0 (keys 1–4 and 0). Nil turns the keys
	// off.
	SetPriority func(t deck.Thread, prio int) error
	// SetIssueState moves a Linear issue to the workflow state with that
	// ID (m on the Linear view), SetIssuePriority gives it Linear's
	// priority 0–4 (P). Nil turns the keys off.
	SetIssueState    func(is deck.LinearIssue, stateID string) error
	SetIssuePriority func(is deck.LinearIssue, prio int) error
	// LinearView starts the deck on the Linear view (V switches).
	LinearView bool
	// Relist sorts board tasks and groups them into lists, as the board
	// source does, for a priority shown before the reload confirms it.
	// Nil leaves the lists until then.
	Relist func(threads []deck.Thread) []deck.TaskList
	// Diff reads what a thread's worktree changed against its base. It
	// runs off the UI goroutine when the selection moves to another
	// thread and on every reload. Nil shows no Files section.
	Diff func(context.Context, deck.Thread) deck.Diff
	// OpenDiff opens the diff tool for the worktree at path against base
	// (a commit), for one file (a rename's old and new path) or, with no
	// files, the whole diff. Tests replace it so no diff tool is ever run.
	OpenDiff func(path, base string, files []string) error
	// Patch reads one file's diff against the merge-base for the preview
	// (v on the Files tab). It runs off the UI goroutine when the preview
	// moves to another file and on every reload. Nil turns the preview off.
	Patch func(ctx context.Context, t deck.Thread, mergeBase string, f deck.DiffFile) deck.Patch
	// Commits reads the commits of a thread's branch since its base, for
	// the Commits tab. It runs off the UI goroutine when the selection
	// moves to another thread and on every reload. Nil turns the tab off.
	Commits func(context.Context, deck.Thread) deck.Commits
	// CommitPatch reads one commit for the preview (v or ↵ on the Commits
	// tab). It runs off the UI goroutine. Nil turns that preview off.
	CommitPatch func(ctx context.Context, t deck.Thread, sha string) deck.CommitPatch
	// CommitFiles reads the files a commit changed, for an expanded
	// commit on the Commits tab. It runs off the UI goroutine. Nil leaves
	// commits unexpandable.
	CommitFiles func(ctx context.Context, t deck.Thread, sha string) ([]deck.DiffFile, error)
	// CommitFilePatch reads one file's change in a commit for the preview.
	// It runs off the UI goroutine.
	CommitFilePatch func(ctx context.Context, t deck.Thread, sha string, f deck.DiffFile) deck.Patch
	// Arch reads what a thread's branch did to its repository's shape,
	// for the Impact tab. It runs off the UI goroutine when the selection
	// moves to another thread and on every reload. Nil turns the tab off.
	Arch func(context.Context, deck.Thread) *arch.Result
	// OpenImpact opens a thread's Impact view in a herdr pane of its own
	// (herdr-deck arch). Nil means there is no herdr to open one in.
	OpenImpact func(t deck.Thread) error
	// FileAt reads a file as a commit has it, to show an import the
	// branch did not change in its file. It runs off the UI goroutine.
	// Nil shows such imports without their code.
	FileAt func(ctx context.Context, t deck.Thread, rev, path string) ([]byte, error)
	// OpenCommit opens the diff tool for commit sha of the worktree at
	// path, for some files (a rename's old and new path) or, with none,
	// the whole commit. Tests replace it so no diff tool is ever run.
	OpenCommit func(path, sha string, files []string) error
	// FoldedLists are the headings of the lists that start folded, ignoring
	// case (ui.folded_lists). Nil is config.DefaultFoldedLists; an empty,
	// non-nil list folds none.
	FoldedLists []string
	// DrawerHeight is the drawer's share of the pane at its normal size
	// (ui.drawer_height); zero is config.DefaultDrawerHeight. DrawerKept,
	// when above zero, is the height last dragged to, and wins over it.
	DrawerHeight float64
	DrawerKept   float64
	// KeepDrawer keeps a height the drawer was dragged (or resized with
	// + -) to, for the next start. It runs off the UI goroutine. Nil
	// keeps nothing.
	KeepDrawer func(float64) error
	// DiffSplit starts the diff preview in the split layout (diff.layout =
	// "split"); S switches layouts for the session.
	DiffSplit bool
	// DiffTree starts the Files section in the tree view (diff.view =
	// "tree"); d t switches views for the session.
	DiffTree bool
	// StartDev runs dev for a thread's worktree (the dev manifest's
	// commands.dev, or its services) and returns the line the footer
	// shows; StopDev stops what dev started. Tests replace them so no dev
	// server is ever started.
	StartDev func(t deck.Thread) (string, error)
	StopDev  func(t deck.Thread) (string, error)
	// Now is the clock for ages; Location the zone clock times show in.
	Now      func() time.Time
	Location *time.Location
	// Version is shown dim at the right of the footer when the key help
	// leaves room for it; empty shows nothing.
	Version string
	// BinaryChanged says whether the deck's binary was replaced and a new
	// one is ready. It runs off the UI goroutine on every Tick; when it
	// says yes the program quits and Restart reports true. Nil never
	// restarts.
	BinaryChanged func(context.Context) bool
	// CheckUpdate looks for a newer deck. It runs off the UI goroutine at
	// start and every UpdateEvery (zero is DefaultUpdateEvery). Nil shows
	// no hint.
	CheckUpdate func(context.Context) Update
	UpdateEvery time.Duration
	// RestartFailed says why an earlier restart did not work; the header
	// and the Sources view show it.
	RestartFailed string
	// Settings backs the settings page (s); nil turns it off.
	Settings *SettingsHooks
	// Changelog is this deck's own changelog, which What's new shows.
	Changelog changelog.Log
	// Updated is the X.Y.Z this deck was just updated to: the footer says
	// so until the first key. "" says nothing.
	Updated string
}

// DefaultUpdateEvery is how often CheckUpdate runs.
const DefaultUpdateEvery = time.Hour

// Update is the outcome of an update check.
type Update struct {
	// Available names the newer version (a tag or a short commit); ""
	// when there is none.
	Available string
	// Problem says why the check failed. It shows only in Sources.
	Problem string
	// News is what the newer version's changelog lists beyond this deck,
	// for What's new; empty when it could not be read.
	News []changelog.Release
}

// RefreshMsg asks the model to reload its snapshot, e.g. after the project
// folder changed. Reloads are coalesced: one runs at a time.
type RefreshMsg struct{}

type (
	tickMsg       struct{}
	updateTickMsg struct{}
	binaryMsg     bool
	updateMsg     Update
	updateOnceMsg Update // a check outside the hourly round
	snapshotMsg   deck.Snapshot
	openedMsg     struct {
		what string
		err  error
		verb string // "open" when empty
	}
	drawerKeptMsg struct{ err error }
	devUpMsg      struct {
		status string
		err    error
		stop   bool
	}
	diffMsg struct {
		key  string
		diff deck.Diff
	}
)

// drawerSize cycles with z.
type drawerSize int

const (
	sizeNormal drawerSize = iota // half of the pane
	sizeFull
	sizeHidden
)

const (
	defaultWidth  = 80
	defaultHeight = 28
	// wideMin is the narrowest pane that gets the THREAD and PR columns.
	wideMin = 72
	// noKind marks the chooser as closed.
	noKind deck.LinkKind = -1
)

// Model is the deck's Bubble Tea model.
type Model struct {
	opt   Options
	keys  keyMap
	snap  deck.Snapshot
	rows  []row
	folds map[string]bool // the user's fold toggles, by list name

	cursor    int
	moved     bool // the user moved the cursor, so needs-you no longer pulls it
	listOff   int
	drawerOff int
	mode      drawerMode
	size      drawerSize
	frac      float64       // the drawer's share of the pane at its normal size
	dragging  bool          // the rule above the drawer is being dragged
	dragFrom  float64       // frac when the drag started
	bar       barKind       // the scrollbar whose thumb is being dragged
	barGrab   int           // the row of the thumb the drag holds
	choosing  deck.LinkKind // the link chooser's kind, or noKind
	tab       tabKind       // the drawer tab chosen; curTab is the one shown
	dfocus    bool          // the drawer has the focus (tab), not the list
	dcur      int           // the drawer cursor: a stop of the tab's content
	tree      bool          // the Files tab shows a folder tree
	desktop   bool          // the Figma chooser opens the desktop app
	status    string        // a one-off message in the footer
	stopArmed string        // the thread U was pressed on once; a second U stops it
	prioArmed string        // the board task P was pressed on; the next key sets its priority
	// view is what the list shows: the tasks, or a view tab (Linear,
	// GitHub, Review), lastTab the view tab V goes back to.
	view, lastTab viewKind
	// moveArmed is the Linear issue the state picker is open on (m, or
	// a click on its state), moveSel the picker's selected state and
	// moveOff its first state shown. issuePrioArmed is the issue P was
	// pressed on: the next key sets its priority.
	moveArmed, issuePrioArmed string
	moveSel, moveOff          int
	// failed is the status that says Linear refused a change, drawn red
	// while it is the status; issueFail is the refusal, shown in that
	// issue's drawer until a change to it goes through.
	failed    string
	issueFail issueFailure
	notice    string // "Updated to …", until the first key or click

	width, height int
	light         bool // the terminal's background is light

	loaded      bool // a snapshot came from Load (or there is no Load)
	loading     bool
	pending     bool // a reload was asked for while one ran
	sourcesSeen bool // the Sources view came up by itself once

	checkingBinary bool   // a BinaryChanged call is running
	restart        bool   // quit to run the new binary
	update         Update // the last update check

	diffs     map[string]deck.Diff // the last diff read, by diffKey
	diffSel   string               // the diffKey last asked for
	diffing   bool                 // a Diff call is running
	diffAgain bool                 // the selection moved while it ran

	commitLists  map[string]deck.Commits // the last commits read, by diffKey
	commitsSel   string                  // the diffKey last asked for
	committing   bool                    // a Commits call is running
	commitsAgain bool                    // the selection moved while it ran
	archs        map[string]*arch.Result // the last architecture read, by diffKey
	archSel      string                  // the diffKey last asked for
	archReading  bool                    // an Arch call is running
	archAgain    bool                    // the selection moved while it ran
	// The Impact tab's selected box (for the thread iselKey), whether it
	// shows every finding, and its drawn canvases.
	isel, iselKey string
	iedge         string // the selected edge, as edgeKey names it; "" for none
	esite         int    // the selected edge's site shown (for the edge esiteFor)
	esiteFor      string
	ecode         edgeCodes
	escroll       edgeScroll // the main view's edge code, scrolled by the wheel
	iall          bool
	icache        *canvasCache

	// Expanded commits (by commitKey), their files once read, and the
	// reads running.
	expand         map[string]bool
	commitFileSets map[string]commitFiles
	filesReading   map[string]bool

	// The diff preview (preview.go): on, the thread (diffKey) and file
	// (patchKey) or commit (commitKey) it shows, and its scroll.
	preview    bool
	prevTab    tabKind
	prevThread string
	prevKey    string
	prevOff    int
	prevSplit  bool               // prevOff counts rows of the split layout
	split      bool               // the split layout is chosen (it shows when wide enough)
	patches    map[string]preview // the last patch read, by patchKey (one)
	patchSel   string             // the patchKey last asked for
	patching   bool               // a Patch call is running
	patchAgain bool               // the preview moved while it ran
	prevGoto   previewGoto        // a line to scroll to once its patch is read

	set  settingsPage
	pick picker

	// watched is the PR last given to FocusPR; logs are the failed
	// checks' logs read, by job ID, and logCheck (of the PR logPR) the
	// one the log view shows.
	watched  string
	detailed string // the PR last given to DetailPR
	unfolded map[string]bool
	logs     map[int64]checkLog
	logCheck deck.Check
	logPR    string
}

// New returns a model showing snap until Options.Load delivers a fresh one.
func New(snap deck.Snapshot, opt Options) Model {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Tick == 0 {
		opt.Tick = DefaultTick
	}
	if opt.UpdateEvery == 0 {
		opt.UpdateEvery = DefaultUpdateEvery
	}
	if opt.FoldedLists == nil {
		opt.FoldedLists = config.DefaultFoldedLists
	}
	if opt.DrawerHeight <= 0 {
		opt.DrawerHeight = config.DefaultDrawerHeight
	}
	m := Model{
		opt:            opt,
		keys:           defaultKeys(),
		folds:          map[string]bool{},
		diffs:          map[string]deck.Diff{},
		commitLists:    map[string]deck.Commits{},
		archs:          map[string]*arch.Result{},
		icache:         &canvasCache{},
		expand:         map[string]bool{},
		commitFileSets: map[string]commitFiles{},
		filesReading:   map[string]bool{},
		patches:        map[string]preview{},
		logs:           map[int64]checkLog{},
		unfolded:       map[string]bool{},
		choosing:       noKind,
		tree:           opt.DiffTree,
		split:          opt.DiffSplit,
		width:          defaultWidth,
		height:         defaultHeight,
		loaded:         opt.Load == nil,
		loading:        opt.Load != nil, // Init starts the first load
		frac:           opt.DrawerHeight,
		lastTab:        viewLinear,
	}
	if opt.LinearView {
		m.view = viewLinear
	}
	if opt.DrawerKept > 0 && opt.DrawerKept < 1 {
		m.frac = opt.DrawerKept
	}
	if opt.Updated != "" {
		m.notice = "Updated to v" + opt.Updated + " · " + m.keys.News.Keys()[0] + " what's new"
	}
	m.SetSnapshot(snap)
	return m
}

// SetSnapshot replaces the data. The selection stays on the same row when it
// still exists; until the user moves the cursor it follows what needs them.
func (m *Model) SetSnapshot(snap deck.Snapshot) {
	var selKey string
	if r, ok := m.selected(); ok {
		selKey = r.key
	}
	m.snap = snap
	m.rows = m.buildRows(snap)

	switch {
	case !m.moved:
		m.cursor = m.homeRow()
		// The cursor jumped to what needs the user: Next says why.
		if r, ok := m.selected(); ok && r.key != selKey && r.needsYou() {
			m.tab = tabOverview
		}
	default:
		found := false
		for i, r := range m.rows {
			if r.key == selKey {
				m.cursor, found = i, true
				break
			}
		}
		if !found {
			m.cursor = m.offGap(clamp(m.cursor, 0, len(m.rows)-1), 1)
		}
	}
	// On another row, the drawer starts over: a report or a scroll
	// position belongs to the row it was opened on.
	if r, _ := m.selected(); r.key != selKey {
		m.drawerOff, m.dcur = 0, 0
		if m.mode == modeReport || m.mode == modeCheck {
			m.mode = modeRow
		}
	}
	if m.pick.open {
		m.pickVisible()
	}
	// The Sources view comes up by itself once, when something is missing.
	if m.loaded && !m.sourcesSeen && len(snap.Missing) > 0 && m.mode == modeRow {
		m.mode = modeSources
		m.sourcesSeen = true
	}
	m.ensureVisible()
}

// buildRows lays out the list the model shows: a view tab's or the
// tasks'.
func (m Model) buildRows(snap deck.Snapshot) []row {
	switch m.view {
	case viewLinear:
		return buildIssueRows(snap, m.folds, m.opt.FoldedLists)
	case viewGitHub, viewReview:
		return buildPullRows(snap, m.view, m.folds, m.opt.FoldedLists)
	}
	return buildRows(snap, m.folds, m.opt.FoldedLists)
}

// homeRow is where the cursor rests on its own: the first row that needs the
// user, else the first row of work, else the top.
func (m Model) homeRow() int {
	for i, r := range m.rows {
		if r.kind != rowHeading && r.needsYou() {
			return i
		}
	}
	for i, r := range m.rows {
		if r.kind == rowWork {
			return i
		}
	}
	return 0
}

// offGap moves i off a gap in direction dir (1 or -1). A gap always sits
// between two groups, so the next row that way is a heading or a row.
func (m Model) offGap(i, dir int) int {
	for i >= 0 && i < len(m.rows) && m.rows[i].kind == rowGap {
		i += dir
	}
	return i
}

// Restart says the model quit because a new binary is ready to run.
func (m Model) Restart() bool { return m.restart }

// Snapshot returns the data the model shows.
func (m Model) Snapshot() deck.Snapshot { return m.snap }

// Status returns the one-off footer message, if any.
func (m Model) Status() string { return m.status }

func (m Model) selected() (row, bool) {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return row{}, false
	}
	return m.rows[m.cursor], true
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{tea.RequestBackgroundColor}
	if m.opt.Load != nil {
		cmds = append(cmds, m.load(), m.tick())
	} else if m.opt.BinaryChanged != nil {
		cmds = append(cmds, m.tick())
	}
	if m.opt.CheckUpdate != nil {
		cmds = append(cmds, m.checkUpdate())
	}
	return tea.Batch(cmds...)
}

func (m Model) checkUpdate() tea.Cmd {
	check := m.opt.CheckUpdate
	return func() tea.Msg { return updateMsg(check(context.Background())) }
}

func (m Model) updateTick() tea.Cmd {
	return tea.Tick(m.opt.UpdateEvery, func(time.Time) tea.Msg { return updateTickMsg{} })
}

// checkBinary asks whether a new binary is ready, one call at a time.
func (m *Model) checkBinary() tea.Cmd {
	if m.opt.BinaryChanged == nil || m.checkingBinary || m.restart {
		return nil
	}
	m.checkingBinary = true
	changed := m.opt.BinaryChanged
	return func() tea.Msg { return binaryMsg(changed(context.Background())) }
}

func (m Model) load() tea.Cmd {
	load := m.opt.Load
	return func() tea.Msg { return snapshotMsg(load(context.Background())) }
}

func (m Model) tick() tea.Cmd {
	return tea.Tick(m.opt.Tick, func(time.Time) tea.Msg { return tickMsg{} })
}

// refresh starts a reload, or notes that another is wanted after the
// running one.
func (m *Model) refresh() tea.Cmd {
	if m.opt.Load == nil {
		return nil
	}
	if m.loading {
		m.pending = true
		return nil
	}
	m.loading = true
	return m.load()
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.scrollDrawer(0)
		m.ensureVisible()
		m.relayoutPreview()
		m.scrollPreview(0)
		if m.pick.open {
			m.pickVisible()
		}
	case tea.BackgroundColorMsg:
		m.light = !msg.IsDark()
	case tickMsg:
		return m, tea.Batch(m.refresh(), m.checkBinary(), m.tick())
	case binaryMsg:
		m.checkingBinary = false
		if msg {
			m.restart = true
			return m, tea.Quit
		}
	case updateTickMsg:
		return m, m.checkUpdate()
	case updateMsg:
		m.update = Update(msg)
		return m, m.updateTick()
	case updateOnceMsg:
		m.update = Update(msg)
	case settingsMsg:
		if msg.err != nil {
			m.set.err = msg.err.Error()
		} else {
			m.set.cfg, m.set.loaded, m.set.err = msg.cfg, true, ""
		}
		m.followSetting()
	case settingsSavedMsg:
		cmd := m.settingsSaved(msg)
		m.followSetting()
		return m, cmd
	case RefreshMsg:
		return m, m.refresh()
	case snapshotMsg:
		m.loading = false
		m.loaded = true
		m.SetSnapshot(deck.Snapshot(msg))
		m.syncPreview()
		diff := tea.Batch(m.readDiff(true), m.readCommits(true), m.readArch(true), m.readPatch(true), m.watchPR())
		if m.pending {
			m.pending = false
			return m, tea.Batch(m.refresh(), diff)
		}
		return m, diff
	case diffMsg:
		m.diffing = false
		m.diffs[msg.key] = msg.diff
		m.syncPreview()
		if m.diffAgain {
			m.diffAgain = false
			return m, tea.Batch(m.readDiff(true), m.readPatch(false), m.readEdge())
		}
		return m, tea.Batch(m.readPatch(false), m.readEdge())
	case commitsMsg:
		m.committing = false
		m.setCommits(msg.key, msg.commits)
		m.syncPreview()
		if m.commitsAgain {
			m.commitsAgain = false
			return m, tea.Batch(m.readCommits(true), m.readPatch(false))
		}
		return m, m.readPatch(false)
	case edgeViewMsg:
		if m.ecode.keep(msg.key, msg.v) {
			return m, m.readEdge()
		}
	case archMsg:
		// A reload reads again and mostly gets the same result back; only
		// a new one moves the stops, and the cursor with them.
		changed := m.archs[msg.key] != msg.res
		prev, _ := m.impactStopAct()
		cmd := m.setArch(msg)
		if changed {
			m.relocateImpact(prev)
		}
		// A new head is a new site key: read the selected edge's code.
		return m, tea.Batch(cmd, m.readEdge())
	case commitFilesMsg:
		m.setCommitFiles(msg)
		m.syncPreview()
		return m, m.readPatch(false)
	case patchMsg:
		m.patching = false
		// Only the shown file's patch is drawn: keep that one alone.
		clear(m.patches)
		m.patches[msg.key] = msg.data
		m.scrollPreview(0)
		m.applyGoto()
		if m.patchAgain {
			m.patchAgain = false
			return m, m.readPatch(false)
		}
	case issueSetMsg:
		if msg.err != nil {
			m.status = "could not change " + msg.id + ": " + msg.err.Error()
			m.failed = m.status
			m.issueFail = issueFailure{id: msg.id, msg: msg.err.Error()}
		} else {
			m.status = msg.id + ": " + msg.what + " in Linear"
			if m.issueFail.id == msg.id {
				m.issueFail = issueFailure{}
			}
		}
		return m, m.refresh()
	case prioritySetMsg:
		if msg.err != nil {
			m.status = "could not set the priority of " + msg.id + ": " + msg.err.Error()
		} else if msg.prio == 0 {
			m.status = msg.id + ": no priority"
		} else {
			m.status = fmt.Sprintf("%s: P%d %s", msg.id, msg.prio, prioName(msg.prio))
		}
		return m, m.refresh()
	case openedMsg:
		verb, past := "open", "opened"
		if msg.verb != "" {
			verb, past = msg.verb, msg.verb+"ed"
		}
		if msg.err != nil {
			m.status = fmt.Sprintf("could not %s %s: %v", verb, msg.what, msg.err)
		} else {
			m.status = past + " " + msg.what
		}
	case checkLogMsg:
		m.setCheckLog(msg)
	case devUpMsg:
		switch {
		case msg.err != nil && msg.stop:
			m.status = "could not stop dev servers: " + msg.err.Error()
		case msg.err != nil:
			m.status = "could not start dev servers: " + msg.err.Error()
		default:
			m.status = msg.status
		}
		// The drawer shows the log and that the servers are starting.
		return m, m.refresh()
	case tea.KeyPressMsg:
		m.bar = barNone // a key may change the view under a held thumb
		next, cmd := m.handleKey(msg)
		return next.(Model).followDiff(cmd)
	case tea.MouseClickMsg:
		next, cmd := m.handleClick(msg.Mouse())
		return next.(Model).followDiff(cmd)
	case tea.MouseWheelMsg:
		m.handleWheel(msg.Mouse())
		return m.followDiff(nil)
	case tea.MouseMotionMsg:
		if m.dragging {
			m.dragTo(msg.Mouse().Y)
		}
		if m.bar != barNone {
			m.barTo(msg.Mouse().Y)
		}
	case tea.MouseReleaseMsg:
		m.bar = barNone
		if m.dragging {
			m.dragging = false
			if m.frac != m.dragFrom {
				return m, m.keepDrawer()
			}
		}
	case drawerKeptMsg:
		if msg.err != nil {
			m.status = "could not keep the drawer height: " + msg.err.Error()
		}
	}
	return m, nil
}

// followDiff adds a diff read to cmd when the selection moved to another
// thread, and a patch read when the preview moved to another file.
func (m Model) followDiff(cmd tea.Cmd) (tea.Model, tea.Cmd) {
	m.syncPreview()
	return m, tea.Batch(cmd, m.readDiff(false), m.readCommits(false), m.readArch(false), m.readPatch(false), m.readEdge(), m.watchPR())
}

// diffThread is the selected row's thread whose worktree the Files section
// shows: not for a resolved thread, whose worktree is likely gone.
func (m Model) diffThread() (deck.Thread, bool) {
	r, _ := m.selected()
	t, ok := r.thread()
	if r.kind != rowWork || !ok || t.Worktree == "" || t.Status == deck.StatusDone {
		return deck.Thread{}, false
	}
	return t, true
}

func diffKey(t deck.Thread) string { return t.ID + "\x00" + t.Worktree + "\x00" + t.Base }

// diff is the selected thread's last diff, when one was read.
func (m Model) diff() (deck.Thread, deck.Diff, bool) {
	t, ok := m.diffThread()
	if !ok || m.opt.Diff == nil {
		return t, deck.Diff{}, false
	}
	d, ok := m.diffs[diffKey(t)]
	return t, d, ok
}

// readDiff reads the selected thread's diff, again when force is set, else
// only when the selection moved to another thread. One read runs at a
// time; a move meanwhile reads again when it ends.
func (m *Model) readDiff(force bool) tea.Cmd {
	t, ok := m.diffThread()
	if m.opt.Diff == nil || !ok {
		return nil
	}
	k := diffKey(t)
	if !force && k == m.diffSel {
		return nil
	}
	if m.diffing {
		m.diffAgain = true
		return nil
	}
	m.diffing, m.diffSel = true, k
	read := m.opt.Diff
	return func() tea.Msg { return diffMsg{key: k, diff: read(context.Background(), t)} }
}

// diffKeyPressed focuses the drawer's Files tab, so a digit then previews
// that file and d (filesOnlyKey) opens the file under the cursor in the
// diff tool.
func (m *Model) diffKeyPressed() tea.Cmd {
	if m.opt.Diff == nil {
		m.status = "the Files tab is off"
		return nil
	}
	if _, ok := m.diffThread(); !ok {
		r, _ := m.selected()
		if t, has := r.thread(); r.kind == rowWork && has && t.Status == deck.StatusDone && t.Worktree != "" {
			m.status = t.ID + " is resolved"
		} else {
			m.status = "no worktree on this row"
		}
		return nil
	}
	if m.mode != modeRow || m.curTab() != tabFiles {
		m.switchTab(tabFiles)
	}
	if m.size == sizeHidden {
		m.size = sizeNormal
		m.ensureVisible()
	}
	m.dfocus = true
	return nil
}

// previewFileAt shows the file at display index i in the preview, with
// the Files tab focused and its cursor on that file. Without a preview
// (Options.Patch nil) it opens the file in the diff tool instead.
func (m *Model) previewFileAt(i int) tea.Cmd {
	t, d, ok := m.diff()
	if !ok || d.Note != "" || i < 0 || i >= len(d.Files) {
		m.status = fmt.Sprintf("no file %d on this row", i+1)
		return nil
	}
	m.dfocus = true
	m.dcur = i // every file is a stop, in display order
	m.moveDrawerCursor(0)
	if m.opt.Patch == nil {
		return m.openFile(t, d, i)
	}
	if !m.preview {
		m.togglePreview()
	}
	return nil
}

// openFile opens the file at display index i in the diff tool.
func (m *Model) openFile(t deck.Thread, d deck.Diff, i int) tea.Cmd {
	if i < 0 || i >= len(d.Files) {
		m.status = fmt.Sprintf("no file %d on this row", i+1)
		return nil
	}
	f := fileOrder(d.Files, m.tree)[i]
	if f.Untracked {
		m.status = f.Path + " is untracked: git diff shows it once it is added"
		return nil
	}
	return m.openDiff(t, d, f)
}

// filesOnlyKey handles the Files tab's own keys: t switches list and tree,
// a opens the whole diff in the diff tool, v turns the preview on and
// off, and in the focused drawer d opens the file under the cursor in the
// diff tool. done is false for any other key, so d from the list focuses
// Files.
func (m *Model) filesOnlyKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.mode != modeRow || m.curTab() != tabFiles {
		return nil, false
	}
	switch msg.String() {
	case "t":
		m.tree = !m.tree
		m.dcur = 0
		return nil, true
	case "a":
		if t, d, ok := m.diff(); ok && len(d.Files) > 0 {
			return m.openDiff(t, d, deck.DiffFile{}), true
		}
		return nil, true
	case "v":
		m.togglePreview()
		return nil, true
	case "d":
		if !m.dfocus {
			return nil, false
		}
		if t, d, ok := m.diff(); ok && d.Note == "" && len(d.Files) > 0 {
			return m.openFile(t, d, m.dcur), true
		}
		return nil, true
	}
	return nil, false
}

// switchTab shows tab in the drawer, from its top.
func (m *Model) switchTab(tab tabKind) {
	m.setMode(modeRow)
	m.choosing = noKind
	m.tab = tab
	m.dcur = 0
	m.syncImpactSel()
}

// cycleTab moves to the next (dir 1) or previous (-1) tab that has
// something behind it.
func (m *Model) cycleTab(dir int) {
	r, ok := m.selected()
	if !ok || r.kind != rowWork {
		return
	}
	tab := m.curTab()
	for range numTabs {
		tab = (tab + tabKind(dir) + numTabs) % numTabs
		if m.tabEnabled(r, tab) {
			break
		}
	}
	m.switchTab(tab)
}

// moveDrawerCursor moves the drawer cursor by delta stops and scrolls the
// drawer to keep it in view.
func (m *Model) moveDrawerCursor(delta int) {
	l := m.layout()
	if l.drawer == nil || len(l.drawer.stops) == 0 {
		m.scrollDrawer(delta)
		return
	}
	lo := 0
	if _, cs, ok := m.commits(); ok && m.preview && m.curTab() == tabCommits && len(cs.List) > 0 {
		lo = m.commitStop(cs, 0) // the preview stays on the commits
	}
	m.dcur = clamp(m.dcur+delta, lo, len(l.drawer.stops)-1)
	line := l.drawer.stops[m.dcur].line
	if line < m.drawerOff {
		m.drawerOff = line
	}
	if line >= m.drawerOff+l.drawerH {
		m.drawerOff = line - l.drawerH + 1
	}
	m.scrollDrawer(0)
}

// act does what a click or enter on a drawer stop does.
func (m *Model) act(a action) tea.Cmd {
	switch a.kind {
	case actLink:
		return m.openNumbered(a.n)
	case actFile, actDiff:
		t, d, ok := m.diff()
		switch {
		case !ok:
			return nil
		case a.kind == actDiff:
			return m.openDiff(t, d, deck.DiffFile{})
		}
		return m.previewFileAt(a.n - 1)
	case actEvent:
		switch kind, n := m.eventAction(a.n); kind {
		case cmdReport:
			m.setMode(modeReport)
		case cmdLink:
			return m.openNumbered(n)
		case cmdPane:
			return m.focusPane()
		}
	case actTab:
		if r, ok := m.selected(); ok && m.tabEnabled(r, tabKind(a.n)) {
			m.switchTab(tabKind(a.n))
		}
	case actCommit:
		if a.n == 0 {
			// The uncommitted row: those changes are the Files tab's.
			if r, ok := m.selected(); ok && m.tabEnabled(r, tabFiles) {
				m.switchTab(tabFiles)
				m.dfocus = true
			}
			return nil
		}
		m.previewCommit(a.n - 1)
	case actExpand:
		return m.toggleExpand(a.n-1, -1)
	case actCommitFile:
		m.previewCommitFile(a.n-1, a.f)
	case actCheck:
		if pr, ok := m.prOf(); ok {
			return m.openCheck(pr, a.n)
		}
	case actComment:
		return m.openComment(a.n)
	case actFold:
		m.toggleComment(a.n)
	case actFinding, actFindMore, actBox, actSite, actEdge:
		return m.impactAct(a)
	case actState:
		m.armMove()
	}
	return nil
}

// openDiff opens the diff tool on the thread's worktree against the merge
// base, so it shows what the Files section counts: one file, or with a
// zero file the whole diff. A rename passes both paths so git pairs them.
// Untracked files are not in git's diff.
func (m *Model) openDiff(t deck.Thread, d deck.Diff, f deck.DiffFile) tea.Cmd {
	open := m.opt.OpenDiff
	what := "the diff of " + t.ID
	var files []string
	if f.Path != "" {
		what = "the diff of " + f.Path
		if f.OldPath != "" {
			files = append(files, f.OldPath)
		}
		files = append(files, f.Path)
	}
	if open == nil {
		m.status = "opening diffs is off"
		return nil
	}
	base := d.MergeBase
	if base == "" {
		base = d.Base
	}
	path := t.Worktree
	return func() tea.Msg { return openedMsg{what: what, err: open(path, base, files)} }
}

func (m Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	m.status, m.notice = "", ""
	// U stops on the second press in a row, on the same thread.
	armed := m.stopArmed
	m.stopArmed = ""
	if m.pick.open {
		return m, m.pickerKey(msg)
	}
	if cmd, done := m.issueKey(msg); done {
		return m, cmd
	}
	if cmd, done := m.priorityKey(msg); done {
		return m, cmd
	}
	if m.mode == modeSettings {
		if cmd, done := m.settingsKey(msg); done {
			return m, cmd
		}
	}
	if m.choosing != noKind {
		if cmd, done := m.chooserKey(msg); done {
			return m, cmd
		}
	}
	if s := msg.String(); len(s) == 1 && s[0] >= '1' && s[0] <= '9' {
		if m.mode == modeRow && m.curTab() == tabCommits {
			m.previewCommit(int(s[0] - '1'))
			return m, nil
		}
		if m.mode == modeRow && m.curTab() == tabFiles {
			return m, m.previewFileAt(int(s[0] - '1'))
		}
		return m, m.openNumbered(int(s[0] - '1'))
	}
	if cmd, done := m.filesOnlyKey(msg); done {
		return m, cmd
	}
	if cmd, done := m.commitsOnlyKey(msg); done {
		return m, cmd
	}
	if m.prOnlyKey(msg) {
		return m, nil
	}
	if m.impactKey(msg) {
		return m, nil
	}
	if m.previewKey(msg) {
		return m, nil
	}
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m, tea.Quit
	case key.Matches(msg, m.keys.NextTab):
		m.cycleTab(1)
	case key.Matches(msg, m.keys.PrevTab):
		m.cycleTab(-1)
	case key.Matches(msg, m.keys.Focus):
		if m.dfocus {
			m.cycleTab(1)
		} else if r, ok := m.selected(); ok && r.kind == rowWork && m.size != sizeHidden {
			m.setMode(modeRow)
			m.dfocus, m.dcur = true, 0
			m.syncImpactSel()
		}
	case key.Matches(msg, m.keys.Unfocus):
		if m.dfocus {
			m.cycleTab(-1)
		}
	case m.dfocus && key.Matches(msg, m.keys.Down):
		m.moveDrawerCursor(1)
	case m.dfocus && key.Matches(msg, m.keys.Up):
		m.moveDrawerCursor(-1)
	case m.dfocus && key.Matches(msg, m.keys.Pane):
		if l := m.layout(); l.drawer != nil && m.dcur < len(l.drawer.stops) {
			return m, m.act(l.drawer.stops[m.dcur].act)
		}
		return m, m.focusPane()
	case m.dfocus && key.Matches(msg, m.keys.Back):
		m.dfocus = false
	case m.mode == modeCheck && key.Matches(msg, m.keys.Pane):
		return m, m.openURL(m.logCheck.URL, m.logCheck.Name)
	case key.Matches(msg, m.keys.Check):
		return m, m.checkKey()
	case key.Matches(msg, m.keys.Down):
		m.move(1)
	case key.Matches(msg, m.keys.Up):
		m.move(-1)
	case key.Matches(msg, m.keys.Fold):
		m.toggleFold()
	case key.Matches(msg, m.keys.Linear):
		return m, m.linkKey(deck.LinkLinear)
	case key.Matches(msg, m.keys.Figma):
		return m, m.linkKey(deck.LinkFigma)
	case key.Matches(msg, m.keys.Notion):
		return m, m.linkKey(deck.LinkNotion)
	case key.Matches(msg, m.keys.PR):
		return m, m.linkKey(deck.LinkGitHub)
	case key.Matches(msg, m.keys.Localhost):
		return m, m.linkKey(deck.LinkLocalhost)
	case key.Matches(msg, m.keys.Pane):
		return m, m.focusPane()
	case key.Matches(msg, m.keys.Editor):
		return m, m.openEditor()
	case key.Matches(msg, m.keys.DevUp):
		return m, m.startDev()
	case key.Matches(msg, m.keys.DevStop):
		return m, m.stopDev(armed)
	case key.Matches(msg, m.keys.Diff):
		return m, m.diffKeyPressed()
	case key.Matches(msg, m.keys.Report):
		m.toggleReport()
	case key.Matches(msg, m.keys.Priority):
		if is, ok := m.selectedIssue(); ok {
			m.armIssuePriority(is)
		} else {
			m.armPriority()
		}
	case key.Matches(msg, m.keys.Move):
		m.armMove()
	case key.Matches(msg, m.keys.View):
		m.toggleView()
	case !m.dfocus && m.view != viewTasks && key.Matches(msg, m.keys.PrevView):
		m.stepView(-1)
	case !m.dfocus && m.view != viewTasks && key.Matches(msg, m.keys.NextView):
		m.stepView(1)
	case key.Matches(msg, m.keys.Page):
		return m, m.openBoardPage()
	case key.Matches(msg, m.keys.Drawer):
		prev, _ := m.impactStopAct()
		m.size = (m.size + 1) % 3
		if m.size == sizeHidden {
			m.dfocus = false
		}
		m.drawerOff = 0
		m.ensureVisible()
		m.relocateImpact(prev)
	case key.Matches(msg, m.keys.Grow):
		return m, m.resizeDrawer(1)
	case key.Matches(msg, m.keys.Shrink):
		return m, m.resizeDrawer(-1)
	case key.Matches(msg, m.keys.Sources):
		m.toggleMode(modeSources)
	case key.Matches(msg, m.keys.Help):
		m.toggleMode(modeHelp)
	case key.Matches(msg, m.keys.Settings):
		return m, m.openSettings()
	case key.Matches(msg, m.keys.News):
		m.toggleMode(modeNews)
	case key.Matches(msg, m.keys.Projects):
		m.openPicker()
	case key.Matches(msg, m.keys.ImpactPane):
		cmd := m.openImpactPane()
		return m, cmd
	case key.Matches(msg, m.keys.PageDown):
		m.scrollDrawer(max(m.layout().drawerH/2, 1))
	case key.Matches(msg, m.keys.PageUp):
		m.scrollDrawer(-max(m.layout().drawerH/2, 1))
	case key.Matches(msg, m.keys.Back):
		m.setMode(modeRow)
	}
	return m, nil
}

func (m *Model) move(delta int) {
	if len(m.rows) == 0 {
		return
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	m.cursor = m.offGap(clamp(m.cursor+delta, 0, len(m.rows)-1), dir)
	m.moved = true
	m.dcur = 0
	m.setMode(modeRow)
	m.ensureVisible()
}

func (m *Model) setMode(mode drawerMode) {
	m.mode = mode
	if mode != modeRow {
		m.dfocus = false // a full view has no drawer cursor
	}
	m.drawerOff = 0
	m.ensureVisible()
}

func (m *Model) toggleMode(mode drawerMode) {
	if m.mode == mode {
		m.setMode(modeRow)
	} else {
		m.setMode(mode)
	}
}

func (m *Model) toggleFold() {
	r, ok := m.selected()
	if !ok || r.kind != rowHeading || !r.foldable {
		return
	}
	m.folds[r.list] = !r.folded
	m.rows = m.buildRows(m.snap)
	m.ensureVisible()
}

func (m *Model) toggleReport() {
	if m.mode == modeReport {
		m.setMode(modeRow)
		return
	}
	r, _ := m.selected()
	t, ok := r.thread()
	switch {
	case r.kind != rowWork || !ok:
		m.status = "no thread on this row"
	case t.Report == "":
		m.status = t.ID + " has no report yet"
	default:
		m.setMode(modeReport)
	}
}

// links returns the selected row's links in drawer order.
func (m Model) links() []deck.Link {
	r, ok := m.selected()
	if !ok {
		return nil
	}
	return rowLinks(r)
}

// linkKey opens the row's only link of kind, or opens the chooser when there
// are several. Pressed again in the chooser, it opens the first.
func (m *Model) linkKey(kind deck.LinkKind) tea.Cmd {
	if kind == deck.LinkLocalhost {
		return m.openLocalhost()
	}
	var of []int
	for i, l := range m.links() {
		if l.Kind == kind {
			of = append(of, i)
		}
	}
	switch len(of) {
	case 0:
		m.status = "no " + kind.String() + " link on this row"
		return nil
	case 1:
		return m.openNumbered(of[0])
	}
	m.setMode(modeRow)
	m.tab = tabOverview // the chooser highlights Overview's chips
	m.choosing = kind
	m.desktop = false
	return nil
}

// openLocalhost opens the row's first localhost link whose dev server is
// running. Links to servers that are down open only by their digit.
func (m *Model) openLocalhost() tea.Cmd {
	some := false
	for i, l := range m.links() {
		if l.Kind != deck.LinkLocalhost {
			continue
		}
		if !l.Down {
			return m.openNumbered(i)
		}
		some = true
	}
	if some {
		m.status = "no dev server on this row is running; a digit opens a link anyway"
	} else {
		m.status = "no localhost link on this row"
	}
	return nil
}

// chooserKey handles a key while the chooser waits. done is false when the
// key closes the chooser and should then be handled as usual.
func (m *Model) chooserKey(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	kind := m.choosing
	desktop := m.desktop
	m.choosing, m.desktop = noKind, false
	links := m.links()
	s := msg.String()
	switch {
	case len(s) == 1 && s[0] >= '1' && s[0] <= '9':
		i := int(s[0] - '1')
		if desktop {
			return m.openDesktop(links, i), true
		}
		return m.openNumbered(i), true
	case s == "a":
		var cmds []tea.Cmd
		for _, l := range links {
			if l.Kind == kind {
				cmds = append(cmds, m.openLink(l))
			}
		}
		return tea.Batch(cmds...), true
	case s == "d" && kind == deck.LinkFigma:
		m.choosing, m.desktop = kind, true
		return nil, true
	case key.Matches(msg, m.keys.Back):
		return nil, true
	case s == kindKey(kind):
		for i, l := range links {
			if l.Kind == kind {
				if desktop {
					return m.openDesktop(links, i), true
				}
				return m.openNumbered(i), true
			}
		}
		return nil, true
	}
	return nil, false
}

func kindKey(k deck.LinkKind) string {
	switch k {
	case deck.LinkLinear:
		return "l"
	case deck.LinkFigma:
		return "f"
	case deck.LinkNotion:
		return "n"
	case deck.LinkGitHub:
		return "g"
	case deck.LinkLocalhost:
		return "o"
	}
	return ""
}

func (m *Model) openNumbered(i int) tea.Cmd {
	links := m.links()
	if i < 0 || i >= len(links) {
		m.status = fmt.Sprintf("no link %d on this row", i+1)
		return nil
	}
	return m.openLink(links[i])
}

// openLink opens l, a Figma link in the desktop app when FigmaDesktop is set.
func (m *Model) openLink(l deck.Link) tea.Cmd {
	if d := l.DesktopURL(); d != "" && m.opt.FigmaDesktop {
		return m.openURL(d, l.Label+" in Figma")
	}
	return m.openURL(l.URL, l.Label)
}

func (m *Model) openDesktop(links []deck.Link, i int) tea.Cmd {
	if i < 0 || i >= len(links) || links[i].DesktopURL() == "" {
		m.status = fmt.Sprintf("link %d is not a Figma link", i+1)
		return nil
	}
	return m.openURL(links[i].DesktopURL(), links[i].Label+" in Figma")
}

func (m *Model) openURL(url, what string) tea.Cmd {
	if url == "" {
		m.status = what + ": " + noWorkspace
		if m.snap.LinearKey {
			m.status = what + ": no URL from Linear yet"
		}
		return nil
	}
	return m.openOrCopy(url, what)
}

// openOrCopy opens url, or copies it with CopyLinks, and says so in the status
// line.
func (m *Model) openOrCopy(url, what string) tea.Cmd {
	if m.opt.CopyLinks {
		m.status = "copied the link to " + what + " (no browser on this machine)"
		return tea.SetClipboard(url)
	}
	open := m.opt.OpenURL
	if open == nil {
		m.status = "opening links is off: " + url
		return nil
	}
	return func() tea.Msg { return openedMsg{what: what, err: open(url)} }
}

func (m *Model) openEditor() tea.Cmd {
	r, _ := m.selected()
	t, ok := r.thread()
	if !ok || t.Worktree == "" {
		m.status = "no worktree on this row"
		return nil
	}
	open := m.opt.OpenEditor
	if open == nil {
		m.status = "opening the editor is off: " + t.Worktree
		return nil
	}
	path := t.Worktree
	return func() tea.Msg { return openedMsg{what: tilde(path), err: open(path)} }
}

// startDev runs dev for the selected thread's worktree.
func (m *Model) startDev() tea.Cmd {
	t, ok := m.devThread()
	if !ok {
		return nil
	}
	start := m.opt.StartDev
	if start == nil {
		m.status = "starting dev servers is off"
		return nil
	}
	m.status = "starting " + t.ID + "'s dev servers…"
	return func() tea.Msg {
		status, err := start(t)
		return devUpMsg{status: status, err: err}
	}
}

// stopDev stops what dev started in the selected thread's worktree, once U
// is pressed a second time on it.
func (m *Model) stopDev(armed string) tea.Cmd {
	t, ok := m.devThread()
	if !ok {
		return nil
	}
	stop := m.opt.StopDev
	if stop == nil {
		m.status = "stopping dev servers is off"
		return nil
	}
	if armed != t.ID {
		m.stopArmed = t.ID
		m.status = "press U again to stop " + t.ID + "'s dev servers"
		return nil
	}
	m.status = "stopping " + t.ID + "'s dev servers…"
	return func() tea.Msg {
		status, err := stop(t)
		return devUpMsg{status: status, err: err, stop: true}
	}
}

// devThread is the selected row's thread, when u and U can act on it.
func (m *Model) devThread() (deck.Thread, bool) {
	r, _ := m.selected()
	t, ok := r.thread()
	switch {
	case r.kind != rowWork || !ok:
		m.status = "no thread on this row"
		return t, false
	case t.Worktree == "":
		m.status = "no worktree on this row"
		return t, false
	case t.Status == deck.StatusDone:
		m.status = t.ID + " is done"
		return t, false
	}
	return t, true
}

// focusPane focuses the herdr pane of the selected row's thread.
func (m *Model) focusPane() tea.Cmd {
	r, _ := m.selected()
	t, ok := r.thread()
	switch {
	case r.pull != nil:
		// A PR has no pane: ↵ opens it, as g does.
		return m.linkKey(deck.LinkGitHub)
	case r.kind != rowWork:
		m.status = "no pane on this row"
		return nil
	case !ok:
		m.status = "no thread on this row"
		return nil
	}
	id := ""
	switch {
	case t.Pane != nil:
		id = t.Pane.ID
	case !m.snap.Herdr && t.PaneID != "":
		// herdr's live state is unknown: try the recorded pane.
		id = t.PaneID
	default:
		m.status = t.ID + " has no open pane"
		return nil
	}
	focus := m.opt.FocusPane
	if focus == nil {
		m.status = "focusing panes is off: " + id
		return nil
	}
	label := t.ID + "'s pane " + id
	return func() tea.Msg { return openedMsg{what: label, err: focus(id), verb: "focus"} }
}

func (m *Model) handleClick(mouse tea.Mouse) (tea.Model, tea.Cmd) {
	if mouse.Button != tea.MouseLeft {
		return *m, nil
	}
	m.status, m.notice = "", ""
	if m.pick.open {
		return *m, m.pickClick(mouse.Y)
	}
	if m.moveArmed != "" {
		return *m, m.statePickClick(mouse)
	}
	l := m.layout()
	if k, s, top := m.barAt(l, mouse); k != barNone {
		// A scrollbar: a press grabs the thumb, or jumps there on the
		// track, and a drag goes on scrolling.
		var off int
		m.barGrab, off = s.grab(mouse.Y - top)
		m.bar = k
		m.setBarOff(k, off)
		return *m, nil
	}
	switch {
	case l.sepY >= 0 && mouse.Y == l.sepY && m.effectiveSize() == sizeNormal:
		// The rule above the drawer: a press starts a drag, and does
		// nothing else.
		if _, _, _, ok := m.drawerBlocks(); ok {
			m.dragging, m.dragFrom = true, m.frac
		}
	case l.attn > 0 && mouse.Y == l.attn:
		m.openPicker()
	case mouse.Y == 0 && mouse.X >= l.bang.x0 && mouse.X < l.bang.x1:
		m.toggleMode(modeSources)
	case mouse.Y == 0 && m.viewTabAt(mouse.X) != viewTasks:
		m.showView(m.viewTabAt(mouse.X))
	case (m.preview || m.edgeShown()) && mouse.Y >= l.listTop-1 && mouse.Y < l.listTop+l.listH:
		// The preview or an edge's code has the list's place: a click
		// there does nothing.
	case mouse.Y >= l.listTop && mouse.Y < l.listTop+l.listH:
		i := m.listOff + mouse.Y - l.listTop
		if i >= len(m.rows) || m.rows[i].kind == rowGap {
			break
		}
		if x0, x1, ok := m.prCell(m.rows[i]); ok && mouse.X >= x0 && mouse.X < x1 {
			return *m, m.showPRTab(i)
		}
		onMark := m.rows[i].issue != nil && mouse.X >= markX && mouse.X <= markX+1
		if i != m.cursor {
			m.dcur = 0
		}
		m.cursor, m.moved = i, true
		m.choosing, m.dfocus = noKind, false
		m.setMode(modeRow)
		if m.rows[i].kind == rowHeading {
			m.toggleFold()
		}
		if onMark {
			m.armMove()
		}
	case l.drawer != nil && l.tabY >= 0 && mouse.Y == l.tabY:
		m.focusDrawer()
		for _, z := range l.drawer.tabZones {
			if mouse.X >= z.x0 && mouse.X < z.x1 {
				return *m, m.act(z.act)
			}
		}
	case mouse.Y >= l.drawerTop && mouse.Y < l.drawerTop+l.drawerH:
		m.scrollDrawer(0) // match the offset the screen was drawn with
		dl := l.drawer.lines
		i := m.drawerOff + mouse.Y - l.drawerTop
		if i >= len(dl) {
			break
		}
		if cmd, done := m.impactClick(l, i, mouse.X); done {
			return *m, cmd
		}
		if s := dl[i].setting; s > 0 && !m.set.editing {
			m.set.cursor = s - 1
			m.followSetting()
			break
		}
		m.focusDrawer()
		for _, z := range dl[i].zones {
			if mouse.X < z.x0 || mouse.X >= z.x1 {
				continue
			}
			m.choosing = noKind
			// The cursor goes to the stop clicked, or for a part of a
			// line that is not a stop itself (a fold's ▸), the line's.
			at := -1
			for k, st := range l.drawer.stops {
				if st.line == i && (st.act == z.act || at < 0) {
					at = k
				}
				if st.line == i && st.act == z.act {
					break
				}
			}
			if at >= 0 {
				m.dcur = at
			}
			return *m, m.act(z.act)
		}
	}
	return *m, nil
}

// barOf is view k's scrollbar in frame l, if it shows, with its
// track's first screen line.
func (m Model) barOf(k barKind, l frame) (scrollbar, int, bool) {
	switch k {
	case barPreview:
		s := m.previewBar(m.width, l.listH)
		return s, l.listTop, m.preview && !m.pick.open && s.shown()
	case barDrawer:
		s := l.drawerBar(m.drawerOff)
		return s, l.drawerTop, l.barred && !m.pick.open && s.shown()
	}
	return scrollbar{}, 0, false
}

// barAt is the scrollbar under the pointer, if any, with its place and
// its track's first screen line.
func (m Model) barAt(l frame, mouse tea.Mouse) (barKind, scrollbar, int) {
	if mouse.X != m.width-1 {
		return barNone, scrollbar{}, 0
	}
	for _, k := range []barKind{barPreview, barDrawer} {
		if s, top, ok := m.barOf(k, l); ok && mouse.Y >= top && mouse.Y < top+s.h {
			return k, s, top
		}
	}
	return barNone, scrollbar{}, 0
}

// barTo drags the held scrollbar's thumb to screen line y, keeping the
// row of it the press took hold of under the pointer. While that row
// stays where the thumb is drawn, the view stays put: the thumb's place
// is rounded, so mapping it back could move the view a little.
func (m *Model) barTo(y int) {
	s, top, ok := m.barOf(m.bar, m.layout())
	if !ok {
		m.bar = barNone // the view went away
		return
	}
	want := y - top - m.barGrab
	if cur, _ := s.thumb(); want == cur {
		return
	}
	m.setBarOff(m.bar, s.offsetAt(want))
}

func (m *Model) setBarOff(k barKind, off int) {
	switch k {
	case barPreview:
		m.prevOff = off
		m.scrollPreview(0)
	case barDrawer:
		m.drawerOff = off
		m.scrollDrawer(0)
	}
}

// dragTo moves the rule above the drawer to screen line y, within the
// list's and the drawer's limits.
func (m *Model) dragTo(y int) {
	_, lo, hi, ok := m.drawerBlocks()
	if !ok {
		return
	}
	body := max(m.height, 8) - 4
	listTop := 3 // see layout
	m.setDrawerBlock(clamp(body-1-(y-listTop), lo, hi))
}

// resizeDrawer grows (or shrinks) the drawer by delta lines and keeps the
// new height; a hidden drawer grows to its normal size and a full one
// shrinks to it.
func (m *Model) resizeDrawer(delta int) tea.Cmd {
	switch {
	case m.size == sizeHidden && delta > 0, m.size == sizeFull && delta < 0:
		m.size = sizeNormal
		m.drawerOff = 0
		m.ensureVisible()
		return nil
	case m.effectiveSize() != sizeNormal:
		return nil
	}
	block, lo, hi, ok := m.drawerBlocks()
	if !ok || clamp(block+delta, lo, hi) == block {
		return nil
	}
	m.setDrawerBlock(block + delta)
	return m.keepDrawer()
}

// setDrawerBlock gives the drawer block lines at its normal size, as a
// share of the pane, so the height follows the pane's.
func (m *Model) setDrawerBlock(block int) {
	m.frac = float64(block) / float64(max(m.height, 8)-4)
	m.relayoutDrawer()
}

// relayoutDrawer keeps the list's cursor, the drawer's and the preview's
// scroll in view after the drawer's height changed.
func (m *Model) relayoutDrawer() {
	m.scrollDrawer(0)
	m.ensureVisible()
	m.scrollPreview(0)
}

// keepDrawer keeps the drawer's height for the next start.
func (m *Model) keepDrawer() tea.Cmd {
	keep, frac := m.opt.KeepDrawer, m.frac
	if keep == nil {
		return nil
	}
	return func() tea.Msg { return drawerKeptMsg{err: keep(frac)} }
}

// DrawerHeight is the drawer's share of the pane at its normal size, for a
// deck that takes over from this one.
func (m Model) DrawerHeight() float64 { return m.frac }

// focusDrawer gives a row's drawer the focus, as a click in it does.
func (m *Model) focusDrawer() {
	if r, ok := m.selected(); ok && r.kind == rowWork && m.mode == modeRow && m.size != sizeHidden {
		m.dfocus = true
	}
}

func (m *Model) handleWheel(mouse tea.Mouse) {
	delta := 0
	switch mouse.Button {
	case tea.MouseWheelDown:
		delta = 1
	case tea.MouseWheelUp:
		delta = -1
	default:
		return
	}
	if m.pick.open {
		m.pickMove(delta)
		return
	}
	// The wheel scrolls what is under the pointer. It moves the list's
	// selection only over the list itself, never over a view that has the
	// list's place (the diff preview, an edge's code).
	l := m.layout()
	inList := mouse.Y >= l.listTop-1 && mouse.Y < l.listTop+l.listH
	switch {
	case m.preview && inList:
		m.scrollPreview(delta * 3)
	case m.edgeShown() && inList:
		m.scrollEdge(delta * 3)
	case mouse.Y >= l.listTop && mouse.Y < l.listTop+l.listH:
		m.move(delta)
	case l.drawerH > 0 && mouse.Y >= l.listTop+l.listH && mouse.Y < l.drawerTop+l.drawerH:
		m.scrollDrawer(delta * 3)
	}
}

// scrollDrawer moves the drawer's view by delta lines, within its content.
func (m *Model) scrollDrawer(delta int) {
	l := m.layout()
	most := 0
	if l.drawer != nil {
		most = max(len(l.drawer.lines)-l.drawerH, 0)
	}
	m.drawerOff = clamp(m.drawerOff+delta, 0, most)
}

// ensureVisible scrolls the list so the cursor shows.
func (m *Model) ensureVisible() {
	h := m.layout().listH
	if h <= 0 {
		return
	}
	if m.cursor < m.listOff {
		m.listOff = m.cursor
	}
	if m.cursor >= m.listOff+h {
		m.listOff = m.cursor - h + 1
	}
	m.listOff = clamp(m.listOff, 0, max(len(m.rows)-h, 0))
}

func (m Model) loc() *time.Location {
	if m.opt.Location != nil {
		return m.opt.Location
	}
	return time.Local
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	return max(lo, min(v, hi))
}
