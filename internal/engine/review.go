package engine

// Posting a GitHub review: one batched review, submitted at once, with a
// line comment for each ready item on diff lines GitHub can place, and the
// rest (the description, the Briefing, the whole PR, a whole file, a PR
// Comment, lines outside GitHub's hunks, Stale Anchors) in its body.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// ReviewPost is a review that passed its checks, ready to send.
type ReviewPost struct {
	IDs     []string
	Event   string // Comment | Request changes | Approve
	Summary string
	// Review is what goes to GitHub.
	Review forge.Review

	forge     forge.Forge
	ownerRepo string
	number    int
}

// Lines is how many items are line comments (the rest are in the body).
func (p *ReviewPost) Lines() int { return len(p.Review.Comments) }

// Local: there's no Forge (fixture mode), so sending only records it.
func (p *ReviewPost) Local() bool { return p.forge == nil }

// Send posts the review. It touches no engine state, so it may run on any
// goroutine. With no Forge it sends nothing.
func (p *ReviewPost) Send(ctx context.Context) (forge.Posted, error) {
	if p.forge == nil {
		return forge.Posted{}, nil
	}
	return p.forge.PostReview(ctx, p.ownerRepo, p.number, p.Review)
}

// ghEvent is GitHub's name for a review type.
func ghEvent(ev string) (string, error) {
	switch ev {
	case "Comment", "":
		return forge.EventComment, nil
	case "Request changes":
		return forge.EventRequestChanges, nil
	case "Approve":
		return forge.EventApprove, nil
	}
	return "", fmt.Errorf("unknown review type %q", ev)
}

// PrepareReview checks a review can be posted and builds its payload:
// only ready items, no unfilled Agent Notes, only Comment on your own PR.
func (e *Engine) PrepareReview(c PostReview) (*ReviewPost, error) {
	ids, err := e.readyItems(c.IDs)
	if err != nil {
		return nil, err
	}
	if e.st.ReviewerIsAuthor && c.Event != "Comment" && c.Event != "" {
		return nil, fmt.Errorf("it's your own PR: GitHub allows only a Comment review, not %s", c.Event)
	}
	ev, err := ghEvent(c.Event)
	if err != nil {
		return nil, err
	}
	var fs []*session.Feedback
	for _, id := range ids {
		fs = append(fs, e.st.FeedbackByID(id))
	}
	if bad, n := UnfilledNotes(fs); n > 0 {
		s := "s"
		if n == 1 {
			s = ""
		}
		return nil, fmt.Errorf("%s: %d Agent Note%s unfilled — Refine or remove them", strings.Join(bad, ", "), n, s)
	}
	if c.Event == "" {
		c.Event = "Comment"
	}
	r := e.reviewPayload(fs, ev, c.Summary)
	return &ReviewPost{IDs: ids, Event: c.Event, Summary: strings.TrimSpace(c.Summary), Review: r,
		forge: e.forge, ownerRepo: e.st.PR.OwnerRepo, number: e.st.PR.Number}, nil
}

// ReviewSent records a review that was posted (or, with no Forge, only
// recorded): its items become posted.
type ReviewSent struct {
	Post   *ReviewPost
	Result forge.Posted
}

func (c ReviewSent) events(e *Engine) ([]session.Payload, error) {
	if c.Post == nil {
		return nil, errors.New("no review")
	}
	return []session.Payload{session.ReviewPosted{
		FeedbackIDs: c.Post.IDs, Event: c.Post.Event, Summary: c.Post.Summary,
		ReviewID: c.Result.ID, URL: c.Result.URL, LineComments: c.Post.Lines(), Local: c.Post.Local(),
	}}, nil
}

func (e *Engine) reviewPayload(fs []*session.Feedback, event, summary string) forge.Review {
	r := forge.Review{CommitID: e.st.PR.HeadSHA, Event: event}
	var parts []string
	if s := strings.TrimSpace(summary); s != "" {
		parts = append(parts, s)
	}
	for _, f := range fs {
		if c, ok := e.lineComment(f); ok {
			r.Comments = append(r.Comments, c)
			continue
		}
		parts = append(parts, e.bodyItem(f))
	}
	r.Body = strings.Join(parts, "\n\n")
	return r
}

// rootAnchor follows Anchors on Answers back to the Thread's own Anchor.
func (e *Engine) rootAnchor(a session.Anchor) session.Anchor {
	for n := 0; a.Type == session.AnchorAnswer && n < 20; n++ {
		t := e.st.ThreadByID(a.ThreadID)
		if t == nil {
			break
		}
		a = t.Anchor
	}
	return a
}

// lineComment makes f a line comment when its Anchor is on diff lines
// inside one of GitHub's hunks (3 lines of context, like ours).
func (e *Engine) lineComment(f *session.Feedback) (forge.ReviewComment, bool) {
	a := e.rootAnchor(f.Anchor)
	if a.Type != session.AnchorDiff || a.Stale {
		return forge.ReviewComment{}, false
	}
	fd := e.st.Files[a.Path]
	if fd == nil || a.RowStart < 0 || a.RowEnd < a.RowStart || !inOneHunk(fd, a.RowStart, a.RowEnd) {
		return forge.ReviewComment{}, false
	}
	a = e.st.LocateAnchor(a)
	c := forge.ReviewComment{Path: a.Path, Body: f.Body}
	switch {
	case a.NewStart > 0:
		c.Side, c.Line = "RIGHT", a.NewEnd
		if a.NewStart != a.NewEnd {
			c.StartLine, c.StartSide = a.NewStart, "RIGHT"
		}
	case a.OldStart > 0:
		c.Side, c.Line = "LEFT", a.OldEnd
		if a.OldStart != a.OldEnd {
			c.StartLine, c.StartSide = a.OldStart, "LEFT"
		}
	default:
		return forge.ReviewComment{}, false
	}
	return c, true
}

func inOneHunk(fd *session.FileDiff, from, to int) bool {
	for _, h := range fd.Hunks {
		if from >= h.Start && to < h.End {
			return true
		}
	}
	return false
}

// bodyItem is f in the review's body: where it's about, quoted, then the
// Feedback.
func (e *Engine) bodyItem(f *session.Feedback) string {
	a := e.rootAnchor(f.Anchor)
	var head, quote string
	switch a.Type {
	case session.AnchorDiff:
		loc := a.Path
		if a.NewStart > 0 {
			loc = lineRange(a.Path, a.NewStart, a.NewEnd)
		} else if a.OldStart > 0 {
			loc = lineRange(a.Path, a.OldStart, a.OldEnd) + " (removed lines)"
		}
		head = "**On `" + loc + "`**"
		if a.Stale {
			head += " (these lines changed after this was written)"
		}
		quote = a.Text
	case session.AnchorFile:
		head = "**On `" + a.Path + "`**"
	case session.AnchorDescription:
		head, quote = "**On the PR description**", a.Text
	case session.AnchorComment:
		who := "a comment"
		if c := e.st.CommentByID(a.Ref); c != nil {
			who = "@" + c.Author + "'s comment"
		}
		head, quote = "**Re "+who+"**", a.Text
	case session.AnchorPR:
		if a.Stale && a.Path != "" {
			head, quote = "**On `"+a.Path+"`** (since removed from the PR)", a.Text
		}
	default: // the Briefing: private, so only its text is quoted
		quote = a.Text
	}
	var b strings.Builder
	if head != "" {
		b.WriteString(head + "\n\n")
	}
	if q := strings.TrimSpace(quote); q != "" {
		for _, l := range strings.Split(q, "\n") {
			b.WriteString("> " + l + "\n")
		}
		b.WriteString("\n")
	}
	b.WriteString(strings.TrimSpace(f.Body))
	return b.String()
}

func lineRange(path string, from, to int) string {
	if from == to || to == 0 {
		return fmt.Sprintf("%s:%d", path, from)
	}
	return fmt.Sprintf("%s:%d-%d", path, from, to)
}
