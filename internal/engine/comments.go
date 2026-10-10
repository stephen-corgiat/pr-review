package engine

import (
	"context"

	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// PR Comments come from the Forge. They're GitHub's, not the Review
// Session's, so like the diff they're fetched, not logged.

// CommentsFetcher returns a function that fetches the PR Comments, safe to
// run on any goroutine (hand its result to SetComments), or nil when
// there's no Forge.
func (e *Engine) CommentsFetcher() func(context.Context) ([]forge.Comment, error) {
	if e.forge == nil || e.st.RepoMode {
		return nil
	}
	f, repo, n := e.forge, e.st.PR.OwnerRepo, e.st.PR.Number
	return func(ctx context.Context) ([]forge.Comment, error) { return f.Comments(ctx, repo, n) }
}

// SetComments replaces the PR Comments.
func (e *Engine) SetComments(cs []forge.Comment) {
	out := make([]*session.PRComment, 0, len(cs))
	for _, c := range cs {
		out = append(out, &session.PRComment{
			ID: c.ID, Kind: c.Kind, Author: c.Author, Body: c.Body, CreatedAt: c.CreatedAt, URL: c.URL,
			Path: c.Path, Line: c.Line, StartLine: c.StartLine, Side: c.Side, OriginalLine: c.OriginalLine,
			CommitID: c.CommitID, OriginalCommit: c.OriginalCommit, InReplyTo: c.InReplyTo,
		})
	}
	e.st.Comments = out
}

// FetchComments fetches and sets the PR Comments (tests, the CLI).
func (e *Engine) FetchComments(ctx context.Context) error {
	fetch := e.CommentsFetcher()
	if fetch == nil {
		return nil
	}
	cs, err := fetch(ctx)
	if err != nil {
		return err
	}
	e.SetComments(cs)
	return nil
}
