package deck

import "testing"

func TestTitleIssueID(t *testing.T) {
	for title, want := range map[string]string{
		"ENG-1656 Save the launch flag": "ENG-1656",
		"ENG-1656: Save":                "ENG-1656",
		"[ENG-12] Fix":                  "ENG-12",
		"  INF-112 Slett test-brukere":  "INF-112",
		"ENG-16560 is another issue":    "ENG-16560",
		"Fix ENG-1656 later":            "",
		"herdr-deck for herdr-projects": "",
		"eng-1656 lower case":           "",
		"ENG-1656x":                     "",
	} {
		if got := TitleIssueID(title); got != want {
			t.Errorf("TitleIssueID(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestPriorityRank(t *testing.T) {
	if !(PriorityRank(1) < PriorityRank(2) && PriorityRank(4) < PriorityRank(0)) {
		t.Error("none must sort after Low")
	}
	if PriorityName(0) != "No priority" || PriorityName(1) != "Urgent" {
		t.Error("names")
	}
}
