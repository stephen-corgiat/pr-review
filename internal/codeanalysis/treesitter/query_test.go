package treesitter

import (
	"strings"
	"testing"
)

// The upstream files still have what tagsQuery drops (so an update that
// changes their shape is noticed), and the result has none of it.
func TestTagsQueryDropsDocCaptures(t *testing.T) {
	for _, f := range []string{"javascript/tags.scm", "ruby/tags.scm"} {
		raw, err := queryFS.ReadFile("queries/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if !docPrefix.Match(raw) || !docDirective.Match(raw) {
			t.Errorf("%s: no (comment)* @doc prefix or @doc directive to drop; check queries/README.md still holds", f)
		}
		q, err := tagsQuery(f)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(q, "@doc") {
			t.Errorf("%s: @doc left after tagsQuery:\n%s", f, q)
		}
		inDirectives := strings.Count(strings.Join(docDirective.FindAllString(string(raw), -1), ""), "@definition.")
		if strings.Count(q, "@definition.") != strings.Count(string(raw), "@definition.")-inDirectives {
			t.Errorf("%s: tagsQuery lost a definition pattern", f)
		}
	}
}
