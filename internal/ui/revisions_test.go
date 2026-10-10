package ui

// Revisions: text, editing, undo, the send dialog and sampling again.

import (
	"strings"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

func refinedF1(t *testing.T, m *Model) *session.Feedback {
	t.Helper()
	run(t, m, 140, 45, "wait:1600ms j j enter", 50*time.Millisecond)
	f1 := feedback(t, m, "F1")
	cursorOn(m, rowWhere(t, m, "F1's card", func(r Row) bool { return r.Feedback == f1 }))
	run(t, m, 140, 45, "r", 6*time.Second)
	if f1.Revision == nil {
		t.Fatalf("no Revision")
	}
	return f1
}

func revisionText(m *Model, f *session.Feedback) string {
	var b strings.Builder
	for _, r := range m.centreRows {
		if r.Revision && r.Feedback == f {
			b.WriteString(r.text() + "\n")
		}
	}
	return b.String()
}

// 36. The Revision shows the revised text, highlights, "words removed"; d
// toggles the old diff.
func TestScenario36_RevisionText(t *testing.T) {
	m := newPRModel(t)
	f1 := refinedF1(t, m)
	cursorOn(m, rowWhere(t, m, "Revision", func(r Row) bool { return r.Revision && strings.Contains(r.text(), "words removed") }))
	out := run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "(beacon.go:184)", "the revised sentence")
	mustContain(t, out, "words removed", "the removed count")
	if rt := revisionText(m, f1); strings.Contains(rt, "<<quote the exact condition") {
		t.Errorf("the Revision shouldn't show the note it replaced:\n%s", rt)
	}
	if !strings.Contains(out, "47;97;67") && !strings.Contains(out, "47:97:67") {
		t.Errorf("no green highlight (#2f6143) on the inserted words")
	}
	out = run(t, m, 140, 45, "d", 50*time.Millisecond)
	if rt := revisionText(m, f1); !strings.Contains(rt, "<<quote the exact condition") {
		t.Errorf("d should show the old word diff (with the deleted note)")
	}
	mustContain(t, out, "word diff against the Feedback", "diff mode line")
}

// 37. e edits the Revision, not the Feedback; A then accepts it.
func TestScenario37_EditRevision(t *testing.T) {
	m := newPRModel(t)
	f1 := refinedF1(t, m)
	body := f1.Body
	cursorOn(m, rowWhere(t, m, "Revision", func(r Row) bool { return r.Revision && strings.Contains(r.text(), "d diff") }))
	out := run(t, m, 140, 45, "e", 50*time.Millisecond)
	if m.fbEdit == nil || !m.fbEdit.rev {
		t.Fatalf("e on a Revision should edit the Revision")
	}
	mustContain(t, out, "Editing F1's Revision", "footer")
	run(t, m, 140, 45, `" EDITED" enter`, 50*time.Millisecond)
	if !strings.Contains(f1.Revision.Text, "EDITED") || f1.Body != body {
		t.Fatalf("saving should change the Revision only: rev %q body %q", f1.Revision.Text, f1.Body)
	}
	cursorOn(m, rowWhere(t, m, "Revision", func(r Row) bool { return r.Revision && strings.Contains(r.text(), "d diff") }))
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "EDITED", "the edited Revision")
	mustContain(t, out, "edited by you", "label")
	// esc cancels an edit.
	run(t, m, 140, 45, `e " NOPE" esc`, 50*time.Millisecond)
	if strings.Contains(f1.Revision.Text, "NOPE") {
		t.Errorf("esc should cancel")
	}
	cursorOn(m, rowWhere(t, m, "Revision", func(r Row) bool { return r.Revision && strings.Contains(r.text(), "d diff") }))
	run(t, m, 140, 45, "A", 50*time.Millisecond)
	if !strings.Contains(f1.Body, "EDITED") || f1.Revision != nil {
		t.Errorf("A should put the edited Revision in the Feedback, got %q", f1.Body)
	}
}

// 38. Undo / redo in the Feedback editor, a word at a time.
func TestScenario38_Undo(t *testing.T) {
	m := newPRModel(t)
	run(t, m, 140, 45, "wait:1600ms j j enter f", 50*time.Millisecond)
	if m.fbEdit == nil {
		t.Fatalf("f should open the editor")
	}
	run(t, m, 140, 45, `"one two three"`, 50*time.Millisecond)
	run(t, m, 140, 45, "ctrl+z", 50*time.Millisecond)
	if v := m.fbEdit.ta.Value(); v != "one two" {
		t.Errorf("ctrl+z: got %q, want %q", v, "one two")
	}
	run(t, m, 140, 45, "ctrl+y", 50*time.Millisecond)
	if v := m.fbEdit.ta.Value(); v != "one two three" {
		t.Errorf("ctrl+y: got %q, want %q", v, "one two three")
	}
	run(t, m, 140, 45, "ctrl+z ctrl+z ctrl+shift+z", 50*time.Millisecond)
	if v := m.fbEdit.ta.Value(); v != "one two" {
		t.Errorf("z z shift+z: got %q, want %q", v, "one two")
	}
	// The cursor comes back where it was: edit in the middle, undo, type.
	run(t, m, 140, 45, `ctrl+z home "X" ctrl+z "Y"`, 50*time.Millisecond)
	if v := m.fbEdit.ta.Value(); v != "Yone" {
		t.Errorf("cursor after undo: got %q, want %q", v, "Yone")
	}
}

// 39. The send dialog keeps one height, and works from the keyboard.
func TestScenario39_SendDialog(t *testing.T) {
	m := newPRModel(t)
	run(t, m, 140, 45, "wait:1600ms", 50*time.Millisecond)
	f4 := feedback(t, m, "F4") // no Agent Notes
	toggleReady(t, m, f4)
	run(t, m, 140, 45, "P", 50*time.Millisecond)
	if m.overlay != OverlaySend || m.send.focus != sendFocusDest {
		t.Fatalf("P should open Send with focus on the destination")
	}
	gh := len(m.renderSend())
	run(t, m, 140, 45, "right space", 50*time.Millisecond)
	if m.send.dest != destAgent {
		t.Fatalf("→ space on the destination should select Hand to agent")
	}
	if ag := len(m.renderSend()); ag != gh {
		t.Errorf("dialog height changes with the destination: %d vs %d", gh, ag)
	}
	run(t, m, 140, 45, "left space", 50*time.Millisecond)
	out := run(t, m, 140, 45, "tab right space", 50*time.Millisecond)
	if m.send.focus != sendFocusEvent || m.send.event != 1 {
		t.Fatalf("tab → space should select Request changes, got focus %d event %d", m.send.focus, m.send.event)
	}
	mustContain(t, out, "▸ Review", "focus marker on the Review group")
	// tab (Summary) tab (Send) enter.
	run(t, m, 140, 45, "tab tab", 50*time.Millisecond)
	if m.send.focus != sendFocusSend {
		t.Fatalf("tab tab should reach Send, focus %d", m.send.focus)
	}
	out = run(t, m, 140, 45, "enter", 50*time.Millisecond)
	if f4.Status != session.FeedbackPosted {
		t.Fatalf("enter on Send should post F4, it's %s", f4.Status)
	}
	mustContain(t, out, "Request changes", "posted as Request changes")
}

// 40. Sample this path twice: two follow-ups, the second the placeholder.
func TestScenario40_SampleAgain(t *testing.T) {
	m := newPRModel(t)
	t8 := askTraceA04(t, m, 140, 45)
	m.expandedCards["T8"] = true
	m.dirty = true
	for k := 0; k < 2; k++ {
		cursorOn(m, rowWhere(t, m, "sample row", func(r Row) bool { return r.Thread == t8 && strings.Contains(r.text(), "[sample this path]") }))
		out := run(t, m, 140, 45, "x", 6*time.Second)
		mustNotContain(t, out, "busy", "a settled Thread isn't busy")
	}
	var qs, fallback int
	for _, e := range t8.Entries {
		if e.Role == session.RoleQuestion && e.Text == session.SampleThisPathQuestion {
			qs++
		}
		if e.Role == session.RoleAnswer && e.A != nil && strings.HasPrefix(e.A.Gist, "No canned Answer") {
			fallback++
		}
	}
	if qs != 2 || fallback != 1 {
		t.Fatalf("want 2 sample follow-ups and 1 placeholder Answer, got %d and %d", qs, fallback)
	}
	if last := t8.Entries[t8.LatestAnswerIdx()]; !strings.HasPrefix(last.A.Gist, "No canned Answer") {
		t.Errorf("the second follow-up should get the placeholder")
	}
	// While one is running, a second press says busy and adds nothing.
	cursorOn(m, rowWhere(t, m, "sample row", func(r Row) bool { return r.Thread == t8 && strings.Contains(r.text(), "[sample this path]") }))
	out := run(t, m, 140, 45, "x x", 50*time.Millisecond)
	mustContain(t, out, "busy", "busy while running")
}
