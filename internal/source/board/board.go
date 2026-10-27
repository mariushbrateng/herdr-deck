// Package board reads a herdr-projects 1.x work board into a deck.Snapshot.
//
// herdr-projects 1.0 replaced 0.2's project folders (PROJECT.md, TASKS.md,
// threads/t-*.toml) with tasks: one folder per task under
// <home>/data/tasks/<repo>/<n>/, and the CLI `herdr-projects list --json`,
// `details <id>` and `show <id>`. The reader prefers the CLI and reads a
// task's folder only for what the CLI leaves out (its pane, base branch,
// repo root and context use), or for everything when the CLI cannot run.
//
// The reader never writes under the home folder. The commands it runs are
// `list --json --all` and `details <id>`; both only read.
package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/FredricW/herdr-deck/internal/deck"
	"github.com/FredricW/herdr-deck/internal/source/tasks"
)

// Slug is the deck's project slug for the whole board.
const Slug = "board"

// commandTimeout bounds one CLI call; the task folders are read when it
// runs out.
const commandTimeout = 5 * time.Second

// RunFunc runs a command and returns its standard output.
type RunFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

// Reader reads the board kept in Home, ~/.herdr-projects.
type Reader struct {
	Home string
	// Bin is the herdr-projects binary; empty means the one the board
	// hands its agents, <home>/data/bin/herdr-projects, else
	// herdr-projects on $PATH.
	Bin string
	// Run runs Bin; nil means exec. Tests replace it.
	Run RunFunc
	// Repo, when set, keeps only that repo's tasks.
	Repo string
	// Archived keeps archived tasks too.
	Archived bool
	// LinearWorkspace is the slug bare Linear IDs link into.
	LinearWorkspace string
	// Priorities gives the deck's own priority per task; nil gives none.
	Priorities *Priorities
	// Diff reads a task's worktree changes, for the critical mark and the
	// frontend folder; nil leaves both to the review gate alone.
	Diff func(ctx context.Context, t deck.Thread) deck.Diff
	// Rules says which changed paths are critical in a repo; nil uses
	// the default rules only.
	Rules *Rules
}

// Detect reports whether home holds a herdr-projects 1.x board: its
// data/tasks folder.
func Detect(home string) bool {
	if home == "" {
		return false
	}
	fi, err := os.Stat(filepath.Join(home, "data", "tasks"))
	return err == nil && fi.IsDir()
}

// TasksDir is the folder the board keeps its tasks in; a change there is a
// change on the board.
func (r Reader) TasksDir() string { return filepath.Join(r.Home, "data", "tasks") }

func (r Reader) bin() string {
	if r.Bin != "" {
		return r.Bin
	}
	own := filepath.Join(r.Home, "data", "bin", "herdr-projects")
	if fi, err := os.Stat(own); err == nil && !fi.IsDir() {
		return own
	}
	return "herdr-projects"
}

func (r Reader) run(ctx context.Context, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	run := r.Run
	if run == nil {
		run = execRun
	}
	return run(ctx, r.bin(), args...)
}

func execRun(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("%w: %s", err, firstLine(msg))
		}
		return nil, err
	}
	return out, nil
}

// listed is one task of `list --json`.
type listed struct {
	ID          string          `json:"id"`
	State       string          `json:"state"`
	Kind        string          `json:"kind"`
	App         string          `json:"app"`
	Title       string          `json:"title"`
	Msg         string          `json:"msg"`
	Stage       string          `json:"stage"`
	Progress    *float64        `json:"progress"`
	ETA         *float64        `json:"eta"`
	PR          string          `json:"pr"`
	Research    json.RawMessage `json:"research"`
	Chat        bool            `json:"chat"`
	Queue       int             `json:"queue"`
	ArchivedAt  json.RawMessage `json:"archivedAt"`
	Review      *review         `json:"review"`
	ReviewRound int             `json:"reviewRound"`
}

type review struct {
	Artifacts []struct {
		Kind string `json:"kind"`
		Link string `json:"link"`
	} `json:"artifacts"`
	Meets    *bool     `json:"meets"`
	Pass     *bool     `json:"pass"`
	Gate     string    `json:"gate"`
	Text     string    `json:"text"`
	Findings []finding `json:"findings"`
}

type finding struct {
	ID          string `json:"id"`
	Severity    string `json:"severity"`
	Subject     string `json:"subject"`
	Where       string `json:"where"`
	Preexisting bool   `json:"preexisting"`
}

// details is the part of `details <id>` the deck uses.
type details struct {
	Task struct {
		ID          string `json:"id"`
		Description string `json:"description"`
	} `json:"task"`
	Report *struct {
		File string `json:"file"`
		Link string `json:"link"`
	} `json:"report"`
	Review   *review         `json:"review"`
	PR       json.RawMessage `json:"pr"`
	Worktree string          `json:"worktree"`
	Branch   string          `json:"branch"`
	Limits   struct {
		Time struct {
			UsedMs float64 `json:"usedMs"`
		} `json:"time"`
		Tokens struct {
			Used float64 `json:"used"`
		} `json:"tokens"`
	} `json:"limits"`
}

// taskFile is the part of a task's task.json the deck uses.
type taskFile struct {
	ID          string          `json:"id"`
	Repo        string          `json:"repo"`
	N           int             `json:"n"`
	Root        string          `json:"root"`
	Title       string          `json:"title"`
	Description string          `json:"description"`
	Stage       string          `json:"stage"`
	Msg         string          `json:"msg"`
	App         string          `json:"app"`
	Model       string          `json:"model"`
	Created     int64           `json:"created"`
	At          int64           `json:"at"`
	Worktree    string          `json:"worktree"`
	Branch      string          `json:"branch"`
	From        string          `json:"from"`
	PR          string          `json:"pr"`
	PRState     string          `json:"prState"`
	PRMerged    json.RawMessage `json:"prMerged"`
	ArchivedAt  json.RawMessage `json:"archivedAt"`
	ResearchRun bool            `json:"researchRun"`
	Link        *struct {
		Pane string `json:"pane"`
	} `json:"link"`
	Usage *struct {
		Context float64 `json:"context"`
	} `json:"usage"`
	Report *struct {
		File string `json:"file"`
		Link string `json:"link"`
	} `json:"report"`
}

// Read returns what can be read of the board now. Like the 0.2 reader it
// never fails: what could not be read is named in Snapshot.Missing.
func (r Reader) Read(ctx context.Context) deck.Snapshot {
	snap := deck.Snapshot{Project: deck.Project{Slug: Slug, Name: "Board", Dir: r.TasksDir()}}
	if r.Repo != "" {
		snap.Project.Name = r.Repo
	}
	list, err := r.list(ctx)
	if err != nil {
		snap.Missing = append(snap.Missing, "herdr-projects list: "+err.Error()+": tasks from their folders")
		list = r.listFiles(&snap)
	}
	var prios map[string]int
	if r.Priorities != nil {
		if prios, err = r.Priorities.Load(); err != nil {
			snap.Missing = append(snap.Missing, "priorities: "+err.Error())
		}
	}
	repos := map[string]bool{}
	for _, l := range list {
		repo, _ := splitID(l.ID)
		if r.Repo != "" && repo != r.Repo {
			continue
		}
		if !r.Archived && archived(l.ArchivedAt) {
			continue
		}
		t := r.thread(ctx, l, &snap)
		t.Board.Priority = prios[l.ID]
		snap.Threads = append(snap.Threads, t)
		repos[repo] = true
	}
	for repo := range repos {
		snap.Project.Repos = append(snap.Project.Repos, repo)
	}
	sort.Strings(snap.Project.Repos)
	Sort(snap.Threads)
	snap.TaskLists = Lists(snap.Threads)
	return snap
}

func (r Reader) list(ctx context.Context) ([]listed, error) {
	args := []string{"list", "--json"}
	if r.Archived {
		args = append(args, "--all")
	}
	out, err := r.run(ctx, args...)
	if err != nil {
		return nil, err
	}
	var list []listed
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("unreadable output: %w", err)
	}
	return list, nil
}

// listFiles is the CLI's list read from the task folders, for when the CLI
// cannot run: without its live message and progress.
func (r Reader) listFiles(snap *deck.Snapshot) []listed {
	paths, _ := filepath.Glob(filepath.Join(r.TasksDir(), "*", "*", "task.json"))
	var list []listed
	for _, p := range paths {
		var f taskFile
		if err := readJSON(p, &f); err != nil {
			snap.Missing = append(snap.Missing, p+": "+err.Error())
			continue
		}
		if f.ID == "" {
			continue
		}
		list = append(list, listed{ID: f.ID, Title: f.Title, Msg: f.Msg, Stage: f.Stage, App: f.App, PR: f.PR, ArchivedAt: f.ArchivedAt})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	return list
}

// thread turns one listed task, its details and its folder into a thread.
func (r Reader) thread(ctx context.Context, l listed, snap *deck.Snapshot) deck.Thread {
	repo, n := splitID(l.ID)
	var f taskFile
	if err := readJSON(filepath.Join(r.TasksDir(), repo, n, "task.json"), &f); err != nil && !errors.Is(err, os.ErrNotExist) {
		snap.Missing = append(snap.Missing, l.ID+" task.json: "+err.Error())
	}
	var d details
	if out, err := r.run(ctx, "details", l.ID); err == nil {
		if err := json.Unmarshal(out, &d); err != nil {
			snap.Missing = append(snap.Missing, l.ID+" details: unreadable output")
		}
	}

	key := d.Task.ID
	if key == "" {
		key = l.ID
	}
	b := &deck.BoardTask{
		ID:          l.ID,
		Repo:        repo,
		Stage:       l.Stage,
		State:       l.State,
		Msg:         strings.TrimSpace(l.Msg),
		Research:    research(l.Research) || f.ResearchRun,
		Chat:        l.Chat,
		Queue:       l.Queue,
		App:         first(l.App, f.App),
		Model:       f.Model,
		Description: first(d.Task.Description, f.Description),
		Elapsed:     time.Duration(d.Limits.Time.UsedMs) * time.Millisecond,
		Tokens:      int(d.Limits.Tokens.Used),
	}
	if l.Progress != nil {
		p := int(*l.Progress)
		b.Progress = &p
	}
	if l.ETA != nil {
		e := int(*l.ETA)
		b.ETA = &e
	}
	if f.Usage != nil {
		b.Context = int(f.Usage.Context)
	}
	rv := l.Review
	if d.Review != nil {
		rv = d.Review
	}
	if rv != nil {
		b.Review = &deck.BoardReview{Pass: rv.Pass != nil && *rv.Pass, Gate: rv.Gate, Text: rv.Text, Round: l.ReviewRound}
		for _, a := range rv.Artifacts {
			if a.Kind == "review" && b.ReviewURL == "" {
				b.ReviewURL = a.Link
			}
		}
		for _, x := range rv.Findings {
			b.Review.Findings = append(b.Review.Findings, deck.Finding{ID: x.ID, Severity: x.Severity, Subject: x.Subject, Where: x.Where})
		}
	}
	if rep := d.Report; rep != nil {
		b.ReportURL = rep.Link
		b.ReportFile = r.reportPath(repo, n, rep.File)
	} else if rep := f.Report; rep != nil {
		b.ReportURL = rep.Link
		b.ReportFile = r.reportPath(repo, n, rep.File)
	}

	prURL := first(l.PR, prString(d.PR), f.PR)
	merged := f.PRState == "merged" || truthy(f.PRMerged)
	b.Phase = PhaseOf(l.Stage, l.State, merged)

	t := deck.Thread{
		ID:       key,
		Title:    l.Title,
		Status:   b.Phase.Status(),
		Group:    l.Stage,
		Worktree: first(d.Worktree, f.Worktree),
		Repo:     f.Root,
		Branch:   first(d.Branch, f.Branch),
		Base:     f.From,
		Board:    b,
		Created:  millis(f.Created),
		Changed:  millis(f.At),
	}
	if f.Link != nil {
		t.PaneID = f.Link.Pane
	}
	if b.Progress != nil {
		t.Percent = b.Progress
	}
	t.StateLine = b.Phase.String()
	if b.Phase == deck.PhaseWorking || b.Phase == deck.PhaseAsks {
		t.Activity = b.Msg
	}
	if prURL != "" {
		t.PR = pullRequest(prURL, f.PRState, merged)
	}
	if b.ReportFile != "" {
		if data, err := os.ReadFile(b.ReportFile); err == nil && filepath.Ext(b.ReportFile) == ".md" {
			t.Report = string(data)
		}
	}
	if t.Report == "" {
		if data, err := os.ReadFile(filepath.Join(r.TasksDir(), repo, n, "closing-note.md")); err == nil {
			t.Report = string(data)
		}
	}
	t.Links = r.links(l.Title, b.Description, t.Report)
	if t.PR != nil {
		t.Links = deck.AppendLink(t.Links, deck.Link{Kind: deck.LinkGitHub, Label: "PR #" + strconv.Itoa(t.PR.Number), URL: t.PR.URL})
	}
	t.Log = logOf(t, b)

	r.mark(ctx, &t)
	return t
}

// mark sets the task's critical reasons and frontend folder from its review
// and its worktree's changes.
func (r Reader) mark(ctx context.Context, t *deck.Thread) {
	b := t.Board
	if b.Review != nil {
		if strings.EqualFold(b.Review.Gate, "critical") {
			b.Critical = append(b.Critical, "review tier critical")
		}
		for _, f := range b.Review.Findings {
			if f.Severity == "critical" {
				b.Critical = append(b.Critical, "critical review finding "+f.ID)
				break
			}
		}
	}
	if r.Diff == nil || t.Worktree == "" || b.Phase == deck.PhaseTodo {
		return
	}
	if fi, err := os.Stat(t.Worktree); err != nil || !fi.IsDir() {
		return
	}
	d := r.Diff(ctx, *t)
	paths := make([]string, 0, len(d.Files))
	for _, f := range d.Files {
		paths = append(paths, f.Path)
		if f.OldPath != "" {
			paths = append(paths, f.OldPath)
		}
	}
	rules := r.Rules
	if rules == nil {
		rules = &Rules{}
	}
	b.Critical = append(b.Critical, rules.Critical(t.Repo, paths)...)
	b.Frontend = Frontend(t.Worktree, paths)
}

func (r Reader) reportPath(repo, n, file string) string {
	if file == "" {
		return ""
	}
	if filepath.IsAbs(file) {
		return file
	}
	for _, p := range []string{
		filepath.Join(r.Home, "reports", repo, n, file),
		filepath.Join(r.TasksDir(), repo, n, "artifacts", "report", file),
		filepath.Join(r.TasksDir(), repo, n, "artifacts", file),
		// The report the agent wrote, when the board named another.
		filepath.Join(r.Home, "reports", repo, n, "index.html"),
		filepath.Join(r.Home, "reports", repo, n, "report.md"),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// titleLinear is the Linear ID a task's title starts with: "ENG-1025 Lag …".
var titleLinear = regexp.MustCompile(`^\s*\[?([A-Z][A-Z0-9]{1,5}-\d+)\]?\b`)

// LinearID returns the Linear issue ID the title starts with, or "".
func LinearID(title string) string {
	if m := titleLinear.FindStringSubmatch(title); m != nil {
		return m[1]
	}
	return ""
}

// links are the title's Linear issue and the other links of the
// description and report. Bare IDs elsewhere are left out: the board's own
// task keys (WEB-2) look like Linear IDs.
func (r Reader) links(title, description, report string) []deck.Link {
	var links []deck.Link
	if id := LinearID(title); id != "" {
		for _, l := range tasks.Scrape(id, r.LinearWorkspace) {
			links = deck.AppendLink(links, l)
		}
	}
	// Scraped without a workspace, a bare ID has no URL: only full
	// Linear URLs count.
	for _, l := range tasks.ScrapeAll([]string{description, report}, "") {
		if l.Kind == deck.LinkLinear && l.URL == "" {
			continue
		}
		links = deck.AppendLink(links, l)
	}
	return links
}

// logOf is the task's short timeline: what the board recorded of it.
func logOf(t deck.Thread, b *deck.BoardTask) []deck.LogEvent {
	var log []deck.LogEvent
	if !t.Changed.IsZero() {
		log = append(log, deck.LogEvent{At: t.Changed, Kind: deck.EventOther, Text: b.Phase.String(), Detail: b.Msg})
	}
	if !t.Created.IsZero() {
		log = append(log, deck.LogEvent{At: t.Created, Kind: deck.EventCreated, Text: "added"})
	}
	return log
}

var prNum = regexp.MustCompile(`/pull/(\d+)`)

func pullRequest(url, state string, merged bool) *deck.PullRequest {
	pr := &deck.PullRequest{URL: url, State: "OPEN"}
	if m := prNum.FindStringSubmatch(url); m != nil {
		pr.Number, _ = strconv.Atoi(m[1])
	}
	switch {
	case merged:
		pr.State = "MERGED"
	case state == "closed":
		pr.State = "CLOSED"
	case state == "failing":
		pr.FailingChecks = []string{"checks"}
	}
	return pr
}

// prString is details' pr: a URL, or an object with one.
func prString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var o struct {
		URL  string `json:"url"`
		Link string `json:"link"`
	}
	if json.Unmarshal(raw, &o) == nil {
		return first(o.URL, o.Link)
	}
	return ""
}

func research(raw json.RawMessage) bool {
	return truthy(raw)
}

// truthy is a JSON value that is set: true, a non-empty string or object,
// a non-zero number.
func truthy(raw json.RawMessage) bool {
	s := strings.TrimSpace(string(raw))
	return s != "" && s != "null" && s != "false" && s != "0" && s != `""`
}

func archived(raw json.RawMessage) bool { return truthy(raw) }

func splitID(id string) (repo, n string) {
	if i := strings.LastIndexByte(id, '/'); i >= 0 {
		return id[:i], id[i+1:]
	}
	return id, ""
}

func millis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

func readJSON(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

func first(s ...string) string {
	for _, x := range s {
		if x != "" {
			return x
		}
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// Use reports whether the deck for slug under the projects root reads the
// herdr-projects 1.x board: root holds one, and slug is not a 0.2 project
// folder (one with a PROJECT.md) beside it. An empty slug, or Slug, always
// means the board when there is one.
func Use(root, slug string) bool {
	if !Detect(root) {
		return false
	}
	if slug == "" || slug == Slug {
		return true
	}
	_, err := os.Stat(filepath.Join(root, slug, "PROJECT.md"))
	return err != nil
}

// RepoOf is the board repo slug names, for a deck opened on one repo's
// tasks: a repo folder under data/tasks; "" for the whole board.
func RepoOf(root, slug string) string {
	if slug == "" || slug == Slug || strings.ContainsAny(slug, `/\`) {
		return ""
	}
	if fi, err := os.Stat(filepath.Join(root, "data", "tasks", slug)); err == nil && fi.IsDir() {
		return slug
	}
	return ""
}
