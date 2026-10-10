package ui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

func (m *Model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	switch m.overlay {
	case OverlayTextbox:
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleTextboxKey(msg)
	case OverlayHelp:
		switch key {
		case "esc", "?", "q", "enter":
			m.overlay = OverlayNone
		case "j", "down":
			m.helpScroll++
		case "k", "up":
			m.helpScroll--
		case "ctrl+d", "pgdown", "space":
			m.helpScroll += 10
		case "ctrl+u", "pgup":
			m.helpScroll -= 10
		}
		return m, nil
	case OverlayConfirm:
		return m.handleConfirmKey(key)
	case OverlaySend:
		if key == "ctrl+c" {
			return m, tea.Quit
		}
		return m.handleSendKey(msg)
	}

	if m.fbEdit != nil {
		return m.handleFeedbackEditKey(msg)
	}
	if m.search.prompt {
		return m.handleSearchKey(msg)
	}
	// While a search is on, n / N / p step through its matches (p only
	// then: it's unbound otherwise).
	if m.search.hl && !m.sel.Active {
		switch key {
		case "n":
			return m, m.searchStep(1)
		case "N", "p":
			return m, m.searchStep(-1)
		}
	}

	switch key {
	case "ctrl+c", "q":
		return m, tea.Quit
	case "?":
		m.overlay = OverlayHelp
		m.helpScroll = 0
		return m, nil
	case "tab":
		m.cycleFocus(1)
		return m, nil
	case "shift+tab":
		m.cycleFocus(-1)
		return m, nil
	case "z":
		m.keepViewport(m.toggleAllCards)
		return m, nil
	case "h":
		m.holdOn = !m.holdOn
		if m.holdOn {
			return m, m.flashCmd("Hold on: new Questions become Drafts until H")
		}
		return m, m.flashCmd("Hold off")
	case "H":
		return m, m.do(engine.SendDrafts{})
	case "S":
		return m, m.openSince()
	case "U":
		return m, m.startUpdate()
	case "b":
		m.panelVisible = !m.panelVisible
		if !m.panelVisible && m.focus == FocusPanel {
			m.focus = FocusCentre
		}
		return m, nil
	case "1", "2":
		m.panelTab, m.panelVisible = PanelTab(key[0]-'1'), true
		m.panelCursor, m.panelScroll = 0, 0
		return m, nil
	case "esc":
		if m.search.hl {
			m.clearSearch() // highlights go first, before esc does anything else
			return m, nil
		}
		m.sel = Selection{}
		return m, nil
	case "/":
		m.startSearch()
		return m, nil
	case "ctrl+o", "alt+left":
		return m, m.goBack()
	case "alt+right":
		return m, m.goForward()
	case "P":
		return m, m.openSend()
	}

	// A row's own keys come first: r on a Feedback card refines, x on a
	// Trace Answer samples its path, A/e/X on a Revision.
	if m.focus == FocusCentre && !m.sel.Active && m.centreCursor >= 0 && m.centreCursor < len(m.centreRows) {
		if fn := m.centreRows[m.centreCursor].Keys[key]; fn != nil {
			m.dirty = true
			return m, fn(m)
		}
	}

	if isActionKey(key) && m.actionKeyApplies() {
		return m, m.tryActionKey(key)
	}

	switch m.focus {
	case FocusNav:
		return m.handleNavKey(key)
	case FocusCentre:
		return m.handleCentreKey(key)
	case FocusPanel:
		return m.handlePanelKey(key)
	}
	return m, nil
}

func (m *Model) cycleFocus(d int) {
	order := []Focus{FocusNav, FocusCentre}
	if m.panelVisible {
		order = append(order, FocusPanel)
	}
	i := 0
	for k, f := range order {
		if f == m.focus {
			i = k
		}
	}
	m.focus = order[(i+d+len(order))%len(order)]
	m.sel = Selection{}
}

func isActionKey(key string) bool {
	switch key {
	case "d", "s", "x", "t", "c", "r", "f", "y":
		return true
	}
	return false
}

// actionKeyApplies: the action letters ask Questions everywhere except the
// Threads/Feedback list, which has no text to anchor on.
func (m *Model) actionKeyApplies() bool {
	return m.sel.Active || m.focus != FocusPanel
}

func (m *Model) tryActionKey(key string) tea.Cmd {
	defer func() { m.dirty = true }()
	switch key {
	case "d":
		return m.doQuestion(session.Define)
	case "s":
		return m.doQuestion(session.Summarize)
	case "x":
		return m.doQuestion(session.SampleData)
	case "t":
		return m.doQuestion(session.Trace)
	case "c":
		return m.doQuestion(session.TestCases)
	case "r":
		if a, ok := m.currentAnchor(); ok {
			m.tbAnchor = a
			m.askKind = session.Research
			m.openTextbox(TextboxAsk, "")
		}
	case "f":
		if a, ok := m.currentAnchor(); ok {
			return m.startFeedback(a)
		}
		return m.flashCmd("Nothing to comment on here: move to a line of code or text, or select some")
	case "y":
		return m.doCopy()
	}
	return nil
}

func (m *Model) doQuestion(kind session.QuestionKind) tea.Cmd {
	anchor, ok := m.currentAnchor()
	if !ok {
		return m.flashCmd("Nothing to ask about here: move to a line of code or text, or select some")
	}
	n := len(m.sess.Threads)
	cmd := m.do(engine.Ask{Anchor: anchor, Kind: kind, Hold: m.holdOn})
	m.sel = Selection{}
	if len(m.sess.Threads) == n {
		return cmd // refused: the flash says why
	}
	t := m.sess.Threads[len(m.sess.Threads)-1]
	what := "asked"
	if t.Status == session.StatusDraft {
		what = "held as a Draft"
	}
	return tea.Batch(cmd, m.flashCmd(t.ID+" "+kind.String()+" "+what))
}

func (m *Model) doCopy() tea.Cmd {
	text := ""
	if a, ok := m.currentAnchor(); ok {
		text = a.Text
	}
	if text == "" {
		return nil
	}
	m.sel = Selection{}
	return copyCmd(text)
}

// currentAnchor is what an action applies to: the selection, else the
// cursor row (centre, Thread tab) or the highlighted navigator entry.
func (m *Model) currentAnchor() (session.Anchor, bool) {
	m.ensureRows()
	if m.sel.Active {
		return m.buildSelectionAnchor()
	}
	switch m.focus {
	case FocusNav:
		return m.navAnchor()
	case FocusCentre:
		if m.centreCursor < 0 || m.centreCursor >= len(m.centreRows) {
			return session.Anchor{}, false
		}
		// The cursor's whole code line, including its wrapped continuations.
		r0, r1 := m.centreCursor, m.centreCursor
		for r0 > 0 && m.centreRows[r0].Join == JoinHard {
			r0--
		}
		for r1+1 < len(m.centreRows) && m.centreRows[r1+1].Join == JoinHard {
			r1++
		}
		m.sel = Selection{Active: true, Pane: FocusCentre, AnchorRow: r0, CursorRow: r1, Keyboard: true}
		a, ok := m.buildSelectionAnchor()
		m.sel = Selection{}
		return a, ok
	}
	return session.Anchor{}, false
}

// buildSelectionAnchor turns the selection into an Anchor: its text, and
// the most specific anchor type among the selected rows.
func (m *Model) buildSelectionAnchor() (session.Anchor, bool) {
	rows := m.rowsForPane(m.sel.Pane)
	if len(rows) == 0 {
		return session.Anchor{}, false
	}
	r0, c0, r1, c1 := m.sel.Bounds()
	r0, r1 = clamp(r0, 0, len(rows)-1), clamp(r1, 0, len(rows)-1)
	var b strings.Builder
	var a session.Anchor
	found, firstDiff, lastDiff := false, -1, -1
	for i := r0; i <= r1; i++ {
		row := rows[i]
		rr := []rune(row.text())
		s, e := 0, len(rr)
		if i == r0 {
			s = clamp(c0, 0, len(rr))
		}
		if i == r1 {
			e = clamp(c1, 0, len(rr))
		}
		if i > r0 && b.Len() > 0 {
			switch row.Join {
			case JoinSoft:
				b.WriteString(" ")
			case JoinLine:
				b.WriteString("\n")
			}
		}
		if s < e {
			b.WriteString(string(rr[s:e]))
		}
		if !row.Selectable {
			continue
		}
		if row.DiffRow >= 0 {
			if firstDiff < 0 {
				firstDiff = row.DiffRow
			}
			lastDiff = row.DiffRow
			if !found || a.Type != session.AnchorDiff {
				a = session.Anchor{Type: session.AnchorDiff, Path: row.Anchor.Path}
				found = true
			}
			continue
		}
		if !found {
			a = row.Anchor
			found = true
		}
	}
	if !found {
		return session.Anchor{}, false
	}
	if a.Type == session.AnchorDiff {
		a.RowStart, a.RowEnd = firstDiff, lastDiff
	}
	a.Text = strings.TrimSpace(b.String())
	return a, a.Text != "" || a.Type != session.AnchorDiff
}

func (m *Model) rowsForPane(pane Focus) []Row {
	switch pane {
	case FocusCentre:
		return m.centreRows
	case FocusPanel:
		return m.panelRows
	}
	return nil
}

func (m *Model) toggleAllCards() {
	anyOpen := false
	for _, v := range m.expandedCards {
		anyOpen = anyOpen || v
	}
	for _, t := range m.sess.Threads {
		m.expandedCards[t.ID] = !anyOpen
	}
	for _, f := range m.sess.Feedback {
		m.expandedCards[fbKey(f)] = !anyOpen
	}
}

// ---- Navigator ----

func (m *Model) handleNavKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		m.navCursor = minInt(m.navCursor+1, len(m.navEntries)-1)
	case "k", "up":
		m.navCursor = maxInt(m.navCursor-1, 0)
	case "g", "home":
		m.navCursor = 0
	case "G", "end":
		m.navCursor = len(m.navEntries) - 1
	case "enter", "right", "l":
		if e := m.navEntries[m.navCursor]; e.Kind == "file" {
			m.navExpanded[e.Path] = true
		}
		cmd := m.navActivate()
		if key == "enter" {
			m.focus = FocusCentre
		}
		return m, cmd
	case "left":
		e := m.navEntries[m.navCursor]
		if e.Kind == "method" {
			for i := m.navCursor; i >= 0; i-- {
				if m.navEntries[i].Kind == "file" {
					m.navCursor = i
					break
				}
			}
		} else if e.Kind == "file" {
			m.navExpanded[e.Path] = false
		}
	case "space":
		m.navToggle()
	}
	m.rebuild()
	m.navFollow()
	return m, nil
}

// ---- Centre ----

func (m *Model) handleCentreKey(key string) (tea.Model, tea.Cmd) {
	n := len(m.centreRows)
	half := maxInt(1, m.centreH()/2)
	move := func(d int) {
		m.centreCursor = clamp(m.centreCursor+d, 0, n-1)
		m.followCursor()
	}
	switch key {
	case "j", "down":
		move(1)
	case "k", "up":
		move(-1)
	case "ctrl+d", "pgdown":
		m.centreScroll += half
		move(half)
	case "ctrl+u", "pgup":
		m.centreScroll -= half
		move(-half)
	case "g", "home":
		m.centreCursor, m.centreScroll = 0, 0
	case "G", "end":
		m.centreCursor = n - 1
		m.followCursor()
	case "v":
		if m.sel.Active && m.sel.Keyboard {
			m.sel = Selection{}
		} else {
			m.sel = Selection{Active: true, Pane: FocusCentre, Keyboard: true, AnchorRow: m.centreCursor, CursorRow: m.centreCursor}
		}
	case "E":
		if m.centreView == ViewFile {
			m.keepViewport(func() { m.hunkMode[m.filePath] = !m.currentHunkMode(m.filePath) })
		}
	case "]":
		return m, m.jumpChange(1)
	case "[":
		return m, m.jumpChange(-1)
	case "}":
		m.jumpFile(1)
	case "{":
		m.jumpFile(-1)
	case "J":
		m.jumpMethod(1)
	case "K":
		m.jumpMethod(-1)
	case "n", "N":
		if m.search.hl {
			d := 1
			if key == "N" {
				d = -1
			}
			return m, m.searchStep(d)
		}
	case "enter":
		if m.centreCursor < n {
			row := m.centreRows[m.centreCursor]
			if row.Activate != nil {
				return m, row.Activate(m)
			}
			if row.Feedback != nil {
				return m, m.editFeedback(row.Feedback, false, fbInPlace)
			}
			if t := m.threadOnRow(row); t != nil {
				m.openThread(t)
			}
		}
	case "R", "X", "e", "space", "P", "A", "r", "D", "O", "Q":
		if m.centreCursor < n {
			row := m.centreRows[m.centreCursor]
			if row.Feedback != nil {
				return m, m.feedbackKeyAction(row.Feedback, key)
			}
			t := row.Thread
			if t == nil {
				t = m.threadOnRow(row)
			}
			return m, m.threadKeyAction(t, key)
		}
	}
	if m.sel.Active && m.sel.Keyboard && m.sel.Pane == FocusCentre {
		m.sel.CursorRow = m.centreCursor
	}
	return m, nil
}

// threadOnRow is the first Thread marked on a row.
func (m *Model) threadOnRow(r Row) *session.Thread {
	if r.DiffRow >= 0 && r.Join != JoinHard {
		if ts := m.sess.ThreadsOnRow(r.Anchor.Path, r.DiffRow); len(ts) > 0 {
			return ts[0]
		}
	}
	if len(r.markerThreads) > 0 {
		return r.markerThreads[0]
	}
	return nil
}

// ---- Panel ----

func (m *Model) handlePanelKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "j", "down":
		m.movePanelCursor(1)
		return m, nil
	case "k", "up":
		m.movePanelCursor(-1)
		return m, nil
	}
	if !m.panelItemRow(m.panelCursor) {
		return m, nil
	}
	row := m.panelRows[m.panelCursor]
	if key == "enter" {
		return m, row.Activate(m) // jump to it
	}
	if row.Feedback != nil {
		return m, m.feedbackKeyAction(row.Feedback, key)
	}
	return m, m.threadKeyAction(row.Thread, key)
}

// threadKeyAction handles R / X on a Thread (its card, or its row in the
// list).
func (m *Model) threadKeyAction(t *session.Thread, key string) tea.Cmd {
	if t == nil {
		return nil
	}
	m.dirty = true
	switch key {
	case "Q":
		return m.reask(t)
	case "R":
		m.tbTarget = t
		m.openTextbox(TextboxFollowUp, "")
	case "P":
		return m.openSend()
	case "X":
		if t.Status == session.StatusDraft {
			id := t.ID
			return tea.Batch(m.do(engine.DeleteDraft{ThreadID: id}), m.flashCmd(id+" Draft deleted"))
		}
		id := t.ID
		if _, err := m.eng.Do(engine.Cancel{ThreadID: id}); err != nil {
			return m.flashCmd(err.Error())
		}
		m.dirty = true
		return m.flashCmd(id + " cancelled")
	}
	return nil
}

func (m *Model) feedbackKeyAction(f *session.Feedback, key string) tea.Cmd {
	m.dirty = true
	switch key {
	case "enter", "e":
		how := fbInPlace
		if m.focus == FocusPanel {
			how = fbShow // from the list: the card may be off-screen
		}
		return m.editFeedback(f, false, how)
	case "space":
		if f.Status.Locked() {
			return m.flashCmd(f.ID + " is " + f.Status.String() + ": it can't go back to draft (O reopens it)")
		}
		return m.do(engine.ToggleReady{ID: f.ID})
	case "D":
		if _, err := m.eng.Do(engine.MarkDone{ID: f.ID}); err != nil {
			return m.flashCmd(err.Error())
		}
		return m.flashCmd(f.ID + " done")
	case "O":
		if _, err := m.eng.Do(engine.Reopen{ID: f.ID}); err != nil {
			return m.flashCmd(err.Error())
		}
		return m.flashCmd(f.ID + " reopened: a draft again")
	case "P":
		return m.openSend()
	case "r":
		return m.refine(f)
	case "A":
		return m.acceptRevision(f)
	case "X":
		if f.Status.Locked() {
			return m.flashCmd(f.ID + " is " + f.Status.String() + " and can't be deleted")
		}
		ff := f
		m.overlay = OverlayConfirm
		m.confirmMsg = "Delete Feedback " + f.ID + "? (y/n)"
		m.confirmFn = func(mm *Model) tea.Cmd {
			if _, err := mm.eng.Do(engine.DeleteFeedback{ID: ff.ID}); err != nil {
				return mm.flashCmd(err.Error())
			}
			mm.dirty = true
			return mm.flashCmd(ff.ID + " deleted")
		}
	}
	return nil
}
