package main

import (
	"strings"
	"testing"
)

// The bundled /pull-review imports the Feedback and research, asks for a
// commit and push, mentions resolve as optional, and prescribes nothing
// else.
func TestBundledSkill(t *testing.T) {
	for _, want := range []string{"name: pull-review", "disable-model-invocation: true", "pr-review export", "## Research",
		"Commit and push", "pr-review resolve", "optional", "Follow the user's lead", "refused"} {
		if !strings.Contains(pullReviewSkill, want) {
			t.Errorf("the skill lacks %q", want)
		}
	}
	for _, bad := range []string{"go run", "Report the Outcomes", "one Outcome"} {
		if strings.Contains(pullReviewSkill, bad) {
			t.Errorf("the skill still says %q", bad)
		}
	}
}
