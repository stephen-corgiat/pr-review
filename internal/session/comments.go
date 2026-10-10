package session

// Where PR Comments sit in the diff. A review comment sits on its line at
// the Review Session's head: GitHub gives its line on the PR's current head
// (CommitID) and its original line (OriginalCommit), so whichever of those
// is our head wins; otherwise the current line is a best guess. Replies sit
// with the comment they answer. General comments (issue comments, review
// bodies) and comments that can't be placed go at the top.

// CommentLine is where c sits at the Review Session's head: the line, on
// which side, and whether it's a guess (the comment is on another head).
func (s *Session) CommentLine(c *PRComment) (line int, old bool, guess bool) {
	old = c.Side == "LEFT"
	switch {
	case c.CommitID == s.PR.HeadSHA && c.Line > 0:
		return c.Line, old, false
	case c.OriginalCommit == s.PR.HeadSHA && c.OriginalLine > 0:
		return c.OriginalLine, old, false
	case c.Line > 0:
		return c.Line, old, true
	}
	// Outdated on another head: its original line would be a wild guess.
	return 0, old, true
}

// CommentRow is the diff row a review comment sits after, or -1.
func (s *Session) CommentRow(c *PRComment) int {
	if c.General() {
		return -1
	}
	fd := s.Files[c.Path]
	if fd == nil {
		return -1
	}
	line, old, _ := s.CommentLine(c)
	if line <= 0 {
		return -1
	}
	for i, l := range fd.All {
		if old && l.OldNo == line && l.Kind != LineAdded {
			return i
		}
		if !old && l.NewNo == line && l.Kind != LineRemoved {
			return i
		}
	}
	return -1
}

// CommentRoot follows replies up to the comment that started the
// conversation.
func (s *Session) CommentRoot(c *PRComment) *PRComment {
	for n := 0; c != nil && c.InReplyTo != "" && n < 50; n++ {
		p := s.CommentByID(c.InReplyTo)
		if p == nil {
			break
		}
		c = p
	}
	return c
}

// CommentReplies are the replies to root, oldest first (replies to
// replies included).
func (s *Session) CommentReplies(root *PRComment) []*PRComment {
	var out []*PRComment
	for _, c := range s.Comments {
		if c != root && c.InReplyTo != "" && s.CommentRoot(c) == root {
			out = append(out, c)
		}
	}
	return out
}

// CommentsOnRow are the conversations (root comments) placed after a diff
// row.
func (s *Session) CommentsOnRow(path string, row int) []*PRComment {
	var out []*PRComment
	for _, c := range s.Comments {
		if c.InReplyTo == "" && c.Path == path && s.CommentRow(c) == row {
			out = append(out, c)
		}
	}
	return out
}

// FileTopComments are a file's conversations that can't be placed on a
// line (outdated, or on lines this diff doesn't show).
func (s *Session) FileTopComments(path string) []*PRComment {
	var out []*PRComment
	for _, c := range s.Comments {
		if c.InReplyTo == "" && c.Path == path && s.CommentRow(c) < 0 {
			out = append(out, c)
		}
	}
	return out
}

// GeneralComments are the comments on the PR as a whole, plus review
// comments on files that aren't in the diff.
func (s *Session) GeneralComments() []*PRComment {
	var out []*PRComment
	for _, c := range s.Comments {
		if c.InReplyTo != "" {
			continue
		}
		if c.General() || s.Files[c.Path] == nil {
			out = append(out, c)
		}
	}
	return out
}
