package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/highlight"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// centreBodyW is the width available to rows (after the marker column).
func (m *Model) centreBodyW() int { return maxInt(10, m.centreInner().W-1) }

func (m *Model) buildCentreRows() {
	switch m.centreView {
	case ViewFile:
		m.centreRows = m.buildFileRows(m.filePath)
	case ViewBriefing:
		m.centreRows = m.buildBriefingRows()
	case ViewDescription:
		m.centreRows = m.buildDescriptionRows()
	case ViewSince:
		m.centreRows = m.buildSinceRows()
	case ViewOutside:
		m.centreRows = m.buildOutsideRows(m.filePath)
	}
	m.clampCentre()
}

// ---- scrolling ----

func (m *Model) centreH() int { return maxInt(1, m.centreInner().H) }

func (m *Model) clampCentre() {
	n := len(m.centreRows)
	m.centreCursor = clamp(m.centreCursor, 0, n-1)
	m.centreScroll = clamp(m.centreScroll, 0, n-m.centreH())
}

// followCursor scrolls just enough to keep the cursor visible, with a
// couple of rows of context.
func (m *Model) followCursor() {
	h := m.centreH()
	off := minInt(2, (h-1)/2)
	if m.centreCursor < m.centreScroll+off {
		m.centreScroll = m.centreCursor - off
	}
	if m.centreCursor > m.centreScroll+h-1-off {
		m.centreScroll = m.centreCursor - h + 1 + off
	}
	m.clampCentre()
}

// revealRow puts the cursor on row i and scrolls so it sits near the top,
// with a few rows of context above: used for every jump.
func (m *Model) revealRow(i int) {
	m.centreCursor = i
	m.centreScroll = i - minInt(3, m.centreH()/4)
	m.clampCentre()
}

// keepViewport runs fn (which changes what rows exist) and then puts the
// code line under the cursor back on the same screen line.
func (m *Model) keepViewport(fn func()) {
	m.ensureRows()
	anchorDiff, offset, delta := -1, 0, 0
	for i := m.centreCursor; i >= 0 && i < len(m.centreRows); i-- {
		if r := m.centreRows[i]; r.DiffRow >= 0 && r.Join != JoinHard {
			anchorDiff, offset, delta = r.DiffRow, i-m.centreScroll, m.centreCursor-i
			break
		}
	}
	idx, scrollOff := m.centreCursor, m.centreCursor-m.centreScroll
	fn()
	m.rebuild()
	if anchorDiff >= 0 {
		for i, r := range m.centreRows {
			if r.DiffRow == anchorDiff && r.Join != JoinHard {
				// The cursor keeps its distance from that line (it may sit on a
				// method block or card just below it).
				m.centreCursor = i + delta
				m.centreScroll = i - offset
				m.clampCentre()
				return
			}
		}
	}
	m.centreCursor = idx
	m.centreScroll = idx - scrollOff
	m.clampCentre()
}

// ---- the centre pane ----

func (m *Model) renderCentreBox(r rect) []string {
	in := r.inner()
	rows := m.centreRows
	selOn := m.sel.Active && m.sel.Pane == FocusCentre
	r0, c0, r1, c1 := m.sel.Bounds()
	var lines []string
	for i := m.centreScroll; i < len(rows) && i < m.centreScroll+in.H; i++ {
		selS, selE := 0, 0
		if selOn && i >= r0 && i <= r1 {
			selS, selE = 0, 1<<30
			if i == r0 {
				selS = c0
			}
			if i == r1 {
				selE = c1
			}
		}
		cursor := i == m.centreCursor && m.focus == FocusCentre && !selOn
		lines = append(lines, renderRow(rows[i], in.W, true, selS, selE, cursor, m.rowSearchHL(i)...))
	}
	title, note := m.centreTitle()
	if n := len(rows); n > in.H {
		pct := 100
		if n-in.H > 0 {
			pct = m.centreScroll * 100 / (n - in.H)
		}
		note = stDim.Render(fmt.Sprintf(" %d%% ", pct))
	}
	return drawBox(r, lines, title, note, m.focus == FocusCentre)
}

func (m *Model) centreTitle() (string, string) {
	switch m.centreView {
	case ViewBriefing:
		t := " " + stHeading.Render("Briefing") + " "
		switch {
		case m.sess.Briefing.Stale:
			t += stStale.Render("Stale (from "+shortSHA(m.sess.Briefing.StaleFrom)+") · "+m.briefingPendingText()) + " "
		case !m.sess.Briefing.Ready:
			t += stRunning.Render(m.briefingPendingText()) + " "
		}
		return t, ""
	case ViewDescription:
		return " " + stHeading.Render("Description") + " ", ""
	case ViewOutside:
		return m.outsideTitle(), ""
	case ViewSince:
		return " " + stHeading.Render("Since last review") + stDim.Render(" "+shortSHA(m.sess.PrevHead)+" → "+shortSHA(m.sess.PR.HeadSHA)+" "), ""
	}
	fd := m.sess.Files[m.filePath]
	if fd == nil {
		return "", ""
	}
	mode := "hunks · E full file"
	if !m.currentHunkMode(m.filePath) {
		mode = "full file · E hunks"
	}
	t := " " + fileGlyph(fd) + " " + stBold.Render(m.filePath) + "  " +
		stAdded.Render(fmt.Sprintf("+%d", fd.Added)) + " " + stRemoved.Render(fmt.Sprintf("−%d", fd.Removed)) +
		stDim.Render("  "+mode) + " "
	return t, ""
}

func fileGlyph(fd *session.FileDiff) string {
	switch fd.Status {
	case session.FileAdded:
		return stAdded.Render("+")
	case session.FileDeleted:
		return stRemoved.Render("−")
	}
	return stModified.Render("~")
}

// ---- file diff rows ----

func (m *Model) currentHunkMode(path string) bool {
	if v, ok := m.hunkMode[path]; ok {
		return v
	}
	return true
}

func digits(n int) int {
	d := 1
	for n >= 10 {
		n /= 10
		d++
	}
	return d
}

// fileGutter is the gutter layout for a file: widths of the old/new line
// number columns, and the total gutter width (after the marker column).
func fileGutter(fd *session.FileDiff) (numW, total int) {
	maxNo := 0
	for _, l := range fd.All {
		maxNo = maxInt(maxNo, maxInt(l.OldNo, l.NewNo))
	}
	numW = maxInt(3, digits(maxNo))
	return numW, numW*2 + 4 // "old new ± "
}

func (m *Model) buildFileRows(path string) []Row {
	fd := m.sess.Files[path]
	if fd == nil {
		return nil
	}
	w := m.centreBodyW()
	if fd.Binary {
		return []Row{{Kind: RowText, Prefix: " ", Spans: []Span{sp("binary file changed", stDim)}, DiffRow: -1}}
	}
	numW, gutterW := fileGutter(fd)
	codeIndent := strings.Repeat(" ", gutterW)
	emph := m.emphFor(path, fd)
	newHl, oldHl := m.highlighted(path)

	var rows []Row
	// Whole-file Threads and Feedback sit at the top.
	for _, t := range m.sess.Threads {
		if t.Anchor.Type == session.AnchorFile && t.Anchor.Path == path {
			rows = append(rows, m.threadCardRows(t, " ", w)...)
		}
	}
	for _, f := range m.sess.Feedback {
		if f.Anchor.Type == session.AnchorFile && f.Anchor.Path == path {
			rows = append(rows, m.feedbackCardRows(f, " ", w)...)
		}
	}
	// PR Comments this diff can't place (outdated, or on hidden lines).
	for _, c := range m.sess.FileTopComments(path) {
		rows = append(rows, m.commentCardRows(c, " ", w, false)...)
	}

	seenMethod := map[string]bool{}
	emitRange := func(s, e int) {
		// A Touched Method whose start (its leading comment, else its own first line)
		// is hidden above this range still gets its block, before its first
		// visible line; a later hunk of it gets a slim rule.
		for _, mk := range m.sess.MethodsByFile[path] {
			meth := m.sess.Methods[mk]
			start, mEnd := m.methodBlockRow(meth), m.methodRowEnd(meth)
			if start >= s && start < e {
				continue
			}
			if mEnd < s || start >= e {
				continue
			}
			if !seenMethod[mk] {
				rows = append(rows, m.methodBlockRows(meth, w, fmt.Sprintf("%s:%d", meth.Path, lineNoOf(fd, m.methodRowStart(meth))), false)...)
				seenMethod[mk] = true
			} else {
				rows = append(rows, m.methodBlockRows(meth, w, "", true)...)
			}
		}
		for i := s; i < e && i < len(fd.All); i++ {
			// The block sits above the method, like a note pinned to it.
			for _, mk := range m.sess.MethodsByFile[path] {
				meth := m.sess.Methods[mk]
				if m.methodBlockRow(meth) == i && !seenMethod[mk] {
					rows = append(rows, m.methodBlockRows(meth, w, "", false)...)
					seenMethod[mk] = true
				}
			}
			rows = append(rows, m.codeRows(path, fd, i, numW, w, emph, newHl, oldHl)...)
			for _, t := range m.sess.ThreadsOnRow(path, i) {
				rows = append(rows, m.threadCardRows(t, codeIndent, w)...)
			}
			for _, f := range m.sess.FeedbackOnRow(path, i) {
				rows = append(rows, m.feedbackCardRows(f, codeIndent, w)...)
			}
			for _, c := range m.sess.CommentsOnRow(path, i) {
				rows = append(rows, m.commentCardRows(c, codeIndent, w, true)...)
			}
		}
	}

	if !m.currentHunkMode(path) || len(fd.Hunks) == 0 {
		emitRange(0, len(fd.All))
		return rows
	}
	prevEnd := 0
	for i, h := range fd.Hunks {
		if h.Start > prevEnd {
			key := fmt.Sprintf("%s:gap:%d", path, i)
			if m.gapExpanded[key] {
				emitRange(prevEnd, h.Start)
			} else {
				rows = append(rows, m.gapRow(path, key, prevEnd, h.Start, w))
			}
		}
		emitRange(h.Start, h.End)
		prevEnd = h.End
	}
	if prevEnd < len(fd.All) {
		key := path + ":gap:end"
		if m.gapExpanded[key] {
			emitRange(prevEnd, len(fd.All))
		} else {
			rows = append(rows, m.gapRow(path, key, prevEnd, len(fd.All), w))
		}
	}
	return rows
}

func lineNoOf(fd *session.FileDiff, row int) int {
	if row < 0 || row >= len(fd.All) {
		return 0
	}
	if l := fd.All[row]; l.NewNo != 0 {
		return l.NewNo
	} else {
		return l.OldNo
	}
}

func (m *Model) gapRow(path, key string, start, end, w int) Row {
	fd := m.sess.Files[path]
	text := fmt.Sprintf("⋯  %d unchanged lines  (%d–%d)", end-start, lineNoOf(fd, start), lineNoOf(fd, end-1))
	return Row{
		Kind: RowGap, Prefix: " ", Spans: []Span{sp(text, stDim.Italic(true))}, BG: cMantle, DiffRow: -1,
		Activate: func(mm *Model) tea.Cmd {
			mm.gapExpanded[key] = true
			return nil
		},
	}
}

// codeRows renders one merged diff line: the gutter, then the highlighted
// code, hard-wrapped onto continuation rows when it's wider than the pane.
func (m *Model) codeRows(path string, fd *session.FileDiff, idx, numW, w int, emph map[int][2]int, newHl, oldHl highlight.File) []Row {
	l := fd.All[idx]
	num := func(n int) string {
		if n <= 0 {
			return strings.Repeat(" ", numW)
		}
		return fmt.Sprintf("%*d", numW, n)
	}
	var spans []Span
	if l.Kind != session.LineRemoved && l.NewNo > 0 && l.NewNo < len(newHl.Lines) {
		spans = newHl.Lines[l.NewNo]
	} else if l.OldNo > 0 && l.OldNo < len(oldHl.Lines) {
		spans = oldHl.Lines[l.OldNo]
	}
	if len(spans) == 0 && l.Text != "" {
		spans = []Span{{Text: l.Text, Style: stText}}
	}
	spans = expandTabs(spans)

	sign, bg, numSt, signSt, emBG := " ", "", stFaint, stFaint, ""
	switch l.Kind {
	case session.LineAdded:
		sign, bg, numSt, signSt, emBG = "+", bgAdded, fg("#5d8a6a"), stAdded.Bold(true), bgAddedEmph
	case session.LineRemoved:
		sign, bg, numSt, signSt, emBG = "−", bgRemoved, fg("#9a5a64"), stRemoved.Bold(true), bgRemovedEmph
	}
	gst := func(st lg.Style) lg.Style {
		if bg != "" {
			return st.Background(lg.Color(bg))
		}
		return st
	}
	gutter := gst(numSt).Render(num(l.OldNo)+" "+num(l.NewNo)+" ") + gst(signSt).Render(sign) + gst(lg.NewStyle()).Render(" ")
	contGutter := gst(numSt).Render(strings.Repeat(" ", numW*2+2)) + gst(stFaint).Render("↪") + gst(lg.NewStyle()).Render(" ")

	marker, mst := "", lg.NewStyle()
	if fb := m.sess.FeedbackOnRow(path, idx); len(fb) > 0 {
		marker, mst = "✎", stFeedback
	} else if ts := m.sess.ThreadsOnRow(path, idx); len(ts) > 0 {
		_, mst = threadMarker(ts)
		marker = "●"
	}

	anchor := session.Anchor{Type: session.AnchorDiff, Path: path, RowStart: idx, RowEnd: idx}
	em := emph[idx]
	parts := hardWrapSpans(spans, w-cellWidth(gutter))
	rows := make([]Row, 0, len(parts))
	off := 0
	for i, p := range parts {
		r := Row{
			Kind: RowCode, Prefix: gutter, Spans: p, BG: bg, DiffRow: idx, Selectable: true, Anchor: anchor,
			Marker: marker, MarkerStyle: mst,
		}
		if em[1] > em[0] {
			r.EmphStart, r.EmphEnd, r.EmphBG = em[0]-off, em[1]-off, emBG
		}
		if i > 0 {
			r.Prefix, r.Join, r.Marker = contGutter, JoinHard, ""
		}
		off += spansLen(p)
		rows = append(rows, r)
	}
	return rows
}

// emphFor pairs each run of removed lines with the run of added lines right
// after it, line by line, and marks the part of each line that differs
// (common prefix and suffix trimmed) so the changed words stand out.
func (m *Model) emphFor(path string, fd *session.FileDiff) map[int][2]int {
	if e, ok := m.emph[path]; ok {
		return e
	}
	e := map[int][2]int{}
	all := fd.All
	for i := 0; i < len(all); {
		if all[i].Kind != session.LineRemoved {
			i++
			continue
		}
		rs := i
		for i < len(all) && all[i].Kind == session.LineRemoved {
			i++
		}
		as := i
		for i < len(all) && all[i].Kind == session.LineAdded {
			i++
		}
		nr, na := as-rs, i-as
		for k := 0; k < minInt(nr, na); k++ {
			a := []rune(strings.ReplaceAll(all[rs+k].Text, "\t", "    "))
			b := []rune(strings.ReplaceAll(all[as+k].Text, "\t", "    "))
			p := 0
			for p < len(a) && p < len(b) && a[p] == b[p] {
				p++
			}
			s := 0
			for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
				s++
			}
			// Only worth it when most of the line is shared.
			if p+s < maxInt(len(a), len(b))/3 {
				continue
			}
			e[rs+k] = [2]int{p, len(a) - s}
			e[as+k] = [2]int{p, len(b) - s}
		}
	}
	m.emph[path] = e
	return e
}

// methodTName is the method's full T.Name form: (*Beacon).ProcessBounce.
func methodTName(meth *session.Method) string {
	switch {
	case meth.Owner == "":
		return meth.Name
	case strings.HasPrefix(meth.Owner, "*"):
		return "(" + meth.Owner + ")." + meth.Name
	}
	return meth.Owner + "." + meth.Name
}

// methodBlockRows is the note pinned above a Touched Method: a header rule,
// then a tinted band (call graph, does, changed) with a bar in the marker
// column, then one blank row before the code. loc is set when the method's
// own first line is hidden (hunk mode); slim is a later hunk's rule.
func (m *Model) methodBlockRows(meth *session.Method, w int, loc string, slim bool) []Row {
	anchor := session.Anchor{Type: session.AnchorBriefing, Ref: "method:" + meth.Key, Path: meth.Path, Text: meth.DisplayName()}
	tmpl := Row{Kind: RowText, DiffRow: -1, Selectable: true, Anchor: anchor}
	rule := func(spans []Span, st lg.Style) Row {
		r := tmpl
		used := cellWidth(spansText(spans))
		r.Spans = append(spans, sp(" "+strings.Repeat("─", maxInt(0, w-used-2)), st))
		return r
	}
	if slim {
		st := stMethod.Faint(true)
		return []Row{rule([]Span{sp("── ", st), sp(methodTName(meth), stMethod), sp(" (cont.)", stDim)}, st)}
	}
	head := []Span{sp("── ", stMethod), sp(methodTName(meth), stMethodBold)}
	if loc != "" {
		head = append(head, sp("  "+loc, stDim))
	}
	rows := []Row{rule(head, stMethod)}

	band := tmpl
	band.BG = bgMethodBand
	band.Marker, band.MarkerStyle = "▎", fg(cMauve).Background(lg.Color(bgMethodBand))
	bgSt := lg.NewStyle().Background(lg.Color(bgMethodBand))
	const labelW = 11
	first, rest := bgSt.Render("  "), bgSt.Render(strings.Repeat(" ", 2+labelW))
	stLabel := fg(cOverlay1)
	labelled := func(label string, spans []Span, links []Link, selectable bool) {
		r := band
		r.Selectable = selectable
		lead := sp(label+strings.Repeat(" ", labelW-cellWidth(label)), stLabel)
		for i := range links {
			links[i].Start += labelW
			links[i].End += labelW
		}
		r.Links = links
		rows = append(rows, textRows(append([]Span{lead}, spans...), w, first, rest, r)...)
	}

	// Call graph: every name is a link to that method.
	names := func(bare []string, keys []string) ([]Span, []Link) {
		var spans []Span
		var links []Link
		pos := 0
		for i, k := range keys {
			if i > 0 {
				spans = append(spans, sp(" · ", stDim))
				pos += 3
			}
			kk, name := k, bare[i]
			links = append(links, Link{Start: pos, End: pos + len([]rune(name)), Act: func(mm *Model) tea.Cmd {
				mm.jumpToMethod(kk)
				return nil
			}})
			spans = append(spans, sp(name, stLink))
			pos += len([]rune(name))
		}
		return spans, links
	}
	if exact, expandable, ok := m.callersSummary(meth, w); ok {
		// Go: the exact callers across the module, after the Touched
		// Methods among them; enter (or a click) expands the list.
		sps, ls := names(meth.Callers, meth.CallerKeys)
		if len(sps) > 0 {
			sps = append(sps, sp("   ", stDim))
		}
		at := len(rows)
		labelled("↑ callers", append(sps, exact...), ls, true)
		if expandable {
			key := meth.Key
			for i := at; i < len(rows); i++ {
				rows[i].Activate = func(mm *Model) tea.Cmd { mm.toggleCallers(key); return nil }
			}
			if m.callersOpen[key] {
				rows = append(rows, m.callerListRows(meth, band, w)...)
			}
		}
	} else if len(meth.CallerKeys) == 0 {
		labelled("↑ callers", []Span{sp("none in PR", stDim)}, nil, true)
	} else {
		sps, ls := names(meth.Callers, meth.CallerKeys)
		labelled("↑ callers", sps, ls, true)
	}
	if len(meth.CalleeKeys) > 0 {
		sps, ls := names(meth.Callees, meth.CalleeKeys)
		labelled("↓ calls", sps, ls, true)
	}
	for _, part := range [][2]string{{"does", meth.Does}, {"changed", meth.Changed}} {
		switch {
		case part[1] != "":
			labelled(part[0], []Span{sp(part[1], stText)}, nil, true)
		case !m.sess.Briefing.Ready:
			labelled(part[0], []Span{sp(m.briefingPendingText(), stRunning.Italic(true))}, nil, false)
		}
	}
	rows = append(rows, Row{Kind: RowText, DiffRow: -1})
	return rows
}

// ---- jumps ----

func (m *Model) openFile(path string) {
	m.markJump()
	if m.centreView != ViewFile || m.filePath != path {
		m.centreView, m.filePath = ViewFile, path
		m.centreCursor, m.centreScroll = 0, 0
		m.sel = Selection{}
	}
}

// jumpToMethod opens the method's file (in whatever mode the file is in)
// and lands with its block's header rule near the top, so the whole block
// and the code under it are in view.
func (m *Model) jumpToMethod(key string) {
	meth := m.sess.Methods[key]
	if meth == nil {
		return
	}
	m.markJump()
	m.openFile(meth.Path)
	m.rebuild()
	for i, r := range m.centreRows {
		if r.Anchor.Type == session.AnchorBriefing && r.Anchor.Ref == "method:"+key {
			m.centreCursor = i
			m.centreScroll = i - 1
			m.clampCentre()
			return
		}
	}
	m.revealDiffRow(m.methodRowStart(meth))
}

// gotoLine opens path and reveals the first row showing line (new side,
// else old side), expanding to the full file if hunks hide it.
func (m *Model) gotoLine(path string, line int) bool {
	fd := m.sess.Files[path]
	if fd == nil {
		return false
	}
	m.markJump()
	target := -1
	for i, l := range fd.All {
		if l.NewNo == line {
			target = i
			break
		}
	}
	if target < 0 {
		for i, l := range fd.All {
			if l.OldNo == line {
				target = i
				break
			}
		}
	}
	m.openFile(path)
	m.rebuild()
	if target < 0 {
		return true
	}
	if !m.revealDiffRow(target) {
		m.hunkMode[path] = false
		m.rebuild()
		m.revealDiffRow(target)
	}
	return true
}

func (m *Model) revealDiffRow(diffRow int) bool {
	for i, r := range m.centreRows {
		if r.DiffRow == diffRow && r.Join != JoinHard {
			m.revealRow(i)
			return true
		}
	}
	return false
}

func (m *Model) jumpToCitation(c session.Citation) tea.Cmd {
	if c.Path == "" {
		return nil
	}
	if strings.Contains(c.Path, ":") {
		return m.flashCmd("Not in this PR (open in editor): " + c.Path) // a Related Codebase
	}
	return m.jumpToCaller(c.Path, c.Line)
}

func (m *Model) jumpHunk(dir int) {
	fd := m.sess.Files[m.filePath]
	if m.centreView != ViewFile || fd == nil || len(fd.Hunks) == 0 {
		return
	}
	cur := m.cursorDiffRow()
	// A hunk "starts" at its first changed line, not its leading context.
	starts := make([]int, 0, len(fd.Hunks))
	for _, h := range fd.Hunks {
		s := h.Start
		for s < h.End && fd.All[s].Kind == session.LineContext {
			s++
		}
		starts = append(starts, s)
	}
	target := -1
	if dir > 0 {
		for _, s := range starts {
			if s > cur {
				target = s
				break
			}
		}
	} else {
		for i := len(starts) - 1; i >= 0; i-- {
			if starts[i] < cur {
				target = starts[i]
				break
			}
		}
	}
	if target >= 0 {
		m.revealDiffRow(target)
	}
}

// cursorDiffRow is the diff row at or above the cursor (-1 if none).
func (m *Model) cursorDiffRow() int {
	for i := m.centreCursor; i >= 0 && i < len(m.centreRows); i-- {
		if r := m.centreRows[i].DiffRow; r >= 0 {
			return r
		}
	}
	return -1
}

func (m *Model) jumpMethod(dir int) {
	keys := m.sess.MethodsByFile[m.filePath]
	if m.centreView != ViewFile || len(keys) == 0 {
		return
	}
	// On a method's block (synthetic rows), "here" is that method's start;
	// otherwise the nearest code line at or above the cursor.
	cur := m.cursorDiffRow()
	if m.centreCursor >= 0 && m.centreCursor < len(m.centreRows) {
		if r := m.centreRows[m.centreCursor]; r.Anchor.Type == session.AnchorBriefing && strings.HasPrefix(r.Anchor.Ref, "method:") {
			if meth := m.sess.Methods[strings.TrimPrefix(r.Anchor.Ref, "method:")]; meth != nil {
				cur = m.methodBlockRow(meth)
			}
		}
	}
	best := ""
	if dir > 0 {
		for _, k := range keys {
			if m.methodBlockRow(m.sess.Methods[k]) > cur {
				best = k
				break
			}
		}
	} else {
		for i := len(keys) - 1; i >= 0; i-- {
			if m.methodBlockRow(m.sess.Methods[keys[i]]) < cur {
				best = keys[i]
				break
			}
		}
	}
	if best != "" {
		m.jumpToMethod(best)
	}
}

func (m *Model) jumpFile(dir int) {
	order := m.sess.FileOrder()
	idx := -1
	for i, p := range order {
		if p == m.filePath {
			idx = i
			break
		}
	}
	idx += dir
	if idx < 0 || idx >= len(order) {
		return
	}
	m.openFile(order[idx])
}
