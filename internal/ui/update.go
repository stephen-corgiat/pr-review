package ui

import (
	tea "charm.land/bubbletea/v2"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Jumps made while handling this message land in the Back history.
	m.jumpFrom = nil
	defer m.commitJump()

	// Rows are rebuilt from session and view state before any message that
	// can change them, so handlers hit-test and act on what's current, and
	// again before the next frame. Wheel and drag motion only move the view
	// or the selection, so they skip the rebuild: scrolling stays cheap.
	switch msg.(type) {
	case tea.MouseWheelMsg, tea.MouseMotionMsg:
	default:
		if m.ready {
			m.rebuild()
		}
		defer func() { m.dirty = true }()
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		first := !m.ready
		m.width, m.height = msg.Width, msg.Height
		m.ready = true
		if first {
			m.rebuild()
		} else {
			m.keepViewport(func() {})
		}
		return m, nil

	case engineMsg:
		return m, m.handleEngine(msg)

	case pollMsg:
		return m, tea.Batch(m.onEvents(m.eng.Poll()), pollCmd())

	case headTickMsg:
		var cmd tea.Cmd
		if m.eng.HeadDue() {
			cmd = m.checkHead()
		}
		return m, tea.Batch(cmd, headTickCmd())

	case headMsg:
		return m, m.handleHead(msg)

	case updatedMsg:
		return m, m.handleUpdated(msg)

	case tea.FocusMsg:
		if m.eng.NoteFocus() {
			return m, m.checkHead()
		}
		return m, nil

	case commentsMsg:
		return m, m.handleComments(msg)

	case postedMsg:
		return m, m.handlePosted(msg)

	case flashClearMsg:
		if msg.Seq == m.flashSeq {
			m.flash = ""
		}
		return m, nil

	case clipboardCopiedMsg:
		return m, m.flashCmd(string(msg))

	case tea.PasteMsg:
		var cmd tea.Cmd
		switch {
		case m.overlay == OverlayTextbox:
			cmd = m.editTA(&m.ta, msg)
		case m.overlay == OverlaySend && m.send.editing:
			cmd = m.editTA(&m.sendTA, msg)
		case m.fbEdit != nil:
			cmd = m.editTA(&m.fbEdit.ta, msg)
		}
		return m, cmd

	case tea.KeyPressMsg:
		return withCmd(m.handleKey(msg))(m.noteInput())

	case tea.MouseClickMsg:
		return withCmd(m.handleMouseClick(msg))(m.noteInput())
	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg)
	case tea.MouseMotionMsg:
		return withCmd(m.handleMouseMotion(msg))(m.noteInput())
	case tea.MouseWheelMsg:
		return withCmd(m.handleMouseWheel(msg))(m.noteInput())
	}
	return m, nil
}

// withCmd adds a command to a handler's result: the head check an input
// may start (the first input after a pause).
func withCmd(tm tea.Model, cmd tea.Cmd) func(tea.Cmd) (tea.Model, tea.Cmd) {
	return func(extra tea.Cmd) (tea.Model, tea.Cmd) {
		if extra == nil {
			return tm, cmd
		}
		return tm, tea.Batch(cmd, extra)
	}
}
