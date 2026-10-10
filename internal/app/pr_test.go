package app_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/fixtureagent"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/paths"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

func fakePR(r *testrepo.Repo, state string) forge.PR {
	return forge.PR{Number: 7, OwnerRepo: "acme/calc", Title: "Clamp results", Body: "Clamps at Max.",
		Author: "ana", HeadRef: "feature", BaseRef: "main", HeadSHA: r.Head, BaseSHA: r.Base, State: state}
}

func opener(r *testrepo.Repo, f forge.Forge, dataDir string, number int) app.PR {
	return app.PR{
		Checkout: r.Dir, Number: number, Forge: f, DataDir: dataDir,
		Agent: func(paths.Session, session.Opened) (engine.Agent, error) {
			return fixtureagent.New(nil).NoDelay(), nil
		},
	}
}

// checkoutState is what must never change in the Reviewer's checkout.
func checkoutState(t *testing.T, dir string) string {
	return testrepo.Git(t, dir, "rev-parse", "--abbrev-ref", "HEAD") + " " +
		testrepo.Git(t, dir, "rev-parse", "HEAD") + " [" + testrepo.Git(t, dir, "status", "--porcelain") + "]"
}

func TestOpenTheCurrentBranchsPR(t *testing.T) {
	r := testrepo.New(t)
	testrepo.Git(t, r.Dir, "checkout", "-q", "feature")
	os.WriteFile(filepath.Join(r.Dir, "scratch.txt"), []byte("the Reviewer's uncommitted work"), 0o644)
	before := checkoutState(t, r.Dir)
	f := forge.NewFake("ana", fakePR(r, "open"))
	data := t.TempDir()

	eng, res, _, err := opener(r, f, data, 0).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if !res.New || f.Calls[0] != "PRForBranch feature" {
		t.Fatalf("res %+v, calls %v", res, f.Calls)
	}
	st := eng.State()
	if st.PR.Number != 7 || len(st.Files) != 2 {
		t.Fatalf("PR %+v files %v", st.PR, st.FileGitOrder)
	}
	wt := paths.SessionDir(data, "acme/calc", 7).Worktree()
	if st.WorktreePath != wt {
		t.Fatalf("worktree %s, want %s", st.WorktreePath, wt)
	}
	if got := testrepo.Git(t, wt, "rev-parse", "HEAD"); got != r.Head {
		t.Fatalf("worktree at %s, want the PR head", got)
	}
	if b := testrepo.Git(t, wt, "rev-parse", "--abbrev-ref", "HEAD"); b != "HEAD" {
		t.Fatalf("the worktree should be detached, is on %s", b)
	}
	if after := checkoutState(t, r.Dir); after != before {
		t.Fatalf("the Reviewer's checkout changed:\n%s\n%s", before, after)
	}
	if _, err := os.Stat(paths.SessionDir(data, "acme/calc", 7).Scratch()); err != nil {
		t.Fatalf("no scratch dir: %v", err)
	}
}

func TestReopenKeepsTheWorktreePathAndTheLog(t *testing.T) {
	r := testrepo.New(t)
	f := forge.NewFake("ana", fakePR(r, "open"))
	data := t.TempDir()
	eng, _, _, err := opener(r, f, data, 7).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	wt := eng.State().WorktreePath
	eng.Do(engine.AddFeedback{Anchor: session.Anchor{Type: session.AnchorFile, Path: "README.md"}, Body: "hi"})
	eng.Close()

	eng2, res, _, err := opener(r, f, data, 7).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer eng2.Close()
	if res.New || eng2.State().WorktreePath != wt || len(eng2.State().Feedback) != 1 {
		t.Fatalf("reopen: new=%v wt=%s feedback=%d", res.New, eng2.State().WorktreePath, len(eng2.State().Feedback))
	}
}

func TestStackedPRDiffsAgainstItsBase(t *testing.T) {
	r := testrepo.New(t)
	head2 := r.Stack(t)
	r.AdvanceMain(t)
	pr := forge.PR{Number: 8, OwnerRepo: "acme/calc", Title: "Docs", HeadRef: "feature2", BaseRef: "feature",
		HeadSHA: head2, BaseSHA: r.Head, State: "open"}
	f := forge.NewFake("ana", pr)
	eng, _, _, err := opener(r, f, t.TempDir(), 8).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if files := eng.State().FileGitOrder; len(files) != 1 || files[0] != "README.md" {
		t.Fatalf("a stacked PR shows only its own changes, got %v", files)
	}
	if eng.State().PR.BaseSHA != r.Head {
		t.Fatalf("base = %s", eng.State().PR.BaseSHA)
	}
}

func TestMergeBaseWhenTheBaseBranchMovedOn(t *testing.T) {
	r := testrepo.New(t)
	tip := r.AdvanceMain(t)
	pr := fakePR(r, "open")
	pr.BaseSHA = tip // gh reports the base branch's tip
	eng, _, _, err := opener(r, forge.NewFake("ana", pr), t.TempDir(), 7).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if files := eng.State().FileGitOrder; len(files) != 2 || eng.State().Files["NOTES.md"] != nil {
		t.Fatalf("main's later commit must not show: %v", files)
	}
}

func TestANewSessionOnAMergedPRWorksButCantSend(t *testing.T) {
	r := testrepo.New(t)
	eng, _, _, err := opener(r, forge.NewFake("ana", fakePR(r, "merged")), t.TempDir(), 7).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if !eng.HasAgent() {
		t.Fatalf("a new Review Session on a merged PR has its agent")
	}
	if _, err := eng.Do(engine.Ask{Anchor: session.Anchor{Type: session.AnchorFile, Path: "README.md"}, Kind: session.Summarize}); err != nil {
		t.Fatalf("asking: %v", err)
	}
	eng.Do(engine.AddFeedback{Anchor: session.Anchor{Type: session.AnchorFile, Path: "README.md"}, Body: "ok"})
	eng.Do(engine.ToggleReady{ID: "F1"})
	for _, c := range []engine.Command{engine.HandOff{IDs: []string{"F1"}}, engine.PostReview{IDs: []string{"F1"}, Event: "Comment"}} {
		if _, err := eng.Do(c); err == nil || !strings.Contains(err.Error(), "merged") {
			t.Fatalf("%T on a merged PR: %v", c, err)
		}
	}
}

func TestASessionWhosePRFinishedGoesReadOnly(t *testing.T) {
	r := testrepo.New(t)
	f := forge.NewFake("ana", fakePR(r, "open"))
	data := t.TempDir()
	eng, _, _, err := opener(r, f, data, 7).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	eng.Do(engine.AddFeedback{Anchor: session.Anchor{Type: session.AnchorFile, Path: "README.md"}, Body: "kept"})
	wt := eng.State().WorktreePath
	eng.Close()

	f.SetState(7, "merged")
	eng2, res, _, err := opener(r, f, data, 7).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer eng2.Close()
	st := eng2.State()
	if res.Finished != "merged" || !st.ReadOnly() || eng2.HasAgent() {
		t.Fatalf("finished=%q readOnly=%v agent=%v", res.Finished, st.ReadOnly(), eng2.HasAgent())
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("the worktree should be removed: %v", err)
	}
	if list := testrepo.Git(t, r.Dir, "worktree", "list"); strings.Contains(list, wt) {
		t.Fatalf("the worktree should be pruned:\n%s", list)
	}
	if len(st.Files) != 2 || len(st.Feedback) != 1 {
		t.Fatalf("still readable: files %d feedback %d", len(st.Files), len(st.Feedback))
	}
	if _, err := eng2.Do(engine.Ask{Anchor: session.Anchor{Type: session.AnchorFile, Path: "README.md"}, Kind: session.Define}); err == nil {
		t.Fatalf("a read-only session has no agent")
	}
	// Reopening again stays read-only without another Finished event.
	eng2.Close()
	eng3, res3, _, err := opener(r, f, data, 7).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer eng3.Close()
	if !eng3.State().ReadOnly() || res3.Finished != "merged" {
		t.Fatalf("reopened: %+v", res3)
	}
}

func TestPruneRemovesFinishedSessionsWorktrees(t *testing.T) {
	r := testrepo.New(t)
	head2 := r.Stack(t)
	f := forge.NewFake("ana", fakePR(r, "open"),
		forge.PR{Number: 8, OwnerRepo: "acme/calc", HeadRef: "feature2", BaseRef: "feature", HeadSHA: head2, BaseSHA: r.Head, State: "open"})
	data := t.TempDir()
	for _, n := range []int{7, 8} {
		eng, _, _, err := opener(r, f, data, n).Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		eng.Close()
	}
	f.SetState(8, "closed")
	removed, err := app.Prune(context.Background(), data, func(string, string) forge.Forge { return f })
	if err != nil {
		t.Fatal(err)
	}
	if len(removed) != 1 || !strings.Contains(removed[0], "#8 (closed)") {
		t.Fatalf("removed %v", removed)
	}
	if _, err := os.Stat(paths.SessionDir(data, "acme/calc", 8).Worktree()); !os.IsNotExist(err) {
		t.Fatalf("#8's worktree should be gone")
	}
	if _, err := os.Stat(paths.SessionDir(data, "acme/calc", 7).Worktree()); err != nil {
		t.Fatalf("#7 is open: its worktree stays")
	}
	eng, res, _, err := opener(r, f, data, 8).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	if !eng.State().ReadOnly() || res.Finished != "closed" {
		t.Fatalf("a pruned session reopens read-only: %+v", res)
	}
}
