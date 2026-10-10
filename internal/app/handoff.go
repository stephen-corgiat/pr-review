package app

// Hand-off from the Implementing Agent's side (ADR 0003): `pr-review
// export` and `pr-review resolve` run in the Author's checkout, as their
// own processes, and append to the Review Session's event log; a running
// TUI picks the events up. Both find the PR from the current branch, and
// only ever read a real Review Session's log: never fixture mode's.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/eventlog"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/paths"
)

// Refusal is why export or resolve won't run. Nothing was changed: queued
// items stay queued.
type Refusal struct{ Reason string }

func (r *Refusal) Error() string { return r.Reason }

func refuse(format string, a ...any) error { return &Refusal{Reason: fmt.Sprintf(format, a...)} }

// ErrNothingQueued: export found no queued Feedback.
var ErrNothingQueued = errors.New("nothing queued for the Implementing Agent: the Reviewer queues ready Feedback in the TUI (P → Hand to agent)")

// Located is a real Review Session found from a checkout.
type Located struct {
	Checkout string // the checkout's top level
	Branch   string
	PR       forge.PR
	Dir      paths.Session
}

// remoteRepos are the owner/repo of every GitHub remote in the checkout.
func remoteRepos(ctx context.Context, root string) []string {
	out, err := Git(ctx, root, "remote", "-v")
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var repos []string
	for _, l := range strings.Split(out, "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		if r := OwnerRepoFromRemote(f[1]); r != "" && !seen[strings.ToLower(r)] {
			seen[strings.ToLower(r)] = true
			repos = append(repos, r)
		}
	}
	return repos
}

var remoteRE = regexp.MustCompile(`github\.com[:/]+([^/]+)/([^/]+?)(?:\.git)?/?$`)

// OwnerRepoFromRemote reads owner/repo from a GitHub remote URL
// (git@github.com:o/r.git, https://github.com/o/r, ssh://git@github.com/o/r).
func OwnerRepoFromRemote(u string) string {
	m := remoteRE.FindStringSubmatch(strings.TrimSpace(u))
	if m == nil {
		return ""
	}
	return m[1] + "/" + m[2]
}

// Locate finds the Review Session for the PR of the branch checked out in
// dir, and checks dir is a checkout of the PR's repo on the PR's branch.
// It refuses (a *Refusal) with the reason otherwise.
func Locate(ctx context.Context, dir string, f forge.Forge, dataDir string) (*Located, error) {
	root, err := Git(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, refuse("not in a git checkout: run this in a checkout of the PR's repo, on the PR's branch")
	}
	branch, err := Git(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "HEAD" {
		return nil, refuse("this checkout has no branch checked out (detached HEAD): check out the PR's branch")
	}
	number, err := f.PRForBranch(ctx, branch)
	if err != nil {
		if errors.Is(err, forge.ErrNoPR) {
			return nil, refuse("branch %q has no Pull Request: run this in a checkout of the PR's repo, on the PR's branch", branch)
		}
		return nil, refuse("couldn't find the PR for branch %q: %v", branch, err)
	}
	pr, err := f.PR(ctx, number)
	if err != nil {
		return nil, refuse("couldn't read PR #%d: %v", number, err)
	}
	if pr.HeadRef != branch {
		return nil, refuse("this checkout is on %q, but PR #%d's branch is %q", branch, pr.Number, pr.HeadRef)
	}
	repos := remoteRepos(ctx, root)
	match := false
	for _, r := range repos {
		match = match || strings.EqualFold(r, pr.OwnerRepo)
	}
	if !match {
		return nil, refuse("this checkout isn't %s (its remotes: %s)", pr.OwnerRepo, strings.Join(repos, ", "))
	}
	return &Located{Checkout: root, Branch: branch, PR: pr, Dir: paths.SessionDir(dataDir, pr.OwnerRepo, pr.Number)}, nil
}

// openLogged opens the located Review Session's engine from its log, with
// no agent and no Forge (nothing runs; only commands are recorded).
func (l *Located) openLogged() (*engine.Engine, error) {
	log, err := eventlog.Open(l.Dir.Dir())
	if err != nil {
		return nil, err
	}
	op, _, err := firstOpened(log)
	if err != nil {
		return nil, err
	}
	if op == nil {
		return nil, refuse("no Review Session for %s #%d: open it with `pr-review` in this checkout first", l.PR.OwnerRepo, l.PR.Number)
	}
	if op.Fixture || op.RepoMode {
		return nil, refuse("%s holds a fixture or -repo Review Session, not %s #%d's: refusing to hand it off", l.Dir.Dir(), l.PR.OwnerRepo, l.PR.Number)
	}
	if !strings.EqualFold(op.PR.OwnerRepo, l.PR.OwnerRepo) || op.PR.Number != l.PR.Number {
		return nil, refuse("%s holds %s #%d's Review Session, not %s #%d's", l.Dir.Dir(), op.PR.OwnerRepo, op.PR.Number, l.PR.OwnerRepo, l.PR.Number)
	}
	// The diff comes from the Review Session's worktree (at its current
	// head), or the checkout once the worktree is gone.
	diffDir := l.Dir.Worktree()
	if _, err := os.Stat(diffDir); err != nil {
		diffDir = l.Checkout
	}
	eng, _, err := engine.Open(engine.Config{
		Repo:   gitsrc.OpenRepo(diffDir, op.PR.BaseSHA, op.PR.HeadSHA),
		Opened: *op,
		Store:  log,
	})
	if err != nil {
		return nil, err
	}
	if st := eng.State(); st.Fixture || st.PR.Number != l.PR.Number {
		eng.Close()
		return nil, refuse("the log at %s isn't %s #%d's Review Session", l.Dir.Dir(), l.PR.OwnerRepo, l.PR.Number)
	}
	return eng, nil
}

// Export is `pr-review export [--again]`.
type Export struct {
	Dir     string // where it runs: must be a checkout of the PR's repo on its branch
	Forge   forge.Forge
	DataDir string
	Again   bool // hand off the already handed-off items (not done) again too
}

// Exported is what export printed and marked.
type Exported struct {
	Markdown string
	IDs      []string
	Where    *Located
}

// Run checks where it runs, then prints the queued Feedback and research
// and marks those items handed off. Any refusal leaves them queued.
func (x Export) Run(ctx context.Context) (*Exported, error) {
	loc, err := Locate(ctx, x.Dir, x.Forge, x.DataDir)
	if err != nil {
		return nil, err
	}
	if loc.PR.State != "" && loc.PR.State != "open" {
		return nil, refuse("PR #%d is %s: Hand-off is disabled on merged and closed PRs, so nothing was exported (the items stay queued)", loc.PR.Number, loc.PR.State)
	}
	eng, err := loc.openLogged()
	if err != nil {
		return nil, err
	}
	defer eng.Close()
	items := eng.ToExport(x.Again)
	if len(items) == 0 {
		return nil, ErrNothingQueued
	}
	md := eng.State().HandoffMarkdown(items)
	var ids []string
	for _, f := range items {
		ids = append(ids, f.ID)
	}
	if _, err := eng.Do(engine.MarkHandedOff{IDs: ids}); err != nil {
		return nil, fmt.Errorf("marking %s handed off: %w", strings.Join(ids, ", "), err)
	}
	return &Exported{Markdown: md, IDs: ids, Where: loc}, nil
}

// Resolve is `pr-review resolve <id> addressed|declined|needs-you "<note>"
// [--commit <sha>]`: it records an Outcome.
type Resolve struct {
	Dir     string
	Forge   forge.Forge
	DataDir string
	engine.RecordOutcome
}

func (r Resolve) Run(ctx context.Context) (*Located, error) {
	loc, err := Locate(ctx, r.Dir, r.Forge, r.DataDir)
	if err != nil {
		return nil, err
	}
	eng, err := loc.openLogged()
	if err != nil {
		return nil, err
	}
	defer eng.Close()
	if _, err := eng.Do(r.RecordOutcome); err != nil {
		return nil, err
	}
	return loc, nil
}
