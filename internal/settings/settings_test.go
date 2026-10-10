package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func write(t *testing.T, dir, name, s string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestNoFileGivesTheDefaults(t *testing.T) {
	s, err := Load(filepath.Join(t.TempDir(), FileName), "git@github.com:acme/calc.git")
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "claude-opus-5-5" || s.Effort != "medium" || s.Briefing != 0.50 || s.Question != 0.25 || s.Path != "" {
		t.Fatalf("defaults = %+v", s)
	}
}

func TestPerRepoOverridesAndUnknownKeys(t *testing.T) {
	dir := t.TempDir()
	asdf := filepath.Join(dir, "work", "asdf-repo")
	os.MkdirAll(asdf, 0o755)
	os.MkdirAll(filepath.Join(dir, "svc", "billing"), 0o755)
	os.MkdirAll(filepath.Join(dir, "svc", "orders"), 0o755)
	sample := write(t, dir, "samples/orders.json", "{}")
	p := write(t, dir, FileName, `
model = "claude-sonnet-5-5"
effort = "medium"
colour = "blue"
sample_data = ["`+filepath.Join(dir, "samples", "*.json")+`"]

[budgets]
briefing = 0.40

[mcp]
allow = ["jira"]

[[related_codebases]]
path = "`+asdf+`"
alias = "ASDF"

[[related_codebases]]
path = "`+filepath.Join(dir, "svc", "*")+`"

[repos."github.com/acme/calc"]
model = "claude-opus-5-5"
effort = "high"
[repos."github.com/acme/calc".budgets]
question = 0.10

[repos."github.com/other/thing"]
model = "nope"
`)
	s, err := Load(p, "https://github.com/acme/calc.git")
	if err != nil {
		t.Fatal(err)
	}
	if s.Model != "claude-opus-5-5" || s.Effort != "high" || s.Briefing != 0.40 || s.Question != 0.10 {
		t.Fatalf("overrides: %+v", s)
	}
	if len(s.MCPAllow) != 1 || s.MCPAllow[0] != "jira" {
		t.Fatalf("mcp: %v", s.MCPAllow)
	}
	if len(s.Related) != 3 || s.Related[0].Alias != "ASDF" || s.Related[1].Alias != "billing" || s.Related[2].Path != filepath.Join(dir, "svc", "orders") {
		t.Fatalf("related: %+v", s.Related)
	}
	if len(s.SampleData) != 1 || s.SampleData[0] != sample {
		t.Fatalf("sample data: %v", s.SampleData)
	}
	if len(s.Warnings) != 1 || !strings.Contains(s.Warnings[0], `"colour"`) {
		t.Fatalf("warnings: %v", s.Warnings)
	}
	home, _ := Load(p, "git@github.com:someone/else.git")
	if home.Model != "claude-sonnet-5-5" || home.Question != 0.25 {
		t.Fatalf("another repo gets the top level: %+v", home)
	}
}

func TestABadFileIsAnError(t *testing.T) {
	p := write(t, t.TempDir(), FileName, "model = \n")
	if _, err := Load(p, ""); err == nil {
		t.Fatal("want a parse error")
	}
}

func TestNormalizeRemote(t *testing.T) {
	for _, r := range []string{"git@github.com:Acme/calc.git", "https://github.com/acme/calc", "ssh://git@github.com/acme/calc.git", "github.com/acme/calc"} {
		if got := NormalizeRemote(r); got != "github.com/acme/calc" {
			t.Errorf("%s → %s", r, got)
		}
	}
}

func TestMCPServersFromClaudeConfig(t *testing.T) {
	dir := t.TempDir()
	checkout := filepath.Join(dir, "repo")
	write(t, checkout, ".mcp.json", `{"mcpServers":{"github":{"command":"gh-mcp"}}}`)
	cj := write(t, dir, ".claude.json", `{"mcpServers":{"jira":{"command":"jira-mcp"},"slack":{"command":"x"}},
"projects":{"`+checkout+`":{"mcpServers":{"confluence":{"type":"http","url":"https://c"}}}}}`)
	got, warnings := MCPServers([]string{"jira", "github", "confluence", "missing"}, checkout, cj)
	if len(got) != 3 || got["slack"] != nil {
		t.Fatalf("servers: %v", got)
	}
	var def map[string]string
	json.Unmarshal(got["confluence"], &def)
	if def["url"] != "https://c" {
		t.Fatalf("confluence: %s", got["confluence"])
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "missing") {
		t.Fatalf("warnings: %v", warnings)
	}
}
