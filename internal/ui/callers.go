package ui

// Exact Go callers: the callers row of a Go Touched Method's block
// says how many callers the analysis found across its module; enter (or a
// click) expands the list in place, one caller per row, and enter on a
// caller jumps to it, the way a Citation does. A caller in a file outside
// the PR opens that file read-only at the head (ViewOutside).

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/gocallers"
	"github.com/stephen-corgiat/pr-review/internal/highlight"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// callersShown is how many callers an expanded list shows before its
// "… N more" row.
const callersShown = 20

// callersSummary is the callers row's text after the name-matched Touched
// Methods: "callers …", "12 callers · 3 in tests · 2 via interface", "no
// callers in this module" or "callers unavailable: <reason>". ok is false
// when the exact callers don't apply (not Go, removed, or not running
// here): the row stays as it was.
func (m *Model) callersSummary(meth *session.Method, w int) (spans []Span, expandable, ok bool) {
	c, st, ok := m.sess.MethodCallers(meth.Key)
	if !ok {
		return nil, false, false
	}
	n, tests, via := c.Counts()
	open := m.callersOpen[meth.Key]
	arrow := "▸ "
	if open {
		arrow = "▾ "
	}
	switch {
	case st == session.CallersRunning:
		return []Span{sp("callers …", stRunning.Italic(true))}, false, true
	case c.Unavailable != "":
		reason := truncEllipsis(oneLine(c.Unavailable), maxInt(20, w-40))
		spans = []Span{sp("callers unavailable: "+reason, stModified)}
		if n > 0 {
			spans = append([]Span{sp(arrow, stDim)}, append(spans, sp(fmt.Sprintf(" · %d found", n), stSub))...)
		}
		return spans, n > 0, true
	case n == 0:
		return []Span{sp("no callers in this module", stDim)}, false, true
	}
	text := fmt.Sprintf("%d caller%s", n, plural(n))
	if tests > 0 {
		text += fmt.Sprintf(" · %d in tests", tests)
	}
	if via > 0 {
		text += fmt.Sprintf(" · %d via interface", via)
	}
	return []Span{sp(arrow, stDim), sp(text, stSub)}, true, true
}

// toggleCallers expands or collapses a method's callers list.
func (m *Model) toggleCallers(key string) {
	m.callersOpen[key] = !m.callersOpen[key]
	m.dirty = true
}

// callerListRows are an expanded callers list, under the callers row in
// the method's band: direct callers first, then one sub-row per interface
// with its callers under it; past callersShown, a "… N more" row.
func (m *Model) callerListRows(meth *session.Method, band Row, w int) []Row {
	c, _, _ := m.sess.MethodCallers(meth.Key)
	var direct []gocallers.Caller
	var ifaces []string
	byIface := map[string][]gocallers.Caller{}
	for _, x := range c.Callers {
		if x.Via == "" {
			direct = append(direct, x)
			continue
		}
		if byIface[x.Via] == nil {
			ifaces = append(ifaces, x.Via)
		}
		byIface[x.Via] = append(byIface[x.Via], x)
	}
	all := m.callersAll[meth.Key]
	shown, total := 0, len(c.Callers)
	bg := band.BG
	indent := func(n int) string { return bgStyle(bg).Render(strings.Repeat(" ", n)) }
	row := band
	row.Selectable = false // a caller row isn't an Anchor (this pass)
	var rows []Row
	callerRow := func(x gocallers.Caller, depth int) {
		r := row
		xx := x
		r.Activate = func(mm *Model) tea.Cmd { return mm.jumpToCaller(xx.Path, xx.Line) }
		loc := fmt.Sprintf("%s:%d", x.Path, x.Line)
		spans := []Span{sp(x.Func, stText), sp("  ", stDim), sp(loc, stLink)}
		var marks []string
		if x.Test {
			marks = append(marks, "test")
		}
		if x.Ref {
			marks = append(marks, "ref")
		}
		if len(marks) > 0 {
			spans = append(spans, sp("  "+strings.Join(marks, " "), stDim))
		}
		rows = append(rows, textRows(spans, w, indent(depth), indent(depth+2), r)...)
		shown++
	}
	const base = 2 + 11 // under the labels' column
	for _, x := range direct {
		if !all && shown == callersShown {
			break
		}
		callerRow(x, base)
	}
	for _, iface := range ifaces {
		if !all && shown == callersShown {
			break
		}
		h := row
		cs := byIface[iface]
		rows = append(rows, textRows([]Span{sp("via interface ", stDim), sp(iface, stMethod), sp(fmt.Sprintf("  %d", len(cs)), stDim)}, w, indent(base), indent(base+2), h)...)
		for _, x := range cs {
			if !all && shown == callersShown {
				break
			}
			callerRow(x, base+2)
		}
	}
	if more := total - shown; more > 0 {
		r := row
		key := meth.Key
		r.Activate = func(mm *Model) tea.Cmd {
			mm.callersAll[key] = true
			mm.dirty = true
			return nil
		}
		rows = append(rows, textRows([]Span{sp(fmt.Sprintf("… %d more", more), stLink)}, w, indent(base), indent(base), r)...)
	}
	return rows
}

// jumpToCaller opens a caller's line: in the PR's diff, or read-only at
// the head when its file isn't in the PR. It's a jump for Back/Forward.
func (m *Model) jumpToCaller(path string, line int) tea.Cmd {
	m.focus = FocusCentre
	if _, ok := m.sess.Files[path]; ok {
		m.gotoLine(path, line)
		return nil
	}
	return m.openOutside(path, line)
}

// ---- files outside the PR ----

// outsideFile is a file outside the PR, at the head.
type outsideFile struct {
	hl    highlight.File
	lines int
}

// openOutside shows path (a file the PR doesn't change) read-only at the
// head, with line near the top.
func (m *Model) openOutside(path string, line int) tea.Cmd {
	if _, ok := m.outside[path]; !ok {
		content, ok := m.eng.HeadFile(path)
		if !ok {
			return m.flashCmd("Not in this PR, and not at the head: " + path)
		}
		m.outside[path] = outsideFile{hl: highlight.Tokenise(path, content), lines: strings.Count(strings.TrimSuffix(content, "\n"), "\n") + 1}
	}
	m.markJump()
	if m.centreView != ViewOutside || m.filePath != path {
		m.centreView, m.filePath = ViewOutside, path
		m.centreCursor, m.centreScroll = 0, 0
		m.sel = Selection{}
	}
	m.rebuild()
	for i, r := range m.centreRows {
		if r.OutLine == line && r.Join != JoinHard {
			m.revealRow(i)
			break
		}
	}
	return nil
}

// buildOutsideRows is a file outside the PR: every line, numbered, read
// only (no Anchors on it in this pass).
func (m *Model) buildOutsideRows(path string) []Row {
	f, ok := m.outside[path]
	if !ok {
		return nil
	}
	w := m.centreBodyW()
	numW := maxInt(3, digits(f.lines))
	var rows []Row
	for n := 1; n <= f.lines; n++ {
		var spans []Span
		if n < len(f.hl.Lines) {
			spans = expandTabs(f.hl.Lines[n])
		}
		gutter := stFaint.Render(fmt.Sprintf("%*d ", numW, n)) + " "
		cont := stFaint.Render(strings.Repeat(" ", numW)+"↪") + " "
		for i, p := range hardWrapSpans(spans, w-cellWidth(gutter)) {
			r := Row{Kind: RowCode, Prefix: gutter, Spans: p, DiffRow: -1, OutLine: n}
			if i > 0 {
				r.Prefix, r.Join = cont, JoinHard
			}
			rows = append(rows, r)
		}
	}
	return rows
}

func (m *Model) outsideTitle() string {
	return " " + stDim.Render("◦") + " " + stBold.Render(m.filePath) + stDim.Render("  not in this PR · read-only at "+shortSHA(m.sess.PR.HeadSHA)) + " "
}

// briefingCallers is the caller count after a Go method in the Briefing's
// Touched Methods list, once the analysis is ready.
func (m *Model) briefingCallers(key string) []Span {
	c, st, ok := m.sess.MethodCallers(key)
	if !ok || st != session.CallersOK {
		return nil
	}
	if c.Unavailable != "" {
		return []Span{sp("  · callers unavailable", stDim)}
	}
	n, _, _ := c.Counts()
	return []Span{sp(fmt.Sprintf("  · %d caller%s", n, plural(n)), stDim)}
}

func bgStyle(bg string) lg.Style { return lg.NewStyle().Background(lg.Color(bg)) }
