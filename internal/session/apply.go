package session

import (
	"strconv"
	"strings"
)

// Apply changes the state by one event. It trusts the event: the engine
// checks a command is allowed before it emits the events for it. Events
// about Threads or Feedback that no longer exist are ignored, so a log
// with events from another process still replays.
func (s *Session) Apply(ev Event) {
	switch e := ev.Data.(type) {
	case Opened:
		s.PR = e.PR
		s.Checkout = e.Checkout
		if e.Worktree != "" {
			s.WorktreePath = e.Worktree
		}
		s.ScratchPath = e.Scratch
		s.Fixture = e.Fixture
		s.RepoMode = e.RepoMode
		s.Model, s.Effort = e.Model, e.Effort

	case Finished:
		s.Finished = e.State
		s.PR.State = e.State

	case HeadMoved:
		s.PrevHead, s.UpdateCommits = e.From, e.Commits
		s.PR.HeadSHA, s.PR.BaseSHA = e.To, e.BaseTo
		if e.Title != "" {
			s.PR.Title = e.Title
		}
		if e.Description != "" {
			s.PR.Description = e.Description
		}
		if e.Files != nil {
			s.PR.Files, s.PR.Additions, s.PR.Deletions = e.Files, e.Additions, e.Deletions
		}
		for id, a := range e.Anchors {
			if t := s.threadByID(id); t != nil {
				t.Anchor = a
			} else if f := s.FeedbackByID(id); f != nil {
				f.Anchor = a
			}
		}
		for _, id := range e.StaleAnswers {
			if t := s.threadByID(id); t != nil {
				for i := range t.Entries {
					if t.Entries[i].Role == RoleAnswer {
						t.Entries[i].Stale = true
					}
				}
			}
		}
		// The old Briefing stays visible, Stale, until the new one lands.
		if s.Briefing != nil && s.lastNotes != nil {
			s.applyBriefing(*s.lastNotes)
		}
		if s.Briefing != nil {
			s.Briefing.Stale, s.Briefing.StaleFrom = true, e.From
		}
		s.BriefingError = ""

	case BriefingArrived:
		notes := e.Notes
		s.lastNotes = &notes
		s.applyBriefing(e.Notes)
		if s.Briefing != nil {
			s.Briefing.Stale, s.Briefing.StaleFrom = false, ""
		}
		s.BriefingRun = e.Run
		s.BriefingError = ""
		s.TotalCost += e.Run.Cost

	case BriefingFailed:
		s.BriefingError = e.Error
		s.TotalCost += e.Run.Cost

	case ThreadCreated:
		th := &Thread{ID: e.ThreadID, Anchor: e.Anchor, Kind: e.Kind, Created: ev.At, Status: StatusQueued}
		if e.Draft {
			th.Status = StatusDraft
		}
		th.Entries = append(th.Entries, Entry{Role: RoleQuestion, Kind: e.Kind, Text: e.Text, At: ev.At})
		s.Threads = append(s.Threads, th)
		s.threadSeq = max(s.threadSeq, idNumber(e.ThreadID))

	case FollowUpAsked:
		if th := s.threadByID(e.ThreadID); th != nil {
			th.Entries = append(th.Entries, Entry{Role: RoleQuestion, Kind: e.Kind, Text: e.Text, At: ev.At, Reask: e.Reask})
			th.Status = StatusQueued
		}

	case DraftsSent:
		for _, id := range e.ThreadIDs {
			if th := s.threadByID(id); th != nil && th.Status == StatusDraft {
				th.Status = StatusQueued
			}
		}

	case QuestionStarted:
		if th := s.threadByID(e.ThreadID); th != nil {
			th.Status = StatusRunning
		}

	case AnswerArrived:
		s.TotalCost += e.Run.Cost
		th := s.threadByID(e.ThreadID)
		if th == nil {
			return
		}
		a := e.Answer
		th.Entries = append(th.Entries, Entry{Role: RoleAnswer, Kind: th.PendingKind(), A: &a, Unread: !e.Read, At: ev.At})
		if !e.Read {
			th.Unread = true
		}
		th.Status = StatusAnswered
		s.noteRun(&th.AgentSession, &th.AgentTotal, &th.Cost, e.Run)

	case QuestionFailed:
		s.TotalCost += e.Run.Cost
		th := s.threadByID(e.ThreadID)
		if th == nil {
			return
		}
		th.Entries = append(th.Entries, Entry{Role: RoleNote, Text: "The agent failed: " + oneLine(e.Error), At: ev.At})
		th.Status = StatusFailed
		th.Cost += e.Run.Cost

	case QuestionCancelled:
		th := s.threadByID(e.ThreadID)
		if th == nil {
			return
		}
		// A cancelled follow-up leaves the Thread's earlier Answers; a
		// cancelled first Question removes the Thread.
		if th.LatestAnswerIdx() < 0 {
			s.removeThread(e.ThreadID)
			return
		}
		if n := len(th.Entries); n > 0 && th.Entries[n-1].Role == RoleQuestion {
			th.Entries = th.Entries[:n-1]
		}
		th.Status = StatusAnswered

	case DraftDeleted:
		s.removeThread(e.ThreadID)

	case ThreadRead:
		if th := s.threadByID(e.ThreadID); th != nil {
			th.Unread = false
			for i := range th.Entries {
				th.Entries[i].Unread = false
			}
		}

	case FeedbackCreated:
		s.Feedback = append(s.Feedback, &Feedback{ID: e.FeedbackID, Anchor: e.Anchor, Status: e.Status, Body: e.Body, Created: ev.At})
		if s.usedFeedbackIDs == nil {
			s.usedFeedbackIDs = map[string]bool{}
		}
		s.usedFeedbackIDs[e.FeedbackID] = true

	case FeedbackEdited:
		if f := s.FeedbackByID(e.FeedbackID); f != nil {
			f.Body = e.Body
			if f.Revision != nil && f.Revision.BaseBody != e.Body {
				f.Revision.Stale = true
			}
		}

	case FeedbackDeleted:
		for i, f := range s.Feedback {
			if f.ID == e.FeedbackID {
				s.Feedback = append(s.Feedback[:i], s.Feedback[i+1:]...)
				break
			}
		}

	case FeedbackStatusChanged:
		for _, id := range e.FeedbackIDs {
			if f := s.FeedbackByID(id); f != nil {
				f.Status = e.Status
			}
		}

	case ReviewPosted:
		for _, id := range e.FeedbackIDs {
			if f := s.FeedbackByID(id); f != nil {
				f.Status = FeedbackPosted
			}
		}

	case OutcomeRecorded:
		if f := s.FeedbackByID(e.FeedbackID); f != nil {
			f.Outcomes = append(f.Outcomes, Outcome{Kind: e.Outcome, Note: e.Note, Commit: e.Commit, At: ev.At})
		}

	case RefineAsked:
		if f := s.FeedbackByID(e.FeedbackID); f != nil {
			f.Revision = nil
			f.RefineError = ""
			f.RefineBase, f.RefineAnswers, f.RefineRound = e.Base, e.Answers, e.Round
			f.Refine = RefineQueued
		}

	case RefineStarted:
		if f := s.FeedbackByID(e.FeedbackID); f != nil {
			f.Refine = RefineRunning
		}

	case RevisionArrived:
		s.TotalCost += e.Run.Cost
		f := s.FeedbackByID(e.FeedbackID)
		if f == nil {
			return
		}
		f.Refine = RefineNone
		rev := e.Revision
		rev.BaseBody = f.RefineBase
		rev.Round = f.RefineRound
		rev.Stale = f.Body != f.RefineBase
		f.Revision = &rev
		s.noteRun(&f.AgentSession, &f.AgentTotal, &f.Cost, e.Run)

	case RefineFailed:
		s.TotalCost += e.Run.Cost
		if f := s.FeedbackByID(e.FeedbackID); f != nil {
			f.Refine = RefineNone
			f.RefineError = oneLine(e.Error)
			f.Cost += e.Run.Cost
		}

	case RevisionAccepted:
		if f := s.FeedbackByID(e.FeedbackID); f != nil && f.Revision != nil {
			f.PrevBody = f.Body
			f.Body = f.Revision.Text
			f.Revision = nil
		}

	case RevisionEdited:
		if f := s.FeedbackByID(e.FeedbackID); f != nil && f.Revision != nil {
			f.Revision.Text = e.Text
			f.Revision.Edited = true
		}

	case RevisionIgnored:
		if f := s.FeedbackByID(e.FeedbackID); f != nil {
			f.Revision = nil
		}
	}
}

// noteRun records a finished run against its Thread or Feedback: the agent
// session to resume, its cumulative total, and its own cost.
func (s *Session) noteRun(sessionID *string, total, cost *float64, r RunInfo) {
	if r.SessionID != "" {
		*sessionID = r.SessionID
		*total = r.Total
	}
	*cost += r.Cost
}

// idNumber is the number in an ID like "T12" or "F4" (0 if none).
func idNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimLeft(id, "TF"))
	return n
}
