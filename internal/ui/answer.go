package ui

// An Answer in its new shape: the gist, the Kind's fields compactly, then a
// collapsed "▸ detail" row, then the Citations on one wrapped row.

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/highlight"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

var (
	bgCode    = "#1a1a28" // a Sample Data value's code block
	stInvent  = fg(cOverlay0).Italic(true)
	stHopFn   = fg(cMauve)
	stSrcTag  = fg(cTeal)
	stNoteTag = fg(cOverlay1)
)

// inline styles `code` spans in agent prose (the backticks are dropped).
func inline(text string, base lg.Style) []Span {
	parts := strings.Split(text, "`")
	if len(parts) == 1 || len(parts)%2 == 0 { // none, or unbalanced
		return []Span{sp(text, base)}
	}
	var out []Span
	for i, p := range parts {
		switch {
		case p == "":
		case i%2 == 1:
			out = append(out, sp(p, stCode))
		default:
			out = append(out, sp(p, base))
		}
	}
	return out
}

var stCode = fg(cPeach)

func detailKey(t *session.Thread, i int) string { return fmt.Sprintf("%s#%d", t.ID, i) }

// answerRows renders Answer entry i of t at width w.
func (m *Model) answerRows(t *session.Thread, i int, w int, tmpl Row) []Row {
	e := t.Entries[i]
	a := e.A
	var rows []Row
	body := func(spans []Span) { rows = append(rows, textRows(spans, w, "   ", "   ", tmpl)...) }
	gist := func() {
		if a.Kind == session.Define && a.Term != "" {
			body(append([]Span{sp(a.Term, stBold), sp("  ", stText)}, inline(a.Gist, stText)...))
			return
		}
		body(inline(a.Gist, stText))
	}
	fixed := tmpl
	fixed.Selectable = false

	switch {
	case len(a.Hops) > 0:
		rows = append(rows, m.hopRows(a.Hops, w, tmpl)...)
		rows = append(rows, textRow(fixed, nil))
		gist()
	default:
		gist()
	}

	switch {
	case a.Kind == session.Define && a.Example != "":
		rows = append(rows, textRows(append([]Span{sp("e.g. ", stDim)}, inline(a.Example, stDim)...), w, "   ", "        ", tmpl)...)
	case a.Changed != "":
		rows = append(rows, textRows(append([]Span{sp("changed: ", stDim)}, inline(a.Changed, stText)...), w, "   ", "            ", tmpl)...)
	case len(a.Steps) > 0:
		for _, st := range a.Steps {
			rows = append(rows, m.stepRows(st, "", w, tmpl)...)
		}
	case a.Before != nil:
		rows = append(rows, m.stepRows(*a.Before, "before", w, tmpl)...)
		if a.After != nil {
			rows = append(rows, m.stepRows(*a.After, "after", w, tmpl)...)
		}
		if a.Note != "" {
			rows = append(rows, textRows(append([]Span{sp("note: ", stDim)}, inline(a.Note, stSub)...), w, "   ", "         ", tmpl)...)
		}
	case a.Verdict != "":
		rows = append(rows, m.verdictRows(a, w, tmpl)...)
	}
	if len(a.Cases) > 0 {
		rows = append(rows, textRow(fixed, nil))
		rows = append(rows, m.testCaseRows(a.Cases, w, tmpl)...)
	} else if a.Kind == session.TestCases && a.HasKindData {
		rows = append(rows, textRow(fixed, []Span{sp("   no test cases found", stDim.Italic(true))}))
	}
	if len(a.Hops) > 0 {
		r := fixed
		tt, idx := t, i
		label := "[sample this path]"
		r.Spans = []Span{sp("   ", stDim), sp(label, fg(cTeal).Bold(true)), sp("  x · a Sample Data follow-up in this Thread", stDim)}
		act := func(mm *Model) tea.Cmd { return mm.sampleThisPath(tt, idx) }
		r.Links = []Link{{Start: 3, End: 3 + len([]rune(label)), Act: act}}
		r.Activate = act
		rows = append(rows, r)
	}

	// The detail, collapsed until asked for.
	if a.Detail != "" {
		key := detailKey(t, i)
		n := len(renderMarkdown(a.Detail, w-3))
		open := m.detailOpen[key]
		r := fixed
		arrow, label := "▸ ", fmt.Sprintf("detail (%d lines)", n)
		if open {
			arrow, label = "▾ ", "detail"
		}
		r.Spans = []Span{sp("   "+arrow, stDim), sp(label, fg(cLavender))}
		tt := t
		r.Activate = func(mm *Model) tea.Cmd {
			mm.detailOpen[key] = !mm.detailOpen[key]
			if mm.detailOpen[key] {
				mm.markRead(tt) // expanding counts as reading
			}
			return nil
		}
		r.Links = []Link{{Start: 3, End: 5 + len([]rune(label)), Act: r.Activate}}
		rows = append(rows, r)
		if open {
			rows = append(rows, markdownRows(a.Detail, w, "     ", tmpl)...)
		}
	}
	if len(a.Citations) > 0 {
		rows = append(rows, m.citationListRows("↳ ", a.Citations, w, tmpl)...)
	}
	return rows
}

// citeLoc is a Citation's short text: path:line or path:start-end.
func citeLoc(c session.Citation) string {
	switch {
	case c.URL != "":
		return c.URL
	case c.EndLine != 0:
		return fmt.Sprintf("%s:%d-%d", shortenPath(c.Path), c.Line, c.EndLine)
	case c.Line != 0:
		return fmt.Sprintf("%s:%d", shortenPath(c.Path), c.Line)
	}
	return shortenPath(c.Path)
}

// citationListRows is every Citation on one wrapped row, each clickable.
func (m *Model) citationListRows(lead string, cs []session.Citation, w int, tmpl Row) []Row {
	return m.citationListRowsIn("   ", lead, cs, w, tmpl)
}

func (m *Model) citationListRowsIn(indent, lead string, cs []session.Citation, w int, tmpl Row) []Row {
	spans := []Span{sp(lead, stDim)}
	var links []Link
	pos := len([]rune(lead))
	for i, c := range cs {
		if i > 0 {
			spans = append(spans, sp(" · ", stDim))
			pos += 3
		}
		loc := citeLoc(c)
		n := len([]rune(loc))
		spans = append(spans, sp(loc, stLink))
		if c.Path != "" {
			cc := c
			links = append(links, Link{Start: pos, End: pos + n, Act: func(mm *Model) tea.Cmd { return mm.jumpToCitation(cc) }})
		}
		pos += n
	}
	r := tmpl
	r.Links = links
	return textRows(spans, w, indent, indent+"  ", r)
}

// hopRows: one row per Trace hop, path:line · fn · text.
func (m *Model) hopRows(hops []session.Hop, w int, tmpl Row) []Row {
	var rows []Row
	locW := 0
	for _, h := range hops {
		locW = maxInt(locW, cellWidth(citeLoc(session.ParseCitation(h.At))))
	}
	locW = minInt(locW, maxInt(12, w/3))
	for i, h := range hops {
		c := session.ParseCitation(h.At)
		loc := truncEllipsis(citeLoc(c), locW)
		num := fmt.Sprintf("%d ", i+1)
		lead := "   " + num
		spans := []Span{sp(lead, stDim), sp(padTo(loc, locW), stLink), sp(" · ", stDim), sp(h.Fn, stHopFn), sp(" · ", stDim)}
		spans = append(spans, inline(h.Text, stText)...)
		r := tmpl
		if c.Path != "" {
			start := len([]rune(lead))
			r.Links = []Link{{Start: start, End: start + len([]rune(loc)), Act: func(mm *Model) tea.Cmd { return mm.jumpToCitation(c) }}}
		}
		rows = append(rows, textRows(spans, w, "", strings.Repeat(" ", cellWidth(lead)+locW+3), r)...)
	}
	return rows
}

// stepRows: "label  path:line  [invented | fixture ref]" then the value as a
// highlighted code block. Invented values are dimmed.
func (m *Model) stepRows(st session.SampleStep, side string, w int, tmpl Row) []Row {
	var rows []Row
	fixed := tmpl
	fixed.Selectable = false
	head := []Span{sp("   ▪ ", stDim)}
	pos := 5
	if side != "" {
		head = append(head, sp(side+" ", stSubheading))
		pos += len([]rune(side)) + 1
	}
	if st.Label != "" {
		head = append(head, sp(st.Label, stBold), sp("  ", stDim))
		pos += len([]rune(st.Label)) + 2
	}
	var links []Link
	if st.At != "" {
		c := session.ParseCitation(st.At)
		loc := citeLoc(c)
		head = append(head, sp(loc, stLink))
		if c.Path != "" {
			links = append(links, Link{Start: pos, End: pos + len([]rune(loc)), Act: func(mm *Model) tea.Cmd { return mm.jumpToCitation(c) }})
		}
		pos += len([]rune(loc))
	}
	switch st.Source {
	case "invented":
		head = append(head, sp("  invented", stInvent))
	case "":
	default:
		tag := "  " + st.Source
		head = append(head, sp(tag, stSrcTag))
		pos += len([]rune(tag))
		if st.SourceRef != "" {
			c := session.ParseCitation(st.SourceRef)
			loc := citeLoc(c)
			head = append(head, sp(" ", stDim), sp(loc, stLink))
			if c.Path != "" {
				links = append(links, Link{Start: pos + 1, End: pos + 1 + len([]rune(loc)), Act: func(mm *Model) tea.Cmd { return mm.jumpToCitation(c) }})
			}
		}
	}
	hr := tmpl
	hr.Links = links
	rows = append(rows, textRows(head, w, "", "       ", hr)...)

	// The value: chroma by lang, hard-wrapped, on a code background.
	hl := highlight.TokeniseLang(st.Lang, st.Value)
	cw := maxInt(8, w-7)
	for li := 1; li < len(hl.Lines); li++ {
		spans := expandTabs(hl.Lines[li])
		if st.Source == "invented" {
			dim := make([]Span, len(spans))
			for k, s := range spans {
				dim[k] = Span{Text: s.Text, Style: s.Style.Faint(true).Italic(true)}
			}
			spans = dim
		}
		for j, part := range hardWrapSpans(spans, cw) {
			r := tmpl
			r.Prefix = "     " + lg.NewStyle().Background(lg.Color(bgCode)).Render(" ")
			r.Spans = part
			r.BG = bgCode
			r.Width = 0
			r.Join = JoinLine
			if j > 0 {
				r.Join = JoinHard
			}
			rows = append(rows, r)
		}
	}
	return rows
}

// verdictRows: a coloured chip, the why, then the evidence as Citations.
func (m *Model) verdictRows(a *session.AnswerBody, w int, tmpl Row) []Row {
	color := cOverlay1
	switch a.Verdict {
	case "yes":
		color = cGreen
	case "no":
		color = cRed
	case "partly":
		color = cYellow
	}
	chip := lg.NewStyle().Foreground(lg.Color(cCrust)).Background(lg.Color(color)).Bold(true)
	rows := textRows(append([]Span{sp(" "+strings.ToUpper(a.Verdict)+" ", chip), sp("  ", stText)}, inline(a.Why, stSub)...), w, "   ", "   ", tmpl)
	if len(a.Evidence) > 0 {
		rows = append(rows, m.citationListRows("evidence: ", a.Evidence, w, tmpl)...)
	}
	return rows
}

// sampleThisPath asks a Sample Data follow-up in t's Thread, anchored on
// its Trace Answer (entry i).
func (m *Model) sampleThisPath(t *session.Thread, i int) tea.Cmd {
	if t.Busy() {
		return m.flashCmd(t.ID + " is busy: its last Question is still " + t.Status.String())
	}
	m.expandedCards[t.ID] = true
	cmd := m.do(engine.FollowUp{ThreadID: t.ID, Kind: session.SampleData, Text: session.SampleThisPathQuestion})
	// Show the new Question: it lands at the end of the card, often below
	// the pane, so without this the first press looked like it did nothing.
	m.rebuild()
	last := -1
	for k, r := range m.centreRows {
		if r.Thread == t && strings.HasPrefix(r.text(), "◐  Agent answering") || r.Thread == t && strings.HasPrefix(r.text(), "…  Queued") {
			last = k
		}
	}
	if last >= 0 && last >= m.centreScroll+m.centreH() {
		m.centreScroll = last - m.centreH() + 1
		m.clampCentre()
		m.centreCursor = clamp(m.centreCursor, m.centreScroll, m.centreScroll+m.centreH()-1)
	}
	m.dirty = true
	return tea.Batch(cmd, m.flashCmd(t.ID+": Sample Data follow-up asked (sample this path)"))
}

// traceAnswerIdx is the entry index of t's latest Trace Answer, or -1.
func traceAnswerIdx(t *session.Thread) int {
	if t == nil {
		return -1
	}
	for i := len(t.Entries) - 1; i >= 0; i-- {
		if e := t.Entries[i]; e.Role == session.RoleAnswer && e.A != nil && len(e.A.Hops) > 0 {
			return i
		}
	}
	return -1
}
