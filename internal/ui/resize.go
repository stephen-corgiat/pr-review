package ui

// Drag-to-resize: the vertical border between the left column and the
// centre, and the horizontal one between the navigator and the list pane.

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

const (
	borderNone = iota
	borderV
	borderH
)

type borderDrag struct {
	which  int
	x0, y0 int
	size0  int // left column width, or the list pane's outer height, at the press
}

// borderAt is the draggable border under (x, y), if any. Each border is the
// pair of box edges that meet there, so either cell grabs it.
func (m *Model) borderAt(x, y int) int {
	nav, centre, panel := m.layout()
	if centre.H > 0 && y >= centre.Y && y < centre.Y+centre.H && (x == nav.W-1 || x == nav.W) {
		return borderV
	}
	if panel.H > 0 && x >= 0 && x < nav.W-1 && (y == nav.Y+nav.H-1 || y == panel.Y) {
		return borderH
	}
	return borderNone
}

// pressBorder starts a drag, or resets the border on a double-click.
func (m *Model) pressBorder(which, x, y int) {
	if m.registerClick(x, y, Focus(100+which)) == 2 {
		m.border = borderDrag{}
		m.keepViewport(func() {
			if which == borderV {
				m.leftW = 0
			} else {
				m.listN = 0
			}
		})
		return
	}
	nav, _, panel := m.layout()
	size := nav.W
	if which == borderH {
		size = panel.H
	}
	m.border = borderDrag{which: which, x0: x, y0: y, size0: size}
	m.sel = Selection{}
	m.pending = pendingClick{}
	m.mouseDown = false
}

// dragBorder follows the pointer; the layout clamps to the minimums, and the
// stored size is clamped too so dragging back starts from the edge.
func (m *Model) dragBorder(x, y int) {
	d := m.border
	switch d.which {
	case borderV:
		w := clamp(d.size0+x-d.x0, minLeftW, maxInt(minLeftW, m.width-minCentreW))
		if w != m.navWidth() {
			m.keepViewport(func() { m.leftW = w })
		}
	case borderH:
		bodyH := maxInt(0, m.height-2-m.bannerH())
		items := clamp(d.size0-(y-d.y0)-2, minListItems, maxInt(minListItems, bodyH-(minNavRows+2)-2))
		if items != m.listN {
			m.listN = items
			m.dirty = true
		}
	}
}

// accentBorder redraws the hovered or dragged border heavy, in the accent
// colour, so it reads as something you can grab.
func (m *Model) accentBorder(frame []string) {
	which := m.border.which
	if which == borderNone {
		which = m.hoverBorder
	}
	if which == borderNone || m.overlay != OverlayNone {
		return
	}
	nav, centre, panel := m.layout()
	switch which {
	case borderV:
		for y := centre.Y; y < centre.Y+centre.H && y < len(frame); y++ {
			frame[y] = heavyCells(frame[y], nav.W-1, nav.W+1)
		}
	case borderH:
		if panel.H == 0 {
			return
		}
		frame[nav.Y+nav.H-1] = heavyCells(frame[nav.Y+nav.H-1], 0, nav.W)
		frame[panel.Y] = heavyCells(frame[panel.Y], 0, nav.W)
	}
}

var heavy = map[string]string{
	"│": "┃", "─": "━", "╭": "┏", "╮": "┓", "╰": "┗", "╯": "┛",
}

// heavyCells swaps the light box-drawing cells in [x0,x1) for heavy ones in
// the accent colour; anything else (tab labels, notes) is left alone.
func heavyCells(line string, x0, x1 int) string {
	w := cellWidth(line)
	x0, x1 = clamp(x0, 0, w), clamp(x1, 0, w)
	if x0 >= x1 {
		return line
	}
	var b strings.Builder
	b.WriteString(ansi.Cut(line, 0, x0))
	st := fg(cBlue).Bold(true)
	for x := x0; x < x1; x++ {
		cell := ansi.Cut(line, x, x+1)
		if h, ok := heavy[ansi.Strip(cell)]; ok {
			b.WriteString(st.Render(h))
		} else {
			b.WriteString(cell)
		}
	}
	b.WriteString(ansi.Cut(line, x1, w))
	return b.String()
}
