package engine_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/eventlog"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

// snapshot is the logged part of the state, for comparing a live session
// with its replay.
type snapshot struct {
	Threads   []session.Thread
	Feedback  []session.Feedback
	Briefing  session.BriefingNotes
	Order     []string
	TotalCost float64
	PR        session.PullRequest
}

func snap(t *testing.T, st *session.Session) string {
	t.Helper()
	s := snapshot{TotalCost: st.TotalCost, PR: st.PR, Order: st.FileOrder()}
	for _, th := range st.Threads {
		s.Threads = append(s.Threads, *th)
	}
	for _, f := range st.Feedback {
		s.Feedback = append(s.Feedback, *f)
	}
	s.Briefing.TLDR = st.Briefing.TLDR
	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func openLog(t *testing.T, dir string) *eventlog.Log {
	t.Helper()
	l, err := eventlog.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestReplayReproducesTheState(t *testing.T) {
	dir := t.TempDir()
	r := testrepo.New(t)
	e := openOn(t, r, fixtureAgent(nil), openLog(t, dir))
	e.Start()
	e.Pump(2 * time.Second)
	do(t, e, engine.Ask{Anchor: diffAnchor(e, 4, 5), Kind: session.Define})
	do(t, e, engine.Ask{Anchor: fileAnchor("README.md"), Kind: session.Summarize, Text: "What is this?"})
	e.Pump(2 * time.Second)
	do(t, e, engine.MarkRead{ThreadID: "T1"})
	do(t, e, engine.FollowUp{ThreadID: "T1", Kind: session.Define, Text: "Simpler please"})
	e.Pump(2 * time.Second)
	do(t, e, engine.AddFeedback{Anchor: diffAnchor(e, 12, 12), Body: "Name <<the constant>> better"})
	do(t, e, engine.Refine{ID: "F1"})
	e.Pump(2 * time.Second)
	do(t, e, engine.EditRevision{ID: "F1", Text: "Name Max better: MaxResult"})
	do(t, e, engine.AcceptRevision{ID: "F1"})
	do(t, e, engine.ToggleReady{ID: "F1"})
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("go.mod"), Body: "gone soon"})
	do(t, e, engine.DeleteFeedback{ID: "F2"})
	want := snap(t, e.State())
	e.Close()

	again := openOn(t, r, fixtureAgent(nil), openLog(t, dir))
	if got := snap(t, again.State()); got != want {
		t.Fatalf("replay differs.\nwant %s\ngot  %s", want, got)
	}
	if again.State().ThreadByID("T1").AgentSession == "" {
		t.Fatalf("the Thread's agent session should come back from the log")
	}
	if n := again.State().NextFeedbackID(); n != "F3" {
		t.Fatalf("IDs carry on after a replay: next Feedback %s", n)
	}
}

func TestSessionClosedPartwayComesBackTheSame(t *testing.T) {
	dir := t.TempDir()
	r := testrepo.New(t)
	s := newStub()
	e := openOn(t, r, s, openLog(t, dir))
	e.Start()
	s.next(t).reply <- engine.BriefingResult{Notes: session.BriefingNotes{TLDR: "Clamps."}, Run: session.RunInfo{SessionID: "brief", Total: 0.1}}
	handle(t, e)
	// Three running, one queued, one Draft, a Revision waiting, and a
	// Refine that was running when the TUI quit.
	for _, p := range []string{"calc/calc.go", "calc/calc_test.go", "README.md", "go.mod"} {
		do(t, e, engine.Ask{Anchor: fileAnchor(p), Kind: session.Trace})
	}
	var calls []*call
	for i := 0; i < 3; i++ {
		calls = append(calls, s.next(t))
	}
	for _, c := range calls {
		if c.Q.ThreadID == "T1" {
			c.reply <- answer("Traced.", "t1-fork", 0.15)
		}
	}
	handle(t, e) // T1 answers; T4 starts
	s.next(t)
	do(t, e, engine.Ask{Anchor: fileAnchor("README.md"), Kind: session.Define, Hold: true})
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("README.md"), Body: "Fix <<what>>"})
	do(t, e, engine.Refine{ID: "F1"})
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("go.mod"), Body: "Bump"})
	want := snap(t, e.State())
	e.Close()

	s2 := newStub()
	again := openOn(t, r, s2, openLog(t, dir))
	st := again.State()
	if got := snap(t, st); got != want {
		t.Fatalf("replay differs.\nwant %s\ngot  %s", want, got)
	}
	if st.ThreadByID("T5").Status != session.StatusDraft || st.FeedbackByID("F1").Refine != session.RefineQueued {
		t.Fatalf("Draft or queued Refine lost")
	}
	// On Start the interrupted runs go first, then the queue; the
	// Briefing isn't run again, and the Draft stays a Draft.
	again.Start()
	var got []string
	for i := 0; i < 3; i++ {
		c := s2.next(t)
		if c.What == "briefing" {
			t.Fatalf("the Briefing landed already: it must not run again")
		}
		got = append(got, c.Q.ThreadID+c.R.FeedbackID)
	}
	s2.none(t)
	for _, id := range []string{"T2", "T3", "T4"} {
		if !strings.Contains(strings.Join(got, " "), id) {
			t.Fatalf("interrupted %s not re-run first: %v", id, got)
		}
	}
	if q := again.Queued(); len(q) != 1 || q[0] != "fb:F1" {
		t.Fatalf("queue after restart = %v", q)
	}
	// A follow-up resumes T1's fork, recorded in the log.
	do(t, again, engine.FollowUp{ThreadID: "T1", Kind: session.Trace, Text: "More"})
	if th := st.ThreadByID("T1"); th.AgentSession != "t1-fork" || th.AgentTotal != 0.15 {
		t.Fatalf("T1's agent session after replay: %q %v", th.AgentSession, th.AgentTotal)
	}
}

func TestEventsFromAnotherProcessArePickedUp(t *testing.T) {
	dir := t.TempDir()
	r := testrepo.New(t)
	e := openOn(t, r, fixtureAgent(nil), openLog(t, dir))
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("README.md"), Body: "Typo"})
	do(t, e, engine.ToggleReady{ID: "F1"})
	do(t, e, engine.HandOff{IDs: []string{"F1"}})

	// Another process (the export) appends to the same log.
	other := openLog(t, dir)
	if err := other.Append(session.Event{ID: "export:1", Writer: "export", At: time.Now(),
		Data: session.FeedbackStatusChanged{FeedbackIDs: []string{"F1"}, Status: session.FeedbackHandedOff}}); err != nil {
		t.Fatal(err)
	}
	evs := e.Poll()
	if len(evs) != 1 || e.State().FeedbackByID("F1").Status != session.FeedbackHandedOff {
		t.Fatalf("poll: %s, status %s", types(evs), e.State().FeedbackByID("F1").Status)
	}
	if evs := e.Poll(); len(evs) != 0 {
		t.Fatalf("a second poll re-read %s", types(evs))
	}
	// Our own appends are never re-applied by a poll.
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("go.mod"), Body: "Bump"})
	if evs := e.Poll(); len(evs) != 0 || len(e.State().Feedback) != 2 {
		t.Fatalf("own events re-applied: %s", types(evs))
	}
}

func TestTheLogIsOneJSONObjectPerLine(t *testing.T) {
	dir := t.TempDir()
	e := openOn(t, testrepo.New(t), fixtureAgent(nil), openLog(t, dir))
	do(t, e, engine.AddFeedback{Anchor: fileAnchor("README.md"), Body: "multi\nline <<note>>"})
	b, err := os.ReadFile(filepath.Join(dir, eventlog.FileName))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines (opened, feedback_created), got %d:\n%s", len(lines), b)
	}
	for _, l := range lines {
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("bad line %q: %v", l, err)
		}
		if m["type"] == nil || m["id"] == nil || m["at"] == nil {
			t.Fatalf("line lacks type/id/at: %s", l)
		}
	}
	if !strings.Contains(lines[1], `"type":"feedback_created"`) || !strings.Contains(lines[1], `"anchor":{"Type":"file"`) {
		t.Fatalf("enums are stored by name: %s", lines[1])
	}
}
