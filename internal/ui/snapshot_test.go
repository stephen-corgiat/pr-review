package ui

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/fixtureagent"
	"github.com/stephen-corgiat/pr-review/internal/fixturerepo"
	"github.com/stephen-corgiat/pr-review/internal/harness"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

const fixtureDir = "../../testdata/dispatchly"

// newPRModel opens the fixture PR (dispatchly #214) in fixture mode, with
// the log in memory and an agent that answers at once: wait for it with
// the "idle" event.
func newPRModel(t *testing.T) *Model {
	t.Helper()
	fx, err := fixtureagent.LoadFixtures(fixtureDir)
	if err != nil {
		t.Fatal(err)
	}
	return newPRModelWith(t, fixtureagent.New(fx).NoDelay())
}

// newPRModelWith is newPRModel with agent (nil: the fixture agent with its
// usual delays, for a run still going when the test looks).
func newPRModelWith(t *testing.T, agent engine.Agent) *Model {
	t.Helper()
	eng, _, err := app.Fixture{FixturesDir: fixtureDir, CloneDir: fixturerepo.CacheParent(), Agent: agent}.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(eng.Close)
	return New(eng, false)
}

// fixtureRepo is the fixture PR's git repo: the PR is C1..C2, and C0..C1 is
// an earlier commit.
func fixtureRepo(t *testing.T) *fixturerepo.Repo {
	t.Helper()
	r, err := fixturerepo.Build(fixtureDir+"/repo", fixturerepo.CacheParent())
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func newRepoModel(t *testing.T, base, head string) *Model {
	t.Helper()
	eng, _, err := app.Repo(fixtureRepo(t).Dir, base, head, nil)
	if err != nil {
		t.Fatalf("open (-repo mode): %v", err)
	}
	t.Cleanup(eng.Close)
	return New(eng, true)
}

// toggleReady marks Feedback ready (or back to draft), as space does.
func toggleReady(t *testing.T, m *Model, f *session.Feedback) {
	t.Helper()
	if _, err := m.eng.Do(engine.ToggleReady{ID: f.ID}); err != nil {
		t.Fatalf("ready: %v", err)
	}
	m.dirty = true
}

// ask sends an Ask straight to the engine (as the action keys do).
func ask(t *testing.T, m *Model, a session.Anchor, k session.QuestionKind) {
	t.Helper()
	if _, err := m.eng.Do(engine.Ask{Anchor: a, Kind: k}); err != nil {
		t.Fatalf("ask: %v", err)
	}
	m.dirty = true
}

// run feeds events to m (keeping its state between calls) and returns the
// frame, checking that it fits w×h exactly.
func run(t *testing.T, m *Model, w, h int, events string, settle time.Duration) string {
	t.Helper()
	evs, err := harness.ParseEvents(events)
	if err != nil {
		t.Fatalf("bad event spec %q: %v", events, err)
	}
	_, out := harness.Run(m, w, h, evs, settle)
	checkFrameFit(t, out, w, h)
	if testing.Verbose() {
		t.Logf("frame after %q:\n%s", events, ansi.Strip(out))
	}
	return out
}

func checkFrameFit(t *testing.T, content string, w, h int) {
	t.Helper()
	lines := strings.Split(content, "\n")
	if len(lines) != h {
		t.Errorf("frame height = %d, want %d", len(lines), h)
	}
	for i, l := range lines {
		if wid := ansi.StringWidth(l); wid != w {
			t.Errorf("line %d width = %d, want %d: %q", i, wid, w, ansi.Strip(l))
		}
	}
}

func mustContain(t *testing.T, content, substr, what string) {
	t.Helper()
	if !strings.Contains(ansi.Strip(content), substr) {
		t.Errorf("%s: expected %q in frame\n%s", what, substr, ansi.Strip(content))
	}
}

func mustNotContain(t *testing.T, content, substr, what string) {
	t.Helper()
	if strings.Contains(ansi.Strip(content), substr) {
		t.Errorf("%s: did not expect %q in frame\n%s", what, substr, ansi.Strip(content))
	}
}

// findXY is the screen cell of the first occurrence of needle.
func findXY(t *testing.T, content, needle string) (x, y int) {
	t.Helper()
	for row, l := range strings.Split(content, "\n") {
		plain := ansi.Strip(l)
		if idx := strings.Index(plain, needle); idx >= 0 {
			return ansi.StringWidth(plain[:idx]), row
		}
	}
	t.Fatalf("could not find %q in frame\n%s", needle, ansi.Strip(content))
	return 0, 0
}

func xy(x, y int) string { return strconv.Itoa(x) + "," + strconv.Itoa(y) }

// centreRowOf finds the centre row index whose text contains needle.
func centreRowOf(t *testing.T, m *Model, needle string) int {
	t.Helper()
	for i, r := range m.centreRows {
		if strings.Contains(r.text(), needle) {
			return i
		}
	}
	t.Fatalf("no centre row contains %q", needle)
	return -1
}

// ---- the core scenarios ----

func TestScenario01_Startup(t *testing.T) {
	t.Parallel()
	for _, sz := range []struct{ w, h int }{{100, 30}, {200, 50}} {
		m := newPRModel(t)
		out := run(t, m, sz.w, sz.h, "idle", 100*time.Millisecond)
		mustContain(t, out, "TL;DR", "TL;DR heading")
		mustContain(t, out, "webhooks/beacon.go", "navigator lists beacon.go")
		// The navigator is in reading order once the Briefing lands:
		// beacon.go (1st in reading order) before cmd/bounce.go.
		ax, ay := findXY(t, out, "beacon.go")
		_, by := findXY(t, out, "cmd/bounce.go")
		if ay > by || ax > 30 {
			t.Errorf("navigator not in reading order (beacon.go row %d, bounce.go row %d)", ay, by)
		}
		m.focus = FocusCentre
		m.centreScroll = centreRowOf(t, m, "⚙ ") - 1
		out = run(t, m, sz.w, sz.h, "", 50*time.Millisecond)
		mustContain(t, out, "⚙ Added", "computed Contract Changes")
		m.centreScroll = centreRowOf(t, m, "Call map")
		out = run(t, m, sz.w, sz.h, "", 50*time.Millisecond)
		mustContain(t, out, "App.BounceWebhook", "call map root")
		mustContain(t, out, "└─ ", "call map tree connectors")
	}
}

func TestScenario02_MethodHeaderRows(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 120, 40, "idle j j enter", 200*time.Millisecond)
	mustContain(t, out, "internal/bounce/webhooks/beacon.go", "centre title shows beacon.go")
	for i := 0; i < 8 && !strings.Contains(ansi.Strip(out), "func (b *Beacon) ProcessBounce"); i++ {
		out = run(t, m, 120, 40, "J", 50*time.Millisecond)
	}
	// The jump puts the method's block near the top, so its rows show
	// without scrolling.
	mustContain(t, out, "── (*Beacon).ProcessBounce", "ProcessBounce's header rule")
	mustContain(t, out, "↑ callers  BounceWebhook", "caller row for ProcessBounce")
	mustContain(t, out, "↓ calls    verifyAuth · parseBeaconEvents · mapBeaconStatus", "callee row for ProcessBounce")
	if off := m.centreCursor - m.centreScroll; off > 4 {
		t.Errorf("method jump left the header rule %d rows down the pane; want near the top", off)
	}
}

func TestScenario03_DoubleClickDefineCard(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 140, 40, "idle j j j enter", 200*time.Millisecond)
	x, y := findXY(t, out, "X-Beacon-Event")
	out = run(t, m, 140, 40, "dclick:"+xy(x+2, y), 100*time.Millisecond)
	mustContain(t, out, "d Define", "action bar shows after a double-click")
	out = run(t, m, 140, 40, "d", 100*time.Millisecond)
	mustContain(t, out, "T8", "the new Thread's card")
	out = run(t, m, 140, 40, "idle", 50*time.Millisecond)
	mustContain(t, out, "● new", "answered card shows the unread marker")
	out = run(t, m, 140, 40, "z", 100*time.Millisecond)
	mustContain(t, out, "Beacon Relay", "A01's body once expanded")
}

// No Probe dialog. The agent runs its Probe in auto mode and the
// Answer arrives Verified, listing what it ran.
func TestScenario04_ProbeRunsInAutoMode(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 140, 45, "idle j j enter "+strings.Repeat("J ", 7), 200*time.Millisecond)
	mustContain(t, out, "func mapBeaconStatus", "cursor on mapBeaconStatus")
	run(t, m, 140, 45, "v "+strings.Repeat("j ", 30)+"x idle", 50*time.Millisecond)
	t8 := m.sess.ThreadByID("T8")
	if t8 == nil || t8.Status != session.StatusAnswered {
		t.Fatalf("T8 should be answered, got %+v", t8)
	}
	a := t8.Entries[t8.LatestAnswerIdx()].A
	if !a.Verified || len(a.Ran) == 0 {
		t.Fatalf("T8's Answer should be Verified with what it ran, got %+v", a)
	}
	m.expandedCards["T8"] = true
	m.dirty = true
	m.ensureRows()
	m.centreScroll = centreRowOf(t, m, "Sending domain not verified") - 5
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "✅", "the Answer is Verified")
	mustContain(t, out, "Sending domain not verified", "the Answer's body")
	mustNotContain(t, out, "Probe proposed", "no Probe dialog or prompt")
	m.centreScroll = centreRowOf(t, m, "$ cat > internal/bounce/webhooks/zz_probe_test.go") - 3
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "ran", "the Thread lists what ran")
	mustContain(t, out, "$ cat > internal/bounce/webhooks/zz_probe_test.go", "the command's first line")
}

func TestScenario05_TestCasesOnWholeFile(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j c idle", 50*time.Millisecond)
	// A whole-file Thread's card sits at the top of the file.
	out := run(t, m, 140, 45, "enter z", 200*time.Millisecond)
	// An earlier run ran `go test -cover` itself: the table and
	// the command come with the Answer.
	mustContain(t, out, "Confirmed: none of the three bounce packages", "A05's gist")
	m.centreScroll = centreRowOf(t, m, "Test case") - 3
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "Test case", "Test Cases table heading")
	mustContain(t, out, "bounces.cy.js:3", "A05 test case location")
	mustContain(t, out, "untouched", "A05 change column")
	mustContain(t, out, "$ go test -cover ./...", "what it ran")
}

// The list pane under the navigator jumps to a Thread: its card opens near
// the top of the centre.
func TestScenario06_ListJumpsToThread(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 140, 40, "idle tab tab", 100*time.Millisecond)
	mustContain(t, out, "Threads 7", "list pane tab")
	for i, r := range m.panelRows {
		if r.Thread != nil && r.Thread.ID == "T2" {
			m.panelCursor = i
		}
	}
	out = run(t, m, 140, 40, "enter", 100*time.Millisecond)
	if m.centreView != ViewFile || !m.expandedCards["T2"] {
		t.Fatalf("enter in the list should open T2's card in the centre")
	}
	mustContain(t, out, "▾ ✓ T2", "T2's card is expanded")
	if off := m.centreCursor - m.centreScroll; off > 4 {
		t.Errorf("T2's card is %d rows down; want near the top", off)
	}
	// The list shows ten items, pinned under the navigator.
	if in := m.panelInner(); in.H != listItems {
		t.Errorf("list pane shows %d rows, want %d", in.H, listItems)
	}
}

func TestScenario07_HoldAndSendDrafts(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 140, 40, "idle j j enter h d j j j d", 300*time.Millisecond)
	mustContain(t, out, "HOLD", "header shows HOLD")
	mustContain(t, out, "○ 4 drafts", "two seeded + two held Drafts")
	// H sends the held Drafts (S is "Since last review" now).
	out = run(t, m, 140, 40, "H", 300*time.Millisecond)
	mustNotContain(t, out, "4 drafts", "Drafts sent")
}

func TestScenario08_CancelQueuedThread(t *testing.T) {
	t.Parallel()
	m := newPRModelWith(t, nil)   // the Questions must still be running and queued
	run(t, m, 140, 40, "idle", 0) // Questions wait for the Briefing: they fork from it
	for _, p := range []string{"cmd/bounce.go", "cmd/init.go", "cmd/settings.go", "internal/bounce/bounce.go"} {
		ask(t, m, session.Anchor{Type: session.AnchorFile, Path: p}, session.Trace)
	}
	if c := m.sess.Counts(); c.Running != 3 || c.Queued != 1 {
		t.Fatalf("want 3 running + 1 queued, got %+v", c)
	}
	out := run(t, m, 140, 40, "tab tab 1", 100*time.Millisecond)
	mustContain(t, out, "1 queued", "header shows the queue")
	for i, r := range m.panelRows {
		if r.Thread != nil && r.Thread.Status == session.StatusQueued {
			m.panelCursor = i
		}
	}
	out = run(t, m, 140, 40, "X", 100*time.Millisecond)
	mustContain(t, out, "cancelled", "cancel flash")
	if c := m.sess.Counts(); c.Queued != 0 {
		t.Errorf("queued Thread not removed")
	}
}

func TestScenario09_FeedbackAddAndDelete(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	// f opens an editor inside a new Feedback card, under the line: no popup.
	out := run(t, m, 140, 40, "idle j j enter f", 200*time.Millisecond)
	mustContain(t, out, "F3 · Feedback · new", "in-place editor card")
	mustContain(t, out, "enter save", "editor hints")
	if m.overlay != OverlayNone {
		t.Fatalf("Feedback should not open an overlay")
	}
	out = run(t, m, 140, 40, `"looks" space "wrong" enter`, 100*time.Millisecond)
	if m.fbEdit != nil {
		t.Fatalf("enter should close the editor")
	}
	mustContain(t, out, "✎ F3", "saved Feedback card")
	mustContain(t, out, "looks", "saved body shows in the card")
	// e edits it in place again; esc leaves it unchanged.
	m.centreCursor = centreRowOf(t, m, "F3")
	out = run(t, m, 140, 40, `e "!!" esc`, 100*time.Millisecond)
	if f := m.sess.Feedback[3]; strings.Contains(f.Body, "!!") {
		t.Errorf("esc should discard the edit, got %q", f.Body)
	}
	// f then esc on a new one leaves nothing behind.
	run(t, m, 140, 40, "g f esc", 100*time.Millisecond)
	if len(m.sess.Feedback) != 4 {
		t.Errorf("a cancelled new Feedback should be deleted, have %d", len(m.sess.Feedback))
	}
	// The seeds are F1, F2, F4: the new F3 is listed last.
	out = run(t, m, 140, 40, "tab 2 j j j", 100*time.Millisecond)
	out = run(t, m, 140, 40, "X", 100*time.Millisecond)
	mustContain(t, out, "Delete Feedback F3?", "confirm")
	out = run(t, m, 140, 40, "y", 100*time.Millisecond)
	mustNotContain(t, out, "Delete Feedback F3?", "confirm closed")
	if len(m.sess.Feedback) != 3 {
		t.Errorf("F3 not deleted")
	}
}

func TestScenario10_CopySelection(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 140, 40, "idle j j j enter", 200*time.Millisecond)
	x, y := findXY(t, out, "X-Beacon-Event")
	out = run(t, m, 140, 40, "dclick:"+xy(x+2, y)+" y", 300*time.Millisecond)
	mustContain(t, out, "Copied 14 chars", "copy flash")
}

func TestScenario11_FallbackAnswer(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 40, "idle j j enter g d idle", 50*time.Millisecond)
	out := run(t, m, 140, 40, "z", 200*time.Millisecond)
	mustContain(t, out, "No canned Answer", "fallback Answer")
}

func TestScenario12_RepoMode(t *testing.T) {
	t.Parallel()
	r := fixtureRepo(t)
	m := newRepoModel(t, r.C0, r.C1)
	out := run(t, m, 140, 40, "", 100*time.Millisecond)
	mustContain(t, out, "No Briefing agent in -repo mode", "no-agent note")
	m.centreScroll = centreRowOf(t, m, "UpdateContactWithLists") - 2
	out = run(t, m, 140, 40, "", 50*time.Millisecond)
	mustContain(t, out, "UpdateContactWithLists", "signature-changed method in the Briefing")
}

// ---- regressions for the review of the first v2 build ----

// Briefing text wraps instead of running off the pane.
func TestReview_BriefingWraps(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 100, 30, "idle", 100*time.Millisecond)
	mustContain(t, out, "no auth.", "the TL;DR's last words are on screen")
	for _, r := range m.centreRows {
		if w := cellWidth(r.Prefix) + cellWidth(r.text()) + 1; w > m.centreInner().W {
			t.Errorf("row wider than the pane (%d > %d): %q", w, m.centreInner().W, r.text())
		}
	}
}

// The wheel scrolls a long file all the way, and doesn't snap back to the
// cursor.
func TestReview_WheelScrollsLongFile(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 40, "idle j j enter E", 100*time.Millisecond)
	in := m.centreInner()
	wheel := strings.Repeat("wheeldown:"+xy(in.X+10, in.Y+5)+" ", 20)
	run(t, m, 140, 40, wheel, 50*time.Millisecond)
	if m.centreScroll < 55 {
		t.Errorf("20 wheel notches scrolled to %d, want ≥55", m.centreScroll)
	}
	run(t, m, 140, 40, "j", 50*time.Millisecond)
	if m.centreScroll < 55 {
		t.Errorf("a key after wheel-scrolling snapped the view back to %d", m.centreScroll)
	}
	up := strings.Repeat("wheelup:"+xy(in.X+10, in.Y+5)+" ", 40)
	run(t, m, 140, 40, up, 50*time.Millisecond)
	if m.centreScroll != 0 {
		t.Errorf("wheel up didn't return to the top: %d", m.centreScroll)
	}
}

// Clicking a Touched Method in the navigator puts it near the top.
func TestReview_NavMethodOpensNearTop(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 140, 40, "idle j j enter", 100*time.Millisecond)
	x, y := findXY(t, out, "parseBeaconEvents")
	out = run(t, m, 140, 40, "click:"+xy(x+1, y), 100*time.Millisecond)
	if off := m.centreCursor - m.centreScroll; off > 4 {
		t.Errorf("method opened %d rows down; want near the top", off)
	}
	mustContain(t, out, "func parseBeaconEvents", "the method's func line")
}

// Threads and Feedback on the Description show as cards on the Description.
func TestReview_DescriptionCards(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 140, 45, "idle j enter", 100*time.Millisecond)
	mustContain(t, out, "T6", "seeded Define on internetMessageId shows inline")
	// Select a word in the description and add Feedback on it.
	x, y := findXY(t, out, "small")
	run(t, m, 140, 45, "dclick:"+xy(x+2, y)+` f "nit" enter`, 100*time.Millisecond)
	if len(m.sess.Feedback) != 4 || m.sess.Feedback[3].Body != "nit" {
		t.Fatalf("expected F3 = nit on the Description")
	}
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "✎ F3", "new Feedback card on the Description")
}

// Removed lines render, tinted red, with a − sign.
func TestReview_RemovedLinesShow(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle", 50*time.Millisecond)
	m.openFile("cmd/init.go")
	m.dirty = true
	m.ensureRows()
	m.centreScroll = centreRowOf(t, m, `RecordBounce:     a.recordBounce`) - 5
	out := run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "−", "removed sign")
	if !strings.Contains(out, "61;31;36") && !strings.Contains(out, "61:31:36") {
		t.Errorf("no red tint (#3d1f24) in the frame")
	}
}

// Double-click on a list row jumps to it; double-click on a Feedback card
// edits it in place.
func TestReview_DoubleClickOpensThread(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 140, 50, "idle", 50*time.Millisecond)
	// T1 is in the list's first five (most urgent first).
	x, y := findXY(t, out, "T1  bounce.go")
	out = run(t, m, 140, 50, "dclick:"+xy(x+1, y), 100*time.Millisecond)
	if !m.expandedCards["T1"] || m.filePath != "cmd/bounce.go" {
		t.Fatalf("double-click should open T1 in the centre")
	}
	mustContain(t, out, "▾ ✓ T1", "T1's card is expanded")
	out = run(t, m, 140, 50, "2", 50*time.Millisecond)
	x, y = findXY(t, out, "F1  beacon.go")
	// F1's row is next to where T1's was: wait out the 400ms click window
	// so this isn't counted as a triple-click.
	out = run(t, m, 140, 50, "wait:450ms dclick:"+xy(x+1, y), 100*time.Millisecond)
	if !m.expandedCards["fb:F1"] || m.filePath != "internal/bounce/webhooks/beacon.go" {
		t.Fatalf("double-click should open F1's card in the centre")
	}
	// Double-click the card's body: edit in place.
	x, y = findXY(t, out, "DNS mistake")
	out = run(t, m, 140, 50, "dclick:"+xy(x+2, y), 100*time.Millisecond)
	if m.fbEdit == nil || m.fbEdit.fb.ID != "F1" {
		t.Fatalf("double-click on the Feedback should edit it in place")
	}
	mustContain(t, out, "F1 · Feedback · editing", "editor card")
}

// Clicking a call-graph name jumps to that method.
func TestReview_CallGraphLinks(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 140, 40, "idle j j enter", 100*time.Millisecond)
	for i := 0; i < 8 && !strings.Contains(ansi.Strip(out), "↓ calls    verifyAuth"); i++ {
		out = run(t, m, 140, 40, "J", 50*time.Millisecond)
	}
	x, y := findXY(t, out, "mapBeaconStatus")
	out = run(t, m, 140, 40, "click:"+xy(x+2, y), 100*time.Millisecond)
	mustContain(t, out, "func mapBeaconStatus", "jumped to mapBeaconStatus")
}

// ---- core scenarios ----

// 13. The list pane shows ten items; on a short terminal the navigator
// keeps at least eight rows.
func TestScenario13_ListPaneHeight(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	for _, p := range []string{"cmd/bounce.go", "cmd/init.go", "cmd/settings.go", "internal/bounce/bounce.go"} {
		ask(t, m, session.Anchor{Type: session.AnchorFile, Path: p}, session.Trace)
	}
	out := run(t, m, 120, 40, "idle", 100*time.Millisecond)
	mustContain(t, out, "Threads 11", "eleven Threads")
	if in := m.panelInner(); in.H != 10 {
		t.Errorf("list pane has %d rows at 120×40, want 10", in.H)
	}
	// Count the rendered list rows that hold a Thread.
	in := m.panelInner()
	lines := strings.Split(ansi.Strip(out), "\n")
	items := 0
	for y := in.Y; y < in.Y+in.H; y++ {
		r := []rune(lines[y])
		cell := strings.TrimSpace(string(r[in.X:minInt(len(r), in.X+8)]))
		if f := strings.Fields(cell); len(f) >= 2 && strings.HasPrefix(f[1], "T") {
			items++
		}
	}
	if items != 10 {
		t.Errorf("counted %d Thread item rows in the list, want 10", items)
	}
	mustContain(t, out, "1–10 of 11", "the list's scroll note")

	m2 := newPRModel(t)
	run(t, m2, 100, 24, "idle", 100*time.Millisecond)
	if h := m2.navInner().H; h < 8 {
		t.Errorf("navigator has %d rows at 100×24, want ≥ 8", h)
	}
	if h := m2.panelInner().H; h < 3 {
		t.Errorf("list pane has %d rows at 100×24, want ≥ 3", h)
	}
}

// 14. } opens the next file in reading order; J moves to the next Touched
// Method with its header rule near the top.
func TestScenario14_FilesAndMethods(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 120, 40, "idle", 50*time.Millisecond)
	order := m.sess.FileOrder()
	// From the Briefing, { does nothing and } opens the first file.
	run(t, m, 120, 40, "tab {", 50*time.Millisecond)
	if m.centreView != ViewBriefing {
		t.Fatalf("{ on the Briefing should do nothing")
	}
	run(t, m, 120, 40, "}", 50*time.Millisecond)
	if m.filePath != order[0] || order[0] != "internal/bounce/webhooks/beacon.go" {
		t.Fatalf("} on the Briefing opened %q, want the first file %q", m.filePath, order[0])
	}
	out := run(t, m, 120, 40, "J", 50*time.Millisecond)
	mustContain(t, out, "── NewBeacon ─", "NewBeacon's header rule")
	out = run(t, m, 120, 40, "J", 50*time.Millisecond)
	mustContain(t, out, "── (*Beacon).ProcessHandshake ─", "the next method's header rule")
	_, y := findXY(t, out, "── (*Beacon).ProcessHandshake")
	if top := m.centreInner().Y; y-top >= 5 {
		t.Errorf("header rule is %d rows down the centre, want within the top 5", y-top)
	}
	run(t, m, 120, 40, "K", 50*time.Millisecond)
	if r := m.centreRows[m.centreCursor]; r.Anchor.Ref != "method:"+m.sess.MethodsByFile[m.filePath][0] {
		t.Errorf("K should go back to NewBeacon's block, cursor on %q", r.text())
	}
	run(t, m, 120, 40, "}", 50*time.Millisecond)
	if m.filePath != order[1] {
		t.Errorf("} opened %q, want the next file %q", m.filePath, order[1])
	}
	run(t, m, 120, 40, "{", 50*time.Millisecond)
	if m.filePath != order[0] {
		t.Errorf("{ opened %q, want %q", m.filePath, order[0])
	}
}

// 15. / searches the centre view: n/M in the footer, n advances, esc clears,
// smart case.
func TestScenario15_Search(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 120, 40, "idle j j enter", 100*time.Millisecond)
	out := run(t, m, 120, 40, `/ "Beacon"`, 50*time.Millisecond)
	if !m.search.prompt || len(m.search.matches) == 0 {
		t.Fatalf("typing /Beacon should highlight matches incrementally")
	}
	mustContain(t, out, "/Beacon", "the prompt in the footer")
	out = run(t, m, 120, 40, "enter", 50*time.Millisecond)
	total := len(m.search.matches)
	footer := ansi.Strip(strings.Split(out, "\n")[39])
	want := "1/" + strconv.Itoa(total)
	if !strings.Contains(footer, "/Beacon") || !strings.Contains(footer, want) {
		t.Errorf("footer = %q, want /Beacon and %s", footer, want)
	}
	if !strings.Contains(out, "249;226;175") && !strings.Contains(out, "249:226:175") {
		t.Errorf("no current-match highlight (#f9e2af) in the frame")
	}
	row := m.search.matches[0].row
	if !strings.Contains(m.centreRows[row].text(), "Beacon") {
		t.Errorf("match row %q doesn't contain Beacon", m.centreRows[row].text())
	}
	out = run(t, m, 120, 40, "n", 50*time.Millisecond)
	mustContain(t, out, "2/"+strconv.Itoa(total), "n advances")
	if off := m.centreCursor - m.search.matches[1].row; off != 0 {
		t.Errorf("n should put the cursor on the match row")
	}
	out = run(t, m, 120, 40, "N N", 50*time.Millisecond)
	mustContain(t, out, strconv.Itoa(total)+"/"+strconv.Itoa(total), "N wraps to the last match")
	out = run(t, m, 120, 40, "esc", 50*time.Millisecond)
	if m.search.on() {
		t.Errorf("esc should clear the search")
	}
	mustNotContain(t, out, "/Beacon", "footer after esc")

	// Smart case: lower case matches both cases.
	run(t, m, 120, 40, `/ "beacon" enter`, 50*time.Millisecond)
	if n := len(m.search.matches); n <= total {
		t.Errorf("/beacon found %d matches, want more than /Beacon's %d", n, total)
	}
	// esc inside the prompt restores where the view was.
	run(t, m, 120, 40, "esc g", 50*time.Millisecond)
	s0, c0 := m.centreScroll, m.centreCursor
	run(t, m, 120, 40, `/ "mapBeaconStatus" esc`, 50*time.Millisecond)
	if m.centreScroll != s0 || m.centreCursor != c0 || m.search.on() {
		t.Errorf("esc in the prompt should restore the view and clear the search")
	}
	// Opening another file clears it.
	run(t, m, 120, 40, `/ "Beacon" enter }`, 50*time.Millisecond)
	if m.search.on() {
		t.Errorf("opening another file should clear the search")
	}
}

// 16. Back / Forward over jumps.
func TestScenario16_BackForward(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 120, 40, "idle", 50*time.Millisecond)
	m.centreScroll = centreRowOf(t, m, "Call map")
	out := run(t, m, 120, 40, "", 50*time.Millisecond)
	briefScroll := m.centreScroll
	x, y := findXY(t, out, "App.BounceWebhook")
	run(t, m, 120, 40, "click:"+xy(x+2, y), 100*time.Millisecond)
	if m.centreView != ViewFile || m.filePath != "cmd/bounce.go" {
		t.Fatalf("clicking the call map should open cmd/bounce.go, got %v %q", m.centreView, m.filePath)
	}
	fileScroll := m.centreScroll
	out = run(t, m, 120, 40, "backward:"+xy(60, 10), 50*time.Millisecond)
	if m.centreView != ViewBriefing || m.centreScroll != briefScroll {
		t.Errorf("backward should restore the Briefing at scroll %d, got view %v scroll %d", briefScroll, m.centreView, m.centreScroll)
	}
	mustContain(t, out, "Call map", "the Briefing's call map is back on screen")
	run(t, m, 120, 40, "forward:"+xy(5, 5), 50*time.Millisecond)
	if m.centreView != ViewFile || m.filePath != "cmd/bounce.go" || m.centreScroll != fileScroll {
		t.Errorf("forward should return to cmd/bounce.go at %d, got %v %q %d", fileScroll, m.centreView, m.filePath, m.centreScroll)
	}
	out = run(t, m, 120, 40, "forward:"+xy(5, 5), 50*time.Millisecond)
	mustContain(t, out, "Forward: nothing later", "flash at the end")
	run(t, m, 120, 40, "ctrl+o", 50*time.Millisecond)
	if m.centreView != ViewBriefing || m.centreScroll != briefScroll {
		t.Errorf("ctrl+o should go back to the Briefing")
	}
	out = run(t, m, 120, 40, "ctrl+o", 50*time.Millisecond)
	mustContain(t, out, "Back: nothing earlier", "flash at the start")
	// Scrolling and hunk steps are not jumps.
	run(t, m, 120, 40, "} ] ] j j", 50*time.Millisecond)
	if n := len(m.back); n != 1 {
		t.Errorf("} is one jump; ] and j aren't: back stack has %d entries, want 1", n)
	}
}

// 17. Dragging the borders resizes the panes.
func TestScenario17_Resize(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 120, 40, "idle", 50*time.Millisecond)
	nav0, _, panel0 := m.layout()
	bx := nav0.W - 1
	run(t, m, 120, 40, "drag:"+xy(bx, 10)+"-"+xy(bx+10, 10), 50*time.Millisecond)
	if nav, _, _ := m.layout(); nav.W != nav0.W+10 {
		t.Errorf("left column is %d wide after a 10-col drag, want %d", nav.W, nav0.W+10)
	}
	if m.sel.Active {
		t.Errorf("dragging a border must not start a selection")
	}
	// Horizontal: the navigator's bottom edge, 3 rows up: the list grows.
	hy := nav0.Y + nav0.H - 1
	run(t, m, 120, 40, "drag:"+xy(5, hy)+"-"+xy(5, hy-3), 50*time.Millisecond)
	if _, _, p := m.layout(); p.H != panel0.H+3 {
		t.Errorf("list pane is %d tall after dragging up 3, want %d", p.H, panel0.H+3)
	}
	if in := m.panelInner(); in.H != listItems+3 {
		t.Errorf("list shows %d rows, want %d", in.H, listItems+3)
	}
	// Double-click resets each border.
	nav1, _, _ := m.layout()
	run(t, m, 120, 40, "wait:450ms dclick:"+xy(nav1.W, 20), 50*time.Millisecond)
	if nav, _, _ := m.layout(); nav.W != nav0.W {
		t.Errorf("double-click should reset the left column to %d, got %d", nav0.W, nav.W)
	}
	nav2, _, _ := m.layout()
	run(t, m, 120, 40, "wait:450ms dclick:"+xy(5, nav2.Y+nav2.H-1), 50*time.Millisecond)
	if _, _, p := m.layout(); p.H != panel0.H {
		t.Errorf("double-click should reset the list to %d rows, got %d", panel0.H, p.H)
	}
	// Past a minimum it stops there.
	run(t, m, 120, 40, "wait:450ms drag:"+xy(nav0.W, 10)+"-"+xy(2, 10), 50*time.Millisecond)
	if nav, _, _ := m.layout(); nav.W != minLeftW {
		t.Errorf("left column dragged to %d, want the minimum %d", nav.W, minLeftW)
	}
	out := run(t, m, 120, 40, "drag:"+xy(minLeftW-1, 10)+"-"+xy(115, 10), 50*time.Millisecond)
	if _, c, _ := m.layout(); c.W != minCentreW {
		t.Errorf("centre is %d wide, want the minimum %d", c.W, minCentreW)
	}
	checkFrameFit(t, out, 120, 40)
	nav3, _, _ := m.layout()
	run(t, m, 120, 40, "drag:"+xy(3, nav3.Y+nav3.H-1)+"-"+xy(3, 38), 50*time.Millisecond)
	if in := m.panelInner(); in.H != minListItems {
		t.Errorf("list dragged to %d rows, want the minimum %d", in.H, minListItems)
	}
	// A terminal resize keeps the user's sizes, clamped.
	out = run(t, m, 100, 30, "", 50*time.Millisecond)
	if _, c, _ := m.layout(); c.W < minCentreW {
		t.Errorf("after resize the centre is %d wide, below the minimum", c.W)
	}
	if in := m.panelInner(); in.H != minListItems {
		t.Errorf("after resize the list has %d rows, want the user's %d", in.H, minListItems)
	}
	checkFrameFit(t, out, 100, 30)
}

// 18. A Question on a two-line Anchor doesn't break the frame.
func TestScenario18_MultiLineAnchor(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	out := run(t, m, 120, 40, "idle G enter", 100*time.Millisecond)
	mustContain(t, out, "docs/swagger/collections.yaml", "the file is open")
	navRows := func(frame string) []string {
		nav, _, _ := m.layout()
		var rows []string
		for i, l := range strings.Split(ansi.Strip(frame), "\n") {
			if i < nav.Y || i >= nav.Y+nav.H {
				continue
			}
			r := []rune(l)
			rows = append(rows, string(r[:minInt(len(r), nav.W)]))
		}
		return rows
	}
	before := navRows(out)
	x, y := findXY(t, out, "schema:")
	out = run(t, m, 120, 40, "drag:"+xy(x, y)+"-"+xy(x+14, y+1)+" c z", 200*time.Millisecond)
	var q *session.Thread
	for _, th := range m.sess.Threads {
		if th.ID == "T8" {
			q = th
		}
	}
	if q == nil || !strings.Contains(q.Anchor.Text, "\n") {
		t.Fatalf("expected T8 anchored on two lines, got %+v", q)
	}
	if strings.Contains(q.Entries[0].Text, "\n") {
		t.Errorf("the Question's quote kept a newline: %q", q.Entries[0].Text)
	}
	mustContain(t, out, "What tests cover “schema: type:", "the one-line quote in the expanded card")
	after := navRows(out)
	for i := range before {
		if strings.Contains(before[i], "collections.yaml") {
			continue // gains the ●1 marker
		}
		if i >= len(after) || before[i] != after[i] {
			t.Errorf("navigator row %d changed:\n  before %q\n  after  %q", i, before[i], after[i])
		}
	}
}

// 19. e on a Feedback card edits it in place: the view doesn't move.
func TestScenario19_EditInPlace(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j enter", 100*time.Millisecond)
	card := centreRowOf(t, m, "F1  Feedback")
	m.centreScroll = card - 12
	m.centreCursor = card
	run(t, m, 140, 45, "", 50*time.Millisecond)
	s0 := m.centreScroll
	out := run(t, m, 140, 45, "e", 50*time.Millisecond)
	if m.fbEdit == nil || m.fbEdit.fb.ID != "F1" {
		t.Fatalf("e should open F1's editor")
	}
	if m.centreScroll != s0 {
		t.Errorf("e moved the view: scroll %d → %d", s0, m.centreScroll)
	}
	mustContain(t, out, "F1 · Feedback · editing", "the editor, in place")
	run(t, m, 140, 45, "esc", 50*time.Millisecond)

	// From the list with the card off-screen: the minimum scroll that shows
	// the whole editor, not a jump to the top.
	m.centreScroll, m.centreCursor = 0, 0
	run(t, m, 140, 45, "tab 2", 50*time.Millisecond)
	for i, r := range m.panelRows {
		if r.Feedback != nil && r.Feedback.ID == "F1" {
			m.panelCursor = i
		}
	}
	run(t, m, 140, 45, "e", 50*time.Millisecond)
	first, last := -1, -1
	for i, r := range m.centreRows {
		if r.Feedback != nil && r.Feedback.ID == "F1" {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	h := m.centreH()
	if first < m.centreScroll || last >= m.centreScroll+h {
		t.Errorf("editor rows %d–%d not in view %d–%d", first, last, m.centreScroll, m.centreScroll+h-1)
	}
	if last != m.centreScroll+h-1 {
		t.Errorf("expected the minimum scroll (editor's last row on the pane's last row), got scroll %d for rows %d–%d", m.centreScroll, first, last)
	}
}

// Frame fit for the core views at two terminal sizes: the method
// block, search highlights, a hovered and a dragged border, a Back.
func TestFrameFit(t *testing.T) {
	t.Parallel()
	for _, sz := range []struct{ w, h int }{{100, 30}, {200, 50}} {
		m := newPRModel(t)
		run(t, m, sz.w, sz.h, "idle j j enter J J", 50*time.Millisecond)
		out := run(t, m, sz.w, sz.h, `/ "Beacon" enter n`, 50*time.Millisecond)
		mustContain(t, out, "/Beacon", "search footer")
		nav, _, _ := m.layout()
		out = run(t, m, sz.w, sz.h, "move:"+xy(nav.W, 5), 50*time.Millisecond)
		mustContain(t, out, "┃", "hovered border drawn heavy")
		out = run(t, m, sz.w, sz.h, "move:"+xy(nav.W+20, 5), 50*time.Millisecond)
		mustNotContain(t, out, "┃", "hover ends off the border")
		run(t, m, sz.w, sz.h, "drag:"+xy(nav.W, 5)+"-"+xy(nav.W+15, 5), 50*time.Millisecond)
		run(t, m, sz.w, sz.h, "drag:"+xy(3, nav.Y+nav.H-1)+"-"+xy(3, nav.Y+nav.H-4), 50*time.Millisecond)
		run(t, m, sz.w, sz.h, "ctrl+o ctrl+o esc", 50*time.Millisecond)
	}
}
