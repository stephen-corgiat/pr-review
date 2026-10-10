package ui

import (
	"fmt"
	"strings"

	glamour "charm.land/glamour/v2"
	"charm.land/glamour/v2/ansi"
	"charm.land/glamour/v2/styles"
)

var mdCache = map[string][][]Span{}

// markdownStyle is glamour's dark style with the document margin removed:
// our rows supply their own indent.
func markdownStyle() ansi.StyleConfig {
	s := styles.DarkStyleConfig
	zero := uint(0)
	s.Document.Margin = &zero
	s.Document.BlockPrefix = ""
	s.Document.BlockSuffix = ""
	return s
}

// renderMarkdown renders body with Glamour at width and returns one span
// line per output line, cached per (width, body).
func renderMarkdown(body string, width int) [][]Span {
	width = maxInt(width, 10)
	key := fmt.Sprintf("%d|%s", width, body)
	if v, ok := mdCache[key]; ok {
		return v
	}
	var out string
	r, err := glamour.NewTermRenderer(glamour.WithStyles(markdownStyle()), glamour.WithWordWrap(width))
	if err == nil {
		out, err = r.Render(body)
	}
	if err != nil {
		out = body
	}
	lines := strings.Split(strings.Trim(out, "\n"), "\n")
	// Drop leading/trailing blank lines and collapse runs of blanks.
	var res [][]Span
	blank := false
	for _, l := range lines {
		spans := trimRightSpans(ansiToSpans(l))
		if len(spans) == 0 {
			if blank || len(res) == 0 {
				continue
			}
			blank = true
			res = append(res, nil)
			continue
		}
		blank = false
		res = append(res, spans)
	}
	for len(res) > 0 && res[len(res)-1] == nil {
		res = res[:len(res)-1]
	}
	mdCache[key] = res
	return res
}

// markdownRows turns rendered markdown into rows: lines that continue a
// paragraph join a selection with a space.
func markdownRows(body string, width int, prefix string, tmpl Row) []Row {
	lines := renderMarkdown(body, width-cellWidth(prefix))
	rows := make([]Row, 0, len(lines))
	for i, l := range lines {
		r := tmpl
		r.Prefix = prefix
		r.Spans = l
		r.Join = JoinLine
		if i > 0 && lines[i-1] != nil && l != nil {
			r.Join = JoinSoft
		}
		rows = append(rows, r)
	}
	return rows
}
