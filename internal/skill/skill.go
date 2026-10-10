// Package skill installs the bundled /pull-review Claude Code skill into
// the user's skills directory, so it works in any repo.
package skill

import (
	"errors"
	"os"
	"path/filepath"
)

// Name is the skill's directory name, and its slash command.
const Name = "pull-review"

// Dir is where user-level skills live: dir when it's set (the --dir flag),
// else $CLAUDE_CONFIG_DIR/skills, else ~/.claude/skills.
func Dir(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	if v := os.Getenv("CLAUDE_CONFIG_DIR"); v != "" {
		return filepath.Join(v, "skills"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "skills"), nil
}

// Install writes content as <skillsDir>/pull-review/SKILL.md, replacing
// an older version, and returns the file's path.
func Install(skillsDir, content string) (string, error) {
	if content == "" {
		return "", errors.New("the bundled skill is empty")
	}
	d := filepath.Join(skillsDir, Name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(d, "SKILL.md")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return "", err
	}
	return p, os.Rename(tmp, p)
}
