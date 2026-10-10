package app

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/eventlog"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/paths"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// PR opens a real Pull Request from a checkout of its repo (ADR 0004).
type PR struct {
	Checkout string // any directory inside the Reviewer's checkout
	Number   int    // 0: the current branch's PR
	Forge    forge.Forge
	DataDir  string
	// Agent builds the agent for the Review Session (nil: none). It isn't
	// called for a read-only session.
	// It gets the Review Session's Opened event: model and effort are
	// fixed when it's created.
	Agent func(dir paths.Session, op session.Opened) (engine.Agent, error)
	// Model and Effort are recorded when the Review Session is created.
	Model, Effort string
	// Budgets for the engine (zero: the defaults).
	Budgets engine.Budgets
	// Log receives progress lines ("Creating the worktree…").
	Log func(string)
	// Callers finds the Go Touched Methods' exact callers
	// (engine.GoCallers); nil doesn't. They're cached in the Review
	// Session's directory.
	Callers engine.CallersFunc
}

// Opened is what OpenPR found, for the CLI to report.
type Opened struct {
	Dir      paths.Session
	New      bool   // the Review Session was created now
	Finished string // "merged"/"closed": it finished since it was created (read-only now)
}

func (o PR) log(format string, a ...any) {
	if o.Log != nil {
		o.Log(fmt.Sprintf(format, a...))
	}
}

// Git runs git in dir and returns its trimmed stdout.
func Git(ctx context.Context, dir string, args ...string) (string, error) {
	return runIn(ctx, dir, "git", args...)
}

// runIn runs a command in dir and returns its trimmed stdout.
func runIn(ctx context.Context, dir, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

func hasCommit(ctx context.Context, dir, sha string) bool {
	_, err := Git(ctx, dir, "cat-file", "-e", sha+"^{commit}")
	return err == nil
}

// ensureCommits fetches the PR's head and base into the checkout's object
// store when they're missing (no refs or FETCH_HEAD are written; the
// worktree's detached HEAD keeps the head reachable).
func (o PR) ensureCommits(ctx context.Context, repoRoot string, pr forge.PR) error {
	if !hasCommit(ctx, repoRoot, pr.HeadSHA) {
		o.log("Fetching the PR's head from origin…")
		if _, err := Git(ctx, repoRoot, "fetch", "--no-write-fetch-head", "-q", "origin", fmt.Sprintf("refs/pull/%d/head", pr.Number)); err != nil {
			return err
		}
	}
	if !hasCommit(ctx, repoRoot, pr.BaseSHA) {
		o.log("Fetching the PR's base from origin…")
		if _, err := Git(ctx, repoRoot, "fetch", "--no-write-fetch-head", "-q", "origin", pr.BaseSHA); err != nil {
			if _, err2 := Git(ctx, repoRoot, "fetch", "--no-write-fetch-head", "-q", "origin", "refs/heads/"+pr.BaseRef); err2 != nil {
				return err
			}
		}
	}
	if !hasCommit(ctx, repoRoot, pr.HeadSHA) || !hasCommit(ctx, repoRoot, pr.BaseSHA) {
		return fmt.Errorf("the PR's commits (%s, %s) aren't available", short(pr.BaseSHA), short(pr.HeadSHA))
	}
	return nil
}

func short(s string) string {
	if len(s) > 7 {
		return s[:7]
	}
	return s
}

// firstOpened reads the log's Opened event, if the log has one. Its PR's
// head and base are the latest ones (after any Update).
func firstOpened(log *eventlog.Log) (*session.Opened, bool, error) {
	evs, err := log.Load()
	if err != nil {
		return nil, false, err
	}
	var op *session.Opened
	finished := false
	head, base := "", ""
	for _, ev := range evs {
		switch p := ev.Data.(type) {
		case session.Opened:
			if op == nil {
				pp := p
				op = &pp
			}
		case session.Finished:
			finished = true
		case session.HeadMoved:
			head, base = p.To, p.BaseTo
		}
	}
	if op != nil && head != "" {
		op.PR.HeadSHA, op.PR.BaseSHA = head, base
	}
	return op, finished, nil
}

// RemoveWorktree removes a Review Session's worktree and prunes the
// checkout's worktree list. A worktree that's already gone is fine.
func RemoveWorktree(ctx context.Context, checkout, wt string) error {
	if _, err := os.Stat(wt); err == nil {
		if _, err := Git(ctx, checkout, "worktree", "remove", "--force", wt); err != nil {
			// Not registered (a checkout moved, say): remove the directory.
			if rmErr := os.RemoveAll(wt); rmErr != nil {
				return err
			}
		}
	}
	_, err := Git(ctx, checkout, "worktree", "prune")
	return err
}

// Open finds the PR, creates or resumes its Review Session, and opens the
// engine on it.
func (o PR) Open(ctx context.Context) (*engine.Engine, Opened, []string, error) {
	var res Opened
	root, err := Git(ctx, o.Checkout, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, res, nil, errors.New("not in a git checkout: run pr-review in a checkout of the PR's repo, or pass the PR's link (pr-review https://github.com/owner/repo/pull/123)")
	}
	number := o.Number
	if number == 0 {
		branch, err := Git(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
		if err != nil {
			return nil, res, nil, err
		}
		if branch == "HEAD" {
			return nil, res, nil, errors.New("detached HEAD: pass a PR number (pr-review <number>)")
		}
		if number, err = o.Forge.PRForBranch(ctx, branch); err != nil {
			return nil, res, nil, err
		}
	}
	pr, err := o.Forge.PR(ctx, number)
	if err != nil {
		return nil, res, nil, err
	}
	res.Dir = paths.SessionDir(o.DataDir, pr.OwnerRepo, pr.Number)
	log, err := eventlog.Open(res.Dir.Dir())
	if err != nil {
		return nil, res, nil, err
	}
	prior, finished, err := firstOpened(log)
	if err != nil {
		return nil, res, nil, err
	}
	wt := res.Dir.Worktree()

	var opened session.Opened
	var extra []session.Payload
	if prior == nil {
		// A new Review Session, pinned to the PR's head. The diff's base is
		// the merge base of the PR's base and head, like GitHub's: a stacked
		// PR shows only its own changes.
		res.New = true
		if err := o.ensureCommits(ctx, root, pr); err != nil {
			return nil, res, nil, err
		}
		mb, err := Git(ctx, root, "merge-base", pr.BaseSHA, pr.HeadSHA)
		if err != nil {
			return nil, res, nil, err
		}
		opened = session.Opened{
			PR: toSessionPR(pr, mb), Checkout: root, Worktree: wt, Scratch: res.Dir.Scratch(),
			Model: o.Model, Effort: o.Effort,
		}
	} else {
		opened = *prior
		// Created while the PR was open, and it has finished since: the
		// worktree goes and the session is read-only from now on.
		if !finished && sessionState(prior.PR.State) == "open" && pr.State != "open" {
			o.log("PR #%d is %s: removing its worktree", pr.Number, pr.State)
			if err := RemoveWorktree(ctx, root, wt); err != nil {
				return nil, res, nil, err
			}
			extra = append(extra, session.Finished{State: pr.State})
			finished = true
		}
		if finished {
			res.Finished = pr.State
		}
	}

	// The diff comes from the worktree, or the checkout once it's gone.
	diffDir := wt
	if finished {
		diffDir = root
		if err := o.ensureCommits(ctx, root, forge.PR{Number: pr.Number, HeadSHA: opened.PR.HeadSHA, BaseSHA: opened.PR.BaseSHA, BaseRef: opened.PR.BaseRef}); err != nil {
			return nil, res, nil, err
		}
	} else if err := o.ensureWorktree(ctx, root, wt, opened.PR.HeadSHA, pr); err != nil {
		return nil, res, nil, err
	}
	if err := os.MkdirAll(res.Dir.Scratch(), 0o755); err != nil {
		return nil, res, nil, err
	}

	var agent engine.Agent
	if !finished && o.Agent != nil {
		if agent, err = o.Agent(res.Dir, opened); err != nil {
			return nil, res, nil, err
		}
	}
	eng, notes, err := engine.Open(engine.Config{
		Repo:     gitsrc.OpenRepo(diffDir, opened.PR.BaseSHA, opened.PR.HeadSHA),
		Opened:   opened,
		Store:    log,
		Agent:    agent,
		Budgets:  o.Budgets,
		Forge:    o.Forge,
		Callers:  o.Callers,
		CacheDir: res.Dir.Dir(),
	})
	if err != nil {
		return nil, res, nil, err
	}
	if len(extra) > 0 {
		if err := eng.Record(extra...); err != nil {
			eng.Close()
			return nil, res, nil, err
		}
	}
	return eng, res, notes, nil
}

func sessionState(s string) string {
	if s == "" {
		return "open"
	}
	return s
}

// ensureWorktree checks the Review Session's head out, detached, at its
// fixed path (created on first use; ADR 0004).
func (o PR) ensureWorktree(ctx context.Context, root, wt, head string, pr forge.PR) error {
	if _, err := os.Stat(filepath.Join(wt, ".git")); err == nil {
		// An Update interrupted between its checkout and its log entry can
		// leave the worktree at another head: put it back.
		if at, err := Git(ctx, wt, "rev-parse", "HEAD"); err == nil && at != head {
			o.log("Checking the worktree out at %s…", short(head))
			_, err := Git(ctx, wt, "checkout", "--detach", "--force", "-q", head)
			return err
		}
		return nil
	}
	if err := o.ensureCommits(ctx, root, forge.PR{Number: pr.Number, HeadSHA: head, BaseSHA: pr.BaseSHA, BaseRef: pr.BaseRef}); err != nil {
		return err
	}
	o.log("Creating the worktree at %s…", wt)
	if err := os.MkdirAll(filepath.Dir(wt), 0o755); err != nil {
		return err
	}
	_, _ = Git(ctx, root, "worktree", "prune") // a stale entry for this path
	_, err := Git(ctx, root, "worktree", "add", "--detach", wt, head)
	return err
}

func toSessionPR(p forge.PR, mergeBase string) session.PullRequest {
	return session.PullRequest{
		RepoURL: "https://github.com/" + p.OwnerRepo, OwnerRepo: p.OwnerRepo,
		Number: p.Number, URL: p.URL, Title: p.Title, Author: p.Author,
		HeadRef: p.HeadRef, BaseRef: p.BaseRef, MergedAt: p.MergedAt, State: p.State,
		BaseSHA: mergeBase, HeadSHA: p.HeadSHA, Description: p.Body,
		Files: p.Files, Additions: p.Additions, Deletions: p.Deletions,
	}
}

// Prune removes the worktrees of finished Review Sessions: those whose PR
// is merged or closed. It returns what it removed.
func Prune(ctx context.Context, dataDir string, forgeFor func(checkout, ownerRepo string) forge.Forge) ([]string, error) {
	dirs, err := paths.AllSessions(dataDir)
	if err != nil {
		return nil, err
	}
	var removed []string
	for _, d := range dirs {
		if _, err := os.Stat(d.Worktree()); err != nil {
			continue // no worktree
		}
		log, err := eventlog.Open(d.Dir())
		if err != nil {
			return removed, err
		}
		op, finished, err := firstOpened(log)
		if err != nil || op == nil {
			continue
		}
		state := "merged"
		if !finished {
			pr, err := forgeFor(op.Checkout, op.PR.OwnerRepo).PR(ctx, op.PR.Number)
			if err != nil {
				return removed, fmt.Errorf("%s #%d: %w", op.PR.OwnerRepo, op.PR.Number, err)
			}
			if pr.State == "open" {
				continue
			}
			state = pr.State
		}
		if err := RemoveWorktree(ctx, op.Checkout, d.Worktree()); err != nil {
			return removed, err
		}
		if !finished {
			ev := session.Event{ID: "prune:" + fmt.Sprint(os.Getpid()), Writer: "prune", At: now(), Data: session.Finished{State: state}}
			if err := log.Append(ev); err != nil {
				return removed, err
			}
		}
		removed = append(removed, fmt.Sprintf("%s #%d (%s)", op.PR.OwnerRepo, op.PR.Number, state))
	}
	return removed, nil
}

var now = time.Now
