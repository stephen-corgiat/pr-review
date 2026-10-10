// Package replayagent records a live agent's results to a directory of JSON
// files, and replays them: a deterministic, free agent for the README demo
// (see demo/). Each call is keyed by what the Reviewer asked (the Question,
// the code it's anchored to, the Feedback body), not by session ids, so a
// replay doesn't care about run ids or budgets.
package replayagent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/engine"
)

// Agent wraps Inner: with Record it calls Inner for each call it hasn't
// recorded yet and writes the result to Dir; without, it never calls Inner (which may be nil) and serves what
// Record wrote, after Delay, so the "working…" state is visible.
type Agent struct {
	Inner  engine.Agent
	Dir    string
	Record bool
	Delay  time.Duration
}

// Parse reads the PR_REVIEW_REPLAY value: "record:DIR" or "replay:DIR".
func Parse(v string) (dir string, record, ok bool) {
	mode, dir, found := strings.Cut(v, ":")
	if !found || dir == "" || (mode != "record" && mode != "replay") {
		return "", false, false
	}
	return dir, mode == "record", true
}

type entry struct {
	Request any
	Result  json.RawMessage
}

func (a *Agent) path(kind string, req any) (string, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return filepath.Join(a.Dir, kind+"-"+hex.EncodeToString(sum[:5])+".json"), nil
}

// do serves one call: from the recording, or (when recording) from Inner.
func do[R any](ctx context.Context, a *Agent, kind string, req any, live func() (R, error)) (R, error) {
	var zero R
	file, err := a.path(kind, req)
	if err != nil {
		return zero, err
	}
	// Recording only fills in what's missing: delete a file to re-record it.
	_, statErr := os.Stat(file)
	if a.Record && statErr != nil {
		res, err := live()
		if err != nil {
			return res, err
		}
		raw, err := json.Marshal(res)
		if err != nil {
			return res, err
		}
		if err := os.MkdirAll(a.Dir, 0o755); err != nil {
			return res, err
		}
		out, _ := json.MarshalIndent(entry{Request: req, Result: raw}, "", "  ")
		return res, os.WriteFile(file, append(out, '\n'), 0o644)
	}
	delay := a.Delay
	if a.Record {
		delay = 0
	}
	select {
	case <-time.After(delay):
	case <-ctx.Done():
		return zero, ctx.Err()
	}
	b, err := os.ReadFile(file)
	if err != nil {
		return zero, fmt.Errorf("replay: no recording for this %s (record it with PR_REVIEW_REPLAY=record:%s): %w", kind, a.Dir, err)
	}
	var e entry
	if err := json.Unmarshal(b, &e); err != nil {
		return zero, fmt.Errorf("replay: %s: %w", file, err)
	}
	var res R
	if err := json.Unmarshal(e.Result, &res); err != nil {
		return zero, fmt.Errorf("replay: %s: %w", file, err)
	}
	return res, nil
}

func (a *Agent) Briefing(ctx context.Context, req engine.BriefingRequest) (engine.BriefingResult, error) {
	key := struct {
		Number int
		Head   string
	}{req.PR.Number, req.PR.HeadSHA}
	return do(ctx, a, "briefing", key, func() (engine.BriefingResult, error) { return a.Inner.Briefing(ctx, req) })
}

type questionKey struct {
	Kind                      string
	Question                  string
	Path                      string
	AnchorCode, AnchorContext string
	Reask                     bool
}

func qkey(req engine.QuestionRequest) questionKey {
	return questionKey{Kind: req.Kind.String(), Question: req.Question, Path: req.Anchor.Path,
		AnchorCode: req.AnchorCode, AnchorContext: req.AnchorContext, Reask: req.Reask}
}

func (a *Agent) Ask(ctx context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	return do(ctx, a, "ask", qkey(req), func() (engine.AnswerResult, error) { return a.Inner.Ask(ctx, req) })
}

func (a *Agent) Continue(ctx context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	return do(ctx, a, "continue", qkey(req), func() (engine.AnswerResult, error) { return a.Inner.Continue(ctx, req) })
}

func (a *Agent) Refine(ctx context.Context, req engine.RefineRequest) (engine.RevisionResult, error) {
	key := struct {
		Body, Answers, AnchorCode, AnchorContext string
		Round                                    int
	}{req.Body, req.Answers, req.AnchorCode, req.AnchorContext, req.Round}
	return do(ctx, a, "refine", key, func() (engine.RevisionResult, error) { return a.Inner.Refine(ctx, req) })
}
