package session

import "strings"

// The agent runs in the worktree, so its paths can come back absolute
// ("/home/…/worktree/cmd/bounce.go:12"). They're rewritten to repo-relative
// before anything is stored (ADR 0004).

// RelativeText strips the worktree root from every path in s.
func RelativeText(s, root string) string {
	if root == "" || s == "" {
		return s
	}
	root = strings.TrimSuffix(root, "/")
	s = strings.ReplaceAll(s, root+"/", "")
	return strings.ReplaceAll(s, root, ".")
}

func relCitations(cs []Citation, root string) []Citation {
	out := make([]Citation, len(cs))
	for i, c := range cs {
		c.Path = RelativeText(c.Path, root)
		c.Note = RelativeText(c.Note, root)
		out[i] = c
	}
	if cs == nil {
		return nil
	}
	return out
}

func relStep(st SampleStep, root string) SampleStep {
	st.At = RelativeText(st.At, root)
	st.SourceRef = RelativeText(st.SourceRef, root)
	return st
}

// RelativeAnswer rewrites every path in an Answer: Citations, the Kind's
// locations, what it ran, and the prose.
func RelativeAnswer(a AnswerBody, root string) AnswerBody {
	if root == "" {
		return a
	}
	a.Gist = RelativeText(a.Gist, root)
	a.Detail = RelativeText(a.Detail, root)
	a.Example = RelativeText(a.Example, root)
	a.Changed = RelativeText(a.Changed, root)
	a.Note = RelativeText(a.Note, root)
	a.Why = RelativeText(a.Why, root)
	a.Citations = relCitations(a.Citations, root)
	a.Evidence = relCitations(a.Evidence, root)
	if a.Steps != nil {
		steps := make([]SampleStep, len(a.Steps))
		for i, st := range a.Steps {
			steps[i] = relStep(st, root)
		}
		a.Steps = steps
	}
	if a.Before != nil {
		b := relStep(*a.Before, root)
		a.Before = &b
	}
	if a.After != nil {
		af := relStep(*a.After, root)
		a.After = &af
	}
	if a.Hops != nil {
		hops := make([]Hop, len(a.Hops))
		for i, h := range a.Hops {
			h.At = RelativeText(h.At, root)
			h.Text = RelativeText(h.Text, root)
			hops[i] = h
		}
		a.Hops = hops
	}
	if a.Cases != nil {
		cases := make([]TestCase, len(a.Cases))
		for i, c := range a.Cases {
			c.Location = RelativeText(c.Location, root)
			cases[i] = c
		}
		a.Cases = cases
	}
	if a.Ran != nil {
		ran := make([]Ran, len(a.Ran))
		for i, r := range a.Ran {
			ran[i] = Ran{Command: RelativeText(r.Command, root), Result: RelativeText(r.Result, root)}
		}
		a.Ran = ran
	}
	return a
}

// RelativeRevision rewrites the paths in a Revision.
func RelativeRevision(r Revision, root string) Revision {
	if root == "" {
		return r
	}
	r.Text = RelativeText(r.Text, root)
	r.WhatChanged = RelativeText(r.WhatChanged, root)
	r.Gist = RelativeText(r.Gist, root)
	r.Citations = relCitations(r.Citations, root)
	if r.Questions != nil {
		qs := make([]string, len(r.Questions))
		for i, q := range r.Questions {
			qs[i] = RelativeText(q, root)
		}
		r.Questions = qs
	}
	return r
}

// RelativeBriefing rewrites the paths in the Briefing's agent parts.
func RelativeBriefing(n BriefingNotes, root string) BriefingNotes {
	if root == "" {
		return n
	}
	n.TLDR = RelativeText(n.TLDR, root)
	if n.ReadingOrder != nil {
		ro := make([]ReadingStep, len(n.ReadingOrder))
		for i, r := range n.ReadingOrder {
			r.Path = RelativeText(r.Path, root)
			ro[i] = r
		}
		n.ReadingOrder = ro
	}
	if n.ContractChanges != nil {
		cc := make([]ContractChange, len(n.ContractChanges))
		for i, c := range n.ContractChanges {
			c.Detail = RelativeText(c.Detail, root)
			if c.Anchors != nil {
				as := make([]ContractChangeAnchor, len(c.Anchors))
				for j, a := range c.Anchors {
					a.Path = RelativeText(a.Path, root)
					as[j] = a
				}
				c.Anchors = as
			}
			cc[i] = c
		}
		n.ContractChanges = cc
	}
	return n
}
