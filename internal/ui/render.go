package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type rect struct{ X, Y, W, H int }

func (r rect) inner() rect { return rect{r.X + 1, r.Y + 1, maxInt(0, r.W-2), maxInt(0, r.H-2)} }

func (r rect) contains(x, y int) bool { return x >= r.X && x < r.X+r.W && y >= r.Y && y < r.Y+r.H }

func (m *Model) defaultNavW() int {
	if m.width < 120 {
		return 28
	}
	return 34
}

// navWidth is the left column's width: the user's dragged width, or the
// default, kept within the minimums (left ≥ 20, centre ≥ 60).
func (m *Model) navWidth() int {
	w := m.defaultNavW()
	if m.leftW > 0 {
		w = m.leftW
	}
	return clamp(w, minLeftW, maxInt(minLeftW, m.width-minCentreW))
}

// layout returns the outer (bordered) rectangles of the three panes: the
// navigator above the list pane on the left, the centre at full height on
// the right. On a short terminal the list pane gives up rows first (down to
// minListItems), then the navigator (it keeps minNavRows while it can).
func (m *Model) layout() (nav, centre, panel rect) {
	navW := m.navWidth()
	bodyTop, bodyH := 1+m.bannerH(), maxInt(0, m.height-2-m.bannerH())
	panelH := 0
	if m.panelVisible {
		items := listItems
		if m.listN > 0 {
			items = m.listN
		}
		panelH = items + 2
		// The navigator keeps minNavRows inner rows; the list shrinks first.
		panelH = minInt(panelH, bodyH-(minNavRows+2))
		panelH = maxInt(panelH, minListItems+2)
		// Then the navigator shrinks, but never below one row.
		panelH = minInt(panelH, maxInt(0, bodyH-3))
	}
	nav = rect{0, bodyTop, navW, bodyH - panelH}
	panel = rect{0, bodyTop + nav.H, navW, panelH}
	centre = rect{navW, bodyTop, maxInt(0, m.width-navW), bodyH}
	return
}

func (m *Model) centreInner() rect { _, c, _ := m.layout(); return c.inner() }
func (m *Model) panelInner() rect  { _, _, p := m.layout(); return p.inner() }
func (m *Model) navInner() rect    { n, _, _ := m.layout(); return n.inner() }

func (m *Model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeAllMotion // all motion, for the resize borders' hover
	v.ReportFocus = true                 // regaining focus checks the PR's head
	if m.width == 0 {
		v.SetContent("")
		return v
	}
	v.SetContent(m.render())
	return v
}

func (m *Model) render() string {
	m.ensureRows()
	m.zones = m.zones[:0]
	m.dialogZones = m.dialogZones[:0]
	w, h := m.width, m.height
	nav, centre, panel := m.layout()

	frame := make([]string, h)
	frame[0] = m.renderHeader(w)
	if m.bannerH() > 0 && h > 2 {
		frame[1] = m.renderBanner(w)
	}
	frame[h-1] = m.renderFooter(w)

	left := m.renderNavBox(nav)
	if panel.H > 0 {
		left = append(left, m.renderPanelBox(panel)...)
	}
	right := m.renderCentreBox(centre)
	for i := 0; i < centre.H; i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		frame[centre.Y+i] = padTo(l, nav.W) + padTo(r, centre.W)
	}

	m.accentBorder(frame)

	if m.sel.Active && !m.sel.Dragging && m.overlay == OverlayNone && m.fbEdit == nil {
		frame = m.overlayActionBar(frame)
	}

	var box []string
	switch m.overlay {
	case OverlayHelp:
		box = m.renderHelp()
	case OverlayTextbox:
		box = m.renderTextbox()
	case OverlayConfirm:
		box = m.renderConfirm()
	case OverlaySend:
		box = m.renderSend()
	}
	if box != nil {
		frame = m.overlayCentered(frame, box)
	}

	for i, l := range frame {
		frame[i] = padTo(l, w)
	}
	return strings.Join(frame, "\n")
}

// drawBox frames content (already inner-width lines) in a rounded border
// with a title on the top edge and a note on the bottom edge.
func drawBox(r rect, content []string, title, note string, focused bool) []string {
	if r.W < 2 || r.H < 2 {
		return nil
	}
	bc := fg(cSurface1)
	if focused {
		bc = fg(cBlue)
	}
	iw := r.W - 2
	edge := func(left, right, label string) string {
		label = truncEllipsisANSI(label, maxInt(0, iw-2))
		fill := iw - 1 - cellWidth(label)
		if label == "" {
			return bc.Render(left + strings.Repeat("─", iw) + right)
		}
		return bc.Render(left+"─") + label + bc.Render(strings.Repeat("─", maxInt(0, fill))+right)
	}
	out := make([]string, 0, r.H)
	out = append(out, edge("╭", "╮", title))
	side := bc.Render("│")
	for i := 0; i < r.H-2; i++ {
		l := ""
		if i < len(content) {
			l = content[i]
		}
		out = append(out, side+padTo(l, iw)+side)
	}
	out = append(out, edge("╰", "╯", note))
	return out
}

func truncEllipsisANSI(s string, w int) string {
	if cellWidth(s) <= w {
		return s
	}
	if w <= 1 {
		return ""
	}
	return ansi.Truncate(s, w-1, "") + "…"
}

// ---- header & footer ----

func (m *Model) renderHeader(w int) string {
	pr := m.sess.PR
	var left string
	if m.repoMode {
		left = stBold.Render(" "+pr.Title) + stDim.Render(fmt.Sprintf("  %s..%s", pr.BaseRef, pr.HeadRef))
	} else {
		left = stAccent.Render(fmt.Sprintf(" %s #%d", ownerRepoShort(pr.OwnerRepo), pr.Number)) + "  " +
			stBold.Render(pr.Title) + stDim.Render(fmt.Sprintf("  %s · %s→%s · %d files ", pr.Author,
			shortSHA(pr.BaseSHA), shortSHA(pr.HeadSHA), len(pr.Files))) +
			stAdded.Render(fmt.Sprintf("+%d", pr.Additions)) + " " + stRemoved.Render(fmt.Sprintf("−%d", pr.Deletions))
	}
	c := m.sess.Counts()
	var parts []string
	switch {
	case m.sess.ReadOnly():
		parts = append(parts, lg.NewStyle().Foreground(lg.Color(cCrust)).Background(lg.Color(cOverlay1)).Bold(true).Render(" READ-ONLY · "+strings.ToUpper(m.sess.Finished)+" "))
	case m.sess.PRState() != "open":
		parts = append(parts, lg.NewStyle().Foreground(lg.Color(cCrust)).Background(lg.Color(cMauve)).Bold(true).Render(" "+strings.ToUpper(m.sess.PRState())+" "))
	}
	if m.holdOn {
		parts = append(parts, lg.NewStyle().Foreground(lg.Color(cCrust)).Background(lg.Color(cYellow)).Bold(true).Render(" HOLD "))
	}
	if c.Failed > 0 {
		parts = append(parts, stNeedsYou.Render(fmt.Sprintf("! %d failed", c.Failed)))
	}
	if c.New > 0 {
		parts = append(parts, stNew.Render(fmt.Sprintf("● %d new", c.New)))
	}
	if c.Drafts > 0 {
		parts = append(parts, stDraft.Render(fmt.Sprintf("○ %d drafts", c.Drafts)))
	}
	run := fmt.Sprintf("◐ %d/%d running", c.Running, c.MaxRunning)
	if c.Queued > 0 {
		run += fmt.Sprintf(" · %d queued", c.Queued)
	}
	if c.Running > 0 || c.Queued > 0 {
		parts = append(parts, stRunning.Render(run))
	} else {
		parts = append(parts, stDim.Render(run))
	}
	right := strings.Join(parts, stFaint.Render("  ")) + " "
	avail := w - cellWidth(right) - 2
	if cellWidth(left) > avail {
		left = truncEllipsisANSI(left, maxInt(1, avail))
	}
	gap := maxInt(1, w-cellWidth(left)-cellWidth(right))
	return paintBG(left+strings.Repeat(" ", gap)+right, cMantle, w)
}

func ownerRepoShort(s string) string {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[i+1:]
	}
	return s
}

func shortSHA(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

func hints(pairs ...string) string {
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, stKey.Render(pairs[i])+" "+stSub.Render(pairs[i+1]))
	}
	return strings.Join(parts, stFaint.Render("  ·  "))
}

// costText is the Review Session's running agent cost, for the footer.
func (m *Model) costText() string {
	return fmt.Sprintf("$%.2f", m.sess.TotalCost)
}

func (m *Model) renderFooter(w int) string {
	right := stSub.Render(m.costText()) + stFaint.Render("  ·  ") + hints("?", "help", "q", "quit") + " "
	var left string
	switch {
	case m.search.prompt:
		left = m.searchFooter()
		right = ""
	case m.flash != "":
		left = fg(cYellow).Bold(true).Render(" " + m.flash)
	case m.overlay == OverlayTextbox:
		left = " " + hints("enter", "submit", "alt+enter", "newline", "esc", "cancel")
		right = ""
	case m.fbEdit != nil:
		what := "Editing " + m.fbEdit.fb.ID
		if m.fbEdit.rev {
			what += "'s Revision"
		}
		left = " " + stFeedback.Render(what) + "   " + hints("enter", "save", "alt+enter", "newline", "ctrl+z/y", "undo/redo", "esc", "cancel")
		right = ""
	case m.search.hl:
		left = m.searchFooter()
	case m.sel.Active:
		left = " " + hints("d s x t c", "ask", "r", "Ask…", "f", "Feedback", "y", "copy", "esc", "clear")
	case m.focus == FocusNav:
		left = " " + hints("j/k", "move", "enter", "open", "space", "expand", "d s x t c r f y", "act on entry", "tab", "pane")
	case m.focus == FocusPanel && m.panelTab == TabThreads:
		left = " " + hints("enter", "jump to", "R", "follow-up", "Q", "re-ask Stale", "X", "cancel/delete", "H", "send drafts", "2", "Feedback")
	case m.focus == FocusPanel:
		left = " " + hints("enter", "jump to", "e", "edit", "r", "refine", "A", "accept", "space", "ready↔draft", "P", "send", "X", "delete", "1", "Threads")
	default:
		left = " " + hints("J/K", "method", "}/{", "file", "]/[", "change", "/", "search", "n/p", "match", "ctrl+o/alt+←", "back", "v", "select", "d s x t c r f y", "act on line", "enter", "open/toggle", "E", "full file")
	}
	avail := w - cellWidth(right) - 1
	if cellWidth(left) > avail {
		left = truncEllipsisANSI(left, maxInt(1, avail))
	}
	gap := maxInt(1, w-cellWidth(left)-cellWidth(right))
	return paintBG(left+strings.Repeat(" ", gap)+right, cMantle, w)
}

// ---- the action bar ----

type barItem struct{ key, long, short string }

var barItems = []barItem{
	{"d", "Define", "def"}, {"s", "Summarize", "sum"}, {"x", "Sample Data", "data"},
	{"t", "Trace", "trace"}, {"c", "Test Cases", "tests"}, {"r", "Ask…", "ask"},
	{"f", "Feedback", "fb"}, {"y", "Copy", "copy"},
}

func (m *Model) overlayActionBar(frame []string) []string {
	if m.sel.Pane != FocusCentre {
		return frame
	}
	pane, scroll := m.centreInner(), m.centreScroll
	if pane.H <= 0 {
		return frame
	}
	r0, _, r1, _ := m.sel.Bounds()
	y := pane.Y + (r1 - scroll) + 1
	if y >= pane.Y+pane.H || y < pane.Y {
		y = pane.Y + (r0 - scroll) - 1
	}
	if y < pane.Y || y >= pane.Y+pane.H {
		y = pane.Y + pane.H - 1
	}

	build := func(long bool) (string, [][2]int) {
		var b strings.Builder
		var spans [][2]int
		x := 1
		b.WriteString(" ")
		for i, it := range barItems {
			label := it.short
			if long {
				label = it.long
			}
			seg := stKey.Render(it.key) + " " + fg(cText).Render(label)
			segW := cellWidth(it.key) + 1 + cellWidth(label)
			spans = append(spans, [2]int{x, x + segW})
			b.WriteString(seg)
			x += segW
			if i < len(barItems)-1 {
				b.WriteString(stDim.Render("  "))
				x += 2
			}
		}
		b.WriteString(" ")
		return b.String(), spans
	}
	bar, spans := build(true)
	if cellWidth(bar) > pane.W {
		bar, spans = build(false)
	}
	bar = paintBG(bar, cSurface1, minInt(cellWidth(bar), pane.W))
	frame[y] = overlayLine(frame[y], pane.X, bar)
	for i, s := range spans {
		key := barItems[i].key
		m.zones = append(m.zones, zone{X0: pane.X + s[0], X1: pane.X + s[1], Y: y, Fn: func(mm *Model) tea.Cmd {
			return mm.tryActionKey(key)
		}})
	}
	return frame
}

func overlayLine(line string, x int, content string) string {
	w := cellWidth(line)
	cw := cellWidth(content)
	left := ansi.Cut(line, 0, clamp(x, 0, w))
	right := ansi.Cut(line, clamp(x+cw, 0, w), w)
	return left + content + right
}

// overlayCentered draws box over the middle of frame and returns frame.
// It also records the box's rectangle so clicks inside it can be routed.
func (m *Model) overlayCentered(frame []string, box []string) []string {
	bw := 0
	for _, l := range box {
		bw = maxInt(bw, cellWidth(l))
	}
	x := clamp((m.width-bw)/2, 0, maxInt(0, m.width-bw))
	y := clamp((m.height-len(box))/2, 0, maxInt(0, m.height-len(box)))
	for i, l := range box {
		if row := y + i; row >= 0 && row < len(frame) {
			frame[row] = overlayLine(frame[row], x, padTo(l, bw))
		}
	}
	m.dialogRect = rect{x, y, bw, len(box)}
	// While a dialog is open only its own buttons are clickable.
	m.zones = m.zones[:0]
	for _, z := range m.dialogZones {
		m.zones = append(m.zones, zone{X0: x + z.X0, X1: x + z.X1, Y: y + z.Y, Fn: z.Fn})
	}
	return frame
}
