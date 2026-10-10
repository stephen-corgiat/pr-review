package ui

// ] / [: next / previous change across the whole PR. Stops, per file: the
// first line of each run of changed lines, and the start of each
// declaration (from the code analysis: Go, TypeScript, TSX, Ruby) whose
// first line is added, which lands on its method block's header rule when
// it has one.

import (
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

type changeStop struct {
	diffRow int
	method  string // method key: land on its block's header rule
}

func (m *Model) changeStops(p string) []changeStop {
	fd := m.sess.Files[p]
	if fd == nil || fd.Binary {
		return nil
	}
	at := map[int]*changeStop{}
	add := func(row int, method string) {
		if s, ok := at[row]; ok {
			if method != "" {
				s.method = method
			}
			return
		}
		at[row] = &changeStop{diffRow: row, method: method}
	}
	for i, l := range fd.All {
		if l.Kind != session.LineContext && (i == 0 || fd.All[i-1].Kind == session.LineContext) {
			add(i, "")
		}
	}
	decl := map[int]bool{}
	for _, n := range m.sess.DeclLines[p] {
		decl[n] = true
	}
	blocks := map[int]string{}
	for _, k := range m.sess.MethodsByFile[p] {
		blocks[m.methodBlockRow(m.sess.Methods[k])] = k
	}
	for i, l := range fd.All {
		if l.Kind == session.LineAdded && decl[l.NewNo] {
			add(i, blocks[i])
		}
	}
	out := make([]changeStop, 0, len(at))
	for _, s := range at {
		out = append(out, *s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].diffRow < out[j].diffRow })
	return out
}

// hereRow is the cursor's position in diff rows; on a method block, the
// method's block row (as for J/K).
func (m *Model) hereRow() int {
	cur := m.cursorDiffRow()
	if m.centreCursor >= 0 && m.centreCursor < len(m.centreRows) {
		if r := m.centreRows[m.centreCursor]; r.Anchor.Type == session.AnchorBriefing && strings.HasPrefix(r.Anchor.Ref, "method:") {
			if meth := m.sess.Methods[strings.TrimPrefix(r.Anchor.Ref, "method:")]; meth != nil {
				cur = m.methodBlockRow(meth)
			}
		}
	}
	return cur
}

// landStop puts the stop near the top of the pane (in the open file).
func (m *Model) landStop(s changeStop) {
	m.rebuild()
	if s.method != "" {
		for i, r := range m.centreRows {
			if r.Anchor.Type == session.AnchorBriefing && r.Anchor.Ref == "method:"+s.method {
				m.centreCursor = i
				m.centreScroll = i - 1
				m.clampCentre()
				return
			}
		}
	}
	if !m.revealDiffRow(s.diffRow) {
		m.hunkMode[m.filePath] = false
		m.rebuild()
		m.revealDiffRow(s.diffRow)
	}
}

func (m *Model) jumpChange(dir int) tea.Cmd {
	order := m.sess.FileOrder()
	if m.centreView != ViewFile {
		if dir < 0 {
			return nil
		}
		for _, p := range order {
			if st := m.changeStops(p); len(st) > 0 {
				m.openFile(p) // a jump
				m.landStop(st[0])
				return nil
			}
		}
		return nil
	}
	stops := m.changeStops(m.filePath)
	cur := m.hereRow()
	if dir > 0 {
		for _, s := range stops {
			if s.diffRow > cur {
				m.landStop(s)
				return nil
			}
		}
	} else {
		for i := len(stops) - 1; i >= 0; i-- {
			if stops[i].diffRow < cur {
				m.landStop(stops[i])
				return nil
			}
		}
	}
	// Cross into the next / previous file that has changes.
	idx := -1
	for i, p := range order {
		if p == m.filePath {
			idx = i
		}
	}
	for i := idx + dir; i >= 0 && i < len(order); i += dir {
		st := m.changeStops(order[i])
		if len(st) == 0 {
			continue
		}
		m.openFile(order[i]) // crossing files is a jump
		if dir > 0 {
			m.landStop(st[0])
		} else {
			m.landStop(st[len(st)-1])
		}
		arrow := "→ "
		if dir < 0 {
			arrow = "← "
		}
		return m.flashCmd(arrow + shortenPath(order[i]))
	}
	if dir > 0 {
		return m.flashCmd("last change")
	}
	return m.flashCmd("first change")
}
