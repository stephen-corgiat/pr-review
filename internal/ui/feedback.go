package ui

// Feedback cards: Agent Notes, the Research list, Refine and its Revision
// (a word diff, or questions back), and the in-place editor.

import (
	"fmt"
	"regexp"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

var (
	stNote       = fg(cPeach).Faint(true).Italic(true) // an Agent Note: dim italic, accent colour
	stNoteAccent = fg(cPeach)
	stDiffIns    = fg(cGreen).Background(lg.Color(bgAdded))
	stDiffDel    = fg(cRed).Faint(true).Strikethrough(true)
	stRevision   = fg(cMauve)
)

func fbKey(f *session.Feedback) string { return "fb:" + f.ID }

func feedbackStyle(f *session.Feedback) lg.Style {
	switch f.Status {
	case session.FeedbackReady:
		return fg(cGreen)
	case session.FeedbackQueued, session.FeedbackHandedOff:
		return fg(cTeal)
	case session.FeedbackPosted:
		return stRead
	case session.FeedbackDone:
		return fg(cGreen).Faint(true)
	}
	return stFeedback
}

// outcomeSpan is an Outcome's short label: ✓ addressed, ✗ declined,
// ? needs you.
func outcomeSpan(o *session.Outcome) Span {
	switch o.Kind {
	case session.OutcomeAddressed:
		return sp("✓ addressed", stAdded)
	case session.OutcomeDeclined:
		return sp("✗ declined", stRemoved)
	}
	return sp("? needs you", fg(cYellow).Bold(true))
}

// outcomeRows lists the Outcomes reported on handed-off Feedback.
func outcomeRows(f *session.Feedback, w int, tmpl Row) []Row {
	var rows []Row
	for i := range f.Outcomes {
		o := &f.Outcomes[i]
		spans := []Span{sp("Outcome  ", stDim), outcomeSpan(o)}
		if o.Commit != "" {
			spans = append(spans, sp(" · ", stDim), sp(shortSHA(o.Commit), fg(cPeach)))
		}
		if o.Note != "" {
			spans = append(spans, sp(" · ", stDim), sp(o.Note, stText))
		}
		if !o.At.IsZero() {
			spans = append(spans, sp("  "+o.At.Local().Format("2 Jan 15:04"), stDim))
		}
		rows = append(rows, textRows(spans, w, "", "         ", tmpl)...)
	}
	return rows
}

// noteSpans styles text with its Agent Notes picked out.
func noteSpans(text string, base lg.Style) []Span {
	var out []Span
	last := 0
	for _, n := range session.AgentNotes(text) {
		if n.Start > last {
			out = append(out, sp(text[last:n.Start], base))
		}
		out = append(out, sp(text[n.Start:n.End], stNote))
		last = n.End
	}
	if last < len(text) {
		out = append(out, sp(text[last:], base))
	}
	return out
}

// linesOf splits spans at newlines, one []Span per line.
func linesOf(spans []Span) [][]Span {
	plain := []rune(spansText(spans))
	var out [][]Span
	start := 0
	for i, r := range plain {
		if r == '\n' {
			out = append(out, sliceSpans(spans, start, i))
			start = i + 1
		}
	}
	return append(out, sliceSpans(spans, start, len(plain)))
}

func notesLabel(n int) string {
	if n == 1 {
		return "✎ 1 agent note"
	}
	return fmt.Sprintf("✎ %d agent notes", n)
}

// feedbackKeys are the keys a Feedback card's rows answer to (before the
// action letters, so r refines rather than asks).
func (m *Model) feedbackKeys(f *session.Feedback) map[string]func(*Model) tea.Cmd {
	return map[string]func(*Model) tea.Cmd{
		"r": func(mm *Model) tea.Cmd { return mm.refine(f) },
		"A": func(mm *Model) tea.Cmd { return mm.acceptRevision(f) },
	}
}

func (m *Model) feedbackCardRows(f *session.Feedback, indent string, paneW int) []Row {
	cw := cardWidth(paneW, indent)
	rowW := cellWidth(indent) + cw
	fst := feedbackStyle(f)
	base := Row{Kind: RowCard, DiffRow: -1, Feedback: f, Width: rowW, Keys: m.feedbackKeys(f)}
	var rows []Row
	nNotes := len(session.AgentNotes(f.Body))
	switch {
	case m.fbEdit != nil && m.fbEdit.fb == f && !m.fbEdit.rev:
		rows = m.feedbackEditorRows(f, indent, cw, base)
	case !m.expandedCards[fbKey(f)]:
		head := []Span{sp(" ▸ ", stDim), sp("✎ ", fst), sp(f.ID, stBold), sp("  Feedback · ", stSub), sp(f.Status.String(), fst)}
		if nNotes > 0 {
			head = append(head, sp("  "+notesLabel(nNotes), stNoteAccent))
		}
		if o := f.LastOutcome(); o != nil {
			head = append(head, sp("  ", stDim), outcomeSpan(o))
		}
		if f.Anchor.Stale {
			head = append(head, sp("  ⚠ Stale", stStale))
		}
		if f.Revision != nil {
			head = append(head, sp("  ✦ Revision", stRevision))
		}
		head = append(head, sp("  ", lg.NewStyle()))
		room := cw - cellWidth(spansText(head)) - 1
		r := base
		r.Prefix, r.BG = indent, bgCard
		r.Spans = append(head, sp(truncEllipsis(oneLine(f.Body), maxInt(3, room)), stText))
		r.Activate = func(mm *Model) tea.Cmd { mm.expandedCards[fbKey(f)] = true; return nil }
		rows = []Row{r}
	default:
		rows = m.feedbackExpandedRows(f, indent, cw, rowW, base, nNotes)
	}
	return append(rows, m.revisionRows(f, indent, paneW)...)
}

func (m *Model) feedbackExpandedRows(f *session.Feedback, indent string, cw, rowW int, base Row, nNotes int) []Row {
	fst := feedbackStyle(f)
	bst := fg(cPink).Faint(f.Status.Locked())
	label := []Span{sp("▾ ", stDim), sp("✎ ", fst), sp(f.ID, stBold), sp(" · Feedback · ", stSub), sp(f.Status.String(), fst)}
	rows := []Row{m.cardEdge(base, indent, bst, "╭─ ", label, "╮", cw, func(mm *Model) tea.Cmd {
		mm.expandedCards[fbKey(f)] = false
		return nil
	})}
	tmpl := base
	tmpl.Selectable = true
	tmpl.Anchor = f.Anchor
	body := m.staleAnchorRows(f.Anchor, cw-4, base)
	if len(body) > 0 {
		body = append(body, base)
	}
	for i, line := range linesOf(noteSpans(f.Body, stText)) {
		t := tmpl
		if i > 0 {
			t.Join = JoinLine
		}
		body = append(body, textRows(line, cw-4, "", "", t)...)
	}
	fixed := base
	if nNotes > 0 {
		body = append(body, textRow(fixed, []Span{sp(notesLabel(nNotes), stNoteAccent), sp("  · r Refine fills them · Hand to agent keeps them", stDim)}))
	}
	if len(f.Outcomes) > 0 {
		body = append(body, fixed)
		body = append(body, outcomeRows(f, cw-4, fixed)...)
	}
	hint := []Span{sp("e", stKey), sp(" edit  ", stDim), sp("r", stKey), sp(" refine  ", stDim), sp("space", stKey), sp(" ready↔draft  ", stDim), sp("P", stKey), sp(" send  ", stDim), sp("X", stKey), sp(" delete", stDim)}
	switch f.Status {
	case session.FeedbackQueued:
		hint = []Span{sp("queued: /pull-review hands it off  ", stDim.Italic(true)), sp("O", stKey), sp(" takes it back", stDim)}
	case session.FeedbackHandedOff:
		hint = []Span{sp("handed off  ", stDim.Italic(true)), sp("D", stKey), sp(" done  ", stDim), sp("O", stKey), sp(" reopen (back to a draft)", stDim)}
	case session.FeedbackDone:
		hint = []Span{sp("done  ", stDim.Italic(true)), sp("O", stKey), sp(" reopen (back to a draft)", stDim)}
	case session.FeedbackPosted:
		hint = []Span{sp("posted: no more edits", stDim.Italic(true))}
	}
	body = append(body, fixed, textRow(fixed, hint))
	for _, r := range body {
		r.Prefix = indent + bst.Render("│") + " " + r.Prefix
		r.Suffix = " " + bst.Render("│")
		r.Width = rowW
		rows = append(rows, r)
	}
	return append(rows, m.cardEdge(base, indent, bst, "╰", nil, "╯", cw, nil))
}

// ---- Refine and the Revision ----

func (m *Model) refine(f *session.Feedback) tea.Cmd {
	m.dirty = true
	if f.Status.Locked() {
		return m.flashCmd(fmt.Sprintf("%s is %s: it can't be refined", f.ID, f.Status))
	}
	cmd := m.do(engine.Refine{ID: f.ID})
	if f.Refine == session.RefineNone {
		return cmd
	}
	return tea.Batch(cmd, m.flashCmd(f.ID+": Refine asked"))
}

func (m *Model) acceptRevision(f *session.Feedback) tea.Cmd {
	m.dirty = true
	if f.Revision == nil {
		return m.flashCmd(f.ID + " has no Revision: r refines it")
	}
	if _, err := m.eng.Do(engine.AcceptRevision{ID: f.ID}); err != nil {
		return m.flashCmd(err.Error())
	}
	return m.flashCmd(f.ID + ": Revision accepted")
}

func (m *Model) revisionKeys(f *session.Feedback) map[string]func(*Model) tea.Cmd {
	answer := func(mm *Model) tea.Cmd {
		if f.Revision == nil || len(f.Revision.Questions) == 0 {
			return nil
		}
		mm.tbFeedback = f
		mm.openTextbox(TextboxRefineAnswer, "")
		return nil
	}
	return map[string]func(*Model) tea.Cmd{
		"A": func(mm *Model) tea.Cmd { return mm.acceptRevision(f) },
		"e": func(mm *Model) tea.Cmd { return mm.editRevision(f) },
		"d": func(mm *Model) tea.Cmd {
			mm.revDiff[f.ID] = !mm.revDiff[f.ID]
			mm.dirty = true
			return nil
		},
		"X": func(mm *Model) tea.Cmd {
			mm.do(engine.IgnoreRevision{ID: f.ID})
			return mm.flashCmd(f.ID + ": Revision ignored")
		},
		"R": answer,
		"r": func(mm *Model) tea.Cmd { return mm.refine(f) },
	}
}

// revisionRows sits under the Feedback card: the Refine's state, or the
// Revision (a word diff, or questions back).
func (m *Model) revisionRows(f *session.Feedback, indent string, paneW int) []Row {
	ind := indent + "  "
	cw := cardWidth(paneW, ind)
	rowW := cellWidth(ind) + cw
	base := Row{Kind: RowCard, DiffRow: -1, Feedback: f, Width: rowW, Revision: true, Keys: m.revisionKeys(f)}
	switch f.Refine {
	case session.RefineQueued:
		r := base
		r.Prefix = ind
		r.Spans = []Span{sp("…  Refine queued for "+f.ID, stRunning.Italic(true))}
		return []Row{r}
	case session.RefineRunning:
		r := base
		r.Prefix = ind
		r.Spans = []Span{sp("◐  Refining "+f.ID+"…", stRunning.Italic(true)), sp("  edits now make the Revision Stale", stDim)}
		return []Row{r}
	}
	rev := f.Revision
	if rev == nil {
		if f.RefineError == "" {
			return nil
		}
		r := base
		r.Prefix = ind
		r.Spans = []Span{sp("!  Refine failed: ", stNeedsYou), sp(truncEllipsis(f.RefineError, maxInt(10, cw-24)), stSub), sp("  r tries again", stDim)}
		return []Row{r}
	}
	w := cw - 4
	bst := stRevision
	if rev.Stale {
		bst = fg(cRed)
	}
	label := []Span{sp("✦ Revision", stRevision.Bold(true)), sp(" · "+f.ID, stSub)}
	questions := len(rev.Questions) > 0 && rev.Text == ""
	switch {
	case rev.Stale:
		label = append(label, sp(" · Stale", stNeedsYou))
	case questions:
		label = append(label, sp(" · questions back", fg(cYellow)))
	case rev.Placeholder:
		label = append(label, sp(" · placeholder", stDim))
	}
	if rev.Edited {
		label = append(label, sp(" · edited by you", stSub))
	}
	editing := m.fbEdit != nil && m.fbEdit.fb == f && m.fbEdit.rev
	if editing {
		label = append(label, sp(" · editing", stFeedback))
	}
	rows := []Row{m.cardEdge(base, ind, bst, "╭─ ", label, "╮", cw, nil)}
	tmpl := base
	tmpl.Selectable = true
	tmpl.Anchor = f.Anchor
	var body []Row
	switch {
	case editing:
		m.fbEdit.ta.SetWidth(w)
		for i, l := range strings.Split(m.fbEdit.ta.View(), "\n") {
			r := textRow(base, ansiToSpans(l))
			r.TALine = i + 1
			body = append(body, r)
		}
		body = append(body, textRow(base, nil), textRow(base, []Span{
			sp("enter", stKey), sp(" save the Revision  ", stDim), sp("alt+enter", stKey), sp(" newline  ", stDim), sp("ctrl+z/y", stKey), sp(" undo/redo  ", stDim), sp("esc", stKey), sp(" cancel", stDim),
		}))
	case questions:
		body = append(body, textRows(inline(rev.Gist, stText), w, "", "", tmpl)...)
		body = append(body, textRow(base, nil))
		for i, q := range rev.Questions {
			qr := tmpl
			qr.Activate = base.Keys["R"]
			body = append(body, textRows(append([]Span{sp(fmt.Sprintf("%d. ", i+1), stAccent)}, inline(q, stText)...), w, "", "   ", qr)...)
		}
		body = append(body, textRow(base, nil))
		body = append(body, textRow(base, []Span{sp("R", stKey), sp(" (or enter on a question) answers them and refines again · ", stDim), sp("X", stKey), sp(" dismiss", stDim)}))
	default:
		spans, removed := revisedSpans(rev.BaseBody, rev.Text)
		if m.revDiff[f.ID] {
			spans = wordDiffSpans(rev.BaseBody, rev.Text)
		}
		for i, line := range linesOf(spans) {
			t := tmpl
			if i > 0 {
				t.Join = JoinLine
			}
			body = append(body, textRows(line, w, "", "", t)...)
		}
		switch {
		case m.revDiff[f.ID]:
			body = append(body, textRow(base, []Span{sp("word diff against the Feedback · ", stDim), sp("d", stKey), sp(" shows the revised text", stDim)}))
		case removed > 0:
			body = append(body, textRow(base, []Span{sp(fmt.Sprintf("− %d word%s removed · ", removed, plural(removed)), stDim), sp("d", stKey), sp(" shows the word diff", stDim)}))
		}
		if rev.WhatChanged != "" {
			body = append(body, textRow(base, nil))
			body = append(body, textRows([]Span{sp(rev.WhatChanged, stDim.Italic(true))}, w, "", "", tmpl)...)
		}
		if len(rev.Citations) > 0 {
			body = append(body, m.citationListRowsIn("", "↳ ", rev.Citations, w, tmpl)...)
		}
		body = append(body, textRow(base, nil))
		if rev.Stale {
			body = append(body, textRows([]Span{sp("Stale: ", stNeedsYou), sp(f.ID+" changed while Refine ran, so A is off. ", stSub), sp("r", stKey), sp(" refines again · ", stDim), sp("e", stKey), sp(" edits the Revision · ", stDim), sp("X", stKey), sp(" ignore", stDim)}, w, "", "", base)...)
		} else {
			body = append(body, textRow(base, []Span{sp("A", stKey), sp(" accept  ", stDim), sp("e", stKey), sp(" edit  ", stDim), sp("d", stKey), sp(" diff  ", stDim), sp("X", stKey), sp(" ignore  ", stDim), sp("r", stKey), sp(" refine again", stDim)}))
		}
	}
	for _, r := range body {
		r.Prefix = ind + bst.Render("│") + " " + r.Prefix
		r.Suffix = " " + bst.Render("│")
		r.Width = rowW
		rows = append(rows, r)
	}
	return append(rows, m.cardEdge(base, ind, bst, "╰", nil, "╯", cw, nil))
}

// ---- word diff ----

var wordRE = regexp.MustCompile(`^\s+|\S+\s*`)

type wordOp struct {
	kind byte // '=', '-', '+'
	text string
}

// wordOps diffs a against b word by word (LCS over words with their
// trailing space). Within each changed stretch every deletion comes before
// every insertion, so old and new wording read as phrases.
func wordOps(a, b string) []wordOp {
	at, bt := wordRE.FindAllString(a, -1), wordRE.FindAllString(b, -1)
	key := func(t string) string { return strings.TrimRight(t, " \t") }
	n, mm := len(at), len(bt)
	lcs := make([][]int, n+1)
	for i := range lcs {
		lcs[i] = make([]int, mm+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := mm - 1; j >= 0; j-- {
			if key(at[i]) == key(bt[j]) {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = maxInt(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	var out, dels, inss []wordOp
	flush := func() {
		out = append(append(out, dels...), inss...)
		dels, inss = nil, nil
	}
	i, j := 0, 0
	for i < n || j < mm {
		switch {
		case i < n && j < mm && key(at[i]) == key(bt[j]):
			flush()
			out = append(out, wordOp{'=', bt[j]})
			i++
			j++
		case j >= mm || (i < n && lcs[i+1][j] >= lcs[i][j+1]):
			dels = append(dels, wordOp{'-', at[i]})
			i++
		default:
			inss = append(inss, wordOp{'+', bt[j]})
			j++
		}
	}
	flush()
	return out
}

func appendSpan(out []Span, t string, st lg.Style) []Span {
	if len(out) > 0 && out[len(out)-1].Style.String() == st.String() {
		out[len(out)-1].Text += t
		return out
	}
	return append(out, sp(t, st))
}

// wordDiffSpans is the old interleaved view (d): deletions red and struck
// through, insertions green.
func wordDiffSpans(a, b string) []Span {
	var out []Span
	for _, o := range wordOps(a, b) {
		word := strings.TrimRight(o.text, " ")
		switch o.kind {
		case '=':
			out = appendSpan(out, o.text, stText)
		case '-':
			out = appendSpan(appendSpan(out, word, stDiffDel), " ", stText)
		default:
			out = appendSpan(appendSpan(out, word, stDiffIns), " ", stText)
		}
	}
	return out
}

// revisedSpans is the revised text on its own, inserted and changed words
// on a green tint (so is whatever filled an Agent Note), plus how many
// words were removed.
func revisedSpans(a, b string) ([]Span, int) {
	var out []Span
	removed := 0
	for _, o := range wordOps(a, b) {
		switch o.kind {
		case '=':
			out = appendSpan(out, o.text, stText)
		case '-':
			removed++
		default:
			word := strings.TrimRight(o.text, " ")
			out = appendSpan(appendSpan(out, word, stRevIns), o.text[len(word):], stText)
		}
	}
	return out, removed
}

var stRevIns = fg(cText).Background(lg.Color(bgAddedEmph))

// ---- the editor ----

// feedbackEditorRows is the Feedback card while it's being written: the
// text area sits inside the card, right under the line it's about. The
// textarea can't style its own text, so Agent Notes show in a live preview
// line under it.
func (m *Model) feedbackEditorRows(f *session.Feedback, indent string, cw int, base Row) []Row {
	ed := m.fbEdit
	bst := fg(cPink).Bold(true)
	title := "editing"
	if ed.isNew {
		title = "new"
	}
	label := []Span{sp("✎ ", stFeedback), sp(f.ID, stBold), sp(" · Feedback · ", stSub), sp(title, stFeedback)}
	rows := []Row{m.cardEdge(base, indent, bst, "╭─ ", label, "╮", cw, nil)}
	ed.ta.SetWidth(cw - 4)
	var body []Row
	for i, l := range strings.Split(ed.ta.View(), "\n") {
		r := textRow(base, ansiToSpans(l))
		r.TALine = i + 1
		body = append(body, r)
	}
	if notes := session.AgentNotes(ed.ta.Value()); len(notes) > 0 {
		spans := []Span{sp(notesLabel(len(notes))+": ", stNoteAccent)}
		for i, n := range notes {
			if i > 0 {
				spans = append(spans, sp("  ", stDim))
			}
			spans = append(spans, sp("<<"+n.Text+">>", stNote))
		}
		body = append(body, base)
		body = append(body, textRows(spans, cw-4, "", "  ", base)...)
	} else if strings.Contains(ed.ta.Value(), "<<") {
		body = append(body, base, textRow(base, []Span{sp("<< … >> makes an Agent Note (outside `code`)", stDim)}))
	}
	body = append(body, base, textRow(base, []Span{
		sp("enter", stKey), sp(" save  ", stDim), sp("alt+enter", stKey), sp(" newline  ", stDim), sp("ctrl+z/y", stKey), sp(" undo/redo  ", stDim), sp("esc", stKey), sp(" cancel  ", stDim), sp("<<note>>", stNote), sp(" for the agent", stDim),
	}))
	for _, r := range body {
		r.Prefix = indent + bst.Render("│") + " "
		r.Suffix = " " + bst.Render("│")
		rows = append(rows, r)
	}
	return append(rows, m.cardEdge(base, indent, bst, "╰", nil, "╯", cw, nil))
}

// editRevision edits f's Revision text in place; the Feedback body is
// untouched until A.
func (m *Model) editRevision(f *session.Feedback) tea.Cmd {
	r := f.Revision
	switch {
	case r == nil:
		return nil
	case r.Text == "" && len(r.Questions) > 0:
		return m.flashCmd(f.ID + "'s Revision is questions: R answers them")
	}
	var cmd tea.Cmd
	if m.fbEdit != nil {
		cmd = m.finishFeedbackEdit(true)
	}
	m.fbEdit = &fbEditor{fb: f, rev: true, ta: newTextarea(60, "The revised Feedback…", r.Text)}
	m.undo = undoStack{}
	m.sel = Selection{}
	m.focus = FocusCentre
	m.dirty = true
	return cmd
}
