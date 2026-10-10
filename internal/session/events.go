package session

import (
	"encoding/json"
	"fmt"
	"reflect"
	"time"
)

// Event is one line of the Review Session's event log (ADR 0002): the
// engine emits it, the reducer (Apply) applies it, and the log stores it.
// Replaying every Event in order rebuilds the state.
type Event struct {
	ID     string    // "<writer>:<n>", unique across processes
	Writer string    // the process that appended it
	At     time.Time // when it happened
	Data   Payload   // what happened
}

// Type is the Event's type name, as stored in the log.
func (e Event) Type() string {
	if e.Data == nil {
		return ""
	}
	return e.Data.EventType()
}

// Payload is an Event's data. Every payload type is registered below.
type Payload interface{ EventType() string }

// RunInfo is what one agent run cost and which agent session it left.
// Total is the CLI's cumulative total_cost_usd for that session (it
// includes the parent's spend); Cost is this run's own share.
type RunInfo struct {
	SessionID  string  `json:"session_id,omitempty"`
	Total      float64 `json:"total_usd,omitempty"`
	Cost       float64 `json:"cost_usd,omitempty"`
	DurationMS int64   `json:"duration_ms,omitempty"`
	Model      string  `json:"model,omitempty"`
}

// MethodNote is the agent's two sentences about one Touched Method.
type MethodNote struct {
	Does    string `json:"does"`
	Changed string `json:"changed"`
}

// BriefingNotes is the agent-written part of the Briefing.
type BriefingNotes struct {
	TLDR            string                `json:"tldr"`
	ReadingOrder    []ReadingStep         `json:"reading_order"`
	Methods         map[string]MethodNote `json:"methods,omitempty"`
	ContractChanges []ContractChange      `json:"contract_changes,omitempty"`
}

// ---- the events ----

// Opened starts every log: the Pull Request as it was when the Review
// Session was created, and where its worktree and scratch directory live.
type Opened struct {
	PR       PullRequest `json:"pr"`
	Checkout string      `json:"checkout,omitempty"` // the Reviewer's checkout the session was opened from
	Worktree string      `json:"worktree,omitempty"`
	Scratch  string      `json:"scratch,omitempty"`
	Fixture  bool        `json:"fixture,omitempty"` // fixture mode: the canned agent
	RepoMode bool        `json:"repo_mode,omitempty"`
	Model    string      `json:"model,omitempty"`
	Effort   string      `json:"effort,omitempty"`
}

// Finished: the PR was merged or closed after the Review Session was
// created, so its worktree was removed and it's read-only now.
type Finished struct {
	State string `json:"state"`
}

type BriefingArrived struct {
	Notes BriefingNotes `json:"notes"`
	Run   RunInfo       `json:"run"`
}

type BriefingFailed struct {
	Error string  `json:"error"`
	Run   RunInfo `json:"run"`
}

// ThreadCreated: a new Question, sent (queued) or held as a Draft.
type ThreadCreated struct {
	ThreadID string       `json:"thread"`
	Anchor   Anchor       `json:"anchor"`
	Kind     QuestionKind `json:"kind"`
	Text     string       `json:"text"`
	Draft    bool         `json:"draft,omitempty"`
}

// FollowUpAsked: another Question in an existing Thread (queued). Reask:
// a Stale Question asked again at the new head.
type FollowUpAsked struct {
	ThreadID string       `json:"thread"`
	Kind     QuestionKind `json:"kind"`
	Text     string       `json:"text"`
	Reask    bool         `json:"reask,omitempty"`
}

// HeadMoved: the Reviewer updated the Review Session to the PR's new head
// (Update). The worktree was checked out at To, and the diff is now
// BaseTo..To. Every Thread's and Feedback's Anchor is given as it is at To
// (Stale where its lines changed), and the Threads whose Answers went
// Stale are listed.
type HeadMoved struct {
	From     string `json:"from"`
	To       string `json:"to"`
	BaseFrom string `json:"base_from"`
	BaseTo   string `json:"base_to"`
	Commits  int    `json:"commits,omitempty"` // From..To

	// The PR's metadata at To.
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Files       []string `json:"files,omitempty"`
	Additions   int      `json:"additions,omitempty"`
	Deletions   int      `json:"deletions,omitempty"`

	Anchors      map[string]Anchor `json:"anchors,omitempty"` // Thread or Feedback ID → its Anchor at To
	StaleAnswers []string          `json:"stale_answers,omitempty"`
}

type DraftsSent struct {
	ThreadIDs []string `json:"threads"`
}

type QuestionStarted struct {
	ThreadID string `json:"thread"`
}

type AnswerArrived struct {
	ThreadID string     `json:"thread"`
	Answer   AnswerBody `json:"answer"`
	Run      RunInfo    `json:"run"`
	Read     bool       `json:"read,omitempty"` // seeded as already read
}

type QuestionFailed struct {
	ThreadID string  `json:"thread"`
	Error    string  `json:"error"`
	Run      RunInfo `json:"run"`
}

type QuestionCancelled struct {
	ThreadID string `json:"thread"`
}

type DraftDeleted struct {
	ThreadID string `json:"thread"`
}

type ThreadRead struct {
	ThreadID string `json:"thread"`
}

type FeedbackCreated struct {
	FeedbackID string         `json:"feedback"`
	Anchor     Anchor         `json:"anchor"`
	Body       string         `json:"body"`
	Status     FeedbackStatus `json:"status"`
}

type FeedbackEdited struct {
	FeedbackID string `json:"feedback"`
	Body       string `json:"body"`
}

type FeedbackDeleted struct {
	FeedbackID string `json:"feedback"`
}

// FeedbackStatusChanged: ready ↔ draft, queued for the agent, handed off,
// posted.
type FeedbackStatusChanged struct {
	FeedbackIDs []string       `json:"feedback"`
	Status      FeedbackStatus `json:"status"`
}

// ReviewPosted: ready Feedback sent to GitHub as one review.
type ReviewPosted struct {
	FeedbackIDs []string `json:"feedback"`
	Event       string   `json:"event"` // Comment | Request changes | Approve
	Summary     string   `json:"summary,omitempty"`
	// What GitHub made: the review's ID and URL, and how many items went
	// in as line comments (the rest are in its body).
	ReviewID     int64  `json:"review_id,omitempty"`
	URL          string `json:"url,omitempty"`
	LineComments int    `json:"line_comments,omitempty"`
	// Local: there's no Forge (fixture mode), so nothing left the machine.
	Local bool `json:"local,omitempty"`
}

// OutcomeRecorded: whoever acted on handed-off Feedback said what they did
// (`pr-review resolve`, usually from another process).
type OutcomeRecorded struct {
	FeedbackID string `json:"feedback"`
	Outcome    string `json:"outcome"` // addressed | declined | needs-you
	Note       string `json:"note,omitempty"`
	Commit     string `json:"commit,omitempty"`
}

type RefineAsked struct {
	FeedbackID string `json:"feedback"`
	Base       string `json:"base"`              // the body being refined
	Answers    string `json:"answers,omitempty"` // answers to a questions-back Revision
	Round      int    `json:"round"`
}

type RefineStarted struct {
	FeedbackID string `json:"feedback"`
}

type RevisionArrived struct {
	FeedbackID string   `json:"feedback"`
	Revision   Revision `json:"revision"`
	Run        RunInfo  `json:"run"`
}

type RefineFailed struct {
	FeedbackID string  `json:"feedback"`
	Error      string  `json:"error"`
	Run        RunInfo `json:"run"`
}

type RevisionAccepted struct {
	FeedbackID string `json:"feedback"`
}

type RevisionEdited struct {
	FeedbackID string `json:"feedback"`
	Text       string `json:"text"`
}

type RevisionIgnored struct {
	FeedbackID string `json:"feedback"`
}

func (Opened) EventType() string                { return "opened" }
func (Finished) EventType() string              { return "finished" }
func (HeadMoved) EventType() string             { return "head_moved" }
func (BriefingArrived) EventType() string       { return "briefing_arrived" }
func (BriefingFailed) EventType() string        { return "briefing_failed" }
func (ThreadCreated) EventType() string         { return "thread_created" }
func (FollowUpAsked) EventType() string         { return "follow_up_asked" }
func (DraftsSent) EventType() string            { return "drafts_sent" }
func (QuestionStarted) EventType() string       { return "question_started" }
func (AnswerArrived) EventType() string         { return "answer_arrived" }
func (QuestionFailed) EventType() string        { return "question_failed" }
func (QuestionCancelled) EventType() string     { return "question_cancelled" }
func (DraftDeleted) EventType() string          { return "draft_deleted" }
func (ThreadRead) EventType() string            { return "thread_read" }
func (FeedbackCreated) EventType() string       { return "feedback_created" }
func (FeedbackEdited) EventType() string        { return "feedback_edited" }
func (FeedbackDeleted) EventType() string       { return "feedback_deleted" }
func (FeedbackStatusChanged) EventType() string { return "feedback_status" }
func (ReviewPosted) EventType() string          { return "review_posted" }
func (OutcomeRecorded) EventType() string       { return "outcome_recorded" }
func (RefineAsked) EventType() string           { return "refine_asked" }
func (RefineStarted) EventType() string         { return "refine_started" }
func (RevisionArrived) EventType() string       { return "revision_arrived" }
func (RefineFailed) EventType() string          { return "refine_failed" }
func (RevisionAccepted) EventType() string      { return "revision_accepted" }
func (RevisionEdited) EventType() string        { return "revision_edited" }
func (RevisionIgnored) EventType() string       { return "revision_ignored" }

var payloadTypes = map[string]func() any{}

func init() {
	for _, p := range []Payload{
		Opened{}, Finished{}, HeadMoved{}, BriefingArrived{}, BriefingFailed{},
		ThreadCreated{}, FollowUpAsked{}, DraftsSent{}, QuestionStarted{}, AnswerArrived{},
		QuestionFailed{}, QuestionCancelled{}, DraftDeleted{}, ThreadRead{},
		FeedbackCreated{}, FeedbackEdited{}, FeedbackDeleted{}, FeedbackStatusChanged{}, ReviewPosted{}, OutcomeRecorded{},
		RefineAsked{}, RefineStarted{}, RevisionArrived{}, RefineFailed{},
		RevisionAccepted{}, RevisionEdited{}, RevisionIgnored{},
	} {
		t := p.EventType()
		if _, dup := payloadTypes[t]; dup {
			panic("duplicate event type " + t)
		}
		payloadTypes[t] = newPayloadFunc(p)
	}
}

// newPayloadFunc returns a constructor for p's type: a pointer, so JSON
// can decode into it (deref turns it back into a value).
func newPayloadFunc(p Payload) func() any {
	t := reflect.TypeOf(p)
	return func() any { return reflect.New(t).Interface() }
}

// Unknown is an event type this build doesn't know (written by a newer
// version, say). It's kept and skipped by the reducer.
type Unknown struct {
	Type string
	Raw  json.RawMessage
}

func (u Unknown) EventType() string { return u.Type }

type eventJSON struct {
	ID     string          `json:"id"`
	Writer string          `json:"writer,omitempty"`
	At     time.Time       `json:"at"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data"`
}

func (e Event) MarshalJSON() ([]byte, error) {
	if u, ok := e.Data.(Unknown); ok {
		return json.Marshal(eventJSON{ID: e.ID, Writer: e.Writer, At: e.At, Type: u.Type, Data: u.Raw})
	}
	data, err := json.Marshal(e.Data)
	if err != nil {
		return nil, err
	}
	return json.Marshal(eventJSON{ID: e.ID, Writer: e.Writer, At: e.At, Type: e.Type(), Data: data})
}

func (e *Event) UnmarshalJSON(b []byte) error {
	var j eventJSON
	if err := json.Unmarshal(b, &j); err != nil {
		return err
	}
	e.ID, e.Writer, e.At = j.ID, j.Writer, j.At
	mk, ok := payloadTypes[j.Type]
	if !ok {
		e.Data = Unknown{Type: j.Type, Raw: j.Data}
		return nil
	}
	p := mk()
	if len(j.Data) > 0 && string(j.Data) != "null" {
		if err := json.Unmarshal(j.Data, p); err != nil {
			return fmt.Errorf("event %s (%s): %w", j.ID, j.Type, err)
		}
	}
	e.Data = deref(p)
	return nil
}

// deref turns the decoded *T back into T, so payloads are always values.
func deref(p any) Payload {
	return reflect.ValueOf(p).Elem().Interface().(Payload)
}
