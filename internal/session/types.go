// Package session is the Review Session's domain model: plain structs, the
// events that change them (events.go) and the reducer that applies an
// event (apply.go). State is only ever changed by applying events, which is
// how the engine replays the event log (ADR 0002). The package knows
// nothing about Bubble Tea or the agent. Vocabulary follows GLOSSARY.md.
package session

import (
	"strings"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis"
)

// ---- Pull Request, diff, files ----

type PullRequest struct {
	RepoURL     string
	OwnerRepo   string
	GoModule    string
	Number      int
	URL         string
	Title       string
	Author      string
	HeadRef     string
	BaseRef     string
	MergedAt    string
	State       string // open | merged | closed ("" in fixture and -repo modes: treated as open)
	BaseSHA     string // the diff's base: the merge base of the PR's base and head
	HeadSHA     string
	Description string
	Files       []string
	Additions   int
	Deletions   int
}

type FileStatus int

const (
	FileModified FileStatus = iota
	FileAdded
	FileDeleted
)

func (s FileStatus) Glyph() string {
	switch s {
	case FileAdded:
		return "+"
	case FileDeleted:
		return "-"
	default:
		return "~"
	}
}

type LineKind int

const (
	LineContext LineKind = iota
	LineAdded
	LineRemoved
)

// DiffLine is one merged line of a file (old and new sides merged).
type DiffLine struct {
	Kind  LineKind
	OldNo int // 0 if new-side only
	NewNo int // 0 if old-side only
	Text  string
}

// Hunk is a window [Start,End) into FileDiff.All, indices into All.
type Hunk struct {
	Start, End int
}

// FileDiff holds one changed file as merged diff lines.
type FileDiff struct {
	Path     string
	Status   FileStatus
	Binary   bool
	All      []DiffLine // every line, context+added+removed, in order
	Hunks    []Hunk     // merged windows with 3 lines of context
	Added    int
	Removed  int
	ParseErr string // the code analysis couldn't parse it cleanly, if set
}

// ---- Touched Methods & Briefing ----

type MethodStatus int

const (
	MethodAdded MethodStatus = iota
	MethodRemoved
	MethodChanged
)

// Method is a Touched Method (from the code analysis) with the Briefing's
// agent-written sentences and the resolved call graph (callers/callees,
// among Touched Methods).
type Method struct {
	Key                      string
	Path                     string
	Name                     string
	Owner                    string // "" for a function; Go's receiver "T"/"*T", or the class or module
	Status                   MethodStatus
	NewStart, NewEnd         int
	OldStart, OldEnd         int
	NewDeclLine, OldDeclLine int    // the declaration's own first line, excluding leading comments
	Does                     string // from Briefing fixture; empty until the agent run lands
	Changed                  string
	Callers                  []string // bare names, for the call-graph header row
	Callees                  []string // bare names, for the call-graph header row
	CallerKeys               []string // method keys, for the Briefing's call-map tree
	CalleeKeys               []string
}

func (m Method) DisplayName() string {
	if m.Owner == "" {
		return m.Name
	}
	r := m.Owner
	if len(r) > 0 && r[0] == '*' {
		r = r[1:]
	}
	return r + "." + m.Name
}

// QualifiedName prefixes the package directory's last element, for use
// outside the method's own file.
func (m Method) QualifiedName(pkgDir string) string {
	if pkgDir == "" {
		return m.DisplayName()
	}
	return pkgDir + "." + m.DisplayName()
}

func fromStatus(s codeanalysis.Status) MethodStatus {
	switch s {
	case codeanalysis.Added:
		return MethodAdded
	case codeanalysis.Removed:
		return MethodRemoved
	default:
		return MethodChanged
	}
}

type ContractCategory int

const (
	CategoryEndpoint ContractCategory = iota
	CategorySchema
	CategoryConfig
	CategoryExported
)

func (c ContractCategory) String() string {
	switch c {
	case CategoryEndpoint:
		return "Endpoints"
	case CategorySchema:
		return "Database schema"
	case CategoryConfig:
		return "Config and env"
	default:
		return "Exported signatures"
	}
}

// ContractChangeAnchor is a clickable path:line inside a Contract Change.
type ContractChangeAnchor struct {
	Path string
	Line int
}

// ContractChange is one entry in the Briefing's Contract Changes section:
// either computed (Computed=true, marked with a gear) or agent-written.
type ContractChange struct {
	Category ContractCategory
	Title    string
	Detail   string
	Anchors  []ContractChangeAnchor
	Computed bool
}

type ReadingStep struct {
	Path    string
	Summary string // empty until the agent run lands
}

// Briefing is the agent's up-front orientation to the Pull Request.
type Briefing struct {
	Ready bool // agent-written parts have landed
	// Stale: the agent-written parts are from an older head (StaleFrom);
	// they stay visible until the new Briefing arrives.
	Stale           bool
	StaleFrom       string
	TLDR            string
	ReadingOrder    []ReadingStep
	ContractChanges []ContractChange
	Methods         map[string]*Method // key -> Method, computed + agent sentences
	MethodOrder     []string           // reading order of method keys
}

// ---- Anchors ----

type AnchorType int

const (
	AnchorDiff AnchorType = iota
	AnchorFile
	AnchorDescription
	AnchorBriefing
	AnchorAnswer
	AnchorPR
	AnchorComment // a PR Comment: Ref is its ID
)

type Side int

const (
	SideNew Side = iota
	SideOld
)

// Anchor is what a Thread (or Feedback) is attached to.
type Anchor struct {
	Type AnchorType

	// AnchorDiff / AnchorFile
	Path     string
	RowStart int // AnchorDiff: inclusive indices into FileDiff.All
	RowEnd   int // AnchorDiff: the Anchor "ends" here (marker row)

	// AnchorDescription: span [Start,End) rune offsets into the description
	// AnchorBriefing: Ref e.g. "tldr", "reading:<path>", "contract:<i>", "method:<key>", "callmap"
	Ref string

	// AnchorAnswer
	ThreadID string
	Entry    int

	// AnchorDiff: the first and last line on each side of the diff (0 when
	// the Anchor has no line on that side). Filled in by the engine.
	NewStart, NewEnd int
	OldStart, OldEnd int

	// Selected text, quoted back to the Reviewer and the agent.
	Text string

	// Stale: a newer head edited the anchored lines (or deleted the file,
	// which moves the Anchor onto the whole PR). Its rows are a best guess
	// and Text is what it was anchored to at StaleFrom (a head sha).
	Stale     bool   `json:",omitempty"`
	StaleFrom string `json:",omitempty"`
}

// ---- Question Kinds ----

type QuestionKind int

const (
	Define QuestionKind = iota
	Summarize
	SampleData
	BeforeAfter
	Trace
	TestCases
	Research
)

// Kinds are the 7 Question Kinds. Before/After and Research are reached
// through Ask… (r); the others have their own key.
var Kinds = []QuestionKind{Define, Summarize, SampleData, BeforeAfter, Trace, TestCases, Research}

func (k QuestionKind) String() string {
	return [...]string{"Define", "Summarize", "Sample Data", "Before/After", "Trace", "Test Cases", "Research"}[k]
}

func (k QuestionKind) FixtureName() string {
	return [...]string{"define", "summarize", "sample_data", "before_after", "trace", "test_cases", "research"}[k]
}

func (k QuestionKind) Key() string {
	return [...]string{"d", "s", "x", "", "t", "c", ""}[k]
}

func KindFromFixture(s string) (QuestionKind, bool) {
	for _, k := range Kinds {
		if k.FixtureName() == s {
			return k, true
		}
	}
	return 0, false
}

// DefaultQuestion is the Question text used when no text was typed.
func (k QuestionKind) DefaultQuestion(anchorText string) string {
	q := quoteShort(anchorText)
	switch k {
	case Define:
		return "What does " + q + " mean in this context?"
	case Summarize:
		return "In one sentence, what does " + q + " do?"
	case SampleData:
		return "Show sample data flowing through " + q + "."
	case BeforeAfter:
		return "How does the data for " + q + " look before and after this change?"
	case Trace:
		return "Trace " + q + ": what calls it and what does it call?"
	case TestCases:
		return "What tests cover " + q + "? Which did this PR add or change?"
	default:
		return "Research " + q + "."
	}
}

func quoteShort(s string) string {
	// A multi-line Anchor (two YAML lines, say) must read as one line.
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 60 {
		s = string(r[:57]) + "…"
	}
	return "“" + s + "”"
}

// ---- Threads, Entries, Probes ----

type ThreadStatus int

const (
	StatusDraft ThreadStatus = iota
	StatusQueued
	StatusRunning
	StatusFailed // the agent run failed; a note says why
	StatusAnswered
)

func (s ThreadStatus) String() string {
	return [...]string{"draft", "queued", "running", "failed", "answered"}[s]
}

type EntryRole int

const (
	RoleQuestion EntryRole = iota
	RoleAnswer
	RoleNote
)

type Citation struct {
	Path    string
	Line    int
	EndLine int
	URL     string
	Note    string
}

type TestCase struct {
	Case        string
	Location    string
	Description string
	Change      string // added|changed|untouched
}

// Ran is one thing the agent ran while answering (a Probe): the command and
// a one-line result. An Answer with a Ran entry confirming it is Verified.
type Ran struct {
	Command string `json:"command"`
	Result  string `json:"result"`
}

// SampleStep is one value in a Sample Data or Before/After Answer.
type SampleStep struct {
	Label     string
	At        string // path:line
	Value     string
	Lang      string
	Source    string // fixture|spec|test|invented
	SourceRef string // path:line of the fixture/spec/test it came from
}

// Hop is one step of a Trace.
type Hop struct {
	At   string // path:line
	Fn   string
	Text string
}

// AnswerBody is the agent's Answer in the union schema's shape: a gist, an
// optional detail (markdown, collapsed until asked for), Citations, what
// the agent ran, and the Kind's own fields.
type AnswerBody struct {
	Kind      QuestionKind
	Verified  bool // a Ran entry confirms it
	Gist      string
	Detail    string
	Citations []Citation
	Ran       []Ran

	Term, Example string       // Define
	Changed       string       // Summarize
	Steps         []SampleStep // Sample Data
	Before, After *SampleStep  // Before/After
	Note          string       // Before/After
	Hops          []Hop        // Trace
	Cases         []TestCase   // Test Cases
	Verdict, Why  string       // Research
	Evidence      []Citation   // Research
	HasKindData   bool         // the Kind's object was present
}

// Entry is one Question, Answer, or one-line note in a Thread.
type Entry struct {
	Role   EntryRole
	Kind   QuestionKind // the Question's (or its Answer's) Kind
	Text   string       // Question text, or note text
	A      *AnswerBody  // Answer entries only
	Unread bool
	At     time.Time
	// Stale: an Answer from an older head whose Anchor went Stale or whose
	// Citations point at code that changed since.
	Stale bool
	// Reask: a Question asked again at a new head.
	Reask bool
}

// Verified is an Answer entry's Verified flag.
func (e Entry) Verified() bool { return e.A != nil && e.A.Verified }

type Thread struct {
	ID      string
	Anchor  Anchor
	Kind    QuestionKind
	Status  ThreadStatus
	Entries []Entry
	Unread  bool // any entry unread
	Created time.Time

	// The Thread's own agent session (a fork of the Briefing's), which
	// follow-ups resume, and its cumulative cost so far.
	AgentSession string
	AgentTotal   float64
	Cost         float64 // this Thread's own spend
}

// Busy: a Question in the Thread is queued or running.
func (t *Thread) Busy() bool { return t.Status == StatusQueued || t.Status == StatusRunning }

// PendingKind is the Kind of the Thread's latest Question: a follow-up
// ("sample this path") can ask a different Kind than the Thread's.
func (t *Thread) PendingKind() QuestionKind {
	for i := len(t.Entries) - 1; i >= 0; i-- {
		if t.Entries[i].Role == RoleQuestion {
			return t.Entries[i].Kind
		}
	}
	return t.Kind
}

// Stale: the Thread's latest Answer is Stale (and nothing newer is on
// its way).
func (t *Thread) Stale() bool {
	i := t.LatestAnswerIdx()
	return i >= 0 && t.Entries[i].Stale && !t.Busy()
}

// LatestQuestion is the Thread's latest Question entry.
func (t *Thread) LatestQuestion() *Entry {
	for i := len(t.Entries) - 1; i >= 0; i-- {
		if t.Entries[i].Role == RoleQuestion {
			return &t.Entries[i]
		}
	}
	return nil
}

func (t *Thread) LatestAnswerIdx() int {
	for i := len(t.Entries) - 1; i >= 0; i-- {
		if t.Entries[i].Role == RoleAnswer {
			return i
		}
	}
	return -1
}

// ---- Feedback ----

type FeedbackStatus int

const (
	FeedbackDraft FeedbackStatus = iota
	FeedbackReady
	FeedbackQueued    // queued for the Implementing Agent (Hand-off)
	FeedbackHandedOff // pulled in by the Implementing Agent (export)
	FeedbackPosted    // posted to GitHub as part of a review
	FeedbackDone      // the Reviewer marked handed-off Feedback done
)

func (s FeedbackStatus) String() string {
	return [...]string{"draft", "ready", "queued for agent", "handed off", "posted", "done"}[s]
}

// Locked: sent one way or another, so it can't be edited or deleted.
func (s FeedbackStatus) Locked() bool { return s >= FeedbackQueued }

// RefineState is where a Feedback's Refine is in the agent queue.
type RefineState int

const (
	RefineNone RefineState = iota
	RefineQueued
	RefineRunning
)

// Revision is the agent's proposed rewording of a Feedback body, or, when
// the Reviewer's intent is unclear, questions back.
type Revision struct {
	Text        string // revised_text
	WhatChanged string
	Questions   []string
	Citations   []Citation
	Gist        string
	BaseBody    string // the body the Revision was made from
	Stale       bool   // the body was edited while Refine ran
	Placeholder bool   // no canned Revision: the fixture agent filled the notes itself
	Edited      bool   // the Reviewer edited the Revision's text (e)
	Round       int
}

type Feedback struct {
	ID      string
	Anchor  Anchor
	Status  FeedbackStatus
	Body    string
	Created time.Time

	Revision *Revision
	Refine   RefineState
	PrevBody string // the body before the last accepted Revision

	// The Refine in the queue: the body it refines, the Reviewer's answers
	// to a questions-back Revision, and its round.
	RefineBase, RefineAnswers string
	RefineRound               int

	RefineError string // the last Refine failed

	// The last Refine's agent session: answering its questions resumes it.
	AgentSession string
	AgentTotal   float64
	Cost         float64

	// Outcomes are what whoever acted on the handed-off Feedback reported
	// (`pr-review resolve`), oldest first. Optional, and advisory: the
	// Reviewer marks it done or reopens it whatever they say.
	Outcomes []Outcome
}

// Outcome kinds.
const (
	OutcomeAddressed = "addressed"
	OutcomeDeclined  = "declined"
	OutcomeNeedsYou  = "needs-you"
)

// Outcome is what was done about one piece of handed-off Feedback.
type Outcome struct {
	Kind   string // addressed | declined | needs-you
	Note   string // what changed, the reason, or the question
	Commit string // optional, for addressed
	At     time.Time
}

// LastOutcome is the latest Outcome, or nil.
func (f *Feedback) LastOutcome() *Outcome {
	if len(f.Outcomes) == 0 {
		return nil
	}
	return &f.Outcomes[len(f.Outcomes)-1]
}

// Counts summarises the header/overview badges.
type Counts struct {
	New        int
	Failed     int
	Drafts     int
	Running    int // Status == running (at most MaxRunning)
	Queued     int // Status == queued, waiting for a slot
	MaxRunning int
}

// ---- PR Comments ----

// PRComment is a comment already on the Pull Request in GitHub, read-only
// here. Fetched from the Forge when the Review Session opens (never logged,
// like the diff).
type PRComment struct {
	ID        string // rc… review comment, ic… issue comment, rv… review body
	Kind      string // review | issue | review_body
	Author    string
	Body      string
	CreatedAt string
	URL       string
	// Review comments: where GitHub put it (Line on CommitID, OriginalLine
	// on OriginalCommit; Line 0 once outdated).
	Path           string
	Line           int
	StartLine      int
	Side           string // RIGHT | LEFT
	OriginalLine   int
	CommitID       string
	OriginalCommit string
	InReplyTo      string
}

// General: not on a line (an issue comment or a review's body).
func (c *PRComment) General() bool { return c.Path == "" }
