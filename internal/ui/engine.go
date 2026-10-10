package ui

// The one adapter between the engine's message API and Bubble Tea: keys
// and clicks become engine commands (do), finished agent runs come in
// through a tea.Cmd waiting on the engine's Inbox (listen), and the
// events that come out become view state (onEvents).

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// engineMsg is a finished agent run, for Engine.Handle.
type engineMsg struct{ in engine.Input }

// pollMsg: time to pick up events other processes appended to the log.
type pollMsg struct{}

const pollEvery = 2 * time.Second

// listen waits for the next finished agent run.
func (m *Model) listen() tea.Cmd {
	ch := m.eng.Inbox()
	return func() tea.Msg { return engineMsg{<-ch} }
}

func pollCmd() tea.Cmd {
	return tea.Tick(pollEvery, func(time.Time) tea.Msg { return pollMsg{} })
}

// do sends a command to the engine. A refusal is flashed.
func (m *Model) do(c engine.Command) tea.Cmd {
	m.dirty = true
	evs, err := m.eng.Do(c)
	cmd := m.onEvents(evs)
	if err != nil {
		return tea.Batch(cmd, m.flashCmd(err.Error()))
	}
	return cmd
}

// handleEngine commits a finished run and listens for the next.
func (m *Model) handleEngine(msg engineMsg) tea.Cmd {
	var evs []session.Event
	if msg.in.IsBriefing() {
		// The navigator re-sorts into reading order: keep the view steady.
		m.keepViewport(func() { evs = m.eng.Handle(msg.in) })
	} else {
		evs = m.eng.Handle(msg.in)
	}
	return tea.Batch(m.onEvents(evs), m.listen())
}

// onEvents turns events into view state: for now, a few flashes.
func (m *Model) onEvents(evs []session.Event) tea.Cmd {
	var cmd tea.Cmd
	for _, ev := range evs {
		switch p := ev.Data.(type) {
		case session.CallersReady, session.CallersUnavailable:
			m.dirty = true
		case session.RevisionArrived:
			if f := m.sess.FeedbackByID(p.FeedbackID); f != nil && f.Revision != nil && f.Revision.Stale {
				cmd = m.flashCmd(f.ID + "'s Revision is Stale: the Feedback changed while Refine ran")
			}
		case session.QuestionFailed:
			cmd = m.flashCmd(p.ThreadID + ": the agent failed (see the Thread)")
		case session.RefineFailed:
			cmd = m.flashCmd(p.FeedbackID + ": Refine failed: " + oneLine(p.Error))
		case session.BriefingFailed:
			cmd = m.flashCmd("The Briefing agent failed: " + oneLine(p.Error))
		case session.FeedbackStatusChanged:
			if p.Status == session.FeedbackHandedOff && ev.Writer != "" && m.externalWriter(ev) {
				cmd = m.flashCmd(fmt.Sprintf("The Implementing Agent pulled in %s (handed off)", strings.Join(p.FeedbackIDs, ", ")))
			}
		case session.OutcomeRecorded:
			if m.externalWriter(ev) {
				o := session.Outcome{Kind: p.Outcome}
				msg := p.FeedbackID + ": " + outcomeSpan(&o).Text
				if p.Note != "" {
					msg += " · " + oneLine(p.Note)
				}
				cmd = m.flashCmd(msg)
			}
		}
	}
	if err := m.eng.StoreErr(); err != nil && err != m.lastStoreErr {
		m.lastStoreErr = err
		cmd = m.flashCmd("The event log can't be written: " + err.Error())
	}
	return cmd
}

// externalWriter: the event came from another process (export, resolve).
func (m *Model) externalWriter(ev session.Event) bool { return ev.Writer != m.eng.Writer() }

// markRead clears a Thread's "new" marker (it counts as read once you
// open or expand it).
func (m *Model) markRead(t *session.Thread) {
	if t != nil && t.Unread {
		m.eng.Do(engine.MarkRead{ThreadID: t.ID})
	}
}
