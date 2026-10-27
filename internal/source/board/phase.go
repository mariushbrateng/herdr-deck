package board

import (
	"sort"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// PhaseOf is the deck's phase for a board stage and agent state. merged is
// set when the task's pull request (or the board's own merge) landed. The
// stages are herdr-projects' STAGES (crates/core/src/store/files.rs).
func PhaseOf(stage, state string, merged bool) deck.Phase {
	switch stage {
	case "done":
		if merged {
			return deck.PhaseMerged
		}
		return deck.PhaseDone
	case "failed", "stopped":
		return deck.PhaseFailed
	case "plan":
		return deck.PhasePlan
	case "asks", "nosummary", "reviewneeds", "pick":
		// pick: more than one attempt passed, and the user chooses.
		return deck.PhaseAsks
	case "approval":
		return deck.PhaseApprove
	case "merging":
		return deck.PhaseMerging
	case "reviewing":
		return deck.PhaseReview
	case "checking":
		return deck.PhaseChecking
	case "progress", "starting", "setup", "pushing", "attempts", "tried":
		// The board or the agent is under way: starting it, setting up
		// its worktree, pushing its branch, or several attempts at once
		// (tried is one of them that passed).
		return deck.PhaseWorking
	case "queued", "after":
		return deck.PhaseQueued
	case "todo", "":
		if state == "blocked" {
			return deck.PhaseAsks
		}
		return deck.PhaseTodo
	}
	// working, and any stage a newer board adds: the agent state says
	// what it waits for.
	switch state {
	case "blocked":
		return deck.PhaseAsks
	case "error":
		return deck.PhaseFailed
	case "done":
		return deck.PhaseReview
	}
	return deck.PhaseWorking
}

// phaseRank orders phases within a priority: what waits on the user first,
// then work under way, then work not started, then finished work.
func phaseRank(p deck.Phase) int {
	switch p {
	case deck.PhaseAsks:
		return 0
	case deck.PhasePlan:
		return 1
	case deck.PhaseFailed:
		return 2
	case deck.PhaseApprove:
		return 3
	case deck.PhaseReview, deck.PhaseChecking:
		return 4
	case deck.PhaseMerging:
		return 5
	case deck.PhaseWorking:
		return 6
	case deck.PhaseQueued:
		return 7
	case deck.PhaseTodo:
		return 8
	case deck.PhaseDone:
		return 9
	}
	return 10
}

// prioRank orders priorities: 1 first, 4 last, and none after 4.
func prioRank(p int) int {
	if p < 1 || p > MaxPriority {
		return MaxPriority + 1
	}
	return p
}

// Sort orders board tasks by priority first, then by phase, then newest
// first. Threads without board data keep their place after them.
func Sort(threads []deck.Thread) {
	sort.SliceStable(threads, func(i, j int) bool {
		a, b := threads[i].Board, threads[j].Board
		if a == nil || b == nil {
			return a != nil && b == nil
		}
		if pa, pb := prioRank(a.Priority), prioRank(b.Priority); pa != pb {
			return pa < pb
		}
		if ra, rb := phaseRank(a.Phase), phaseRank(b.Phase); ra != rb {
			return ra < rb
		}
		return threads[i].Created.After(threads[j].Created)
	})
}

// ListDone is the list finished tasks go to, whatever their priority: the
// deck's own name for finished threads, which starts folded.
const ListDone = "Resolved"

// Lists groups sorted board tasks into the deck's lists: one per priority
// in use, highest first, then the tasks without one, then ListDone.
func Lists(threads []deck.Thread) []deck.TaskList {
	var lists []deck.TaskList
	index := map[string]int{}
	add := func(name string, t deck.Thread) {
		i, ok := index[name]
		if !ok {
			i = len(lists)
			index[name] = i
			lists = append(lists, deck.TaskList{Name: name})
		}
		lists[i].Tasks = append(lists[i].Tasks, deck.Task{
			Title:   t.Title,
			Done:    t.Status == deck.StatusDone,
			Threads: []string{t.ID},
		})
	}
	var done []deck.Thread
	for _, t := range threads {
		if t.Board == nil {
			continue
		}
		if t.Status == deck.StatusDone {
			done = append(done, t)
			continue
		}
		add(PriorityList(t.Board.Priority), t)
	}
	for _, t := range done {
		add(ListDone, t)
	}
	return lists
}
