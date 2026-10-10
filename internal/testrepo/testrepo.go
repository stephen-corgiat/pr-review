// Package testrepo builds small git repos for tests, in a temp dir: a Go
// package with a base commit and a head commit on a branch.
package testrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a scratch git repo: base on main, head on the "feature" branch.
type Repo struct {
	Dir        string
	Base, Head string // commit shas
}

const baseGo = `package calc

// Add adds two numbers.
func Add(a, b int) int {
	return a + b
}

// Scale multiplies n by k.
func Scale(n, k int) int {
	return n * k
}
`

const headGo = `package calc

// Add adds two numbers, clamped at Max.
func Add(a, b int) int {
	return clamp(a + b)
}

// Scale multiplies n by k.
func Scale(n, k int) int {
	return clamp(n * k)
}

// Max is the largest result.
const Max = 1000

func clamp(n int) int {
	if n > Max {
		return Max
	}
	return n
}
`

// Git runs git in dir and returns its trimmed output.
func Git(t testing.TB, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t testing.TB, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// New builds the repo under t.TempDir(): calc/calc.go and README.md at
// base; head edits calc.go and adds calc/calc_test.go. The checkout is
// left on main (the Reviewer's own branch), with the feature branch
// holding head.
func New(t testing.TB) *Repo {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "calc-repo")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	Git(t, dir, "init", "-q", "-b", "main")
	write(t, dir, "go.mod", "module example.com/calc\n\ngo 1.22\n")
	write(t, dir, "README.md", "# calc\n\nA tiny calculator.\n")
	write(t, dir, "calc/calc.go", baseGo)
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-q", "-m", "base")
	base := Git(t, dir, "rev-parse", "HEAD")
	Git(t, dir, "checkout", "-q", "-b", "feature")
	write(t, dir, "calc/calc.go", headGo)
	write(t, dir, "calc/calc_test.go", "package calc\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"Add\")\n\t}\n}\n")
	Git(t, dir, "add", "-A")
	Git(t, dir, "commit", "-q", "-m", "Clamp results at Max")
	head := Git(t, dir, "rev-parse", "HEAD")
	Git(t, dir, "checkout", "-q", "main")
	return &Repo{Dir: dir, Base: base, Head: head}
}

// Stack adds a branch "feature2" on top of feature (a stacked PR): it
// edits README.md. It returns the new head. The checkout stays on main.
func (r *Repo) Stack(t testing.TB) string {
	t.Helper()
	Git(t, r.Dir, "checkout", "-q", "-b", "feature2", r.Head)
	write(t, r.Dir, "README.md", "# calc\n\nA tiny calculator that clamps.\n")
	Git(t, r.Dir, "commit", "-q", "-am", "Document clamping")
	head := Git(t, r.Dir, "rev-parse", "HEAD")
	Git(t, r.Dir, "checkout", "-q", "main")
	return head
}

// AdvanceMain adds a commit on main after the PR branched (so a PR's base
// branch tip is past its merge base).
func (r *Repo) AdvanceMain(t testing.TB) string {
	t.Helper()
	write(t, r.Dir, "NOTES.md", "later work on main\n")
	Git(t, r.Dir, "add", "-A")
	Git(t, r.Dir, "commit", "-q", "-m", "later")
	return Git(t, r.Dir, "rev-parse", "HEAD")
}

// headBGo is calc.go at head B: Scale's comment edited (line 8), Min
// added after Max (3 lines after line 15), and clamp's body gains a
// lower bound (3 lines after its line 19).
const headBGo = `package calc

// Add adds two numbers, clamped at Max.
func Add(a, b int) int {
	return clamp(a + b)
}

// Scale multiplies n by k, clamped to [Min, Max].
func Scale(n, k int) int {
	return clamp(n * k)
}

// Max is the largest result.
const Max = 1000

// Min is the smallest result.
const Min = -1000

func clamp(n int) int {
	if n > Max {
		return Max
	}
	if n < Min {
		return Min
	}
	return n
}
`

// Push adds head B on top of head A on the feature branch (a new push to
// the PR): calc.go as headBGo, calc_test.go deleted, calc/doc.go added.
// It returns B. The checked-out branch is left as it was.
func (r *Repo) Push(t testing.TB) string {
	t.Helper()
	was := Git(t, r.Dir, "rev-parse", "--abbrev-ref", "HEAD")
	Git(t, r.Dir, "checkout", "-q", "feature")
	write(t, r.Dir, "calc/calc.go", headBGo)
	write(t, r.Dir, "calc/doc.go", "// Package calc clamps arithmetic.\npackage calc\n")
	Git(t, r.Dir, "rm", "-q", "calc/calc_test.go")
	Git(t, r.Dir, "add", "-A")
	Git(t, r.Dir, "commit", "-q", "-m", "Add Min")
	b := Git(t, r.Dir, "rev-parse", "HEAD")
	Git(t, r.Dir, "checkout", "-q", was)
	return b
}

// Unpushed adds n commits to a local branch (not on the PR's head): the
// Author's work not pushed yet. The checked-out branch is left as it was.
func (r *Repo) Unpushed(t testing.TB, branch string, n int) {
	t.Helper()
	was := Git(t, r.Dir, "rev-parse", "--abbrev-ref", "HEAD")
	Git(t, r.Dir, "checkout", "-q", branch)
	for i := 0; i < n; i++ {
		write(t, r.Dir, "NOTES.md", strings.Repeat("wip\n", i+1))
		Git(t, r.Dir, "add", "-A")
		Git(t, r.Dir, "commit", "-q", "-m", "wip")
	}
	Git(t, r.Dir, "checkout", "-q", was)
}
