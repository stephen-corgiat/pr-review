// Package forge is everything that touches GitHub, behind one interface:
// the gh/git implementation and a fake for tests.
package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// PR is a Pull Request's metadata.
type PR struct {
	Number    int
	OwnerRepo string
	URL       string
	Title     string
	Body      string
	Author    string
	HeadRef   string
	BaseRef   string
	HeadSHA   string
	BaseSHA   string // the base branch's tip (the diff uses its merge base with the head)
	State     string // open | merged | closed
	IsDraft   bool
	MergedAt  string
	Additions int
	Deletions int
	Files     []string
}

// ErrNoPR: the branch has no Pull Request.
var ErrNoPR = errors.New("no Pull Request for this branch")

// Forge is the GitHub side of a Review Session.
type Forge interface {
	// PRForBranch finds the PR whose head is branch.
	PRForBranch(ctx context.Context, branch string) (int, error)
	// PR gets a PR's metadata, including its base and head shas.
	PR(ctx context.Context, number int) (PR, error)
	// User is the authenticated user's login.
	User(ctx context.Context) (string, error)
	// HeadSHA is the PR's current head, from `git ls-remote` (no API
	// rate limit).
	HeadSHA(ctx context.Context, number int) (string, error)
	// PostReview submits one review at once (not left pending).
	PostReview(ctx context.Context, ownerRepo string, number int, r Review) (Posted, error)
	// Comments are the PR Comments: review comments (on lines), issue
	// comments and review bodies (general), oldest first.
	Comments(ctx context.Context, ownerRepo string, number int) ([]Comment, error)
	// OpenPRs lists the repo's open PRs, newest first, at most limit.
	OpenPRs(ctx context.Context, limit int) ([]PRSummary, error)
}

// PRSummary is one line of a PR list.
type PRSummary struct {
	Number int
	Title  string
}

// Review is one GitHub review, submitted with its event, so it's never
// left pending. Its JSON is the REST API's body.
type Review struct {
	CommitID string          `json:"commit_id,omitempty"`
	Event    string          `json:"event"` // COMMENT | REQUEST_CHANGES | APPROVE
	Body     string          `json:"body,omitempty"`
	Comments []ReviewComment `json:"comments,omitempty"`
}

// ReviewComment is one line comment in a Review: lines StartLine..Line
// (just Line when StartLine is 0) on one side of the diff.
type ReviewComment struct {
	Path      string `json:"path"`
	Line      int    `json:"line"`
	Side      string `json:"side"` // RIGHT (the head) | LEFT (the base)
	StartLine int    `json:"start_line,omitempty"`
	StartSide string `json:"start_side,omitempty"`
	Body      string `json:"body"`
}

// Posted is the review GitHub made.
type Posted struct {
	ID  int64
	URL string
}

// Review events, as GitHub spells them.
const (
	EventComment        = "COMMENT"
	EventRequestChanges = "REQUEST_CHANGES"
	EventApprove        = "APPROVE"
)

// Comment is a PR Comment: read-only in the Review Session.
type Comment struct {
	ID        string // "rc123" review comment, "ic456" issue comment, "rv789" review body
	Kind      string // review | issue | review_body
	Author    string
	Body      string
	CreatedAt string
	URL       string
	// Review comments only: where it sits. Line is on the PR's current
	// head (0 once outdated); OriginalLine on OriginalCommit.
	Path           string
	Line           int
	StartLine      int
	Side           string // RIGHT | LEFT
	OriginalLine   int
	CommitID       string
	OriginalCommit string
	InReplyTo      string // the ID of the comment it answers
}

// GH is the Forge over the gh and git CLIs, run in a checkout.
type GH struct {
	Dir  string // a checkout of the repo
	Repo string // owner/repo; empty means the checkout's own
}

func run(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s %s: %s", name, strings.Join(args, " "), msg)
	}
	return out.String(), nil
}

func (g GH) gh(ctx context.Context, args ...string) (string, error) {
	if g.Repo != "" {
		args = append(args, "-R", g.Repo)
	}
	return run(ctx, g.Dir, "gh", args...)
}

func (g GH) PRForBranch(ctx context.Context, branch string) (int, error) {
	out, err := g.gh(ctx, "pr", "list", "--head", branch, "--state", "all", "--json", "number", "--limit", "1")
	if err != nil {
		return 0, err
	}
	var prs []struct{ Number int }
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return 0, fmt.Errorf("gh pr list: %w", err)
	}
	if len(prs) == 0 {
		return 0, fmt.Errorf("%w %q", ErrNoPR, branch)
	}
	return prs[0].Number, nil
}

func (g GH) OpenPRs(ctx context.Context, limit int) ([]PRSummary, error) {
	out, err := g.gh(ctx, "pr", "list", "--state", "open", "--limit", strconv.Itoa(limit), "--json", "number,title")
	if err != nil {
		return nil, err
	}
	var j []struct {
		Number int
		Title  string
	}
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		return nil, fmt.Errorf("gh pr list: %w", err)
	}
	var prs []PRSummary
	for _, p := range j {
		prs = append(prs, PRSummary{Number: p.Number, Title: p.Title})
	}
	sort.SliceStable(prs, func(a, b int) bool { return prs[a].Number > prs[b].Number })
	return prs, nil
}

func (g GH) PR(ctx context.Context, number int) (PR, error) {
	out, err := g.gh(ctx, "pr", "view", strconv.Itoa(number), "--json",
		"number,title,body,author,url,headRefName,baseRefName,headRefOid,baseRefOid,state,isDraft,mergedAt,additions,deletions,files")
	if err != nil {
		return PR{}, err
	}
	var j struct {
		Number      int
		Title, Body string
		Author      struct{ Login string }
		URL         string
		HeadRefName string
		BaseRefName string
		HeadRefOid  string
		BaseRefOid  string
		State       string
		IsDraft     bool
		MergedAt    string
		Additions   int
		Deletions   int
		Files       []struct{ Path string }
	}
	if err := json.Unmarshal([]byte(out), &j); err != nil {
		return PR{}, fmt.Errorf("gh pr view: %w", err)
	}
	pr := PR{
		Number: j.Number, URL: j.URL, Title: j.Title, Body: j.Body, Author: j.Author.Login,
		HeadRef: j.HeadRefName, BaseRef: j.BaseRefName, HeadSHA: j.HeadRefOid, BaseSHA: j.BaseRefOid,
		State: strings.ToLower(j.State), IsDraft: j.IsDraft, MergedAt: j.MergedAt,
		Additions: j.Additions, Deletions: j.Deletions,
	}
	for _, f := range j.Files {
		pr.Files = append(pr.Files, f.Path)
	}
	pr.OwnerRepo = ownerRepoFromURL(j.URL)
	return pr, nil
}

// ownerRepoFromURL: https://github.com/owner/repo/pull/12 → owner/repo.
func ownerRepoFromURL(u string) string {
	u = strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	parts := strings.Split(u, "/")
	if len(parts) >= 3 {
		return parts[1] + "/" + parts[2]
	}
	return ""
}

func (g GH) User(ctx context.Context) (string, error) {
	out, err := run(ctx, g.Dir, "gh", "api", "user", "-q", ".login")
	return strings.TrimSpace(out), err
}

func (g GH) HeadSHA(ctx context.Context, number int) (string, error) {
	out, err := run(ctx, g.Dir, "git", "ls-remote", "origin", fmt.Sprintf("refs/pull/%d/head", number))
	if err != nil {
		return "", err
	}
	f := strings.Fields(out)
	if len(f) == 0 {
		return "", fmt.Errorf("git ls-remote: no refs/pull/%d/head", number)
	}
	return f[0], nil
}

func (g GH) repoPath(ownerRepo string) string {
	switch {
	case ownerRepo != "":
		return ownerRepo
	case g.Repo != "":
		return g.Repo
	}
	return "{owner}/{repo}"
}

func (g GH) PostReview(ctx context.Context, ownerRepo string, number int, r Review) (Posted, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return Posted{}, err
	}
	cmd := exec.CommandContext(ctx, "gh", "api", "--method", "POST",
		fmt.Sprintf("repos/%s/pulls/%d/reviews", g.repoPath(ownerRepo), number), "--input", "-")
	cmd.Dir = g.Dir
	cmd.Stdin = bytes.NewReader(body)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if m := ghMessage(out.Bytes()); m != "" {
			msg = m
		}
		if msg == "" {
			msg = err.Error()
		}
		return Posted{}, fmt.Errorf("GitHub refused the review: %s", msg)
	}
	var j struct {
		ID      int64  `json:"id"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.Unmarshal(out.Bytes(), &j); err != nil {
		return Posted{}, fmt.Errorf("gh api: %w", err)
	}
	return Posted{ID: j.ID, URL: j.HTMLURL}, nil
}

// ghMessage is the message and errors in a GitHub API error body.
func ghMessage(b []byte) string {
	var j struct {
		Message string `json:"message"`
		Errors  []any  `json:"errors"`
	}
	if json.Unmarshal(b, &j) != nil || j.Message == "" {
		return ""
	}
	msg := j.Message
	for _, e := range j.Errors {
		switch v := e.(type) {
		case string:
			msg += ": " + v
		case map[string]any:
			if m, ok := v["message"].(string); ok {
				msg += ": " + m
			}
		}
	}
	return msg
}

func (g GH) Comments(ctx context.Context, ownerRepo string, number int) ([]Comment, error) {
	repo := g.repoPath(ownerRepo)
	type user struct{ Login string }
	var out []Comment

	// Review comments (on lines).
	raw, err := run(ctx, g.Dir, "gh", "api", "--paginate", fmt.Sprintf("repos/%s/pulls/%d/comments?per_page=100", repo, number))
	if err != nil {
		return nil, err
	}
	var rcs []struct {
		ID               int64
		User             user
		Body             string
		CreatedAt        string `json:"created_at"`
		HTMLURL          string `json:"html_url"`
		Path             string
		Line             int
		StartLine        int `json:"start_line"`
		Side             string
		OriginalLine     int    `json:"original_line"`
		CommitID         string `json:"commit_id"`
		OriginalCommitID string `json:"original_commit_id"`
		InReplyToID      int64  `json:"in_reply_to_id"`
	}
	if err := decodePages(raw, &rcs); err != nil {
		return nil, fmt.Errorf("review comments: %w", err)
	}
	for _, c := range rcs {
		cm := Comment{ID: fmt.Sprintf("rc%d", c.ID), Kind: "review", Author: c.User.Login, Body: c.Body, CreatedAt: c.CreatedAt, URL: c.HTMLURL,
			Path: c.Path, Line: c.Line, StartLine: c.StartLine, Side: c.Side, OriginalLine: c.OriginalLine,
			CommitID: c.CommitID, OriginalCommit: c.OriginalCommitID}
		if c.InReplyToID != 0 {
			cm.InReplyTo = fmt.Sprintf("rc%d", c.InReplyToID)
		}
		out = append(out, cm)
	}

	// Issue comments (general).
	raw, err = run(ctx, g.Dir, "gh", "api", "--paginate", fmt.Sprintf("repos/%s/issues/%d/comments?per_page=100", repo, number))
	if err != nil {
		return nil, err
	}
	var ics []struct {
		ID        int64
		User      user
		Body      string
		CreatedAt string `json:"created_at"`
		HTMLURL   string `json:"html_url"`
	}
	if err := decodePages(raw, &ics); err != nil {
		return nil, fmt.Errorf("issue comments: %w", err)
	}
	for _, c := range ics {
		out = append(out, Comment{ID: fmt.Sprintf("ic%d", c.ID), Kind: "issue", Author: c.User.Login, Body: c.Body, CreatedAt: c.CreatedAt, URL: c.HTMLURL})
	}

	// Review bodies (general; most reviews have none).
	raw, err = run(ctx, g.Dir, "gh", "api", "--paginate", fmt.Sprintf("repos/%s/pulls/%d/reviews?per_page=100", repo, number))
	if err != nil {
		return nil, err
	}
	var rvs []struct {
		ID          int64
		User        user
		Body        string
		State       string
		SubmittedAt string `json:"submitted_at"`
		HTMLURL     string `json:"html_url"`
	}
	if err := decodePages(raw, &rvs); err != nil {
		return nil, fmt.Errorf("reviews: %w", err)
	}
	for _, r := range rvs {
		if strings.TrimSpace(r.Body) == "" {
			continue
		}
		out = append(out, Comment{ID: fmt.Sprintf("rv%d", r.ID), Kind: "review_body", Author: r.User.Login, Body: r.Body, CreatedAt: r.SubmittedAt, URL: r.HTMLURL})
	}
	SortComments(out)
	return out, nil
}

// decodePages decodes `gh api --paginate` output: one JSON array per page,
// back to back.
func decodePages[T any](raw string, into *[]T) error {
	dec := json.NewDecoder(strings.NewReader(raw))
	for dec.More() {
		var page []T
		if err := dec.Decode(&page); err != nil {
			return err
		}
		*into = append(*into, page...)
	}
	return nil
}

// SortComments orders comments oldest first (RFC 3339 times sort as text).
func SortComments(cs []Comment) {
	sort.SliceStable(cs, func(i, j int) bool { return cs[i].CreatedAt < cs[j].CreatedAt })
}

// ---- Fake ----

// Fake is an in-memory Forge for tests.
type Fake struct {
	mu       sync.Mutex
	PRs      map[int]PR
	Branches map[string]int
	Login    string
	UserErr  error // User fails with it (gh not signed in, say)
	Calls    []string
	// Reviews is every review posted, in order; PostErr fails the next one.
	Reviews []Review
	PostErr error
	Comment []Comment // what Comments returns
	ListErr error     // OpenPRs fails with it
	nextID  int64
}

// NewFake returns a Fake holding prs.
func NewFake(login string, prs ...PR) *Fake {
	f := &Fake{PRs: map[int]PR{}, Branches: map[string]int{}, Login: login}
	for _, p := range prs {
		f.PRs[p.Number] = p
		if p.HeadRef != "" {
			f.Branches[p.HeadRef] = p.Number
		}
	}
	return f
}

func (f *Fake) call(s string) {
	f.mu.Lock()
	f.Calls = append(f.Calls, s)
	f.mu.Unlock()
}

// SetState changes a PR's state (a PR merged since, say).
func (f *Fake) SetState(number int, state string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.PRs[number]
	p.State = state
	f.PRs[number] = p
}

func (f *Fake) PRForBranch(_ context.Context, branch string) (int, error) {
	f.call("PRForBranch " + branch)
	f.mu.Lock()
	defer f.mu.Unlock()
	if n, ok := f.Branches[branch]; ok {
		return n, nil
	}
	return 0, fmt.Errorf("%w %q", ErrNoPR, branch)
}

func (f *Fake) PR(_ context.Context, number int) (PR, error) {
	f.call(fmt.Sprintf("PR %d", number))
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.PRs[number]; ok {
		return p, nil
	}
	return PR{}, fmt.Errorf("no PR #%d", number)
}

func (f *Fake) User(context.Context) (string, error) {
	f.call("User")
	if f.UserErr != nil {
		return "", f.UserErr
	}
	return f.Login, nil
}

func (f *Fake) HeadSHA(_ context.Context, number int) (string, error) {
	f.call(fmt.Sprintf("HeadSHA %d", number))
	f.mu.Lock()
	defer f.mu.Unlock()
	if p, ok := f.PRs[number]; ok {
		return p.HeadSHA, nil
	}
	return "", fmt.Errorf("no PR #%d", number)
}

func (f *Fake) PostReview(_ context.Context, ownerRepo string, number int, r Review) (Posted, error) {
	f.call(fmt.Sprintf("PostReview %s#%d", ownerRepo, number))
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.PostErr != nil {
		err := f.PostErr
		f.PostErr = nil
		return Posted{}, err
	}
	f.Reviews = append(f.Reviews, r)
	f.nextID++
	return Posted{ID: f.nextID, URL: fmt.Sprintf("https://github.com/%s/pull/%d#pullrequestreview-%d", ownerRepo, number, f.nextID)}, nil
}

func (f *Fake) Comments(_ context.Context, ownerRepo string, number int) ([]Comment, error) {
	f.call(fmt.Sprintf("Comments %s#%d", ownerRepo, number))
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Comment(nil), f.Comment...), nil
}

func (f *Fake) OpenPRs(_ context.Context, limit int) ([]PRSummary, error) {
	f.call("OpenPRs")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	var out []PRSummary
	for _, p := range f.PRs {
		if p.State == "" || p.State == "open" {
			out = append(out, PRSummary{Number: p.Number, Title: p.Title})
		}
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Number > out[b].Number })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SetHead moves a PR's head (a push, say).
func (f *Fake) SetHead(number int, sha string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := f.PRs[number]
	p.HeadSHA = sha
	f.PRs[number] = p
}

// Posted returns the reviews posted so far.
func (f *Fake) Posted() []Review {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Review(nil), f.Reviews...)
}
