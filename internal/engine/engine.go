// Package engine owns a Review Session: its state, the agent queue, the
// event log and (through the Forge) the Pull Request. It has a message
// API: commands go in (Do), events come out. Agent runs happen on
// goroutines that never touch the state; each finished run arrives on the
// Inbox channel, and the client hands it back with Handle on its own
// goroutine, which is where every state change happens. The engine doesn't
// know about Bubble Tea: the TUI adapts Inbox into a tea.Cmd.
package engine

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// Budgets are the per-call spending caps, in USD. A fork's cap is its
// parent's cumulative total plus its own budget.
type Budgets struct {
	Briefing float64
	Question float64 // a Question or a Refine
}

// DefaultBudgets are the spec's defaults.
var DefaultBudgets = Budgets{Briefing: 0.50, Question: 0.25}

// Store is where the events are kept: the JSONL event log, or memory.
type Store interface {
	// Load returns every event so far, in order.
	Load() ([]session.Event, error)
	// Append adds events at the end.
	Append(evs ...session.Event) error
	// Poll returns the events other processes appended since the last
	// Load, Append or Poll.
	Poll() ([]session.Event, error)
}

// Config opens an Engine.
type Config struct {
	// Repo is the diff's source: base and head, read with git.
	Repo *gitsrc.Repo
	// Opened is the first event of a new log. For an existing log it's
	// ignored: the log's own Opened wins.
	Opened session.Opened
	// Seed adds events to a new log after Opened (the fixture seeds).
	Seed func(*session.Session) []session.Payload
	// Store keeps the events; nil keeps them in memory only.
	Store Store
	// Agent answers; nil means no agent (a read-only session).
	Agent Agent
	// NoBriefing: don't run the Briefing (-repo mode has none).
	NoBriefing bool
	Budgets    Budgets
	// Now is the clock (tests inject one).
	Now func() time.Time
	// Forge is GitHub (nil in fixture and -repo modes: nothing leaves the
	// machine). It tells who the Reviewer is, posts reviews, fetches PR
	// Comments and watches the PR's head.
	Forge forge.Forge
	// Callers finds the Go Touched Methods' exact callers (GoCallers); nil
	// doesn't (-repo mode, most tests): the name-matched callers stay.
	Callers CallersFunc
	// CacheDir is the Review Session's data directory, where the callers
	// are cached per head ("" doesn't cache).
	CacheDir string
}

// Input is a finished agent run, waiting on the Inbox for Handle.
type Input struct {
	key    string // "briefing", a Thread ID, or "fb:" + a Feedback ID
	p      session.Payload
	parent float64 // the parent run's cumulative total, for the run's own cost
}

// Engine runs one Review Session.
type Engine struct {
	st      *session.Session
	cfg     Config
	store   Store
	agent   Agent
	forge   forge.Forge
	writer  string
	seq     int
	inbox   chan Input
	ctx     context.Context
	cancel  context.CancelFunc
	queue   []string        // keys waiting for a slot, in order
	active  map[string]bool // keys running on a goroutine in this process
	started bool

	briefingTried   bool
	briefingRunning bool
	storeErr        error

	watch    watch // when to check the PR's head
	updating bool  // an Update is fetching and checking out: nothing starts

	callers *callersRun // the callers analysis, while it computes
}

// Open builds the diff view from git, then replays the log (or starts a
// new one with cfg.Opened and the seeds). Nothing runs until Start.
func Open(cfg Config) (*Engine, []string, error) {
	if cfg.Store == nil {
		cfg.Store = &MemStore{}
	}
	evs, err := cfg.Store.Load()
	if err != nil {
		return nil, nil, fmt.Errorf("reading the event log: %w", err)
	}
	// After an Update the diff is the latest head's, and "Since last
	// review" is the diff from the head before it.
	repo, since := cfg.Repo, [2]string{}
	for _, ev := range evs {
		if hm, ok := ev.Data.(session.HeadMoved); ok {
			repo = gitsrc.OpenRepo(cfg.Repo.Dir, hm.BaseTo, hm.To)
			since = [2]string{hm.From, hm.To}
		}
	}
	st, notes, err := session.Build(repo)
	if err != nil {
		return nil, nil, err
	}
	if since[0] != "" {
		files, order, err := session.BuildSince(gitsrc.OpenRepo(repo.Dir, since[0], since[1]))
		if err != nil {
			notes = append(notes, "\"Since last review\" isn't available: "+err.Error())
		} else {
			st.SetSince(files, order)
		}
	}
	if cfg.Budgets == (Budgets{}) {
		cfg.Budgets = DefaultBudgets
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	ctx, cancel := context.WithCancel(context.Background())
	e := &Engine{
		st: st, cfg: cfg, store: cfg.Store, agent: cfg.Agent, forge: cfg.Forge,
		writer: newWriterID(), inbox: make(chan Input, 64),
		ctx: ctx, cancel: cancel, active: map[string]bool{},
	}
	e.watch.lastInput = cfg.Now()
	if len(evs) == 0 {
		if _, err := e.commit(cfg.Opened); err != nil {
			cancel()
			return nil, nil, err
		}
		if cfg.Seed != nil {
			if _, err := e.commit(cfg.Seed(e.st)...); err != nil {
				cancel()
				return nil, nil, err
			}
		}
	} else {
		for _, ev := range evs {
			e.st.Apply(ev)
			e.track(ev)
		}
	}
	if n := e.detectAuthor(); n != "" {
		notes = append(notes, n)
	}
	return e, notes, nil
}

// detectAuthor: the Reviewer is the Author when the Forge's authenticated
// user opened the PR. It returns a note when it can't tell.
func (e *Engine) detectAuthor() string {
	if e.forge == nil || e.st.Fixture || e.st.RepoMode {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	login, err := e.forge.User(ctx)
	if err != nil || login == "" {
		return fmt.Sprintf("couldn't tell who you are on GitHub (%v): Send defaults to a GitHub review", err)
	}
	e.st.ReviewerLogin = login
	e.st.ReviewerIsAuthor = strings.EqualFold(login, e.st.PR.Author)
	return ""
}

// Forge is the engine's GitHub (nil in fixture and -repo modes).
func (e *Engine) Forge() forge.Forge { return e.forge }

// newWriterID names this process in event IDs: other processes (the
// export and resolve) append to the same log.
func newWriterID() string {
	return fmt.Sprintf("%d-%04x", os.Getpid(), rand.Intn(1<<16))
}

// State is the Review Session as it stands. The client may read it on the
// goroutine it calls Do and Handle from, and must not change it.
func (e *Engine) State() *session.Session { return e.st }

// Inbox delivers finished agent runs. Hand each one to Handle.
func (e *Engine) Inbox() <-chan Input { return e.inbox }

// StoreErr is the last error appending to the log, if any.
func (e *Engine) StoreErr() error { return e.storeErr }

// HasAgent reports whether Questions can be asked.
func (e *Engine) HasAgent() bool { return e.agent != nil && !e.st.ReadOnly() }

// Start begins agent work: the Briefing (unless it already landed), then
// whatever the log left queued or running, re-queued ahead of the rest.
func (e *Engine) Start() []session.Event {
	if e.started {
		return nil
	}
	e.started = true
	// Runs a previous process started but never finished go first.
	var interrupted []string
	for _, t := range e.st.Threads {
		if t.Status == session.StatusRunning && !e.active[t.ID] {
			interrupted = append(interrupted, t.ID)
		}
	}
	for _, f := range e.st.Feedback {
		if f.Refine == session.RefineRunning && !e.active["fb:"+f.ID] {
			interrupted = append(interrupted, "fb:"+f.ID)
		}
	}
	e.queue = append(interrupted, e.queue...)
	evs := e.startCallers()
	return append(evs, e.dispatch()...)
}

// Close stops every running agent call. Queued work stays queued in the
// log and runs next time.
func (e *Engine) Close() { e.cancel() }

// Idle reports whether nothing is queued or running.
func (e *Engine) Idle() bool {
	return len(e.active) == 0 && !e.briefingRunning && (len(e.queue) == 0 || !e.HasAgent())
}

// Handle commits a finished run from the Inbox and starts whatever can
// run next. It returns the events that came out.
func (e *Engine) Handle(in Input) []session.Event {
	if in.key == callersKey {
		return e.handleCallers(in)
	}
	if in.key == "briefing" {
		e.briefingRunning = false
	} else {
		delete(e.active, in.key)
	}
	p := e.finishPayload(in)
	out, _ := e.commit(p)
	return append(out, e.dispatch()...)
}

// Pump waits up to d for finished runs and handles them, until the engine
// is idle. For tests and the CLI; the TUI uses Inbox.
func (e *Engine) Pump(d time.Duration) []session.Event {
	var out []session.Event
	deadline := time.After(d)
	for !e.Idle() {
		select {
		case in := <-e.inbox:
			out = append(out, e.Handle(in)...)
		case <-deadline:
			return out
		}
	}
	return out
}

// Poll picks up events other processes appended to the log.
func (e *Engine) Poll() []session.Event {
	all, err := e.store.Poll()
	if err != nil {
		e.storeErr = err
		return nil
	}
	var evs []session.Event
	for _, ev := range all {
		if ev.Writer == e.writer {
			continue // already applied when this process wrote it
		}
		e.st.Apply(ev)
		e.track(ev)
		evs = append(evs, ev)
	}
	if len(evs) > 0 {
		evs = append(evs, e.dispatch()...)
	}
	return evs
}

// commit stamps, logs and applies payloads, in order.
func (e *Engine) commit(ps ...session.Payload) ([]session.Event, error) {
	var out []session.Event
	for _, p := range ps {
		if p == nil {
			continue
		}
		e.seq++
		ev := session.Event{ID: fmt.Sprintf("%s:%d", e.writer, e.seq), Writer: e.writer, At: e.cfg.Now(), Data: p}
		if err := e.store.Append(ev); err != nil {
			e.storeErr = err
			return out, fmt.Errorf("writing the event log: %w", err)
		}
		e.st.Apply(ev)
		e.track(ev)
		out = append(out, ev)
	}
	return out, nil
}

// track keeps the run queue in step with the events: the same code runs
// for live events and for replay, so the queue comes back after a restart.
func (e *Engine) track(ev session.Event) {
	switch p := ev.Data.(type) {
	case session.ThreadCreated:
		if !p.Draft {
			e.enqueue(p.ThreadID)
		}
	case session.FollowUpAsked:
		e.enqueue(p.ThreadID)
	case session.DraftsSent:
		for _, id := range p.ThreadIDs {
			e.enqueue(id)
		}
	case session.QuestionStarted:
		e.dequeue(p.ThreadID)
	case session.QuestionCancelled:
		e.dequeue(p.ThreadID)
	case session.DraftDeleted:
		e.dequeue(p.ThreadID)
	case session.RefineAsked:
		e.enqueue("fb:" + p.FeedbackID)
	case session.RefineStarted:
		e.dequeue("fb:" + p.FeedbackID)
	case session.FeedbackDeleted:
		e.dequeue("fb:" + p.FeedbackID)
	case session.HeadMoved:
		e.briefingTried = false // the Briefing is regenerated at the new head
	}
}

func (e *Engine) enqueue(key string) {
	for _, k := range e.queue {
		if k == key {
			return
		}
	}
	e.queue = append(e.queue, key)
}

func (e *Engine) dequeue(key string) {
	for i, k := range e.queue {
		if k == key {
			e.queue = append(e.queue[:i], e.queue[i+1:]...)
			return
		}
	}
}

// Queued is the run queue's keys, in order (Thread IDs, "fb:F1" for a
// Refine).
func (e *Engine) Queued() []string { return append([]string(nil), e.queue...) }

// needBriefing: the Briefing hasn't landed and this process hasn't tried.
func (e *Engine) needBriefing() bool {
	b := e.st.Briefing
	return !e.cfg.NoBriefing && b != nil && (!b.Ready || b.Stale) && !e.briefingTried
}

// dispatch starts the Briefing if it's due, then queued work while there's
// a slot. Questions and Refines wait for the Briefing: they fork from it.
func (e *Engine) dispatch() []session.Event {
	if !e.started || !e.HasAgent() || e.updating {
		return nil
	}
	if e.needBriefing() {
		e.briefingTried = true
		e.briefingRunning = true
		e.launchBriefing()
	}
	if e.briefingRunning {
		return nil
	}
	var out []session.Event
	for len(e.active) < session.MaxRunning && len(e.queue) > 0 {
		key := e.queue[0]
		e.queue = e.queue[1:]
		if fid, ok := strings.CutPrefix(key, "fb:"); ok {
			f := e.st.FeedbackByID(fid)
			if f == nil || f.Refine == session.RefineNone {
				continue
			}
			req := e.refineRequest(f)
			evs, _ := e.commit(session.RefineStarted{FeedbackID: fid})
			out = append(out, evs...)
			e.active[key] = true
			e.launchRefine(key, req)
			continue
		}
		th := e.st.ThreadByID(key)
		if th == nil || !th.Busy() {
			continue
		}
		req, fork := e.questionRequest(th)
		evs, _ := e.commit(session.QuestionStarted{ThreadID: th.ID})
		out = append(out, evs...)
		e.active[key] = true
		e.launchQuestion(key, req, fork)
	}
	return out
}

// send delivers a finished run to the Inbox, unless the engine closed.
func (e *Engine) send(in Input) {
	select {
	case e.inbox <- in:
	case <-e.ctx.Done():
	}
}

func (e *Engine) launchBriefing() {
	req := e.briefingRequest()
	go func() {
		res, err := e.agent.Briefing(e.ctx, req)
		if err != nil {
			e.send(Input{key: "briefing", p: session.BriefingFailed{Error: err.Error(), Run: runOf(err)}})
			return
		}
		e.send(Input{key: "briefing", p: session.BriefingArrived{Notes: res.Notes, Run: res.Run}})
	}()
}

func (e *Engine) launchQuestion(key string, req QuestionRequest, fork bool) {
	go func() {
		var res AnswerResult
		var err error
		if fork {
			res, err = e.agent.Ask(e.ctx, req)
		} else {
			res, err = e.agent.Continue(e.ctx, req)
		}
		if err != nil {
			e.send(Input{key: key, parent: req.Parent.Total, p: session.QuestionFailed{ThreadID: req.ThreadID, Error: err.Error(), Run: runOf(err)}})
			return
		}
		e.send(Input{key: key, parent: req.Parent.Total, p: session.AnswerArrived{ThreadID: req.ThreadID, Answer: res.Answer, Run: res.Run}})
	}()
}

func (e *Engine) launchRefine(key string, req RefineRequest) {
	go func() {
		res, err := e.agent.Refine(e.ctx, req)
		if err != nil {
			e.send(Input{key: key, parent: req.Parent.Total, p: session.RefineFailed{FeedbackID: req.FeedbackID, Error: err.Error(), Run: runOf(err)}})
			return
		}
		e.send(Input{key: key, parent: req.Parent.Total, p: session.RevisionArrived{FeedbackID: req.FeedbackID, Revision: res.Revision, Run: res.Run}})
	}()
}

func runOf(err error) session.RunInfo {
	var re *RunError
	if errors.As(err, &re) {
		return re.Run
	}
	return session.RunInfo{}
}

// finishPayload accounts a finished run's own cost (its cumulative total
// minus its parent's) and rewrites worktree paths to repo-relative.
func (e *Engine) finishPayload(in Input) session.Payload {
	own := func(r session.RunInfo) session.RunInfo {
		if r.Total > 0 {
			r.Cost = r.Total - in.parent
			if r.Cost < 0 {
				r.Cost = 0
			}
		}
		return r
	}
	root := e.st.WorktreePath
	switch p := in.p.(type) {
	case session.BriefingArrived:
		p.Run = own(p.Run)
		p.Notes = session.RelativeBriefing(p.Notes, root)
		return p
	case session.BriefingFailed:
		p.Run = own(p.Run)
		return p
	case session.AnswerArrived:
		p.Run = own(p.Run)
		p.Answer = session.RelativeAnswer(p.Answer, root)
		return p
	case session.QuestionFailed:
		p.Run = own(p.Run)
		p.Error = session.RelativeText(p.Error, root)
		return p
	case session.RevisionArrived:
		p.Run = own(p.Run)
		p.Revision = session.RelativeRevision(p.Revision, root)
		return p
	case session.RefineFailed:
		p.Run = own(p.Run)
		return p
	}
	return in.p
}

// ---- building agent requests (on the client's goroutine) ----

func (e *Engine) briefingRequest() BriefingRequest {
	req := BriefingRequest{PR: e.st.PR, MaxBudgetUSD: e.cfg.Budgets.Briefing}
	for _, p := range e.st.FileGitOrder {
		fd := e.st.Files[p]
		status := "modified"
		switch fd.Status {
		case session.FileAdded:
			status = "added"
		case session.FileDeleted:
			status = "deleted"
		}
		req.Files = append(req.Files, FileSummary{Path: p, Status: status, Added: fd.Added, Removed: fd.Removed})
	}
	for _, p := range e.st.FileGitOrder {
		req.Methods = append(req.Methods, e.st.MethodsByFile[p]...)
	}
	return req
}

// questionRequest is the Thread's latest Question. fork: it forks from the
// Briefing (the Thread has no agent session of its own yet).
func (e *Engine) questionRequest(th *session.Thread) (QuestionRequest, bool) {
	q := ""
	for i := len(th.Entries) - 1; i >= 0; i-- {
		if th.Entries[i].Role == session.RoleQuestion {
			q = th.Entries[i].Text
			break
		}
	}
	req := QuestionRequest{
		ThreadID: th.ID, Kind: th.PendingKind(), ThreadKind: th.Kind, Question: q,
		Anchor: th.Anchor, Head: e.st.PR.HeadSHA,
	}
	if lq := th.LatestQuestion(); lq != nil {
		req.Reask = lq.Reask
	}
	req.AnchorCode, req.AnchorContext = e.anchorText(th.Anchor)
	fork := th.AgentSession == ""
	if fork {
		req.Parent = e.st.BriefingRun
	} else {
		req.Parent = session.RunInfo{SessionID: th.AgentSession, Total: th.AgentTotal}
	}
	req.MaxBudgetUSD = req.Parent.Total + e.cfg.Budgets.Question
	return req, fork
}

func (e *Engine) refineRequest(f *session.Feedback) RefineRequest {
	req := RefineRequest{
		FeedbackID: f.ID, Body: f.RefineBase, Answers: f.RefineAnswers, Round: f.RefineRound,
		Anchor: f.Anchor, Research: e.research(f.Anchor),
	}
	req.AnchorCode, req.AnchorContext = e.anchorText(f.Anchor)
	if f.RefineAnswers != "" && f.AgentSession != "" {
		req.Resume = true
		req.Parent = session.RunInfo{SessionID: f.AgentSession, Total: f.AgentTotal}
	} else {
		req.Parent = e.st.BriefingRun
	}
	req.MaxBudgetUSD = req.Parent.Total + e.cfg.Budgets.Question
	return req
}

// anchorText is a diff Anchor's code (numbered lines), or a description of
// any other Anchor.
func (e *Engine) anchorText(a session.Anchor) (code, context string) {
	switch a.Type {
	case session.AnchorDiff:
		fd := e.st.Files[a.Path]
		if fd == nil {
			return "", ""
		}
		var b strings.Builder
		for i := a.RowStart; i <= a.RowEnd && i < len(fd.All); i++ {
			if i < 0 {
				continue
			}
			l := fd.All[i]
			switch {
			case l.Kind == session.LineRemoved:
				fmt.Fprintf(&b, "-%d (base only): %s\n", l.OldNo, l.Text)
			default:
				fmt.Fprintf(&b, "%d: %s\n", l.NewNo, l.Text)
			}
		}
		return b.String(), ""
	case session.AnchorFile:
		return "", "the whole file " + a.Path
	case session.AnchorDescription:
		return "", "the PR description"
	case session.AnchorBriefing:
		return "", "the Briefing (" + a.Ref + ")"
	case session.AnchorPR:
		return "", "the Pull Request as a whole"
	case session.AnchorComment:
		if c := e.st.CommentByID(a.Ref); c != nil {
			where := "on the PR"
			if c.Path != "" {
				line, _, _ := e.st.CommentLine(c)
				where = fmt.Sprintf("on %s:%d", c.Path, line)
			}
			return "", fmt.Sprintf("a PR Comment by @%s %s", c.Author, where)
		}
		return "", "a PR Comment"
	case session.AnchorAnswer:
		t := e.st.ThreadByID(a.ThreadID)
		if t == nil || a.Entry < 0 || a.Entry >= len(t.Entries) {
			return "", "an earlier Answer"
		}
		ent := t.Entries[a.Entry]
		ctx := fmt.Sprintf("%s's %s Answer", t.ID, ent.Kind)
		if ent.A != nil {
			ctx += ": " + ent.A.Gist
		}
		return "", ctx
	}
	return "", ""
}

// research is one line per answered Thread whose Anchor overlaps a.
func (e *Engine) research(a session.Anchor) []string {
	var out []string
	for _, t := range e.st.Threads {
		i := t.LatestAnswerIdx()
		if i < 0 || !overlaps(t.Anchor, a) {
			continue
		}
		out = append(out, fmt.Sprintf("%s %s: %s", t.ID, t.Kind, t.Entries[i].A.Gist))
	}
	return out
}

func overlaps(x, y session.Anchor) bool {
	if x.Type != y.Type {
		return false
	}
	switch x.Type {
	case session.AnchorDiff:
		return x.Path == y.Path && x.RowStart <= y.RowEnd && y.RowStart <= x.RowEnd
	case session.AnchorFile:
		return x.Path == y.Path
	case session.AnchorAnswer:
		return x.ThreadID == y.ThreadID
	case session.AnchorBriefing, session.AnchorComment:
		return x.Ref == y.Ref
	}
	return true
}

// ---- MemStore ----

// MemStore keeps events in memory (tests, and sessions with no log).
type MemStore struct{ evs []session.Event }

func (m *MemStore) Load() ([]session.Event, error) {
	return append([]session.Event(nil), m.evs...), nil
}
func (m *MemStore) Append(evs ...session.Event) error {
	m.evs = append(m.evs, evs...)
	return nil
}
func (m *MemStore) Poll() ([]session.Event, error) { return nil, nil }

// Events is everything appended, for tests.
func (m *MemStore) Events() []session.Event { return append([]session.Event(nil), m.evs...) }

// IsBriefing reports whether the run is the Briefing (the TUI keeps the
// viewport steady when it lands: the navigator re-sorts).
func (in Input) IsBriefing() bool { return in.key == "briefing" }

// Writer is this process's writer ID in event IDs.
func (e *Engine) Writer() string { return e.writer }

// Record logs and applies events that come from outside the Review
// Session's own commands (the PR finished, say).
func (e *Engine) Record(ps ...session.Payload) error {
	_, err := e.commit(ps...)
	return err
}
