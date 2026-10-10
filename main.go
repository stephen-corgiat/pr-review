// Command pr-review is the Review Session TUI: the PR Review
// Assistant. See GLOSSARY.md for the vocabulary.
package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/claudeagent"
	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/harness"
	"github.com/stephen-corgiat/pr-review/internal/highlight"
	"github.com/stephen-corgiat/pr-review/internal/paths"
	"github.com/stephen-corgiat/pr-review/internal/replayagent"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/settings"
	"github.com/stephen-corgiat/pr-review/internal/shellcomp"
	"github.com/stephen-corgiat/pr-review/internal/skill"
	"github.com/stephen-corgiat/pr-review/internal/ui"
)

const usage = `pr-review: the PR Review Assistant.

  pr-review [flags]            in a checkout: open the current branch's Pull Request
  pr-review [flags] <number>   open Pull Request <number> of the current repo
  pr-review [flags] <link>     open a PR by its link (https://github.com/owner/repo/pull/123,
                              or owner/repo#123), from any directory
  pr-review prune              remove the worktrees of finished Review Sessions
  pr-review export [--again]   (in the PR's checkout) print queued Feedback for the Implementing Agent
  pr-review resolve <id> addressed|declined|needs-you "<note>" [--commit <sha>]
  pr-review skill install [--dir DIR]   install /pull-review into your Claude skills
  pr-review completion bash|zsh|fish    print a shell completion script (see the README)
  pr-review -repo DIR -base REF [-head REF]   a local base..head (developer aid)

Flags:
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "export":
			exportMain(os.Args[2:])
			return
		case "resolve":
			resolveMain(os.Args[2:])
			return
		case "skill":
			skillMain(os.Args[2:])
			return
		case "prune":
			pruneMain()
			return
		case "completion":
			completionMain(os.Args[2:])
			return
		case "__complete":
			completeMain(os.Args[2:])
			return
		}
	}
	fs := flag.NewFlagSet("pr-review", flag.ExitOnError)
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fs.PrintDefaults()
	}
	repoFlag := fs.String("repo", "", "open a local repo's base..head (developer aid)")
	baseFlag := fs.String("base", "", "base ref (-repo mode)")
	headFlag := fs.String("head", "HEAD", "head ref (-repo mode)")
	styleFlag := fs.String("style", "", "override the chroma highlighting style")
	snapshot := fs.Bool("snapshot", false, "print one frame with no TTY, then exit")
	size := fs.String("size", "120x40", "WxH for -snapshot")
	events := fs.String("events", "", "event spec for -snapshot (see internal/harness)")
	wait := fs.String("wait", "0s", "settle duration for -snapshot")
	// Flags may come before or after the PR number.
	positional := parseAnywhere(fs, os.Args[1:])

	highlight.Setup(*styleFlag)
	var eng *engine.Engine
	var notes []string
	var err error
	repoMode := *repoFlag != ""
	switch {
	case repoMode:
		if *baseFlag == "" {
			fatal(fmt.Errorf("-repo requires -base"))
		}
		eng, notes, err = app.Repo(*repoFlag, *baseFlag, *headFlag, nil)
	default:
		var target app.Target
		if len(positional) > 1 {
			fatal(fmt.Errorf("one PR at a time: got %s", strings.Join(positional, " ")))
		}
		if len(positional) > 0 {
			if target, err = app.ParseTarget(positional[0]); err != nil {
				fatal(err)
			}
		}
		eng, notes, err = openPR(target)
	}
	if err != nil {
		fatal(err)
	}
	defer eng.Close()
	for _, n := range notes {
		fmt.Fprintln(os.Stderr, "note:", n)
	}

	m := ui.New(eng, repoMode)
	var shown []string
	for _, n := range notes {
		if !strings.HasPrefix(n, "settings: ") {
			shown = append(shown, n)
		}
	}
	m.Note(strings.Join(shown, " · "))

	if *snapshot {
		w, h, err := parseSize(*size)
		if err != nil {
			fatal(err)
		}
		evs, err := harness.ParseEvents(*events)
		if err != nil {
			fatal(fmt.Errorf("bad -events: %w", err))
		}
		settle, err := time.ParseDuration(*wait)
		if err != nil {
			fatal(fmt.Errorf("bad -wait: %w", err))
		}
		_, out := harness.Run(m, w, h, evs, settle)
		fmt.Println(out)
		return
	}

	p := tea.NewProgram(m)
	if _, err := p.Run(); err != nil {
		fatal(err)
	}
}

// openPR opens a real PR: from the checkout in the current directory, or,
// for a PR link, from a checkout of the link's repo (app.Checkouts).
func openPR(target app.Target) (*engine.Engine, []string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return nil, nil, err
	}
	dataDir, err := paths.DataDir()
	if err != nil {
		return nil, nil, err
	}
	cfgDir, err := paths.ConfigDir()
	if err != nil {
		return nil, nil, err
	}
	logLine := func(s string) { fmt.Fprintln(os.Stderr, s) }
	checkout := cwd
	if target.OwnerRepo != "" {
		c := app.Checkouts{Cwd: cwd, DataDir: dataDir, Log: logLine}
		if checkout, err = c.For(context.Background(), target.OwnerRepo, target.Number); err != nil {
			return nil, nil, err
		}
	}
	remote, _ := app.Git(context.Background(), checkout, "remote", "get-url", "origin")
	set, err := settings.Load(filepath.Join(cfgDir, settings.FileName), remote)
	if err != nil {
		return nil, nil, err
	}
	notes := append([]string(nil), set.Warnings...)
	var fg forge.Forge = forge.GH{Dir: checkout, Repo: target.OwnerRepo}
	if dir, record, ok := replayagent.Parse(os.Getenv("PR_REVIEW_REPLAY")); ok {
		// The README demo (demo/): a merged PR re-enacted as open, with
		// nothing ever posted to GitHub.
		fg = replayagent.Forge{Inner: fg, Dir: dir, Record: record}
	}
	o := app.PR{
		Checkout: checkout, Number: target.Number, Forge: fg, DataDir: dataDir,
		Model: set.Model, Effort: set.Effort,
		Budgets: engine.Budgets{Briefing: set.Briefing, Question: set.Question},
		Log:     logLine,
		Callers: engine.GoCallers,
		Agent: func(dir paths.Session, op session.Opened) (engine.Agent, error) {
			mcp, warn := settings.MCPServers(set.MCPAllow, op.Checkout, "")
			notes = append(notes, warn...)
			var related []claudeagent.Related
			for _, r := range set.Related {
				related = append(related, claudeagent.Related{Alias: r.Alias, Path: r.Path})
			}
			if replayDir, record, ok := replayagent.Parse(os.Getenv("PR_REVIEW_REPLAY")); ok {
				// The README demo (demo/): replay what an earlier run recorded.
				ra := &replayagent.Agent{Dir: replayDir, Record: record, Delay: 2 * time.Second}
				if !record {
					return ra, nil
				}
				inner, err := newClaude(set, op, mcp, related)
				ra.Inner = inner
				return ra, err
			}
			return newClaude(set, op, mcp, related)
		},
	}
	eng, res, more, err := o.Open(context.Background())
	if err != nil {
		return nil, nil, err
	}
	notes = append(notes, more...)
	if set.Path != "" {
		notes = append(notes, "settings: "+set.Path)
	}
	if res.Finished != "" {
		notes = append(notes, fmt.Sprintf("PR #%d is %s: this Review Session is read-only (no agent, worktree removed)", eng.State().PR.Number, res.Finished))
	}
	return eng, notes, nil
}

func newClaude(set settings.Settings, op session.Opened, mcp map[string]json.RawMessage, related []claudeagent.Related) (engine.Agent, error) {
	return claudeagent.New(claudeagent.Config{
		Worktree: op.Worktree, Scratch: op.Scratch, Model: op.Model, Effort: op.Effort,
		Related: related, SampleData: set.SampleData, MCPServers: mcp,
	})
}

func pruneMain() {
	dataDir, err := paths.DataDir()
	if err != nil {
		fatal(err)
	}
	removed, err := app.Prune(context.Background(), dataDir, func(checkout, ownerRepo string) forge.Forge {
		return forge.GH{Dir: checkout, Repo: ownerRepo}
	})
	for _, r := range removed {
		fmt.Println("removed the worktree of", r)
	}
	if err != nil {
		fatal(err)
	}
	if len(removed) == 0 {
		fmt.Println("nothing to prune: every Review Session with a worktree has an open PR")
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

func parseSize(s string) (int, int, error) {
	var w, h int
	_, err := fmt.Sscanf(s, "%dx%d", &w, &h)
	if err != nil {
		return 0, 0, fmt.Errorf("bad -size %q (want WxH)", s)
	}
	return w, h, nil
}

// pullReviewSkill is the bundled /pull-review skill (`pr-review skill
// install`).
//
//go:embed skills/pull-review/SKILL.md
var pullReviewSkill string

// parseAnywhere parses flags that may come before or after positional
// arguments, and returns the positional ones.
func parseAnywhere(fs *flag.FlagSet, args []string) []string {
	fs.Parse(args)
	var positional []string
	for fs.NArg() > 0 {
		positional = append(positional, fs.Arg(0))
		fs.Parse(fs.Args()[1:])
	}
	return positional
}

// here is the current directory, the data dir and the Forge for it.
func here() (string, string, forge.Forge) {
	cwd, err := os.Getwd()
	if err != nil {
		fatal(err)
	}
	dataDir, err := paths.DataDir()
	if err != nil {
		fatal(err)
	}
	var f forge.Forge = forge.GH{Dir: cwd}
	if dir, record, ok := replayagent.Parse(os.Getenv("PR_REVIEW_REPLAY")); ok {
		// The hand-off demo (demo/handoff.tape): export and resolve on the
		// recorded PR, re-enacted as open.
		f = replayagent.Forge{Inner: f, Dir: dir, Record: record}
	}
	return cwd, dataDir, f
}

// refused prints why export or resolve didn't run and exits 1.
func refused(cmd string, err error) {
	var r *app.Refusal
	if errors.As(err, &r) {
		fmt.Fprintf(os.Stderr, "pr-review %s: refused: %s\n", cmd, r.Reason)
	} else {
		fmt.Fprintf(os.Stderr, "pr-review %s: %v\n", cmd, err)
	}
	os.Exit(1)
}

// exportMain is `pr-review export [--again]`: in a checkout of the PR's
// repo on the PR's branch, it prints the queued Feedback and the research
// (ADR 0003's format) and marks those items handed off.
func exportMain(args []string) {
	fs := flag.NewFlagSet("export", flag.ExitOnError)
	again := fs.Bool("again", false, "also hand off again the items already handed off (not done)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: pr-review export [--again]   (in a checkout of the PR's repo, on its branch)")
		fs.PrintDefaults()
	}
	if extra := parseAnywhere(fs, args); len(extra) > 0 {
		fs.Usage()
		os.Exit(2)
	}
	cwd, dataDir, f := here()
	res, err := app.Export{Dir: cwd, Forge: f, DataDir: dataDir, Again: *again}.Run(context.Background())
	if errors.Is(err, app.ErrNothingQueued) {
		fmt.Println("Nothing queued: " + err.Error())
		return
	}
	if err != nil {
		refused("export", err)
	}
	os.Stdout.WriteString(res.Markdown)
	fmt.Fprintf(os.Stderr, "pr-review export: handed off %s from %s #%d's Review Session\n", strings.Join(res.IDs, ", "), res.Where.PR.OwnerRepo, res.Where.PR.Number)
}

// resolveMain is `pr-review resolve <id> addressed|declined|needs-you
// "<note>" [--commit <sha>]`.
func resolveMain(args []string) {
	fs := flag.NewFlagSet("resolve", flag.ExitOnError)
	commit := fs.String("commit", "", "the commit that addressed it (optional)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, `usage: pr-review resolve <id> addressed|declined|needs-you "<note>" [--commit <sha>]`)
		fs.PrintDefaults()
	}
	pos := parseAnywhere(fs, args)
	if len(pos) < 2 || len(pos) > 3 {
		fs.Usage()
		os.Exit(2)
	}
	note := ""
	if len(pos) == 3 {
		note = pos[2]
	}
	cwd, dataDir, f := here()
	r := app.Resolve{Dir: cwd, Forge: f, DataDir: dataDir,
		RecordOutcome: engine.RecordOutcome{ID: strings.ToUpper(pos[0]), Outcome: strings.ToLower(pos[1]), Note: note, Commit: *commit}}
	loc, err := r.Run(context.Background())
	if err != nil {
		refused("resolve", err)
	}
	fmt.Printf("%s: %s, recorded in %s #%d's Review Session\n", r.ID, r.Outcome, loc.PR.OwnerRepo, loc.PR.Number)
}

// skillMain is `pr-review skill install [--dir DIR]`.
func skillMain(args []string) {
	fs := flag.NewFlagSet("skill", flag.ExitOnError)
	dir := fs.String("dir", "", "the skills directory (default $CLAUDE_CONFIG_DIR/skills, else ~/.claude/skills)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: pr-review skill install [--dir DIR]")
		fs.PrintDefaults()
	}
	pos := parseAnywhere(fs, args)
	if len(pos) != 1 || pos[0] != "install" {
		fs.Usage()
		os.Exit(2)
	}
	d, err := skill.Dir(*dir)
	if err != nil {
		fatal(err)
	}
	p, err := skill.Install(d, pullReviewSkill)
	if err != nil {
		fatal(err)
	}
	fmt.Println("installed /pull-review at", p)
	if _, err := exec.LookPath("pr-review"); err != nil {
		fmt.Println("note: `pr-review` isn't on your PATH, and /pull-review runs it: `go install .` in a checkout of the pr-review repo (and put $(go env GOPATH)/bin on your PATH)")
	}
}

// completionMain is `pr-review completion bash|zsh|fish`.
func completionMain(args []string) {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: pr-review completion bash|zsh|fish")
		os.Exit(2)
	}
	script, err := shellcomp.Script(args[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, "pr-review completion:", err)
		os.Exit(2)
	}
	fmt.Print(script)
}

// completeMain is the hidden `pr-review __complete <words…>`. It never
// fails loudly: on any error it prints no candidates, and a directive that
// says not to fall back to files.
func completeMain(words []string) {
	out := app.Completion{Directive: app.DirNoFileComp}
	if cwd, err := os.Getwd(); err == nil {
		if dataDir, err := paths.DataDir(); err == nil {
			out = app.Completer{Dir: cwd, DataDir: dataDir, Forge: forge.GH{Dir: cwd}}.Complete(context.Background(), words)
		}
	}
	fmt.Print(out)
}
