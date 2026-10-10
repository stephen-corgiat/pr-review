package ui

// New heads: the TUI polls the PR's head through the engine (which decides
// when: every 60 s while you're active, paused when idle, at once on your
// first input after a pause and when the terminal regains focus), shows a
// banner, and updates only when you press U. S shows "Since last review".

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/highlight"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

var stStale = fg(cPeach)

// headTickMsg asks the engine whether a head check is due.
type headTickMsg struct{}

const headTick = 5 * time.Second

func headTickCmd() tea.Cmd {
	return tea.Tick(headTick, func(time.Time) tea.Msg { return headTickMsg{} })
}

// headMsg is a finished head check.
type headMsg struct {
	c   engine.HeadCheck
	err error
}

// checkHead starts a head check unless one is running.
func (m *Model) checkHead() tea.Cmd {
	if m.headChecking {
		return nil
	}
	run := m.eng.HeadChecker()
	if run == nil {
		return nil
	}
	m.headChecking = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := run(ctx)
		return headMsg{c: c, err: err}
	}
}

func (m *Model) handleHead(msg headMsg) tea.Cmd {
	m.headChecking = false
	if msg.err != nil {
		return nil // offline, say: the next check tries again
	}
	if m.eng.SawHead(msg.c) {
		m.dirty = true
	}
	return nil
}

// noteInput tells the engine the Reviewer is active: the first input after
// a pause checks the head at once.
func (m *Model) noteInput() tea.Cmd {
	if m.eng.NoteInput() {
		return m.checkHead()
	}
	return nil
}

// ---- the banner ----

// bannerH is the banner's height: one row when there's news about the
// head, else none.
func (m *Model) bannerH() int {
	if m.updatingTo != "" || m.sess.NewHead() || m.sess.Unpushed > 0 {
		return 1
	}
	return 0
}

func (m *Model) renderBanner(w int) string {
	st := lg.NewStyle().Foreground(lg.Color(cCrust)).Background(lg.Color(cPeach)).Bold(true)
	var parts []string
	switch {
	case m.updatingTo != "":
		parts = append(parts, st.Render(" ↻ Updating to "+shortSHA(m.updatingTo)+"… "))
	case m.sess.NewHead():
		n := ""
		if m.sess.RemoteCommits > 0 {
			n = fmt.Sprintf(" (%d)", m.sess.RemoteCommits)
		}
		parts = append(parts, st.Render(" ↑ New commits"+n+" ")+stText.Render(" ")+stKey.Render("U")+stText.Render(" to update"))
	}
	if u := m.sess.Unpushed; u > 0 {
		hint := fmt.Sprintf("%d local commit%s not pushed", u, plural(u))
		if !m.sess.NewHead() && m.updatingTo == "" {
			hint += " · only pushed commits can be reviewed"
		}
		parts = append(parts, stSubheading.Render(hint))
	}
	line := " " + strings.Join(parts, stFaint.Render("  ·  "))
	return paintBG(truncEllipsisANSI(line, w), cSurface0, w)
}

// ---- Update ----

// updatedMsg: the slow part of an Update finished (or failed).
type updatedMsg struct {
	p   *engine.UpdatePrep
	err error
}

func (m *Model) startUpdate() tea.Cmd {
	if !m.sess.NewHead() {
		// Nothing seen yet: U checks now (the banner comes if there is).
		return tea.Batch(m.flashCmd("No new commits: the Review Session is at the PR's head (checking again…)"), m.checkHead())
	}
	run, err := m.eng.BeginUpdate()
	if err != nil {
		return m.flashCmd(err.Error())
	}
	m.updatingTo = m.sess.RemoteHead
	m.dirty = true
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		p, err := run(ctx)
		return updatedMsg{p: p, err: err}
	}
}

func (m *Model) handleUpdated(msg updatedMsg) tea.Cmd {
	m.updatingTo = ""
	m.dirty = true
	if msg.err != nil {
		return tea.Batch(m.onEvents(m.eng.AbortUpdate()), m.flashCmd("Update failed: "+oneLine(msg.err.Error())))
	}
	evs, sum, err := m.eng.ApplyUpdate(msg.p)
	// The files' contents changed: drop what was worked out from them.
	m.hlNew, m.hlOld = map[string]highlight.File{}, map[string]highlight.File{}
	m.emph, m.gapExpanded = map[string]map[int][2]int{}, map[string]bool{}
	m.sel = Selection{}
	m.outside = map[string]outsideFile{}
	if (m.centreView == ViewFile && m.sess.Files[m.filePath] == nil) || m.centreView == ViewOutside {
		m.centreView, m.filePath = ViewBriefing, ""
	}
	m.centreCursor, m.centreScroll = 0, 0
	cmd := m.onEvents(evs)
	if err != nil {
		return tea.Batch(cmd, m.flashCmd("Update failed: "+err.Error()))
	}
	note := fmt.Sprintf("Updated to %s: %d Anchor%s moved, %d Stale, %d Answer%s Stale · S shows what changed", shortSHA(m.sess.PR.HeadSHA),
		sum.Moved, plural(sum.Moved), sum.Stale, sum.StaleAnswers, plural(sum.StaleAnswers))
	return tea.Batch(cmd, m.flashCmd(note), m.fetchCommentsCmd())
}

// ---- re-asking ----

func (m *Model) reask(t *session.Thread) tea.Cmd {
	if t == nil {
		return nil
	}
	if !t.Stale() {
		return m.flashCmd(t.ID + " isn't Stale: R asks a follow-up")
	}
	m.expandedCards[t.ID] = true
	return tea.Batch(m.do(engine.Reask{ThreadID: t.ID}), m.flashCmd(t.ID+" asked again at "+shortSHA(m.sess.PR.HeadSHA)))
}

// ---- Stale presentation ----

// staleAnchorRows says an Anchor went Stale, quoting what it was on.
func (m *Model) staleAnchorRows(a session.Anchor, w int, tmpl Row) []Row {
	if !a.Stale {
		return nil
	}
	what := "its lines changed"
	if a.Type == session.AnchorPR && a.Path != "" {
		what = shortenPath(a.Path) + " left the PR"
	}
	spans := []Span{sp("⚠ Stale: ", stStale.Bold(true)), sp(what+" after "+shortSHA(a.StaleFrom)+". It was on ", stSub)}
	if q := oneLine(a.Text); q != "" {
		spans = append(spans, sp("“"+truncEllipsis(q, 160)+"”", stText))
	}
	return textRows(spans, w, "", "  ", tmpl)
}

// ---- "Since last review" ----

// changedSince: the file changed since the head the Reviewer updated from.
func (m *Model) changedSince(path string) bool {
	return m.sess.PrevHead != "" && m.sess.Since[path] != nil
}

// methodChangedSince: lines of the method changed since the last review.
func (m *Model) methodChangedSince(meth *session.Method) bool {
	fd := m.sess.Since[meth.Path]
	if m.sess.PrevHead == "" || fd == nil || meth.NewStart == 0 {
		return false
	}
	for _, l := range fd.All {
		if l.Kind == session.LineAdded && l.NewNo >= meth.NewStart && l.NewNo <= meth.NewEnd {
			return true
		}
	}
	// Lines removed from inside it.
	for i, l := range fd.All {
		if l.Kind != session.LineRemoved {
			continue
		}
		for j := i + 1; j < len(fd.All); j++ {
			if n := fd.All[j].NewNo; n > 0 {
				if n > meth.NewStart && n <= meth.NewEnd+1 {
					return true
				}
				break
			}
		}
	}
	return false
}

// sinceNote is the Briefing's one line about what changed since.
func (m *Model) sinceNote() string {
	s := m.sess
	add, rem := 0, 0
	for _, fd := range s.Since {
		add += fd.Added
		rem += fd.Removed
	}
	return fmt.Sprintf("Since your last review (%s): %d commit%s · %d file%s · +%d −%d · S shows the diff",
		shortSHA(s.PrevHead), s.UpdateCommits, plural(s.UpdateCommits), len(s.Since), plural(len(s.Since)), add, rem)
}

func (m *Model) openSince() tea.Cmd {
	if m.sess.PrevHead == "" {
		return m.flashCmd("Nothing to compare yet: “Since last review” comes after an Update (U)")
	}
	m.markJump()
	if m.centreView != ViewSince {
		m.centreView, m.filePath = ViewSince, ""
		m.centreCursor, m.centreScroll = 0, 0
		m.sel = Selection{}
	}
	m.focus = FocusCentre
	m.dirty = true
	return nil
}

// buildSinceRows is the diff from the last head reviewed to this one,
// file by file in hunks; enter on a line opens it in the file's diff.
func (m *Model) buildSinceRows() []Row {
	s := m.sess
	w := m.centreBodyW()
	var rows []Row
	blank := Row{Kind: RowText, DiffRow: -1}
	rows = append(rows, blank, Row{Kind: RowText, DiffRow: -1, Prefix: " ", Spans: []Span{sp(m.sinceNote(), stSub)}}, blank)
	for _, p := range s.SinceOrder {
		fd := s.Since[p]
		pp := p
		open := func(mm *Model) tea.Cmd {
			if mm.sess.Files[pp] == nil {
				return mm.flashCmd(pp + " isn't in the PR any more")
			}
			mm.openFile(pp)
			return nil
		}
		gst := stModified
		switch fd.Status {
		case session.FileAdded:
			gst = stAdded
		case session.FileDeleted:
			gst = stRemoved
		}
		head := []Span{sp(strings.Replace(fd.Status.Glyph(), "-", "−", 1)+" ", gst), sp(p, stBold), sp(fmt.Sprintf("  +%d", fd.Added), stAdded), sp(fmt.Sprintf(" −%d", fd.Removed), stRemoved)}
		if s.Files[p] == nil {
			head = append(head, sp("  (no longer in the PR)", stDim))
		}
		rule := strings.Repeat("─", maxInt(0, w-cellWidth(spansText(head))-3))
		rows = append(rows, Row{Kind: RowText, DiffRow: -1, Prefix: " ", Spans: append(head, sp(" "+rule, stFaint)), Activate: open})
		if fd.Binary {
			rows = append(rows, Row{Kind: RowText, DiffRow: -1, Prefix: "   ", Spans: []Span{sp("binary file changed", stDim)}}, blank)
			continue
		}
		numW, _ := fileGutter(fd)
		for hi, h := range fd.Hunks {
			if hi > 0 {
				rows = append(rows, Row{Kind: RowGap, DiffRow: -1, Prefix: " ", BG: cMantle, Spans: []Span{sp("⋯", stDim)}})
			}
			for i := h.Start; i < h.End; i++ {
				rows = append(rows, m.sinceLineRows(p, fd.All[i], numW, w)...)
			}
		}
		rows = append(rows, blank)
	}
	return rows
}

func (m *Model) sinceLineRows(path string, l session.DiffLine, numW, w int) []Row {
	num := func(n int) string {
		if n <= 0 {
			return strings.Repeat(" ", numW)
		}
		return fmt.Sprintf("%*d", numW, n)
	}
	sign, bg, st := " ", "", stFaint
	switch l.Kind {
	case session.LineAdded:
		sign, bg, st = "+", bgAdded, stAdded.Bold(true)
	case session.LineRemoved:
		sign, bg, st = "−", bgRemoved, stRemoved.Bold(true)
	}
	gst := func(x lg.Style) lg.Style {
		if bg != "" {
			return x.Background(lg.Color(bg))
		}
		return x
	}
	gutter := gst(stFaint).Render(num(l.OldNo)+" "+num(l.NewNo)+" ") + gst(st).Render(sign) + gst(lg.NewStyle()).Render(" ")
	r := Row{Kind: RowCode, Prefix: gutter, BG: bg, DiffRow: -1}
	if l.NewNo > 0 {
		line := l.NewNo
		r.Activate = func(mm *Model) tea.Cmd {
			if !mm.gotoLine(path, line) {
				return mm.flashCmd(path + " isn't in the PR any more")
			}
			return nil
		}
	}
	parts := hardWrapSpans(expandTabs([]Span{sp(l.Text, stText)}), w-cellWidth(gutter))
	out := make([]Row, 0, len(parts))
	for i, p := range parts {
		rr := r
		rr.Spans = p
		if i > 0 {
			rr.Prefix, rr.Join, rr.Activate = strings.Repeat(" ", cellWidth(gutter)), JoinHard, nil
		}
		out = append(out, rr)
	}
	return out
}
