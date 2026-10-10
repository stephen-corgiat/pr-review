// Package paths is where pr-review keeps things: the user data directory
// (Review Sessions: event logs, worktrees, scratch directories) and the
// config directory (the settings file). Never inside a repo.
package paths

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DataDir is $PR_REVIEW_DATA_DIR, else $XDG_DATA_HOME/pr-review, else
// ~/.local/share/pr-review (Linux) or ~/Library/Application Support/pr-review
// (macOS).
func DataDir() (string, error) {
	if v := os.Getenv("PR_REVIEW_DATA_DIR"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_DATA_HOME"); v != "" {
		return filepath.Join(v, "pr-review"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library", "Application Support", "pr-review"), nil
	}
	return filepath.Join(home, ".local", "share", "pr-review"), nil
}

// ConfigDir is $PR_REVIEW_CONFIG_DIR, else $XDG_CONFIG_HOME/pr-review, else
// ~/.config/pr-review (on macOS too: a dotfile a CLI user expects).
func ConfigDir() (string, error) {
	if v := os.Getenv("PR_REVIEW_CONFIG_DIR"); v != "" {
		return v, nil
	}
	if v := os.Getenv("XDG_CONFIG_HOME"); v != "" {
		return filepath.Join(v, "pr-review"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "pr-review"), nil
}

// Session is one Review Session's directory: the event log, the worktree
// and the scratch directory live in it.
type Session string

// SessionDir is <data>/reviews/<owner>/<repo>/<number>.
func SessionDir(dataDir, ownerRepo string, number int) Session {
	parts := strings.SplitN(ownerRepo, "/", 2)
	if len(parts) != 2 {
		parts = []string{"local", ownerRepo}
	}
	return Session(filepath.Join(dataDir, "reviews", safe(parts[0]), safe(parts[1]), fmt.Sprint(number)))
}

// CloneDir is <data>/clones/<owner>/<repo>: pr-review's own clone of a
// repo, for a PR opened by link from outside a checkout of it.
func CloneDir(dataDir, ownerRepo string) string {
	parts := strings.SplitN(ownerRepo, "/", 2)
	if len(parts) != 2 {
		parts = []string{"local", ownerRepo}
	}
	return filepath.Join(dataDir, "clones", safe(parts[0]), safe(parts[1]))
}

// FixtureSessionDir is fixture mode's own directory, apart from real ones.
func FixtureSessionDir(dataDir, ownerRepo string, number int) Session {
	return SessionDir(filepath.Join(dataDir, "fixtures"), ownerRepo, number)
}

func safe(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, s)
	if s == "" || s == "." || s == ".." {
		return "_"
	}
	return s
}

func (s Session) Dir() string      { return string(s) }
func (s Session) Worktree() string { return filepath.Join(string(s), "worktree") }
func (s Session) Scratch() string  { return filepath.Join(string(s), "scratch") }

// AllSessions lists every Review Session directory under the data dir
// (not fixture mode's).
func AllSessions(dataDir string) ([]Session, error) {
	matches, err := filepath.Glob(filepath.Join(dataDir, "reviews", "*", "*", "*", "events.jsonl"))
	if err != nil {
		return nil, err
	}
	var out []Session
	for _, m := range matches {
		out = append(out, Session(filepath.Dir(m)))
	}
	return out, nil
}
