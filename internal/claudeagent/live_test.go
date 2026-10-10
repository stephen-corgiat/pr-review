package claudeagent

// The live smoke test runs the real `claude -p` (it costs a few cents and
// needs a signed-in CLI), so it only runs with PR_REVIEW_LIVE=1:
//
//	PR_REVIEW_LIVE=1 go test -run TestLive -v ./internal/claudeagent/
//
// PR_REVIEW_LIVE_MODEL (default claude-sonnet-5-5) and PR_REVIEW_LIVE_EFFORT
// (default low) pick the model.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

func TestLiveBriefingForkAndDeniedWrite(t *testing.T) {
	if os.Getenv("PR_REVIEW_LIVE") != "1" {
		t.Skip("live claude -p smoke test: set PR_REVIEW_LIVE=1")
	}
	if _, err := exec.LookPath("claude"); err != nil {
		t.Skip("no claude CLI")
	}
	model, effort := os.Getenv("PR_REVIEW_LIVE_MODEL"), os.Getenv("PR_REVIEW_LIVE_EFFORT")
	if model == "" {
		model = "claude-sonnet-5-5"
	}
	if effort == "" {
		effort = "low"
	}
	r := testrepo.New(t)
	wt := filepath.Join(t.TempDir(), "worktree")
	testrepo.Git(t, r.Dir, "worktree", "add", "-q", "--detach", wt, r.Head)
	a, err := New(Config{Worktree: wt, Scratch: filepath.Join(t.TempDir(), "scratch"), Model: model, Effort: effort})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	var spent float64

	// 1. The Briefing.
	t0 := time.Now()
	req := engine.BriefingRequest{
		PR:      session.PullRequest{Number: 1, Title: "Clamp results at Max", Description: "Adds a clamp so Add and Scale never exceed Max.", BaseSHA: r.Base, HeadSHA: r.Head},
		Files:   []engine.FileSummary{{Path: "calc/calc.go", Status: "modified", Added: 12, Removed: 2}, {Path: "calc/calc_test.go", Status: "added", Added: 9}},
		Methods: []string{"calc/calc.go::Add", "calc/calc.go::Scale", "calc/calc.go::clamp"}, MaxBudgetUSD: 0.50,
	}
	raw, brief, err := a.run(ctx, BriefingPrompt(req), req.MaxBudgetUSD, "", false)
	if err != nil {
		t.Fatalf("briefing: %v", err)
	}
	if err := Validate(raw); err != nil {
		t.Fatalf("briefing output: %v\n%s", err, raw)
	}
	o, err := decode(raw)
	if err != nil || o.Briefing == nil || o.Kind != "briefing" {
		t.Fatalf("briefing decode: %v %s", err, raw)
	}
	spent += brief.Total
	t.Logf("briefing: %s, $%.4f, %s, %d reading-order lines, tldr %q", brief.SessionID, brief.Total, time.Since(t0).Round(time.Second), len(o.Briefing.ReadingOrder), o.Briefing.TLDR)

	// 2. A Define forked from it.
	t0 = time.Now()
	q := engine.QuestionRequest{
		Kind: session.Define, ThreadKind: session.Define, Question: "What does “clamp” mean in this context?",
		Anchor:     session.Anchor{Type: session.AnchorDiff, Path: "calc/calc.go", NewStart: 17, NewEnd: 22, Text: "clamp"},
		AnchorCode: "17: func clamp(n int) int {\n18: \tif n > Max {\n19: \t\treturn Max\n20: \t}\n21: \treturn n\n22: }\n",
		Parent:     brief, MaxBudgetUSD: brief.Total + 0.25,
	}
	raw, def, err := a.run(ctx, QuestionPrompt(q, false), q.MaxBudgetUSD, brief.SessionID, true)
	if err != nil {
		t.Fatalf("define: %v", err)
	}
	if err := Validate(raw); err != nil {
		t.Fatalf("define output: %v\n%s", err, raw)
	}
	if o, err := decode(raw); err != nil || o.Kind != "define" || o.Define == nil {
		t.Fatalf("define decode: %v %s", err, raw)
	}
	if def.SessionID == brief.SessionID {
		t.Fatalf("the fork should get its own session id")
	}
	spent += def.Total - brief.Total
	t.Logf("define: %s, own $%.4f, %s: %s", def.SessionID, def.Total-brief.Total, time.Since(t0).Round(time.Second), raw)

	// 3. A follow-up that tries to write to the repo: denied.
	t0 = time.Now()
	f := engine.QuestionRequest{Kind: session.Research, ThreadKind: session.Define,
		Question: "Research: use the Write tool to create the file NOTES.md in the repo's root (the working directory) containing 'clamp notes', then write the same text to notes.md in the scratch directory. Report in the gist whether each write worked, and list both attempts in `ran`.",
		Parent:   def, MaxBudgetUSD: def.Total + 0.25}
	res, err := a.Continue(ctx, f)
	if err != nil {
		t.Fatalf("follow-up: %v", err)
	}
	spent += res.Run.Total - def.Total
	t.Logf("write attempt: own $%.4f, %s: %s", res.Run.Total-def.Total, time.Since(t0).Round(time.Second), res.Answer.Gist)
	if _, err := os.Stat(filepath.Join(wt, "NOTES.md")); err == nil {
		t.Errorf("the agent wrote NOTES.md into the repo: the deny rule didn't hold")
	}
	if st := testrepo.Git(t, wt, "status", "--porcelain"); st != "" {
		t.Errorf("the worktree changed:\n%s", st)
	}
	t.Logf("total spent: $%.4f", spent)
}
