package app_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/paths"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

func TestParseTarget(t *testing.T) {
	ok := []struct {
		in   string
		want app.Target
	}{
		{"", app.Target{}},
		{"123", app.Target{Number: 123}},
		{"#123", app.Target{Number: 123}},
		{"https://github.com/acme/calc/pull/7", app.Target{OwnerRepo: "acme/calc", Number: 7}},
		{"http://github.com/acme/calc/pull/7/files", app.Target{OwnerRepo: "acme/calc", Number: 7}},
		{"https://www.github.com/acme/calc/pull/7#discussion_r1", app.Target{OwnerRepo: "acme/calc", Number: 7}},
		{"github.com/acme/calc/pull/7?w=1", app.Target{OwnerRepo: "acme/calc", Number: 7}},
		{"https://github.com/acme/calc.js/pull/7/commits/abc123", app.Target{OwnerRepo: "acme/calc.js", Number: 7}},
		{"acme/calc#7", app.Target{OwnerRepo: "acme/calc", Number: 7}},
	}
	for _, c := range ok {
		got, err := app.ParseTarget(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseTarget(%q) = %+v, %v; want %+v", c.in, got, err, c.want)
		}
	}
	for _, bad := range []string{"0", "-3", "abc", "https://gitlab.com/acme/calc/pull/7", "https://github.com/acme/calc/issues/7", "https://github.com/acme/calc/pull/x"} {
		if got, err := app.ParseTarget(bad); err == nil {
			t.Errorf("ParseTarget(%q) = %+v, want an error", bad, got)
		}
	}
}

// cloneOf is a Checkouts.Clone that clones the test repo, as gh would
// clone the real one, and counts its calls.
func cloneOf(t *testing.T, r *testrepo.Repo, calls *int) func(context.Context, string, string) error {
	return func(_ context.Context, _, dir string) error {
		*calls++
		testrepo.Git(t, filepath.Dir(dir), "clone", "-q", "--no-checkout", r.Dir, dir)
		return nil
	}
}

func TestCheckoutsForALinkOutsideACheckout(t *testing.T) {
	r := testrepo.New(t)
	data, elsewhere := t.TempDir(), t.TempDir()
	clones := 0
	c := app.Checkouts{Cwd: elsewhere, DataDir: data, Clone: cloneOf(t, r, &clones)}

	dir, err := c.For(context.Background(), "acme/calc", 7)
	if err != nil {
		t.Fatal(err)
	}
	if want := paths.CloneDir(data, "acme/calc"); dir != want || clones != 1 {
		t.Fatalf("checkout %s (%d clones), want pr-review's clone %s", dir, clones, want)
	}

	// The PR opens from that clone, and its Review Session is the usual one.
	f := forge.NewFake("ana", fakePR(r, "open"))
	o := opener(r, f, data, 7)
	o.Checkout = dir
	eng, _, _, err := o.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer eng.Close()
	wt := paths.SessionDir(data, "acme/calc", 7).Worktree()
	if got := testrepo.Git(t, wt, "rev-parse", "HEAD"); got != r.Head {
		t.Fatalf("worktree at %s, want the PR head", got)
	}

	// Again, and for another PR of the repo: the session's checkout, no
	// second clone.
	for _, n := range []int{7, 8} {
		if again, err := c.For(context.Background(), "ACME/calc", n); err != nil || again != dir || clones != 1 {
			t.Fatalf("PR %d: checkout %s, %v (%d clones), want %s reused", n, again, err, clones, dir)
		}
	}
}

func TestCheckoutsPrefersTheCurrentCheckout(t *testing.T) {
	r := testrepo.New(t)
	testrepo.Git(t, r.Dir, "remote", "add", "origin", "git@github.com:acme/calc.git")
	sub := filepath.Join(r.Dir, "sub")
	os.MkdirAll(sub, 0o755)
	clones := 0
	c := app.Checkouts{Cwd: sub, DataDir: t.TempDir(), Clone: cloneOf(t, r, &clones)}

	dir, err := c.For(context.Background(), "acme/calc", 7)
	if err != nil || dir != r.Dir || clones != 0 {
		t.Fatalf("checkout %s, %v (%d clones), want the current one %s", dir, err, clones, r.Dir)
	}
	// A checkout of another repo doesn't count.
	if dir, err := c.For(context.Background(), "acme/other", 7); err != nil || dir == r.Dir || clones != 1 {
		t.Fatalf("acme/other: checkout %s, %v (%d clones), want a clone", dir, err, clones)
	}
}

func TestCheckoutsReusesAnotherSessionsCheckout(t *testing.T) {
	r := testrepo.New(t)
	data := t.TempDir()
	f := forge.NewFake("ana", fakePR(r, "open"))
	eng, _, _, err := opener(r, f, data, 7).Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	eng.Close()

	clones := 0
	c := app.Checkouts{Cwd: t.TempDir(), DataDir: data, Clone: cloneOf(t, r, &clones)}
	dir, err := c.For(context.Background(), "acme/calc", 9)
	if err != nil || dir != r.Dir || clones != 0 {
		t.Fatalf("checkout %s, %v (%d clones), want PR 7's checkout %s", dir, err, clones, r.Dir)
	}
}
