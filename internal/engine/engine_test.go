package engine_test

// The engine's message API is the main test seam: send commands, check the
// events that come out and the state they leave. The agent is the fixture
// Agent with no delays, or a stub that runs only when a test lets it.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/fixtureagent"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

// ---- helpers ----

func openEngine(t *testing.T, agent engine.Agent, store engine.Store) (*engine.Engine, *testrepo.Repo) {
	t.Helper()
	r := testrepo.New(t)
	return openOn(t, r, agent, store), r
}

func openOn(t *testing.T, r *testrepo.Repo, agent engine.Agent, store engine.Store) *engine.Engine {
	t.Helper()
	e, _, err := engine.Open(engine.Config{
		Repo:   gitsrc.OpenRepo(r.Dir, r.Base, r.Head),
		Opened: session.Opened{PR: session.PullRequest{Number: 7, Title: "Clamp results", OwnerRepo: "acme/calc", State: "open"}, Worktree: r.Dir},
		Agent:  agent,
		Store:  store,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}

func do(t *testing.T, e *engine.Engine, c engine.Command) []session.Event {
	t.Helper()
	evs, err := e.Do(c)
	if err != nil {
		t.Fatalf("%T: %v", c, err)
	}
	return evs
}

func types(evs []session.Event) string {
	var ts []string
	for _, ev := range evs {
		ts = append(ts, ev.Type())
	}
	return strings.Join(ts, " ")
}

func fileAnchor(p string) session.Anchor {
	return session.Anchor{Type: session.AnchorFile, Path: p, Text: p}
}

// diffAnchor anchors on new-side lines [from, to] of calc/calc.go.
func diffAnchor(e *engine.Engine, from, to int) session.Anchor {
	return e.State().AnchorOnLines("calc/calc.go", "new", from, to, "")
}

func fixtureAgent(fx *fixtureagent.Fixtures) *fixtureagent.Agent {
	return fixtureagent.New(fx).NoDelay()
}

// started opens with the fixture agent and waits for the Briefing.
func started(t *testing.T, fx *fixtureagent.Fixtures) *engine.Engine {
	t.Helper()
	e, _ := openEngine(t, fixtureAgent(fx), nil)
	e.Start()
	e.Pump(2 * time.Second)
	if !e.State().Briefing.Ready {
		t.Fatalf("the Briefing didn't land")
	}
	return e
}

// ---- a stub agent the test drives ----

type call struct {
	What  string // briefing | ask | continue | refine
	Q     engine.QuestionRequest
	R     engine.RefineRequest
	B     engine.BriefingRequest
	reply chan any // an AnswerResult, RevisionResult, BriefingResult or error
}

type stub struct {
	mu    sync.Mutex
	calls chan *call
	all   []*call
}

func newStub() *stub { return &stub{calls: make(chan *call, 100)} }

func (s *stub) wait(ctx context.Context, c *call) (any, error) {
	c.reply = make(chan any, 1)
	s.mu.Lock()
	s.all = append(s.all, c)
	s.mu.Unlock()
	s.calls <- c
	select {
	case r := <-c.reply:
		if err, ok := r.(error); ok {
			return nil, err
		}
		return r, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (s *stub) Briefing(ctx context.Context, req engine.BriefingRequest) (engine.BriefingResult, error) {
	r, err := s.wait(ctx, &call{What: "briefing", B: req})
	if err != nil {
		return engine.BriefingResult{}, err
	}
	return r.(engine.BriefingResult), nil
}

func (s *stub) Ask(ctx context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	r, err := s.wait(ctx, &call{What: "ask", Q: req})
	if err != nil {
		return engine.AnswerResult{}, err
	}
	return r.(engine.AnswerResult), nil
}

func (s *stub) Continue(ctx context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	r, err := s.wait(ctx, &call{What: "continue", Q: req})
	if err != nil {
		return engine.AnswerResult{}, err
	}
	return r.(engine.AnswerResult), nil
}

func (s *stub) Refine(ctx context.Context, req engine.RefineRequest) (engine.RevisionResult, error) {
	r, err := s.wait(ctx, &call{What: "refine", R: req})
	if err != nil {
		return engine.RevisionResult{}, err
	}
	return r.(engine.RevisionResult), nil
}

// next is the next call the engine made (it fails the test after 2 s).
func (s *stub) next(t *testing.T) *call {
	t.Helper()
	select {
	case c := <-s.calls:
		return c
	case <-time.After(2 * time.Second):
		t.Fatalf("expected an agent call")
		return nil
	}
}

func (s *stub) none(t *testing.T) {
	t.Helper()
	select {
	case c := <-s.calls:
		t.Fatalf("unexpected agent call: %s %s", c.What, c.Q.ThreadID+c.R.FeedbackID)
	case <-time.After(50 * time.Millisecond):
	}
}

// handle waits for one finished run and commits it.
func handle(t *testing.T, e *engine.Engine) []session.Event {
	t.Helper()
	select {
	case in := <-e.Inbox():
		return e.Handle(in)
	case <-time.After(2 * time.Second):
		t.Fatalf("expected a finished run")
		return nil
	}
}

// briefed starts the engine on the stub and lands a Briefing with the
// given cumulative total.
func briefed(t *testing.T, s *stub, total float64) *engine.Engine {
	t.Helper()
	e, _ := openEngine(t, s, nil)
	e.Start()
	b := s.next(t)
	if b.What != "briefing" {
		t.Fatalf("first call = %s, want the Briefing", b.What)
	}
	b.reply <- engine.BriefingResult{Notes: session.BriefingNotes{TLDR: "Clamps results."}, Run: session.RunInfo{SessionID: "brief", Total: total}}
	handle(t, e)
	return e
}

func answer(gist string, sid string, total float64) engine.AnswerResult {
	return engine.AnswerResult{Answer: session.AnswerBody{Kind: session.Define, Gist: gist}, Run: session.RunInfo{SessionID: sid, Total: total}}
}

// ---- tests ----

func TestOpenBuildsTheDiffAndWaitsToStart(t *testing.T) {
	s := newStub()
	e, _ := openEngine(t, s, nil)
	st := e.State()
	if len(st.Files) != 2 || st.Files["calc/calc.go"] == nil || st.Files["calc/calc_test.go"] == nil {
		t.Fatalf("files = %v", st.FileGitOrder)
	}
	if len(st.Methods) == 0 {
		t.Fatalf("no Touched Methods")
	}
	if st.PR.Number != 7 || st.PR.Title != "Clamp results" {
		t.Fatalf("PR = %+v", st.PR)
	}
	s.none(t) // nothing runs before Start
	do(t, e, engine.Ask{Anchor: fileAnchor("calc/calc.go"), Kind: session.Summarize})
	s.none(t)
	e.Start()
	if c := s.next(t); c.What != "briefing" || len(c.B.Files) != 2 || len(c.B.Methods) == 0 {
		t.Fatalf("Briefing request = %+v", c.B)
	}
	s.none(t) // the Question waits for the Briefing: it forks from it
}

func TestAskAnswersAndFollowUpContinuesTheThread(t *testing.T) {
	e := started(t, nil)
	evs := do(t, e, engine.Ask{Anchor: diffAnchor(e, 4, 5), Kind: session.Define})
	if got := types(evs); got != "thread_created question_started" {
		t.Fatalf("ask events = %s", got)
	}
	th := e.State().ThreadByID("T1")
	if th.Anchor.NewStart != 4 || th.Anchor.NewEnd != 5 || th.Entries[0].Text == "" {
		t.Fatalf("thread = %+v", th)
	}
	evs = e.Pump(2 * time.Second)
	if got := types(evs); got != "answer_arrived" {
		t.Fatalf("pump events = %s", got)
	}
	if th.Status != session.StatusAnswered || !th.Unread || th.AgentSession == "" {
		t.Fatalf("after the Answer: %+v", th)
	}
	do(t, e, engine.MarkRead{ThreadID: "T1"})
	if th.Unread {
		t.Fatalf("still unread")
	}
	if evs := do(t, e, engine.MarkRead{ThreadID: "T1"}); len(evs) != 0 {
		t.Fatalf("marking a read Thread read again should do nothing, got %s", types(evs))
	}
	evs = do(t, e, engine.FollowUp{ThreadID: "T1", Kind: session.Define, Text: "Explain it like I'm five"})
	if got := types(evs); got != "follow_up_asked question_started" {
		t.Fatalf("follow-up events = %s", got)
	}
	if _, err := e.Do(engine.FollowUp{ThreadID: "T1", Kind: session.Define, Text: "again"}); err == nil {
		t.Fatalf("a follow-up on a busy Thread should be refused")
	}
	e.Pump(2 * time.Second)
	if n := len(th.Entries); n != 4 || th.Entries[3].Role != session.RoleAnswer {
		t.Fatalf("entries = %d", n)
	}
}

func TestFollowUpResumesTheThreadsOwnFork(t *testing.T) {
	s := newStub()
	e := briefed(t, s, 0.10)
	do(t, e, engine.Ask{Anchor: fileAnchor("calc/calc.go"), Kind: session.Summarize})
	c := s.next(t)
	if c.What != "ask" || c.Q.Parent.SessionID != "brief" {
		t.Fatalf("a first Question forks the Briefing: %s parent %q", c.What, c.Q.Parent.SessionID)
	}
	c.reply <- answer("Adds.", "t1-fork", 0.13)
	handle(t, e)
	do(t, e, engine.FollowUp{ThreadID: "T1", Kind: session.Summarize, Text: "More?"})
	c = s.next(t)
	if c.What != "continue" || c.Q.Parent.SessionID != "t1-fork" || c.Q.Question != "More?" {
		t.Fatalf("a follow-up resumes the Thread's fork: %s %+v", c.What, c.Q.Parent)
	}
}

func TestCancelQueued(t *testing.T) {
	s := newStub()
	e := briefed(t, s, 0)
	for _, p := range []string{"calc/calc.go", "calc/calc_test.go", "README.md", "go.mod"} {
		do(t, e, engine.Ask{Anchor: fileAnchor(p), Kind: session.Trace})
	}
	for i := 0; i < 3; i++ {
		s.next(t)
	}
	st := e.State()
	if c := st.Counts(); c.Running != 3 || c.Queued != 1 {
		t.Fatalf("counts = %+v", c)
	}
	if _, err := e.Do(engine.Cancel{ThreadID: "T1"}); err == nil || !strings.Contains(err.Error(), "can't be cancelled") {
		t.Fatalf("cancelling a running Question: %v", err)
	}
	evs := do(t, e, engine.Cancel{ThreadID: "T4"})
	if types(evs) != "question_cancelled" || st.ThreadByID("T4") != nil {
		t.Fatalf("cancel: %s", types(evs))
	}
	if q := e.Queued(); len(q) != 0 {
		t.Fatalf("queue = %v", q)
	}
	s.none(t)
}

func TestCancelAQueuedFollowUpKeepsTheThread(t *testing.T) {
	s := newStub()
	e := briefed(t, s, 0)
	for _, p := range []string{"calc/calc.go", "calc/calc_test.go", "README.md"} {
		do(t, e, engine.Ask{Anchor: fileAnchor(p), Kind: session.Trace})
	}
	var first *call
	for i := 0; i < 3; i++ {
		if c := s.next(t); c.Q.ThreadID == "T1" {
			first = c
		}
	}
	first.reply <- answer("Traced.", "t1", 0)
	handle(t, e)
	do(t, e, engine.Ask{Anchor: fileAnchor("go.mod"), Kind: session.Trace}) // takes T1's slot
	s.next(t)
	do(t, e, engine.FollowUp{ThreadID: "T1", Kind: session.Trace, Text: "And then?"})
	do(t, e, engine.Cancel{ThreadID: "T1"})
	th := e.State().ThreadByID("T1")
	if th == nil || th.Status != session.StatusAnswered || len(th.Entries) != 2 {
		t.Fatalf("T1 after cancelling its follow-up: %+v", th)
	}
}

func TestQueueRunsThreeAtATime(t *testing.T) {
	s := newStub()
	e := briefed(t, s, 0)
	for i := 0; i < 5; i++ {
		do(t, e, engine.Ask{Anchor: diffAnchor(e, i+1, i+1), Kind: session.Define})
	}
	var running []*call
	for i := 0; i < 3; i++ {
		running = append(running, s.next(t))
	}
	s.none(t)
	if q := e.Queued(); len(q) != 2 || q[0] != "T4" || q[1] != "T5" {
		t.Fatalf("queue = %v", q)
	}
	running[1].reply <- answer("Two.", "t2", 0)
	evs := handle(t, e)
	if got := types(evs); got != "answer_arrived question_started" {
		t.Fatalf("a finished run starts the next: %s", got)
	}
	if c := s.next(t); c.Q.ThreadID != "T4" {
		t.Fatalf("next = %s, want T4", c.Q.ThreadID)
	}
	s.none(t)
}

func TestDraftsWaitUntilSent(t *testing.T) {
	s := newStub()
	e := briefed(t, s, 0)
	do(t, e, engine.Ask{Anchor: fileAnchor("README.md"), Kind: session.Summarize, Hold: true})
	do(t, e, engine.Ask{Anchor: fileAnchor("go.mod"), Kind: session.Summarize, Hold: true})
	s.none(t)
	if c := e.State().Counts(); c.Drafts != 2 {
		t.Fatalf("counts = %+v", c)
	}
	do(t, e, engine.DeleteDraft{ThreadID: "T2"})
	evs := do(t, e, engine.SendDrafts{})
	if got := types(evs); got != "drafts_sent question_started" {
		t.Fatalf("send drafts: %s", got)
	}
	if c := s.next(t); c.Q.ThreadID != "T1" {
		t.Fatalf("sent %s", c.Q.ThreadID)
	}
}

func TestRefineWithQuestionsBack(t *testing.T) {
	body := "Can we do it the other way?"
	fx := &fixtureagent.Fixtures{Revisions: []fixtureagent.RevisionFixture{{
		FeedbackID: "F1", ForBody: body,
		Revision: session.Revision{Questions: []string{"Which other way?"}},
	}}}
	e := started(t, fx)
	do(t, e, engine.AddFeedback{Anchor: diffAnchor(e, 4, 4), Body: body})
	evs := do(t, e, engine.Refine{ID: "F1"})
	if got := types(evs); got != "refine_asked refine_started" {
		t.Fatalf("refine: %s", got)
	}
	e.Pump(2 * time.Second)
	f := e.State().FeedbackByID("F1")
	if f.Revision == nil || len(f.Revision.Questions) != 1 || f.Revision.Text != "" {
		t.Fatalf("want questions back, got %+v", f.Revision)
	}
	if _, err := e.Do(engine.AcceptRevision{ID: "F1"}); err == nil {
		t.Fatalf("questions can't be accepted")
	}
	// Answering re-runs Refine, in the same agent session.
	do(t, e, engine.Refine{ID: "F1", Answers: "The retry queue."})
	if f.Revision != nil || f.RefineAnswers != "The retry queue." || f.RefineRound != 2 {
		t.Fatalf("answering: %+v", f)
	}
	e.Pump(2 * time.Second)
	if f.Revision == nil || f.Revision.Text == "" || f.Revision.Round != 2 {
		t.Fatalf("second round: %+v", f.Revision)
	}
	do(t, e, engine.AcceptRevision{ID: "F1"})
	if f.Revision != nil || f.PrevBody != body {
		t.Fatalf("accept: %+v", f)
	}
}

func TestRefineAnswersResumeTheRefinesSession(t *testing.T) {
	s := newStub()
	e := briefed(t, s, 0.10)
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("README.md"), Body: "Fix it"})
	do(t, e, engine.Refine{ID: "F1"})
	c := s.next(t)
	if c.R.Resume || c.R.Parent.SessionID != "brief" {
		t.Fatalf("a first Refine forks the Briefing: %+v", c.R)
	}
	c.reply <- engine.RevisionResult{Revision: session.Revision{Questions: []string{"Fix what?"}}, Run: session.RunInfo{SessionID: "ref1", Total: 0.12}}
	handle(t, e)
	do(t, e, engine.Refine{ID: "F1", Answers: "The typo"})
	c = s.next(t)
	if !c.R.Resume || c.R.Parent.SessionID != "ref1" || c.R.Answers != "The typo" || c.R.Body != "Fix it" {
		t.Fatalf("answers resume the Refine's session: %+v", c.R)
	}
}

func TestStaleRevision(t *testing.T) {
	s := newStub()
	e := briefed(t, s, 0)
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("README.md"), Body: "Needs <<which file>> docs"})
	do(t, e, engine.Refine{ID: "F1"})
	c := s.next(t)
	if c.R.Body != "Needs <<which file>> docs" {
		t.Fatalf("refine body = %q", c.R.Body)
	}
	do(t, e, engine.EditFeedback{ID: "F1", Body: "Needs README docs"}) // while Refine runs
	c.reply <- engine.RevisionResult{Revision: session.Revision{Text: "Needs calc.go docs", WhatChanged: "filled the note"}}
	handle(t, e)
	f := e.State().FeedbackByID("F1")
	if f.Revision == nil || !f.Revision.Stale {
		t.Fatalf("the Revision should be Stale: %+v", f.Revision)
	}
	if _, err := e.Do(engine.AcceptRevision{ID: "F1"}); err == nil || !strings.Contains(err.Error(), "Stale") {
		t.Fatalf("accepting a Stale Revision: %v", err)
	}
	if f.Body != "Needs README docs" {
		t.Fatalf("the body must stay the Reviewer's: %q", f.Body)
	}
}

func TestAgentNotesBlockGitHubNotHandOff(t *testing.T) {
	e := started(t, nil)
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("README.md"), Body: "Rename <<the helper's name>>"})
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("go.mod"), Body: "Bump Go"})
	do(t, e, engine.ToggleReady{ID: "F1"})
	do(t, e, engine.ToggleReady{ID: "F2"})
	_, err := e.Do(engine.PostReview{IDs: []string{"F1", "F2"}, Event: "Comment"})
	if err == nil || !strings.Contains(err.Error(), "F1: 1 Agent Note unfilled") {
		t.Fatalf("posting with an unfilled Agent Note: %v", err)
	}
	st := e.State()
	if st.FeedbackByID("F1").Status != session.FeedbackReady {
		t.Fatalf("a refused post changes nothing")
	}
	evs := do(t, e, engine.HandOff{IDs: []string{"F1", "F2"}})
	if types(evs) != "feedback_status" || st.FeedbackByID("F1").Status != session.FeedbackQueued {
		t.Fatalf("hand-off: %s", types(evs))
	}
	if _, err := e.Do(engine.DeleteFeedback{ID: "F1"}); err == nil {
		t.Fatalf("queued Feedback can't be deleted")
	}
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("README.md"), Body: "LGTM"})
	do(t, e, engine.ToggleReady{ID: "F3"})
	evs = do(t, e, engine.PostReview{IDs: []string{"F3"}, Event: "Approve", Summary: "Nice"})
	if types(evs) != "review_posted" || st.FeedbackByID("F3").Status != session.FeedbackPosted {
		t.Fatalf("post: %s", types(evs))
	}
}

func TestBudgetCapsAndOwnCost(t *testing.T) {
	s := newStub()
	e, _ := openEngine(t, s, nil)
	e.Start()
	b := s.next(t)
	if b.B.MaxBudgetUSD != 0.50 {
		t.Fatalf("Briefing cap = %v", b.B.MaxBudgetUSD)
	}
	b.reply <- engine.BriefingResult{Run: session.RunInfo{SessionID: "brief", Total: 0.10}}
	handle(t, e)
	do(t, e, engine.Ask{Anchor: fileAnchor("README.md"), Kind: session.Summarize})
	c := s.next(t)
	if !near(c.Q.MaxBudgetUSD, 0.35) {
		t.Fatalf("a fork's cap is its parent's total plus its budget: %v", c.Q.MaxBudgetUSD)
	}
	c.reply <- answer("Readme.", "t1", 0.13)
	evs := handle(t, e)
	run := evs[0].Data.(session.AnswerArrived).Run
	if !near(run.Cost, 0.03) || !near(run.Total, 0.13) {
		t.Fatalf("own cost = total − parent's: %+v", run)
	}
	do(t, e, engine.FollowUp{ThreadID: "T1", Kind: session.Summarize, Text: "More"})
	c = s.next(t)
	if !near(c.Q.MaxBudgetUSD, 0.38) {
		t.Fatalf("a follow-up's cap is its Thread's total plus its budget: %v", c.Q.MaxBudgetUSD)
	}
	c.reply <- &engine.RunError{Err: fmt.Errorf("budget exceeded"), Run: session.RunInfo{SessionID: "t1", Total: 0.38}}
	handle(t, e)
	st := e.State()
	if !near(st.TotalCost, 0.10+0.03+0.25) {
		t.Fatalf("running cost = %v", st.TotalCost)
	}
	if th := st.ThreadByID("T1"); th.Status != session.StatusFailed || !strings.Contains(th.Entries[len(th.Entries)-1].Text, "budget exceeded") {
		t.Fatalf("a failed run: %+v", th)
	}
}

func near(a, b float64) bool { return a-b < 1e-9 && b-a < 1e-9 }

func TestPathsAreRewrittenToRepoRelative(t *testing.T) {
	s := newStub()
	e := briefed(t, s, 0)
	wt := e.State().WorktreePath
	do(t, e, engine.Ask{Anchor: fileAnchor("calc/calc.go"), Kind: session.Trace})
	c := s.next(t)
	c.reply <- engine.AnswerResult{Answer: session.AnswerBody{
		Kind: session.Trace, Gist: "See " + wt + "/calc/calc.go:5.",
		Citations: []session.Citation{{Path: wt + "/calc/calc.go", Line: 5}},
		Hops:      []session.Hop{{At: wt + "/calc/calc.go:12", Fn: "clamp", Text: "caps"}},
		Ran:       []session.Ran{{Command: "cd " + wt + " && go test ./calc/", Result: "ok " + wt + "/calc"}},
		Verified:  true,
	}}
	handle(t, e)
	a := e.State().ThreadByID("T1").Entries[1].A
	for _, got := range []string{a.Gist, a.Citations[0].Path, a.Hops[0].At, a.Ran[0].Command, a.Ran[0].Result} {
		if strings.Contains(got, wt) {
			t.Errorf("worktree path left in %q", got)
		}
	}
	if a.Citations[0].Path != "calc/calc.go" || a.Hops[0].At != "calc/calc.go:12" {
		t.Errorf("citation %q, hop %q", a.Citations[0].Path, a.Hops[0].At)
	}
}

func TestBriefingFailureLetsQuestionsRunFresh(t *testing.T) {
	s := newStub()
	e, _ := openEngine(t, s, nil)
	e.Start()
	s.next(t).reply <- fmt.Errorf("claude not signed in")
	handle(t, e)
	if e.State().BriefingError == "" {
		t.Fatalf("the failure should be recorded")
	}
	do(t, e, engine.Ask{Anchor: fileAnchor("README.md"), Kind: session.Summarize})
	if c := s.next(t); c.What != "ask" || c.Q.Parent.SessionID != "" {
		t.Fatalf("with no Briefing a Question runs fresh: %+v", c.Q.Parent)
	}
}
