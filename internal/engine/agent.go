package engine

import (
	"context"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

// Agent answers for the Review Session. Two implementations sit behind it:
// the live `claude -p` adapter and the fixture agent (recorded
// outputs, for tests and offline demos). The engine runs at most
// session.MaxRunning calls at once, each on its own goroutine, and never
// touches the state from them.
type Agent interface {
	// Briefing reads the Pull Request once. Its run is the parent every
	// Question and Refine forks from.
	Briefing(ctx context.Context, req BriefingRequest) (BriefingResult, error)
	// Ask forks a new Thread's first Question from req.Parent (the
	// Briefing's run; a fresh session when it has no SessionID).
	Ask(ctx context.Context, req QuestionRequest) (AnswerResult, error)
	// Continue asks a follow-up in the Thread's own agent session
	// (req.Parent).
	Continue(ctx context.Context, req QuestionRequest) (AnswerResult, error)
	// Refine proposes a Revision of a Feedback body, or questions back.
	// It forks from the Briefing, or resumes the Feedback's previous Refine
	// when it carries the Reviewer's answers.
	Refine(ctx context.Context, req RefineRequest) (RevisionResult, error)
}

// FileSummary is one changed file, as the Briefing is told about it.
type FileSummary struct {
	Path    string
	Status  string // added | modified | deleted
	Added   int
	Removed int
}

type BriefingRequest struct {
	PR      session.PullRequest
	Files   []FileSummary
	Methods []string // Touched Methods, as path::name keys
	// MaxBudgetUSD is the spending cap for the call (--max-budget-usd).
	MaxBudgetUSD float64
}

// QuestionRequest is one Question in a Thread.
type QuestionRequest struct {
	ThreadID   string
	Kind       session.QuestionKind // this Question's Kind
	ThreadKind session.QuestionKind // the Thread's first Kind
	Question   string
	Anchor     session.Anchor // the Thread's Anchor, line numbers filled in
	// AnchorCode is a diff Anchor's lines at the head (or the base, for
	// removed lines), one "N: text" per line. Empty for other Anchors.
	AnchorCode string
	// AnchorContext describes a non-code Anchor: the Answer, Feedback or
	// Briefing line it points at.
	AnchorContext string
	// Parent is the run this one forks (Ask) or resumes (Continue): its
	// agent session and its cumulative total.
	Parent       session.RunInfo
	MaxBudgetUSD float64
	// Reask: a Stale Question asked again after Update; Head is the head
	// the worktree is at now.
	Reask bool
	Head  string
}

type RefineRequest struct {
	FeedbackID    string
	Body          string // the Feedback body to refine
	Answers       string // the Reviewer's answers to a questions-back Revision
	Round         int
	Anchor        session.Anchor
	AnchorCode    string
	AnchorContext string
	// Research is what the Threads on overlapping Anchors found, one line
	// each: "T3 Define: gist".
	Research     []string
	Parent       session.RunInfo
	Resume       bool // resume Parent (answers to its questions) rather than fork it
	MaxBudgetUSD float64
}

type BriefingResult struct {
	Notes session.BriefingNotes
	Run   session.RunInfo
}

type AnswerResult struct {
	Answer session.AnswerBody
	Run    session.RunInfo
}

type RevisionResult struct {
	Revision session.Revision
	Run      session.RunInfo
}

// RunError is an agent failure that still cost something (a run stopped
// by its budget cap, say).
type RunError struct {
	Err error
	Run session.RunInfo
}

func (e *RunError) Error() string { return e.Err.Error() }
func (e *RunError) Unwrap() error { return e.Err }
