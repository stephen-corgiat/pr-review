// Package claudeagent is the live Agent: `claude -p` in the Review
// Session's worktree, in auto mode, with the repo write-denied and a
// writable scratch directory (ADR 0004; spec: Running the agent). The
// Briefing runs once; Questions and Refine fork from it with --resume
// --fork-session; follow-ups resume their Thread's own fork.
package claudeagent

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

//go:embed system.md
var systemPrompt string

//go:embed union.json
var unionSchema string

// Schema is the union JSON schema every call answers with.
func Schema() string { return unionSchema }

// Related is a Related Codebase: another checkout the agent may read.
type Related struct {
	Alias string
	Path  string
}

// Config is one Review Session's agent setup.
type Config struct {
	Binary     string // "claude" when empty
	Worktree   string // the working directory (write-denied)
	Scratch    string // writable
	Model      string
	Effort     string
	Related    []Related
	SampleData []string // files the agent reads first for sample values
	// MCPServers are the allowlisted servers' definitions (name → the
	// mcpServers entry), written to an --mcp-config file.
	MCPServers map[string]json.RawMessage
	// Env is extra environment for the CLI.
	Env []string
}

// Agent runs `claude -p`.
type Agent struct {
	cfg      Config
	system   string
	settings string
	mcpFile  string
}

// New prepares the agent: the system prompt with this session's context,
// the permission settings, and the MCP config file (in the scratch
// directory's parent, never in the repo).
func New(cfg Config) (*Agent, error) {
	if cfg.Binary == "" {
		cfg.Binary = "claude"
	}
	if cfg.Worktree == "" || cfg.Scratch == "" {
		return nil, errors.New("claudeagent: the worktree and scratch directories are required")
	}
	if err := os.MkdirAll(cfg.Scratch, 0o755); err != nil {
		return nil, err
	}
	a := &Agent{cfg: cfg}
	a.system = strings.NewReplacer("{{SCRATCH}}", cfg.Scratch, "{{CONTEXT}}", contextSection(cfg)).Replace(systemPrompt)
	s, err := json.Marshal(Settings(cfg))
	if err != nil {
		return nil, err
	}
	a.settings = string(s)
	if len(cfg.MCPServers) > 0 {
		b, err := json.MarshalIndent(map[string]any{"mcpServers": cfg.MCPServers}, "", "  ")
		if err != nil {
			return nil, err
		}
		a.mcpFile = filepath.Join(filepath.Dir(cfg.Scratch), "mcp.json")
		if err := os.WriteFile(a.mcpFile, b, 0o600); err != nil {
			return nil, err
		}
	}
	return a, nil
}

// contextSection tells the agent about this Review Session's sample data
// and Related Codebases.
func contextSection(cfg Config) string {
	var b strings.Builder
	b.WriteString("# Sample data sources\n\n")
	if len(cfg.SampleData) == 0 {
		b.WriteString("None configured: use the repo's specs and fixtures, then invent (marked `invented`).\n")
	} else {
		b.WriteString("Read these on demand, before the repo's specs and fixtures, and mark values from them `fixture` with `source_ref`:\n\n")
		for _, f := range cfg.SampleData {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
	}
	b.WriteString("\n# Related Codebases\n\n")
	if len(cfg.Related) == 0 {
		b.WriteString("None.\n")
	} else {
		b.WriteString("Other codebases you may read (never edit), as they are on disk. Cite them as `ALIAS:path:line`.\n\n")
		for _, r := range cfg.Related {
			fmt.Fprintf(&b, "- `%s` → `%s`\n", r.Alias, r.Path)
		}
	}
	return b.String()
}

// abs is the permission-rule form of an absolute path ("//abs/path").
func abs(p string) string { return "/" + filepath.Clean(p) }

// Settings is the --settings JSON: hooks off, the worktree and Related
// Codebases write-denied, the scratch directory writable, reads scoped.
func Settings(cfg Config) map[string]any {
	var allow, deny []string
	readable := []string{cfg.Worktree, cfg.Scratch}
	for _, r := range cfg.Related {
		readable = append(readable, r.Path)
	}
	for _, d := range readable {
		for _, tool := range []string{"Read", "Grep", "Glob"} {
			allow = append(allow, fmt.Sprintf("%s(%s/**)", tool, abs(d)))
		}
	}
	for _, f := range cfg.SampleData {
		allow = append(allow, fmt.Sprintf("Read(%s)", abs(f)))
	}
	allow = append(allow, fmt.Sprintf("Write(%s/**)", abs(cfg.Scratch)), fmt.Sprintf("Edit(%s/**)", abs(cfg.Scratch)))
	denied := []string{cfg.Worktree}
	for _, r := range cfg.Related {
		denied = append(denied, r.Path)
	}
	for _, d := range denied {
		deny = append(deny, fmt.Sprintf("Edit(%s/**)", abs(d)), fmt.Sprintf("Write(%s/**)", abs(d)))
	}
	return map[string]any{
		"disableAllHooks": true,
		"permissions":     map[string]any{"allow": allow, "deny": deny},
	}
}

// Args is the command line for one call (without the prompt, which goes
// on stdin). resume is the parent session, fork forks it.
func (a *Agent) Args(maxBudget float64, resume string, fork bool) []string {
	args := []string{
		"-p", "--output-format", "json",
		"--permission-mode", "auto",
		"--setting-sources", "project",
		"--settings", a.settings,
		"--strict-mcp-config",
		"--disable-slash-commands",
		"--tools", "Read,Grep,Glob,Bash,Write",
		"--append-system-prompt", a.system,
		"--json-schema", unionSchema,
		"--max-budget-usd", fmt.Sprintf("%.4f", maxBudget),
	}
	if a.cfg.Model != "" {
		args = append(args, "--model", a.cfg.Model)
	}
	if a.cfg.Effort != "" {
		args = append(args, "--effort", a.cfg.Effort)
	}
	if a.mcpFile != "" {
		args = append(args, "--mcp-config", a.mcpFile)
	}
	// --add-dir isn't restored on resume: pass it on every call.
	args = append(args, "--add-dir", a.cfg.Scratch)
	for _, r := range a.cfg.Related {
		args = append(args, "--add-dir", r.Path)
	}
	if resume != "" {
		args = append(args, "--resume", resume)
		if fork {
			args = append(args, "--fork-session")
		}
	}
	return args
}

// result is the CLI's --output-format json result.
type result struct {
	Type              string          `json:"type"`
	Subtype           string          `json:"subtype"`
	IsError           bool            `json:"is_error"`
	Result            string          `json:"result"`
	SessionID         string          `json:"session_id"`
	TotalCostUSD      float64         `json:"total_cost_usd"`
	DurationMS        int64           `json:"duration_ms"`
	StructuredOutput  json.RawMessage `json:"structured_output"`
	ModelUsage        map[string]any  `json:"modelUsage"`
	PermissionDenials []struct {
		ToolName  string          `json:"tool_name"`
		ToolInput json.RawMessage `json:"tool_input"`
	} `json:"permission_denials"`
}

// run calls the CLI and returns its structured output.
func (a *Agent) run(ctx context.Context, prompt string, maxBudget float64, resume string, fork bool) (json.RawMessage, session.RunInfo, error) {
	cmd := exec.CommandContext(ctx, a.cfg.Binary, a.Args(maxBudget, resume, fork)...)
	cmd.Dir = a.cfg.Worktree
	cmd.Stdin = strings.NewReader(prompt)
	cmd.Env = append(cleanEnv(os.Environ()), a.cfg.Env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	start := time.Now()
	err := cmd.Run()
	var r result
	if jerr := json.Unmarshal(out.Bytes(), &r); jerr != nil {
		msg := strings.TrimSpace(errb.String())
		if msg == "" {
			msg = strings.TrimSpace(out.String())
		}
		if err == nil {
			err = jerr
		}
		return nil, session.RunInfo{}, fmt.Errorf("claude -p: %v: %s", err, firstLine(msg))
	}
	info := session.RunInfo{SessionID: r.SessionID, Total: r.TotalCostUSD, DurationMS: r.DurationMS, Model: modelOf(r.ModelUsage)}
	if info.DurationMS == 0 {
		info.DurationMS = time.Since(start).Milliseconds()
	}
	switch {
	case r.IsError || strings.HasPrefix(r.Subtype, "error"):
		what := r.Subtype
		if r.Result != "" {
			what += ": " + r.Result
		}
		if strings.Contains(r.Subtype, "budget") {
			what = fmt.Sprintf("stopped at its spending cap ($%.2f)", maxBudget)
		}
		return nil, info, &engine.RunError{Err: errors.New("claude -p " + what), Run: info}
	case len(r.StructuredOutput) == 0 || string(r.StructuredOutput) == "null":
		return nil, info, &engine.RunError{Err: fmt.Errorf("claude -p gave no structured output: %s", firstLine(r.Result)), Run: info}
	}
	return r.StructuredOutput, info, nil
}

// cleanEnv drops ANTHROPIC_API_KEY so the CLI uses the Reviewer's
// subscription login.
func cleanEnv(env []string) []string {
	var out []string
	for _, e := range env {
		if !strings.HasPrefix(e, "ANTHROPIC_API_KEY=") {
			out = append(out, e)
		}
	}
	return out
}

func modelOf(mu map[string]any) string {
	var ks []string
	for k := range mu {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return strings.Join(ks, ",")
}

func firstLine(s string) string {
	l, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	if len(l) > 300 {
		l = l[:300] + "…"
	}
	return l
}

// ---- the four calls ----

func (a *Agent) Briefing(ctx context.Context, req engine.BriefingRequest) (engine.BriefingResult, error) {
	raw, run, err := a.run(ctx, BriefingPrompt(req), req.MaxBudgetUSD, "", false)
	if err != nil {
		return engine.BriefingResult{}, err
	}
	o, err := decode(raw)
	if err != nil {
		return engine.BriefingResult{}, &engine.RunError{Err: err, Run: run}
	}
	return engine.BriefingResult{Notes: o.briefingNotes(), Run: run}, nil
}

func (a *Agent) Ask(ctx context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	return a.question(ctx, req, req.Parent.SessionID != "")
}

func (a *Agent) Continue(ctx context.Context, req engine.QuestionRequest) (engine.AnswerResult, error) {
	return a.question(ctx, req, false)
}

func (a *Agent) question(ctx context.Context, req engine.QuestionRequest, fork bool) (engine.AnswerResult, error) {
	prompt := QuestionPrompt(req, !fork && req.Parent.SessionID != "")
	raw, run, err := a.run(ctx, prompt, req.MaxBudgetUSD, req.Parent.SessionID, fork)
	if err != nil {
		return engine.AnswerResult{}, err
	}
	o, err := decode(raw)
	if err != nil {
		return engine.AnswerResult{}, &engine.RunError{Err: err, Run: run}
	}
	return engine.AnswerResult{Answer: o.answer(req.Kind), Run: run}, nil
}

func (a *Agent) Refine(ctx context.Context, req engine.RefineRequest) (engine.RevisionResult, error) {
	raw, run, err := a.run(ctx, RefinePrompt(req), req.MaxBudgetUSD, req.Parent.SessionID, !req.Resume && req.Parent.SessionID != "")
	if err != nil {
		return engine.RevisionResult{}, err
	}
	o, err := decode(raw)
	if err != nil {
		return engine.RevisionResult{}, &engine.RunError{Err: err, Run: run}
	}
	return engine.RevisionResult{Revision: o.revision(), Run: run}, nil
}
