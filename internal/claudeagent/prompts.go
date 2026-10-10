package claudeagent

import (
	"fmt"
	"strings"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// The request prompts put the
// Kind first, then the Anchor, then the Question.

func prContext(pr session.PullRequest) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Pull Request #%d: %s\n", pr.Number, pr.Title)
	if pr.BaseRef != "" || pr.HeadRef != "" {
		fmt.Fprintf(&b, "Branch %s into %s.\n", pr.HeadRef, pr.BaseRef)
	}
	fmt.Fprintf(&b, "Base %s, head %s (checked out). See the change with `git diff %s %s`.\n", pr.BaseSHA, pr.HeadSHA, pr.BaseSHA, pr.HeadSHA)
	fmt.Fprintf(&b, "Description:\n%s\n", strings.TrimSpace(pr.Description))
	return b.String()
}

// BriefingPrompt asks for the Briefing, giving the computed parts.
func BriefingPrompt(req engine.BriefingRequest) string {
	var b strings.Builder
	b.WriteString("Kind: briefing\n")
	b.WriteString(prContext(req.PR))
	b.WriteString("\nChanged files (computed):\n")
	for _, f := range req.Files {
		fmt.Fprintf(&b, "%s  %s  +%d -%d\n", f.Path, f.Status, f.Added, f.Removed)
	}
	if len(req.Methods) > 0 {
		b.WriteString("\nTouched Methods (computed, as file::method; use these keys in `methods`):\n")
		for _, m := range req.Methods {
			b.WriteString(m + "\n")
		}
	} else {
		b.WriteString("\nTouched Methods: none computed (this language isn't analysed yet); leave `methods` empty.\n")
	}
	return b.String()
}

func anchorBlock(a session.Anchor, code, context string) string {
	var b strings.Builder
	switch a.Type {
	case session.AnchorDiff:
		lines := ""
		switch {
		case a.NewStart > 0:
			lines = fmt.Sprintf("%s:%d-%d (new side of the diff)", a.Path, a.NewStart, a.NewEnd)
		case a.OldStart > 0:
			lines = fmt.Sprintf("%s:%d-%d (old side of the diff: removed lines)", a.Path, a.OldStart, a.OldEnd)
		default:
			lines = a.Path
		}
		fmt.Fprintf(&b, "Anchor: %s\n```\n%s```\n", lines, code)
		if t := strings.TrimSpace(a.Text); t != "" && !strings.Contains(code, t) {
			fmt.Fprintf(&b, "Selected text: “%s”\n", t)
		}
	default:
		fmt.Fprintf(&b, "Anchor: %s\n", context)
		if t := strings.TrimSpace(a.Text); t != "" && a.Type != session.AnchorFile {
			fmt.Fprintf(&b, "Selected text: “%s”\n", t)
		}
	}
	return b.String()
}

// QuestionPrompt is a Question: the first in a Thread (forked from the
// Briefing), or a follow-up in the Thread's own session.
func QuestionPrompt(req engine.QuestionRequest, followUp bool) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Kind: %s\n", req.Kind.FixtureName())
	switch {
	case followUp && req.Reask:
		fmt.Fprintf(&b, "The Pull Request has moved to a new head, %s, and your working directory is checked out at it. Your earlier Answer in this Thread is Stale: answer the Question again from the code as it is now (re-read what you cite), and say in the gist if the Answer changed.\n", req.Head)
		b.WriteString(anchorBlock(req.Anchor, req.AnchorCode, req.AnchorContext))
	case followUp:
		b.WriteString("A follow-up in this Thread: the Anchor and your earlier Answer are above.\n")
	default:
		b.WriteString(anchorBlock(req.Anchor, req.AnchorCode, req.AnchorContext))
	}
	fmt.Fprintf(&b, "Question: %s\n", req.Question)
	return b.String()
}

// RefinePrompt asks for a Revision of Feedback, or carries the Reviewer's
// answers to the questions it asked.
func RefinePrompt(req engine.RefineRequest) string {
	var b strings.Builder
	b.WriteString("Kind: revision\n")
	if req.Resume {
		fmt.Fprintf(&b, "The Reviewer answered your questions:\n%s\n\nRefine the same Feedback again, now with a `revised_text`:\n%s\n", req.Answers, req.Body)
		return b.String()
	}
	b.WriteString(anchorBlock(req.Anchor, req.AnchorCode, req.AnchorContext))
	b.WriteString("Feedback to refine (the Reviewer will post this to the author):\n")
	b.WriteString(req.Body + "\n")
	if req.Answers != "" {
		fmt.Fprintf(&b, "\nThe Reviewer's answers to your earlier questions:\n%s\n", req.Answers)
	}
	if len(req.Research) > 0 {
		b.WriteString("\nWhat the Reviewer's Questions on this Anchor found:\n")
		for _, r := range req.Research {
			b.WriteString("- " + r + "\n")
		}
	}
	return b.String()
}
