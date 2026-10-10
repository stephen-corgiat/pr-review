package ui

import (
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/highlight"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

type Span = highlight.Span

// RowKind distinguishes how a Row behaves.
type RowKind int

const (
	RowCode RowKind = iota
	RowGap
	RowText
	RowCard
)

// Join says how a row's text joins the previous row's when a selection
// spans both.
const (
	JoinLine = iota // a new line: "\n"
	JoinSoft        // word-wrapped continuation: " "
	JoinHard        // hard-wrapped continuation (long code): ""
)

// Link is a clickable rune range [Start,End) of a Row's text.
type Link struct {
	Start, End int
	Act        func(*Model) tea.Cmd
}

// Row is one screen line of pane content: enough to render, hit-test and
// select. Every row is exactly one terminal line; long text is wrapped into
// several rows when the rows are built (they're rebuilt on resize).
type Row struct {
	Kind RowKind

	Marker        string // 1-cell marker column (centre only): "●" "✎" or ""
	MarkerStyle   lg.Style
	markerThreads []*session.Thread

	Prefix string // styled, not selectable (gutter, indent, card border)
	Spans  []Span // the selectable content
	Suffix string // styled, drawn at the row's right edge (card border)
	Width  int    // 0 = pane width; card rows use their card width

	BG                 string // whole-row background (diff tint, card band)
	EmphStart, EmphEnd int    // rune range drawn with EmphBG (changed words)
	EmphBG             string

	Join       int
	Selectable bool
	Anchor     session.Anchor
	DiffRow    int // index into FileDiff.All, -1 if none
	OutLine    int // a file outside the PR: its line number (0 if none)

	Thread   *session.Thread
	Feedback *session.Feedback
	Comment  *session.PRComment // a row of a PR Comment's card
	Revision bool               // a row of Feedback's Revision block
	// TALine is 1 + the index of the open editor's view line this row
	// draws (0: not an editor row), for click to place the cursor.
	TALine int

	// Keys the row answers to when the centre cursor is on it (no
	// selection), before the action letters: r on a Feedback card refines.
	Keys map[string]func(*Model) tea.Cmd

	// Activate runs on enter, or a click that isn't on a Link.
	Activate func(m *Model) tea.Cmd
	Links    []Link
}

func (r Row) text() string { return highlight.PlainText(r.Spans) }

func (r Row) prefixWidth(markerCol bool) int {
	w := cellWidth(r.Prefix)
	if markerCol {
		w++
	}
	return w
}

func (r Row) linkAt(col int) *Link {
	for i := range r.Links {
		if col >= r.Links[i].Start && col < r.Links[i].End {
			return &r.Links[i]
		}
	}
	return nil
}

// ---- building spans ----

func sp(text string, st lg.Style) Span { return Span{Text: text, Style: st} }

func spansLen(spans []Span) int {
	n := 0
	for _, s := range spans {
		n += utf8.RuneCountInString(s.Text)
	}
	return n
}

// sliceSpans returns the rune range [s,e) of spans.
func sliceSpans(spans []Span, s, e int) []Span {
	var out []Span
	pos := 0
	for _, sp := range spans {
		rs := []rune(sp.Text)
		a, b := pos, pos+len(rs)
		pos = b
		lo, hi := maxInt(a, s), minInt(b, e)
		if lo >= hi {
			continue
		}
		out = append(out, Span{Text: string(rs[lo-a : hi-a]), Style: sp.Style})
	}
	return out
}

func trimRightSpans(spans []Span) []Span {
	for len(spans) > 0 {
		last := &spans[len(spans)-1]
		t := strings.TrimRight(last.Text, " ")
		if t != "" {
			last.Text = t
			return spans
		}
		spans = spans[:len(spans)-1]
	}
	return spans
}

// wrapSpans word-wraps spans: the first line gets firstW cells, the rest
// restW. Breaks fall on spaces (which are dropped); a word longer than the
// width is broken hard.
func wrapSpans(spans []Span, firstW, restW int) [][]Span {
	// A raw newline must never reach the frame: each line wraps on its own.
	if p := highlight.PlainText(spans); strings.ContainsRune(p, '\n') {
		var out [][]Span
		pos := 0
		for i, line := range strings.Split(p, "\n") {
			n := len([]rune(line))
			w := restW
			if i == 0 {
				w = firstW
			}
			out = append(out, wrapSpans(sliceSpans(spans, pos, pos+n), w, restW)...)
			pos += n + 1
		}
		return out
	}
	plain := []rune(highlight.PlainText(spans))
	n := len(plain)
	if n == 0 {
		return [][]Span{nil}
	}
	var out [][]Span
	start, w := 0, maxInt(1, firstW)
	for start < n {
		cells, i, lastSpace := 0, start, -1
		for i < n {
			cw := runeWidth(plain[i])
			if cells+cw > w {
				break
			}
			if plain[i] == ' ' {
				lastSpace = i
			}
			cells += cw
			i++
		}
		if i >= n {
			out = append(out, sliceSpans(spans, start, n))
			break
		}
		if i == start {
			i = start + 1 // one glyph wider than the line
			if i >= n {
				out = append(out, sliceSpans(spans, start, n))
				break
			}
		}
		end, next := i, i
		switch {
		case plain[i] == ' ':
			end, next = i, i+1
		case lastSpace > start:
			end, next = lastSpace, lastSpace+1
		}
		out = append(out, trimRightSpans(sliceSpans(spans, start, end)))
		for next < n && plain[next] == ' ' {
			next++
		}
		start, w = next, maxInt(1, restW)
	}
	return out
}

// hardWrapSpans cuts spans every w cells, keeping every character (code).
func hardWrapSpans(spans []Span, w int) [][]Span {
	plain := []rune(highlight.PlainText(spans))
	if len(plain) == 0 {
		return [][]Span{nil}
	}
	w = maxInt(1, w)
	var out [][]Span
	start, cells := 0, 0
	for i, r := range plain {
		cw := runeWidth(r)
		if cells+cw > w && i > start {
			out = append(out, sliceSpans(spans, start, i))
			start, cells = i, 0
		}
		cells += cw
	}
	return append(out, sliceSpans(spans, start, len(plain)))
}

// textRows word-wraps spans into rows of total width w. The first row gets
// prefix first, continuation rows get rest (both are styled, unselectable).
// tmpl supplies everything else (anchor, activate, ...).
func textRows(spans []Span, w int, first, rest string, tmpl Row) []Row {
	lines := wrapSpans(spans, w-cellWidth(first), w-cellWidth(rest))
	rows := make([]Row, 0, len(lines))
	for i, l := range lines {
		r := tmpl
		r.Spans = l
		r.Prefix = first
		r.Join = tmpl.Join
		if i > 0 {
			r.Prefix = rest
			r.Join = JoinSoft
			r.Links = nil
		}
		rows = append(rows, r)
	}
	// Links are given in rune offsets of the unwrapped text; move each onto
	// the row it falls in (wrapping drops the break spaces, so re-find them).
	if len(tmpl.Links) > 0 && len(rows) > 1 {
		placeLinks(rows, spans, tmpl.Links)
	}
	return rows
}

func placeLinks(rows []Row, spans []Span, links []Link) {
	full := []rune(highlight.PlainText(spans))
	rows[0].Links = nil
	off := 0 // rune offset in full of the current row's start
	rowStart := make([]int, len(rows))
	for i := range rows {
		t := []rune(rows[i].text())
		// skip spaces dropped at the break
		for off < len(full) && i > 0 && full[off] == ' ' && (len(t) == 0 || t[0] != ' ') {
			off++
		}
		rowStart[i] = off
		off += len(t)
	}
	for _, l := range links {
		for i := len(rows) - 1; i >= 0; i-- {
			if l.Start >= rowStart[i] {
				rows[i].Links = append(rows[i].Links, Link{Start: l.Start - rowStart[i], End: l.End - rowStart[i], Act: l.Act})
				break
			}
		}
	}
}

// ---- rendering one row ----

// hlRange is a search match drawn over a row's text (rune offsets).
type hlRange struct {
	s, e int
	bg   string
	cur  bool // the current match: dark text on the strong background
}

// renderSpans draws spans with a row background, an emphasis range, search
// highlights and a selection range (rune offsets); the selection wins over
// search, which wins over emphasis, which wins over the row background.
func renderSpans(spans []Span, bg string, emS, emE int, emBG string, selS, selE int, hl ...hlRange) string {
	var sb strings.Builder
	pos := 0
	type look struct {
		bg  string
		cur bool
	}
	at := func(i int) look {
		if i >= selS && i < selE {
			return look{bg: bgSel}
		}
		for _, h := range hl {
			if i >= h.s && i < h.e {
				return look{h.bg, h.cur}
			}
		}
		if emBG != "" && i >= emS && i < emE {
			return look{bg: emBG}
		}
		return look{bg: bg}
	}
	for _, s := range spans {
		text := s.Text
		if strings.ContainsRune(text, '\n') {
			text = strings.ReplaceAll(text, "\n", " ") // never let a newline reach the frame
		}
		rs := []rune(text)
		i := 0
		for i < len(rs) {
			b := at(pos + i)
			j := i + 1
			for j < len(rs) && at(pos+j) == b {
				j++
			}
			st := s.Style
			if b.bg != "" {
				st = st.Background(lg.Color(b.bg))
			}
			if b.cur {
				st = st.Foreground(lg.Color(cCrust)).Bold(true)
			}
			sb.WriteString(st.Render(string(rs[i:j])))
			i = j
		}
		pos += len(rs)
	}
	return sb.String()
}

func cursorBG(bg string) string {
	switch bg {
	case bgAdded:
		return bgCursorAdded
	case bgRemoved:
		return bgCursorRemoved
	case "":
		return bgCursor
	}
	return bgCursor
}

// renderRow draws r into exactly paneW cells. sel is the selected rune range
// on this row (selS >= selE for none).
func renderRow(r Row, paneW int, markerCol bool, selS, selE int, cursor bool, hl ...hlRange) string {
	// Row.Width counts from after the marker column.
	var b strings.Builder
	if markerCol {
		mk := r.Marker
		if mk == "" {
			mk = " "
		}
		b.WriteString(r.MarkerStyle.Render(mk))
		paneW--
	}
	w := r.Width
	if w <= 0 || w > paneW {
		w = paneW
	}
	pw, sw := cellWidth(r.Prefix), cellWidth(r.Suffix)
	bodyW := maxInt(0, w-pw-sw)
	bg := r.BG
	if cursor {
		bg = cursorBG(bg)
	}
	body := renderSpans(r.Spans, bg, r.EmphStart, r.EmphEnd, r.EmphBG, selS, selE, hl...)
	if bw := cellWidth(body); bw > bodyW {
		body = padTo(body, bodyW)
	} else if bw < bodyW {
		fill := strings.Repeat(" ", bodyW-bw)
		if bg != "" {
			fill = lg.NewStyle().Background(lg.Color(bg)).Render(fill)
		}
		body += fill
	}
	b.WriteString(r.Prefix)
	b.WriteString(body)
	b.WriteString(r.Suffix)
	return padTo(b.String(), paneW+boolInt(markerCol))
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// expandTabs replaces tabs with 4 spaces, so cell and rune offsets agree.
func expandTabs(spans []Span) []Span {
	out := make([]Span, len(spans))
	for i, s := range spans {
		out[i] = s
		if strings.ContainsRune(s.Text, '\t') {
			out[i].Text = strings.ReplaceAll(s.Text, "\t", "    ")
		}
	}
	return out
}

// colToRune converts a cell offset within text to a rune offset.
func colToRune(text string, cells int) int {
	if cells <= 0 {
		return 0
	}
	c, i := 0, 0
	for _, r := range text {
		cw := runeWidth(r)
		if c+cw > cells {
			return i
		}
		c += cw
		i++
	}
	return i
}
