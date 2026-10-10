// Package settings reads the user settings file: TOML, at
// <config dir>/settings.toml (~/.config/pr-review/settings.toml; the
// directory is $PR_REVIEW_CONFIG_DIR when set). Per-repo overrides sit
// under [repos."<remote>"], keyed by the repo's remote (for example
// "github.com/acme/calc"). Unknown keys are reported, never fatal.
//
//	model = "claude-sonnet-5-5"
//	effort = "medium"
//	sample_data = ["~/work/samples/orders.json"]
//
//	[budgets]
//	briefing = 0.50
//	question = 0.25
//
//	[mcp]
//	allow = ["jira", "confluence"]
//
//	[[related_codebases]]
//	path = "~/work/asdf-repo"   # a directory, or a glob of them
//	alias = "ASDF"              # default: the directory's name
//
//	[repos."github.com/acme/calc"]
//	model = "claude-opus-5-5"
package settings

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
)

// FileName is the settings file's name in the config directory.
const FileName = "settings.toml"

// The spec's defaults: Opus 5.5 at medium, $0.50 a Briefing, $0.25 a
// Question or Refine.
const (
	DefaultModel    = "claude-opus-5-5"
	DefaultEffort   = "medium"
	DefaultBriefing = 0.50
	DefaultQuestion = 0.25
)

type Budgets struct {
	Briefing *float64 `toml:"briefing"`
	Question *float64 `toml:"question"`
}

type MCP struct {
	Allow []string `toml:"allow"`
}

type RelatedCodebase struct {
	Path  string `toml:"path"`
	Alias string `toml:"alias"`
}

// Layer is one level of settings: the file's top level, or one repo's
// overrides. Unset fields don't override.
type Layer struct {
	Model            string            `toml:"model"`
	Effort           string            `toml:"effort"`
	Budgets          Budgets           `toml:"budgets"`
	MCP              MCP               `toml:"mcp"`
	RelatedCodebases []RelatedCodebase `toml:"related_codebases"`
	SampleData       []string          `toml:"sample_data"`
}

type file struct {
	Layer
	Repos map[string]Layer `toml:"repos"`
}

// Related is a resolved Related Codebase.
type Related struct {
	Alias string
	Path  string
}

// Settings are the effective settings for one repo.
type Settings struct {
	Model      string
	Effort     string
	Briefing   float64 // budget, USD
	Question   float64 // budget, USD (a Question or a Refine)
	MCPAllow   []string
	Related    []Related
	SampleData []string
	// Path is the file read ("" when there was none).
	Path string
	// Warnings are unknown keys and other problems worth telling the
	// Reviewer about.
	Warnings []string
}

// Defaults are the settings with no file.
func Defaults() Settings {
	return Settings{Model: DefaultModel, Effort: DefaultEffort, Briefing: DefaultBriefing, Question: DefaultQuestion}
}

// Load reads path and resolves it for the repo whose remote is remote
// (any form: https://github.com/a/b.git, git@github.com:a/b). A missing
// file gives the defaults.
func Load(path, remote string) (Settings, error) {
	s := Defaults()
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.Path = path
	var f file
	md, err := toml.Decode(string(b), &f)
	if err != nil {
		return s, fmt.Errorf("%s: %w", path, err)
	}
	for _, k := range md.Undecoded() {
		s.Warnings = append(s.Warnings, fmt.Sprintf("%s: unknown key %q (ignored)", filepath.Base(path), k.String()))
	}
	s.apply(f.Layer)
	want := NormalizeRemote(remote)
	keys := make([]string, 0, len(f.Repos))
	for k := range f.Repos {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if want != "" && NormalizeRemote(k) == want {
			s.apply(f.Repos[k])
		}
	}
	return s, nil
}

func (s *Settings) apply(l Layer) {
	if l.Model != "" {
		s.Model = l.Model
	}
	if l.Effort != "" {
		switch l.Effort {
		case "low", "medium", "high", "xhigh", "max":
			s.Effort = l.Effort
		default:
			s.Warnings = append(s.Warnings, fmt.Sprintf("effort %q isn't low, medium, high, xhigh or max (ignored)", l.Effort))
		}
	}
	if l.Budgets.Briefing != nil {
		s.Briefing = *l.Budgets.Briefing
	}
	if l.Budgets.Question != nil {
		s.Question = *l.Budgets.Question
	}
	if l.MCP.Allow != nil {
		s.MCPAllow = l.MCP.Allow
	}
	if l.RelatedCodebases != nil {
		s.Related = nil
		for _, rc := range l.RelatedCodebases {
			s.Related = append(s.Related, s.resolveRelated(rc)...)
		}
	}
	if l.SampleData != nil {
		s.SampleData = nil
		for _, p := range l.SampleData {
			matches, err := filepath.Glob(expand(p))
			if err != nil || len(matches) == 0 {
				s.Warnings = append(s.Warnings, fmt.Sprintf("sample_data %q matches no file", p))
				continue
			}
			s.SampleData = append(s.SampleData, matches...)
		}
	}
}

// resolveRelated expands ~ and globs; each match is a Related Codebase,
// its alias the one given (for a single match) or the directory's name.
func (s *Settings) resolveRelated(rc RelatedCodebase) []Related {
	matches, err := filepath.Glob(expand(rc.Path))
	if err != nil || len(matches) == 0 {
		s.Warnings = append(s.Warnings, fmt.Sprintf("related_codebases %q matches no directory", rc.Path))
		return nil
	}
	var out []Related
	for _, m := range matches {
		if fi, err := os.Stat(m); err != nil || !fi.IsDir() {
			continue
		}
		alias := rc.Alias
		if alias == "" || len(matches) > 1 {
			alias = filepath.Base(m)
		}
		out = append(out, Related{Alias: alias, Path: m})
	}
	return out
}

func expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(p, "~"))
		}
	}
	return p
}

// NormalizeRemote turns a git remote URL into host/owner/repo:
// "git@github.com:acme/calc.git" and "https://github.com/acme/calc" are
// both "github.com/acme/calc".
func NormalizeRemote(r string) string {
	r = strings.TrimSpace(r)
	r = strings.TrimSuffix(strings.TrimSuffix(r, "/"), ".git")
	for _, p := range []string{"https://", "http://", "ssh://", "git://"} {
		r = strings.TrimPrefix(r, p)
	}
	if at := strings.Index(r, "@"); at >= 0 {
		r = r[at+1:]
	}
	if i := strings.Index(r, ":"); i >= 0 && !strings.Contains(r[:i], "/") {
		r = r[:i] + "/" + r[i+1:]
	}
	return strings.ToLower(r)
}
