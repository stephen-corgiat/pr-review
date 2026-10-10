package engine_test

// New heads: polling (with an injected clock), Update's Anchor mapping and
// Stale marking, re-asking, "Since last review". The repo has base, head A
// and head B (testrepo.Push); the Review Session is opened through app.PR,
// so it has its own worktree.

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/paths"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

// clock is a settable time for the engine's Now.
type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }
func newClock() *clock               { return &clock{t: time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC)} }

func TestPollingPausesWhenIdle(t *testing.T) {
	r := testrepo.New(t)
	c := newClock()
	e, _, err := engine.Open(engine.Config{
		Repo:   gitsrc.OpenRepo(r.Dir, r.Base, r.Head),
		Opened: session.Opened{PR: session.PullRequest{Number: 7, OwnerRepo: "acme/calc", State: "open", HeadSHA: r.Head}, Checkout: r.Dir, Worktree: r.Dir},
		Forge:  forge.NewFake("bo"),
		Now:    c.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	if !e.HeadDue() {
		t.Fatalf("checked at once when it opens")
	}
	e.HeadChecker()
	for _, step := range []struct {
		d    time.Duration
		due  bool
		what string
	}{{59 * time.Second, false, "59 s"}, {time.Second, true, "60 s"}} {
		c.add(step.d)
		e.NoteInput()
		if e.HeadDue() != step.due {
			t.Fatalf("after %s active: due = %v", step.what, !step.due)
		}
	}
	e.HeadChecker() // checked at 60 s
	// No input for 10 minutes: paused, however long since the last check.
	c.add(10 * time.Minute)
	if e.HeadDue() || !e.HeadPaused() {
		t.Fatalf("idle 10 min: checks pause")
	}
	c.add(time.Hour)
	if e.HeadDue() {
		t.Fatalf("still idle: still paused")
	}
	// The first input after the pause checks at once; the next doesn't.
	if !e.NoteInput() {
		t.Fatalf("the first input after a pause checks now")
	}
	e.HeadChecker()
	c.add(time.Second)
	if e.NoteInput() || e.HeadDue() {
		t.Fatalf("active again: back to every 60 s")
	}
	// Regaining focus checks at once, paused or not.
	if !e.NoteFocus() {
		t.Fatalf("focus checks now")
	}
	// Nothing to watch without a Forge.
	e2, _ := openEngine(t, nil, nil)
	if e2.HeadDue() || e2.NoteFocus() || e2.HeadChecker() != nil {
		t.Fatalf("no Forge: no polling")
	}
}

// citingAgent answers with a Citation on the Anchor's lines, plus any
// "cite path:a-b" in the Question.
type citingAgent struct{ briefings int }

var citeRE = regexp.MustCompile(`cite (\S+):(\d+)-(\d+)`)

func (a *citingAgent) Briefing(_ context.Context, req engine.BriefingRequest) (engine.BriefingResult, error) {
	a.briefings++
	return engine.BriefingResult{Notes: session.BriefingNotes{TLDR: fmt.Sprintf("Briefing %d at %.7s", a.briefings, req.PR.HeadSHA)}}, nil
}
func (a *citingAgent) Ask(_ context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	ab := session.AnswerBody{Kind: req.Kind, Gist: "At " + req.Head[:7] + ": " + req.Question}
	if req.Anchor.Type == session.AnchorDiff && req.Anchor.NewStart > 0 {
		ab.Citations = append(ab.Citations, session.Citation{Path: req.Anchor.Path, Line: req.Anchor.NewStart, EndLine: req.Anchor.NewEnd})
	}
	if m := citeRE.FindStringSubmatch(req.Question); m != nil {
		l, _ := strconv.Atoi(m[2])
		el, _ := strconv.Atoi(m[3])
		ab.Citations = append(ab.Citations, session.Citation{Path: m[1], Line: l, EndLine: el})
	}
	return engine.AnswerResult{Answer: ab, Run: session.RunInfo{SessionID: "s-" + req.ThreadID}}, nil
}
func (a *citingAgent) Continue(ctx context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	return a.Ask(ctx, req)
}
func (a *citingAgent) Refine(context.Context, engine.RefineRequest) (engine.RevisionResult, error) {
	return engine.RevisionResult{}, fmt.Errorf("no")
}

type updateSetup struct {
	r     *testrepo.Repo
	f     *forge.Fake
	e     *engine.Engine
	agent *citingAgent
	o     app.PR
	b     string // head B
}

// updated opens #7 at head A with Threads and Feedback on lines that B
// keeps, moves, edits and deletes, then pushes B.
func newUpdate(t *testing.T) *updateSetup {
	t.Helper()
	r := testrepo.New(t)
	f := forge.NewFake("ana", forge.PR{Number: 7, OwnerRepo: "acme/calc", Title: "Clamp results", Author: "ana",
		HeadRef: "feature", BaseRef: "main", HeadSHA: r.Head, BaseSHA: r.Base, State: "open"})
	ag := &citingAgent{}
	o := app.PR{Checkout: r.Dir, Number: 7, Forge: f, DataDir: t.TempDir(),
		Agent: func(paths.Session, session.Opened) (engine.Agent, error) { return ag, nil }}
	e, _, _, err := o.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.Close() })
	e.Start()
	st := e.State()
	on := func(path string, from, to int) session.Anchor { return st.AnchorOnLines(path, "new", from, to, "") }
	do(t, e, engine.Ask{Anchor: on("calc/calc.go", 4, 5), Kind: session.Define})                                           // T1 Add: kept
	do(t, e, engine.Ask{Anchor: on("calc/calc.go", 8, 8), Kind: session.Define})                                           // T2 Scale's comment: edited
	do(t, e, engine.Ask{Anchor: on("calc/calc.go", 16, 16), Kind: session.Define, Text: "clamp? cite calc/calc.go:16-21"}) // T3 moves; its Citation changed
	do(t, e, engine.Ask{Anchor: on("calc/calc_test.go", 5, 7), Kind: session.Define})                                      // T4 a deleted file
	do(t, e, engine.Ask{Anchor: on("calc/calc.go", 14, 14), Kind: session.Define})                                         // T5 const Max: kept
	e.Pump(2 * time.Second)
	feedbackOn(t, e, on("calc/calc.go", 14, 14), "Why 1000?", false)                                // F1 kept
	feedbackOn(t, e, st.AnchorOnLines("calc/calc.go", "old", 5, 5, ""), "This was simpler.", false) // F2 the base's removed line
	feedbackOn(t, e, on("calc/calc.go", 8, 9), "Say Min too.", false)                               // F3 edited
	b := r.Push(t)
	f.SetHead(7, b)
	return &updateSetup{r: r, f: f, e: e, agent: ag, o: o, b: b}
}

func TestUpdateMapsAnchorsAndMarksStale(t *testing.T) {
	u := newUpdate(t)
	e, st := u.e, u.e.State()
	a := st.PR.HeadSHA
	wt := st.WorktreePath
	evs, sum, err := e.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if types(evs) != "head_moved" && types(evs)[:10] != "head_moved" {
		t.Fatalf("events: %s", types(evs))
	}
	if st.PR.HeadSHA != u.b || st.WorktreePath != wt || testrepo.Git(t, wt, "rev-parse", "HEAD") != u.b {
		t.Fatalf("the same worktree, checked out at B")
	}
	if st.PrevHead != a || st.UpdateCommits != 1 {
		t.Fatalf("prev head %s commits %d", st.PrevHead, st.UpdateCommits)
	}
	th := func(id string) *session.Thread { return st.ThreadByID(id) }
	lines := func(an session.Anchor) string { return fmt.Sprintf("%d-%d", an.NewStart, an.NewEnd) }
	check := func(what string, an session.Anchor, wantLines string, wantStale bool) {
		t.Helper()
		if an.Stale != wantStale {
			t.Errorf("%s: Stale = %v", what, an.Stale)
		}
		if wantLines != "" && lines(an) != wantLines {
			t.Errorf("%s: lines %s, want %s", what, lines(an), wantLines)
		}
		if an.Type == session.AnchorDiff {
			fd := st.Files[an.Path]
			if l := fd.All[an.RowStart]; an.NewStart > 0 && l.NewNo != an.NewStart {
				t.Errorf("%s: row %d holds new line %d, not %d", what, an.RowStart, l.NewNo, an.NewStart)
			}
		}
	}
	check("T1 (Add, kept)", th("T1").Anchor, "4-5", false)
	check("T2 (Scale's comment, edited)", th("T2").Anchor, "8-8", true)
	check("T3 (func clamp, moved)", th("T3").Anchor, "19-19", false)
	check("T5 (const Max, kept)", th("T5").Anchor, "14-14", false)
	if a4 := th("T4").Anchor; a4.Type != session.AnchorPR || !a4.Stale || a4.Path != "calc/calc_test.go" || a4.Text == "" {
		t.Errorf("T4's file is gone: Stale onto the whole PR, got %+v", a4)
	}
	if a2 := th("T2").Anchor; a2.StaleFrom != a || a2.Text != "// Scale multiplies n by k." {
		t.Errorf("a Stale Anchor keeps the text it was anchored to: %+v", a2)
	}
	fb := func(id string) session.Anchor { return st.FeedbackByID(id).Anchor }
	check("F1 (kept)", fb("F1"), "14-14", false)
	check("F3 (edited)", fb("F3"), "", true)
	if f2 := fb("F2"); f2.Stale || f2.OldStart != 5 || st.Files["calc/calc.go"].All[f2.RowStart].Text != "\treturn a + b" {
		t.Errorf("F2 on the base's removed line stays: %+v", f2)
	}
	// Answers: Stale when the Anchor is, or a Citation's range changed.
	for id, want := range map[string]bool{"T1": false, "T2": true, "T3": true, "T4": true, "T5": false} {
		if got := th(id).Stale(); got != want {
			t.Errorf("%s's Answer Stale = %v, want %v", id, got, want)
		}
	}
	if sum.Stale != 3 || sum.StaleAnswers != 3 {
		t.Errorf("summary %+v", sum)
	}
	// The Briefing: the old one stays, Stale, until the new one lands.
	if b := st.Briefing; !b.Stale || b.StaleFrom != a || b.TLDR == "" {
		t.Fatalf("Stale Briefing: %+v", b)
	}
	e.Pump(2 * time.Second)
	if b := st.Briefing; b.Stale || u.agent.briefings != 2 || b.TLDR != "Briefing 2 at "+u.b[:7] {
		t.Fatalf("regenerated: stale=%v briefings=%d tldr=%q", b.Stale, u.agent.briefings, b.TLDR)
	}
	// Nothing was re-asked by itself.
	for _, id := range []string{"T2", "T3", "T4"} {
		if n := len(th(id).Entries); n != 2 {
			t.Errorf("%s has %d entries: Stale Questions aren't re-asked automatically", id, n)
		}
	}

	// Replaying the log gives the same state, at B.
	e.Close()
	e2, _, _, err := u.o.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer e2.Close()
	s2 := e2.State()
	if s2.PR.HeadSHA != u.b || s2.PrevHead != a || s2.Files["calc/doc.go"] == nil || s2.Files["calc/calc_test.go"] != nil {
		t.Fatalf("replayed at %s (prev %s), files %v", s2.PR.HeadSHA, s2.PrevHead, s2.FileGitOrder)
	}
	for _, id := range []string{"T1", "T2", "T3", "T4", "T5"} {
		if x, y := s2.ThreadByID(id), th(id); x.Anchor != y.Anchor || x.Stale() != y.Stale() {
			t.Errorf("%s replays differently: %+v vs %+v", id, x.Anchor, y.Anchor)
		}
	}
	if s2.Briefing.Stale || s2.Briefing.TLDR != st.Briefing.TLDR || len(s2.Since) != 3 {
		t.Errorf("replayed Briefing %+v, since %d files", s2.Briefing, len(s2.Since))
	}
}

// A Stale Question is re-asked with one command, as a follow-up in its
// Thread, at the new head.
func TestReaskAStaleQuestion(t *testing.T) {
	u := newUpdate(t)
	e := u.e
	if _, err := e.Do(engine.Reask{ThreadID: "T1"}); err == nil {
		t.Fatalf("T1 isn't Stale")
	}
	if _, _, err := e.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	e.Pump(2 * time.Second) // the new Briefing
	evs := do(t, e, engine.Reask{ThreadID: "T2"})
	p := evs[0].Data.(session.FollowUpAsked)
	if !p.Reask || p.Text != "Asked again at the new head "+u.b[:7]+": "+session.Define.DefaultQuestion("// Scale multiplies n by k.") {
		t.Fatalf("re-ask: %+v", p)
	}
	e.Pump(2 * time.Second)
	t2 := e.State().ThreadByID("T2")
	if t2.Stale() || len(t2.Entries) != 4 || t2.Entries[1].Stale != true {
		t.Fatalf("T2 after re-asking: stale=%v entries=%d", t2.Stale(), len(t2.Entries))
	}
	if last := t2.Entries[3]; last.A == nil || last.A.Gist[:10] != "At "+u.b[:7] {
		t.Fatalf("answered at the new head: %+v", last.A)
	}
	if _, err := e.Do(engine.Reask{ThreadID: "T2"}); err == nil {
		t.Fatalf("T2 is fresh again")
	}
}

// The check finds new commits and the local branch's unpushed ones.
func TestHeadCheck(t *testing.T) {
	u := newUpdate(t)
	e := u.e
	u.r.Unpushed(t, "feature", 2)
	c, err := e.HeadChecker()(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if c.Head != u.b || c.Commits != 1 || c.Unpushed != 2 {
		t.Fatalf("check: %+v", c)
	}
	if !e.SawHead(c) || !e.State().NewHead() || e.SawHead(c) {
		t.Fatalf("SawHead records it once")
	}
	if _, _, err := e.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.Update(context.Background()); err == nil {
		t.Fatalf("a second Update finds no new head")
	}
}

// Update waits while an agent run reads the worktree, and holds queued
// work back while it runs.
func TestUpdateWaitsForRunningAgents(t *testing.T) {
	r := testrepo.New(t)
	f := forge.NewFake("ana", forge.PR{Number: 7, OwnerRepo: "acme/calc", HeadRef: "feature", BaseRef: "main", HeadSHA: r.Head, BaseSHA: r.Base, State: "open"})
	s := newStub()
	o := app.PR{Checkout: r.Dir, Number: 7, Forge: f, DataDir: t.TempDir(),
		Agent: func(paths.Session, session.Opened) (engine.Agent, error) { return s, nil }}
	e, _, _, err := o.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	e.Start()
	b := s.next(t) // the Briefing is running
	f.SetHead(7, r.Push(t))
	if _, err := e.BeginUpdate(); err == nil || !strings.Contains(err.Error(), "1 agent run still running") {
		t.Fatalf("while the Briefing runs: %v", err)
	}
	b.reply <- engine.BriefingResult{Notes: session.BriefingNotes{TLDR: "x"}}
	handle(t, e)
	run, err := e.BeginUpdate()
	if err != nil {
		t.Fatal(err)
	}
	// While it fetches and checks out, a new Question waits.
	do(t, e, engine.Ask{Anchor: fileAnchor("calc/calc.go"), Kind: session.Define})
	s.none(t)
	p, err := run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.ApplyUpdate(p); err != nil {
		t.Fatal(err)
	}
	if c := s.next(t); c.What != "briefing" {
		t.Fatalf("after Update the Briefing runs again first, got %s", c.What)
	}
}

// "Since last review": the diff from the head the Reviewer updated from.
func TestSinceLastReview(t *testing.T) {
	u := newUpdate(t)
	st := u.e.State()
	if st.PrevHead != "" || st.Since != nil {
		t.Fatalf("nothing since before an Update")
	}
	if _, _, err := u.e.Update(context.Background()); err != nil {
		t.Fatal(err)
	}
	want := map[string]session.FileStatus{"calc/calc.go": session.FileModified, "calc/calc_test.go": session.FileDeleted, "calc/doc.go": session.FileAdded}
	if len(st.Since) != len(want) {
		t.Fatalf("since: %v", st.SinceOrder)
	}
	for p, s := range want {
		if st.Since[p] == nil || st.Since[p].Status != s {
			t.Errorf("%s: %+v", p, st.Since[p])
		}
	}
	calc := st.Since["calc/calc.go"]
	if calc.Added != 7 || calc.Removed != 1 {
		t.Errorf("calc.go since A: +%d −%d", calc.Added, calc.Removed)
	}
}
