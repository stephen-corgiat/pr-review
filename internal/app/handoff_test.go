package app_test

// Hand-off from the Implementing Agent's side: `pr-review export` and
// `pr-review resolve`, run as separate processes in the Author's checkout.

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/eventlog"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/paths"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

// researchAgent answers every Question with a recognisable Answer.
type researchAgent struct{}

func (researchAgent) Briefing(context.Context, engine.BriefingRequest) (engine.BriefingResult, error) {
	return engine.BriefingResult{Notes: session.BriefingNotes{TLDR: "Clamps results."}}, nil
}
func (researchAgent) Ask(_ context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	return engine.AnswerResult{Answer: session.AnswerBody{Kind: req.Kind, Gist: "REAL-RESEARCH: Max caps every result at 1000."}}, nil
}
func (a researchAgent) Continue(ctx context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	return a.Ask(ctx, req)
}
func (researchAgent) Refine(context.Context, engine.RefineRequest) (engine.RevisionResult, error) {
	return engine.RevisionResult{}, errors.New("no")
}

// handoffSetup is the Author's checkout of acme/calc on #7's branch (with
// a GitHub remote), and #7's Review Session open in a "TUI" engine with
// one answered Thread and F1 queued for the agent.
type handoffSetup struct {
	repo *testrepo.Repo
	f    *forge.Fake
	data string
	tui  *engine.Engine
}

func newHandoff(t *testing.T, state string) *handoffSetup {
	t.Helper()
	r := testrepo.New(t)
	testrepo.Git(t, r.Dir, "remote", "add", "origin", "git@github.com:acme/calc.git")
	testrepo.Git(t, r.Dir, "checkout", "-q", "feature")
	f := forge.NewFake("ana", fakePR(r, "open"))
	data := t.TempDir()
	o := app.PR{Checkout: r.Dir, Number: 7, Forge: f, DataDir: data,
		Agent: func(paths.Session, session.Opened) (engine.Agent, error) { return researchAgent{}, nil }}
	eng, _, _, err := o.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	eng.Start()
	must(t, eng, engine.Ask{Anchor: eng.State().AnchorOnLines("calc/calc.go", "new", 14, 14, ""), Kind: session.Define})
	eng.Pump(2 * time.Second)
	must(t, eng, engine.AddFeedback{Anchor: eng.State().AnchorOnLines("calc/calc.go", "new", 14, 14, ""), Body: "REAL-FEEDBACK: name <<the limit>> in a comment."})
	must(t, eng, engine.ToggleReady{ID: "F1"})
	must(t, eng, engine.HandOff{IDs: []string{"F1"}})
	f.SetState(7, state)
	return &handoffSetup{repo: r, f: f, data: data, tui: eng}
}

func must(t *testing.T, e *engine.Engine, c engine.Command) []session.Event {
	t.Helper()
	evs, err := e.Do(c)
	if err != nil {
		t.Fatalf("%T: %v", c, err)
	}
	return evs
}

func (h *handoffSetup) export(dir string, again bool) (*app.Exported, error) {
	return app.Export{Dir: dir, Forge: h.f, DataDir: h.data, Again: again}.Run(context.Background())
}

// status is F's status as a fresh reader of the log sees it.
func (h *handoffSetup) status(t *testing.T, id string) session.FeedbackStatus {
	t.Helper()
	h.tui.Poll()
	return h.tui.State().FeedbackByID(id).Status
}

// The export refuses anywhere but a checkout of the PR's repo on the PR's
// branch, says why, and leaves the items queued.
func TestExportRefusesOutsideTheCheckout(t *testing.T) {
	h := newHandoff(t, "open")
	other := testrepo.New(t) // another repo, also on a "feature" branch
	testrepo.Git(t, other.Dir, "remote", "add", "origin", "https://github.com/acme/other.git")
	testrepo.Git(t, other.Dir, "checkout", "-q", "feature")
	onMain := testrepo.New(t)
	testrepo.Git(t, onMain.Dir, "remote", "add", "origin", "https://github.com/acme/calc.git")
	for _, c := range []struct{ dir, want string }{
		{t.TempDir(), "not in a git checkout"},
		{onMain.Dir, `branch "main" has no Pull Request`},
		{other.Dir, "this checkout isn't acme/calc"},
		{filepath.Join(h.repo.Dir, "calc"), ""}, // a subdirectory of the right checkout is fine
	} {
		res, err := h.export(c.dir, false)
		if c.want == "" {
			if err != nil {
				t.Fatalf("in %s: %v", c.dir, err)
			}
			if len(res.IDs) != 1 {
				t.Fatalf("exported %v", res.IDs)
			}
			continue
		}
		var r *app.Refusal
		if !errors.As(err, &r) || !strings.Contains(r.Reason, c.want) {
			t.Fatalf("in %s: want a refusal with %q, got %v", c.dir, c.want, err)
		}
		if s := h.status(t, "F1"); s != session.FeedbackQueued {
			t.Fatalf("after a refusal F1 is %s, want queued", s)
		}
	}
}

// On the right checkout it prints the export and marks the items handed
// off; the running TUI's engine picks that up; a second export finds
// nothing, and --again hands them off again.
func TestExportHandsOffAndTheTUIPicksItUp(t *testing.T) {
	h := newHandoff(t, "open")
	res, err := h.export(h.repo.Dir, false)
	if err != nil {
		t.Fatal(err)
	}
	md := res.Markdown
	for _, want := range []string{"# Review Feedback: acme/calc #7", "Written against head sha:** `" + h.repo.Head + "`", "## F1",
		"`calc/calc.go:14`", "const Max = 1000", "REAL-FEEDBACK: name <<the limit>> in a comment.", "## Research", "### T1 · Define",
		"REAL-RESEARCH", "Commit and push", "pr-review resolve <id> addressed"} {
		if !strings.Contains(md, want) {
			t.Errorf("export lacks %q", want)
		}
	}
	evs := h.tui.Poll()
	if len(evs) != 1 || evs[0].Writer == h.tui.Writer() {
		t.Fatalf("the TUI should pick up export's event, got %d", len(evs))
	}
	if s := h.tui.State().FeedbackByID("F1").Status; s != session.FeedbackHandedOff {
		t.Fatalf("F1 is %s", s)
	}
	if _, err := h.export(h.repo.Dir, false); !errors.Is(err, app.ErrNothingQueued) {
		t.Fatalf("a second export: %v", err)
	}
	again, err := h.export(h.repo.Dir, true)
	if err != nil || len(again.IDs) != 1 || !strings.Contains(again.Markdown, "## F1") {
		t.Fatalf("--again: %v %v", again, err)
	}
}

// A closed or merged PR: the checkout check passes, and the refusal says
// the PR's state is why.
func TestExportRefusesOnAFinishedPR(t *testing.T) {
	for _, state := range []string{"closed", "merged"} {
		h := newHandoff(t, state)
		_, err := h.export(h.repo.Dir, false)
		var r *app.Refusal
		if !errors.As(err, &r) || !strings.Contains(r.Reason, "PR #7 is "+state) || !strings.Contains(r.Reason, "Hand-off is disabled on merged and closed PRs") {
			t.Fatalf("%s: %v", state, err)
		}
		if s := h.status(t, "F1"); s != session.FeedbackQueued {
			t.Fatalf("%s: F1 is %s, want queued", state, s)
		}
	}
}

// An early bug: export printed the fixture's hand-off instead of the real
// Review Session's. Fixture data never reaches a real export: not fixture
// mode's own logs.
func TestExportFromARealCheckoutHasOnlyItsOwnSession(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_DATA_HOME", xdg)
	h := newHandoff(t, "open")
	// Fixture mode's own logs, in the same data dir: dispatchly's, and one
	// under this very PR's name.
	for _, fx := range []struct {
		repo string
		n    int
	}{{"acme/dispatchly", 214}, {"acme/calc", 7}} {
		writeFixtureLog(t, paths.FixtureSessionDir(h.data, fx.repo, fx.n).Dir(), fx.repo, fx.n)
	}
	res, err := h.export(h.repo.Dir, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"acme/calc #7", "REAL-FEEDBACK", "REAL-RESEARCH"} {
		if !strings.Contains(res.Markdown, want) {
			t.Errorf("the export lacks the session's own %q", want)
		}
	}
	for _, bad := range []string{"FIXTURE", "dispatchly", "214", "beacon"} {
		if strings.Contains(res.Markdown, bad) {
			t.Errorf("fixture data %q reached a real export:\n%s", bad, res.Markdown)
		}
	}
}

// A log at a real PR's path that says it's a fixture Review Session is
// refused, not exported.
func TestExportRefusesAFixtureLog(t *testing.T) {
	r := testrepo.New(t)
	testrepo.Git(t, r.Dir, "remote", "add", "origin", "git@github.com:acme/calc.git")
	testrepo.Git(t, r.Dir, "checkout", "-q", "feature")
	data := t.TempDir()
	writeFixtureLog(t, paths.SessionDir(data, "acme/calc", 7).Dir(), "acme/calc", 7)
	_, err := app.Export{Dir: r.Dir, Forge: forge.NewFake("ana", fakePR(r, "open")), DataDir: data}.Run(context.Background())
	var ref *app.Refusal
	if !errors.As(err, &ref) || !strings.Contains(ref.Reason, "fixture") {
		t.Fatalf("want a refusal naming the fixture log, got %v", err)
	}
}

// writeFixtureLog writes a fixture-mode log with queued Feedback and an
// answered Thread.
func writeFixtureLog(t *testing.T, dir, repo string, n int) {
	t.Helper()
	log, err := eventlog.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Now()
	ev := func(i int, p session.Payload) session.Event {
		return session.Event{ID: "fx:" + string(rune('a'+i)), Writer: "fx", At: at, Data: p}
	}
	a := session.Anchor{Type: session.AnchorFile, Path: "internal/bounce/webhooks/beacon.go", Text: "beacon.go"}
	if err := log.Append(
		ev(0, session.Opened{PR: session.PullRequest{OwnerRepo: repo, Number: n, Title: "FIXTURE dispatchly beacon"}, Fixture: true}),
		ev(1, session.ThreadCreated{ThreadID: "T1", Anchor: a, Kind: session.Define, Text: "FIXTURE question"}),
		ev(2, session.AnswerArrived{ThreadID: "T1", Answer: session.AnswerBody{Kind: session.Define, Gist: "FIXTURE research"}}),
		ev(3, session.FeedbackCreated{FeedbackID: "F1", Anchor: a, Body: "FIXTURE-ONLY feedback", Status: session.FeedbackQueued}),
	); err != nil {
		t.Fatal(err)
	}
}

// resolve records an Outcome, with or without a commit; the TUI picks it
// up; bad input is refused.
func TestResolve(t *testing.T) {
	h := newHandoff(t, "open")
	resolve := func(oc engine.RecordOutcome) error {
		_, err := app.Resolve{Dir: h.repo.Dir, Forge: h.f, DataDir: h.data, RecordOutcome: oc}.Run(context.Background())
		return err
	}
	if err := resolve(engine.RecordOutcome{ID: "F1", Outcome: "addressed", Note: "named it"}); err == nil || !strings.Contains(err.Error(), "not handed off yet") {
		t.Fatalf("a queued item takes no Outcome yet: %v", err)
	}
	if _, err := h.export(h.repo.Dir, false); err != nil {
		t.Fatal(err)
	}
	if err := resolve(engine.RecordOutcome{ID: "F1", Outcome: "addressed", Note: "named it"}); err != nil {
		t.Fatal(err)
	}
	if err := resolve(engine.RecordOutcome{ID: "F1", Outcome: "addressed", Note: "and tested it", Commit: "abc1234def"}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []engine.RecordOutcome{
		{ID: "F1", Outcome: "declined"},         // no reason
		{ID: "F1", Outcome: "needs-you"},        // no question
		{ID: "F1", Outcome: "fixed", Note: "x"}, // not an Outcome
		{ID: "F9", Outcome: "addressed"},        // no such item
	} {
		if err := resolve(bad); err == nil {
			t.Errorf("%+v should be refused", bad)
		}
	}
	h.tui.Poll()
	os := h.tui.State().FeedbackByID("F1").Outcomes
	if len(os) != 2 || os[0].Commit != "" || os[1].Commit != "abc1234def" || os[1].Note != "and tested it" {
		t.Fatalf("outcomes %+v", os)
	}
	if s := h.tui.State().FeedbackByID("F1").Status; s != session.FeedbackHandedOff {
		t.Fatalf("an Outcome doesn't change the status: %s", s)
	}
}
