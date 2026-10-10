package settings

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// MCPServers finds the allowlisted servers' definitions where Claude Code
// keeps them: the repo's .mcp.json, then ~/.claude.json (the project's
// entry, then the user's). Names found nowhere are reported.
func MCPServers(allow []string, checkout, claudeJSON string) (map[string]json.RawMessage, []string) {
	if len(allow) == 0 {
		return nil, nil
	}
	var sources []map[string]json.RawMessage
	read := func(path string, pick func(map[string]json.RawMessage) map[string]json.RawMessage) {
		b, err := os.ReadFile(path)
		if err != nil {
			return
		}
		var top map[string]json.RawMessage
		if json.Unmarshal(b, &top) != nil {
			return
		}
		if m := pick(top); m != nil {
			sources = append(sources, m)
		}
	}
	servers := func(raw json.RawMessage) map[string]json.RawMessage {
		var m map[string]json.RawMessage
		if json.Unmarshal(raw, &m) != nil {
			return nil
		}
		return m
	}
	read(filepath.Join(checkout, ".mcp.json"), func(top map[string]json.RawMessage) map[string]json.RawMessage {
		return servers(top["mcpServers"])
	})
	if claudeJSON == "" {
		if home, err := os.UserHomeDir(); err == nil {
			claudeJSON = filepath.Join(home, ".claude.json")
		}
	}
	read(claudeJSON, func(top map[string]json.RawMessage) map[string]json.RawMessage {
		var projects map[string]struct {
			MCPServers map[string]json.RawMessage `json:"mcpServers"`
		}
		json.Unmarshal(top["projects"], &projects)
		return projects[checkout].MCPServers
	})
	read(claudeJSON, func(top map[string]json.RawMessage) map[string]json.RawMessage {
		return servers(top["mcpServers"])
	})
	out := map[string]json.RawMessage{}
	var warnings []string
	for _, name := range allow {
		found := false
		for _, src := range sources {
			if def, ok := src[name]; ok {
				out[name], found = def, true
				break
			}
		}
		if !found {
			warnings = append(warnings, fmt.Sprintf("mcp.allow: no MCP server %q in .mcp.json or ~/.claude.json (skipped)", name))
		}
	}
	return out, warnings
}
