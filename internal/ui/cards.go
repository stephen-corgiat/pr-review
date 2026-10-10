package ui

import (
	"fmt"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

// ---- Thread status presentation ----

func statusGlyph(t *session.Thread) (string, lg.Style) {
	switch {
	case t.Status == session.StatusFailed:
		return "!", stNeedsYou
	case t.Unread:
		return "●", stNew
	case t.Status == session.StatusRunning:
		return "◐", stRunning
	case t.Status == session.StatusQueued:
		return "…", stRunning
	case t.Status == session.StatusDraft:
		return "○", stDraft
	}
	return "✓", stRead
}

func cardColor(t *session.Thread) string {
	switch {
	case t.Status == session.StatusFailed:
		return cRed
	case t.Unread:
		return cBlue
	case t.Status == session.StatusRunning || t.Status == session.StatusQueued:
		return cYellow
	case t.Status == session.StatusDraft:
		return cSubtext0
	}
	return cSurface2
}

// threadMarker returns the count and the style of the most urgent Thread.
func threadMarker(ts []*session.Thread) (int, lg.Style) {
	if len(ts) == 0 {
		return 0, lg.NewStyle()
	}
	rank := func(t *session.Thread) int {
		switch {
		case t.Status == session.StatusFailed:
			return 0
		case t.Unread:
			return 1
		case t.Status == session.StatusRunning || t.Status == session.StatusQueued:
			return 2
		case t.Status == session.StatusDraft:
			return 3
		}
		return 4
	}
	best := ts[0]
	for _, t := range ts[1:] {
		if rank(t) < rank(best) {
			best = t
		}
	}
	_, st := statusGlyph(best)
	if best.Status == session.StatusFailed {
		st = stNeedsYou
	}
	return len(ts), st
}

func badgeFor(e session.Entry) string {
	if e.Verified() {
		return "✅"
	}
	switch e.Kind {
	case session.SampleData, session.BeforeAfter, session.Research:
		return "📖"
	}
	return ""
}

func threadBadge(t *session.Thread) string {
	if idx := t.LatestAnswerIdx(); idx >= 0 {
		return badgeFor(t.Entries[idx])
	}
	return ""
}

// threadExcerpt is what the Thread is about: the anchored text, else the
// Question.
func threadExcerpt(t *session.Thread) string {
	if s := strings.TrimSpace(oneLine(t.Anchor.Text)); s != "" {
		return s
	}
	return oneLine(t.Entries[0].Text)
}

// ---- inline cards ----

func cardWidth(paneW int, indent string) int {
	return clamp(paneW-cellWidth(indent)-1, 24, 110)
}

func (m *Model) threadCardRows(t *session.Thread, indent string, paneW int) []Row {
	rows := m.threadCardRows0(t, indent, paneW)
	keys := map[string]func(*Model) tea.Cmd{}
	if idx := traceAnswerIdx(t); idx >= 0 {
		// x on a Trace Answer: sample this path.
		keys["x"] = func(mm *Model) tea.Cmd { return mm.sampleThisPath(t, idx) }
	}
	if t.Stale() {
		keys["Q"] = func(mm *Model) tea.Cmd { return mm.reask(t) }
	}
	if len(keys) > 0 {
		for i := range rows {
			if rows[i].Thread == t {
				rows[i].Keys = keys
			}
		}
	}
	return rows
}

func (m *Model) threadCardRows0(t *session.Thread, indent string, paneW int) []Row {
	cw := cardWidth(paneW, indent)
	rowW := cellWidth(indent) + cw
	glyph, gst := statusGlyph(t)
	base := Row{Kind: RowCard, DiffRow: -1, Thread: t, Width: rowW}

	if !m.expandedCards[t.ID] {
		head := []Span{sp("▸ ", stDim), sp(glyph+" ", gst), sp(t.ID, stBold), sp("  "+t.Kind.String(), stSub)}
		tail := []Span{sp("  "+t.Status.String(), gst)}
		if b := threadBadge(t); b != "" {
			tail = append(tail, sp(" "+b, lg.NewStyle()))
		}
		if t.Stale() {
			tail = append(tail, sp("  ⚠ Stale", stStale))
		}
		if t.Unread {
			tail = append(tail, sp("  ● new", stNew))
		}
		room := cw - 1 - cellWidth(spansText(head)) - cellWidth(spansText(tail)) - 4
		ex := []Span{sp("  “"+truncEllipsis(threadExcerpt(t), maxInt(3, room))+"”", stText)}
		r := base
		r.Prefix, r.BG = indent, bgCard
		r.Spans = append(append(append([]Span{sp(" ", lg.NewStyle())}, head...), ex...), tail...)
		r.Activate = func(mm *Model) tea.Cmd {
			mm.expandedCards[t.ID] = true
			mm.markRead(t)
			return nil
		}
		return []Row{r}
	}

	bst := fg(cardColor(t))
	label := []Span{sp("▾ ", stDim), sp(glyph+" ", gst), sp(t.ID, stBold), sp(" · "+t.Kind.String()+" · ", stSub), sp(t.Status.String(), gst)}
	if b := threadBadge(t); b != "" {
		label = append(label, sp(" "+b, lg.NewStyle()))
	}
	rows := []Row{m.cardEdge(base, indent, bst, "╭─ ", label, "╮", cw, func(mm *Model) tea.Cmd {
		mm.expandedCards[t.ID] = false
		return nil
	})}
	body := m.staleAnchorRows(t.Anchor, cw-4, Row{Kind: RowCard, DiffRow: -1, Thread: t})
	if len(body) > 0 {
		body = append(body, Row{Kind: RowCard, DiffRow: -1, Thread: t})
	}
	body = append(body, m.entryRows(t, cw-4)...)
	if t.Stale() {
		st := Row{Kind: RowCard, DiffRow: -1, Thread: t}
		body = append(body, st)
		body = append(body, textRows([]Span{sp("⚠ Stale", stStale.Bold(true)), sp(": its Anchor or Citations changed at the new head · ", stSub),
			sp("Q", stKey), sp(" asks it again at "+shortSHA(m.sess.PR.HeadSHA), stSub)}, cw-4, "", "  ", st)...)
	}
	for _, r := range body {
		r.Prefix = indent + bst.Render("│") + " " + r.Prefix
		r.Suffix = " " + bst.Render("│")
		r.Width = rowW
		rows = append(rows, r)
	}
	rows = append(rows, m.cardEdge(base, indent, bst, "╰", nil, "╯", cw, nil))
	m.markRead(t)
	// Threads asked about this Thread's Answers sit at the end of its card.
	for _, c := range m.sess.Threads {
		if c.Anchor.Type == session.AnchorAnswer && c.Anchor.ThreadID == t.ID {
			rows = append(rows, m.threadCardRows(c, indent+"  ", paneW)...)
		}
	}
	for _, f := range m.sess.Feedback {
		if f.Anchor.Type == session.AnchorAnswer && f.Anchor.ThreadID == t.ID {
			rows = append(rows, m.feedbackCardRows(f, indent+"  ", paneW)...)
		}
	}
	return rows
}

// cardEdge draws a card's top or bottom border: left, an optional label,
// then fill up to the right corner.
func (m *Model) cardEdge(base Row, indent string, bst lg.Style, left string, label []Span, right string, cw int, act func(*Model) tea.Cmd) Row {
	r := base
	r.Prefix = indent
	spans := []Span{sp(left, bst)}
	used := cellWidth(left) + cellWidth(right)
	if label != nil {
		lw := cellWidth(spansText(label))
		if used+lw+2 > cw {
			label = []Span{sp(truncEllipsis(spansText(label), maxInt(1, cw-used-2)), stSub)}
			lw = cellWidth(spansText(label))
		}
		spans = append(spans, label...)
		used += lw
		spans = append(spans, sp(" ", lg.NewStyle()))
		used++
	}
	spans = append(spans, sp(strings.Repeat("─", maxInt(0, cw-used))+right, bst))
	r.Spans = spans
	r.Activate = act
	return r
}

func spansText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// entryRows renders a Thread's entries at width w (no border): Questions,
// Answers with their markdown, Test Cases, Citations and what the agent
// ran, and notes. Used by expanded cards and the panel's Thread tab.
func (m *Model) entryRows(t *session.Thread, w int) []Row {
	var rows []Row
	for i, e := range t.Entries {
		tmpl := Row{Kind: RowCard, DiffRow: -1, Selectable: true, Thread: t,
			Anchor: session.Anchor{Type: session.AnchorAnswer, ThreadID: t.ID, Entry: i, Text: ""}}
		if i > 0 && e.Role != session.RoleNote {
			rows = append(rows, Row{Kind: RowCard, DiffRow: -1, Thread: t})
		}
		switch e.Role {
		case session.RoleQuestion:
			rows = append(rows, textRows([]Span{sp("Q  ", stAccent), sp(e.Text, stText)}, w, "", "   ", tmpl)...)
		case session.RoleNote:
			rows = append(rows, textRows([]Span{sp("·  "+e.Text, stDim.Italic(true))}, w, "", "   ", tmpl)...)
		case session.RoleAnswer:
			head := []Span{sp("A  ", stAccent)}
			if b := badgeFor(e); b != "" {
				head = append(head, sp(b+" ", lg.NewStyle()))
			}
			if e.Verified() {
				head = append(head, sp("Verified", stVerified), sp(" · the agent ran something that confirms it", stDim))
			} else {
				head = append(head, sp("Unverified", stSub), sp(" · from reading the code", stDim))
			}
			if e.Kind != t.Kind {
				head = append(head, sp(" · "+e.Kind.String(), stSub))
			}
			if e.Stale {
				head = append(head, sp(" · Stale", stStale))
			}
			if e.Unread {
				head = append(head, sp("  ● new", stNew))
			}
			hr := tmpl
			hr.Selectable = false
			rows = append(rows, textRows(head, w, "", "   ", hr)...)
			if e.A != nil {
				rows = append(rows, m.answerRows(t, i, w, tmpl)...)
			}
			if e.A != nil && len(e.A.Ran) > 0 {
				rows = append(rows, m.ranRows(e.A.Ran, w, tmpl)...)
			}
		}
	}
	// What's happening now.
	st := Row{Kind: RowCard, DiffRow: -1, Thread: t}
	switch {
	case t.Status == session.StatusRunning:
		rows = append(rows, st, textRow(st, []Span{sp("◐  Agent answering…", stRunning.Italic(true))}))
	case t.Status == session.StatusQueued:
		rows = append(rows, st, textRow(st, []Span{sp("…  Queued", stRunning.Italic(true)), sp(" · X cancels", stDim)}))
	case t.Status == session.StatusDraft:
		rows = append(rows, st, textRow(st, []Span{sp("○  Draft", stDraft.Italic(true)), sp(" · H sends all Drafts · X deletes", stDim)}))
	}
	return rows
}

// ranRows lists what the agent ran for an Answer: each command's first
// line and its one-line result.
func (m *Model) ranRows(ran []session.Ran, w int, tmpl Row) []Row {
	var rows []Row
	hr := tmpl
	hr.Selectable = false
	rows = append(rows, textRow(hr, []Span{sp("   ran", stDim.Bold(true))}))
	for _, r := range ran {
		spans := []Span{sp("$ "+firstLine(r.Command), fg(cPeach))}
		if res := oneLine(r.Result); res != "" {
			spans = append(spans, sp("  → ", stDim), sp(res, stSub))
		}
		rows = append(rows, textRows(spans, w, "     ", "       ", tmpl)...)
	}
	return rows
}

func textRow(tmpl Row, spans []Span) Row {
	tmpl.Spans = spans
	return tmpl
}

func (m *Model) citationRows(c session.Citation, w int, tmpl Row) []Row {
	var loc string
	switch {
	case c.URL != "":
		loc = c.URL
	case c.EndLine != 0:
		loc = fmt.Sprintf("%s:%d–%d", c.Path, c.Line, c.EndLine)
	default:
		loc = fmt.Sprintf("%s:%d", c.Path, c.Line)
	}
	spans := []Span{sp("↳ ", stDim), sp(loc, stLink)}
	if c.Note != "" {
		spans = append(spans, sp("  "+c.Note, stSub))
	}
	r := tmpl
	if c.Path != "" {
		cc := c
		act := func(mm *Model) tea.Cmd { return mm.jumpToCitation(cc) }
		r.Links = []Link{{Start: 2, End: 2 + len([]rune(loc)), Act: act}}
	}
	return textRows(spans, w, "   ", "     ", r)
}

func (m *Model) testCaseRows(tcs []session.TestCase, w int, tmpl Row) []Row {
	if len(tcs) == 0 {
		return nil
	}
	caseW, locW := 4, 8
	for _, tc := range tcs {
		caseW = maxInt(caseW, cellWidth(tc.Case))
		locW = maxInt(locW, cellWidth(tc.Location))
	}
	caseW, locW = minInt(caseW, 30), minInt(locW, maxInt(20, w-caseW-16))
	pad := func(s string, n int) string { return padTo(truncEllipsis(s, n), n) }
	hr := tmpl
	hr.Selectable = false
	rows := []Row{textRow(hr, []Span{sp("   "+pad("Test case", caseW)+"  "+pad("Location", locW)+"  Change", stDim.Bold(true))})}
	for _, tc := range tcs {
		chSt := stDim
		switch tc.Change {
		case "added":
			chSt = stAdded
		case "changed":
			chSt = stRunning
		}
		r := tmpl
		path, line := splitLoc(tc.Location)
		if path != "" {
			act := func(mm *Model) tea.Cmd { return mm.jumpToCitation(session.Citation{Path: path, Line: line}) }
			start := 3 + caseW + 2
			r.Links = []Link{{Start: start, End: start + cellWidth(truncEllipsis(tc.Location, locW)), Act: act}}
		}
		r.Spans = []Span{sp("   "+pad(tc.Case, caseW)+"  ", stBold), sp(pad(tc.Location, locW), stLink), sp("  "+tc.Change, chSt)}
		rows = append(rows, r)
		if tc.Description != "" {
			rows = append(rows, textRows([]Span{sp(tc.Description, stSub)}, w, "     ", "     ", tmpl)...)
		}
	}
	return rows
}

func splitLoc(loc string) (string, int) {
	i := strings.LastIndexByte(loc, ':')
	if i < 0 {
		return "", 0
	}
	n, err := strconv.Atoi(strings.SplitN(loc[i+1:], "-", 2)[0])
	if err != nil {
		return "", 0
	}
	return loc[:i], n
}
