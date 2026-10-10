package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstallIntoATempDir(t *testing.T) {
	dir := t.TempDir()
	p, err := Install(dir, "---\nname: pull-review\n---\nv1\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(dir, "pull-review", "SKILL.md"); p != want {
		t.Fatalf("installed at %s, want %s", p, want)
	}
	// Installing again replaces it.
	if _, err := Install(dir, "---\nname: pull-review\n---\nv2\n"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "---\nname: pull-review\n---\nv2\n" {
		t.Fatalf("content %q", b)
	}
}

func TestDir(t *testing.T) {
	if d, _ := Dir("/x/skills"); d != "/x/skills" {
		t.Errorf("the flag wins: %s", d)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "/cfg")
	if d, _ := Dir(""); d != "/cfg/skills" {
		t.Errorf("CLAUDE_CONFIG_DIR: %s", d)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", "/home/u")
	if d, _ := Dir(""); d != "/home/u/.claude/skills" {
		t.Errorf("default: %s", d)
	}
}
