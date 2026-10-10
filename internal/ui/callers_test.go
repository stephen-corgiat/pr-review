package ui

// Exact Go callers. The fixture PR (dispatchly #214) with a stand-in
// analysis whose result the tests choose, so every state of the callers
// row can be shown: running, a count, none, unavailable, and a failure.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/gocallers"
	"github.com/stephen-corgiat/pr-review/internal/fixturerepo"
)

const (
	beacon        = "internal/bounce/webhooks/beacon.go"
	keyNewBeacon  = beacon + "::NewBeacon"
	keyProcess    = beacon + "::(*Beacon).ProcessBounce"
	keyVerifyAuth = beacon + "::(*Beacon).verifyAuth"
	keyMapStatus  = beacon + "::mapBeaconStatus"
)

// stubCallers is the analysis the tests control: it waits for release
// (when set), then returns res or err.
type stubCallers struct {
	release chan struct{}
	res     gocallers.Result
	err     error
}

func (s *stubCallers) run(ctx context.Context, _ string, ms []gocallers.Method) (gocallers.Result, error) {
	if s.release != nil {
		select {
		case <-s.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if s.err != nil {
		return nil, s.err
	}
	out := gocallers.Result{}
	for _, m := range ms {
		if c, ok := s.res[m.Key]; ok {
			out[m.Key] = c
		} else {
			out[m.Key] = gocallers.Callers{Callers: []gocallers.Caller{{Func: "main.main", Path: "cmd/main.go", Line: 1}}}
		}
	}
	return out, nil
}

// processCallers: 22 direct callers (outside the PR, 2 in tests, 1 a
// reference) and 3 through an interface: 25, so 5 are past the first 20.
func processCallers() gocallers.Callers {
	var cs []gocallers.Caller
	cs = append(cs, gocallers.Caller{Func: "(*App).BounceWebhook", Path: "cmd/bounce.go", Line: 58})
	for i := 0; i < 19; i++ {
		cs = append(cs, gocallers.Caller{Func: fmt.Sprintf("main.handler%02d", i), Path: "cmd/main.go", Line: 20 + i})
	}
	cs = append(cs,
		gocallers.Caller{Func: "webhooks.TestProcess", Path: "internal/bounce/webhooks/beacon_test.go", Line: 12, Test: true},
		gocallers.Caller{Func: "webhooks.TestBatch", Path: "internal/bounce/webhooks/beacon_test.go", Line: 30, Test: true, Ref: true},
	)
	for i := 0; i < 3; i++ {
		cs = append(cs, gocallers.Caller{Func: fmt.Sprintf("bounce.route%d", i), Path: "internal/bounce/bounce.go", Line: 40 + i, Via: "bounce.Processor"})
	}
	return gocallers.Callers{Callers: cs}
}

func stubResult() gocallers.Result {
	return gocallers.Result{
		keyNewBeacon:  {Callers: []gocallers.Caller{{Func: "bounce.New", Path: "internal/bounce/bounce.go", Line: 57}}},
		keyProcess:    processCallers(),
		keyVerifyAuth: {},
		keyMapStatus:  {Unavailable: beacon + ":12: undefined: foo"},
	}
}

// newCallersModel opens the fixture PR with the stub analysis (no cache).
func newCallersModel(t *testing.T, s *stubCallers) *Model {
	t.Helper()
	eng, _, err := app.Fixture{FixturesDir: fixtureDir, CloneDir: fixturerepo.CacheParent(), Callers: s.run}.Open()
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(eng.Close)
	return New(eng, false)
}

// callersRow puts the cursor on a method's callers row and returns the
// frame.
func callersRow(t *testing.T, m *Model, key string, w, h int) string {
	t.Helper()
	m.jumpToMethod(key)
	m.focus = FocusCentre
	return run(t, m, w, h, "j", 30*time.Millisecond)
}

// 57. While the analysis runs, Go methods say "callers …"; when it lands
// they say how many, and the Briefing lists the count. TypeScript and Ruby
// rows are unchanged (scenarios 55 and 56).
func TestScenario57_CallersRunningThenCounted(t *testing.T) {
	s := &stubCallers{release: make(chan struct{}), res: stubResult()}
	m := newCallersModel(t, s)
	run(t, m, 140, 45, "wait:1600ms", 50*time.Millisecond)
	out := callersRow(t, m, keyNewBeacon, 140, 45)
	mustContain(t, out, "↑ callers  New   callers …", "running")
	out = callersRow(t, m, "cmd/bounce.go::(*App).BounceWebhook", 140, 45)
	mustContain(t, out, "callers …", "running on another method")

	close(s.release)
	time.Sleep(100 * time.Millisecond)
	out = run(t, m, 140, 45, "", 100*time.Millisecond)
	mustContain(t, out, "↑ callers  ▸ 1 caller", "BounceWebhook's count")
	out = callersRow(t, m, keyNewBeacon, 140, 45)
	mustContain(t, out, "↑ callers  New   ▸ 1 caller", "NewBeacon's count")
	out = callersRow(t, m, keyProcess, 140, 45)
	// The Touched Methods among them stay listed by name, before the count.
	mustContain(t, out, "↑ callers  BounceWebhook   ▸ 25 callers · 2 in tests · 3 via interface", "ProcessBounce's breakdown")

	m.centreView = ViewBriefing
	m.dirty = true
	run(t, m, 140, 45, "", 30*time.Millisecond)
	m.centreScroll = centreRowOf(t, m, "Touched Methods")
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "Beacon.ProcessBounce  added  · 25 callers", "the Briefing's count")
	mustContain(t, out, "NewBeacon  added  · 1 caller", "the Briefing's count")
}

// 58. The other states: none, unavailable, and a whole-analysis failure.
func TestScenario58_CallersNoneAndUnavailable(t *testing.T) {
	m := newCallersModel(t, &stubCallers{res: stubResult()})
	run(t, m, 140, 45, "wait:300ms", 50*time.Millisecond)
	out := callersRow(t, m, keyVerifyAuth, 140, 45)
	mustContain(t, out, "↑ callers  ProcessBounce   no callers in this module", "none")
	out = callersRow(t, m, keyMapStatus, 140, 45)
	mustContain(t, out, "callers unavailable: internal/bounce/webhooks/beacon.go:12: undefined: foo", "unavailable")
	// enter on a row with nothing to expand does nothing.
	before := len(m.centreRows)
	run(t, m, 140, 45, "enter", 30*time.Millisecond)
	if len(m.centreRows) != before {
		t.Errorf("enter on an unavailable callers row changed the rows")
	}

	f := newCallersModel(t, &stubCallers{err: errors.New("the analysis crashed: boom")})
	run(t, f, 140, 45, "wait:300ms", 50*time.Millisecond)
	out = callersRow(t, f, keyNewBeacon, 140, 45)
	mustContain(t, out, "callers unavailable: the analysis crashed: boom", "a failure")
}

// 59. enter on the callers row expands the list in place (direct callers,
// then a sub-row per interface), "… N more" past 20 expands the rest, and
// enter again collapses it.
func TestScenario59_ExpandCallers(t *testing.T) {
	m := newCallersModel(t, &stubCallers{res: stubResult()})
	run(t, m, 140, 45, "wait:300ms", 50*time.Millisecond)
	callersRow(t, m, keyProcess, 140, 45)
	rows0 := len(m.centreRows)
	out := run(t, m, 140, 45, "enter", 30*time.Millisecond)
	mustContain(t, out, "↑ callers  BounceWebhook   ▾ 25 callers", "the arrow turns")
	mustContain(t, out, "(*App).BounceWebhook  cmd/bounce.go:58", "a caller row")
	mustContain(t, out, "main.handler00  cmd/main.go:20", "a caller outside the PR")
	text := m.rowsText()
	for _, want := range []string{"main.handler18  cmd/main.go:38", "… 5 more"} {
		if !strings.Contains(text, want) {
			t.Errorf("expanded list lacks %q", want)
		}
	}
	// The first 20: the tests and the interface group wait.
	if strings.Contains(text, "via interface bounce.Processor") || strings.Contains(text, "webhooks.TestProcess") {
		t.Errorf("only the first 20 callers show:\n%s", text)
	}
	if got := len(m.centreRows) - rows0; got != 21 {
		t.Errorf("expanding added %d rows, want 20 callers + … more", got)
	}

	// … 5 more: the rest, with the interface's sub-row.
	m.centreCursor = centreRowOf(t, m, "… 5 more")
	run(t, m, 140, 45, "enter", 30*time.Millisecond)
	text = m.rowsText()
	for _, want := range []string{
		"webhooks.TestProcess  internal/bounce/webhooks/beacon_test.go:12  test",
		"webhooks.TestBatch  internal/bounce/webhooks/beacon_test.go:30  test ref",
		"via interface bounce.Processor  3",
		"bounce.route2  internal/bounce/bounce.go:42",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("after … more, the list lacks %q", want)
		}
	}
	if strings.Contains(text, "… 5 more") {
		t.Errorf("the … more row stays")
	}
	if via, r := centreRowOf(t, m, "via interface bounce.Processor"), centreRowOf(t, m, "bounce.route0"); r != via+1 {
		t.Errorf("the interface's callers sit under its sub-row")
	}

	// enter on the callers row again collapses it.
	m.centreCursor = centreRowOf(t, m, "▾ 25 callers")
	out = run(t, m, 140, 45, "enter", 30*time.Millisecond)
	mustContain(t, out, "▸ 25 callers", "collapsed")
	if len(m.centreRows) != rows0 {
		t.Errorf("collapsed: %d rows, want %d", len(m.centreRows), rows0)
	}

	// A click on the callers row expands it too.
	x, y := findXY(t, out, "▸ 25 callers")
	out = run(t, m, 140, 45, "click:"+xy(x+4, y), 450*time.Millisecond)
	mustContain(t, out, "▾ 25 callers", "a click expands")
}

// 60. enter on a caller jumps to its line, in the PR's diff or, for a file
// outside the PR, read-only at the head; Back returns to the method.
func TestScenario60_JumpToCallerAndBack(t *testing.T) {
	m := newCallersModel(t, &stubCallers{res: stubResult()})
	run(t, m, 140, 45, "wait:300ms", 50*time.Millisecond)
	callersRow(t, m, keyNewBeacon, 140, 45)
	run(t, m, 140, 45, "enter j", 30*time.Millisecond)
	from := m.curLoc()
	out := run(t, m, 140, 45, "enter", 50*time.Millisecond)
	if m.centreView != ViewFile || m.filePath != "internal/bounce/bounce.go" {
		t.Fatalf("enter on bounce.New should open internal/bounce/bounce.go, got %v %q", m.centreView, m.filePath)
	}
	if r := m.centreRows[m.centreCursor]; r.DiffRow < 0 || m.sess.Files[m.filePath].All[r.DiffRow].NewNo != 57 {
		t.Errorf("the cursor should be on line 57")
	}
	mustContain(t, out, "bounce/bounce.go", "the file's title")
	run(t, m, 140, 45, "ctrl+o", 50*time.Millisecond)
	if m.curLoc() != from {
		t.Errorf("Back: %+v, want %+v", m.curLoc(), from)
	}

	// A caller outside the PR: cmd/main.go at the head, read-only.
	callersRow(t, m, keyProcess, 140, 45)
	run(t, m, 140, 45, "enter", 30*time.Millisecond)
	m.centreCursor = centreRowOf(t, m, "main.handler05")
	from = m.curLoc()
	out = run(t, m, 140, 45, "enter", 50*time.Millisecond)
	if m.centreView != ViewOutside || m.filePath != "cmd/main.go" {
		t.Fatalf("enter on a caller outside the PR should open it, got %v %q", m.centreView, m.filePath)
	}
	mustContain(t, out, "cmd/main.go  not in this PR · read-only at "+fixtureRepo(t).C2[:7]+"", "the outside file's title")
	if r := m.centreRows[m.centreCursor]; r.OutLine != 25 {
		t.Errorf("the cursor is on line %d, want 25", r.OutLine)
	}
	mustContain(t, out, "25 ", "line 25 shows")
	// No Anchors on a file outside the PR.
	out = run(t, m, 140, 45, "d", 50*time.Millisecond)
	if m.overlay != OverlayNone {
		t.Errorf("d on a file outside the PR opened a dialog")
	}
	run(t, m, 140, 45, "ctrl+o", 50*time.Millisecond)
	if m.curLoc() != from {
		t.Errorf("Back from the outside file: %+v, want %+v", m.curLoc(), from)
	}
	run(t, m, 140, 45, "alt+right", 50*time.Millisecond)
	if m.centreView != ViewOutside || m.filePath != "cmd/main.go" {
		t.Errorf("Forward returns to the outside file, got %v %q", m.centreView, m.filePath)
	}
}

func TestCallersFrameFit(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{100, 30}, {200, 50}} {
		s := &stubCallers{release: make(chan struct{}), res: stubResult()}
		m := newCallersModel(t, s)
		run(t, m, sz.w, sz.h, "wait:300ms", 50*time.Millisecond)
		callersRow(t, m, keyProcess, sz.w, sz.h) // running
		close(s.release)
		time.Sleep(50 * time.Millisecond)
		run(t, m, sz.w, sz.h, "", 50*time.Millisecond)
		callersRow(t, m, keyMapStatus, sz.w, sz.h) // unavailable
		callersRow(t, m, keyProcess, sz.w, sz.h)
		run(t, m, sz.w, sz.h, "enter", 30*time.Millisecond) // expanded
		m.centreCursor = centreRowOf(t, m, "main.handler05")
		run(t, m, sz.w, sz.h, "enter", 50*time.Millisecond) // outside the PR
		run(t, m, sz.w, sz.h, "ctrl+o ?", 50*time.Millisecond)
	}
}

// rowsText is the centre rows' text, one per line.
func (m *Model) rowsText() string {
	var b strings.Builder
	for _, r := range m.centreRows {
		b.WriteString(r.text())
		b.WriteString("\n")
	}
	return b.String()
}
