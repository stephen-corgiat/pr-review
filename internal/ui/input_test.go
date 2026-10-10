package ui

// Click-to-place cursors, the summary box, the cost footer, help and read-only sessions.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/fixtureagent"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/paths"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

// clickOn clicks the cell dx columns into the first occurrence of needle.
func clickOn(t *testing.T, m *Model, w, h int, out, needle string, dx int) string {
	t.Helper()
	x, y := findXY(t, out, needle)
	return run(t, m, w, h, "click:"+xy(x+dx, y), 50*time.Millisecond)
}

// 41. A click in the in-place Feedback editor places the cursor; no
// selection starts.
func TestScenario41_ClickPlacesCursorInFeedback(t *testing.T) {
	m := newPRModel(t)
	run(t, m, 140, 45, "wait:1600ms j j enter j j j", 50*time.Millisecond)
	out := run(t, m, 140, 45, `f "hello world"`, 50*time.Millisecond)
	if m.fbEdit == nil {
		t.Fatalf("f should open the editor")
	}
	clickOn(t, m, 140, 45, out, "hello world", 6)
	if m.sel.Active || m.fbEdit == nil {
		t.Fatalf("a click in the editor keeps it open and selects nothing")
	}
	run(t, m, 140, 45, `"X"`, 50*time.Millisecond)
	if v := m.fbEdit.ta.Value(); v != "hello Xworld" {
		t.Fatalf("after the click and X: %q", v)
	}
	// A second line, then a click back on the first.
	out = run(t, m, 140, 45, `end alt+enter "second"`, 50*time.Millisecond)
	clickOn(t, m, 140, 45, out, "hello Xworld", 0)
	run(t, m, 140, 45, `"<"`, 50*time.Millisecond)
	if v := m.fbEdit.ta.Value(); v != "<hello Xworld\nsecond" {
		t.Fatalf("click on the first line: %q", v)
	}
}

// 42. The Ask box and the Revision editor take clicks too.
func TestScenario42_ClickPlacesCursorInAskAndRevision(t *testing.T) {
	m := newPRModel(t)
	run(t, m, 140, 45, "wait:1600ms j j enter j j j", 50*time.Millisecond)
	out := run(t, m, 140, 45, `r "abc def"`, 50*time.Millisecond)
	if m.overlay != OverlayTextbox {
		t.Fatalf("r should open the Ask box")
	}
	clickOn(t, m, 140, 45, out, "abc def", 4)
	run(t, m, 140, 45, `"Z"`, 50*time.Millisecond)
	if v := m.ta.Value(); v != "abc Zdef" {
		t.Fatalf("Ask box after the click: %q", v)
	}
	run(t, m, 140, 45, "esc", 50*time.Millisecond)

	m2 := newPRModel(t)
	f1 := refinedF1(t, m2)
	cursorOn(m2, rowWhere(t, m2, "Revision", func(r Row) bool { return r.Revision && strings.Contains(r.text(), "words removed") }))
	out = run(t, m2, 140, 45, "e", 50*time.Millisecond)
	if m2.fbEdit == nil || !m2.fbEdit.rev {
		t.Fatalf("e should edit the Revision")
	}
	clickOn(t, m2, 140, 45, out, "(beacon.go:184)", 0)
	run(t, m2, 140, 45, `"Q" enter`, 50*time.Millisecond)
	if !strings.Contains(f1.Revision.Text, "Q(beacon.go:184)") {
		t.Fatalf("Revision after the click: %q", f1.Revision.Text)
	}
}

// 43. The send dialog's Summary box has its own background, and a click
// in it starts editing with the cursor where you clicked.
func TestScenario43_SummaryBox(t *testing.T) {
	m := newPRModel(t)
	run(t, m, 140, 45, "wait:1600ms", 50*time.Millisecond)
	toggleReady(t, m, feedback(t, m, "F4"))
	out := run(t, m, 140, 45, "P", 50*time.Millisecond)
	tint := bgSeq(bgInput)
	_, y := findXY(t, out, "none")
	if line := strings.Split(out, "\n")[y]; !strings.Contains(line, tint) {
		t.Fatalf("the Summary box should have the input background %q:\n%q", tint, line)
	}
	_, ty := findXY(t, out, "Send to")
	if other := strings.Split(out, "\n")[ty]; strings.Contains(other, tint) {
		t.Fatalf("only the Summary box is tinted")
	}
	out = clickOn(t, m, 140, 45, out, "none", 0)
	if !m.send.editing || m.send.focus != sendFocusSummary {
		t.Fatalf("a click in the box edits the summary")
	}
	out = run(t, m, 140, 45, `"abc def"`, 50*time.Millisecond)
	clickOn(t, m, 140, 45, out, "abc def", 4)
	run(t, m, 140, 45, `"Z" enter`, 50*time.Millisecond)
	if m.send.summary != "abc Zdef" {
		t.Fatalf("summary: %q", m.send.summary)
	}
}

// 44. The footer shows the Review Session's running cost.
func TestScenario44_CostFooter(t *testing.T) {
	m := newPRModel(t)
	out := run(t, m, 140, 45, "wait:1600ms", 50*time.Millisecond)
	mustContain(t, out, "$0.00", "no spend in fixture mode")
	if err := m.eng.Record(session.AnswerArrived{ThreadID: "T1", Answer: session.AnswerBody{Kind: session.Define, Gist: "x"}, Run: session.RunInfo{Cost: 0.4217}}); err != nil {
		t.Fatal(err)
	}
	m.dirty = true
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "$0.42", "the running cost")
}

// 45. The help covers what changed: no Probe key, what ran, cost.
func TestScenario45_Help(t *testing.T) {
	m := newPRModel(t)
	run(t, m, 200, 50, "wait:1600ms ?", 50*time.Millisecond)
	body := ansi.Strip(strings.Join(helpBody(150), "\n"))
	for _, s := range []string{"lists what ran", "in the footer", "auto mode", "No Probe dialog", "place the cursor"} {
		if !strings.Contains(body, s) {
			t.Errorf("help lacks %q", s)
		}
	}
	if strings.Contains(body, "review the Probe") || strings.Contains(body, "confirmed by a Probe") {
		t.Errorf("help still describes the Probe dialog")
	}
}

// 46. A Review Session whose PR was merged after it was created opens
// read-only: header chip, no Briefing agent, no sending, no asking.
func TestScenario46_ReadOnlySession(t *testing.T) {
	r := testrepo.New(t)
	f := forge.NewFake("ana", forge.PR{Number: 7, OwnerRepo: "acme/calc", Title: "Clamp results", HeadRef: "feature", BaseRef: "main", HeadSHA: r.Head, BaseSHA: r.Base, State: "open"})
	data := t.TempDir()
	o := app.PR{Checkout: r.Dir, Number: 7, Forge: f, DataDir: data,
		Agent: func(paths.Session, session.Opened) (engine.Agent, error) { return fixtureagent.New(nil).NoDelay(), nil }}
	eng, _, _, err := o.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	eng.Do(engine.AddFeedback{Anchor: session.Anchor{Type: session.AnchorFile, Path: "README.md"}, Body: "ready one"})
	eng.Do(engine.ToggleReady{ID: "F1"})
	eng.Close()
	f.SetState(7, "merged")
	eng, _, _, err = o.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	m := New(eng, false)
	out := run(t, m, 140, 40, "", 100*time.Millisecond)
	mustContain(t, out, "READ-ONLY · MERGED", "header chip")
	mustContain(t, out, "read-only", "the Briefing says why it's empty")
	out = run(t, m, 140, 40, "P", 50*time.Millisecond)
	if m.overlay == OverlaySend {
		t.Fatalf("no send dialog on a merged PR")
	}
	mustContain(t, out, "can't be sent", "the flash says why")
	out = run(t, m, 140, 40, "tab d", 50*time.Millisecond)
	mustContain(t, out, "read-only", "asking is refused")
	if len(m.sess.Threads) != 0 {
		t.Fatalf("no Thread on a read-only session")
	}
}

// 47. Frame fit at 100×30 and 200×50 with these views: the ran list,
// the send dialog's Summary box, the help.
func TestInputFrameFit(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{100, 30}, {200, 50}} {
		m := newPRModel(t)
		run(t, m, sz.w, sz.h, "wait:1600ms", 50*time.Millisecond)
		m.openFile("cmd/bounce.go")
		m.expandedCards["T4"] = true
		m.dirty = true
		m.ensureRows()
		cursorOn(m, rowWhere(t, m, "T4's ran list", func(r Row) bool { return r.Thread != nil && r.Thread.ID == "T4" && strings.HasPrefix(r.text(), "$ ") }))
		out := run(t, m, sz.w, sz.h, "", 50*time.Millisecond)
		mustContain(t, out, "ran", "T4 lists what ran")
		toggleReady(t, m, feedback(t, m, "F4"))
		run(t, m, sz.w, sz.h, "P tab tab enter", 50*time.Millisecond)
		run(t, m, sz.w, sz.h, `"a summary" esc esc ?`, 50*time.Millisecond)
	}
}
