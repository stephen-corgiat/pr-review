package replayagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/stephen-corgiat/pr-review/internal/forge"
)

// Forge is the demo's GitHub: it re-enacts a review of a PR, always as an
// open PR. PR metadata comes from forge.json in Dir, which may be written by
// hand (the demo's PR is made up) or, when it's missing, by a recording run
// from the real Forge. The login is fixed; comments are empty; and nothing is
// ever posted: PostReview pretends, in recording too.
type Forge struct {
	Inner  forge.Forge // only used to record
	Dir    string
	Record bool
}

// DemoLogin is who the Reviewer is in the demo.
const DemoLogin = "demo-reviewer"

func (f Forge) file() string { return filepath.Join(f.Dir, "forge.json") }

func (f Forge) PR(ctx context.Context, number int) (forge.PR, error) {
	if _, err := os.Stat(f.file()); f.Record && err != nil {
		pr, err := f.Inner.PR(ctx, number)
		if err != nil {
			return pr, err
		}
		pr.State, pr.MergedAt = "open", ""
		b, _ := json.MarshalIndent(pr, "", "  ")
		if err := os.MkdirAll(f.Dir, 0o755); err != nil {
			return pr, err
		}
		return pr, os.WriteFile(f.file(), append(b, '\n'), 0o644)
	}
	pr, err := f.load()
	if err != nil {
		return pr, err
	}
	if pr.Number != number {
		return pr, fmt.Errorf("replay: only PR #%d is recorded in %s", pr.Number, f.Dir)
	}
	return pr, nil
}

func (f Forge) load() (forge.PR, error) {
	var pr forge.PR
	b, err := os.ReadFile(f.file())
	if err != nil {
		return pr, fmt.Errorf("replay: no recorded PR: %w", err)
	}
	return pr, json.Unmarshal(b, &pr)
}

func (f Forge) PRForBranch(ctx context.Context, branch string) (int, error) {
	if _, err := os.Stat(f.file()); f.Record && err != nil {
		return f.Inner.PRForBranch(ctx, branch)
	}
	pr, err := f.load()
	if err != nil {
		return 0, err
	}
	if pr.HeadRef != branch {
		return 0, fmt.Errorf("%w %q", forge.ErrNoPR, branch)
	}
	return pr.Number, nil
}

func (f Forge) User(context.Context) (string, error) { return DemoLogin, nil }

func (f Forge) HeadSHA(ctx context.Context, number int) (string, error) {
	pr, err := f.load()
	if err != nil {
		if f.Record {
			return f.Inner.HeadSHA(ctx, number)
		}
		return "", err
	}
	return pr.HeadSHA, nil
}

func (f Forge) PostReview(_ context.Context, ownerRepo string, number int, r forge.Review) (forge.Posted, error) {
	return forge.Posted{ID: 1, URL: fmt.Sprintf("https://github.com/%s/pull/%d#pullrequestreview-1", ownerRepo, number)}, nil
}

func (f Forge) Comments(context.Context, string, int) ([]forge.Comment, error) { return nil, nil }

func (f Forge) OpenPRs(context.Context, int) ([]forge.PRSummary, error) {
	return nil, errors.New("not in the demo")
}
