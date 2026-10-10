package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/stephen-corgiat/pr-review/internal/eventlog"
	"github.com/stephen-corgiat/pr-review/internal/paths"
)

// Target is the PR `pr-review [arg]` names.
type Target struct {
	OwnerRepo string // empty: the repo of the checkout pr-review runs in
	Number    int    // 0: the current branch's PR
}

var (
	prURLRE   = regexp.MustCompile(`^(?:https?://)?(?:www\.)?github\.com/([^/\s]+)/([^/\s]+)/pull/(\d+)(?:[/?#].*)?$`)
	prShortRE = regexp.MustCompile(`^([^/\s#]+)/([^/\s#]+)#(\d+)$`)
)

// ParseTarget reads a PR number ("123", "#123"), a PR link
// ("https://github.com/owner/repo/pull/123", with or without the scheme,
// and with anything after the number, like /files) or "owner/repo#123".
// An empty arg is the current branch's PR.
func ParseTarget(arg string) (Target, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return Target{}, nil
	}
	if n, err := strconv.Atoi(strings.TrimPrefix(arg, "#")); err == nil {
		if n <= 0 {
			return Target{}, fmt.Errorf("not a PR number: %q", arg)
		}
		return Target{Number: n}, nil
	}
	m := prURLRE.FindStringSubmatch(arg)
	if m == nil {
		m = prShortRE.FindStringSubmatch(arg)
	}
	if m == nil {
		if strings.Contains(arg, "/pull/") {
			return Target{}, fmt.Errorf("not a GitHub PR link: %q (only github.com is supported)", arg)
		}
		return Target{}, fmt.Errorf("not a PR number or link: %q", arg)
	}
	n, err := strconv.Atoi(m[3])
	if err != nil || n <= 0 {
		return Target{}, fmt.Errorf("not a PR number: %q", m[3])
	}
	return Target{OwnerRepo: m[1] + "/" + strings.TrimSuffix(m[2], ".git"), Number: n}, nil
}

// Checkouts finds a checkout of a repo to open one of its PRs from, so a
// PR link works from any directory. The checkout only supplies git
// objects: the Review Session's worktree is made from it and it's never
// changed (ADR 0004).
type Checkouts struct {
	Cwd     string
	DataDir string
	// Clone makes a checkout of ownerRepo at dir. nil: `gh repo clone`,
	// blobless and without checking files out.
	Clone func(ctx context.Context, ownerRepo, dir string) error
	Log   func(string)
}

// For returns, in order: the checkout this PR's Review Session was opened
// from (its worktree belongs to that checkout); the current directory's,
// when its origin is ownerRepo; the checkout of another Review Session of
// ownerRepo; or pr-review's own clone in the data directory, made on first
// use.
func (c Checkouts) For(ctx context.Context, ownerRepo string, number int) (string, error) {
	var other string
	if dirs, err := paths.AllSessions(c.DataDir); err == nil {
		for _, d := range dirs {
			op, _, err := firstOpened(eventlog.OpenExisting(d.Dir()))
			if err != nil || op == nil || !strings.EqualFold(op.PR.OwnerRepo, ownerRepo) || !isCheckout(ctx, op.Checkout) {
				continue
			}
			if op.PR.Number == number {
				return op.Checkout, nil
			}
			if other == "" {
				other = op.Checkout
			}
		}
	}
	if root, err := Git(ctx, c.Cwd, "rev-parse", "--show-toplevel"); err == nil {
		if u, err := Git(ctx, root, "remote", "get-url", "origin"); err == nil && strings.EqualFold(OwnerRepoFromRemote(u), ownerRepo) {
			return root, nil
		}
	}
	if other != "" {
		return other, nil
	}
	dir := paths.CloneDir(c.DataDir, ownerRepo)
	if isCheckout(ctx, dir) {
		return dir, nil
	}
	if c.Log != nil {
		c.Log(fmt.Sprintf("Cloning %s into %s (once; pr-review reuses it)…", ownerRepo, dir))
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	clone := c.Clone
	if clone == nil {
		clone = ghClone
	}
	if err := clone(ctx, ownerRepo, dir); err != nil {
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("cloning %s: %w", ownerRepo, err)
	}
	return dir, nil
}

// ghClone clones with gh, so the Reviewer's git protocol and auth apply.
// Blobless: file contents are fetched as the worktree and diffs need them.
func ghClone(ctx context.Context, ownerRepo, dir string) error {
	_, err := runIn(ctx, filepath.Dir(dir), "gh", "repo", "clone", ownerRepo, dir, "--", "--filter=blob:none", "--no-checkout", "-q")
	return err
}

func isCheckout(ctx context.Context, dir string) bool {
	if dir == "" {
		return false
	}
	if _, err := os.Stat(dir); err != nil {
		return false
	}
	_, err := Git(ctx, dir, "rev-parse", "--show-toplevel")
	return err == nil
}
