package treesitter

import (
	"embed"
	"fmt"
	"regexp"
	"strings"
)

//go:embed queries/*/*.scm
var queryFS embed.FS

// docPrefix is a tags pattern's leading "(comment)* @doc ." and docDirective
// a directive on @doc. See queries/README.md for why they're dropped.
var (
	docPrefix    = regexp.MustCompile(`\(comment\)\*\s*@doc\s*\.\s*`)
	docDirective = regexp.MustCompile(`(?m)^[ \t]*\(#(?:strip|select-adjacent)! @doc[^\n]*\n`)
)

// tagsQuery reads the named query files (e.g. "typescript/tags.scm") in
// order, drops what gotreesitter can't run (docPrefix, docDirective) and
// joins them.
func tagsQuery(files ...string) (string, error) {
	var b strings.Builder
	for _, f := range files {
		src, err := queryFS.ReadFile("queries/" + f)
		if err != nil {
			return "", fmt.Errorf("tags query %s: %w", f, err)
		}
		s := docDirective.ReplaceAllString(string(src), "")
		s = docPrefix.ReplaceAllString(s, "")
		b.WriteString(s)
		b.WriteString("\n")
	}
	return b.String(), nil
}
