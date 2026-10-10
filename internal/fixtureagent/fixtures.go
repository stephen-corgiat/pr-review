package fixtureagent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

// ---- JSON shapes, matching fixtures/*.json ----

type prJSON struct {
	RepoURL   string   `json:"repo_url"`
	OwnerRepo string   `json:"owner_repo"`
	GoModule  string   `json:"go_module"`
	Number    int      `json:"number"`
	URL       string   `json:"url"`
	Title     string   `json:"title"`
	Author    string   `json:"author"`
	HeadRef   string   `json:"head_ref"`
	BaseRef   string   `json:"base_ref"`
	MergedAt  string   `json:"merged_at"`
	BaseSHA   string   `json:"base_sha"`
	HeadSHA   string   `json:"head_sha"`
	Files     []string `json:"files"`
	Stats     struct {
		Files     int `json:"files"`
		Additions int `json:"additions"`
		Deletions int `json:"deletions"`
	} `json:"stats"`
	Description string `json:"description"`
}

type briefingReadingJSON struct {
	Path    string `json:"path"`
	Summary string `json:"summary"`
}

type briefingMethodJSON struct {
	Does    string `json:"does"`
	Changed string `json:"changed"`
}

type briefingAnchorJSON struct {
	Path string `json:"path"`
	Line int    `json:"line"`
}

type briefingContractJSON struct {
	Category string               `json:"category"`
	Title    string               `json:"title"`
	Detail   string               `json:"detail"`
	Anchors  []briefingAnchorJSON `json:"anchors"`
}

type briefingJSON struct {
	DelayMS         int                           `json:"delay_ms"`
	TLDR            string                        `json:"tldr"`
	ReadingOrder    []briefingReadingJSON         `json:"reading_order"`
	Methods         map[string]briefingMethodJSON `json:"methods"`
	ContractChanges []briefingContractJSON        `json:"contract_changes"`
}

type anchorSpecJSON struct {
	Type  string `json:"type"`
	Path  string `json:"path"`
	Side  string `json:"side"`
	Lines []int  `json:"lines"`
	Text  string `json:"text"`
	Ref   string `json:"ref"`
}

type ranJSON struct {
	Command string `json:"command"`
	Result  string `json:"result"`
}

type stepJSON struct {
	Label     string `json:"label"`
	At        string `json:"at"`
	Value     string `json:"value"`
	Lang      string `json:"lang"`
	Source    string `json:"source"`
	SourceRef string `json:"source_ref"`
}

// answerBodyJSON is the union schema for Answers,
// plus "verified".
type answerBodyJSON struct {
	Kind      string    `json:"kind"`
	Verified  bool      `json:"verified"`
	Gist      string    `json:"gist"`
	Detail    string    `json:"detail"`
	Citations []string  `json:"citations"`
	Ran       []ranJSON `json:"ran"`
	Define    *struct {
		Term    string `json:"term"`
		Example string `json:"example"`
	} `json:"define"`
	Summarize *struct {
		Changed string `json:"changed"`
	} `json:"summarize"`
	SampleData *struct {
		Steps []stepJSON `json:"steps"`
	} `json:"sample_data"`
	BeforeAfter *struct {
		Before stepJSON `json:"before"`
		After  stepJSON `json:"after"`
		Note   string   `json:"note"`
	} `json:"before_after"`
	Trace *struct {
		Hops []struct {
			At   string `json:"at"`
			Fn   string `json:"fn"`
			Text string `json:"text"`
		} `json:"hops"`
	} `json:"trace"`
	TestCases *struct {
		Cases []struct {
			Name   string `json:"name"`
			At     string `json:"at"`
			Text   string `json:"text"`
			Status string `json:"status"`
		} `json:"cases"`
	} `json:"test_cases"`
	Research *struct {
		Verdict  string   `json:"verdict"`
		Why      string   `json:"why"`
		Evidence []string `json:"evidence"`
	} `json:"research"`
	Revision *struct {
		RevisedText string   `json:"revised_text"`
		WhatChanged string   `json:"what_changed"`
		Questions   []string `json:"questions"`
	} `json:"revision"`
}

type answerFixtureJSON struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind"`
	Anchors   []anchorSpecJSON `json:"anchors"`
	Question  string           `json:"question"`
	Seed      *string          `json:"seed"`
	Answer    answerBodyJSON   `json:"answer"`
	FollowUps []answerBodyJSON `json:"follow_ups"`
}

type answersJSON struct {
	Answers []answerFixtureJSON `json:"answers"`
}

type feedbackSeedJSON struct {
	ID     string         `json:"id"`
	Anchor anchorSpecJSON `json:"anchor"`
	Status string         `json:"status"`
	Body   string         `json:"body"`
}

type feedbackFileJSON struct {
	Feedback []feedbackSeedJSON `json:"feedback"`
}

type revisionFixtureJSON struct {
	Feedback string         `json:"feedback"`
	ForBody  string         `json:"for_body"`
	Revision answerBodyJSON `json:"revision"`
}

type revisionsFileJSON struct {
	Revisions []revisionFixtureJSON `json:"revisions"`
}

// ---- Runtime fixture shapes ----

type AnchorSpec struct {
	Type  string // diff|file|description|briefing
	Path  string
	Side  string // new|old
	Lines [2]int
	Text  string
	Ref   string
}

type AnswerFixture struct {
	ID        string
	Kind      session.QuestionKind
	Anchors   []AnchorSpec
	Question  string
	Seed      string
	Answer    session.AnswerBody
	FollowUps []session.AnswerBody
}

type FeedbackSeed struct {
	ID     string
	Anchor AnchorSpec
	Status string
	Body   string
}

// RevisionFixture is a canned Revision for a seeded Feedback item. It only
// fits the body it was made from.
type RevisionFixture struct {
	FeedbackID string
	ForBody    string
	Revision   session.Revision
}

// Fixtures holds everything loaded from fixtures/*.json.
type Fixtures struct {
	PR            prJSON
	BriefingRaw   briefingJSON
	Answers       []AnswerFixture
	FeedbackSeeds []FeedbackSeed
	Revisions     []RevisionFixture
}

func toStep(s stepJSON) session.SampleStep {
	return session.SampleStep{Label: s.Label, At: s.At, Value: s.Value, Lang: s.Lang, Source: s.Source, SourceRef: s.SourceRef}
}

func toAnswerBody(b answerBodyJSON) session.AnswerBody {
	kind, _ := session.KindFromFixture(b.Kind)
	ab := session.AnswerBody{Kind: kind, Verified: b.Verified, Gist: b.Gist, Detail: b.Detail}
	for _, c := range b.Citations {
		ab.Citations = append(ab.Citations, session.ParseCitation(c))
	}
	for _, r := range b.Ran {
		ab.Ran = append(ab.Ran, session.Ran{Command: r.Command, Result: r.Result})
	}
	ab.Verified = ab.Verified || len(ab.Ran) > 0
	switch {
	case b.Define != nil:
		ab.Term, ab.Example, ab.HasKindData = b.Define.Term, b.Define.Example, true
	case b.Summarize != nil:
		ab.Changed, ab.HasKindData = b.Summarize.Changed, true
	case b.SampleData != nil:
		for _, st := range b.SampleData.Steps {
			ab.Steps = append(ab.Steps, toStep(st))
		}
		ab.HasKindData = true
	case b.BeforeAfter != nil:
		bf, af := toStep(b.BeforeAfter.Before), toStep(b.BeforeAfter.After)
		ab.Before, ab.After, ab.Note, ab.HasKindData = &bf, &af, b.BeforeAfter.Note, true
	case b.Trace != nil:
		for _, h := range b.Trace.Hops {
			ab.Hops = append(ab.Hops, session.Hop{At: h.At, Fn: h.Fn, Text: h.Text})
		}
		ab.HasKindData = true
	case b.Research != nil:
		ab.Verdict, ab.Why, ab.HasKindData = b.Research.Verdict, b.Research.Why, true
		for _, e := range b.Research.Evidence {
			ab.Evidence = append(ab.Evidence, session.ParseCitation(e))
		}
	}
	if b.TestCases != nil {
		ab.HasKindData = true
		for _, c := range b.TestCases.Cases {
			ab.Cases = append(ab.Cases, session.TestCase{Case: c.Name, Location: c.At, Description: c.Text, Change: c.Status})
		}
	}
	return ab
}

func toRevision(b answerBodyJSON) session.Revision {
	r := session.Revision{Gist: b.Gist}
	for _, c := range b.Citations {
		r.Citations = append(r.Citations, session.ParseCitation(c))
	}
	if b.Revision != nil {
		r.Text, r.WhatChanged, r.Questions = b.Revision.RevisedText, b.Revision.WhatChanged, b.Revision.Questions
	}
	return r
}

func toAnchorSpec(a anchorSpecJSON) AnchorSpec {
	spec := AnchorSpec{Type: a.Type, Path: a.Path, Side: a.Side, Text: a.Text, Ref: a.Ref}
	if len(a.Lines) == 2 {
		spec.Lines = [2]int{a.Lines[0], a.Lines[1]}
	}
	return spec
}

// LoadFixtures reads fixtures/*.json from dir.
func LoadFixtures(dir string) (*Fixtures, error) {
	f := &Fixtures{}
	if err := readJSON(filepath.Join(dir, "pr.json"), &f.PR); err != nil {
		return nil, fmt.Errorf("pr.json: %w", err)
	}
	if err := readJSON(filepath.Join(dir, "briefing.json"), &f.BriefingRaw); err != nil {
		return nil, fmt.Errorf("briefing.json: %w", err)
	}
	var aj answersJSON
	if err := readJSON(filepath.Join(dir, "answers.json"), &aj); err != nil {
		return nil, fmt.Errorf("answers.json: %w", err)
	}
	for _, a := range aj.Answers {
		kind, ok := session.KindFromFixture(a.Kind)
		if !ok {
			return nil, fmt.Errorf("answers.json: fixture %s has unknown kind %q", a.ID, a.Kind)
		}
		af := AnswerFixture{ID: a.ID, Kind: kind, Question: a.Question, Answer: toAnswerBody(a.Answer)}
		for _, sp := range a.Anchors {
			af.Anchors = append(af.Anchors, toAnchorSpec(sp))
		}
		for _, fu := range a.FollowUps {
			af.FollowUps = append(af.FollowUps, toAnswerBody(fu))
		}
		if a.Seed != nil {
			af.Seed = *a.Seed
		}
		f.Answers = append(f.Answers, af)
	}
	var fj feedbackFileJSON
	if err := readJSON(filepath.Join(dir, "feedback.json"), &fj); err != nil {
		return nil, fmt.Errorf("feedback.json: %w", err)
	}
	for _, s := range fj.Feedback {
		f.FeedbackSeeds = append(f.FeedbackSeeds, FeedbackSeed{
			ID: s.ID, Anchor: toAnchorSpec(s.Anchor), Status: s.Status, Body: s.Body,
		})
	}
	var rj revisionsFileJSON
	if err := readJSON(filepath.Join(dir, "revisions.json"), &rj); err != nil {
		return nil, fmt.Errorf("revisions.json: %w", err)
	}
	for _, r := range rj.Revisions {
		f.Revisions = append(f.Revisions, RevisionFixture{FeedbackID: r.Feedback, ForBody: r.ForBody, Revision: toRevision(r.Revision)})
	}
	return f, nil
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(strings.NewReader(string(b)))
	return dec.Decode(v)
}
