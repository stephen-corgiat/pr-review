package ui

// PR Comments: read-only cards at their lines (general ones at the top of
// the Description; ones that can't be placed at the top of their file).
// Their text can be selected and asked about, like any other Anchor.

import (
	"context"
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

var stComment = fg(cTeal)

func pcKey(c *session.PRComment) string { return "pc:" + c.ID }

// commentsMsg: the PR Comments arrived from the Forge.
type commentsMsg struct {
	cs  []forge.Comment
	err error
}

// fetchCommentsCmd fetches the PR Comments off the UI goroutine.
func (m *Model) fetchCommentsCmd() tea.Cmd {
	fetch := m.eng.CommentsFetcher()
	if fetch == nil {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cs, err := fetch(ctx)
		return commentsMsg{cs: cs, err: err}
	}
}

func (m *Model) handleComments(msg commentsMsg) tea.Cmd {
	if msg.err != nil {
		return m.flashCmd("PR Comments couldn't be fetched: " + oneLine(msg.err.Error()))
	}
	m.keepViewport(func() { m.eng.SetComments(msg.cs) })
	return nil
}

func commentKind(c *session.PRComment) string {
	switch c.Kind {
	case "review":
		return "review comment"
	case "review_body":
		return "review"
	}
	return "comment"
}

func commentDate(c *session.PRComment) string {
	t, err := time.Parse(time.RFC3339, c.CreatedAt)
	if err != nil {
		return c.CreatedAt
	}
	return t.Format("2 Jan 2006")
}

// commentWhere is " · outdated" or " · path:line" when the comment isn't
// sitting on its own line.
func (m *Model) commentWhere(c *session.PRComment, placed bool) string {
	if c.General() {
		return ""
	}
	line, _, guess := m.sess.CommentLine(c)
	switch {
	case !placed && line > 0:
		return fmt.Sprintf(" · %s:%d (not in this diff)", shortenPath(c.Path), line)
	case !placed:
		return " · outdated"
	case guess:
		return " · on another head"
	}
	return ""
}

// commentCardRows is a PR Comment conversation (the comment and its
// replies) as a card, followed by the Threads and Feedback anchored on it.
func (m *Model) commentCardRows(c *session.PRComment, indent string, paneW int, placed bool) []Row {
	cw := cardWidth(paneW, indent)
	rowW := cellWidth(indent) + cw
	base := Row{Kind: RowCard, DiffRow: -1, Comment: c, Width: rowW}
	replies := m.sess.CommentReplies(c)
	var rows []Row
	if !m.expandedCards[pcKey(c)] {
		head := []Span{sp(" ▸ ", stDim), sp("💬 ", stComment), sp("@"+c.Author, stBold), sp("  "+commentKind(c)+" · "+commentDate(c)+m.commentWhere(c, placed), stSub)}
		var tail []Span
		if n := len(replies); n > 0 {
			tail = append(tail, sp(fmt.Sprintf("  +%d repl%s", n, map[bool]string{true: "y", false: "ies"}[n == 1]), stComment))
		}
		room := cw - 1 - cellWidth(spansText(head)) - cellWidth(spansText(tail)) - 4
		r := base
		r.Prefix, r.BG = indent, bgCard
		r.Spans = append(append(head, sp("  “"+truncEllipsis(oneLine(c.Body), maxInt(3, room))+"”", stText)), tail...)
		r.Activate = func(mm *Model) tea.Cmd { mm.expandedCards[pcKey(c)] = true; return nil }
		rows = append(rows, r)
	} else {
		bst := fg(cTeal).Faint(true)
		label := []Span{sp("▾ ", stDim), sp("💬 ", stComment), sp("@"+c.Author, stBold), sp(" · "+commentKind(c)+" · "+commentDate(c)+m.commentWhere(c, placed)+" · read-only", stSub)}
		rows = append(rows, m.cardEdge(base, indent, bst, "╭─ ", label, "╮", cw, func(mm *Model) tea.Cmd {
			mm.expandedCards[pcKey(c)] = false
			return nil
		}))
		var body []Row
		for i, cc := range append([]*session.PRComment{c}, replies...) {
			tmpl := base
			tmpl.Selectable = true
			tmpl.Anchor = session.Anchor{Type: session.AnchorComment, Ref: cc.ID}
			if i > 0 {
				body = append(body, Row{Kind: RowCard, DiffRow: -1, Comment: c})
				h := tmpl
				h.Selectable = false
				body = append(body, textRow(h, []Span{sp("↳ ", stDim), sp("@"+cc.Author, stBold), sp(" · "+commentDate(cc), stSub)}))
			}
			body = append(body, markdownRows(cc.Body, cw-4, "", tmpl)...)
		}
		if c.URL != "" {
			body = append(body, Row{Kind: RowCard, DiffRow: -1, Comment: c}, textRow(base, []Span{sp(c.URL, stDim)}))
		}
		for _, r := range body {
			r.Prefix = indent + bst.Render("│") + " " + r.Prefix
			r.Suffix = " " + bst.Render("│")
			r.Width = rowW
			rows = append(rows, r)
		}
		rows = append(rows, m.cardEdge(base, indent, bst, "╰", nil, "╯", cw, nil))
	}
	// Threads and Feedback on this conversation sit right after it.
	ids := map[string]bool{c.ID: true}
	for _, r := range replies {
		ids[r.ID] = true
	}
	for _, t := range m.sess.Threads {
		if t.Anchor.Type == session.AnchorComment && ids[t.Anchor.Ref] {
			rows = append(rows, m.threadCardRows(t, indent+"  ", paneW)...)
		}
	}
	for _, f := range m.sess.Feedback {
		if f.Anchor.Type == session.AnchorComment && ids[f.Anchor.Ref] {
			rows = append(rows, m.feedbackCardRows(f, indent+"  ", paneW)...)
		}
	}
	return rows
}

// generalCommentRows are the conversations on the PR as a whole, under a
// small heading.
func (m *Model) generalCommentRows(indent string, paneW int) []Row {
	cs := m.sess.GeneralComments()
	if len(cs) == 0 {
		return nil
	}
	rows := []Row{{Kind: RowText, DiffRow: -1, Prefix: indent, Spans: []Span{sp(fmt.Sprintf("PR Comments (%d)", len(cs)), stComment.Bold(true)), sp("  read-only · enter expands", stDim)}}}
	for _, c := range cs {
		rows = append(rows, m.commentCardRows(c, indent, paneW, m.sess.Files[c.Path] == nil && c.General())...)
	}
	return append(rows, Row{Kind: RowText, DiffRow: -1})
}

// commentLocShort names a comment Anchor in lists.
func (m *Model) commentLoc(a session.Anchor) string {
	if c := m.sess.CommentByID(a.Ref); c != nil {
		return "💬 @" + c.Author
	}
	return "💬 comment"
}

// revealComment opens where a PR Comment sits and scrolls to its card.
func (m *Model) revealComment(id string) {
	c := m.sess.CommentByID(id)
	if c == nil {
		return
	}
	root := m.sess.CommentRoot(c)
	m.expandedCards[pcKey(root)] = true
	isCard := func(r Row) bool { return r.Kind == RowCard && r.Comment == root }
	if !root.General() && m.sess.Files[root.Path] != nil {
		m.openFile(root.Path)
		m.rebuild()
		if row := m.sess.CommentRow(root); row >= 0 && m.currentHunkMode(root.Path) && !m.hasDiffRow(row) {
			m.hunkMode[root.Path] = false
			m.rebuild()
		}
	} else if m.centreView != ViewDescription {
		m.centreView, m.centreCursor, m.centreScroll = ViewDescription, 0, 0
		m.rebuild()
	}
	for i, r := range m.centreRows {
		if isCard(r) {
			m.revealRow(i)
			return
		}
	}
}
