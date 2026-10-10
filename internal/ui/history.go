package ui

// Back / Forward over centre jumps. Every jump function calls markJump()
// first; Update pushes the location it started from once the message is
// handled, if the centre actually moved. So nested jumps (reveal → openFile
// → …) record one entry, and a "jump" that goes nowhere records none.

import tea "charm.land/bubbletea/v2"

type centreLoc struct {
	view   CentreView
	path   string
	scroll int
	cursor int
}

const historyCap = 50

func (m *Model) curLoc() centreLoc {
	return centreLoc{view: m.centreView, path: m.filePath, scroll: m.centreScroll, cursor: m.centreCursor}
}

// markJump records where the centre is before a jump (once per message).
func (m *Model) markJump() {
	if m.jumpFrom == nil {
		l := m.curLoc()
		m.jumpFrom = &l
	}
}

// commitJump pushes the pre-jump location onto Back if the centre moved.
func (m *Model) commitJump() {
	from := m.jumpFrom
	m.jumpFrom = nil
	if from == nil || *from == m.curLoc() {
		return
	}
	if n := len(m.back); n > 0 && m.back[n-1] == *from {
		m.fwd = nil
		return
	}
	m.back = append(m.back, *from)
	if len(m.back) > historyCap {
		m.back = m.back[len(m.back)-historyCap:]
	}
	m.fwd = nil
}

func (m *Model) goBack() tea.Cmd {
	return m.stepHistory(&m.back, &m.fwd, "Back: nothing earlier")
}

func (m *Model) goForward() tea.Cmd {
	return m.stepHistory(&m.fwd, &m.back, "Forward: nothing later")
}

// stepHistory pops from, pushes the current location onto to, and restores
// the popped location exactly (clamped if the rows changed since).
func (m *Model) stepHistory(from, to *[]centreLoc, empty string) tea.Cmd {
	m.jumpFrom = nil
	cur := m.curLoc()
	for len(*from) > 0 && (*from)[len(*from)-1] == cur {
		*from = (*from)[:len(*from)-1]
	}
	if len(*from) == 0 {
		return m.flashCmd(empty)
	}
	l := (*from)[len(*from)-1]
	*from = (*from)[:len(*from)-1]
	*to = append(*to, cur)
	if l.view == ViewFile {
		if _, ok := m.sess.Files[l.path]; !ok {
			return m.flashCmd(empty)
		}
	}
	if l.view == ViewOutside {
		if _, ok := m.outside[l.path]; !ok {
			return m.flashCmd(empty)
		}
	}
	m.centreView, m.filePath = l.view, l.path
	if l.view != ViewFile && l.view != ViewOutside {
		m.filePath = ""
	}
	m.sel = Selection{}
	m.rebuild()
	m.centreScroll, m.centreCursor = l.scroll, l.cursor
	m.clampCentre()
	return nil
}
