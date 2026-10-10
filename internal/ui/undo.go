package ui

// Undo / redo for every text editor (Question, Feedback, Revision edit,
// summary): ctrl+z undoes, ctrl+y or ctrl+shift+z redoes. bubbles v2's
// textarea has no history of its own, so we snapshot value + cursor
// (Line(), Column()) before each change. Typing coalesces into one step per
// word: a space or newline, a run of deletes, or a paste ends a step.

import (
	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
)

const undoCap = 100

type taSnap struct {
	val      string
	row, col int
}

type undoStack struct {
	past, future []taSnap
	last         string // the kind of the last change: word, break, delete, paste, other
}

func snapOf(ta *textarea.Model) taSnap {
	return taSnap{val: ta.Value(), row: ta.Line(), col: ta.Column()}
}

// restore sets the value and puts the cursor back where it was: row by
// row from the top (CursorDown walks wrapped lines, Line() reports the
// logical one), then the column.
func restore(ta *textarea.Model, s taSnap) {
	ta.SetValue(s.val)
	ta.MoveToBegin()
	for i := 0; i < 10000 && ta.Line() < s.row; i++ {
		ta.CursorDown()
	}
	ta.SetCursorColumn(s.col)
}

func changeKind(msg tea.Msg) string {
	switch msg := msg.(type) {
	case tea.PasteMsg:
		return "paste"
	case tea.KeyPressMsg:
		switch msg.String() {
		case "space", "enter", "alt+enter", "ctrl+j":
			return "break"
		case "backspace", "delete", "ctrl+h", "ctrl+w", "alt+backspace", "ctrl+k", "ctrl+u", "alt+d", "ctrl+d":
			return "delete"
		}
		if msg.Text != "" {
			return "word"
		}
	}
	return "other"
}

// editTA runs msg through ta with undo/redo around it.
func (m *Model) editTA(ta *textarea.Model, msg tea.Msg) tea.Cmd {
	u := &m.undo
	if k, ok := msg.(tea.KeyPressMsg); ok {
		switch k.String() {
		case "ctrl+z":
			if n := len(u.past); n > 0 {
				u.future = append(u.future, snapOf(ta))
				restore(ta, u.past[n-1])
				u.past = u.past[:n-1]
			}
			u.last = ""
			return nil
		case "ctrl+y", "ctrl+shift+z":
			if n := len(u.future); n > 0 {
				u.past = append(u.past, snapOf(ta))
				restore(ta, u.future[n-1])
				u.future = u.future[:n-1]
			}
			u.last = ""
			return nil
		}
	}
	before := snapOf(ta)
	var cmd tea.Cmd
	*ta, cmd = ta.Update(msg)
	if ta.Value() == before.val {
		return cmd
	}
	kind := changeKind(msg)
	// A word keeps going through its letters, and the space before it joins
	// it; a run of deletes is one step.
	coalesce := (kind == "word" && (u.last == "word" || u.last == "break")) ||
		(kind == "delete" && u.last == "delete")
	if !coalesce {
		u.past = append(u.past, before)
		if len(u.past) > undoCap {
			u.past = u.past[len(u.past)-undoCap:]
		}
	}
	u.future = nil
	u.last = kind
	return cmd
}
