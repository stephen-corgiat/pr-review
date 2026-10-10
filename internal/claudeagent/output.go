package claudeagent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

// output is the union schema's shape (union.json).
type output struct {
	Kind      string   `json:"kind"`
	Gist      string   `json:"gist"`
	Detail    string   `json:"detail"`
	Citations []string `json:"citations"`
	Ran       []struct {
		Command  string `json:"command"`
		Result   string `json:"result"`
		Confirms bool   `json:"confirms"`
	} `json:"ran"`
	Briefing *struct {
		TLDR         string `json:"tldr"`
		ReadingOrder []struct {
			Path string `json:"path"`
			Line string `json:"line"`
		} `json:"reading_order"`
		Methods []struct {
			Key     string `json:"key"`
			Does    string `json:"does"`
			Changed string `json:"changed"`
		} `json:"methods"`
		ContractChanges []struct {
			Type string `json:"type"`
			At   string `json:"at"`
			Text string `json:"text"`
		} `json:"contract_changes"`
	} `json:"briefing"`
	Define *struct {
		Term    string `json:"term"`
		Example string `json:"example"`
	} `json:"define"`
	Summarize *struct {
		Changed string `json:"changed"`
	} `json:"summarize"`
	SampleData *struct {
		Steps []step `json:"steps"`
	} `json:"sample_data"`
	BeforeAfter *struct {
		Before step   `json:"before"`
		After  step   `json:"after"`
		Note   string `json:"note"`
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

type step struct {
	Label     string `json:"label"`
	At        string `json:"at"`
	Value     string `json:"value"`
	Lang      string `json:"lang"`
	Source    string `json:"source"`
	SourceRef string `json:"source_ref"`
}

func (s step) toStep() session.SampleStep {
	return session.SampleStep{Label: s.Label, At: s.At, Value: s.Value, Lang: s.Lang, Source: s.Source, SourceRef: s.SourceRef}
}

func decode(raw json.RawMessage) (*output, error) {
	var o output
	if err := json.Unmarshal(raw, &o); err != nil {
		return nil, fmt.Errorf("the agent's output isn't the union schema: %w", err)
	}
	if strings.TrimSpace(o.Gist) == "" && o.Revision == nil && o.Briefing == nil {
		return nil, fmt.Errorf("the agent's output has no gist")
	}
	return &o, nil
}

func citations(cs []string) []session.Citation {
	var out []session.Citation
	for _, c := range cs {
		if strings.TrimSpace(c) != "" {
			out = append(out, session.ParseCitation(c))
		}
	}
	return out
}

// answer turns the output into an Answer of the asked Kind. Verified: a
// ran entry confirms it.
func (o *output) answer(kind session.QuestionKind) session.AnswerBody {
	a := session.AnswerBody{Kind: kind, Gist: o.Gist, Detail: o.Detail, Citations: citations(o.Citations)}
	for _, r := range o.Ran {
		a.Ran = append(a.Ran, session.Ran{Command: r.Command, Result: r.Result})
		a.Verified = a.Verified || r.Confirms
	}
	switch {
	case o.Define != nil:
		a.Term, a.Example, a.HasKindData = o.Define.Term, o.Define.Example, true
	case o.Summarize != nil:
		a.Changed, a.HasKindData = o.Summarize.Changed, true
	case o.SampleData != nil:
		for _, s := range o.SampleData.Steps {
			a.Steps = append(a.Steps, s.toStep())
		}
		a.HasKindData = true
	case o.BeforeAfter != nil:
		bf, af := o.BeforeAfter.Before.toStep(), o.BeforeAfter.After.toStep()
		a.Before, a.After, a.Note, a.HasKindData = &bf, &af, o.BeforeAfter.Note, true
	case o.Trace != nil:
		for _, h := range o.Trace.Hops {
			a.Hops = append(a.Hops, session.Hop{At: h.At, Fn: h.Fn, Text: h.Text})
		}
		a.HasKindData = true
	case o.Research != nil:
		a.Verdict, a.Why, a.HasKindData = o.Research.Verdict, o.Research.Why, true
		a.Evidence = citations(o.Research.Evidence)
	}
	if o.TestCases != nil {
		a.HasKindData = true
		for _, c := range o.TestCases.Cases {
			a.Cases = append(a.Cases, session.TestCase{Case: c.Name, Location: c.At, Description: c.Text, Change: c.Status})
		}
	}
	return a
}

func (o *output) revision() session.Revision {
	r := session.Revision{Gist: o.Gist, Citations: citations(o.Citations)}
	if o.Revision != nil {
		r.Text, r.WhatChanged, r.Questions = o.Revision.RevisedText, o.Revision.WhatChanged, o.Revision.Questions
	}
	if r.Text == "" && len(r.Questions) == 0 {
		r.Questions = []string{"The agent returned neither a revised text nor questions: " + o.Gist}
	}
	return r
}

func (o *output) briefingNotes() session.BriefingNotes {
	n := session.BriefingNotes{TLDR: o.Gist, Methods: map[string]session.MethodNote{}}
	b := o.Briefing
	if b == nil {
		return n
	}
	if b.TLDR != "" {
		n.TLDR = b.TLDR
	}
	for _, r := range b.ReadingOrder {
		n.ReadingOrder = append(n.ReadingOrder, session.ReadingStep{Path: r.Path, Summary: r.Line})
	}
	for _, m := range b.Methods {
		n.Methods[m.Key] = session.MethodNote{Does: m.Does, Changed: m.Changed}
	}
	for _, c := range b.ContractChanges {
		cc := session.ContractChange{Category: session.CategoryFromName(c.Type), Title: c.Text}
		if c.At != "" {
			cit := session.ParseCitation(c.At)
			cc.Anchors = []session.ContractChangeAnchor{{Path: cit.Path, Line: cit.Line}}
		}
		n.ContractChanges = append(n.ContractChanges, cc)
	}
	return n
}
