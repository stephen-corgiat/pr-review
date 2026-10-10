package engine

// New heads: watching the PR's head (polling), Update (fetch, check out
// the new head in the same worktree, map every Anchor, mark what went
// Stale, regenerate the Briefing) and re-asking a Stale Question.

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// ---- polling ----

// How often the PR's head is checked while the Reviewer is active, and
// how long without input counts as idle (checks pause).
const (
	HeadEvery = 60 * time.Second
	IdleAfter = 10 * time.Minute
)

// watch decides when to check the PR's head: every HeadEvery while the
// Reviewer is active, pausing after IdleAfter without input. The first
// input after a pause, and regaining focus, check at once.
type watch struct {
	lastInput, lastCheck time.Time
}

func (w *watch) paused(now time.Time) bool { return now.Sub(w.lastInput) >= IdleAfter }

// canWatch: there's a Forge, and the PR is open and ours to update.
func (e *Engine) canWatch() bool {
	return e.forge != nil && !e.st.Fixture && !e.st.RepoMode && !e.st.ReadOnly() && e.st.PRState() == "open"
}

// HeadDue reports whether it's time to check the PR's head: the Reviewer
// is active and the last check was HeadEvery ago (or never).
func (e *Engine) HeadDue() bool {
	now := e.cfg.Now()
	return e.canWatch() && !e.watch.paused(now) && now.Sub(e.watch.lastCheck) >= HeadEvery
}

// HeadPaused reports whether checks are paused (the Reviewer is idle).
func (e *Engine) HeadPaused() bool { return e.watch.paused(e.cfg.Now()) }

// NoteInput records the Reviewer's input. It reports whether that ended a
// pause, so the head should be checked now.
func (e *Engine) NoteInput() bool {
	now := e.cfg.Now()
	was := e.watch.paused(now)
	e.watch.lastInput = now
	return was && e.canWatch()
}

// NoteFocus records the TUI regaining focus: check the head now.
func (e *Engine) NoteFocus() bool {
	e.watch.lastInput = e.cfg.Now()
	return e.canWatch()
}

// HeadCheck is what a check of the PR's head found.
type HeadCheck struct {
	Head     string // the PR's head on GitHub
	Commits  int    // commits from our head to it (0 when unknown)
	Unpushed int    // commits on the local branch that aren't on it
}

// HeadChecker starts a check of the PR's head: it returns a function, safe
// to run on any goroutine, that asks the Forge (`git ls-remote`) and
// counts commits with git; hand its result to SawHead. It's nil when
// there's nothing to watch.
func (e *Engine) HeadChecker() func(context.Context) (HeadCheck, error) {
	if !e.canWatch() {
		return nil
	}
	e.watch.lastCheck = e.cfg.Now()
	f, n, ours, checkout, branch := e.forge, e.st.PR.Number, e.st.PR.HeadSHA, e.st.Checkout, e.st.PR.HeadRef
	return func(ctx context.Context) (HeadCheck, error) {
		head, err := f.HeadSHA(ctx, n)
		if err != nil {
			return HeadCheck{}, err
		}
		c := HeadCheck{Head: head}
		if checkout == "" {
			return c, nil
		}
		if head != ours {
			if !hasCommit(ctx, checkout, head) {
				_ = fetchPRHead(ctx, checkout, n)
			}
			if hasCommit(ctx, checkout, head) {
				c.Commits = countCommits(ctx, checkout, ours+".."+head)
			}
		}
		c.Unpushed = unpushed(ctx, checkout, branch, head)
		return c, nil
	}
}

// SawHead records a check's result (not logged: it's GitHub's state, and
// the next check finds it again). It reports whether anything changed.
func (e *Engine) SawHead(c HeadCheck) bool {
	st := e.st
	changed := st.RemoteHead != c.Head || st.RemoteCommits != c.Commits || st.Unpushed != c.Unpushed
	st.RemoteHead, st.RemoteCommits, st.Unpushed = c.Head, c.Commits, c.Unpushed
	return changed
}

func hasCommit(ctx context.Context, dir, sha string) bool {
	if sha == "" {
		return false
	}
	_, err := gitsrc.Run(ctx, dir, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// fetchPRHead fetches the PR's head into the checkout's object store (no
// refs, no FETCH_HEAD).
func fetchPRHead(ctx context.Context, dir string, n int) error {
	_, err := gitsrc.Run(ctx, dir, "fetch", "--no-write-fetch-head", "-q", "origin", fmt.Sprintf("refs/pull/%d/head", n))
	return err
}

func countCommits(ctx context.Context, dir, rng string) int {
	out, err := gitsrc.Run(ctx, dir, "rev-list", "--count", rng)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(out)
	return n
}

// unpushed counts the local branch's commits that aren't on the PR's head
// (or, when that commit isn't here, its remote-tracking branch).
func unpushed(ctx context.Context, dir, branch, head string) int {
	if branch == "" {
		return 0
	}
	local := "refs/heads/" + branch
	if _, err := gitsrc.Run(ctx, dir, "rev-parse", "--verify", "-q", local); err != nil {
		return 0
	}
	switch {
	case hasCommit(ctx, dir, head):
		return countCommits(ctx, dir, head+".."+local)
	default:
		remote := "refs/remotes/origin/" + branch
		if _, err := gitsrc.Run(ctx, dir, "rev-parse", "--verify", "-q", remote); err != nil {
			return 0
		}
		return countCommits(ctx, dir, remote+".."+local)
	}
}

// ---- Update ----

// UpdatePrep is an Update that has fetched and checked out the new head
// and computed what Apply needs; nothing in the Review Session changed
// yet.
type UpdatePrep struct {
	From, To         string
	BaseFrom, BaseTo string
	Commits          int
	PR               forge.PR
	diff             *session.Session
	newSide, oldSide map[string]session.LineMap // From..To, BaseFrom..BaseTo (nil: same base)
	newStatus        map[string]string
	since            map[string]*session.FileDiff
	sinceOrder       []string
}

// ErrNoNewHead: the PR's head is the Review Session's.
var ErrNoNewHead = errors.New("no new commits: the Review Session is at the PR's head")

// updateInputs is what an Update reads from the state, captured on the
// client's goroutine.
type updateInputs struct {
	forge                    forge.Forge
	number                   int
	worktree, checkout       string
	from, baseFrom, remoteTo string
}

// BeginUpdate starts an Update: it refuses while agent runs are running
// (they read the worktree it's about to move), and holds queued ones back
// until ApplyUpdate or AbortUpdate. It returns the function that does the
// slow part (fetch, checkout, diff), safe to run on any goroutine.
func (e *Engine) BeginUpdate() (func(context.Context) (*UpdatePrep, error), error) {
	switch {
	case e.forge == nil || e.st.Fixture || e.st.RepoMode:
		return nil, errors.New("no GitHub here: Update needs a real PR")
	case e.st.ReadOnly():
		return nil, ErrReadOnly
	case e.updating:
		return nil, errors.New("already updating")
	case e.st.WorktreePath == "" || e.st.Checkout == "":
		return nil, errors.New("this Review Session has no worktree to update")
	}
	if n := len(e.active) + boolInt(e.briefingRunning); n > 0 {
		return nil, fmt.Errorf("%d agent run%s still running in the worktree: U works once %s", n, plural(n), map[bool]string{true: "it finishes", false: "they finish"}[n == 1])
	}
	e.updating = true
	e.cancelCallers() // it reads the worktree the Update moves
	in := updateInputs{forge: e.forge, number: e.st.PR.Number, worktree: e.st.WorktreePath, checkout: e.st.Checkout,
		from: e.st.PR.HeadSHA, baseFrom: e.st.PR.BaseSHA, remoteTo: e.st.RemoteHead}
	return func(ctx context.Context) (*UpdatePrep, error) { return prepareUpdate(ctx, in) }, nil
}

// AbortUpdate ends an Update that failed: queued work may start again.
func (e *Engine) AbortUpdate() []session.Event {
	e.updating = false
	var evs []session.Event
	if e.st.CallersState == session.CallersRunning && e.callers == nil {
		evs = e.startCallers() // the Update cancelled it: the head is still ours
	}
	return append(evs, e.dispatch()...)
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func prepareUpdate(ctx context.Context, in updateInputs) (*UpdatePrep, error) {
	pr, err := in.forge.PR(ctx, in.number)
	if err != nil {
		return nil, err
	}
	to := pr.HeadSHA
	if to == "" {
		to = in.remoteTo
	}
	if to == "" || to == in.from {
		return nil, ErrNoNewHead
	}
	for _, sha := range []string{to, pr.BaseSHA} {
		if !hasCommit(ctx, in.checkout, sha) {
			if err := fetchPRHead(ctx, in.checkout, in.number); err != nil {
				return nil, fmt.Errorf("fetching the new head: %w", err)
			}
			if sha == pr.BaseSHA && !hasCommit(ctx, in.checkout, sha) {
				_, _ = gitsrc.Run(ctx, in.checkout, "fetch", "--no-write-fetch-head", "-q", "origin", sha)
			}
		}
	}
	if !hasCommit(ctx, in.checkout, to) {
		return nil, fmt.Errorf("the new head %s isn't available", short(to))
	}
	baseTo, err := gitsrc.Run(ctx, in.checkout, "merge-base", pr.BaseSHA, to)
	if err != nil {
		return nil, err
	}
	// The same worktree path: the agent's sessions are keyed by it.
	if _, err := gitsrc.Run(ctx, in.worktree, "checkout", "--detach", "--force", "-q", to); err != nil {
		return nil, fmt.Errorf("checking out %s in the worktree: %w", short(to), err)
	}
	p, err := diffUpdate(ctx, in, pr, to, baseTo)
	if err != nil {
		// Put the worktree back where the Review Session still is.
		_, _ = gitsrc.Run(context.Background(), in.worktree, "checkout", "--detach", "--force", "-q", in.from)
		return nil, err
	}
	return p, nil
}

func diffUpdate(ctx context.Context, in updateInputs, pr forge.PR, to, baseTo string) (*UpdatePrep, error) {
	p := &UpdatePrep{From: in.from, To: to, BaseFrom: in.baseFrom, BaseTo: baseTo, PR: pr,
		Commits: countCommits(ctx, in.worktree, in.from+".."+to)}
	var err error
	if p.diff, _, err = session.Build(gitsrc.OpenRepo(in.worktree, baseTo, to)); err != nil {
		return nil, err
	}
	hunks, status, err := gitsrc.OpenRepo(in.worktree, in.from, to).ZeroHunks()
	if err != nil {
		return nil, err
	}
	p.newSide, p.newStatus = lineMaps(hunks), status
	if baseTo != in.baseFrom {
		old, _, err := gitsrc.OpenRepo(in.worktree, in.baseFrom, baseTo).ZeroHunks()
		if err != nil {
			return nil, err
		}
		p.oldSide = lineMaps(old)
	}
	if p.since, p.sinceOrder, err = session.BuildSince(gitsrc.OpenRepo(in.worktree, in.from, to)); err != nil {
		return nil, err
	}
	return p, nil
}

func lineMaps(h map[string][]gitsrc.ZeroHunk) map[string]session.LineMap {
	out := map[string]session.LineMap{}
	for path, hs := range h {
		lm := make(session.LineMap, len(hs))
		for i, x := range hs {
			lm[i] = session.LineHunk{OldStart: x.OldStart, OldCount: x.OldCount, NewStart: x.NewStart, NewCount: x.NewCount}
		}
		out[path] = lm
	}
	return out
}

func short(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// UpdateSummary counts what an Update did, for the Reviewer.
type UpdateSummary struct {
	Moved, Stale, StaleAnswers int
}

// ApplyUpdate moves the Review Session to the prepared head: the new diff,
// every Anchor mapped, Answers marked Stale, the head move recorded, and
// the Briefing regenerated (the old one stays, Stale, until it lands).
func (e *Engine) ApplyUpdate(p *UpdatePrep) ([]session.Event, UpdateSummary, error) {
	defer func() { e.updating = false }()
	var sum UpdateSummary
	if p == nil || p.From != e.st.PR.HeadSHA {
		return nil, sum, errors.New("the Review Session moved while updating: try U again")
	}
	hm := session.HeadMoved{From: p.From, To: p.To, BaseFrom: p.BaseFrom, BaseTo: p.BaseTo, Commits: p.Commits,
		Title: p.PR.Title, Description: p.PR.Body, Files: p.PR.Files, Additions: p.PR.Additions, Deletions: p.PR.Deletions,
		Anchors: map[string]session.Anchor{}}
	for _, t := range e.st.Threads {
		a, moved := p.mapAnchor(t.Anchor, e.hasRemoved(t.Anchor))
		hm.Anchors[t.ID] = a
		sum.count(t.Anchor, a, moved)
	}
	for _, f := range e.st.Feedback {
		a, moved := p.mapAnchor(f.Anchor, e.hasRemoved(f.Anchor))
		hm.Anchors[f.ID] = a
		sum.count(f.Anchor, a, moved)
	}
	hm.StaleAnswers = p.staleAnswers(e.st, hm.Anchors)
	sum.StaleAnswers = len(hm.StaleAnswers)

	e.st.ReplaceDiff(p.diff)
	e.st.SetSince(p.since, p.sinceOrder)
	evs, err := e.commit(hm)
	if err != nil {
		return evs, sum, err
	}
	e.updating = false
	evs = append(evs, e.startCallers()...)
	return append(evs, e.dispatch()...), sum, nil
}

func (s *UpdateSummary) count(before, after session.Anchor, moved bool) {
	switch {
	case after.Stale && !before.Stale:
		s.Stale++
	case moved:
		s.Moved++
	}
}

// Update runs a whole Update synchronously (tests, the CLI).
func (e *Engine) Update(ctx context.Context) ([]session.Event, UpdateSummary, error) {
	run, err := e.BeginUpdate()
	if err != nil {
		return nil, UpdateSummary{}, err
	}
	p, err := run(ctx)
	if err != nil {
		return e.AbortUpdate(), UpdateSummary{}, err
	}
	return e.ApplyUpdate(p)
}

// hasRemoved: a diff Anchor covers removed lines (not just context, whose
// base-side numbers it also carries).
func (e *Engine) hasRemoved(a session.Anchor) bool {
	fd := e.st.Files[a.Path]
	if a.Type != session.AnchorDiff || fd == nil {
		return false
	}
	for i := max(0, a.RowStart); i <= a.RowEnd && i < len(fd.All); i++ {
		if fd.All[i].Kind == session.LineRemoved {
			return true
		}
	}
	return false
}

// mapAnchor carries an Anchor to the new head. moved: its lines' numbers
// changed. Lines that were edited go Stale at a best-guess position,
// keeping the text they were anchored to; a file gone from the PR sends
// its Anchors Stale onto the whole PR. removed: the Anchor covers removed
// lines, so its base-side lines count.
func (p *UpdatePrep) mapAnchor(a session.Anchor, removed bool) (session.Anchor, bool) {
	switch a.Type {
	case session.AnchorDiff:
	case session.AnchorFile:
		if p.diff.Files[a.Path] == nil {
			return p.toWholePR(a), false
		}
		return a, false
	default:
		return a, false
	}
	if p.diff.Files[a.Path] == nil || p.newStatus[a.Path] == "deleted" {
		return p.toWholePR(a), false
	}
	b := a
	edited := false
	if a.NewStart > 0 {
		if lm, ok := p.newSide[a.Path]; ok {
			var e bool
			b.NewStart, b.NewEnd, e = lm.Range(a.NewStart, a.NewEnd)
			edited = edited || e
		}
	}
	if a.OldStart > 0 && p.oldSide != nil {
		if lm, ok := p.oldSide[a.Path]; ok {
			var e bool
			b.OldStart, b.OldEnd, e = lm.Range(a.OldStart, a.OldEnd)
			edited = edited || e
		}
	}
	os, oe := b.OldStart, b.OldEnd
	if b.NewStart > 0 && !removed {
		os, oe = 0, 0
	}
	rs, re, ok := p.diff.RowsForLines(a.Path, b.NewStart, b.NewEnd, os, oe)
	if !ok {
		// Its lines aren't in the new diff: the whole file, Stale.
		b.Type = session.AnchorFile
		edited = true
	}
	b.RowStart, b.RowEnd = rs, re
	if ok {
		b = p.diff.LocateAnchor(b)
	}
	if edited && !a.Stale {
		b.Stale, b.StaleFrom = true, p.From
	}
	moved := b.NewStart != a.NewStart || b.OldStart != a.OldStart || b.RowStart != a.RowStart
	return b, moved
}

func (p *UpdatePrep) toWholePR(a session.Anchor) session.Anchor {
	b := session.Anchor{Type: session.AnchorPR, Path: a.Path, Text: a.Text, Stale: true, StaleFrom: a.StaleFrom,
		NewStart: a.NewStart, NewEnd: a.NewEnd, OldStart: a.OldStart, OldEnd: a.OldEnd, RowStart: -1, RowEnd: -1}
	if b.Text == "" {
		b.Text = a.Path
	}
	if b.StaleFrom == "" {
		b.StaleFrom = p.From
	}
	return b
}

// staleAnswers lists the Threads with Answers that aren't all Stale yet
// whose Anchor went Stale (or whose parent Answer did), or whose
// Citations point at lines that changed.
func (p *UpdatePrep) staleAnswers(st *session.Session, anchors map[string]session.Anchor) []string {
	stale := map[string]bool{}
	var out []string
	for _, t := range st.Threads {
		fresh := false
		for _, en := range t.Entries {
			fresh = fresh || (en.Role == session.RoleAnswer && !en.Stale)
		}
		if !fresh {
			continue
		}
		a := anchors[t.ID]
		hit := a.Stale && !t.Anchor.Stale
		if a.Type == session.AnchorAnswer && stale[a.ThreadID] {
			hit = true
		}
		for _, en := range t.Entries {
			if hit || en.A == nil || en.Stale {
				continue
			}
			for _, c := range append(append([]session.Citation(nil), en.A.Citations...), en.A.Evidence...) {
				if p.citationChanged(c) {
					hit = true
					break
				}
			}
		}
		if hit {
			stale[t.ID] = true
			out = append(out, t.ID)
		}
	}
	return out
}

func (p *UpdatePrep) citationChanged(c session.Citation) bool {
	if c.URL != "" || c.Path == "" || strings.Contains(c.Path, ":") {
		return false // a URL, or a Related Codebase (ALIAS:path)
	}
	if p.newStatus[c.Path] == "deleted" {
		return true
	}
	lm, ok := p.newSide[c.Path]
	if !ok {
		return false
	}
	if c.Line == 0 {
		return true // the whole file, and it changed
	}
	return lm.Changed(c.Line, max(c.Line, c.EndLine))
}

// ---- re-asking ----

// Reask asks a Stale Question again at the new head, as a follow-up in
// the same Thread. Nothing is ever re-asked on its own.
type Reask struct{ ThreadID string }

func (c Reask) events(e *Engine) ([]session.Payload, error) {
	if err := e.needAgent(); err != nil {
		return nil, err
	}
	t, err := e.thread(c.ThreadID)
	if err != nil {
		return nil, err
	}
	if !t.Stale() {
		return nil, fmt.Errorf("%s isn't Stale: R asks a follow-up", t.ID)
	}
	// The Question to ask again: the latest one that wasn't itself a re-ask.
	var q *session.Entry
	for i := len(t.Entries) - 1; i >= 0; i-- {
		if en := &t.Entries[i]; en.Role == session.RoleQuestion && (q == nil || !en.Reask) {
			q = en
			if !en.Reask {
				break
			}
		}
	}
	text := q.Text
	if _, rest, ok := strings.Cut(text, ": "); ok && q.Reask {
		text = rest
	}
	return []session.Payload{session.FollowUpAsked{ThreadID: t.ID, Kind: q.Kind, Reask: true,
		Text: fmt.Sprintf("Asked again at the new head %s: %s", short(e.st.PR.HeadSHA), text)}}, nil
}
