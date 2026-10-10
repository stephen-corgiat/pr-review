package engine_test

// Author detection, the review payload, Outcomes, done
// and reopen, polling, Update and "Since last review".

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

// openWithForge opens the calc repo's PR #7 (by author) with a fake Forge
// signed in as login.
func openWithForge(t *testing.T, r *testrepo.Repo, f *forge.Fake, author string) (*engine.Engine, []string) {
	t.Helper()
	e, notes, err := engine.Open(engine.Config{
		Repo:   gitsrc.OpenRepo(r.Dir, r.Base, r.Head),
		Opened: session.Opened{PR: session.PullRequest{Number: 7, Title: "Clamp results", OwnerRepo: "acme/calc", Author: author, State: "open", HeadRef: "feature", BaseRef: "main", BaseSHA: r.Base, HeadSHA: r.Head}, Worktree: r.Dir, Checkout: r.Dir},
		Agent:  fixtureAgent(nil),
		Forge:  f,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e, notes
}

func TestAuthorDetection(t *testing.T) {
	r := testrepo.New(t)
	for _, c := range []struct {
		login, author string
		want          bool
	}{{"ana", "ana", true}, {"Ana", "ana", true}, {"bo", "ana", false}} {
		e, _ := openWithForge(t, r, forge.NewFake(c.login), c.author)
		if got := e.State().ReviewerIsAuthor; got != c.want {
			t.Errorf("login %s, author %s: ReviewerIsAuthor = %v, want %v", c.login, c.author, got, c.want)
		}
		if e.State().ReviewerLogin != c.login {
			t.Errorf("ReviewerLogin = %q", e.State().ReviewerLogin)
		}
	}
	// gh not signed in: not the Author, and a note says why.
	f := forge.NewFake("ana")
	f.UserErr = errors.New("not logged in")
	e, notes := openWithForge(t, r, f, "ana")
	if e.State().ReviewerIsAuthor || len(notes) == 0 || !strings.Contains(notes[len(notes)-1], "not logged in") {
		t.Errorf("unknown user: author=%v notes=%v", e.State().ReviewerIsAuthor, notes)
	}
	// No Forge (fixture and -repo modes): nobody asks.
	e2, _ := openEngine(t, fixtureAgent(nil), nil)
	if e2.State().ReviewerIsAuthor {
		t.Errorf("no Forge: not the Author")
	}
}

// On your own PR GitHub allows only a Comment review.
func TestOwnPRAllowsOnlyComment(t *testing.T) {
	r := testrepo.New(t)
	e, _ := openWithForge(t, r, forge.NewFake("ana"), "ana")
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("README.md"), Body: "Typo in the title."})
	do(t, e, engine.ToggleReady{ID: "F1"})
	for _, ev := range []string{"Approve", "Request changes"} {
		if _, err := e.Do(engine.PostReview{IDs: []string{"F1"}, Event: ev}); err == nil || !strings.Contains(err.Error(), "only a Comment") {
			t.Errorf("%s on your own PR: err = %v", ev, err)
		}
	}
}

// feedbackOn adds Feedback and marks it ready (unless draft).
func feedbackOn(t *testing.T, e *engine.Engine, a session.Anchor, body string, ready bool) string {
	t.Helper()
	evs := do(t, e, engine.AddFeedback{Anchor: a, Body: body})
	id := evs[0].Data.(session.FeedbackCreated).FeedbackID
	if ready {
		do(t, e, engine.ToggleReady{ID: id})
	}
	return id
}

// The review payload: one review, submitted with its event; a line
// comment per ready item on diff lines (the head's side, or the base's for
// removed lines); everything else in the body under the summary; drafts
// never.
func TestReviewPayload(t *testing.T) {
	r := testrepo.New(t)
	f := forge.NewFake("bo")
	e, _ := openWithForge(t, r, f, "ana")
	st := e.State()
	f1 := feedbackOn(t, e, st.AnchorOnLines("calc/calc.go", "new", 4, 5, ""), "Say why it clamps.", true)
	f2 := feedbackOn(t, e, st.AnchorOnLines("calc/calc.go", "old", 5, 5, ""), "This was simpler.", true)
	f3 := feedbackOn(t, e, session.Anchor{Type: session.AnchorDescription, Text: "Clamps results"}, "Mention the limit.", true)
	f4 := feedbackOn(t, e, session.Anchor{Type: session.AnchorPR}, "Looks good overall.", true)
	feedbackOn(t, e, fileAnchor("README.md"), "A draft: never sent.", false)

	post, err := e.PrepareReview(engine.PostReview{IDs: []string{f1, f2, f3, f4, "F5"}, Event: "Request changes", Summary: "Two small things."})
	if err != nil {
		t.Fatal(err)
	}
	rv := post.Review
	if rv.Event != "REQUEST_CHANGES" || rv.CommitID != r.Head {
		t.Fatalf("event %q commit %q", rv.Event, rv.CommitID)
	}
	if len(post.IDs) != 4 || post.Lines() != 2 {
		t.Fatalf("ids %v, %d line comments", post.IDs, post.Lines())
	}
	want := []forge.ReviewComment{
		{Path: "calc/calc.go", Line: 5, Side: "RIGHT", StartLine: 4, StartSide: "RIGHT", Body: "Say why it clamps."},
		{Path: "calc/calc.go", Line: 5, Side: "LEFT", Body: "This was simpler."},
	}
	for i, w := range want {
		if rv.Comments[i] != w {
			t.Errorf("comment %d = %+v, want %+v", i, rv.Comments[i], w)
		}
	}
	for _, s := range []string{"Two small things.", "**On the PR description**", "> Clamps results", "Mention the limit.", "Looks good overall."} {
		if !strings.Contains(rv.Body, s) {
			t.Errorf("body lacks %q:\n%s", s, rv.Body)
		}
	}
	if strings.Contains(rv.Body, "draft") || strings.Index(rv.Body, "Two small things.") != 0 {
		t.Errorf("the summary leads and drafts stay out:\n%s", rv.Body)
	}

	// Posting sends it through the Forge and marks the items posted.
	evs := do(t, e, engine.PostReview{IDs: post.IDs, Event: "Request changes", Summary: "Two small things."})
	got := f.Posted()
	if len(got) != 1 || len(got[0].Comments) != 2 || got[0].Event != "REQUEST_CHANGES" {
		t.Fatalf("the fake Forge got %+v", got)
	}
	p := evs[0].Data.(session.ReviewPosted)
	if p.URL == "" || p.LineComments != 2 || p.Local {
		t.Fatalf("ReviewPosted %+v", p)
	}
	for _, id := range post.IDs {
		if s := e.State().FeedbackByID(id).Status; s != session.FeedbackPosted {
			t.Errorf("%s is %s", id, s)
		}
	}
	if s := e.State().FeedbackByID("F5").Status; s != session.FeedbackDraft {
		t.Errorf("the draft is %s", s)
	}
}

// A refused review leaves the items ready; unfilled Agent Notes never
// reach the Forge.
func TestReviewRefused(t *testing.T) {
	r := testrepo.New(t)
	f := forge.NewFake("bo")
	e, _ := openWithForge(t, r, f, "ana")
	id := feedbackOn(t, e, fileAnchor("calc/calc.go"), "Rename it.", true)
	f.PostErr = errors.New("GitHub refused the review: Unprocessable Entity")
	if _, err := e.Do(engine.PostReview{IDs: []string{id}, Event: "Comment"}); err == nil {
		t.Fatalf("the Forge's refusal is the command's error")
	}
	if s := e.State().FeedbackByID(id).Status; s != session.FeedbackReady {
		t.Fatalf("after a refusal %s is %s", id, s)
	}
	n := feedbackOn(t, e, fileAnchor("README.md"), "Explain <<the limit>>.", true)
	if _, err := e.Do(engine.PostReview{IDs: []string{id, n}, Event: "Comment"}); err == nil || !strings.Contains(err.Error(), "Agent Note") {
		t.Fatalf("notes block: %v", err)
	}
	if len(f.Posted()) != 0 {
		t.Fatalf("nothing reached GitHub")
	}
}

// PR Comments come from the Forge and sit at their lines at the Review
// Session's head; replies with their comment; the rest at the top.
func TestPRCommentsPlacement(t *testing.T) {
	r := testrepo.New(t)
	f := forge.NewFake("bo")
	f.Comment = []forge.Comment{
		{ID: "rc1", Kind: "review", Author: "carol", Body: "Why 1000?", CreatedAt: "2026-09-01T10:00:00Z", Path: "calc/calc.go", Line: 14, Side: "RIGHT", CommitID: r.Head, OriginalCommit: r.Head, OriginalLine: 14},
		{ID: "rc2", Kind: "review", Author: "ana", Body: "It's the product limit.", CreatedAt: "2026-09-01T11:00:00Z", Path: "calc/calc.go", Line: 14, Side: "RIGHT", CommitID: r.Head, InReplyTo: "rc1"},
		{ID: "rc3", Kind: "review", Author: "dan", Body: "Old nit.", CreatedAt: "2026-08-30T09:00:00Z", Path: "calc/calc.go", Line: 0, OriginalLine: 3, Side: "RIGHT", CommitID: "0ld", OriginalCommit: "0ld"},
		{ID: "rc4", Kind: "review", Author: "dan", Body: "On the base.", CreatedAt: "2026-08-30T09:30:00Z", Path: "calc/calc.go", Line: 5, Side: "LEFT", CommitID: "later", OriginalCommit: r.Head, OriginalLine: 5},
		{ID: "ic1", Kind: "issue", Author: "bot", Body: "Coverage 91%.", CreatedAt: "2026-09-02T08:00:00Z"},
	}
	e, _ := openWithForge(t, r, f, "ana")
	if err := e.FetchComments(context.Background()); err != nil {
		t.Fatal(err)
	}
	st := e.State()
	fd := st.Files["calc/calc.go"]
	row := func(id string) int { return st.CommentRow(st.CommentByID(id)) }
	if r1 := row("rc1"); r1 < 0 || fd.All[r1].NewNo != 14 {
		t.Fatalf("rc1 sits on new line 14, got row %d", r1)
	}
	if r4 := row("rc4"); r4 < 0 || fd.All[r4].OldNo != 5 || fd.All[r4].Kind != session.LineRemoved {
		t.Fatalf("rc4 sits on the base's line 5 (its original commit is our head), got row %d", r4)
	}
	if on := st.CommentsOnRow("calc/calc.go", row("rc1")); len(on) != 1 || on[0].ID != "rc1" {
		t.Fatalf("one conversation on line 14 (the reply goes with it): %v", on)
	}
	if rs := st.CommentReplies(st.CommentByID("rc1")); len(rs) != 1 || rs[0].ID != "rc2" {
		t.Fatalf("rc2 answers rc1: %v", rs)
	}
	if top := st.FileTopComments("calc/calc.go"); len(top) != 1 || top[0].ID != "rc3" {
		t.Fatalf("the outdated comment goes at the file's top: %v", top)
	}
	if g := st.GeneralComments(); len(g) != 1 || g[0].ID != "ic1" {
		t.Fatalf("general: %v", g)
	}
	// A Question can be anchored on one; the agent is told whose it is.
	evs := do(t, e, engine.Ask{Anchor: session.Anchor{Type: session.AnchorComment, Ref: "rc1", Text: "Why 1000?"}, Kind: session.Define})
	if a := evs[0].Data.(session.ThreadCreated).Anchor; a.Type != session.AnchorComment || a.Ref != "rc1" {
		t.Fatalf("anchor %+v", a)
	}
}

// handedOff opens the calc PR with F1 and F2 handed off to the agent.
func handedOff(t *testing.T) *engine.Engine {
	t.Helper()
	e, _ := openEngine(t, fixtureAgent(nil), nil)
	for _, b := range []string{"Rename Max.", "Test clamp."} {
		feedbackOn(t, e, fileAnchor("calc/calc.go"), b, true)
	}
	do(t, e, engine.HandOff{IDs: []string{"F1", "F2"}})
	if got := e.ToExport(false); len(got) != 2 {
		t.Fatalf("to export: %d", len(got))
	}
	do(t, e, engine.MarkHandedOff{IDs: []string{"F1", "F2", "F9"}})
	return e
}

// resolve records Outcomes with or without a commit; they don't change the
// status. Done and reopen are the Reviewer's, whatever the Outcome says.
func TestOutcomesDoneAndReopen(t *testing.T) {
	e := handedOff(t)
	st := e.State()
	do(t, e, engine.RecordOutcome{ID: "F1", Outcome: "declined", Note: "Max is the product's name for it"})
	do(t, e, engine.RecordOutcome{ID: "F2", Outcome: "addressed", Commit: "abc1234"})
	f1, f2 := st.FeedbackByID("F1"), st.FeedbackByID("F2")
	if o := f1.LastOutcome(); o == nil || o.Kind != "declined" || o.Commit != "" {
		t.Fatalf("F1: %+v", o)
	}
	if o := f2.LastOutcome(); o == nil || o.Commit != "abc1234" || o.Note != "" {
		t.Fatalf("F2: %+v", o)
	}
	// Done, whatever F1's Outcome says; done again is refused.
	do(t, e, engine.MarkDone{ID: "F1"})
	if f1.Status != session.FeedbackDone {
		t.Fatalf("F1 is %s", f1.Status)
	}
	if _, err := e.Do(engine.MarkDone{ID: "F1"}); err == nil {
		t.Fatalf("done twice")
	}
	// Reopened, though F2 says addressed: a draft again, editable.
	do(t, e, engine.Reopen{ID: "F2"})
	if f2.Status != session.FeedbackDraft {
		t.Fatalf("F2 is %s", f2.Status)
	}
	do(t, e, engine.EditFeedback{ID: "F2", Body: "Test clamp at exactly Max."})
	if len(f2.Outcomes) != 1 {
		t.Fatalf("reopening keeps the Outcome history")
	}
	// Done Feedback can be reopened too; a draft can't be marked done.
	do(t, e, engine.Reopen{ID: "F1"})
	if _, err := e.Do(engine.MarkDone{ID: "F1"}); err == nil {
		t.Fatalf("a draft can't be done")
	}
	if got := e.ToExport(true); len(got) != 0 {
		t.Fatalf("nothing queued or handed off now: %d", len(got))
	}
}
