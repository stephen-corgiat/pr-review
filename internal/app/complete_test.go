package app_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/shellcomp"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

// values are a completion's candidate values.
func values(c app.Completion) []string {
	var v []string
	for _, cand := range c.Candidates {
		v = append(v, cand.Value)
	}
	return v
}

func has(t *testing.T, c app.Completion, want ...string) {
	t.Helper()
	got := values(c)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("candidates %v, want %v", got, want)
	}
}

func (h *handoffSetup) completer() app.Completer {
	h.f.PRs[8] = forge.PR{Number: 8, Title: "Add Beacon bounce webhooks", State: "open"}
	h.f.PRs[3] = forge.PR{Number: 3, Title: "Old merged thing", State: "merged"}
	return app.Completer{Dir: h.repo.Dir, DataDir: h.data, Forge: h.f}
}

// Every row of the completion table.
func TestComplete(t *testing.T) {
	h := newHandoff(t, "open")
	if _, err := h.export(h.repo.Dir, false); err != nil { // F1 is handed off now
		t.Fatal(err)
	}
	c := h.completer()
	ctx := context.Background()
	words := func(w ...string) app.Completion { return c.Complete(ctx, w) }

	// pr-review <tab>: the subcommands and the open PRs, newest first.
	got := words("")
	has(t, got, "export", "resolve", "skill", "prune", "completion", "8", "7")
	if d := got.Candidates[5].Desc; d != "Add Beacon bounce webhooks" {
		t.Fatalf("PR description %q", d)
	}
	has(t, words("e"), "export")
	has(t, words("7"), "7")
	if words("").Directive != app.DirNoFileComp {
		t.Fatal("PR and subcommand completion must not fall back to files")
	}

	// pr-review -<tab>: the flags, and never the developer ones.
	has(t, words("-"), "-repo", "-base", "-head", "-style")
	// -repo <tab>: directories.
	if got := words("-repo", ""); got.Directive != app.DirDirs || len(got.Candidates) != 0 {
		t.Fatalf("-repo: %+v", got)
	}
	// A flag then the PR number.
	has(t, words("-style", "dracula", "8"), "8")

	has(t, words("skill", ""), "install")
	has(t, words("skill", "install", ""))
	has(t, words("completion", ""), "bash", "zsh", "fish")
	has(t, words("export", ""), "--again")
	has(t, words("export", "--"), "--again")

	// resolve: the handed-off Feedback of this branch's session.
	got = words("resolve", "")
	has(t, got, "F1")
	if !strings.Contains(got.Candidates[0].Desc, "REAL-FEEDBACK") {
		t.Fatalf("description %q", got.Candidates[0].Desc)
	}
	has(t, words("resolve", "F1", ""), "addressed", "declined", "needs-you")
	has(t, words("resolve", "F1", "a"), "addressed")
	has(t, words("resolve", "F1", "addressed", ""))
	has(t, words("resolve", "F1", "addressed", "did it", ""), "--commit")
	got = words("resolve", "F1", "addressed", "did it", "--commit", "")
	if len(got.Candidates) == 0 || got.Candidates[0].Desc == "" {
		t.Fatalf("commits: %+v", got)
	}
	sha := testrepo.Git(t, h.repo.Dir, "rev-parse", "--short", "HEAD")
	if got.Candidates[0].Value != sha {
		t.Fatalf("first commit %q, want %q", got.Candidates[0].Value, sha)
	}
	if len(words("resolve", "F1", "addressed", "--commit", "").Candidates) == 0 {
		t.Fatal("--commit before the note still completes commits")
	}

	// Output format: candidates, a tab and the description, then ":<directive>".
	if out := words("completion", "").String(); out != "bash\nzsh\nfish\n:4\n" {
		t.Fatalf("wire format %q", out)
	}
	if out := words("").String(); !strings.Contains(out, "8\tAdd Beacon bounce webhooks\n") || !strings.HasSuffix(out, ":4\n") {
		t.Fatalf("wire format %q", out)
	}
}

// Feedback that isn't handed off, or a branch with no Review Session, offers nothing.
func TestCompleteResolveOnlyHandedOff(t *testing.T) {
	h := newHandoff(t, "open") // F1 is queued, not handed off yet
	c := h.completer()
	ctx := context.Background()
	has(t, c.Complete(ctx, []string{"resolve", ""}))
	if _, err := h.export(h.repo.Dir, false); err != nil {
		t.Fatal(err)
	}
	has(t, c.Complete(ctx, []string{"resolve", ""}), "F1")
	// Done Feedback drops out.
	h.tui.Poll()
	must(t, h.tui, engine.MarkDone{ID: "F1"})
	h.tui.Poll()
	has(t, c.Complete(ctx, []string{"resolve", ""}))

	other := testrepo.New(t) // no Review Session for this one
	testrepo.Git(t, other.Dir, "remote", "add", "origin", "git@github.com:acme/calc.git")
	testrepo.Git(t, other.Dir, "checkout", "-q", "feature")
	c.Dir = other.Dir
	has(t, c.Complete(ctx, []string{"resolve", ""}))
	// And completing never creates a Review Session.
	if _, err := os.Stat(filepath.Join(h.data, "reviews", "acme", "calc", "9")); err == nil {
		t.Fatal("completion created a session directory")
	}
}

// The PR list is cached for 60 seconds per repo.
func TestCompletePRCache(t *testing.T) {
	h := newHandoff(t, "open")
	c := h.completer()
	now := time.Now()
	c.Now = func() time.Time { return now }
	ctx := context.Background()
	calls := func() (n int) {
		for _, s := range h.f.Calls {
			if s == "OpenPRs" {
				n++
			}
		}
		return
	}
	c.Complete(ctx, []string{""})
	c.Complete(ctx, []string{"1"})
	if calls() != 1 {
		t.Fatalf("%d Forge calls within 60s, want 1", calls())
	}
	now = now.Add(59 * time.Second)
	c.Complete(ctx, []string{""})
	if calls() != 1 {
		t.Fatalf("%d Forge calls at 59s, want 1", calls())
	}
	now = now.Add(2 * time.Second)
	c.Complete(ctx, []string{""})
	if calls() != 2 {
		t.Fatalf("%d Forge calls after 61s, want 2", calls())
	}
}

// A Forge error prints no candidates (not even an error), and the
// subcommands still complete.
func TestCompleteForgeErrorIsSilent(t *testing.T) {
	h := newHandoff(t, "open")
	c := h.completer()
	h.f.ListErr = errors.New("gh: HTTP 502 <html>boom</html>")
	got := c.Complete(context.Background(), []string{""})
	has(t, got, "export", "resolve", "skill", "prune", "completion")
	if out := got.String(); strings.Contains(out, "502") || strings.Contains(out, "boom") {
		t.Fatalf("error text leaked: %q", out)
	}
	// No cache was written for a failure.
	h.f.ListErr = nil
	has(t, c.Complete(context.Background(), []string{"7"}), "7")
}

// Outside a git checkout nothing completes, loudly or otherwise.
func TestCompleteOutsideACheckout(t *testing.T) {
	c := app.Completer{Dir: t.TempDir(), DataDir: t.TempDir(), Forge: forge.NewFake("ana")}
	got := c.Complete(context.Background(), []string{""})
	has(t, got, "export", "resolve", "skill", "prune", "completion")
	has(t, c.Complete(context.Background(), []string{"resolve", ""}))
}

// A hung Forge costs a tab press at most the timeout, then prints nothing.
func TestCompleteForgeTimeout(t *testing.T) {
	h := newHandoff(t, "open")
	c := h.completer()
	c.Forge = hangingForge{h.f}
	start := time.Now()
	got := c.Complete(context.Background(), []string{""})
	if d := time.Since(start); d > 4*time.Second {
		t.Fatalf("took %v", d)
	}
	has(t, got, "export", "resolve", "skill", "prune", "completion")
}

type hangingForge struct{ *forge.Fake }

func (hangingForge) OpenPRs(ctx context.Context, _ int) ([]forge.PRSummary, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// Each shell's script calls __complete; when the shell is installed it
// completes `pr-review ` end to end.
func TestCompletionScripts(t *testing.T) {
	for _, sh := range shellcomp.Shells {
		s, err := shellcomp.Script(sh)
		if err != nil || strings.TrimSpace(s) == "" || !strings.Contains(s, "__complete") {
			t.Fatalf("%s script: %v %q", sh, err, s)
		}
	}
	if _, err := shellcomp.Script("powershell"); err == nil {
		t.Fatal("an unknown shell is an error")
	}
}

// bashCompletes sources the script in a real bash, against a stub
// `pr-review` on PATH, and reads COMPREPLY back.
func TestBashScriptEndToEnd(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash isn't installed")
	}
	dir := t.TempDir()
	stub := "#!/bin/sh\ncase \"$*\" in\n" +
		"'__complete resolve ') printf 'F1\\tFirst thing\\nF2\\tSecond\\n:4\\n';;\n" +
		"'__complete -repo ') printf ':16\\n';;\n" +
		"'__complete ex') printf 'export\\texports it\\n:4\\n';;\n" +
		"*) printf ':4\\n';;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "pr-review"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	script, _ := shellcomp.Script("bash")
	run := func(line string) string {
		words := strings.Fields(line)
		if strings.HasSuffix(line, " ") {
			words = append(words, "")
		}
		src := script + "\nCOMP_WORDS=(" + shQuote(words) + ")\nCOMP_CWORD=" + strconv.Itoa(len(words)-1) +
			"\n_pr_review\nprintf '%s\\n' \"${COMPREPLY[@]}\"\n"
		cmd := exec.Command(bash, "--norc", "-c", src)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "PATH="+dir+":"+os.Getenv("PATH"))
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", line, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if got := run("pr-review resolve "); got != "F1\nF2" {
		t.Fatalf("resolve: %q", got)
	}
	if got := run("pr-review ex"); got != "export" {
		t.Fatalf("ex: %q", got)
	}
	os.Mkdir(filepath.Join(dir, "somedir"), 0o755)
	if got := run("pr-review -repo "); !strings.Contains(got, "somedir") {
		t.Fatalf("-repo: %q", got)
	}
}

func shQuote(words []string) string {
	var q []string
	for _, w := range words {
		q = append(q, "'"+strings.ReplaceAll(w, "'", `'\''`)+"'")
	}
	return strings.Join(q, " ")
}
