package session

import (
	"strconv"
	"strings"
)

// ---- Queries ----

func (s *Session) ThreadsOnRow(path string, row int) []*Thread {
	var out []*Thread
	for _, t := range s.Threads {
		if t.Anchor.Type == AnchorDiff && t.Anchor.Path == path && t.Anchor.RowEnd == row {
			out = append(out, t)
		}
	}
	return out
}

func (s *Session) FeedbackOnRow(path string, row int) []*Feedback {
	var out []*Feedback
	for _, f := range s.Feedback {
		if f.Anchor.Type == AnchorDiff && f.Anchor.Path == path && f.Anchor.RowEnd == row {
			out = append(out, f)
		}
	}
	return out
}

func (s *Session) MethodAt(path string, row int) *Method {
	fd := s.Files[path]
	if fd == nil || row < 0 || row >= len(fd.All) {
		return nil
	}
	l := fd.All[row]
	for _, k := range s.MethodsByFile[path] {
		m := s.Methods[k]
		if m.NewStart > 0 && l.NewNo > 0 && l.NewNo >= m.NewStart && l.NewNo <= m.NewEnd {
			return m
		}
		if m.OldStart > 0 && l.OldNo > 0 && l.OldNo >= m.OldStart && l.OldNo <= m.OldEnd {
			return m
		}
	}
	return nil
}

// SampleThisPathQuestion is the follow-up "sample this path" asks.
const SampleThisPathQuestion = "Sample this path: show the value passed at each hop."

// LocateAnchor fills in a diff Anchor's line numbers on each side from
// its rows.
func (s *Session) LocateAnchor(a Anchor) Anchor {
	if a.Type != AnchorDiff {
		return a
	}
	fd := s.Files[a.Path]
	if fd == nil {
		return a
	}
	a.NewStart, a.NewEnd, a.OldStart, a.OldEnd = 0, 0, 0, 0
	for i := a.RowStart; i <= a.RowEnd && i < len(fd.All); i++ {
		if i < 0 {
			continue
		}
		l := fd.All[i]
		if l.NewNo > 0 {
			if a.NewStart == 0 {
				a.NewStart = l.NewNo
			}
			a.NewEnd = l.NewNo
		}
		if l.OldNo > 0 {
			if a.OldStart == 0 {
				a.OldStart = l.OldNo
			}
			a.OldEnd = l.OldNo
		}
	}
	return a
}

// AnchorOnLines builds an Anchor on a file's lines: a diff Anchor on
// side "new" or "old" covering lines [from, to], its text the lines' text
// when text is empty.
func (s *Session) AnchorOnLines(path, side string, from, to int, text string) Anchor {
	fd := s.Files[path]
	rowStart, rowEnd := -1, -1
	if fd != nil {
		for i, l := range fd.All {
			no := l.NewNo
			if side == "old" {
				no = l.OldNo
			}
			if no != 0 && no >= from && no <= to {
				if rowStart == -1 {
					rowStart = i
				}
				rowEnd = i
			}
		}
	}
	if text == "" && fd != nil && rowStart >= 0 {
		var parts []string
		for i := rowStart; i <= rowEnd; i++ {
			parts = append(parts, strings.TrimSpace(fd.All[i].Text))
		}
		text = strings.Join(parts, " ")
	}
	return s.LocateAnchor(Anchor{Type: AnchorDiff, Path: path, RowStart: rowStart, RowEnd: rowEnd, Text: text})
}

// ParseCitation reads a Citation string: "path:12", "path:12-30", "path"
// or a URL.
func ParseCitation(c string) Citation {
	c = strings.TrimSpace(c)
	if strings.HasPrefix(c, "http://") || strings.HasPrefix(c, "https://") {
		return Citation{URL: c}
	}
	i := strings.LastIndexByte(c, ':')
	if i < 0 {
		return Citation{Path: c}
	}
	rng := c[i+1:]
	a, b, _ := strings.Cut(rng, "-")
	start, err := strconv.Atoi(a)
	if err != nil {
		return Citation{Path: c}
	}
	cit := Citation{Path: c[:i], Line: start}
	if b != "" {
		if end, err := strconv.Atoi(b); err == nil && end != start {
			cit.EndLine = end
		}
	}
	return cit
}

// RowsForLines finds a file's diff rows for new-side lines [ns, ne] (0:
// none) and old-side lines [os, oe]: on the new side any row showing those
// lines, on the old side only removed rows (unless there are no new-side
// lines, when any row showing them).
func (s *Session) RowsForLines(path string, ns, ne, os, oe int) (start, end int, ok bool) {
	fd := s.Files[path]
	if fd == nil {
		return -1, -1, false
	}
	start, end = -1, -1
	for i, l := range fd.All {
		hit := false
		if ns > 0 && l.NewNo >= ns && l.NewNo <= ne && l.Kind != LineRemoved {
			hit = true
		}
		if os > 0 && l.OldNo >= os && l.OldNo <= oe && (l.Kind == LineRemoved || (ns == 0 && l.Kind != LineAdded)) {
			hit = true
		}
		if hit {
			if start < 0 {
				start = i
			}
			end = i
		}
	}
	return start, end, start >= 0
}
