package fixtureagent_test

import (
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stephen-corgiat/pr-review/internal/fixtureagent"
	"github.com/stephen-corgiat/pr-review/internal/fixturerepo"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

const fixtures = "../../testdata/dispatchly"

// The fixture JSON is written against the fixture repo's source. This keeps
// the two in step: every anchor and citation must point at a real line, and
// the Briefing must describe the Touched Methods the analysis finds.
func TestFixturesMatchTheRepo(t *testing.T) {
	built, err := fixturerepo.Build(fixtures+"/repo", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := gitsrc.OpenRepo(built.Dir, built.C1, built.C2)
	fx, err := fixtureagent.LoadFixtures(fixtures)
	if err != nil {
		t.Fatal(err)
	}

	size := map[string]int{}
	lines := func(path string) int {
		if n, ok := size[path]; ok {
			return n
		}
		src, ok := repo.Show(repo.Head, path)
		if !ok {
			size[path] = -1
			return -1
		}
		size[path] = strings.Count(src, "\n")
		return size[path]
	}
	checkAnchor := func(who string, a fixtureagent.AnchorSpec) {
		switch a.Type {
		case "diff":
			n := lines(a.Path)
			if n < 0 || a.Lines[0] < 1 || a.Lines[1] < a.Lines[0] || a.Lines[1] > n {
				t.Errorf("%s: anchor %s:%v is outside the file (%d lines)", who, a.Path, a.Lines, n)
				return
			}
			dl, err := repo.FullDiff(a.Path)
			if err != nil {
				t.Errorf("%s: diff of %s: %v", who, a.Path, err)
				return
			}
			changed := false
			for _, l := range dl {
				if l.Kind == gitsrc.LineAdded && l.NewNo >= a.Lines[0] && l.NewNo <= a.Lines[1] {
					changed = true
				}
			}
			if !changed {
				t.Errorf("%s: anchor %s:%v touches no added line", who, a.Path, a.Lines)
			}
		case "file":
			if lines(a.Path) < 0 {
				t.Errorf("%s: no file %s", who, a.Path)
			}
		}
	}
	checkRef := func(who, ref string) {
		c := session.ParseCitation(ref)
		if c.URL != "" {
			return
		}
		n := lines(c.Path)
		if n < 0 {
			t.Errorf("%s: %q names a file that isn't in the repo", who, ref)
			return
		}
		if c.Line > n || c.EndLine > n {
			t.Errorf("%s: %q is past the end of the file (%d lines)", who, ref, n)
		}
	}
	checkBody := func(who string, b session.AnswerBody) {
		for _, c := range b.Citations {
			checkRef(who, citeString(c))
		}
		for _, c := range b.Evidence {
			checkRef(who, citeString(c))
		}
		for _, h := range b.Hops {
			checkRef(who, h.At)
		}
		for _, s := range b.Steps {
			checkRef(who, s.At)
		}
		for _, c := range b.Cases {
			checkRef(who, c.Location)
		}
	}

	for _, a := range fx.Answers {
		for _, an := range a.Anchors {
			checkAnchor(a.ID, an)
		}
		checkBody(a.ID, a.Answer)
		for _, f := range a.FollowUps {
			checkBody(a.ID+" follow-up", f)
		}
	}
	for _, s := range fx.FeedbackSeeds {
		checkAnchor(s.ID, s.Anchor)
	}
	for _, r := range fx.Revisions {
		for _, c := range r.Revision.Citations {
			checkRef(r.FeedbackID, citeString(c))
		}
	}

	st, _, err := session.Build(repo)
	if err != nil {
		t.Fatal(err)
	}
	var found, described []string
	for _, m := range st.GoMethods() {
		found = append(found, m.Key)
	}
	for k := range fx.BriefingRaw.Methods {
		described = append(described, k)
	}
	sort.Strings(found)
	sort.Strings(described)
	if strings.Join(found, "\n") != strings.Join(described, "\n") {
		t.Errorf("the Briefing's methods differ from the Touched Methods\nfound:\n  %s\nbriefing:\n  %s",
			strings.Join(found, "\n  "), strings.Join(described, "\n  "))
	}

	var changed []string
	files, err := repo.NameStatus()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		changed = append(changed, f.Path)
	}
	var listed []string
	for _, r := range fx.BriefingRaw.ReadingOrder {
		listed = append(listed, r.Path)
	}
	sort.Strings(changed)
	sort.Strings(listed)
	if strings.Join(changed, "\n") != strings.Join(listed, "\n") || len(fx.PR.Files) != len(changed) {
		t.Errorf("the Briefing's reading order and pr.json's files must be the PR's %d changed files\nchanged: %v\nreading: %v", len(changed), changed, listed)
	}
}

func citeString(c session.Citation) string {
	switch {
	case c.URL != "":
		return c.URL
	case c.EndLine != 0:
		return c.Path + ":" + itoa(c.Line) + "-" + itoa(c.EndLine)
	case c.Line != 0:
		return c.Path + ":" + itoa(c.Line)
	}
	return c.Path
}

func itoa(n int) string { return strconv.Itoa(n) }
