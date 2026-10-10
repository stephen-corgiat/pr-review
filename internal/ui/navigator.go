package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

// isExpanded reports whether a nav file entry shows its Touched Methods.
func (m *Model) isExpanded(path string) bool {
	if v, ok := m.navExpanded[path]; ok {
		return v
	}
	return m.centreView == ViewFile && m.filePath == path
}

func (m *Model) buildNavEntries() {
	m.navEntries = m.navEntries[:0]
	m.navEntries = append(m.navEntries, navEntry{Kind: "briefing", Label: "Briefing"}, navEntry{Kind: "description", Label: "Description"})
	if m.sess.PrevHead != "" {
		m.navEntries = append(m.navEntries, navEntry{Kind: "since", Label: "Since last review"})
	}
	for _, p := range m.sess.FileOrder() {
		if m.sess.Files[p] == nil {
			continue
		}
		m.navEntries = append(m.navEntries, navEntry{Kind: "file", Path: p, Label: shortenPath(p)})
		if m.isExpanded(p) {
			for _, mk := range m.sess.MethodsByFile[p] {
				m.navEntries = append(m.navEntries, navEntry{Kind: "method", Path: p, Method: mk, Label: m.sess.Methods[mk].DisplayName()})
			}
		}
	}
	m.navCursor = clamp(m.navCursor, 0, len(m.navEntries)-1)
}

func (m *Model) threadsForFile(path string) []*session.Thread {
	var out []*session.Thread
	for _, t := range m.sess.Threads {
		if t.Anchor.Path == path && (t.Anchor.Type == session.AnchorDiff || t.Anchor.Type == session.AnchorFile) {
			out = append(out, t)
		}
	}
	return out
}

func (m *Model) threadsOfType(types ...session.AnchorType) []*session.Thread {
	var out []*session.Thread
	for _, t := range m.sess.Threads {
		for _, ty := range types {
			if t.Anchor.Type == ty {
				out = append(out, t)
			}
		}
	}
	return out
}

func (m *Model) threadsForMethod(key string) []*session.Thread {
	meth := m.sess.Methods[key]
	if meth == nil {
		return nil
	}
	var out []*session.Thread
	for _, t := range m.sess.Threads {
		switch {
		case t.Anchor.Type == session.AnchorBriefing && t.Anchor.Ref == "method:"+key:
			out = append(out, t)
		case t.Anchor.Type == session.AnchorDiff && t.Anchor.Path == meth.Path:
			if mm := m.sess.MethodAt(meth.Path, t.Anchor.RowEnd); mm != nil && mm.Key == key {
				out = append(out, t)
			}
		}
	}
	return out
}

func (m *Model) feedbackForFile(path string) []*session.Feedback {
	var out []*session.Feedback
	for _, f := range m.sess.Feedback {
		if f.Anchor.Path == path {
			out = append(out, f)
		}
	}
	return out
}

// navMarkers is the compact " ●2✎1" suffix of a navigator entry.
func navMarkers(ts []*session.Thread, fbs []*session.Feedback) []Span {
	out := []Span{}
	if n, st := threadMarker(ts); n > 0 {
		out = append(out, sp(fmt.Sprintf("●%d", n), st))
	}
	if len(fbs) > 0 {
		out = append(out, sp(fmt.Sprintf("✎%d", len(fbs)), stFeedback))
	}
	if len(out) > 0 {
		out = append([]Span{sp(" ", stDim)}, out...)
	}
	return out
}

func (m *Model) navH() int { return maxInt(1, m.navInner().H) }

func (m *Model) navFollow() {
	h := m.navH()
	if m.navCursor < m.navScroll {
		m.navScroll = m.navCursor
	}
	if m.navCursor >= m.navScroll+h {
		m.navScroll = m.navCursor - h + 1
	}
	m.navScroll = clamp(m.navScroll, 0, len(m.navEntries)-h)
}

func (m *Model) navIsOpen(e navEntry) bool {
	switch e.Kind {
	case "briefing":
		return m.centreView == ViewBriefing
	case "description":
		return m.centreView == ViewDescription
	case "since":
		return m.centreView == ViewSince
	case "file":
		return m.centreView == ViewFile && m.filePath == e.Path
	}
	return false
}

func (m *Model) renderNavBox(r rect) []string {
	in := r.inner()
	iw := in.W
	m.navScroll = clamp(m.navScroll, 0, len(m.navEntries)-in.H)
	var lines []string
	for i := m.navScroll; i < len(m.navEntries) && i < m.navScroll+in.H; i++ {
		e := m.navEntries[i]
		open := m.navIsOpen(e)
		nameSt := stText
		if open {
			nameSt = stAccent
		}
		var label string
		var marks []Span
		switch e.Kind {
		case "briefing":
			label = nameSt.Render("Briefing")
			if !m.sess.Briefing.Ready && !m.repoMode {
				label += stRunning.Render(" ◐")
			}
			marks = navMarkers(m.threadsOfType(session.AnchorBriefing), nil)
		case "description":
			label = nameSt.Render("Description")
			marks = navMarkers(m.threadsOfType(session.AnchorDescription, session.AnchorPR), nil)
		case "since":
			label = nameSt.Render("Since last review") + stStale.Render(fmt.Sprintf(" Δ%d", len(m.sess.Since)))
		case "file":
			fd := m.sess.Files[e.Path]
			arrow := "▸"
			if m.isExpanded(e.Path) {
				arrow = "▾"
			}
			if len(m.sess.MethodsByFile[e.Path]) == 0 {
				arrow = " "
			}
			if !open {
				nameSt = stSub
			}
			name := e.Label
			if fd != nil && fd.ParseErr != "" {
				name += " !"
			}
			marks = navMarkers(m.threadsForFile(e.Path), m.feedbackForFile(e.Path))
			if m.changedSince(e.Path) {
				marks = append([]Span{sp(" Δ", stStale)}, marks...)
			}
			room := iw - 4 - cellWidth(spansText(marks))
			label = stDim.Render(arrow) + " " + fileGlyph(fd) + " " + nameSt.Render(truncLeft(name, maxInt(3, room)))
		case "method":
			marks = navMarkers(m.threadsForMethod(e.Method), nil)
			if meth := m.sess.Methods[e.Method]; meth != nil && m.methodChangedSince(meth) {
				marks = append([]Span{sp(" Δ", stStale)}, marks...)
			}
			room := iw - 5 - cellWidth(spansText(marks))
			label = "    " + stMethod.Faint(true).Render("ƒ ") + stSub.Render(truncEllipsis(e.Label, maxInt(3, room)))
		}
		mk := renderSpans(marks, "", 0, 0, "", 0, 0)
		line := padTo(label, maxInt(0, iw-cellWidth(mk))) + mk
		if i == m.navCursor && m.focus == FocusNav {
			line = paintBG(line, bgCursor, iw)
		}
		lines = append(lines, line)
	}
	note := stDim.Render(fmt.Sprintf(" %d files ", len(m.sess.Files)))
	return drawBox(r, lines, " "+stHeading.Render("Review")+" ", note, m.focus == FocusNav)
}

// navActivate opens the highlighted entry in the centre.
func (m *Model) navActivate() tea.Cmd {
	if m.navCursor < 0 || m.navCursor >= len(m.navEntries) {
		return nil
	}
	e := m.navEntries[m.navCursor]
	m.markJump()
	switch e.Kind {
	case "briefing":
		if m.centreView != ViewBriefing {
			m.centreView, m.filePath = ViewBriefing, ""
			m.centreCursor, m.centreScroll = 0, 0
		}
	case "description":
		if m.centreView != ViewDescription {
			m.centreView, m.filePath = ViewDescription, ""
			m.centreCursor, m.centreScroll = 0, 0
		}
	case "since":
		return m.openSince()
	case "file":
		m.openFile(e.Path)
	case "method":
		m.jumpToMethod(e.Method)
	}
	m.sel = Selection{}
	m.dirty = true
	return nil
}

func (m *Model) navToggle() {
	if m.navCursor < 0 || m.navCursor >= len(m.navEntries) {
		return
	}
	if e := m.navEntries[m.navCursor]; e.Kind == "file" {
		m.navExpanded[e.Path] = !m.isExpanded(e.Path)
	}
}

// navAnchor is the Anchor for the highlighted entry (action keys with no
// selection).
func (m *Model) navAnchor() (session.Anchor, bool) {
	if m.navCursor < 0 || m.navCursor >= len(m.navEntries) {
		return session.Anchor{}, false
	}
	e := m.navEntries[m.navCursor]
	switch e.Kind {
	case "briefing":
		return session.Anchor{Type: session.AnchorBriefing, Ref: "tldr", Text: m.sess.Briefing.TLDR}, true
	case "description":
		return session.Anchor{Type: session.AnchorPR, Text: m.sess.PR.Title}, true
	case "file":
		return session.Anchor{Type: session.AnchorFile, Path: e.Path, Text: e.Path}, true
	case "method":
		meth := m.sess.Methods[e.Method]
		return session.Anchor{
			Type: session.AnchorDiff, Path: e.Path,
			RowStart: m.methodRowStart(meth), RowEnd: m.methodRowEnd(meth),
			Text: meth.DisplayName(),
		}, true
	}
	return session.Anchor{}, false
}

// methodRowStart is the diff row of the method's "func" line.
func (m *Model) methodRowStart(meth *session.Method) int {
	fd := m.sess.Files[meth.Path]
	if fd == nil {
		return 0
	}
	for i, l := range fd.All {
		if meth.NewDeclLine > 0 && l.NewNo == meth.NewDeclLine && l.Kind != session.LineRemoved {
			return i
		}
		if meth.NewDeclLine == 0 && meth.OldDeclLine > 0 && l.OldNo == meth.OldDeclLine {
			return i
		}
	}
	return 0
}

// methodBlockRow is the diff row the method starts on, doc comment
// included: its block sits above this row.
func (m *Model) methodBlockRow(meth *session.Method) int {
	fd := m.sess.Files[meth.Path]
	if fd == nil {
		return 0
	}
	for i, l := range fd.All {
		if meth.NewStart > 0 && l.NewNo == meth.NewStart && l.Kind != session.LineRemoved {
			return i
		}
		if meth.NewStart == 0 && meth.OldStart > 0 && l.OldNo == meth.OldStart {
			return i
		}
	}
	return m.methodRowStart(meth)
}

func (m *Model) methodRowEnd(meth *session.Method) int {
	fd := m.sess.Files[meth.Path]
	if fd == nil {
		return 0
	}
	for i, l := range fd.All {
		if meth.NewEnd > 0 && l.NewNo == meth.NewEnd && l.Kind != session.LineRemoved {
			return i
		}
		if meth.NewEnd == 0 && meth.OldEnd > 0 && l.OldNo == meth.OldEnd {
			return i
		}
	}
	return len(fd.All) - 1
}
