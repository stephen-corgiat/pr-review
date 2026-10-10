package ui

import (
	"strings"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

// placeCursor moves a textarea's cursor to a clicked cell: vrow is the
// visual (soft-wrapped) line from the top of its content, col the display
// column in that line. The textarea has no mouse support of its own.
func placeCursor(ta *textarea.Model, vrow, col int) {
	ta.MoveToBegin()
	for i := 0; i < vrow; i++ {
		line, off := ta.Line(), ta.LineInfo().RowOffset
		ta.CursorDown()
		if ta.Line() == line && ta.LineInfo().RowOffset == off {
			break // the last line
		}
	}
	li := ta.LineInfo()
	lines := strings.Split(ta.Value(), "\n")
	if ta.Line() >= len(lines) {
		return
	}
	rs := []rune(lines[ta.Line()])
	k, w := 0, 0
	for k < li.Width && li.StartColumn+k < len(rs) {
		cw := cellWidth(string(rs[li.StartColumn+k]))
		if w+cw > col {
			break
		}
		w += cw
		k++
	}
	ta.SetCursorColumn(li.StartColumn + k)
}

// taZones registers each line of a textarea drawn in a dialog at body
// rows y0… (dialog coordinates) and column x0, so a click places the
// cursor there.
func (m *Model) taZones(ta *textarea.Model, x0, y0, w, lines int, focus func(*Model)) {
	for i := 0; i < lines; i++ {
		line := i
		m.dialogZones = append(m.dialogZones, zone{X0: x0, X1: x0 + w, Y: y0 + i, Fn: func(mm *Model) tea.Cmd {
			if focus != nil {
				focus(mm)
			}
			placeCursor(ta, line+ta.ScrollYOffset(), mm.zoneDX)
			return nil
		}})
	}
}
