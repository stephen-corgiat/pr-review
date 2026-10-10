package session

// Mapping lines from one head to the next (Update): a file's hunks from
// `git diff -U0 A B`. Lines outside every hunk move by the hunks before
// them; lines inside one were edited, and an insertion inside a range
// edits the range too.

// LineHunk is one hunk of a zero-context diff: OldCount lines at OldStart
// became NewCount lines at NewStart. A count of 0 is a pure insertion
// (after line OldStart) or deletion (after line NewStart).
type LineHunk struct {
	OldStart, OldCount int
	NewStart, NewCount int
}

// LineMap is a file's hunks from one commit to another, in order.
type LineMap []LineHunk

// Line maps one line. edited: the line itself changed; its new number is
// then a best guess inside the hunk that replaced it.
func (lm LineMap) Line(l int) (n int, edited bool) {
	shift := 0
	for _, h := range lm {
		if h.OldCount == 0 { // inserted after line OldStart
			if h.OldStart < l {
				shift += h.NewCount
				continue
			}
			break
		}
		end := h.OldStart + h.OldCount - 1
		switch {
		case end < l:
			shift += h.NewCount - h.OldCount
		case h.OldStart <= l:
			guess := h.NewStart
			if h.NewCount > 0 {
				guess += min(l-h.OldStart, h.NewCount-1)
			}
			return max(1, guess), true
		default:
			return l + shift, false
		}
	}
	return l + shift, false
}

// Changed reports whether any line in [from, to] was edited, or lines were
// inserted between two of them.
func (lm LineMap) Changed(from, to int) bool {
	if to < from {
		to = from
	}
	for _, h := range lm {
		if h.OldCount == 0 {
			if h.OldStart >= from && h.OldStart < to {
				return true
			}
			continue
		}
		if h.OldStart <= to && h.OldStart+h.OldCount-1 >= from {
			return true
		}
	}
	return false
}

// Range maps [from, to]: its new bounds (a best guess when edited) and
// whether it was edited.
func (lm LineMap) Range(from, to int) (nf, nt int, edited bool) {
	if to < from {
		to = from
	}
	nf, e1 := lm.Line(from)
	nt, e2 := lm.Line(to)
	if nt < nf {
		nt = nf
	}
	return nf, nt, e1 || e2 || lm.Changed(from, to)
}
