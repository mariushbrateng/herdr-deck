package fake

import (
	"fmt"
	"time"

	"github.com/FredricW/herdr-deck/internal/deck"
)

// PRs is a sample of the GitHub views' pull requests: the user's own in
// two repos, in every state the list colours, and two to review.
func PRs(now time.Time) *deck.MyPRs {
	ago := func(d time.Duration) time.Time { return now.Add(-d) }
	pr := func(repo string, n int, title, author, review, ci, mergeable, mergeState string, updated time.Duration) deck.MyPR {
		url := fmt.Sprintf("https://github.com/%s/pull/%d", repo, n)
		return deck.MyPR{
			Repo: repo, Title: title, Author: author, Updated: ago(updated), CI: ci,
			PR: deck.PullRequest{
				URL: url, Number: n, State: "OPEN", Review: review, CheckedAt: now, Live: true,
				Mergeable: mergeable, MergeState: mergeState,
			},
		}
	}
	const web, deploy = "acme/webshop", "acme/deploy"
	ready := pr(web, 2412, "Restrict org admin to own org in stats (ENG-914)", "you", "APPROVED", "SUCCESS", "MERGEABLE", "CLEAN", 20*time.Minute)
	waiting := pr(web, 2420, "Guard på migrations (ENG-791)", "you", "REVIEW_REQUIRED", "PENDING", "MERGEABLE", "BLOCKED", time.Hour)
	failing := pr(web, 2398, "Stop losing chunks on reconnect", "you", "CHANGES_REQUESTED", "FAILURE", "MERGEABLE", "BLOCKED", 5*time.Hour)
	conflict := pr(web, 2377, "Seed data for local development", "you", "REVIEW_REQUIRED", "SUCCESS", "CONFLICTING", "DIRTY", 26*time.Hour)
	draft := pr(deploy, 88, "Deploy the worker to eu-north", "you", "", "", "MERGEABLE", "DRAFT", 3*time.Hour)
	draft.PR.Draft = true
	behind := pr(deploy, 85, "Pin the base image", "you", "APPROVED", "SUCCESS", "MERGEABLE", "BEHIND", 50*time.Hour)
	return &deck.MyPRs{
		Mine: []deck.MyPR{ready, waiting, draft, failing, conflict, behind},
		Review: []deck.MyPR{
			pr(web, 2431, "Search: rank exact matches first", "sam", "REVIEW_REQUIRED", "SUCCESS", "MERGEABLE", "BLOCKED", 40*time.Minute),
			pr(deploy, 90, "Rotate the staging certificates", "ana", "REVIEW_REQUIRED", "PENDING", "UNKNOWN", "UNKNOWN", 2*time.Hour),
		},
		Repos: []string{web, deploy},
		Read:  now,
	}
}
