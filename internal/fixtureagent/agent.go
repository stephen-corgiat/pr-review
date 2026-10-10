// Package fixtureagent is the canned Agent: recorded agent outputs
// (fixtures/*.json) matched to Questions by Kind and Anchor, for tests and
// offline demos. A Question with no canned Answer gets a
// placeholder that says so.
package fixtureagent

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// Agent answers from fixtures. Its agent session IDs carry everything it
// needs to continue a Thread ("fx:A04:0" = fixture A04, follow-up 0 used),
// so it keeps no state and a resumed Review Session carries on.
type Agent struct {
	fx *Fixtures
	// Delay is how long each Question or Refine takes (default: 2–5 s).
	Delay func() time.Duration
	// BriefingDelay is how long the Briefing takes (default: the
	// fixture's delay_ms).
	BriefingDelay time.Duration

	mu  sync.Mutex
	rng *rand.Rand
}

// New returns a fixture Agent. fx may be nil (-repo mode): every Answer
// is then the placeholder.
func New(fx *Fixtures) *Agent {
	if fx == nil {
		fx = &Fixtures{}
	}
	a := &Agent{fx: fx, rng: rand.New(rand.NewSource(time.Now().UnixNano()))}
	a.BriefingDelay = time.Duration(fx.BriefingRaw.DelayMS) * time.Millisecond
	a.Delay = func() time.Duration {
		a.mu.Lock()
		defer a.mu.Unlock()
		return time.Duration(2000+a.rng.Intn(3001)) * time.Millisecond
	}
	return a
}

// NoDelay makes every call return at once (engine tests).
func (a *Agent) NoDelay() *Agent {
	a.Delay = func() time.Duration { return 0 }
	a.BriefingDelay = 0
	return a
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (a *Agent) Briefing(ctx context.Context, req engine.BriefingRequest) (engine.BriefingResult, error) {
	if err := sleep(ctx, a.BriefingDelay); err != nil {
		return engine.BriefingResult{}, err
	}
	raw := a.fx.BriefingRaw
	n := session.BriefingNotes{TLDR: raw.TLDR, Methods: map[string]session.MethodNote{}}
	for _, r := range raw.ReadingOrder {
		n.ReadingOrder = append(n.ReadingOrder, session.ReadingStep{Path: r.Path, Summary: r.Summary})
	}
	for k, m := range raw.Methods {
		n.Methods[k] = session.MethodNote{Does: m.Does, Changed: m.Changed}
	}
	for _, c := range raw.ContractChanges {
		cc := session.ContractChange{Category: session.CategoryFromName(c.Category), Title: c.Title, Detail: c.Detail}
		for _, an := range c.Anchors {
			cc.Anchors = append(cc.Anchors, session.ContractChangeAnchor{Path: an.Path, Line: an.Line})
		}
		n.ContractChanges = append(n.ContractChanges, cc)
	}
	return engine.BriefingResult{Notes: n, Run: session.RunInfo{SessionID: "fx:briefing", Model: "fixture"}}, nil
}

func (a *Agent) Ask(ctx context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	if err := sleep(ctx, a.Delay()); err != nil {
		return engine.AnswerResult{}, err
	}
	fx := a.Match(req.ThreadKind, req.Anchor)
	if fx == nil {
		return engine.AnswerResult{Answer: a.FallbackAnswer(req.Kind), Run: session.RunInfo{SessionID: sessionID("", nil), Model: "fixture"}}, nil
	}
	ab := fx.Answer
	if req.Kind != req.ThreadKind {
		ab = a.FallbackAnswer(req.Kind)
	}
	return engine.AnswerResult{Answer: ab, Run: session.RunInfo{SessionID: sessionID(fx.ID, nil), Model: "fixture"}}, nil
}

// Continue gives a follow-up the first unused follow_ups[] entry of its
// Kind (a plain follow-up: of any Kind), else the placeholder.
func (a *Agent) Continue(ctx context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	if err := sleep(ctx, a.Delay()); err != nil {
		return engine.AnswerResult{}, err
	}
	id, used := parseSessionID(req.Parent.SessionID)
	ab := a.FallbackAnswer(req.Kind)
	if fx := a.byID(id); fx != nil {
		for i, fu := range fx.FollowUps {
			if used[i] || (req.Kind != req.ThreadKind && fu.Kind != req.Kind) {
				continue
			}
			used[i] = true
			ab = fu
			break
		}
	}
	return engine.AnswerResult{Answer: ab, Run: session.RunInfo{SessionID: sessionID(id, used), Model: "fixture"}}, nil
}

// Refine returns the canned Revision when there is one for this Feedback
// and body (first round only), else a placeholder that fills the notes.
func (a *Agent) Refine(ctx context.Context, req engine.RefineRequest) (engine.RevisionResult, error) {
	if err := sleep(ctx, a.Delay()); err != nil {
		return engine.RevisionResult{}, err
	}
	run := session.RunInfo{SessionID: "fx:refine:" + req.FeedbackID, Model: "fixture"}
	if req.Round == 1 && req.Answers == "" {
		for _, rf := range a.fx.Revisions {
			if rf.FeedbackID == req.FeedbackID && rf.ForBody == req.Body {
				return engine.RevisionResult{Revision: rf.Revision, Run: run}, nil
			}
		}
	}
	return engine.RevisionResult{Revision: PlaceholderRevision(req.Body, req.Answers), Run: run}, nil
}

// ---- session IDs ----

func sessionID(fixture string, used map[int]bool) string {
	var idx []string
	for i := 0; i < 64; i++ {
		if used[i] {
			idx = append(idx, strconv.Itoa(i))
		}
	}
	return "fx:" + fixture + ":" + strings.Join(idx, ",")
}

func parseSessionID(s string) (string, map[int]bool) {
	used := map[int]bool{}
	parts := strings.SplitN(strings.TrimPrefix(s, "fx:"), ":", 2)
	if len(parts) == 2 {
		for _, x := range strings.Split(parts[1], ",") {
			if n, err := strconv.Atoi(x); err == nil {
				used[n] = true
			}
		}
	}
	return parts[0], used
}

// ---- matching ----

func (a *Agent) byID(id string) *AnswerFixture {
	for i := range a.fx.Answers {
		if a.fx.Answers[i].ID == id {
			return &a.fx.Answers[i]
		}
	}
	return nil
}

// Match finds the first fixture (in file order) whose Kind matches and
// whose anchor list has any spec matching the Anchor.
func (a *Agent) Match(kind session.QuestionKind, anchor session.Anchor) *AnswerFixture {
	for i := range a.fx.Answers {
		fx := &a.fx.Answers[i]
		if fx.Kind != kind {
			continue
		}
		for _, spec := range fx.Anchors {
			if matchAnchorSpec(spec, anchor) {
				return fx
			}
		}
	}
	return nil
}

func overlap(a0, a1 int, r [2]int) bool {
	return a0 > 0 && a0 <= r[1] && r[0] <= a1
}

func matchAnchorSpec(spec AnchorSpec, a session.Anchor) bool {
	contains := func(hay, needle string) bool {
		if needle == "" {
			return true
		}
		return strings.Contains(strings.ToLower(hay), strings.ToLower(needle))
	}
	switch spec.Type {
	case "diff":
		if a.Type != session.AnchorDiff || a.Path != spec.Path {
			return false
		}
		hit := overlap(a.NewStart, a.NewEnd, spec.Lines)
		if spec.Side == "old" {
			hit = overlap(a.OldStart, a.OldEnd, spec.Lines)
		}
		return hit && contains(a.Text, spec.Text)
	case "file":
		return a.Path == spec.Path && a.Path != ""
	case "description":
		return a.Type == session.AnchorDescription && contains(a.Text, spec.Text)
	case "briefing":
		return a.Type == session.AnchorBriefing && a.Ref == spec.Ref && contains(a.Text, spec.Text)
	}
	return false
}

// FallbackAnswer is the Unverified Answer used when no fixture matches.
func (a *Agent) FallbackAnswer(kind session.QuestionKind) session.AnswerBody {
	var locs []string
	for _, fx := range a.fx.Answers {
		if len(fx.Anchors) == 0 {
			continue
		}
		locs = append(locs, shortLocation(fx.Anchors[0])+" "+fx.Kind.String())
	}
	ab := session.AnswerBody{
		Kind: kind,
		Gist: fmt.Sprintf("No canned Answer for this Anchor in the fixtures. The live agent would answer this %s Question here.", kind),
	}
	if len(locs) > 0 {
		ab.Detail = "Canned Answers exist for:\n\n- " + strings.Join(locs, "\n- ")
	}
	return ab
}

func shortLocation(spec AnchorSpec) string {
	switch spec.Type {
	case "diff":
		if spec.Lines[0] == spec.Lines[1] {
			return fmt.Sprintf("%s:%d", spec.Path, spec.Lines[0])
		}
		return fmt.Sprintf("%s:%d–%d", spec.Path, spec.Lines[0], spec.Lines[1])
	case "file":
		return spec.Path
	case "description":
		return "PR description"
	case "briefing":
		return "briefing:" + spec.Ref
	}
	return spec.Path
}

// PlaceholderRevision fills each Agent Note with "[filled: <note text>]".
func PlaceholderRevision(body, answers string) session.Revision {
	notes := session.AgentNotes(body)
	var b strings.Builder
	last := 0
	for _, n := range notes {
		b.WriteString(body[last:n.Start])
		b.WriteString("[filled: " + n.Text + "]")
		last = n.End
	}
	b.WriteString(body[last:])
	r := session.Revision{Text: b.String(), Placeholder: true}
	switch {
	case answers != "" && len(notes) > 0:
		r.WhatChanged = fmt.Sprintf("Placeholder Revision (no canned one): filled %d Agent Notes; the live agent would use your answers too.", len(notes))
	case answers != "":
		r.WhatChanged = "Placeholder Revision (no canned one): the live agent would reword this using your answers."
	case len(notes) > 0:
		r.WhatChanged = fmt.Sprintf("Placeholder Revision (no canned one): filled %d Agent Notes with [filled: …].", len(notes))
	default:
		r.WhatChanged = "Placeholder Revision (no canned one): no Agent Notes to fill, so nothing changed."
	}
	return r
}

// ---- seeds ----

func anchorFromSpec(s *session.Session, spec AnchorSpec) session.Anchor {
	switch spec.Type {
	case "diff":
		return s.AnchorOnLines(spec.Path, spec.Side, spec.Lines[0], spec.Lines[1], spec.Text)
	case "file":
		return session.Anchor{Type: session.AnchorFile, Path: spec.Path}
	case "description":
		return session.Anchor{Type: session.AnchorDescription, Text: spec.Text}
	case "briefing":
		return session.Anchor{Type: session.AnchorBriefing, Ref: spec.Ref, Text: spec.Text}
	}
	return session.Anchor{}
}

// Seed returns the events a new fixture-mode log starts with: the seeded
// Threads (Drafts and Answers) and Feedback.
func Seed(fx *Fixtures) func(*session.Session) []session.Payload {
	return func(s *session.Session) []session.Payload {
		var out []session.Payload
		n := 0
		for _, f := range fx.Answers {
			if f.Seed == "" || len(f.Anchors) == 0 {
				continue
			}
			n++
			id := fmt.Sprintf("T%d", n)
			out = append(out, session.ThreadCreated{ThreadID: id, Anchor: anchorFromSpec(s, f.Anchors[0]), Kind: f.Kind, Text: f.Question, Draft: f.Seed == "draft"})
			if f.Seed == "draft" {
				continue
			}
			// answered (read) and answered_unread; an Answer that ran
			// something is verified (✅), one that only read is 📖.
			ab := f.Answer
			out = append(out, session.QuestionStarted{ThreadID: id},
				session.AnswerArrived{ThreadID: id, Answer: ab, Read: f.Seed == "answered",
					Run: session.RunInfo{SessionID: sessionID(f.ID, nil), Model: "fixture"}})
		}
		for _, sd := range fx.FeedbackSeeds {
			status := session.FeedbackDraft
			switch sd.Status {
			case "ready":
				status = session.FeedbackReady
			case "posted":
				status = session.FeedbackPosted
			}
			out = append(out, session.FeedbackCreated{FeedbackID: sd.ID, Anchor: anchorFromSpec(s, sd.Anchor), Body: sd.Body, Status: status})
		}
		return out
	}
}
