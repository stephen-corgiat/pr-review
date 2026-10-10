// Package catest holds test helpers for the code-analysis interface: a
// FilePair from two versions of a file, and checks that print Methods and
// Edges in one line each, so a failing test shows the whole result.
package catest

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis"
)

// Pair builds the FilePair for path from its base and head contents, with
// changed lines from a line diff. An empty side doesn't exist (an added or
// deleted file).
func Pair(path, base, head string) codeanalysis.FilePair {
	fp := codeanalysis.FilePair{
		Path: path, Old: base, New: head, OldExists: base != "", NewExists: head != "",
		ChangedOld: map[int]bool{}, ChangedNew: map[int]bool{},
	}
	a, b := lines(base), lines(head)
	// Longest common subsequence; lines outside it are changed.
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(a) || j < len(b) {
		switch {
		case i < len(a) && j < len(b) && a[i] == b[j]:
			i++
			j++
		case j < len(b) && (i == len(a) || lcs[i][j+1] >= lcs[i+1][j]):
			fp.ChangedNew[j+1] = true
			j++
		default:
			fp.ChangedOld[i+1] = true
			i++
		}
	}
	return fp
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// Describe is one line per Method:
// "<key> <status> new <start>-<end> decl <line> old <start>-<end> decl <line>",
// each side only when present.
func Describe(m codeanalysis.Method) string {
	s := m.Key + " " + m.Status.String()
	if m.NewStart > 0 {
		s += fmt.Sprintf(" new %d-%d decl %d", m.NewStart, m.NewEnd, m.NewDeclLine)
	}
	if m.OldStart > 0 {
		s += fmt.Sprintf(" old %d-%d decl %d", m.OldStart, m.OldEnd, m.OldDeclLine)
	}
	return s
}

// Methods checks r's Methods against want (Describe lines, any order).
func Methods(t *testing.T, r *codeanalysis.Result, want []string) {
	t.Helper()
	var got []string
	for _, m := range r.Methods {
		got = append(got, Describe(m))
	}
	same(t, "Touched Methods", got, want)
}

// Edges checks r's Edges against want ("caller -> callee", with " (ambiguous)"
// when it is; any order).
func Edges(t *testing.T, r *codeanalysis.Result, want []string) {
	t.Helper()
	var got []string
	for _, e := range r.Edges {
		s := e.Caller + " -> " + e.Callee
		if e.Ambiguous {
			s += " (ambiguous)"
		}
		got = append(got, s)
	}
	same(t, "edges", got, want)
}

func same(t *testing.T, what string, got, want []string) {
	t.Helper()
	got = append([]string(nil), got...)
	want = append([]string(nil), want...)
	sort.Strings(got)
	sort.Strings(want)
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\ngot:\n  %s\nwant:\n  %s", what, strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}
