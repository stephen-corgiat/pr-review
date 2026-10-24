package ui

// The review loop: own-PR review types, posting, PR comments, Outcomes, new commits and "Since last review".

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/fixtureagent"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/paths"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

// calcPR is a real-PR Review Session on the scratch calc repo (#7, by
// ana), through app.PR and a fake Forge signed in as login.
type calcPR struct {
	m     *Model
	repo  *testrepo.Repo
	forge *forge.Fake
	opts  app.PR
}

func newCalcPR(t *testing.T, login string) *calcPR {
	t.Helper()
	return newCalcPRWith(t, login, fixtureagent.New(nil).NoDelay())
}

func newCalcPRWith(t *testing.T, login string, agent engine.Agent) *calcPR {
	t.Helper()
	r := testrepo.New(t)
	f := forge.NewFake(login, forge.PR{Number: 7, OwnerRepo: "acme/calc", Title: "Clamp results", Body: "Clamps every result at Max.",
		Author: "ana", HeadRef: "feature", BaseRef: "main", HeadSHA: r.Head, BaseSHA: r.Base, State: "open"})
	o := app.PR{Checkout: r.Dir, Number: 7, Forge: f, DataDir: t.TempDir(),
		Agent: func(paths.Session, session.Opened) (engine.Agent, error) { return agent, nil }}
	eng, _, _, err := o.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(eng.Close)
	return &calcPR{m: New(eng, false), repo: r, forge: f, opts: o}
}

// readyFeedback adds Feedback on a whole file and marks it ready.
func readyFeedback(t *testing.T, m *Model, path, body string) *session.Feedback {
	t.Helper()
	evs, err := m.eng.Do(engine.AddFeedback{Anchor: session.Anchor{Type: session.AnchorFile, Path: path, Text: path}, Body: body})
	if err != nil {
		t.Fatal(err)
	}
	id := evs[0].Data.(session.FeedbackCreated).FeedbackID
	f := m.sess.FeedbackByID(id)
	toggleReady(t, m, f)
	return f
}

// 48. On your own PR the send dialog defaults to Hand to agent, and
// Request changes and Approve are greyed out: the keys skip them and a
// click only explains.
func TestScenario48_OwnPRReviewTypes(t *testing.T) {
	t.Parallel()
	c := newCalcPR(t, "ana")
	m := c.m
	if !m.sess.ReviewerIsAuthor {
		t.Fatalf("ana opened #7: she's the Author")
	}
	readyFeedback(t, m, "calc/calc.go", "Name the clamp constant.")
	out := run(t, m, 140, 40, "P", 50*time.Millisecond)
	if m.overlay != OverlaySend || m.send.dest != destAgent {
		t.Fatalf("the Author's default is Hand to agent")
	}
	out = run(t, m, 140, 40, "left space tab", 50*time.Millisecond)
	if m.send.dest != destGitHub || m.send.focus != sendFocusEvent {
		t.Fatalf("← space picks GitHub review, tab reaches Review")
	}
	mustContain(t, out, "Your own PR: GitHub allows only Comment", "the note under the review types")
	run(t, m, 140, 40, "right space", 50*time.Millisecond)
	if m.send.event != 0 || m.send.eventHL != 0 {
		t.Fatalf("→ skips the greyed-out types (hl %d, event %d)", m.send.eventHL, m.send.event)
	}
	out = clickOn(t, m, 140, 40, out, "Approve", 1)
	if m.send.event != 0 {
		t.Fatalf("a click on Approve doesn't pick it")
	}
	mustContain(t, out, "GitHub allows only Comment", "the click explains")

	// Someone else's PR: all three types, GitHub review first.
	o := newCalcPR(t, "bo")
	readyFeedback(t, o.m, "calc/calc.go", "Name the clamp constant.")
	run(t, o.m, 140, 40, "P tab right right space", 50*time.Millisecond)
	if o.m.send.dest != destGitHub || o.m.send.event != 2 {
		t.Fatalf("a colleague's PR: Approve can be picked (dest %d event %d)", o.m.send.dest, o.m.send.event)
	}
}

// 49. Posting a review: the dialog says what goes where, Send posts one
// review through the Forge, and the items become posted.
func TestScenario49_PostReview(t *testing.T) {
	t.Parallel()
	c := newCalcPR(t, "bo")
	m := c.m
	f := readyFeedback(t, m, "calc/calc.go", "Name the clamp constant.")
	a := m.sess.AnchorOnLines("calc/calc.go", "new", 14, 14, "")
	evs, err := m.eng.Do(engine.AddFeedback{Anchor: a, Body: "1000 is arbitrary."})
	if err != nil {
		t.Fatal(err)
	}
	g := m.sess.FeedbackByID(evs[0].Data.(session.FeedbackCreated).FeedbackID)
	toggleReady(t, m, g)
	out := run(t, m, 140, 40, "P", 50*time.Millisecond)
	mustContain(t, out, "Submitted at once as one review · 1 line comment, 1 in the body", "the review's shape")
	out = run(t, m, 140, 40, "tab tab tab enter", 200*time.Millisecond)
	mustContain(t, out, "Posted F1, F2 to GitHub as a “Comment” review", "the result")
	mustContain(t, out, "pullrequestreview-1", "the review's URL")
	if got := c.forge.Posted(); len(got) != 1 || len(got[0].Comments) != 1 || got[0].Comments[0].Line != 14 {
		t.Fatalf("the Forge got %+v", got)
	}
	if f.Status != session.FeedbackPosted || g.Status != session.FeedbackPosted {
		t.Fatalf("posted: %s %s", f.Status, g.Status)
	}
}

// calcComments are PR Comments on the calc PR: a conversation on line 14,
// an outdated one, and a general one.
func calcComments(head string) []forge.Comment {
	return []forge.Comment{
		{ID: "rc1", Kind: "review", Author: "carol", Body: "Why is the limit 1000?", CreatedAt: "2026-09-01T10:00:00Z", Path: "calc/calc.go", Line: 14, Side: "RIGHT", CommitID: head, OriginalCommit: head, OriginalLine: 14, URL: "https://github.com/acme/calc/pull/7#discussion_r1"},
		{ID: "rc2", Kind: "review", Author: "ana", Body: "It's the product limit.", CreatedAt: "2026-09-01T11:00:00Z", Path: "calc/calc.go", Line: 14, Side: "RIGHT", CommitID: head, InReplyTo: "rc1"},
		{ID: "rc3", Kind: "review", Author: "dan", Body: "An old nit.", CreatedAt: "2026-08-30T09:00:00Z", Path: "calc/calc.go", OriginalLine: 3, Side: "RIGHT", CommitID: "0ld", OriginalCommit: "0ld"},
		{ID: "ic1", Kind: "issue", Author: "ci-bot", Body: "Coverage is 91%.", CreatedAt: "2026-09-02T08:00:00Z"},
	}
}

// 50. PR Comments: read-only cards at their lines, replies inside, the
// outdated one at the file's top, the general one at the top of the
// Description; a comment can be an Anchor.
func TestScenario50_PRComments(t *testing.T) {
	t.Parallel()
	c := newCalcPR(t, "bo")
	c.forge.Comment = calcComments(c.repo.Head)
	m := c.m
	run(t, m, 140, 45, "", 100*time.Millisecond) // Init fetches them
	if len(m.sess.Comments) != 4 {
		t.Fatalf("comments: %d", len(m.sess.Comments))
	}
	m.openFile("calc/calc.go")
	m.focus = FocusCentre
	m.dirty = true
	out := run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "@dan  review comment · 30 Aug 2026 · outdated", "the outdated one at the top")
	i := rowWhere(t, m, "carol's card", func(r Row) bool { return r.Comment != nil && r.Comment.ID == "rc1" })
	if prev := m.centreRows[i-1]; prev.DiffRow < 0 || m.sess.Files["calc/calc.go"].All[prev.DiffRow].NewNo != 14 {
		t.Fatalf("carol's card should follow line 14")
	}
	cursorOn(m, i)
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "+1 reply", "the collapsed card counts replies")
	out = run(t, m, 140, 45, "enter", 50*time.Millisecond)
	mustContain(t, out, "read-only", "expanded")
	mustContain(t, out, "It's the product limit.", "the reply inside")
	// Ask about the comment: the cursor on its text, then d.
	cursorOn(m, rowWhere(t, m, "carol's text", func(r Row) bool { return r.Comment != nil && strings.Contains(r.text(), "Why is the limit") }))
	run(t, m, 140, 45, "d", 50*time.Millisecond)
	th := m.sess.Threads[len(m.sess.Threads)-1]
	if th.Anchor.Type != session.AnchorComment || th.Anchor.Ref != "rc1" {
		t.Fatalf("the Thread's Anchor: %+v", th.Anchor)
	}
	out = run(t, m, 140, 45, "1", 50*time.Millisecond)
	mustContain(t, out, "💬 @carol", "the Threads list names the comment")
	// The general comment heads the Description.
	m.centreView, m.centreCursor, m.centreScroll = ViewDescription, 0, 0
	m.dirty = true
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "PR Comments (1)", "the Description's heading")
	mustContain(t, out, "@ci-bot", "the general comment")
}

// 51. The loop from the Author's side: Hand to agent queues; `pr-review
// export` and `pr-review resolve` (other processes) land in the running
// TUI; the Outcome shows on the card and in the list; D marks it done and
// O reopens it, whatever the Outcome said.
func TestScenario51_OutcomesDoneReopen(t *testing.T) {
	t.Parallel()
	c := newCalcPR(t, "ana")
	m := c.m
	f := readyFeedback(t, m, "calc/calc.go", "Name the clamp constant.")
	out := run(t, m, 140, 40, "P tab enter", 100*time.Millisecond)
	if f.Status != session.FeedbackQueued {
		t.Fatalf("F1 is %s", f.Status)
	}
	mustContain(t, out, "Paste /pull-review (copied) into your agent session in a checkout of acme/calc on feature", "where to run it")
	run(t, m, 140, 40, "esc", 50*time.Millisecond)

	// The Author's checkout, where /pull-review runs export and resolve.
	testrepo.Git(t, c.repo.Dir, "remote", "add", "origin", "https://github.com/acme/calc.git")
	testrepo.Git(t, c.repo.Dir, "checkout", "-q", "feature")
	if _, err := (app.Export{Dir: c.repo.Dir, Forge: c.forge, DataDir: c.opts.DataDir}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	out = run(t, m, 140, 40, "", 2300*time.Millisecond) // the TUI polls the log every 2 s
	mustContain(t, out, "The Implementing Agent pulled in F1", "the flash")
	if f.Status != session.FeedbackHandedOff {
		t.Fatalf("F1 is %s", f.Status)
	}
	if _, err := (app.Resolve{Dir: c.repo.Dir, Forge: c.forge, DataDir: c.opts.DataDir,
		RecordOutcome: engine.RecordOutcome{ID: "F1", Outcome: "declined", Note: "Max is the product's word"}}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	out = run(t, m, 140, 40, "2", 2300*time.Millisecond)
	mustContain(t, out, "F1: ✗ declined · Max is the product's word", "the flash")
	mustContain(t, out, "F1  calc.go ✗ declined", "the list row")

	// The card: collapsed and expanded.
	m.openFeedback(f)
	m.focus = FocusCentre
	cursorOn(m, rowWhere(t, m, "F1's card", func(r Row) bool { return r.Feedback == f }))
	out = run(t, m, 140, 40, "", 50*time.Millisecond)
	mustContain(t, out, "Outcome  ✗ declined · Max is the product's word", "the Outcome row")
	mustContain(t, out, "D done", "the hint")
	out = run(t, m, 140, 40, "D", 50*time.Millisecond)
	if f.Status != session.FeedbackDone {
		t.Fatalf("D: F1 is %s", f.Status)
	}
	mustContain(t, out, "O reopen", "done offers reopen")
	run(t, m, 140, 40, "O", 50*time.Millisecond)
	if f.Status != session.FeedbackDraft {
		t.Fatalf("O: F1 is %s", f.Status)
	}
	out = run(t, m, 140, 40, "e", 50*time.Millisecond)
	if m.fbEdit == nil {
		t.Fatalf("a reopened item can be edited")
	}
	run(t, m, 140, 40, "esc", 50*time.Millisecond)
}

// push adds head B to the PR (the Author pushed) and returns it.
func (c *calcPR) push(t *testing.T) string {
	t.Helper()
	b := c.repo.Push(t)
	c.forge.SetHead(7, b)
	return b
}

// 52. The banner: a new head shows "New commits (N) · U to update", with
// a hint for local commits that aren't pushed; nothing updates by itself;
// U updates, and the banner keeps only the unpushed hint.
func TestScenario52_NewCommitsBanner(t *testing.T) {
	t.Parallel()
	c := newCalcPR(t, "ana")
	m := c.m
	a := m.sess.PR.HeadSHA
	out := run(t, m, 140, 40, "", 300*time.Millisecond)
	mustNotContain(t, out, "New commits", "no banner at the PR's head")
	c2 := newCalcPR(t, "ana")
	b := c2.push(t)
	c2.repo.Unpushed(t, "feature", 2)
	m = c2.m
	out = run(t, m, 140, 40, "", 300*time.Millisecond) // it checks when it starts
	mustContain(t, out, "↑ New commits (1)  U to update", "the banner")
	mustContain(t, out, "2 local commits not pushed", "the unpushed hint")
	if y := strings.Index(ansi.Strip(out), "New commits"); y < 0 || strings.Count(ansi.Strip(out)[:y], "\n") != 1 {
		t.Fatalf("the banner is the row under the header")
	}
	run(t, m, 140, 40, "j j", 500*time.Millisecond)
	if m.sess.PR.HeadSHA == b {
		t.Fatalf("nothing updates by itself")
	}
	out = run(t, m, 140, 40, "U", 1500*time.Millisecond)
	if m.sess.PR.HeadSHA != b {
		t.Fatalf("U updates to B, at %s", m.sess.PR.HeadSHA)
	}
	mustContain(t, out, "Updated to "+b[:7], "the flash")
	mustNotContain(t, out, "New commits", "no new commits now")
	mustContain(t, out, "2 local commits not pushed · only pushed commits can be reviewed", "the hint stays")
	_ = a
	out = run(t, m, 140, 40, "U", 50*time.Millisecond)
	mustContain(t, out, "No new commits", "U at the head")
}

// 53. After Update: Stale Anchors, Answers and Feedback show as Stale
// with what they were on; the Briefing is marked Stale until the new one
// lands; Q asks a Stale Question again at the new head.
func TestScenario53_StaleDisplay(t *testing.T) {
	t.Parallel()
	ag := fixtureagent.New(nil).NoDelay()
	c := newCalcPRWith(t, "ana", ag)
	m := c.m
	a := m.sess.PR.HeadSHA
	run(t, m, 140, 45, "", 100*time.Millisecond)
	ask(t, m, m.sess.AnchorOnLines("calc/calc.go", "new", 8, 8, ""), session.Define)
	ask(t, m, m.sess.AnchorOnLines("calc/calc.go", "new", 4, 5, ""), session.Define)
	m.eng.Do(engine.AddFeedback{Anchor: m.sess.AnchorOnLines("calc/calc.go", "new", 8, 9, ""), Body: "Say Min too."})
	m.eng.Do(engine.AddFeedback{Anchor: m.sess.AnchorOnLines("calc/calc_test.go", "new", 5, 7, ""), Body: "Test the clamp."})
	run(t, m, 140, 45, "", 300*time.Millisecond)
	b := c.push(t)
	ag.BriefingDelay = 2 * time.Second                // the new Briefing takes a while
	run(t, m, 140, 45, "focus", 300*time.Millisecond) // regaining focus checks the head
	m.focus = FocusNav
	out := run(t, m, 140, 45, "U", 700*time.Millisecond)
	if m.sess.PR.HeadSHA != b {
		t.Fatalf("not updated")
	}
	mustContain(t, out, "Stale (from "+a[:7]+")", "the Briefing's title while the new one runs")
	mustContain(t, out, "⚠ Stale: written at "+a[:7], "the Briefing's note")
	out = run(t, m, 140, 45, "2", 50*time.Millisecond)
	mustContain(t, out, "F1  calc.go:8 ⚠", "Stale Feedback in the list")
	mustContain(t, out, "calc_test.go (gone)", "Feedback on a deleted file")

	t1 := thread(t, m, "T1")
	m.openThread(t1)
	m.focus = FocusCentre
	cursorOn(m, rowWhere(t, m, "T1's card", func(r Row) bool { return r.Thread == t1 }))
	out = run(t, m, 140, 45, "", 50*time.Millisecond)
	mustContain(t, out, "⚠ Stale: its lines changed after "+a[:7]+". It was on “// Scale multiplies n by k.”", "the Stale Anchor's text")
	mustContain(t, out, "Q asks it again at "+b[:7], "the re-ask hint")
	if t2 := thread(t, m, "T2"); t2.Stale() || t2.Anchor.Stale {
		t.Fatalf("T2 (Add) didn't change")
	}
	out = run(t, m, 140, 45, "1", 50*time.Millisecond)
	mustContain(t, out, "T1  calc.go:8", "the list keeps T1 at its line")
	run(t, m, 140, 45, "", 2500*time.Millisecond) // the new Briefing lands
	if m.sess.Briefing.Stale {
		t.Fatalf("the new Briefing replaced the Stale one")
	}
	out = run(t, m, 140, 45, "Q", 400*time.Millisecond)
	if t1.Stale() || t1.Entries[len(t1.Entries)-2].Text[:22] != "Asked again at the new" {
		t.Fatalf("Q re-asks T1 at the new head: stale=%v", t1.Stale())
	}
	mustNotContain(t, out, "Q asks it again", "fresh again")
}

// 54. "Since last review": S shows the diff from the head you updated
// from; the navigator marks what changed; the Briefing says so in a line;
// enter on a line opens it in the file.
func TestScenario54_SinceLastReview(t *testing.T) {
	t.Parallel()
	c := newCalcPR(t, "ana")
	m := c.m
	out := run(t, m, 140, 45, "S", 100*time.Millisecond)
	mustContain(t, out, "comes after an Update", "S before any Update")
	a := m.sess.PR.HeadSHA
	b := c.push(t)
	out = run(t, m, 140, 45, "U", 300*time.Millisecond) // U checks when it hasn't seen a new head
	mustContain(t, out, "↑ New commits (1)", "the banner after U's check")
	out = run(t, m, 140, 45, "U", 1200*time.Millisecond)
	mustContain(t, out, "Since your last review ("+a[:7]+"): 1 commit · 3 files · +9 −10 · S shows the diff", "the Briefing's line")
	mustContain(t, out, "Since last review Δ3", "the navigator's entry")
	out = run(t, m, 140, 45, "S", 100*time.Millisecond)
	if m.centreView != ViewSince {
		t.Fatalf("S opens the view")
	}
	mustContain(t, out, a[:7]+" → "+b[:7], "the title")
	mustContain(t, out, "const Min = -1000", "an added line")
	mustContain(t, out, "calc/doc.go", "the added file")
	mustContain(t, out, "calc/calc_test.go", "the deleted file")
	mustContain(t, out, "(no longer in the PR)", "…marked")
	mustContain(t, out, "Δ", "files changed since are marked")
	cursorOn(m, rowWhere(t, m, "const Min", func(r Row) bool { return strings.Contains(r.text(), "const Min = -1000") }))
	run(t, m, 140, 45, "enter", 50*time.Millisecond)
	if m.centreView != ViewFile || m.filePath != "calc/calc.go" {
		t.Fatalf("enter opens the file")
	}
	if l := m.sess.Files["calc/calc.go"].All[m.cursorDiffRow()]; l.NewNo != 17 {
		t.Fatalf("at line %d, want 17", l.NewNo)
	}
	// clamp changed since; Add didn't.
	m.navExpanded["calc/calc.go"] = true
	m.dirty = true
	m.ensureRows()
	marks := map[string]bool{}
	for _, e := range m.navEntries {
		if e.Kind == "method" {
			marks[m.sess.Methods[e.Method].Name] = m.methodChangedSince(m.sess.Methods[e.Method])
		}
	}
	if !marks["clamp"] || marks["Add"] || !marks["Scale"] {
		t.Fatalf("methods changed since: %v", marks)
	}
}

// Frame fit at 100×30 and 200×50 with these views: the banner, PR
// Comments, Outcomes, Stale cards, the S view and the help.
func TestReviewFlowFrameFit(t *testing.T) {
	t.Parallel()
	for _, sz := range []struct{ w, h int }{{100, 30}, {200, 50}} {
		c := newCalcPR(t, "ana")
		c.forge.Comment = calcComments(c.repo.Head)
		m := c.m
		ask(t, m, m.sess.AnchorOnLines("calc/calc.go", "new", 8, 8, ""), session.Define)
		run(t, m, sz.w, sz.h, "", 300*time.Millisecond)
		c.push(t)
		c.repo.Unpushed(t, "feature", 1)
		run(t, m, sz.w, sz.h, "focus", 300*time.Millisecond)
		m.openFile("calc/calc.go")
		m.dirty = true
		run(t, m, sz.w, sz.h, "z", 50*time.Millisecond) // the banner, cards open
		run(t, m, sz.w, sz.h, "U", 1200*time.Millisecond)
		run(t, m, sz.w, sz.h, "z", 50*time.Millisecond) // Stale cards
		run(t, m, sz.w, sz.h, "S", 50*time.Millisecond)
		run(t, m, sz.w, sz.h, "ctrl+d ctrl+d", 50*time.Millisecond)
		run(t, m, sz.w, sz.h, "2 1 ?", 50*time.Millisecond)
	}
}
