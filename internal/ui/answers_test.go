package ui

// Answers: shape, research verdicts, Sample Data, Refine, Send and export.

import (
	"strings"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

func rowWhere(t *testing.T, m *Model, what string, pred func(Row) bool) int {
	t.Helper()
	m.ensureRows()
	for i, r := range m.centreRows {
		if pred(r) {
			return i
		}
	}
	t.Fatalf("no centre row: %s", what)
	return -1
}

func thread(t *testing.T, m *Model, id string) *session.Thread {
	t.Helper()
	if th := m.sess.ThreadByID(id); th != nil {
		return th
	}
	t.Fatalf("no Thread %s", id)
	return nil
}

func feedback(t *testing.T, m *Model, id string) *session.Feedback {
	t.Helper()
	if f := m.sess.FeedbackByID(id); f != nil {
		return f
	}
	t.Fatalf("no Feedback %s", id)
	return nil
}

// cursorOn puts the centre cursor on row i and scrolls it into view.
func cursorOn(m *Model, i int) {
	m.focus = FocusCentre
	m.centreCursor = i
	m.centreScroll = maxInt(0, i-5)
	m.clampCentre()
}

// askTraceA04 asks a Trace on cmd/bounce.go:199 (A04's Anchor) and waits
// for its Answer: T8.
func askTraceA04(t *testing.T, m *Model, w, h int) *session.Thread {
	t.Helper()
	run(t, m, w, h, `idle j j j enter / "ProcessBounce(c" enter esc t idle`, 50*time.Millisecond)
	t8 := thread(t, m, "T8")
	if t8.Status != session.StatusAnswered {
		t.Fatalf("T8 should be answered, got %s", t8.Status)
	}
	return t8
}

// 20. Answer shape: gist and hops show, the detail stays collapsed until
// enter; A03's invented values are tagged and dim.
func TestScenario20_AnswerShape(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	t8 := askTraceA04(t, m, 140, 45)
	m.expandedCards["T8"] = true
	m.dirty = true
	cursorOn(m, rowWhere(t, m, "T8's first hop", func(r Row) bool { return r.Thread == t8 && strings.Contains(r.text(), "cmd/main.go:60") }))
	out := run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "cmd/main.go:60", "first trace hop")
	mustContain(t, out, "(*Beacon).verifyAuth", "a hop's fn")
	det := rowWhere(t, m, "T8's detail row", func(r Row) bool { return r.Thread == t8 && strings.Contains(r.text(), "▸ detail (") })
	cursorOn(m, det)
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "Beacon POSTs to the public", "the gist")
	mustNotContain(t, out, "The only caller is", "the detail stays collapsed")
	out = run(t, m, 140, 45, "enter", 50*time.Millisecond)
	mustContain(t, out, "The only caller is", "enter expands the detail")
	mustContain(t, out, "▾ detail", "expanded detail row")

	// A03 (T3): invented values, tagged and dim.
	m.expandedCards["T3"] = true
	m.openFile("internal/bounce/webhooks/beacon.go")
	m.dirty = true
	t3 := thread(t, m, "T3")
	tag := rowWhere(t, m, "an invented step", func(r Row) bool { return r.Thread == t3 && strings.HasSuffix(r.text(), "invented") })
	cursorOn(m, tag)
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "invented", "a step tagged invented")
	val := m.centreRows[tag+1]
	if !strings.Contains(val.text(), "mapBeaconStatus(") {
		t.Fatalf("the row under the tag should be its value, got %q", val.text())
	}
	for _, s := range val.Spans {
		if strings.TrimSpace(s.Text) != "" && !s.Style.GetFaint() {
			t.Errorf("invented value span %q is not dim", s.Text)
		}
	}
}

// 21. Research verdict: A08 shows its chip and evidence Citations.
func TestScenario21_ResearchVerdict(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle", 50*time.Millisecond)
	m.gotoLine("cmd/init.go", 13)
	m.focus = FocusCentre
	out := run(t, m, 140, 45, "r", 50*time.Millisecond)
	mustContain(t, out, "Ask (Research)", "Ask… opens on Research")
	run(t, m, 140, 45, "enter idle", 50*time.Millisecond)
	t8 := thread(t, m, "T8")
	m.expandedCards["T8"] = true
	m.dirty = true
	cursorOn(m, rowWhere(t, m, "the verdict", func(r Row) bool { return r.Thread == t8 && strings.Contains(r.text(), " NO ") }))
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, " NO ", "verdict chip")
	mustContain(t, out, "It is set in initConstConfig", "the why")
	mustContain(t, out, "evidence: cmd/init.go:13", "evidence as Citations")
	// The evidence is clickable: it jumps (and Back returns).
	ev := rowWhere(t, m, "evidence row", func(r Row) bool { return r.Thread == t8 && strings.HasPrefix(r.text(), "evidence:") })
	if len(m.centreRows[ev].Links) == 0 {
		t.Errorf("evidence Citations should be links")
	}
}

// 22. Sample this path: x on the Trace Answer asks a Sample Data follow-up
// answered with the followup-trace-sample fixture.
func TestScenario22_SampleThisPath(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	t8 := askTraceA04(t, m, 140, 45)
	m.expandedCards["T8"] = true
	m.dirty = true
	cursorOn(m, rowWhere(t, m, "sample-this-path row", func(r Row) bool { return r.Thread == t8 && strings.Contains(r.text(), "[sample this path]") }))
	if k := t8.Kind; k != session.Trace {
		t.Fatalf("T8 is %s", k)
	}
	run(t, m, 140, 45, "x idle", 50*time.Millisecond) // ticks only run inside one Run
	if k := t8.PendingKind(); k != session.SampleData {
		t.Errorf("follow-up Kind = %s, want Sample Data", k)
	}
	if q := t8.Entries[len(t8.Entries)-2]; q.Text != session.SampleThisPathQuestion {
		t.Errorf("the follow-up Question is %q", q.Text)
	}
	last := t8.Entries[t8.LatestAnswerIdx()]
	if last.A == nil || !strings.Contains(last.A.Gist, "Ana@Example.com") || len(last.A.Steps) == 0 {
		t.Fatalf("expected the followup-trace-sample Answer, got %+v", last.A)
	}
	if len(thread(t, m, "T8").Entries) < 4 || m.sess.ThreadByID("T9") != nil {
		t.Errorf("the follow-up belongs in T8's Thread, not a new one")
	}
}

// 23. Agent Notes: the seeded Feedback on beacon.go:182-187 shows them.
func TestScenario23_AgentNotes(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j enter", 50*time.Millisecond)
	f1 := feedback(t, m, "F1")
	cursorOn(m, rowWhere(t, m, "F1's card", func(r Row) bool { return r.Feedback == f1 }))
	out := run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "✎ 3 agent notes", "collapsed card counts the notes")
	if n := len(session.AgentNotes(f1.Body)); n != 3 {
		t.Errorf("F1 has %d Agent Notes, want 3", n)
	}
	// Not notes: inside code, across a blank line, unclosed.
	for body, want := range map[string]int{
		"a `<<code>>` b":              0,
		"```\n<<fenced>>\n```\n<<x>>": 1,
		"<<across\n\nblank>>":         0,
		"unclosed << here":            0,
		"<<a>> and <<b\nc>>":          2,
	} {
		if n := len(session.AgentNotes(body)); n != want {
			t.Errorf("AgentNotes(%q) = %d, want %d", body, n, want)
		}
	}
}

// 24. Refine: r on F1, the word diff with what_changed; A replaces the body.
func TestScenario24_Refine(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j enter", 50*time.Millisecond)
	f1 := feedback(t, m, "F1")
	cursorOn(m, rowWhere(t, m, "F1's card", func(r Row) bool { return r.Feedback == f1 }))
	// (The harness runs ticks only inside one Run, so r and its wait go together.)
	run(t, m, 140, 45, "r idle", 50*time.Millisecond)
	if f1.Revision == nil || f1.Revision.Placeholder {
		t.Fatalf("expected the canned Revision, got %+v", f1.Revision)
	}
	cursorOn(m, rowWhere(t, m, "Revision", func(r Row) bool { return r.Revision && strings.Contains(r.text(), "Filled 3 Agent Notes") }))
	out := run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "✦ Revision", "the Revision block")
	mustContain(t, out, "Filled 3 Agent Notes", "what_changed")
	mustContain(t, out, "schema.sql:19", "its Citations")
	// The revised text with highlights, not an interleaved diff.
	mustContain(t, out, "words removed", "the removed-words line")
	out = run(t, m, 140, 45, "A", 100*time.Millisecond)
	if !strings.Contains(f1.Body, "bounce.actions") || f1.PrevBody == "" {
		t.Errorf("A should replace the body, got %q", f1.Body)
	}
	cursorOn(m, rowWhere(t, m, "F1's card", func(r Row) bool { return r.Feedback == f1 }))
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustNotContain(t, out, "agent notes", "the note count is gone")
	mustNotContain(t, out, "✦ Revision", "the Revision is gone")
}

// 25. Questions back: F4's questions; answering re-runs Refine.
func TestScenario25_QuestionsBack(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j j enter", 50*time.Millisecond)
	f4 := feedback(t, m, "F4")
	cursorOn(m, rowWhere(t, m, "F4's card", func(r Row) bool { return r.Feedback == f4 }))
	run(t, m, 140, 45, "r idle", 50*time.Millisecond)
	q := rowWhere(t, m, "a question", func(r Row) bool {
		return r.Revision && strings.Contains(r.text(), "Which approach did you have in mind")
	})
	cursorOn(m, q)
	out := run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "questions back", "the Revision is questions")
	out = run(t, m, 140, 45, "A", 50*time.Millisecond)
	mustContain(t, out, "answer them", "A on questions explains")
	out = run(t, m, 140, 45, "enter", 50*time.Millisecond)
	mustContain(t, out, "Answer the agent", "enter on a question opens the answer box")
	run(t, m, 140, 45, `"the retry approach from PR 2990" enter idle`, 50*time.Millisecond)
	if f4.Revision == nil || !f4.Revision.Placeholder || len(f4.Revision.Questions) > 0 {
		t.Fatalf("expected the placeholder Revision, got %+v", f4.Revision)
	}
}

// 26. Stale: edit while Refine runs; the Revision is Stale and A refuses.
func TestScenario26_Stale(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j enter", 50*time.Millisecond)
	f2 := feedback(t, m, "F2")
	cursorOn(m, rowWhere(t, m, "F2's card", func(r Row) bool { return r.Feedback == f2 }))
	run(t, m, 140, 45, `r e " Also a doc line?" enter idle`, 50*time.Millisecond)
	if !strings.HasSuffix(f2.Body, "Also a doc line?") {
		t.Fatalf("the edit should save, got %q", f2.Body)
	}
	if f2.Revision == nil || !f2.Revision.Stale {
		t.Fatalf("the Revision should be Stale, got %+v", f2.Revision)
	}
	body := f2.Body
	cursorOn(m, rowWhere(t, m, "Revision", func(r Row) bool { return r.Revision && strings.HasPrefix(r.text(), "Stale:") }))
	out := run(t, m, 140, 45, "A", 50*time.Millisecond)
	mustContain(t, out, "Stale", "A is refused with a reason")
	if f2.Body != body {
		t.Errorf("A on a Stale Revision changed the body")
	}
}

// 27. Send: GitHub is blocked by unfilled notes; Hand to agent queues
// (and, in fixture mode, says there's no export).
func TestScenario27_Send(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j enter", 50*time.Millisecond)
	f1 := feedback(t, m, "F1")
	cursorOn(m, rowWhere(t, m, "F1's card", func(r Row) bool { return r.Feedback == f1 }))
	out := run(t, m, 140, 45, "space P", 50*time.Millisecond)
	if m.overlay != OverlaySend || m.send.dest != destGitHub {
		t.Fatalf("P should open Send on GitHub review (not the Author)")
	}
	mustContain(t, out, "F1: 3 Agent Notes unfilled — Refine or remove them", "the blocked line")
	out = run(t, m, 140, 45, "tab tab tab enter", 50*time.Millisecond) // Send
	mustContain(t, out, "Can't post to GitHub", "the refusal")
	if f1.Status != session.FeedbackReady {
		t.Fatalf("GitHub send should be refused, F1 is %s", f1.Status)
	}
	// Arrows move within the focused group, space selects; tab
	// moves between groups (Destination → Send → Cancel for the agent).
	out = run(t, m, 140, 45, "shift+tab shift+tab shift+tab right space", 50*time.Millisecond)
	mustNotContain(t, out, "⚠ F1", "Hand to agent allows notes")
	out = run(t, m, 140, 45, "tab enter", 200*time.Millisecond)
	if f1.Status != session.FeedbackQueued {
		t.Fatalf("F1 should be queued for agent, is %s", f1.Status)
	}
	// Fixture mode has no export (it only hands off a real PR's
	// Review Session, from its checkout); the markdown is what it would be.
	mustContain(t, out, "Fixture mode has no export", "the last line")
	exp := m.sess.HandoffMarkdown([]*session.Feedback{f1})
	for _, want := range []string{"## F1", fixtureRepo(t).C2, "internal/bounce/webhooks/beacon.go:182-187",
		"func mapBeaconStatus(status, details string) (string, bool) {", `strings.Contains(d, "domain") {`, "<<the setting name and its default>>", "\n## Research", "### T3 · Sample Data"} {
		if !strings.Contains(exp, want) {
			t.Errorf("export lacks %q", want)
		}
	}
	out = run(t, m, 140, 45, "esc", 50*time.Millisecond)
	mustContain(t, out, "queued for agent", "the status badge")
	out = run(t, m, 140, 45, "e", 50*time.Millisecond)
	mustContain(t, out, "can't be edited", "queued Feedback is locked")
}

// 28. With nothing ready, P only flashes.
func TestScenario28_NothingReady(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 140, 45, "idle P", 50*time.Millisecond)
	mustContain(t, out, "Nothing ready: mark Feedback ready with space", "flash")
	if m.overlay != OverlayNone {
		t.Errorf("no dialog when nothing is ready")
	}
}

// 29. The export has one ## Research section listing
// every answered Thread, and no per-item Research.
func TestScenario29_ExportResearch(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j enter", 50*time.Millisecond)
	for _, id := range []string{"F1", "F2"} {
		toggleReady(t, m, feedback(t, m, id))
	}
	md := m.sess.HandoffMarkdown(m.sess.Ready())
	if strings.Count(md, "\n## Research\n") != 1 || strings.Contains(md, "### Research") {
		t.Fatalf("want exactly one ## Research section and no per-item ones")
	}
	res := md[strings.Index(md, "\n## Research"):]
	if strings.Index(md, "## F2") > strings.Index(md, "\n## Research") {
		t.Errorf("## Research should come after the Feedback items")
	}
	for _, th := range m.sess.Threads {
		answered := th.LatestAnswerIdx() >= 0
		head := "### " + th.ID + " · "
		if got := strings.Count(res, head); answered && got != 1 {
			t.Errorf("%s is answered: want it listed once, got %d", th.ID, got)
		} else if !answered && got != 0 {
			t.Errorf("%s has no Answer: it shouldn't be listed", th.ID)
		}
	}
	for _, want := range []string{"**Anchor:** `internal/bounce/webhooks/beacon.go:182-187`", "**Anchor:** the PR description", "Use whichever applies to each item"} {
		if !strings.Contains(md, want) {
			t.Errorf("export lacks %q", want)
		}
	}
}

// Frame fit for the answer views at two terminal sizes.
func TestAnswersFrameFit(t *testing.T) {
	t.Parallel()
	for _, sz := range []struct{ w, h int }{{100, 30}, {200, 50}} {
		m := newPRModel(t)
		t8 := askTraceA04(t, m, sz.w, sz.h)
		m.expandedCards["T8"] = true
		m.detailOpen["T8#1"] = true
		m.dirty = true
		cursorOn(m, rowWhere(t, m, "T8 card", func(r Row) bool { return r.Thread == t8 }))
		run(t, m, sz.w, sz.h, "x idle", 50*time.Millisecond)
		run(t, m, sz.w, sz.h, "z", 50*time.Millisecond)
		m.openFile("internal/bounce/webhooks/beacon.go")
		m.dirty = true
		f1 := feedback(t, m, "F1")
		cursorOn(m, rowWhere(t, m, "F1", func(r Row) bool { return r.Feedback == f1 }))
		run(t, m, sz.w, sz.h, "r idle", 50*time.Millisecond)
		cursorOn(m, rowWhere(t, m, "Revision", func(r Row) bool { return r.Revision }))
		run(t, m, sz.w, sz.h, "", 50*time.Millisecond)
		cursorOn(m, rowWhere(t, m, "F1", func(r Row) bool { return r.Feedback == f1 && !r.Revision }))
		run(t, m, sz.w, sz.h, "e", 50*time.Millisecond)
		run(t, m, sz.w, sz.h, `" <<x>>" esc space P`, 50*time.Millisecond)
		run(t, m, sz.w, sz.h, "tab tab enter", 50*time.Millisecond)
		run(t, m, sz.w, sz.h, `"lgtm" enter shift+tab shift+tab right space tab enter`, 50*time.Millisecond)
		run(t, m, sz.w, sz.h, "esc", 50*time.Millisecond)
		cursorOn(m, rowWhere(t, m, "T3", func(r Row) bool { return r.Thread != nil && r.Thread.ID == "T3" }))
		run(t, m, sz.w, sz.h, "r", 50*time.Millisecond)
		run(t, m, sz.w, sz.h, "tab", 50*time.Millisecond)
		run(t, m, sz.w, sz.h, "esc ?", 50*time.Millisecond)
	}
}
