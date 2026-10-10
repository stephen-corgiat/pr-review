package claudeagent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// fakeClaude writes a stand-in `claude` that saves its args and stdin and
// prints reply.
func fakeClaude(t *testing.T, reply string) (bin, argsFile, stdinFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile, stdinFile = filepath.Join(dir, "args"), filepath.Join(dir, "stdin")
	replyFile := filepath.Join(dir, "reply")
	if err := os.WriteFile(replyFile, []byte(reply), 0o644); err != nil {
		t.Fatal(err)
	}
	bin = filepath.Join(dir, "claude")
	script := "#!/bin/sh\nfor a in \"$@\"; do printf '%s\\0' \"$a\"; done > " + argsFile + "\ncat > " + stdinFile + "\ncat " + replyFile + "\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return
}

func readArgs(t *testing.T, f string) []string {
	b, err := os.ReadFile(f)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
}

func argAfter(args []string, flag string) []string {
	var out []string
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			out = append(out, args[i+1])
		}
	}
	return out
}

func has(args []string, flag string) bool {
	for _, a := range args {
		if a == flag {
			return true
		}
	}
	return false
}

func testAgent(t *testing.T, bin string) (*Agent, Config) {
	t.Helper()
	root := t.TempDir()
	cfg := Config{
		Binary: bin, Worktree: filepath.Join(root, "worktree"), Scratch: filepath.Join(root, "scratch"),
		Model: "claude-sonnet-5-5", Effort: "medium",
		Related:    []Related{{Alias: "ASDF", Path: "/src/asdf"}},
		SampleData: []string{"/data/sample.json"},
		MCPServers: map[string]json.RawMessage{"jira": json.RawMessage(`{"command":"jira-mcp"}`)},
	}
	os.MkdirAll(cfg.Worktree, 0o755)
	a, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return a, cfg
}

const defineReply = `{"type":"result","subtype":"success","is_error":false,"session_id":"fork-1","total_cost_usd":0.14,"duration_ms":4000,
"modelUsage":{"claude-sonnet-5-5":{}},
"structured_output":{"kind":"define","gist":"A clamp caps a value.","define":{"term":"clamp","example":"clamp(1200) = 1000"},
"citations":["calc/calc.go:18-23"],"ran":[{"command":"go test ./calc/","result":"ok","confirms":true}]}}`

func TestAForkedQuestionsFlags(t *testing.T) {
	bin, argsFile, stdinFile := fakeClaude(t, defineReply)
	a, cfg := testAgent(t, bin)
	res, err := a.Ask(context.Background(), engine.QuestionRequest{
		ThreadID: "T1", Kind: session.Define, ThreadKind: session.Define, Question: "What does clamp mean?",
		Anchor:     session.Anchor{Type: session.AnchorDiff, Path: "calc/calc.go", NewStart: 18, NewEnd: 23, Text: "clamp"},
		AnchorCode: "18: func clamp(n int) int {\n",
		Parent:     session.RunInfo{SessionID: "brief", Total: 0.11}, MaxBudgetUSD: 0.36,
	})
	if err != nil {
		t.Fatal(err)
	}
	args := readArgs(t, argsFile)
	for _, f := range []string{"-p", "--strict-mcp-config", "--disable-slash-commands", "--fork-session"} {
		if !has(args, f) {
			t.Errorf("missing %s", f)
		}
	}
	want := map[string]string{
		"--permission-mode": "auto", "--setting-sources": "project", "--tools": "Read,Grep,Glob,Bash,Write",
		"--resume": "brief", "--max-budget-usd": "0.3600", "--model": "claude-sonnet-5-5", "--effort": "medium",
		"--output-format": "json",
	}
	for f, v := range want {
		if got := argAfter(args, f); len(got) != 1 || got[0] != v {
			t.Errorf("%s = %v, want %s", f, got, v)
		}
	}
	if dirs := argAfter(args, "--add-dir"); len(dirs) != 2 || dirs[0] != cfg.Scratch || dirs[1] != "/src/asdf" {
		t.Errorf("--add-dir = %v", dirs)
	}
	if mcp := argAfter(args, "--mcp-config"); len(mcp) != 1 {
		t.Errorf("--mcp-config = %v", mcp)
	} else if b, _ := os.ReadFile(mcp[0]); !strings.Contains(string(b), "jira-mcp") || strings.HasPrefix(mcp[0], cfg.Worktree) {
		t.Errorf("mcp config %s: %s", mcp[0], b)
	}
	var settings struct {
		DisableAllHooks bool
		Permissions     struct{ Allow, Deny []string }
	}
	json.Unmarshal([]byte(argAfter(args, "--settings")[0]), &settings)
	if !settings.DisableAllHooks {
		t.Errorf("hooks must be off")
	}
	for _, d := range []string{"Write(/" + cfg.Worktree + "/**)", "Edit(/" + cfg.Worktree + "/**)", "Write(//src/asdf/**)"} {
		if !strings.Contains(strings.Join(settings.Permissions.Deny, " "), d) {
			t.Errorf("deny rules lack %s: %v", d, settings.Permissions.Deny)
		}
	}
	for _, d := range []string{"Write(/" + cfg.Scratch + "/**)", "Read(/" + cfg.Worktree + "/**)", "Read(//data/sample.json)"} {
		if !strings.Contains(strings.Join(settings.Permissions.Allow, " "), d) {
			t.Errorf("allow rules lack %s: %v", d, settings.Permissions.Allow)
		}
	}
	sys := argAfter(args, "--append-system-prompt")[0]
	for _, s := range []string{cfg.Scratch, "ASDF", "/data/sample.json", "Never edit the repo", "`ran`"} {
		if !strings.Contains(sys, s) {
			t.Errorf("system prompt lacks %q", s)
		}
	}
	if strings.Contains(sys, "probe") {
		t.Errorf("the system prompt still mentions probe")
	}
	stdin, _ := os.ReadFile(stdinFile)
	for _, s := range []string{"Kind: define", "calc/calc.go:18-23", "func clamp", "Question: What does clamp mean?"} {
		if !strings.Contains(string(stdin), s) {
			t.Errorf("prompt lacks %q:\n%s", s, stdin)
		}
	}
	ab := res.Answer
	if ab.Term != "clamp" || !ab.Verified || len(ab.Ran) != 1 || len(ab.Citations) != 1 || ab.Citations[0].EndLine != 23 {
		t.Errorf("answer = %+v", ab)
	}
	if res.Run.SessionID != "fork-1" || res.Run.Total != 0.14 || res.Run.Model != "claude-sonnet-5-5" {
		t.Errorf("run = %+v", res.Run)
	}
}

func TestAFollowUpResumesWithoutForking(t *testing.T) {
	bin, argsFile, stdinFile := fakeClaude(t, defineReply)
	a, cfg := testAgent(t, bin)
	_, err := a.Continue(context.Background(), engine.QuestionRequest{Kind: session.Define, Question: "Simpler?", Parent: session.RunInfo{SessionID: "fork-1", Total: 0.14}, MaxBudgetUSD: 0.39})
	if err != nil {
		t.Fatal(err)
	}
	args := readArgs(t, argsFile)
	if has(args, "--fork-session") || argAfter(args, "--resume")[0] != "fork-1" {
		t.Errorf("a follow-up resumes its own fork: %v", args)
	}
	if dirs := argAfter(args, "--add-dir"); len(dirs) != 2 || dirs[0] != cfg.Scratch {
		t.Errorf("--add-dir must be passed again on resume: %v", dirs)
	}
	if stdin, _ := os.ReadFile(stdinFile); !strings.Contains(string(stdin), "follow-up") {
		t.Errorf("prompt: %s", stdin)
	}
}

func TestTheBriefingStartsFresh(t *testing.T) {
	reply := `{"type":"result","subtype":"success","session_id":"brief","total_cost_usd":0.11,"structured_output":{"kind":"briefing","gist":"Clamps results.",
"briefing":{"tldr":"Clamps results at Max.","reading_order":[{"path":"calc/calc.go","line":"adds clamp"}],"methods":[{"key":"calc/calc.go::Add","does":"adds","changed":"clamps"}],
"contract_changes":[{"type":"signature","at":"calc/calc.go:15","text":"New exported const Max"}]}}}`
	bin, argsFile, stdinFile := fakeClaude(t, reply)
	a, _ := testAgent(t, bin)
	res, err := a.Briefing(context.Background(), engine.BriefingRequest{
		PR:    session.PullRequest{Number: 7, Title: "Clamp", BaseSHA: "aaa", HeadSHA: "bbb"},
		Files: []engine.FileSummary{{Path: "calc/calc.go", Status: "modified", Added: 5}}, Methods: []string{"calc/calc.go::Add"}, MaxBudgetUSD: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}
	args := readArgs(t, argsFile)
	if has(args, "--resume") || has(args, "--fork-session") || argAfter(args, "--max-budget-usd")[0] != "0.5000" {
		t.Errorf("briefing args %v", args)
	}
	if stdin, _ := os.ReadFile(stdinFile); !strings.Contains(string(stdin), "git diff aaa bbb") || !strings.Contains(string(stdin), "calc/calc.go::Add") {
		t.Errorf("prompt: %s", stdin)
	}
	n := res.Notes
	if n.TLDR != "Clamps results at Max." || n.Methods["calc/calc.go::Add"].Changed != "clamps" || n.ReadingOrder[0].Summary != "adds clamp" ||
		n.ContractChanges[0].Category != session.CategoryExported || n.ContractChanges[0].Anchors[0].Line != 15 {
		t.Errorf("notes = %+v", n)
	}
}

func TestABudgetStopIsARunErrorWithItsCost(t *testing.T) {
	bin, _, _ := fakeClaude(t, `{"type":"result","subtype":"error_max_budget_usd","is_error":true,"session_id":"x","total_cost_usd":0.37}`)
	a, _ := testAgent(t, bin)
	_, err := a.Ask(context.Background(), engine.QuestionRequest{Kind: session.Trace, MaxBudgetUSD: 0.36, Parent: session.RunInfo{SessionID: "brief"}})
	var re *engine.RunError
	if !errors.As(err, &re) || re.Run.Total != 0.37 || !strings.Contains(err.Error(), "spending cap") {
		t.Fatalf("err = %v", err)
	}
}

func TestRefineAnswersResume(t *testing.T) {
	bin, argsFile, stdinFile := fakeClaude(t, `{"type":"result","subtype":"success","session_id":"r2","total_cost_usd":0.2,"structured_output":{"kind":"revision","gist":"Filled.","revision":{"revised_text":"Rename Max to MaxResult (calc/calc.go:15).","what_changed":"named it"}}}`)
	a, _ := testAgent(t, bin)
	res, err := a.Refine(context.Background(), engine.RefineRequest{Body: "Rename <<which>>", Answers: "the const", Resume: true, Parent: session.RunInfo{SessionID: "r1"}, MaxBudgetUSD: 0.4})
	if err != nil {
		t.Fatal(err)
	}
	args := readArgs(t, argsFile)
	if has(args, "--fork-session") || argAfter(args, "--resume")[0] != "r1" {
		t.Errorf("answers resume the Refine's session: %v", args)
	}
	if stdin, _ := os.ReadFile(stdinFile); !strings.Contains(string(stdin), "the const") || !strings.Contains(string(stdin), "Rename <<which>>") {
		t.Errorf("prompt: %s", stdin)
	}
	if res.Revision.Text == "" || res.Revision.WhatChanged != "named it" {
		t.Errorf("revision = %+v", res.Revision)
	}
}

func TestValidateAgainstTheUnionSchema(t *testing.T) {
	var r result
	json.Unmarshal([]byte(defineReply), &r)
	if err := Validate(r.StructuredOutput); err != nil {
		t.Fatalf("a good Define: %v", err)
	}
	for _, bad := range []string{
		`{"gist":"no kind"}`,
		`{"kind":"nope","gist":"x"}`,
		`{"kind":"define","gist":"` + strings.Repeat("x", 281) + `"}`,
		`{"kind":"define","gist":"x","ran":[{"command":"ls"}]}`,
		`{"kind":"define","gist":"x","probe":{"command":"ls","why":"old"},"define":{}}`,
	} {
		if err := Validate(json.RawMessage(bad)); err == nil {
			t.Errorf("should fail: %s", bad)
		}
	}
}
