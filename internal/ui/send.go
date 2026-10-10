package ui

// Send ready Feedback (P): to GitHub as a review, or handed to the
// Implementing Agent.

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

const (
	destGitHub = iota
	destAgent
)

var reviewEvents = []string{"Comment", "Request changes", "Approve"}

const ownPRNote = "Your own PR: GitHub allows only Comment (not Request changes or Approve)"

// eventsDisabled greys out Request changes and Approve on the Reviewer's
// own PR: GitHub refuses them there.
func (m *Model) eventsDisabled() []bool {
	own := m.sess.ReviewerIsAuthor
	return []bool{false, own, own}
}

type sendState struct {
	dest    int
	event   int
	destHL  int // the highlighted (not yet selected) radio option
	eventHL int
	focus   int // sendFocus*
	items   []*session.Feedback
	editing bool // the summary textarea has the keys
	summary string
	done    []string // after sending: what happened
	posting bool     // the review is on its way to GitHub
}

// Focus groups, in tab order (Review type and Summary only for GitHub).
const (
	sendFocusDest = iota
	sendFocusEvent
	sendFocusSummary
	sendFocusSend
	sendFocusCancel
)

func (st *sendState) groups() []int {
	if st.dest == destGitHub {
		return []int{sendFocusDest, sendFocusEvent, sendFocusSummary, sendFocusSend, sendFocusCancel}
	}
	return []int{sendFocusDest, sendFocusSend, sendFocusCancel}
}

func (st *sendState) moveFocus(d int) {
	gs := st.groups()
	i := 0
	for k, g := range gs {
		if g == st.focus {
			i = k
		}
	}
	st.focus = gs[(i+d+len(gs))%len(gs)]
	st.destHL, st.eventHL = st.dest, st.event // a group opens on its selection
}

// openSend opens the dialog on the ready items; drafts can never be sent.
func (m *Model) openSend() tea.Cmd {
	if !m.sess.CanSend() {
		return m.flashCmd("The PR is " + m.sess.PRState() + ": Feedback can't be sent (no GitHub review, no Hand-off)")
	}
	items := m.sess.Ready()
	if len(items) == 0 {
		return m.flashCmd("Nothing ready: mark Feedback ready with space")
	}
	dest := destGitHub
	if m.sess.ReviewerIsAuthor {
		dest = destAgent
	}
	m.send = sendState{dest: dest, destHL: dest, items: items, summary: m.send.summary, focus: sendFocusDest}
	m.overlay = OverlaySend
	return nil
}

func (m *Model) sendBlocked() string { return m.sendBlockedFor(m.send.dest) }

func (m *Model) sendBlockedFor(dest int) string {
	if dest != destGitHub {
		return ""
	}
	ids, n := engine.UnfilledNotes(m.send.items)
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("%s: %d Agent Note%s unfilled — Refine or remove them", strings.Join(ids, ", "), n, plural(n))
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// focusMark is the focused group's marker in front of its label.
func focusMark(on bool) string {
	if on {
		return stAccent.Render("▸ ")
	}
	return "  "
}

// radio draws a radio group: sel is selected (●), hl highlighted (while
// the group has focus). Each option is clickable: it selects.
// disabled options are greyed out and can't be picked.
func (m *Model) radio(opts []string, disabled []bool, sel, hl int, focused bool, x, y int, pick func(*Model, int) tea.Cmd) string {
	var b strings.Builder
	for i, o := range opts {
		mark := "○ "
		st := stSub
		if i == sel {
			mark, st = "● ", stBold
		}
		off := i < len(disabled) && disabled[i]
		if off {
			st = stFaint.Strikethrough(true)
		}
		seg := st.Render(mark + o)
		if focused && i == hl && !off {
			seg = lg.NewStyle().Foreground(lg.Color(cCrust)).Background(lg.Color(cBlue)).Bold(true).Render(mark + o)
		}
		ii := i
		m.dialogZones = append(m.dialogZones, zone{X0: x, X1: x + cellWidth(seg), Y: y, Fn: func(mm *Model) tea.Cmd { return pick(mm, ii) }})
		b.WriteString(seg + "   ")
		x += cellWidth(seg) + 3
	}
	return b.String()
}

const summaryRows = 3

// bgInput is the send dialog's Summary box background: a shade off the
// dialog's own, so it reads as an input.
const bgInput = cSurface0

// sendBody is the part of the dialog that depends on the destination.
// With zones false it only measures (for the steady height).
func (m *Model) sendBody(dest int, iw int, y0 int, zones bool) []string {
	st := &m.send
	saved := len(m.dialogZones)
	var body []string
	y := func() int { return y0 + len(body) }
	switch dest {
	case destGitHub:
		lead := focusMark(st.focus == sendFocusEvent && zones) + stSub.Render("Review  ")
		body = append(body, lead+m.radio(reviewEvents, m.eventsDisabled(), st.event, st.eventHL, st.focus == sendFocusEvent, 2+cellWidth(lead), y(), func(mm *Model, i int) tea.Cmd {
			if mm.eventsDisabled()[i] {
				return mm.flashCmd(ownPRNote)
			}
			mm.send.event, mm.send.eventHL, mm.send.focus = i, i, sendFocusEvent
			return nil
		}))
		if m.sess.ReviewerIsAuthor {
			body = append(body, stDim.Render("  "+ownPRNote))
		} else {
			body = append(body, "")
		}
		sf := st.focus == sendFocusSummary && zones
		body = append(body, focusMark(sf)+stSub.Render("Summary (optional)")+stDim.Render("  enter edits"))
		top := y()
		// The box has its own background, so it reads as an input.
		var box []string
		if st.editing {
			m.sendTA.SetWidth(iw - 4)
			box = strings.Split(m.sendTA.View(), "\n")
		} else {
			box = wrapText(st.summary, stText, iw-4, "")
			if st.summary == "" {
				box = []string{stDim.Render("none")}
			}
		}
		for len(box) < summaryRows {
			box = append(box, "")
		}
		for _, l := range box[:summaryRows] {
			body = append(body, "  "+paintBG(" "+l, bgInput, iw-2))
		}
		// A click in the box edits the summary and places the cursor.
		m.taZones(&m.sendTA, 5, top, iw-3, summaryRows, func(mm *Model) {
			mm.send.focus = sendFocusSummary
			if !mm.send.editing {
				mm.startSummaryEdit()
			}
		})
		body = append(body, "", stDim.Render(m.reviewShape()))
	case destAgent:
		body = append(body, stText.Render("Queues these for the Implementing Agent. Agent Notes are fine: it fills them."))
		body = append(body, stDim.Render("It pulls them in with /pull-review, which runs `pr-review export` in your checkout."))
	}
	if b := m.sendBlockedFor(dest); b != "" {
		body = append(body, "", stNeedsYou.Render("⚠ "+b))
	}
	if !zones {
		m.dialogZones = m.dialogZones[:saved]
	}
	return body
}

func (m *Model) renderSend() []string {
	w := m.dialogW(96)
	iw := w - 4
	st := &m.send
	var body []string
	y := func() int { return len(body) + 2 } // dialog row of the next body line

	// Destination radio.
	lead := focusMark(st.focus == sendFocusDest) + stSub.Render("Send to  ")
	body = append(body, lead+m.radio([]string{"GitHub review", "Hand to agent"}, nil, st.dest, st.destHL, st.focus == sendFocusDest && st.done == nil, 2+cellWidth(lead), y(), func(mm *Model, i int) tea.Cmd {
		if mm.send.done == nil {
			mm.send.dest, mm.send.destHL, mm.send.focus = i, i, sendFocusDest
		}
		return nil
	}))
	body = append(body, "")

	// The ready items.
	body = append(body, stSubheading.Render(fmt.Sprintf("Ready (%d)", len(st.items))))
	maxItems := maxInt(2, m.height-26)
	for i, f := range st.items {
		if i == maxItems {
			body = append(body, stDim.Render(fmt.Sprintf("  … and %d more", len(st.items)-i)))
			break
		}
		loc := m.anchorLocation(f.Anchor)
		notes := ""
		if n := len(session.AgentNotes(f.Body)); n > 0 {
			notes = fmt.Sprintf("  ✎ %d agent note%s", n, plural(n))
		}
		head := "  " + stBold.Render(f.ID) + "  " + fg(cLavender).Render(loc) + stNoteAccent.Render(notes) + "  "
		room := iw - cellWidth(head)
		body = append(body, head+stSub.Render(truncEllipsis(oneLine(f.Body), maxInt(3, room))))
	}
	body = append(body, "")

	// The destination's part, padded to the taller of the two so the
	// dialog keeps one height.
	part := m.sendBody(st.dest, iw, y(), true)
	other := m.sendBody(1-st.dest, iw, y(), false)
	for len(part) < len(other) {
		part = append(part, "")
	}
	body = append(body, part...)
	body = append(body, "")

	if st.done != nil {
		for _, l := range st.done {
			body = append(body, wrapText(l, stAdded, iw, "")...)
		}
		body = append(body, "")
		body = append(body, m.button("Close (esc)", true, 2, y(), func(mm *Model) tea.Cmd { mm.overlay = OverlayNone; return nil }))
	} else {
		label := "Send to GitHub"
		if st.dest == destAgent {
			label = "Hand to agent"
		}
		blocked := m.sendBlocked() != ""
		var btn string
		if blocked {
			bs := lg.NewStyle().Foreground(lg.Color(cOverlay0)).Background(lg.Color(cSurface0)).Strikethrough(true)
			if st.focus == sendFocusSend {
				bs = bs.Underline(true).Bold(true)
			}
			btn = bs.Render(" " + label + " ")
			m.dialogZones = append(m.dialogZones, zone{X0: 2, X1: 2 + cellWidth(btn), Y: y(), Fn: func(mm *Model) tea.Cmd { mm.send.focus = sendFocusSend; return mm.doSend() }})
		} else {
			btn = m.button(label, st.focus == sendFocusSend, 2, y(), func(mm *Model) tea.Cmd { mm.send.focus = sendFocusSend; return mm.doSend() })
		}
		cancel := m.button("Cancel", st.focus == sendFocusCancel, 2+cellWidth(stripANSI(btn))+3, y(), func(mm *Model) tea.Cmd { mm.overlay = OverlayNone; return nil })
		body = append(body, btn+"   "+cancel)
		body = append(body, "", hints("tab", "next group", "←/→", "move", "space", "select", "enter", "press / edit", "esc", "close"))
	}
	if max := m.height - 4; len(body) > max {
		body = body[:max]
	}
	return dialog(stHeading.Render("Send ready Feedback"), body, w, cBlue)
}

func (m *Model) startSummaryEdit() tea.Cmd {
	if m.send.dest != destGitHub || m.send.done != nil {
		return nil
	}
	m.sendTA = newTextarea(m.dialogW(96)-8, "A summary for the review (optional)…", m.send.summary)
	m.sendTA.MinHeight = summaryRows
	m.sendTA.MaxHeight = summaryRows
	m.send.editing = true
	m.undo = undoStack{}
	return nil
}

func (m *Model) handleSendKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	st := &m.send
	if st.editing {
		switch key {
		case "esc":
			st.editing = false
		case "enter":
			st.summary = strings.TrimSpace(m.sendTA.Value())
			st.editing = false
		default:
			return m, m.editTA(&m.sendTA, msg)
		}
		return m, nil
	}
	if st.done != nil {
		switch key {
		case "esc", "enter", "q":
			m.overlay = OverlayNone
		}
		return m, nil
	}
	move := func(d int) {
		switch st.focus {
		case sendFocusDest:
			st.destHL = (st.destHL + d + 2) % 2
		case sendFocusEvent:
			// Greyed-out review types are skipped.
			for k := 0; k < len(reviewEvents); k++ {
				st.eventHL = (st.eventHL + d + len(reviewEvents)) % len(reviewEvents)
				if !m.eventsDisabled()[st.eventHL] {
					break
				}
			}
		case sendFocusSend, sendFocusCancel:
			if st.focus == sendFocusSend {
				st.focus = sendFocusCancel
			} else {
				st.focus = sendFocusSend
			}
		}
	}
	switch key {
	case "esc", "q":
		m.overlay = OverlayNone
	case "tab":
		st.moveFocus(1)
	case "shift+tab":
		st.moveFocus(-1)
	case "left", "up", "h", "k":
		move(-1)
	case "right", "down", "l", "j":
		move(1)
	case "space":
		switch st.focus {
		case sendFocusDest:
			st.dest = st.destHL
		case sendFocusEvent:
			st.event = st.eventHL
		}
	case "enter":
		switch st.focus {
		case sendFocusDest:
			st.dest = st.destHL
		case sendFocusEvent:
			st.event = st.eventHL
		case sendFocusSummary:
			return m, m.startSummaryEdit()
		case sendFocusSend:
			return m, m.doSend()
		case sendFocusCancel:
			m.overlay = OverlayNone
		}
	}
	return m, nil
}

func (m *Model) doSend() tea.Cmd {
	st := &m.send
	if b := m.sendBlocked(); b != "" {
		return m.flashCmd("Can't post to GitHub: " + b)
	}
	m.dirty = true
	var ids []string
	for _, f := range st.items {
		ids = append(ids, f.ID)
	}
	switch st.dest {
	case destGitHub:
		if st.posting {
			return nil
		}
		post, err := m.eng.PrepareReview(engine.PostReview{IDs: ids, Event: reviewEvents[st.event], Summary: st.summary})
		if err != nil {
			return m.flashCmd(err.Error())
		}
		st.posting = true
		return func() tea.Msg {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			res, err := post.Send(ctx)
			return postedMsg{post: post, res: res, err: err}
		}
	default:
		if _, err := m.eng.Do(engine.HandOff{IDs: ids}); err != nil {
			return m.flashCmd(err.Error())
		}
		st.done = []string{fmt.Sprintf("%s queued for the Implementing Agent.", strings.Join(ids, ", "))}
		if m.sess.Fixture {
			st.done = append(st.done, "Fixture mode has no export: `pr-review export` only hands off a real PR's Review Session, from its checkout.")
		} else {
			st.done = append(st.done, fmt.Sprintf("Paste /pull-review (copied) into your agent session in a checkout of %s on %s: it runs `pr-review export`.", m.sess.PR.OwnerRepo, m.sess.PR.HeadRef))
		}
		return copyCmd("/pull-review")
	}
}

// postedMsg: GitHub answered (or, with no Forge, the review was only
// recorded).
type postedMsg struct {
	post *engine.ReviewPost
	res  forge.Posted
	err  error
}

func (m *Model) handlePosted(msg postedMsg) tea.Cmd {
	st := &m.send
	st.posting = false
	m.dirty = true
	if msg.err != nil {
		st.done = []string{"Nothing was posted: " + oneLine(msg.err.Error()), "The items are still ready."}
		return m.flashCmd("GitHub refused the review")
	}
	if _, err := m.eng.Do(engine.ReviewSent{Post: msg.post, Result: msg.res}); err != nil {
		st.done = []string{"Posted, but not recorded: " + err.Error()}
		return m.flashCmd(err.Error())
	}
	p := msg.post
	shape := fmt.Sprintf("%d line comment%s, %d in the body", p.Lines(), plural(p.Lines()), len(p.IDs)-p.Lines())
	if p.Local() {
		st.done = []string{
			fmt.Sprintf("Marked %s posted as a “%s” review (%s).", strings.Join(p.IDs, ", "), p.Event, shape),
			"Fixture mode has no GitHub: nothing left the machine.",
		}
		return nil
	}
	st.done = []string{fmt.Sprintf("Posted %s to GitHub as a “%s” review (%s).", strings.Join(p.IDs, ", "), p.Event, shape)}
	if msg.res.URL != "" {
		st.done = append(st.done, msg.res.URL)
	}
	return m.flashCmd("Review posted")
}

// reviewShape says what the GitHub review will hold.
func (m *Model) reviewShape() string {
	if m.send.posting {
		return "Posting to GitHub…"
	}
	var ids []string
	for _, f := range m.send.items {
		ids = append(ids, f.ID)
	}
	post, err := m.eng.PrepareReview(engine.PostReview{IDs: ids, Event: "Comment"})
	if err != nil {
		return "Submitted at once (never left pending) as one review."
	}
	lines := post.Lines()
	where := "Submitted at once as one review"
	if post.Local() {
		where = "Fixture mode: recorded as posted, nothing leaves the machine"
	}
	return fmt.Sprintf("%s · %d line comment%s, %d in the body", where, lines, plural(lines), len(ids)-lines)
}
