// Package app opens Review Sessions the ways the CLI does: fixture mode
// (the fixture PR, canned agent), -repo mode (a local base..head, a
// developer aid) and, from a checkout, a real PR. main and the TUI tests
// share it.
package app

import (
	"path/filepath"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/fixtureagent"
	"github.com/stephen-corgiat/pr-review/internal/fixturerepo"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// Fixture opens the fixture PR (dispatchly #214, testdata/dispatchly) with
// the fixture agent.
type Fixture struct {
	FixturesDir string       // testdata/dispatchly/
	CloneDir    string       // where the fixture repo is built (reused when it is there)
	Store       engine.Store // nil: memory only
	Agent       engine.Agent // nil: the fixture agent with its usual delays
	// Callers finds the Go Touched Methods' exact callers on the fixture
	// repo (engine.GoCallers); nil doesn't. CacheDir caches them
	// ("" doesn't).
	Callers  engine.CallersFunc
	CacheDir string
}

// Open builds the fixture repo if needed and opens the Review Session.
func (f Fixture) Open() (*engine.Engine, []string, error) {
	built, err := fixturerepo.Build(filepath.Join(f.FixturesDir, "repo"), f.CloneDir)
	if err != nil {
		return nil, nil, err
	}
	repo := gitsrc.OpenRepo(built.Dir, built.C1, built.C2)
	fx, err := fixtureagent.LoadFixtures(f.FixturesDir)
	if err != nil {
		return nil, nil, err
	}
	agent := f.Agent
	if agent == nil {
		agent = fixtureagent.New(fx)
	}
	p := fx.PR
	pr := session.PullRequest{
		RepoURL: p.RepoURL, OwnerRepo: p.OwnerRepo, GoModule: p.GoModule,
		Number: p.Number, URL: p.URL, Title: p.Title, Author: p.Author,
		HeadRef: p.HeadRef, BaseRef: p.BaseRef, MergedAt: p.MergedAt,
		BaseSHA: built.C1, HeadSHA: built.C2, Description: p.Description,
		Files: p.Files, Additions: p.Stats.Additions, Deletions: p.Stats.Deletions,
	}
	return engine.Open(engine.Config{
		Repo:     repo,
		Opened:   session.Opened{PR: pr, Worktree: repo.Dir, Fixture: true, Model: "fixture"},
		Seed:     fixtureagent.Seed(fx),
		Store:    f.Store,
		Agent:    agent,
		Callers:  f.Callers,
		CacheDir: f.CacheDir,
	})
}

// Repo opens a local repo's base..head with the fixture agent's
// placeholder Answers and no Briefing agent (-repo mode).
func Repo(dir, base, head string, store engine.Store) (*engine.Engine, []string, error) {
	repo := gitsrc.OpenRepo(dir, base, head)
	subj, _ := repo.LogSubject(repo.Head)
	body, _ := repo.LogRangeBody()
	return engine.Open(engine.Config{
		Repo:       repo,
		Opened:     session.Opened{PR: session.PullRequest{Title: subj, Description: body, HeadRef: head, BaseRef: base}, RepoMode: true},
		Store:      store,
		Agent:      fixtureagent.New(nil),
		NoBriefing: true,
	})
}
