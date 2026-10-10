package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// paneAt maps screen coordinates to a pane and pane-inner coordinates. The
// pane's border counts as part of the pane for the wheel (inner=false).
func (m *Model) paneAt(x, y int) (pane Focus, lx, ly int, inner bool, ok bool) {
	nav, centre, panel := m.layout()
	for _, p := range []struct {
		f Focus
		r rect
	}{{FocusNav, nav}, {FocusCentre, centre}, {FocusPanel, panel}} {
		if p.r.H == 0 || !p.r.contains(x, y) {
			continue
		}
		in := p.r.inner()
		return p.f, x - in.X, y - in.Y, in.contains(x, y), true
	}
	return 0, 0, 0, false, false
}

// hitRow maps pane-inner coordinates to a row index and a rune column in
// that row's text.
func (m *Model) hitRow(pane Focus, lx, ly int) (row, col int, ok bool) {
	switch pane {
	case FocusNav:
		row = ly + m.navScroll
		return row, 0, row >= 0 && row < len(m.navEntries)
	case FocusCentre:
		row = ly + m.centreScroll
		if row < 0 || row >= len(m.centreRows) {
			return 0, 0, false
		}
		r := m.centreRows[row]
		return row, colToRune(r.text(), lx-r.prefixWidth(true)), true
	case FocusPanel:
		row = ly + m.panelScroll
		if row < 0 || row >= len(m.panelRows) {
			return 0, 0, false
		}
		r := m.panelRows[row]
		return row, colToRune(r.text(), lx-r.prefixWidth(false)), true
	}
	return 0, 0, false
}

func (m *Model) registerClick(x, y int, pane Focus) int {
	now := time.Now()
	lc := &m.lastClick
	if pane == lc.Pane && abs(x-lc.X) <= 1 && abs(y-lc.Y) <= 1 && now.Sub(lc.At) < 400*time.Millisecond {
		lc.Count++
		if lc.Count > 3 {
			lc.Count = 1
		}
	} else {
		lc.Count = 1
	}
	lc.X, lc.Y, lc.At, lc.Pane = x, y, now, pane
	return lc.Count
}

func isWordChar(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
}

// wordBounds finds the word around col: [A-Za-z0-9_], plus '-' and '.'
// when both neighbours are word characters.
func wordBounds(text string, col int) (int, int) {
	rs := []rune(text)
	if len(rs) == 0 {
		return 0, 0
	}
	col = clamp(col, 0, len(rs)-1)
	at := func(i int) rune {
		if i < 0 || i >= len(rs) {
			return 0
		}
		return rs[i]
	}
	isW := func(i int) bool {
		if i < 0 || i >= len(rs) {
			return false
		}
		if isWordChar(rs[i]) {
			return true
		}
		return (rs[i] == '-' || rs[i] == '.') && isWordChar(at(i-1)) && isWordChar(at(i+1))
	}
	if !isW(col) {
		return col, col + 1
	}
	s, e := col, col+1
	for s > 0 && isW(s-1) {
		s--
	}
	for e < len(rs) && isW(e) {
		e++
	}
	return s, e
}

// pendingClick is a single click waiting for its release: if the mouse
// didn't drag, the release follows the row's link or runs its Activate.
type pendingClick struct {
	active   bool
	pane     Focus
	row, col int
	marker   bool
}

func (m *Model) handleMouseClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	mm := msg.Mouse()
	switch mm.Button {
	case tea.MouseBackward, tea.MouseForward:
		// Back / Forward work wherever the pointer is.
		if m.overlay != OverlayNone {
			return m, nil
		}
		saved := m.finishFeedbackEdit(true)
		if mm.Button == tea.MouseBackward {
			return m, tea.Batch(saved, m.goBack())
		}
		return m, tea.Batch(saved, m.goForward())
	case tea.MouseLeft:
	default:
		return m, nil
	}
	m.render() // hit zones and scroll state as currently drawn
	m.pending = pendingClick{}

	if m.overlay != OverlayNone {
		for _, z := range m.zones {
			if z.Y == mm.Y && mm.X >= z.X0 && mm.X < z.X1 {
				m.zoneDX = mm.X - z.X0
				return m, z.Fn(m)
			}
		}
		if m.overlay == OverlayHelp && !m.dialogRect.contains(mm.X, mm.Y) {
			m.overlay = OverlayNone
		}
		return m, nil
	}
	for _, z := range m.zones {
		if z.Y == mm.Y && mm.X >= z.X0 && mm.X < z.X1 {
			return m, z.Fn(m) // action-bar items act on the selection
		}
	}
	if b := m.borderAt(mm.X, mm.Y); b != borderNone {
		m.pressBorder(b, mm.X, mm.Y)
		return m, nil
	}

	pane, lx, ly, inner, ok := m.paneAt(mm.X, mm.Y)
	if !ok || !inner {
		return m, nil
	}
	count := m.registerClick(mm.X, mm.Y, pane)
	row, col, rowOK := m.hitRow(pane, lx, ly)

	// Clicking anywhere but the Feedback being edited saves it and closes
	// the editor (like leaving a text field).
	var saved tea.Cmd
	if m.fbEdit != nil {
		if pane == FocusCentre && rowOK && m.centreRows[row].Feedback == m.fbEdit.fb {
			// Click to place the cursor; no selection inside an input.
			if r := m.centreRows[row]; r.TALine > 0 {
				dx := lx - r.prefixWidth(true)
				placeCursor(&m.fbEdit.ta, r.TALine-1+m.fbEdit.ta.ScrollYOffset(), dx)
			}
			return m, nil
		}
		saved = m.finishFeedbackEdit(true)
		m.rebuild()
		row, col, rowOK = m.hitRow(pane, lx, ly)
	}
	_, cmd := m.clickPane(pane, lx, count, row, col, rowOK)
	return m, tea.Batch(saved, cmd)
}

func (m *Model) clickPane(pane Focus, lx, count, row, col int, rowOK bool) (tea.Model, tea.Cmd) {
	if m.focus != pane {
		m.focus = pane
	}

	switch pane {
	case FocusNav:
		m.sel = Selection{}
		if !rowOK {
			return m, nil
		}
		m.navCursor = row
		e := m.navEntries[row]
		if e.Kind == "file" && lx <= 2 && count == 1 {
			m.navToggle() // the ▸/▾ arrow
			return m, nil
		}
		if count == 1 {
			if e.Kind == "file" {
				m.navExpanded[e.Path] = true
			}
			return m, m.navActivate()
		}
		return m, nil

	case FocusCentre:
		if !rowOK {
			m.sel = Selection{}
			return m, nil
		}
		m.mouseDown, m.mouseDownPane, m.mouseDownRow, m.mouseDownCol = true, pane, row, col
		r := m.centreRows[row]
		m.centreCursor = row
		switch count {
		case 1:
			m.sel = Selection{}
			m.pending = pendingClick{active: true, pane: pane, row: row, col: col, marker: lx == 0}
		case 2:
			if r.Kind == RowCard && r.Activate != nil {
				return m, nil // a card header: the first click already toggled it
			}
			if r.Feedback != nil {
				return m, m.editFeedback(r.Feedback, false, fbInPlace)
			}
			if t := m.threadOnRow(r); t != nil && lx == 0 {
				m.openThread(t)
				return m, nil
			}
			s, e := wordBounds(r.text(), col)
			m.sel = Selection{Active: true, Pane: pane, AnchorRow: row, AnchorCol: s, CursorRow: row, CursorCol: e}
		case 3:
			m.sel = Selection{Active: true, Pane: pane, AnchorRow: row, AnchorCol: 0, CursorRow: row, CursorCol: len([]rune(r.text()))}
		}
		return m, nil

	case FocusPanel:
		if !rowOK {
			return m, nil
		}
		// The list: a click selects, a double-click jumps to it (like enter).
		m.sel = Selection{}
		if m.panelItemRow(row) {
			m.panelCursor = row
			if r := m.panelRows[row]; count == 2 && r.Activate != nil {
				return m, r.Activate(m)
			}
		}
	}
	return m, nil
}

func (m *Model) handleMouseMotion(msg tea.MouseMotionMsg) (tea.Model, tea.Cmd) {
	mm := msg.Mouse()
	if m.border.which != borderNone {
		m.dragBorder(mm.X, mm.Y)
		return m, nil
	}
	if !m.mouseDown {
		// Plain motion (all-motion mode): only the border hover changes.
		if m.overlay == OverlayNone {
			m.hoverBorder = m.borderAt(mm.X, mm.Y)
		}
		return m, nil
	}
	pane := m.mouseDownPane
	if pane != FocusCentre {
		return m, nil
	}
	in := m.centreInner()
	// Clamp to the pane the drag started in; dragging past its top or
	// bottom edge scrolls it a row per motion event.
	lx, ly := mm.X-in.X, mm.Y-in.Y
	scroll := func(d int) {
		m.centreScroll = clamp(m.centreScroll+d, 0, len(m.centreRows)-in.H)
	}
	if ly <= 0 {
		scroll(-1)
		ly = 0
	} else if ly >= in.H-1 {
		scroll(1)
		ly = in.H - 1
	}
	lx = clamp(lx, 0, in.W)
	row, col, ok := m.hitRow(pane, lx, ly)
	if !ok {
		rows := m.rowsForPane(pane)
		if len(rows) == 0 {
			return m, nil
		}
		row = len(rows) - 1
		col = len([]rune(rows[row].text()))
	}
	if !m.sel.Active {
		if row == m.mouseDownRow && col == m.mouseDownCol {
			return m, nil
		}
		m.sel = Selection{Active: true, Pane: pane, AnchorRow: m.mouseDownRow, AnchorCol: m.mouseDownCol}
	}
	m.sel.CursorRow, m.sel.CursorCol, m.sel.Dragging = row, col, true
	m.pending = pendingClick{}
	m.centreCursor = row
	return m, nil
}

func (m *Model) handleMouseRelease(msg tea.MouseReleaseMsg) (tea.Model, tea.Cmd) {
	if m.border.which != borderNone {
		m.border = borderDrag{}
		return m, nil
	}
	m.mouseDown = false
	if m.sel.Active && m.sel.Dragging {
		m.sel.Dragging = false
		if m.sel.AnchorRow == m.sel.CursorRow && m.sel.AnchorCol == m.sel.CursorCol {
			m.sel = Selection{}
		}
		return m, nil
	}
	p := m.pending
	m.pending = pendingClick{}
	if !p.active || m.sel.Active {
		return m, nil
	}
	rows := m.rowsForPane(p.pane)
	if p.row >= len(rows) {
		return m, nil
	}
	r := rows[p.row]
	if l := r.linkAt(p.col); l != nil {
		return m, l.Act(m)
	}
	if p.marker {
		if t := m.threadOnRow(r); t != nil {
			m.openThread(t) // a click on a ● marker opens its card
			return m, nil
		}
	}
	if r.Activate != nil {
		return m, r.Activate(m)
	}
	return m, nil
}

func (m *Model) handleMouseWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	mm := msg.Mouse()
	if m.overlay != OverlayNone {
		if m.overlay == OverlayHelp {
			if mm.Button == tea.MouseWheelUp {
				m.helpScroll -= 3
			} else if mm.Button == tea.MouseWheelDown {
				m.helpScroll += 3
			}
		}
		return m, nil
	}
	var d int
	switch mm.Button {
	case tea.MouseWheelUp:
		d = -3
	case tea.MouseWheelDown:
		d = 3
	default:
		return m, nil // horizontal wheel
	}
	pane, _, _, _, ok := m.paneAt(mm.X, mm.Y)
	if !ok {
		return m, nil
	}
	// The wheel moves the view, not the cursor; the cursor is only pulled
	// along when it would leave the screen (so keys keep working from
	// where you're looking).
	switch pane {
	case FocusNav:
		m.navScroll = clamp(m.navScroll+d, 0, len(m.navEntries)-m.navH())
	case FocusCentre:
		h := m.centreH()
		m.centreScroll = clamp(m.centreScroll+d, 0, len(m.centreRows)-h)
		if !(m.sel.Active && m.sel.Keyboard && m.sel.Pane == FocusCentre) {
			m.centreCursor = clamp(m.centreCursor, m.centreScroll, m.centreScroll+h-1)
		}
	case FocusPanel:
		// A short list: one row per notch.
		m.panelScroll = clamp(m.panelScroll+d/3, 0, len(m.panelRows)-m.panelH())
	}
	return m, nil
}
