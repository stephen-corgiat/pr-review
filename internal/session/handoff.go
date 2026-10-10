package session

import (
	"fmt"
	"path"
	"strings"
)

// HandoffMarkdown is what the Implementing Agent reads. It must stand on
// its own: the agent has none of the TUI's context.
func (s *Session) HandoffMarkdown(fs []*Feedback) string {
	var b strings.Builder
	pr := s.PR
	w := func(format string, a ...any) { fmt.Fprintf(&b, format, a...) }
	w("# Review Feedback: %s #%d\n\n", pr.OwnerRepo, pr.Number)
	w("- **Pull Request:** #%d %q\n", pr.Number, pr.Title)
	if pr.URL != "" {
		w("- **URL:** %s\n", pr.URL)
	}
	w("- **Written against head sha:** `%s`\n", pr.HeadSHA)
	w("- **Items:** %d\n\n", len(fs))
	w("## Instructions\n\n")
	w("You are the Implementing Agent for this Pull Request. The Reviewer wrote the Feedback below and handed it to you, with the research behind it. How to work through it is the Author's call (fix things, discuss them, or hand them on); follow their lead.\n\n")
	w("- Text between `<<` and `>>` is an Agent Note: an instruction to you, not to be quoted. Fill it in or act on it yourself.\n")
	w("- For an item anchored on the PR description, update the description with `gh pr edit %d --body-file …`.\n", pr.Number)
	w("- Each Anchor is quoted as it was at `%s`. If the branch has moved past that sha, re-locate each Anchor (search for the quoted lines) before acting. An Anchor marked Stale was on lines that changed before this export; its quote is what it was written against.\n", shortSHA(pr.HeadSHA))
	w("- The `## Research` section after the items holds every answered Thread from the review (the Reviewer's Questions and the agent's Answers). Use whichever applies to each item; match them by Anchor. ✅ means the agent ran something that confirmed it, 📖 from reading only; values marked invented are illustrations.\n")
	w("- Commit and push your changes: the Reviewer only sees what's pushed to the PR.\n")
	w("- Optional: say what happened to an item with `pr-review resolve <id> addressed \"<what changed>\" [--commit <sha>]`, `pr-review resolve <id> declined \"<why>\"` or `pr-review resolve <id> needs-you \"<question>\"`, run in this checkout. The Reviewer sees it in their review tool.\n\n")

	for _, f := range fs {
		w("## %s\n\n", f.ID)
		loc, lines, lang := s.anchorQuote(f.Anchor)
		w("**Anchor:** %s\n\n", loc)
		if lines != "" {
			w("```%s\n%s\n```\n\n", lang, lines)
		}
		w("**Feedback:**\n\n%s\n\n", f.Body)
		if n := len(AgentNotes(f.Body)); n > 0 {
			w("_%d Agent Note(s) above for you to fill._\n\n", n)
		}
	}
	w("## Research\n\n")
	n := 0
	for _, t := range s.Threads {
		if t.LatestAnswerIdx() < 0 {
			continue
		}
		n++
		s.threadMarkdown(&b, t)
	}
	if n == 0 {
		w("No answered Threads.\n")
	}
	return b.String()
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// anchorQuote is the Anchor's location and, for a diff Anchor, its
// new-side lines at the head sha.
func (s *Session) anchorQuote(a Anchor) (loc, lines, lang string) {
	for a.Type == AnchorAnswer {
		p := s.threadByID(a.ThreadID)
		if p == nil {
			break
		}
		a = p.Anchor
	}
	switch a.Type {
	case AnchorDiff:
		if a.Stale {
			// Its lines changed after it was written: quote what it was on.
			loc = fmt.Sprintf("`%s:%d-%d` (Stale: these lines changed after `%s`; quoted as the Reviewer saw them)", a.Path, a.NewStart, a.NewEnd, shortSHA(a.StaleFrom))
			return loc, a.Text, fenceLang(a.Path)
		}
		fd := s.Files[a.Path]
		if fd == nil || a.RowStart < 0 {
			return "`" + a.Path + "`", "", ""
		}
		var ls []string
		first, last := 0, 0
		for i := a.RowStart; i <= a.RowEnd && i < len(fd.All); i++ {
			l := fd.All[i]
			if l.NewNo == 0 {
				continue // removed at the head sha
			}
			if first == 0 {
				first = l.NewNo
			}
			last = l.NewNo
			ls = append(ls, l.Text)
		}
		if first == 0 {
			return fmt.Sprintf("`%s` (removed lines only)", a.Path), a.Text, ""
		}
		loc = fmt.Sprintf("`%s:%d-%d`", a.Path, first, last)
		if first == last {
			loc = fmt.Sprintf("`%s:%d`", a.Path, first)
		}
		return loc, strings.Join(ls, "\n"), fenceLang(a.Path)
	case AnchorFile:
		return fmt.Sprintf("`%s` (the whole file)", a.Path), "", ""
	case AnchorDescription:
		return "the PR description (edit it with `gh pr edit`)", a.Text, "text"
	case AnchorBriefing:
		return "the Briefing (" + a.Ref + ")", a.Text, "text"
	case AnchorPR:
		if a.Stale && a.Path != "" {
			return fmt.Sprintf("`%s` (since removed from the PR)", a.Path), a.Text, fenceLang(a.Path)
		}
		return "the Pull Request as a whole", "", ""
	case AnchorComment:
		who := "a PR Comment"
		if c := s.CommentByID(a.Ref); c != nil {
			who = "@" + c.Author + "'s PR Comment"
			if c.URL != "" {
				who += " (" + c.URL + ")"
			}
		}
		return who, a.Text, "text"
	}
	return "unknown", a.Text, ""
}

func fenceLang(p string) string {
	switch path.Ext(p) {
	case ".go":
		return "go"
	case ".sql":
		return "sql"
	case ".vue":
		return "vue"
	case ".json":
		return "json"
	case ".yaml", ".yml":
		return "yaml"
	case ".md":
		return "markdown"
	case ".js":
		return "js"
	case ".ts":
		return "ts"
	case ".tsx":
		return "tsx"
	case ".rb":
		return "ruby"
	}
	return ""
}

func badgeText(a *AnswerBody) string {
	switch {
	case a.Verified:
		return "✅ Verified (the agent ran something that confirms it)"
	default:
		return "📖 Unverified (from reading only)"
	}
}

func citeMD(c Citation) string {
	switch {
	case c.URL != "":
		return c.URL
	case c.EndLine != 0:
		return fmt.Sprintf("`%s:%d-%d`", c.Path, c.Line, c.EndLine)
	case c.Line != 0:
		return fmt.Sprintf("`%s:%d`", c.Path, c.Line)
	}
	return "`" + c.Path + "`"
}

func (s *Session) threadMarkdown(b *strings.Builder, t *Thread) {
	w := func(format string, a ...any) { fmt.Fprintf(b, format, a...) }
	loc, _, _ := s.anchorQuote(t.Anchor)
	loc = strings.TrimSuffix(loc, " (edit it with `gh pr edit`)")
	w("### %s · %s\n\n**Anchor:** %s", t.ID, t.Kind, loc)
	if q := oneLine(t.Anchor.Text); q != "" && t.Anchor.Type != AnchorFile {
		if r := []rune(q); len(r) > 80 {
			q = string(r[:79]) + "…"
		}
		w(" · “%s”", q)
	}
	w("\n\n")
	for i, e := range t.Entries {
		switch e.Role {
		case RoleQuestion:
			w("**Question:** %s\n\n", e.Text)
		case RoleAnswer:
			a := e.A
			if a == nil {
				continue
			}
			label := "Answer"
			if i > 1 {
				label = "Later Answer"
			}
			w("**%s** · %s: %s\n\n", label, badgeText(a), oneLine(a.Gist))
			answerKindMarkdown(b, a)
			if a.Detail != "" {
				w("<details><summary>detail</summary>\n\n%s\n\n</details>\n\n", a.Detail)
			}
			if len(a.Citations) > 0 {
				var cs []string
				for _, c := range a.Citations {
					cs = append(cs, citeMD(c))
				}
				w("Citations: %s\n\n", strings.Join(cs, ", "))
			}
		}
	}
	switch t.Status {
	case StatusDraft, StatusQueued, StatusRunning:
		w("_(%s: no Answer yet)_\n\n", t.Status)
	}
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func firstLine(s string) string {
	l, _, _ := strings.Cut(s, "\n")
	return l
}

func stepMarkdown(b *strings.Builder, st SampleStep, label string) {
	head := label
	if st.Label != "" {
		head += " " + st.Label
	}
	if st.At != "" {
		head += " at `" + st.At + "`"
	}
	switch st.Source {
	case "invented":
		head += " (**invented** value)"
	case "":
	default:
		head += " (from " + st.Source
		if st.SourceRef != "" {
			head += " `" + st.SourceRef + "`"
		}
		head += ")"
	}
	fmt.Fprintf(b, "- %s:\n\n  ```%s\n", strings.TrimSpace(head), st.Lang)
	for _, l := range strings.Split(st.Value, "\n") {
		fmt.Fprintf(b, "  %s\n", l)
	}
	fmt.Fprintf(b, "  ```\n\n")
}

func answerKindMarkdown(b *strings.Builder, a *AnswerBody) {
	w := func(format string, x ...any) { fmt.Fprintf(b, format, x...) }
	switch {
	case a.Term != "":
		w("- Term: **%s**\n", a.Term)
		if a.Example != "" {
			w("- Example: %s\n", a.Example)
		}
		w("\n")
	case a.Changed != "":
		w("- Changed: %s\n\n", a.Changed)
	case len(a.Steps) > 0:
		for _, st := range a.Steps {
			stepMarkdown(b, st, "")
		}
	case a.Before != nil:
		stepMarkdown(b, *a.Before, "Before")
		stepMarkdown(b, *a.After, "After")
		if a.Note != "" {
			w("Note: %s\n\n", a.Note)
		}
	case len(a.Hops) > 0:
		for i, h := range a.Hops {
			w("%d. `%s` · %s · %s\n", i+1, h.At, h.Fn, h.Text)
		}
		w("\n")
	case a.Verdict != "":
		w("- Verdict: **%s**: %s\n", a.Verdict, a.Why)
		if len(a.Evidence) > 0 {
			var cs []string
			for _, c := range a.Evidence {
				cs = append(cs, citeMD(c))
			}
			w("- Evidence: %s\n", strings.Join(cs, ", "))
		}
		w("\n")
	}
	if len(a.Cases) > 0 {
		w("| Test case | Location | Change | What |\n|---|---|---|---|\n")
		for _, c := range a.Cases {
			w("| %s | `%s` | %s | %s |\n", c.Case, c.Location, c.Change, c.Description)
		}
		w("\n")
	} else if a.Kind == TestCases && a.HasKindData {
		w("- Test cases: none found\n\n")
	}
}
