package session

import "strings"

// ---- Feedback ----

func (s *Session) FeedbackByID(id string) *Feedback {
	for _, f := range s.Feedback {
		if f.ID == id {
			return f
		}
	}
	return nil
}

// Ready is every ready Feedback item: the only ones that can be sent.
func (s *Session) Ready() []*Feedback {
	var out []*Feedback
	for _, f := range s.Feedback {
		if f.Status == FeedbackReady {
			out = append(out, f)
		}
	}
	return out
}

// ---- Agent Notes ----

// NoteSpan is one Agent Note: body[Start:End] is "<<Text>>".
type NoteSpan struct {
	Start, End int // byte offsets, including << and >>
	Text       string
}

// AgentNotes finds the <<…>> Agent Notes in body: outside backtick code
// spans and fenced blocks, not nested, never across a blank line. An
// unclosed << is plain text.
func AgentNotes(body string) []NoteSpan {
	var out []NoteSpan
	lines := strings.SplitAfter(body, "\n")
	off := 0
	fenced := false
	// A note can run over several lines of one paragraph: scan each
	// paragraph (lines between blank lines, outside fences) as one piece.
	var para strings.Builder
	paraStart := -1
	flush := func() {
		if paraStart >= 0 {
			for _, n := range notesInText(para.String()) {
				n.Start += paraStart
				n.End += paraStart
				out = append(out, n)
			}
		}
		para.Reset()
		paraStart = -1
	}
	for _, l := range lines {
		trim := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(trim, "```"):
			flush()
			fenced = !fenced
		case fenced:
		case trim == "":
			flush()
		default:
			if paraStart < 0 {
				paraStart = off
			}
			para.WriteString(l)
		}
		off += len(l)
	}
	flush()
	return out
}

// notesInText scans one paragraph, skipping inline code spans.
func notesInText(t string) []NoteSpan {
	var out []NoteSpan
	i := 0
	for i < len(t) {
		switch {
		case t[i] == '`':
			n := 0
			for i+n < len(t) && t[i+n] == '`' {
				n++
			}
			fence := strings.Repeat("`", n)
			if j := strings.Index(t[i+n:], fence); j >= 0 {
				i += n + j + n
			} else {
				i += n
			}
		case strings.HasPrefix(t[i:], "<<"):
			j := strings.Index(t[i+2:], ">>")
			if j < 0 {
				return out // unclosed: plain text
			}
			out = append(out, NoteSpan{Start: i, End: i + 2 + j + 2, Text: strings.TrimSpace(t[i+2 : i+2+j])})
			i += 2 + j + 2
		default:
			i++
		}
	}
	return out
}
