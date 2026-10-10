package ui

// The list pane: a short, fixed-height pane under the navigator listing
// every Thread (tab 1) or Feedback (tab 2), most urgent first. Picking an
// item jumps the centre to it. (Internally it's still "the panel".)

import (
	"fmt"
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

// listItems is how many items the list pane shows at once by default.
const listItems = 10

// Pane minimums (item rows / columns inside the borders, except widths).
const (
	minListItems = 3
	minNavRows   = 8
	minLeftW     = 20
	minCentreW   = 60
)

func (m *Model) panelH() int { return maxInt(1, m.panelInner().H) }

func (m *Model) buildPanelRows() {
	w := maxInt(10, m.panelInner().W)
	switch m.panelTab {
	case TabThreads:
		m.panelRows = m.buildThreadListRows(w)
	case TabFeedback:
		m.panelRows = m.buildFeedbackListRows(w)
	}
	m.panelCursor = clamp(m.panelCursor, 0, len(m.panelRows)-1)
	m.panelScroll = clamp(m.panelScroll, 0, len(m.panelRows)-m.panelH())
}

func (m *Model) panelFollow() {
	h := m.panelH()
	if m.panelCursor < m.panelScroll {
		m.panelScroll = m.panelCursor
	}
	if m.panelCursor >= m.panelScroll+h {
		m.panelScroll = m.panelCursor - h + 1
	}
	m.panelScroll = clamp(m.panelScroll, 0, len(m.panelRows)-h)
}

func (m *Model) panelItemRow(i int) bool {
	if i < 0 || i >= len(m.panelRows) {
		return false
	}
	r := m.panelRows[i]
	return r.Thread != nil || r.Feedback != nil
}

func (m *Model) movePanelCursor(d int) {
	if !m.panelItemRow(m.panelCursor + d) {
		return
	}
	m.panelCursor += d
	m.panelFollow()
}

// ---- locations ----

func (m *Model) anchorLineRange(a session.Anchor) (start, end int) {
	fd, ok := m.sess.Files[a.Path]
	if !ok {
		return 0, 0
	}
	return lineNoOf(fd, a.RowStart), lineNoOf(fd, a.RowEnd)
}

func (m *Model) anchorLocation(a session.Anchor) string { return m.anchorLoc(a, shortenPath, true) }

// anchorLocShort is the list pane's form: the file's base name and the
// Anchor's first line.
func (m *Model) anchorLocShort(a session.Anchor) string { return m.anchorLoc(a, path.Base, false) }

func (m *Model) anchorLoc(a session.Anchor, name func(string) string, ranges bool) string {
	switch a.Type {
	case session.AnchorDiff:
		s, e := m.anchorLineRange(a)
		switch {
		case ranges && s > 0 && e > s:
			return fmt.Sprintf("%s:%d–%d", name(a.Path), s, e)
		case s > 0:
			return fmt.Sprintf("%s:%d", name(a.Path), s)
		}
		return name(a.Path)
	case session.AnchorFile:
		return name(a.Path)
	case session.AnchorDescription:
		return "Description"
	case session.AnchorBriefing:
		ref := a.Ref
		if strings.HasPrefix(ref, "method:") {
			if meth := m.sess.Methods[strings.TrimPrefix(ref, "method:")]; meth != nil {
				ref = meth.DisplayName()
			}
		}
		if !ranges {
			return "Briefing"
		}
		return "Briefing · " + ref
	case session.AnchorPR:
		if a.Path != "" {
			return name(a.Path) + " (gone)"
		}
		return "Pull Request"
	case session.AnchorAnswer:
		return "in " + a.ThreadID
	case session.AnchorComment:
		return m.commentLoc(a)
	}
	return ""
}

// ---- rows ----

func threadRank(t *session.Thread) int {
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

func (m *Model) buildThreadListRows(w int) []Row {
	if len(m.sess.Threads) == 0 {
		return []Row{{Kind: RowText, DiffRow: -1, Prefix: " ", Spans: []Span{sp("No Threads yet", stDim)}}}
	}
	// Creation order (T1, T2, …): a status change only changes the icon,
	// so rows never move under the cursor.
	ts := m.sess.Threads
	var rows []Row
	for _, t := range ts {
		glyph, gst := statusGlyph(t)
		head := []Span{sp(" "+glyph+" ", gst), sp(padTo(t.ID, 4), stBold), sp(m.anchorLocShort(t.Anchor)+" ", fg(cLavender))}
		var tail []Span
		if t.Stale() {
			tail = append(tail, sp(" ⚠", stStale))
		}
		if b := threadBadge(t); b != "" {
			tail = append(tail, sp(" "+b, lg.NewStyle()))
		}
		room := w - cellWidth(spansText(head)) - cellWidth(spansText(tail))
		ex := ""
		if room > 4 {
			ex = truncEllipsis(threadExcerpt(t), room)
		}
		tt := t
		rows = append(rows, Row{Kind: RowText, DiffRow: -1, Thread: t,
			Spans:    append(append(head, sp(ex, stSub)), tail...),
			Activate: func(mm *Model) tea.Cmd { mm.openThread(tt); return nil }})
	}
	return rows
}

func (m *Model) buildFeedbackListRows(w int) []Row {
	if len(m.sess.Feedback) == 0 {
		return []Row{{Kind: RowText, DiffRow: -1, Prefix: " ", Spans: []Span{sp("No Feedback yet · f adds", stDim)}}}
	}
	var rows []Row
	for _, f := range m.sess.Feedback {
		fst := feedbackStyle(f)
		head := []Span{sp(" ✎ ", fst), sp(padTo(f.ID, 4), stBold), sp(m.anchorLocShort(f.Anchor)+" ", fg(cLavender))}
		if f.Anchor.Stale {
			head = append(head, sp("⚠ ", stStale))
		}
		switch {
		case f.Refine != session.RefineNone:
			head = append(head, sp("◐ ", stRunning))
		case f.Revision != nil && f.Revision.Stale:
			head = append(head, sp("✦ stale ", stNeedsYou))
		case f.Revision != nil:
			head = append(head, sp("✦ ", stRevision))
		}
		switch o := f.LastOutcome(); {
		case o != nil && f.Status == session.FeedbackHandedOff:
			// Handed off is implied: the Outcome says more.
			head = append(head, outcomeSpan(o), sp(" ", stDim))
		case f.Status != session.FeedbackDraft:
			head = append(head, sp(f.Status.String()+" ", fst))
		}
		if n := len(session.AgentNotes(f.Body)); n > 0 {
			head = append(head, sp(fmt.Sprintf("<<%d>> ", n), stNoteAccent))
		}
		room := w - cellWidth(spansText(head))
		ex := ""
		if room > 4 {
			ex = truncEllipsis(oneLine(f.Body), room)
		}
		ff := f
		rows = append(rows, Row{Kind: RowText, DiffRow: -1, Feedback: f,
			Spans:    append(head, sp(ex, stSub)),
			Activate: func(mm *Model) tea.Cmd { mm.openFeedback(ff); return nil }})
	}
	return rows
}

// ---- jumping to a Thread or Feedback ----

// openThread scrolls the centre to the Thread's card, expanded.
func (m *Model) openThread(t *session.Thread) {
	m.markRead(t)
	for p := t; p != nil; p = m.parentThread(p) {
		m.expandedCards[p.ID] = true
	}
	m.reveal(t.Anchor, func(r Row) bool { return r.Kind == RowCard && r.Thread == t })
}

// openFeedback scrolls the centre to the Feedback's card, expanded.
func (m *Model) openFeedback(f *session.Feedback) {
	m.expandedCards[fbKey(f)] = true
	if f.Anchor.Type == session.AnchorAnswer {
		if p := m.threadByID(f.Anchor.ThreadID); p != nil {
			for q := p; q != nil; q = m.parentThread(q) {
				m.expandedCards[q.ID] = true
			}
		}
	}
	m.reveal(f.Anchor, func(r Row) bool { return r.Kind == RowCard && r.Feedback == f })
}

func (m *Model) parentThread(t *session.Thread) *session.Thread {
	if t.Anchor.Type != session.AnchorAnswer {
		return nil
	}
	return m.threadByID(t.Anchor.ThreadID)
}

func (m *Model) threadByID(id string) *session.Thread {
	for _, t := range m.sess.Threads {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// reveal switches the centre to where anchor a lives and scrolls the first
// row isCard accepts near the top (the code line it's anchored on sits just
// above it).
func (m *Model) reveal(a session.Anchor, isCard func(Row) bool) {
	m.markJump()
	for a.Type == session.AnchorAnswer {
		p := m.threadByID(a.ThreadID)
		if p == nil {
			return
		}
		a = p.Anchor
	}
	switch a.Type {
	case session.AnchorDiff, session.AnchorFile:
		if _, ok := m.sess.Files[a.Path]; !ok {
			return
		}
		m.openFile(a.Path)
	case session.AnchorDescription, session.AnchorPR:
		if m.centreView != ViewDescription {
			m.centreView, m.centreCursor, m.centreScroll = ViewDescription, 0, 0
		}
	case session.AnchorBriefing:
		if m.centreView != ViewBriefing {
			m.centreView, m.centreCursor, m.centreScroll = ViewBriefing, 0, 0
		}
	case session.AnchorComment:
		m.revealComment(a.Ref)
		for i, r := range m.centreRows {
			if isCard(r) {
				m.revealRow(i)
				return
			}
		}
		return
	}
	m.rebuild()
	if a.Type == session.AnchorDiff && m.currentHunkMode(a.Path) && !m.hasDiffRow(a.RowEnd) {
		m.hunkMode[a.Path] = false
		m.rebuild()
	}
	for i, r := range m.centreRows {
		if isCard(r) {
			m.revealRow(i)
			return
		}
	}
	if a.Type == session.AnchorDiff {
		m.revealDiffRow(a.RowEnd)
	}
}

func (m *Model) hasDiffRow(d int) bool {
	for _, r := range m.centreRows {
		if r.DiffRow == d {
			return true
		}
	}
	return false
}

// ---- the pane ----

func (m *Model) renderPanelBox(r rect) []string {
	in := r.inner()
	var lines []string
	for i := m.panelScroll; i < len(m.panelRows) && i < m.panelScroll+in.H; i++ {
		cursor := i == m.panelCursor && m.focus == FocusPanel && m.panelItemRow(i)
		lines = append(lines, renderRow(m.panelRows[i], in.W, false, 0, 0, cursor))
	}

	// Tabs live in the top border; each is clickable.
	tabs := []struct {
		t     PanelTab
		label string
	}{
		{TabThreads, fmt.Sprintf("Threads %d", len(m.sess.Threads))},
		{TabFeedback, fmt.Sprintf("Feedback %d", len(m.sess.Feedback))},
	}
	var title strings.Builder
	x := r.X + 2
	for i, tb := range tabs {
		st := stSub
		if tb.t == m.panelTab {
			st = lg.NewStyle().Foreground(lg.Color(cCrust)).Background(lg.Color(cBlue)).Bold(true)
		}
		label := tb.label
		segW := cellWidth(label) + 2
		tt := tb.t
		m.zones = append(m.zones, zone{X0: x, X1: x + segW, Y: r.Y, Fn: func(mm *Model) tea.Cmd {
			mm.panelTab, mm.focus = tt, FocusPanel
			mm.panelCursor, mm.panelScroll = 0, 0
			mm.dirty = true
			return nil
		}})
		title.WriteString(st.Render(" " + label + " "))
		x += segW
		if i == 0 {
			title.WriteString(" ")
			x++
		}
	}
	note := ""
	if n := len(m.panelRows); n > in.H {
		note = stDim.Render(fmt.Sprintf(" %d–%d of %d ", m.panelScroll+1, minInt(n, m.panelScroll+in.H), n))
	} else if c := m.sess.Counts(); c.Drafts > 0 && m.panelTab == TabThreads {
		note = stDim.Render(fmt.Sprintf(" H sends %d drafts ", c.Drafts))
	}
	return drawBox(r, lines, title.String(), note, m.focus == FocusPanel)
}
