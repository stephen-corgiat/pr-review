// Package fixturerepo builds the git repo behind the fixture Pull Request:
// "dispatchly", a small made-up Go app kept as three source trees (c0, c1
// and c2) under testdata/dispatchly/repo. The PR is c1..c2; c0..c1 is an
// earlier commit that changes a signature, for -repo mode. The commits
// have fixed authors and dates, so their SHAs are the same everywhere.
package fixturerepo

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Repo is a built fixture repo.
type Repo struct {
	Dir string
	// C0, C1 and C2 are the commits, oldest first. The fixture PR's base
	// is C1 and its head is C2.
	C0, C1, C2 string
}

var commits = []struct{ tree, message, date string }{
	{"c0", "Add contacts, settings and bounce webhooks", "2026-03-02T10:00:00Z"},
	{"c1", "Let UpdateContactWithLists preconfirm the memberships", "2026-03-09T10:00:00Z"},
	{"c2", "Add Beacon bounce webhooks and settings support", "2026-03-16T10:00:00Z"},
}

// Build creates the repo from the trees in treesDir under parent, unless an
// up to date one is there already, and returns it. It is safe to call from
// several processes at once.
func Build(treesDir, parent string) (*Repo, error) {
	sum, err := hashTrees(treesDir)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(parent, "dispatchly-"+sum[:12])
	if r, err := open(dir); err == nil {
		return r, nil
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return nil, err
	}
	tmp, err := os.MkdirTemp(parent, "dispatchly-build-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmp)
	if err := git(tmp, nil, "init", "-q", "-b", "main"); err != nil {
		return nil, err
	}
	for _, c := range commits {
		if err := replaceTree(tmp, filepath.Join(treesDir, c.tree)); err != nil {
			return nil, err
		}
		env := []string{"GIT_AUTHOR_DATE=" + c.date, "GIT_COMMITTER_DATE=" + c.date}
		if err := git(tmp, nil, "add", "-A"); err != nil {
			return nil, err
		}
		if err := git(tmp, env, "commit", "-q", "-m", c.message); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(tmp, dir); err != nil {
		// Another process built it first.
		if r, err2 := open(dir); err2 == nil {
			return r, nil
		}
		return nil, err
	}
	return open(dir)
}

func open(dir string) (*Repo, error) {
	out, err := gitOut(dir, "rev-list", "--reverse", "HEAD")
	if err != nil {
		return nil, err
	}
	shas := strings.Fields(out)
	if len(shas) != len(commits) {
		return nil, fmt.Errorf("fixture repo %s has %d commits, want %d", dir, len(shas), len(commits))
	}
	return &Repo{Dir: dir, C0: shas[0], C1: shas[1], C2: shas[2]}, nil
}

// replaceTree makes dir's working tree (everything but .git) match src.
func replaceTree(dir, src string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.Name() != ".git" {
			if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
				return err
			}
		}
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if rel == "." {
			return nil
		}
		dst := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, b, 0o644)
	})
}

// hashTrees fingerprints the trees' paths and contents.
func hashTrees(treesDir string) (string, error) {
	h := sha256.New()
	for _, c := range commits {
		fmt.Fprintf(h, "%s\x00%s\x00%s\x00", c.tree, c.message, c.date)
		root := filepath.Join(treesDir, c.tree)
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			rel, _ := filepath.Rel(root, p)
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			fmt.Fprintf(h, "%s\x00%s\x00%d\x00", c.tree, rel, len(b))
			h.Write(b)
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("fixture repo trees: %w", err)
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func gitEnv(extra []string) []string {
	return append(os.Environ(), append([]string{
		"GIT_AUTHOR_NAME=Dispatchly Fixture", "GIT_AUTHOR_EMAIL=fixture@example.com",
		"GIT_COMMITTER_NAME=Dispatchly Fixture", "GIT_COMMITTER_EMAIL=fixture@example.com",
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null",
	}, extra...)...)
}

func git(dir string, env []string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(env)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, out)
	}
	return nil
}

func gitOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(nil)
	out, err := cmd.Output()
	return string(out), err
}

// CacheParent is where the tests build the repo: the same place every run,
// so it is built once. It is not the user's data directory.
func CacheParent() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		dir = os.TempDir()
	}
	return filepath.Join(dir, "pr-review", "fixtures")
}
