package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// dialog frames body (lines already fitting w-4 cells) in a rounded box of
// width w on a dark background.
func dialog(title string, body []string, w int, color string) []string {
	bst := fg(color).Background(lg.Color(cMantle))
	iw := w - 4
	tw := cellWidth(title)
	top := bst.Render("╭─ ") + title + bst.Render(" "+strings.Repeat("─", maxInt(0, w-tw-5))+"╮")
	out := []string{paintBG(top, cMantle, w)}
	row := func(l string) string {
		return paintBG(bst.Render("│")+" "+padTo(l, iw)+" "+bst.Render("│"), cMantle, w)
	}
	out = append(out, row(""))
	for _, l := range body {
		out = append(out, row(l))
	}
	out = append(out, row(""), paintBG(bst.Render("╰"+strings.Repeat("─", w-2)+"╯"), cMantle, w))
	return out
}

func (m *Model) dialogW(want int) int { return clamp(want, 30, m.width-4) }

// wrapText word-wraps plain text in one style into rendered lines.
func wrapText(text string, st lg.Style, w int, indent string) []string {
	var out []string
	for _, para := range strings.Split(text, "\n") {
		for _, l := range wrapSpans([]Span{sp(para, st)}, w-cellWidth(indent), w-cellWidth(indent)) {
			out = append(out, indent+renderSpans(l, "", 0, 0, "", 0, 0))
		}
	}
	return out
}

// button renders a dialog button and registers its click zone (relative to
// the dialog box; overlayCentered makes it absolute).
func (m *Model) button(label string, focused bool, x, y int, fn func(*Model) tea.Cmd) string {
	st := lg.NewStyle().Foreground(lg.Color(cText)).Background(lg.Color(cSurface1))
	if focused {
		st = lg.NewStyle().Foreground(lg.Color(cCrust)).Background(lg.Color(cBlue)).Bold(true)
	}
	s := st.Render(" " + label + " ")
	m.dialogZones = append(m.dialogZones, zone{X0: x, X1: x + cellWidth(s), Y: y, Fn: fn})
	return s
}

// ---- Help ----

type helpSection struct {
	name    string
	entries [][2]string // key, description
}

var helpSections = []helpSection{
	{"Anywhere", [][2]string{
		{"tab / shift+tab", "focus navigator → centre → Threads/Feedback list"},
		{"z", "collapse / expand every card"},
		{"h   H", "Hold on/off · send every held Draft"},
		{"U", "update to the PR's new head (the banner says when there are new commits)"},
		{"S", "Since last review: the diff from the head you last updated from"},
		{"1 2   b", "list: Threads · Feedback · show/hide the list"},
		{"P", "send ready Feedback (GitHub review or Hand to agent)"},
		{"esc", "clear search highlights, then the selection / close"},
		{"drag a border", "resize the left column or the list · double-click resets"},
	}},
	{"Selecting and asking", [][2]string{
		{"drag · dbl · tpl", "select text · a word · a line (mouse)"},
		{"v then j/k", "select whole lines (keyboard)"},
		{"d s x t c", "Define · Summarize · Sample Data · Trace · Test Cases"},
		{"r   f   y", "Ask… (Research or Before/After, tab switches) · Feedback (in place) · Copy"},
	}},
	{"Centre", [][2]string{
		{"j/k  ctrl+d/u  g G", "move · half page · top · bottom (the wheel scrolls)"},
		{"] [", "next / previous change across the PR (changed runs and top-level declarations)"},
		{"J K   } {", "Touched Method · file (reading order)"},
		{"/   n  N or p", "search this view (smart case) · next / previous match"},
		{"ctrl+o  alt+←", "back to where the last jump started (mouse: Back button)"},
		{"alt+→", "forward again (mouse: Forward button)"},
		{"E   enter", "hunks ↔ full file · open/toggle the row, follow a link"},
		{"enter on ↑ callers", "Go: expand / collapse the method's exact callers across its module (… N more shows the rest)"},
		{"enter on a caller", "jump to it; a file outside the PR opens read-only at the head · marks: test (a _test.go file), ref (a method or function value, not a call), via interface X (a call through an interface the type satisfies)"},
	}},
	{"Threads & Feedback (on a card, or in the list)", [][2]string{
		{"enter / dbl-click", "list: jump to it · card: open / edit"},
		{"R  X", "follow-up · cancel queued / delete Draft (a running Question can't be cancelled)"},
		{"Q", "ask a Stale Question again at the new head (never automatic)"},
		{"x on a Trace", "sample this path: a Sample Data follow-up in the Thread"},
		{"enter on ▸ detail", "expand / collapse an Answer's detail"},
		{"e  space  X", "edit Feedback in place · ready ↔ draft · delete"},
		{"r", "Refine the Feedback: the agent proposes a Revision under it"},
		{"on a Revision: A e d X", "accept it into the Feedback · edit the Revision · word diff ↔ revised text · ignore"},
		{"<<note>>", "an Agent Note in Feedback: Refine fills it; it blocks GitHub, not Hand-off"},
		{"D  O", "handed-off Feedback: mark done · reopen as a draft (whatever its Outcome says; O also takes back a queued item)"},
	}},
	{"Text editors (Question, Feedback, Revision, summary)", [][2]string{
		{"enter  alt+enter  esc", "save / submit · newline · cancel"},
		{"click", "place the cursor"},
		{"ctrl+z", "undo (a word at a time)"},
		{"ctrl+y  ctrl+shift+z", "redo"},
	}},
}

// helpGap is the minimum space between a key and its description.
const helpGap = 3

// helpBody lays the sections out: in each section the descriptions start in
// one column, helpGap past its longest key, and wrap under themselves.
func helpBody(iw int) []string {
	var body []string
	for si, sec := range helpSections {
		if si > 0 {
			body = append(body, "")
		}
		body = append(body, stSubheading.Render(sec.name))
		keyW := 0
		for _, e := range sec.entries {
			keyW = maxInt(keyW, cellWidth(e[0]))
		}
		col := 2 + keyW + helpGap
		for _, e := range sec.entries {
			lines := wrapSpans([]Span{sp(e[1], stText)}, maxInt(10, iw-col), maxInt(10, iw-col))
			for li, l := range lines {
				lead := strings.Repeat(" ", col)
				if li == 0 {
					lead = "  " + stKey.Render(padTo(e[0], keyW)) + strings.Repeat(" ", helpGap)
				}
				body = append(body, lead+renderSpans(l, "", 0, 0, "", 0, 0))
			}
		}
	}
	legend := []Span{sp("✅ ", stText), sp("Verified: the agent ran something that confirms it (its card lists what ran)   ", stText), sp("📖 ", stText), sp("Unverified: from reading only", stText)}
	for _, l := range wrapSpans(legend, iw-2, iw-2) {
		body = append(body, "  "+renderSpans(l, "", 0, 0, "", 0, 0))
	}
	marks := []Span{sp("●", stNew), sp(" Thread  ", stText), sp("✎", stFeedback), sp(" Feedback  ", stText), sp("!", stNeedsYou), sp(" the agent failed  ", stText), sp("● new", stNew), sp(" unread Answer  ", stText), sp("⚙", stGear), sp(" computed locally  ", stText),
		sp("⚠ Stale", stStale), sp(" its lines changed at a newer head  ", stText), sp("Δ", stStale), sp(" changed since your last review  ", stText), sp("💬", stComment), sp(" a PR Comment (read-only)", stText)}
	for _, l := range wrapSpans(marks, iw-2, iw-2) {
		body = append(body, "  "+renderSpans(l, "", 0, 0, "", 0, 0))
	}
	cost := []Span{sp("$0.42", stSub), sp(" in the footer: what the agent has cost this Review Session so far. Every call has a spending cap (settings: budgets).", stText)}
	for _, l := range wrapSpans(cost, iw-2, iw-2) {
		body = append(body, "  "+renderSpans(l, "", 0, 0, "", 0, 0))
	}
	auto := []Span{sp("The agent runs in auto mode in the Review Session's own worktree: it can run tests and scratch scripts, never edit the repo, and asks you nothing. No Probe dialog.", stDim)}
	for _, l := range wrapSpans(auto, iw-2, iw-2) {
		body = append(body, "  "+renderSpans(l, "", 0, 0, "", 0, 0))
	}
	return body
}

func (m *Model) renderHelp() []string {
	w := m.dialogW(110)
	iw := w - 4
	body := helpBody(iw)
	// Short terminals: scroll (j/k, wheel) rather than clip.
	maxBody := maxInt(3, m.height-5) // the dialog adds 4 rows; the footer line is 1
	foot := stDim.Render("esc or ? closes")
	if len(body) > maxBody {
		m.helpScroll = clamp(m.helpScroll, 0, len(body)-maxBody)
		more := len(body) - maxBody - m.helpScroll
		body = body[m.helpScroll : m.helpScroll+maxBody]
		foot = stDim.Render("esc or ? closes · ") + stKey.Render("j/k") + stDim.Render(fmt.Sprintf(" scroll · %d more below", more))
		if more == 0 {
			foot = stDim.Render("esc or ? closes · ") + stKey.Render("k") + stDim.Render(" scrolls back up")
		}
	}
	for i, l := range body {
		body[i] = truncEllipsisANSI(l, iw)
	}
	body = append(body, foot)
	return dialog(stHeading.Render("Keys"), body, w, cBlue)
}

// ---- Text box ----

// newTextarea is the text input used by the Ask box and the in-place
// Feedback editor: no prompt or line numbers, enter left to the caller,
// alt+enter for a newline.
func newTextarea(width int, placeholder, initial string) textarea.Model {
	ta := textarea.New()
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.Placeholder = placeholder
	ta.KeyMap.InsertNewline = key.NewBinding(key.WithKeys("alt+enter", "ctrl+j"))
	st := ta.Styles()
	st.Cursor.Blink = false
	st.Focused.CursorLine = lg.NewStyle()
	st.Focused.Base = lg.NewStyle()
	st.Focused.Text = lg.NewStyle().Foreground(lg.Color(cText))
	st.Focused.Placeholder = lg.NewStyle().Foreground(lg.Color(cOverlay0))
	ta.SetStyles(st)
	ta.DynamicHeight = true
	ta.MinHeight = 3
	ta.MaxHeight = 12
	ta.MaxContentHeight = 500
	ta.SetWidth(width)
	ta.SetValue(initial)
	ta.Focus()
	return ta
}

func (m *Model) openTextbox(kind TextboxKind, initial string) {
	ph := "Your Question for the agent… (empty: the Kind's default Question)"
	switch kind {
	case TextboxFollowUp:
		ph = "Your follow-up…"
	case TextboxRefineAnswer:
		ph = "Your answers to the agent's questions…"
	}
	m.ta = newTextarea(m.dialogW(80)-4, ph, initial)
	m.tbKind = kind
	m.overlay = OverlayTextbox
	m.undo = undoStack{}
}

func (m *Model) renderTextbox() []string {
	w := m.dialogW(80)
	iw := w - 4
	title := "Ask (" + m.askKind.String() + ")"
	switch m.tbKind {
	case TextboxRefineAnswer:
		title = "Answer the agent"
		if m.tbFeedback != nil {
			title += " · Refine " + m.tbFeedback.ID
		}
	case TextboxFollowUp:
		title = "Follow-up"
		if m.tbTarget != nil {
			title += " · " + m.tbTarget.ID + " " + m.tbTarget.Kind.String()
		}
	case TextboxAsk:
		if m.holdOn {
			title = "Ask (" + m.askKind.String() + ", held as a Draft)"
		}
	}
	excerpt := ""
	loc := ""
	switch m.tbKind {
	case TextboxAsk:
		excerpt, loc = m.tbAnchor.Text, m.anchorLocation(m.tbAnchor)
	case TextboxFollowUp:
		if m.tbTarget != nil {
			excerpt, loc = m.tbTarget.Entries[0].Text, m.anchorLocation(m.tbTarget.Anchor)
		}
	}
	var body []string
	if m.tbKind == TextboxAsk {
		// The Ask… menu: Research or Before/After (the other five Kinds have keys).
		var line strings.Builder
		x := 2
		line.WriteString(stSub.Render("Kind "))
		x += 5
		for _, k := range []session.QuestionKind{session.Research, session.BeforeAfter} {
			st := lg.NewStyle().Foreground(lg.Color(cSubtext0)).Background(lg.Color(cSurface0))
			if k == m.askKind {
				st = lg.NewStyle().Foreground(lg.Color(cCrust)).Background(lg.Color(cBlue)).Bold(true)
			}
			seg := st.Render(" " + k.String() + " ")
			kk := k
			m.dialogZones = append(m.dialogZones, zone{X0: x, X1: x + cellWidth(seg), Y: 2, Fn: func(mm *Model) tea.Cmd { mm.askKind = kk; return nil }})
			line.WriteString(seg + " ")
			x += cellWidth(seg) + 1
		}
		body = append(body, line.String()+stDim.Render(" tab switches"), "")
	}
	if m.tbKind == TextboxRefineAnswer && m.tbFeedback != nil && m.tbFeedback.Revision != nil {
		for i, q := range m.tbFeedback.Revision.Questions {
			body = append(body, wrapText(fmt.Sprintf("%d. %s", i+1, q), stSub, iw, "")...)
		}
		body = append(body, "")
	}
	if loc != "" {
		body = append(body, fg(cLavender).Render(loc))
	}
	if ex := strings.TrimSpace(excerpt); ex != "" {
		q := wrapText("“"+oneLine(ex)+"”", stSub.Italic(true), iw, "")
		if len(q) > 3 {
			q = append(q[:2], stSub.Italic(true).Render(truncEllipsis(stripANSI(q[2]), iw-1)+"…"))
		}
		body = append(body, q...)
	}
	body = append(body, "")
	taLines := strings.Split(m.ta.View(), "\n")
	m.taZones(&m.ta, 2, len(body)+2, iw, len(taLines), nil)
	body = append(body, taLines...)
	body = append(body, "", hints("enter", "submit", "alt+enter", "newline", "esc", "cancel"))
	return dialog(stHeading.Render(title), body, w, cBlue)
}

func (m *Model) handleTextboxKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "tab", "shift+tab":
		if m.tbKind == TextboxAsk {
			if m.askKind == session.Research {
				m.askKind = session.BeforeAfter
			} else {
				m.askKind = session.Research
			}
		}
		return m, nil
	case "esc":
		m.overlay = OverlayNone
		return m, nil
	case "enter":
		text := strings.TrimSpace(m.ta.Value())
		m.overlay = OverlayNone
		return m, m.submitTextbox(text)
	}
	var cmd tea.Cmd
	cmd = m.editTA(&m.ta, msg)
	return m, cmd
}

func (m *Model) submitTextbox(text string) tea.Cmd {
	defer func() { m.sel = Selection{}; m.dirty = true }()
	switch m.tbKind {
	case TextboxAsk:
		return m.do(engine.Ask{Anchor: m.tbAnchor, Kind: m.askKind, Text: text, Hold: m.holdOn})
	case TextboxRefineAnswer:
		if f := m.tbFeedback; f != nil && text != "" {
			// Answering re-runs Refine.
			if _, err := m.eng.Do(engine.Refine{ID: f.ID, Answers: text}); err != nil {
				return m.flashCmd(err.Error())
			}
			return m.flashCmd(f.ID + ": answers sent, refining again")
		}
	case TextboxFollowUp:
		if m.tbTarget != nil && text != "" {
			return m.do(engine.FollowUp{ThreadID: m.tbTarget.ID, Kind: m.tbTarget.Kind, Text: text})
		}
	}
	return nil
}

// ---- Feedback, written in place ----

// startFeedback creates a Feedback on a and opens its editor inside its
// card, under the anchored line.
func (m *Model) startFeedback(a session.Anchor) tea.Cmd {
	cmd := m.finishFeedbackEdit(true)
	evs, err := m.eng.Do(engine.AddFeedback{Anchor: a})
	if err != nil || len(evs) == 0 {
		return tea.Batch(cmd, m.flashCmd(fmt.Sprint("Can't add Feedback: ", err)))
	}
	f := m.sess.FeedbackByID(evs[0].Data.(session.FeedbackCreated).FeedbackID)
	return tea.Batch(cmd, m.editFeedback(f, true, fbShow))
}

// How editFeedback treats the viewport.
const (
	fbInPlace = iota // the card is where you're looking: don't scroll at all
	fbShow           // scroll the minimum to bring the card and its editor into view
)

// editFeedback opens f's editor inside its card.
func (m *Model) editFeedback(f *session.Feedback, isNew bool, how int) tea.Cmd {
	if f.Status.Locked() {
		return m.flashCmd(f.ID + " is " + f.Status.String() + " and can't be edited")
	}
	var cmd tea.Cmd
	if m.fbEdit != nil && m.fbEdit.fb != f {
		cmd = m.finishFeedbackEdit(true)
	}
	m.fbEdit = &fbEditor{fb: f, isNew: isNew, ta: newTextarea(60, "What you'd post on the Pull Request…", f.Body)}
	m.undo = undoStack{}
	m.sel = Selection{}
	m.focus = FocusCentre
	if how == fbInPlace {
		m.rebuild()
		return cmd
	}
	if f.Anchor.Type == session.AnchorAnswer {
		for p := m.threadByID(f.Anchor.ThreadID); p != nil; p = m.parentThread(p) {
			m.expandedCards[p.ID] = true
		}
	}
	m.rebuild()
	first, last := -1, -1
	for i, r := range m.centreRows {
		if r.Feedback == f {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		m.openFeedback(f) // in another view: that's a jump
		return cmd
	}
	h := m.centreH()
	switch {
	case first < m.centreScroll:
		m.centreScroll = first
	case last >= m.centreScroll+h:
		m.centreScroll = minInt(first, last-h+1)
	}
	m.centreCursor = first
	m.clampCentre()
	return cmd
}

// finishFeedbackEdit closes the editor, saving its text when save is set. A
// new Feedback that's cancelled, or saved empty, is deleted.
func (m *Model) finishFeedbackEdit(save bool) tea.Cmd {
	ed := m.fbEdit
	if ed == nil {
		return nil
	}
	m.fbEdit = nil
	m.dirty = true
	text := strings.TrimSpace(ed.ta.Value())
	switch {
	case ed.rev:
		if save && text != "" && ed.fb.Revision != nil {
			if _, err := m.eng.Do(engine.EditRevision{ID: ed.fb.ID, Text: text}); err != nil {
				return m.flashCmd(err.Error())
			}
			return m.flashCmd(ed.fb.ID + "'s Revision edited · A accepts it into the Feedback")
		}
	case save && text != "":
		if _, err := m.eng.Do(engine.EditFeedback{ID: ed.fb.ID, Body: text}); err != nil {
			return m.flashCmd(err.Error())
		}
		if ed.isNew {
			return m.flashCmd(ed.fb.ID + " added as a Feedback draft")
		}
		return m.flashCmd(ed.fb.ID + " saved")
	case ed.isNew:
		m.eng.Do(engine.DeleteFeedback{ID: ed.fb.ID})
	}
	return nil
}

func (m *Model) handleFeedbackEditKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		return m, m.finishFeedbackEdit(false)
	case "enter":
		return m, m.finishFeedbackEdit(true)
	}
	var cmd tea.Cmd
	cmd = m.editTA(&m.fbEdit.ta, msg)
	return m, cmd
}

// ---- Confirm ----

func (m *Model) renderConfirm() []string {
	w := m.dialogW(maxInt(40, cellWidth(m.confirmMsg)+8))
	yes := m.button("Delete (y)", false, 2, 4, func(mm *Model) tea.Cmd { return mm.confirm(true) })
	no := m.button("Keep (n)", true, 2+cellWidth(stripANSI(yes))+3, 4, func(mm *Model) tea.Cmd { return mm.confirm(false) })
	return dialog(stNeedsYou.Render("Confirm"), []string{stText.Render(m.confirmMsg), "", yes + "   " + no}, w, cRed)
}

func (m *Model) confirm(yes bool) tea.Cmd {
	fn := m.confirmFn
	m.overlay = OverlayNone
	m.dirty = true
	if yes && fn != nil {
		return fn(m)
	}
	return nil
}

func (m *Model) handleConfirmKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y":
		return m, m.confirm(true)
	case "n", "esc", "enter":
		return m, m.confirm(false)
	}
	return m, nil
}
