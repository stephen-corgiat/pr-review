// Package gitsrc gets a Pull Request's real diff and file contents from git:
// a bare clone plus a detached worktree in the data dir (checkout mode), or
// any local repo as it stands (-repo mode). Throwaway code: only the error
// handling needed to run.
package gitsrc

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Repo is an opened source of truth: a working tree plus base/head refs.
type Repo struct {
	Dir        string // the worktree (or -repo path) to run git -C against
	Base, Head string
}

// OpenRepo mode: an existing local repo, no clone or worktree.
func OpenRepo(path, base, head string) *Repo {
	return &Repo{Dir: path, Base: base, Head: head}
}

func (r *Repo) git(args ...string) (string, error) {
	full := append([]string{"-C", r.Dir}, args...)
	cmd := exec.Command("git", full...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, errb.String())
	}
	return out.String(), nil
}

// NameStatus returns the changed files between base and head with their
// status (A/M/D).
type FileStatus struct {
	Path   string
	Status string // "added", "modified", "deleted"
}

func (r *Repo) NameStatus() ([]FileStatus, error) {
	out, err := r.git("diff", "--name-status", r.Base, r.Head)
	if err != nil {
		return nil, err
	}
	var files []FileStatus
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		st := "modified"
		switch parts[0][0] {
		case 'A':
			st = "added"
		case 'D':
			st = "deleted"
		case 'M':
			st = "modified"
		default:
			st = "modified"
		}
		files = append(files, FileStatus{Path: parts[1], Status: st})
	}
	return files, nil
}

// LineKind of a merged diff line.
type LineKind int

const (
	LineContext LineKind = iota
	LineAdded
	LineRemoved
)

type DiffLine struct {
	Kind  LineKind
	OldNo int // 0 if new-side-only
	NewNo int // 0 if old-side-only
	Text  string
}

// IsBinary reports whether git considers the file binary for this diff.
func (r *Repo) IsBinary(path string) (bool, error) {
	out, err := r.git("diff", "--numstat", r.Base, r.Head, "--", path)
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(strings.TrimSpace(out), "-\t-\t"), nil
}

// FullDiff returns the whole file as merged lines, with unified=huge so there
// are no separate hunks to reassemble.
func (r *Repo) FullDiff(path string) ([]DiffLine, error) {
	out, err := r.git("diff", "--no-color", "--no-ext-diff", "--unified=1000000", r.Base, r.Head, "--", path)
	if err != nil {
		return nil, err
	}
	return parseUnifiedFull(out), nil
}

func parseUnifiedFull(diff string) []DiffLine {
	lines := strings.Split(diff, "\n")
	var out []DiffLine
	oldNo, newNo := 0, 0
	inHunk := false
	for _, l := range lines {
		if strings.HasPrefix(l, "@@ ") {
			// @@ -oldStart,oldLines +newStart,newLines @@
			parts := strings.SplitN(l, "@@", 3)
			if len(parts) < 2 {
				continue
			}
			fields := strings.Fields(parts[1])
			if len(fields) >= 2 {
				oldNo = parseHunkStart(fields[0])
				newNo = parseHunkStart(fields[1])
			}
			inHunk = true
			continue
		}
		if !inHunk {
			continue
		}
		if strings.HasPrefix(l, "diff --git") || strings.HasPrefix(l, "index ") ||
			strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "+++ ") ||
			strings.HasPrefix(l, "new file mode") || strings.HasPrefix(l, "deleted file mode") ||
			strings.HasPrefix(l, "old mode") || strings.HasPrefix(l, "new mode") ||
			strings.HasPrefix(l, "similarity index") || strings.HasPrefix(l, "rename ") ||
			strings.HasPrefix(l, "\\ No newline") {
			continue
		}
		if l == "" {
			// Could be a genuine blank context/added/removed line with no
			// prefix char stripped away by Split, or trailing empty from the
			// final split. Treat a truly empty string at end-of-output as
			// noise only when it's the very last element (handled by caller
			// via trailing newline in diff output already).
			continue
		}
		switch l[0] {
		case ' ':
			oldNo++
			newNo++
			out = append(out, DiffLine{Kind: LineContext, OldNo: oldNo, NewNo: newNo, Text: l[1:]})
		case '+':
			newNo++
			out = append(out, DiffLine{Kind: LineAdded, NewNo: newNo, Text: l[1:]})
		case '-':
			oldNo++
			out = append(out, DiffLine{Kind: LineRemoved, OldNo: oldNo, Text: l[1:]})
		default:
			// Unexpected; treat as context to stay safe.
			oldNo++
			newNo++
			out = append(out, DiffLine{Kind: LineContext, OldNo: oldNo, NewNo: newNo, Text: l})
		}
	}
	return out
}

func parseHunkStart(field string) int {
	// field like "-1,23" or "+1" or "-0,0"
	field = strings.TrimLeft(field, "+-")
	comma := strings.IndexByte(field, ',')
	if comma >= 0 {
		field = field[:comma]
	}
	n, _ := strconv.Atoi(field)
	if n == 0 {
		return 0
	}
	return n - 1 // so the first ++ lands on n
}

// Show returns a file's content at a ref ("" content, ok=false if it doesn't
// exist there, e.g. an added or deleted file).
func (r *Repo) Show(ref, path string) (string, bool) {
	out, err := r.git("show", ref+":"+path)
	if err != nil {
		return "", false
	}
	return out, true
}

// LogSubject returns `git log -1 --format=%s <ref>`.
func (r *Repo) LogSubject(ref string) (string, error) {
	out, err := r.git("log", "-1", "--format=%s", ref)
	return strings.TrimSpace(out), err
}

// LogRangeBody returns `git log --format='%h %s%n%n%b' base..head`.
func (r *Repo) LogRangeBody() (string, error) {
	out, err := r.git("log", "--format=%h %s%n%n%b", r.Base+".."+r.Head)
	return out, err
}

// GoModPath returns the module path from go.mod at head, or "" if absent.
func (r *Repo) GoModPath() string {
	content, ok := r.Show(r.Head, "go.mod")
	if !ok {
		return ""
	}
	for _, l := range strings.Split(content, "\n") {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(l, "module"))
		}
	}
	return ""
}

// Run runs git in dir and returns its trimmed stdout.
func Run(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errb.String()))
	}
	return strings.TrimSpace(out.String()), nil
}

// ZeroHunk is one hunk of a zero-context diff: OldCount lines at OldStart
// became NewCount lines at NewStart.
type ZeroHunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
}

// ZeroHunks is every file's hunks from Base to Head with no context (and
// no rename detection: a rename is a delete and an add), plus each
// changed file's status: added, modified or deleted.
func (r *Repo) ZeroHunks() (map[string][]ZeroHunk, map[string]string, error) {
	status := map[string]string{}
	ns, err := r.git("diff", "--no-renames", "--name-status", r.Base, r.Head)
	if err != nil {
		return nil, nil, err
	}
	for _, l := range strings.Split(ns, "\n") {
		parts := strings.SplitN(l, "\t", 2)
		if len(parts) != 2 || parts[0] == "" {
			continue
		}
		st := "modified"
		switch parts[0][0] {
		case 'A':
			st = "added"
		case 'D':
			st = "deleted"
		}
		status[parts[1]] = st
	}
	out, err := r.git("diff", "--no-color", "--no-ext-diff", "--no-renames", "-U0", r.Base, r.Head)
	if err != nil {
		return nil, nil, err
	}
	hunks := map[string][]ZeroHunk{}
	path := ""
	for _, l := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(l, "+++ "):
			p := strings.TrimPrefix(l, "+++ ")
			if p == "/dev/null" {
				continue // deleted: the "--- a/" line named it
			}
			path = strings.TrimPrefix(p, "b/")
		case strings.HasPrefix(l, "--- "):
			if p := strings.TrimPrefix(l, "--- "); p != "/dev/null" {
				path = strings.TrimPrefix(p, "a/")
			}
		case strings.HasPrefix(l, "@@ "):
			f := strings.Fields(l)
			if len(f) < 3 || path == "" {
				continue
			}
			os, oc := hunkRange(f[1])
			nsn, nc := hunkRange(f[2])
			hunks[path] = append(hunks[path], ZeroHunk{OldStart: os, OldCount: oc, NewStart: nsn, NewCount: nc})
		}
	}
	return hunks, status, nil
}

// hunkRange reads "-12,3" or "+7" (a count of 1 when there's none).
func hunkRange(s string) (start, count int) {
	s = strings.TrimLeft(s, "+-")
	a, b, found := strings.Cut(s, ",")
	start, _ = strconv.Atoi(a)
	count = 1
	if found {
		count, _ = strconv.Atoi(b)
	}
	return start, count
}
