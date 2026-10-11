package replayagent_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/replayagent"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

type live struct{ calls int }

func (l *live) Briefing(context.Context, engine.BriefingRequest) (engine.BriefingResult, error) {
	l.calls++
	return engine.BriefingResult{Notes: session.BriefingNotes{TLDR: "Guards the rollover."}, Run: session.RunInfo{SessionID: "s1", Cost: 0.5}}, nil
}
func (l *live) Ask(_ context.Context, r engine.QuestionRequest) (engine.AnswerResult, error) {
	l.calls++
	return engine.AnswerResult{Answer: session.AnswerBody{Kind: r.Kind, Gist: "LIVE: " + r.AnchorCode}, Run: session.RunInfo{Cost: 0.1}}, nil
}
func (l *live) Continue(ctx context.Context, r engine.QuestionRequest) (engine.AnswerResult, error) {
	return l.Ask(ctx, r)
}
func (l *live) Refine(_ context.Context, r engine.RefineRequest) (engine.RevisionResult, error) {
	l.calls++
	return engine.RevisionResult{Revision: session.Revision{Text: "REFINED: " + r.Body}}, nil
}

// What a recording run saw is what a replay returns, without the live
// agent, and a request nobody recorded is an error that says so.
func TestRecordThenReplay(t *testing.T) {
	dir := t.TempDir()
	inner := &live{}
	rec := &replayagent.Agent{Inner: inner, Dir: dir, Record: true}
	ctx := context.Background()
	q := engine.QuestionRequest{Kind: session.Define, AnchorCode: "12: if (x) {", Anchor: session.Anchor{Path: "a.ts"}}
	want, err := rec.Ask(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Refine(ctx, engine.RefineRequest{Body: "use <<an example>>"}); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Briefing(ctx, engine.BriefingRequest{PR: session.PullRequest{Number: 66, HeadSHA: "abc"}}); err != nil {
		t.Fatal(err)
	}

	rep := &replayagent.Agent{Dir: dir}
	q.Parent = session.RunInfo{SessionID: "another-run"} // session ids don't matter
	got, err := rep.Ask(ctx, q)
	if err != nil || got.Answer.Gist != want.Answer.Gist || got.Run.Cost != 0.1 {
		t.Fatalf("replayed %+v, %v", got, err)
	}
	if r, err := rep.Refine(ctx, engine.RefineRequest{Body: "use <<an example>>"}); err != nil || r.Revision.Text != "REFINED: use <<an example>>" {
		t.Fatalf("refine %+v, %v", r, err)
	}
	if b, err := rep.Briefing(ctx, engine.BriefingRequest{PR: session.PullRequest{Number: 66, HeadSHA: "abc"}}); err != nil || b.Notes.TLDR != "Guards the rollover." {
		t.Fatalf("briefing %+v, %v", b, err)
	}
	if inner.calls != 3 {
		t.Fatalf("live agent called %d times, want 3 (all while recording)", inner.calls)
	}
	q.AnchorCode = "99: something else"
	if _, err := rep.Ask(ctx, q); err == nil || !strings.Contains(err.Error(), "no recording") {
		t.Fatalf("an unrecorded request: %v", err)
	}
}

// A forge.json that's there, written by hand for a made-up PR, is served in
// recording too: the real Forge is only asked when it's missing.
func TestForgeRecordKeepsForgeJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "forge.json"), []byte(`{"Number": 42, "HeadRef": "discount-codes", "State": "open"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	f := replayagent.Forge{Inner: noForge{}, Dir: dir, Record: true}
	ctx := context.Background()
	if pr, err := f.PR(ctx, 42); err != nil || pr.HeadRef != "discount-codes" {
		t.Fatalf("PR = %+v, %v", pr, err)
	}
	if n, err := f.PRForBranch(ctx, "discount-codes"); err != nil || n != 42 {
		t.Fatalf("PRForBranch = %d, %v", n, err)
	}
}

// noForge is a real Forge that can't be reached.
type noForge struct{ forge.Forge }

func (noForge) PR(context.Context, int) (forge.PR, error) { return forge.PR{}, errors.New("no GitHub") }
func (noForge) PRForBranch(context.Context, string) (int, error) {
	return 0, errors.New("no GitHub")
}

func TestParse(t *testing.T) {
	for in, want := range map[string]struct {
		dir    string
		record bool
		ok     bool
	}{"record:/x/y": {"/x/y", true, true}, "replay:d": {"d", false, true}, "": {}, "replay:": {}, "tape:d": {}} {
		d, r, ok := replayagent.Parse(in)
		if d != want.dir || r != want.record || ok != want.ok {
			t.Errorf("Parse(%q) = %q %v %v", in, d, r, ok)
		}
	}
}
