package ui

// `/` search over the centre view's visible rows, like less: a prompt in
// the footer, incremental highlighting, n / N after enter.

import (
	"fmt"
	"unicode"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

type searchMatch struct{ row, s, e int } // rune range [s,e) of the row's text

type searchState struct {
	prompt  bool   // the footer prompt is open
	query   string // what's highlighted (while prompt or hl)
	hl      bool   // highlights stay on after enter
	matches []searchMatch
	cur     int // index into matches

	// Where the centre was before `/`, for esc; and which view the
	// search belongs to (switching view clears it).
	fromScroll, fromCursor int
	view                   CentreView
	path                   string
}

func (s searchState) on() bool { return s.prompt || s.hl }

func (m *Model) startSearch() {
	m.search = searchState{
		prompt: true, view: m.centreView, path: m.filePath,
		fromScroll: m.centreScroll, fromCursor: m.centreCursor,
	}
	m.sel = Selection{}
	m.focus = FocusCentre
}

func (m *Model) clearSearch() { m.search = searchState{} }

// searchable: code and text rows; not gaps, not Thread / Feedback cards.
func searchable(r Row) bool { return r.Kind == RowCode || r.Kind == RowText }

// findMatches recomputes the matches on the current rows (called after every
// rebuild, since expanding a card or a gap shifts the rows).
func (m *Model) findMatches() {
	s := &m.search
	if !s.on() {
		return
	}
	if s.view != m.centreView || s.path != m.filePath {
		m.clearSearch()
		return
	}
	s.matches = s.matches[:0]
	q := []rune(s.query)
	if len(q) == 0 {
		return
	}
	fold := true
	for _, r := range q {
		if unicode.IsUpper(r) {
			fold = false
			break
		}
	}
	if fold {
		for i, r := range q {
			q[i] = unicode.ToLower(r)
		}
	}
	for i, r := range m.centreRows {
		if !searchable(r) {
			continue
		}
		t := []rune(r.text())
		if fold {
			for k, c := range t {
				t[k] = unicode.ToLower(c)
			}
		}
		for k := 0; k+len(q) <= len(t); k++ {
			if runesEq(t[k:k+len(q)], q) {
				s.matches = append(s.matches, searchMatch{i, k, k + len(q)})
				k += len(q) - 1
			}
		}
	}
	s.cur = clamp(s.cur, 0, len(s.matches)-1)
}

func runesEq(a, b []rune) bool {
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// searchFromOrigin moves to the first match at or after the cursor from
// before `/` (wrapping), as the query is typed.
func (m *Model) searchFromOrigin() {
	m.findMatches()
	s := &m.search
	if len(s.matches) == 0 {
		m.centreScroll, m.centreCursor = s.fromScroll, s.fromCursor
		m.clampCentre()
		return
	}
	s.cur = 0
	for i, mt := range s.matches {
		if mt.row >= s.fromCursor {
			s.cur = i
			break
		}
	}
	m.revealRow(s.matches[s.cur].row)
}

// searchStep is n / N: the next or previous match, wrapping.
func (m *Model) searchStep(d int) tea.Cmd {
	s := &m.search
	if len(s.matches) == 0 {
		return m.flashCmd("/" + s.query + ": no matches")
	}
	s.cur = (s.cur + d + len(s.matches)) % len(s.matches)
	m.revealRow(s.matches[s.cur].row)
	return nil
}

func (m *Model) handleSearchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := &m.search
	switch key := msg.String(); key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc":
		from, cur := s.fromScroll, s.fromCursor
		m.clearSearch()
		m.centreScroll, m.centreCursor = from, cur
		m.clampCentre()
		return m, nil
	case "enter":
		s.prompt = false
		s.hl = s.query != ""
		if !s.hl {
			m.clearSearch()
		}
		return m, nil
	case "backspace", "ctrl+h":
		if s.query == "" {
			return m.handleSearchKey(tea.KeyPressMsg{Code: tea.KeyEscape})
		}
		q := []rune(s.query)
		s.query = string(q[:len(q)-1])
	case "ctrl+u":
		s.query = ""
	default:
		if msg.Text == "" {
			return m, nil
		}
		s.query += msg.Text
	}
	m.searchFromOrigin()
	return m, nil
}

// rowSearchHL are the search highlight ranges on centre row i.
func (m *Model) rowSearchHL(i int) []hlRange {
	s := &m.search
	if !s.on() || len(s.matches) == 0 {
		return nil
	}
	var out []hlRange
	for k, mt := range s.matches {
		if mt.row < i {
			continue
		}
		if mt.row > i {
			break
		}
		h := hlRange{s: mt.s, e: mt.e, bg: bgSearch}
		if k == s.cur {
			h.bg, h.cur = bgSearchCur, true
		}
		out = append(out, h)
	}
	return out
}

// searchFooter is the footer's left side while a search is on.
func (m *Model) searchFooter() string {
	s := m.search
	q := stKey.Render("/") + stText.Render(s.query)
	if s.prompt {
		q += lg.NewStyle().Reverse(true).Render(" ")
	}
	var count string
	switch {
	case s.query == "":
	case len(s.matches) == 0:
		count = stNeedsYou.Render("no matches")
	default:
		count = fg(cYellow).Render(fmt.Sprintf("%d/%d", s.cur+1, len(s.matches)))
	}
	out := " " + q
	if count != "" {
		out += "  " + count
	}
	if s.prompt {
		out += "     " + hints("enter", "keep", "esc", "cancel")
	} else {
		out += "     " + hints("n", "next", "N/p", "previous", "esc", "clear", "/", "new search")
	}
	return out
}
