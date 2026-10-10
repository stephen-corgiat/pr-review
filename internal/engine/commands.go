package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

// Command is something the client asks the engine to do. Do checks it
// against the state and returns the events it produced.
type Command interface {
	events(e *Engine) ([]session.Payload, error)
}

// Do runs a command: its events are logged and applied, then any queued
// work that can start does. An error means nothing happened; its text is
// meant for the Reviewer.
func (e *Engine) Do(c Command) ([]session.Event, error) {
	ps, err := c.events(e)
	if err != nil {
		return nil, err
	}
	out, err := e.commit(ps...)
	if err != nil {
		return out, err
	}
	return append(out, e.dispatch()...), nil
}

// ErrReadOnly: the PR finished after the Review Session was created.
var ErrReadOnly = errors.New("this Review Session is read-only: its PR was merged or closed, so the agent is off")

func (e *Engine) needAgent() error {
	switch {
	case e.st.ReadOnly():
		return ErrReadOnly
	case e.agent == nil:
		return errors.New("no agent in this Review Session")
	}
	return nil
}

func (e *Engine) thread(id string) (*session.Thread, error) {
	if t := e.st.ThreadByID(id); t != nil {
		return t, nil
	}
	return nil, fmt.Errorf("no Thread %s", id)
}

func (e *Engine) feedback(id string) (*session.Feedback, error) {
	if f := e.st.FeedbackByID(id); f != nil {
		return f, nil
	}
	return nil, fmt.Errorf("no Feedback %s", id)
}

// ---- Questions ----

// Ask starts a Thread: queued, or held as a Draft. An empty Text asks the
// Kind's default Question.
type Ask struct {
	Anchor session.Anchor
	Kind   session.QuestionKind
	Text   string
	Hold   bool
}

func (c Ask) events(e *Engine) ([]session.Payload, error) {
	if err := e.needAgent(); err != nil {
		return nil, err
	}
	text := strings.TrimSpace(c.Text)
	if text == "" {
		text = c.Kind.DefaultQuestion(c.Anchor.Text)
	}
	return []session.Payload{session.ThreadCreated{
		ThreadID: e.st.NextThreadID(), Anchor: e.st.LocateAnchor(c.Anchor), Kind: c.Kind, Text: text, Draft: c.Hold,
	}}, nil
}

// SendDrafts sends every Draft Question.
type SendDrafts struct{}

func (c SendDrafts) events(e *Engine) ([]session.Payload, error) {
	var ids []string
	for _, t := range e.st.Threads {
		if t.Status == session.StatusDraft {
			ids = append(ids, t.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if err := e.needAgent(); err != nil {
		return nil, err
	}
	return []session.Payload{session.DraftsSent{ThreadIDs: ids}}, nil
}

// FollowUp asks another Question in a Thread, of any Kind ("sample this
// path" asks Sample Data in a Trace's Thread).
type FollowUp struct {
	ThreadID string
	Kind     session.QuestionKind
	Text     string
}

func (c FollowUp) events(e *Engine) ([]session.Payload, error) {
	if err := e.needAgent(); err != nil {
		return nil, err
	}
	t, err := e.thread(c.ThreadID)
	if err != nil {
		return nil, err
	}
	if t.Busy() || t.Status == session.StatusDraft {
		return nil, fmt.Errorf("%s is busy: its last Question is still %s", t.ID, t.Status)
	}
	text := strings.TrimSpace(c.Text)
	if text == "" {
		return nil, nil
	}
	return []session.Payload{session.FollowUpAsked{ThreadID: t.ID, Kind: c.Kind, Text: text}}, nil
}

// Cancel drops a queued Question. A running one can't be cancelled.
type Cancel struct{ ThreadID string }

func (c Cancel) events(e *Engine) ([]session.Payload, error) {
	t, err := e.thread(c.ThreadID)
	if err != nil {
		return nil, err
	}
	if t.Status != session.StatusQueued {
		return nil, errors.New("Running Questions can't be cancelled")
	}
	return []session.Payload{session.QuestionCancelled{ThreadID: t.ID}}, nil
}

// DeleteDraft deletes a Draft Question.
type DeleteDraft struct{ ThreadID string }

func (c DeleteDraft) events(e *Engine) ([]session.Payload, error) {
	t, err := e.thread(c.ThreadID)
	if err != nil {
		return nil, err
	}
	if t.Status != session.StatusDraft {
		return nil, fmt.Errorf("%s isn't a Draft", t.ID)
	}
	return []session.Payload{session.DraftDeleted{ThreadID: t.ID}}, nil
}

// MarkRead clears a Thread's "new" marker. Nothing happens if it's read.
type MarkRead struct{ ThreadID string }

func (c MarkRead) events(e *Engine) ([]session.Payload, error) {
	t := e.st.ThreadByID(c.ThreadID)
	if t == nil || !t.Unread {
		return nil, nil
	}
	return []session.Payload{session.ThreadRead{ThreadID: t.ID}}, nil
}

// ---- Feedback ----

// AddFeedback creates a Feedback draft (the editor opens on it at once, so
// the body may start empty).
type AddFeedback struct {
	Anchor session.Anchor
	Body   string
}

func (c AddFeedback) events(e *Engine) ([]session.Payload, error) {
	return []session.Payload{session.FeedbackCreated{
		FeedbackID: e.st.NextFeedbackID(), Anchor: e.st.LocateAnchor(c.Anchor), Body: c.Body, Status: session.FeedbackDraft,
	}}, nil
}

// EditFeedback replaces a body. A Refine running on it will come back
// Stale; a Revision already there goes Stale.
type EditFeedback struct {
	ID   string
	Body string
}

func (c EditFeedback) events(e *Engine) ([]session.Payload, error) {
	f, err := e.feedback(c.ID)
	if err != nil {
		return nil, err
	}
	if f.Status.Locked() {
		return nil, fmt.Errorf("%s is %s and can't be edited", f.ID, f.Status)
	}
	if f.Body == c.Body {
		return nil, nil
	}
	return []session.Payload{session.FeedbackEdited{FeedbackID: f.ID, Body: c.Body}}, nil
}

// DeleteFeedback deletes Feedback that hasn't been sent.
type DeleteFeedback struct{ ID string }

func (c DeleteFeedback) events(e *Engine) ([]session.Payload, error) {
	f, err := e.feedback(c.ID)
	if err != nil {
		return nil, err
	}
	if f.Status.Locked() {
		return nil, fmt.Errorf("%s is %s and can't be deleted", f.ID, f.Status)
	}
	return []session.Payload{session.FeedbackDeleted{FeedbackID: f.ID}}, nil
}

// ToggleReady flips Feedback between draft and ready.
type ToggleReady struct{ ID string }

func (c ToggleReady) events(e *Engine) ([]session.Payload, error) {
	f, err := e.feedback(c.ID)
	if err != nil {
		return nil, err
	}
	switch f.Status {
	case session.FeedbackDraft:
		return []session.Payload{session.FeedbackStatusChanged{FeedbackIDs: []string{f.ID}, Status: session.FeedbackReady}}, nil
	case session.FeedbackReady:
		return []session.Payload{session.FeedbackStatusChanged{FeedbackIDs: []string{f.ID}, Status: session.FeedbackDraft}}, nil
	}
	return nil, fmt.Errorf("%s is %s: it can't go back to draft", f.ID, f.Status)
}

// Refine asks for a Revision. Answers are the Reviewer's answers to a
// questions-back Revision, which re-runs it.
type Refine struct {
	ID      string
	Answers string
}

func (c Refine) events(e *Engine) ([]session.Payload, error) {
	if err := e.needAgent(); err != nil {
		return nil, err
	}
	f, err := e.feedback(c.ID)
	if err != nil {
		return nil, err
	}
	if f.Status.Locked() {
		return nil, fmt.Errorf("%s is %s and can't be refined", f.ID, f.Status)
	}
	switch f.Refine {
	case session.RefineQueued:
		return nil, fmt.Errorf("%s: Refine is already queued", f.ID)
	case session.RefineRunning:
		return nil, fmt.Errorf("%s: Refine is already running", f.ID)
	}
	round := 1
	if f.Revision != nil {
		round = f.Revision.Round + 1
	} else if f.RefineRound > 0 && c.Answers != "" {
		round = f.RefineRound + 1
	}
	return []session.Payload{session.RefineAsked{FeedbackID: f.ID, Base: f.Body, Answers: strings.TrimSpace(c.Answers), Round: round}}, nil
}

// AcceptRevision replaces the body with the Revision.
type AcceptRevision struct{ ID string }

func (c AcceptRevision) events(e *Engine) ([]session.Payload, error) {
	f, err := e.feedback(c.ID)
	if err != nil {
		return nil, err
	}
	r := f.Revision
	switch {
	case r == nil:
		return nil, fmt.Errorf("%s has no Revision: r refines it", f.ID)
	case r.Stale:
		return nil, fmt.Errorf("Stale: %s changed while Refine ran. r refines again, e edits", f.ID)
	case len(r.Questions) > 0 && r.Text == "":
		return nil, fmt.Errorf("%s's Revision is questions: answer them with R (or enter on one)", f.ID)
	case f.Status.Locked():
		return nil, fmt.Errorf("%s is %s", f.ID, f.Status)
	}
	return []session.Payload{session.RevisionAccepted{FeedbackID: f.ID}}, nil
}

// EditRevision changes the Revision's text; the body changes only on
// AcceptRevision.
type EditRevision struct {
	ID   string
	Text string
}

func (c EditRevision) events(e *Engine) ([]session.Payload, error) {
	f, err := e.feedback(c.ID)
	if err != nil {
		return nil, err
	}
	if f.Revision == nil {
		return nil, fmt.Errorf("%s has no Revision", f.ID)
	}
	if strings.TrimSpace(c.Text) == "" || c.Text == f.Revision.Text {
		return nil, nil
	}
	return []session.Payload{session.RevisionEdited{FeedbackID: f.ID, Text: c.Text}}, nil
}

// IgnoreRevision discards the Revision.
type IgnoreRevision struct{ ID string }

func (c IgnoreRevision) events(e *Engine) ([]session.Payload, error) {
	f, err := e.feedback(c.ID)
	if err != nil {
		return nil, err
	}
	if f.Revision == nil {
		return nil, nil
	}
	return []session.Payload{session.RevisionIgnored{FeedbackID: f.ID}}, nil
}

// ---- Sending ----

func (e *Engine) readyItems(ids []string) ([]string, error) {
	if !e.st.CanSend() {
		return nil, fmt.Errorf("the PR is %s: Feedback can't be sent", e.st.PRState())
	}
	var out []string
	for _, id := range ids {
		if f := e.st.FeedbackByID(id); f != nil && f.Status == session.FeedbackReady {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		return nil, errors.New("nothing ready to send")
	}
	return out, nil
}

// UnfilledNotes names the items with Agent Notes and counts the notes.
func UnfilledNotes(fs []*session.Feedback) (ids []string, n int) {
	for _, f := range fs {
		if k := len(session.AgentNotes(f.Body)); k > 0 {
			ids = append(ids, f.ID)
			n += k
		}
	}
	return
}

// PostReview sends ready Feedback to GitHub as one review, submitted at
// once. It's blocked while any Agent Note is unfilled. Through Do it runs
// synchronously (tests, the CLI); the TUI prepares it (PrepareReview),
// sends it off its goroutine (ReviewPost.Send) and records it (ReviewSent).
type PostReview struct {
	IDs     []string
	Event   string // Comment | Request changes | Approve
	Summary string
}

func (c PostReview) events(e *Engine) ([]session.Payload, error) {
	p, err := e.PrepareReview(c)
	if err != nil {
		return nil, err
	}
	res, err := p.Send(context.Background())
	if err != nil {
		return nil, err
	}
	return ReviewSent{Post: p, Result: res}.events(e)
}

// HandOff queues ready Feedback for the Implementing Agent. Unfilled
// Agent Notes are fine.
type HandOff struct{ IDs []string }

func (c HandOff) events(e *Engine) ([]session.Payload, error) {
	ids, err := e.readyItems(c.IDs)
	if err != nil {
		return nil, err
	}
	return []session.Payload{session.FeedbackStatusChanged{FeedbackIDs: ids, Status: session.FeedbackQueued}}, nil
}

// MarkHandedOff: the Implementing Agent pulled these queued items in
// (`pr-review export`). Items that aren't queued are left as they are.
type MarkHandedOff struct{ IDs []string }

func (c MarkHandedOff) events(e *Engine) ([]session.Payload, error) {
	var ids []string
	for _, id := range c.IDs {
		if f := e.st.FeedbackByID(id); f != nil && f.Status == session.FeedbackQueued {
			ids = append(ids, f.ID)
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	return []session.Payload{session.FeedbackStatusChanged{FeedbackIDs: ids, Status: session.FeedbackHandedOff}}, nil
}

// ToExport is what `pr-review export` hands over: the queued items, and
// with again the ones already handed off (not done) too.
func (e *Engine) ToExport(again bool) []*session.Feedback {
	var out []*session.Feedback
	for _, f := range e.st.Feedback {
		if f.Status == session.FeedbackQueued || (again && f.Status == session.FeedbackHandedOff) {
			out = append(out, f)
		}
	}
	return out
}

// RecordOutcome records what was done about handed-off Feedback
// (`pr-review resolve`). The commit is optional; a reason or question is
// required for declined and needs-you.
type RecordOutcome struct {
	ID      string
	Outcome string // addressed | declined | needs-you
	Note    string
	Commit  string
}

func (c RecordOutcome) events(e *Engine) ([]session.Payload, error) {
	f, err := e.feedback(c.ID)
	if err != nil {
		return nil, err
	}
	note := strings.TrimSpace(c.Note)
	switch c.Outcome {
	case session.OutcomeAddressed:
	case session.OutcomeDeclined, session.OutcomeNeedsYou:
		if note == "" {
			what := "a reason"
			if c.Outcome == session.OutcomeNeedsYou {
				what = "the question for the Reviewer"
			}
			return nil, fmt.Errorf("%s needs %s", c.Outcome, what)
		}
	default:
		return nil, fmt.Errorf("unknown Outcome %q: it's addressed, declined or needs-you", c.Outcome)
	}
	switch f.Status {
	case session.FeedbackHandedOff, session.FeedbackDone:
	case session.FeedbackQueued:
		return nil, fmt.Errorf("%s is queued but not handed off yet: `pr-review export` hands it off", f.ID)
	default:
		return nil, fmt.Errorf("%s is %s: only handed-off Feedback takes an Outcome", f.ID, f.Status)
	}
	return []session.Payload{session.OutcomeRecorded{FeedbackID: f.ID, Outcome: c.Outcome, Note: note, Commit: strings.TrimSpace(c.Commit)}}, nil
}

// MarkDone: the Reviewer is satisfied with handed-off Feedback, whatever
// its Outcome says.
type MarkDone struct{ ID string }

func (c MarkDone) events(e *Engine) ([]session.Payload, error) {
	f, err := e.feedback(c.ID)
	if err != nil {
		return nil, err
	}
	if f.Status != session.FeedbackHandedOff {
		return nil, fmt.Errorf("%s is %s: only handed-off Feedback can be marked done", f.ID, f.Status)
	}
	return []session.Payload{session.FeedbackStatusChanged{FeedbackIDs: []string{f.ID}, Status: session.FeedbackDone}}, nil
}

// Reopen takes Feedback that's queued, handed off or done back to a draft,
// to edit and send again, whatever its Outcome says.
type Reopen struct{ ID string }

func (c Reopen) events(e *Engine) ([]session.Payload, error) {
	f, err := e.feedback(c.ID)
	if err != nil {
		return nil, err
	}
	switch f.Status {
	case session.FeedbackQueued, session.FeedbackHandedOff, session.FeedbackDone:
	default:
		return nil, fmt.Errorf("%s is %s: only queued, handed-off or done Feedback can be reopened", f.ID, f.Status)
	}
	if !e.st.CanSend() {
		return nil, fmt.Errorf("the PR is %s: nothing can be sent again", e.st.PRState())
	}
	return []session.Payload{session.FeedbackStatusChanged{FeedbackIDs: []string{f.ID}, Status: session.FeedbackDraft}}, nil
}
