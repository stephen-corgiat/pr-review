package ui

// The threads list: stable order, search, navigation, changes and help spacing.

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

func listThreadIDs(m *Model) []string {
	var ids []string
	for _, r := range m.panelRows {
		if r.Thread != nil {
			ids = append(ids, r.Thread.ID)
		}
	}
	return ids
}

// 30. Opening a Thread (marking it read) doesn't move its row.
func TestScenario30_StableThreadsList(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle tab tab 1", 50*time.Millisecond)
	before := listThreadIDs(m)
	if strings.Join(before, " ") != "T1 T2 T3 T4 T5 T6 T7" {
		t.Fatalf("the list should be in creation order, got %v", before)
	}
	for i, r := range m.panelRows {
		if r.Thread != nil && r.Thread.ID == "T1" {
			m.panelCursor = i
		}
	}
	t1 := thread(t, m, "T1")
	if !t1.Unread {
		t.Fatalf("T1 should start ● new")
	}
	glyph0, _ := statusGlyph(t1)
	run(t, m, 140, 45, "enter", 50*time.Millisecond)
	m.focus = FocusPanel
	out := run(t, m, 140, 45, "", 50*time.Millisecond)
	if after := listThreadIDs(m); strings.Join(after, " ") != strings.Join(before, " ") {
		t.Errorf("order changed: %v → %v", before, after)
	}
	if glyph, _ := statusGlyph(t1); glyph == glyph0 || t1.Unread {
		t.Errorf("T1's icon should change once read (was %s, now %s)", glyph0, glyph)
	}
	if r := m.panelRows[m.panelCursor]; r.Thread != t1 {
		t.Errorf("the list cursor left T1")
	}
	mustContain(t, out, "✓ T1", "T1's row shows it read")
}

// 31. p is the previous match while a search is on. Otherwise plain p is
// unbound (the Probe dialog is gone).
func TestScenario31_SearchP(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j enter", 50*time.Millisecond)
	out := run(t, m, 140, 45, `/ "Beacon" enter n n p`, 50*time.Millisecond)
	n := len(m.search.matches)
	if m.search.cur != 1 || n < 3 {
		t.Fatalf("after n n p the current match should be 2, got %d of %d", m.search.cur+1, n)
	}
	mustContain(t, out, "/Beacon  2/", "footer at 2/N")
	mustContain(t, out, "N/p previous", "footer hint")
	if m.overlay != OverlayNone {
		t.Fatalf("p during a search must not open a dialog")
	}
	run(t, m, 140, 45, "esc", 50*time.Millisecond)
	t3 := thread(t, m, "T3")
	cursorOn(m, rowWhere(t, m, "T3's card", func(r Row) bool { return r.Thread == t3 }))
	before := len(m.sess.Threads)
	run(t, m, 140, 45, "p", 50*time.Millisecond)
	if m.overlay != OverlayNone || len(m.sess.Threads) != before {
		t.Fatalf("plain p should do nothing outside a search")
	}
}

// 32. alt+left / alt+right are Back / Forward.
func TestScenario32_AltArrows(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 120, 40, "idle", 50*time.Millisecond)
	m.centreScroll = centreRowOf(t, m, "Call map")
	out := run(t, m, 120, 40, "", 50*time.Millisecond)
	briefScroll := m.centreScroll
	x, y := findXY(t, out, "App.BounceWebhook")
	run(t, m, 120, 40, "click:"+xy(x+2, y), 100*time.Millisecond)
	if m.centreView != ViewFile || m.filePath != "cmd/bounce.go" {
		t.Fatalf("the call map should open cmd/bounce.go")
	}
	fileScroll := m.centreScroll
	run(t, m, 120, 40, "alt+left", 50*time.Millisecond)
	if m.centreView != ViewBriefing || m.centreScroll != briefScroll {
		t.Errorf("alt+left should restore the Briefing")
	}
	run(t, m, 120, 40, "alt+right", 50*time.Millisecond)
	if m.centreView != ViewFile || m.filePath != "cmd/bounce.go" || m.centreScroll != fileScroll {
		t.Errorf("alt+right should return to the file")
	}
	out = run(t, m, 120, 40, "", 50*time.Millisecond)
	mustContain(t, out, "ctrl+o/alt+← back", "footer hint")
}

// here is the new-side line of the cursor's stop (the method's func line
// area for a header rule: the method key instead).
func stopHere(m *Model) (line int, method string) {
	r := m.centreRows[m.centreCursor]
	if r.Anchor.Type == session.AnchorBriefing && strings.HasPrefix(r.Anchor.Ref, "method:") {
		return 0, strings.TrimPrefix(r.Anchor.Ref, "method:")
	}
	return lineNoOf(m.sess.Files[m.filePath], r.DiffRow), ""
}

// 33. ] walks beacon.go's top-level declarations, then crosses files.
func TestScenario33_ChangesInBeacon(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j enter", 50*time.Millisecond)
	beacon := "internal/bounce/webhooks/beacon.go"
	var methods []string
	var lines []int
	backs := len(m.back)
	for i := 0; i < 30 && m.filePath == beacon; i++ {
		run(t, m, 140, 45, "]", 30*time.Millisecond)
		if m.filePath != beacon {
			break
		}
		if off := m.centreCursor - m.centreScroll; off > 4 {
			t.Errorf("stop landed %d rows down; want near the top", off)
		}
		l, meth := stopHere(m)
		if meth != "" {
			methods = append(methods, meth[strings.Index(meth, "::")+2:])
		} else {
			lines = append(lines, l)
		}
	}
	want := []string{"NewBeacon", "(*Beacon).ProcessHandshake", "(*Beacon).ProcessBounce", "(*Beacon).verifyAuth", "secretsEqual", "parseBeaconEvents", "mapBeaconStatus"}
	if strings.Join(methods, " ") != strings.Join(want, " ") {
		t.Errorf("method stops = %v, want %v", methods, want)
	}
	// The type and var declarations are stops too (the Beacon and beaconEvent types, smtpCode and softPhrases).
	if len(lines) < 4 {
		t.Errorf("expected the var/type declarations as stops, got lines %v", lines)
	}
	if len(m.back) != backs+1 {
		t.Errorf("only the crossing is a jump: back stack grew by %d", len(m.back)-backs)
	}
	if m.filePath != "cmd/bounce.go" {
		t.Fatalf("] at the last stop should open the next file in reading order, got %q", m.filePath)
	}
	if !strings.HasPrefix(m.flash, "→ ") {
		t.Errorf("crossing should flash →, got %q", m.flash)
	}
}

// 34. ] in cmd/bounce.go: the new case, then the next file; [ comes back to it.
func TestScenario34_ChangesInBounce(t *testing.T) {
	t.Parallel()
	m := newPRModel(t)
	run(t, m, 140, 45, "idle j j j enter", 50*time.Millisecond)
	if m.filePath != "cmd/bounce.go" {
		t.Fatalf("expected cmd/bounce.go open, got %q", m.filePath)
	}
	run(t, m, 140, 45, "]", 30*time.Millisecond)
	if l, _ := stopHere(m); l != 45 || m.filePath != "cmd/bounce.go" {
		t.Fatalf("first ] should land on line 45, got %d in %s", l, m.filePath)
	}
	out := run(t, m, 140, 45, "]", 30*time.Millisecond)
	if m.filePath == "cmd/bounce.go" {
		t.Fatalf("second ] should cross to the next file")
	}
	mustContain(t, out, "→ ", "crossing flash")
	run(t, m, 140, 45, "[", 30*time.Millisecond)
	if l, _ := stopHere(m); l != 45 || m.filePath != "cmd/bounce.go" {
		t.Errorf("[ should come back to line 45, got %d in %s", l, m.filePath)
	}
	// The PR's ends.
	m.openFile(m.sess.FileOrder()[0])
	m.dirty = true
	out = run(t, m, 140, 45, "g [", 30*time.Millisecond)
	mustContain(t, out, "first change", "flash at the first change")
	// From the Briefing, ] opens the first file's first stop.
	m.centreView = ViewBriefing
	m.dirty = true
	run(t, m, 140, 45, "]", 30*time.Millisecond)
	if m.centreView != ViewFile || m.filePath != m.sess.FileOrder()[0] {
		t.Errorf("] in the Briefing should open the first file")
	}
}

// 35. Help: every description starts ≥ 3 columns after its key; it fits.
func TestScenario35_HelpSpacing(t *testing.T) {
	t.Parallel()
	for _, iw := range []int{92, 106} {
		body := helpBody(iw)
		for _, sec := range helpSections {
			for _, e := range sec.entries {
				found := false
				for _, l := range body {
					p := ansi.Strip(l)
					if !strings.HasPrefix(p, "  "+e[0]) {
						continue
					}
					first := strings.Fields(e[1])[0]
					di := strings.Index(p[len("  "+e[0]):], first)
					if di < 0 {
						continue
					}
					found = true
					if di < helpGap {
						t.Errorf("%q: description starts %d cols after the key", e[0], di)
					}
				}
				if !found {
					t.Errorf("help entry %q not found", e[0])
				}
			}
		}
	}
	for _, sz := range []struct{ w, h int }{{100, 30}, {200, 50}} {
		m := newPRModel(t)
		out := run(t, m, sz.w, sz.h, "idle ?", 50*time.Millisecond)
		mustContain(t, out, "Keys", "help open")
		mustContain(t, out, "esc or ? closes", "the footer line is on screen, not clipped")
		// The help got longer (ran, cost, auto mode): scroll further.
		run(t, m, sz.w, sz.h, strings.Repeat("j ", 40), 50*time.Millisecond)
		out = run(t, m, sz.w, sz.h, "", 50*time.Millisecond)
		mustContain(t, out, "computed locally", "scrolling reaches the legend")
	}
}
