package fixturerepo

import "testing"

const trees = "../../testdata/dispatchly/repo"

func TestBuildIsDeterministic(t *testing.T) {
	parent := t.TempDir()
	a, err := Build(trees, parent)
	if err != nil {
		t.Fatal(err)
	}
	// Another parent: the same commits, so the same SHAs.
	b, err := Build(trees, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if a.C0 != b.C0 || a.C1 != b.C1 || a.C2 != b.C2 {
		t.Fatalf("SHAs differ between builds: %+v %+v", a, b)
	}
	// The same parent: the repo is reused, not rebuilt.
	again, err := Build(trees, parent)
	if err != nil || again.Dir != a.Dir || again.C2 != a.C2 {
		t.Fatalf("second build: %v %+v, want %+v", err, again, a)
	}
}
