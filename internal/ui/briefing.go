package ui

import (
	"fmt"
	"path"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/stephen-corgiat/pr-review/internal/session"
)

// proseW is the reading width for Briefing and Description text: the pane,
// capped so lines stay readable on wide terminals.
func (m *Model) proseW() int { return minInt(m.centreBodyW(), 104) }

// briefingPendingText is what an agent-written Briefing part shows before
// it lands; -repo mode has no fixtures and so never runs a Briefing agent.
func (m *Model) briefingPendingText() string {
	switch {
	case m.repoMode:
		return "No Briefing agent in -repo mode"
	case m.sess.ReadOnly():
		return "No Briefing: the PR is " + m.sess.Finished + " and this Review Session is read-only"
	case !m.eng.HasAgent():
		return "No Briefing agent"
	case m.sess.BriefingError != "":
		return "The Briefing agent failed: " + oneLine(m.sess.BriefingError)
	}
	return "Briefing agent running…"
}

// doc accumulates the rows of a text view (Briefing, Description).
type doc struct {
	rows []Row
	w    int
}

func (d *doc) blank() {
	if n := len(d.rows); n > 0 && len(d.rows[n-1].Spans) == 0 {
		return
	}
	d.rows = append(d.rows, Row{Kind: RowText, DiffRow: -1})
}

func (d *doc) heading(title string) {
	if len(d.rows) > 0 {
		d.blank()
		d.blank2()
	}
	rule := strings.Repeat("─", maxInt(0, d.w-cellWidth(title)-3))
	d.rows = append(d.rows, Row{Kind: RowText, DiffRow: -1, Prefix: " ",
		Spans: []Span{sp(title, stHeading), sp(" "+rule, stFaint)}})
	d.blank()
}

// blank2 adds a second blank row (headings get more air than paragraphs).
func (d *doc) blank2() { d.rows = append(d.rows, Row{Kind: RowText, DiffRow: -1}) }

func (d *doc) subheading(title string, st lg.Style) {
	if n := len(d.rows); n > 0 && len(d.rows[n-1].Spans) != 0 {
		d.blank()
	}
	d.rows = append(d.rows, Row{Kind: RowText, DiffRow: -1, Prefix: "  ", Spans: []Span{sp(title, st)}})
}

func (d *doc) para(spans []Span, first, rest string, tmpl Row) {
	tmpl.Kind, tmpl.DiffRow = RowText, -1
	d.rows = append(d.rows, textRows(spans, d.w, first, rest, tmpl)...)
}

func briefRow(ref string, act func(*Model) tea.Cmd) Row {
	return Row{Selectable: true, Anchor: session.Anchor{Type: session.AnchorBriefing, Ref: ref}, Activate: act}
}

func pendingSpans(text string) []Span { return []Span{sp(text, stRunning.Italic(true))} }

func (m *Model) buildBriefingRows() []Row {
	b := m.sess.Briefing
	d := &doc{w: m.proseW()}

	// What changed since the last review, and whether this is Stale.
	if m.sess.PrevHead != "" {
		d.blank()
		r := Row{Activate: func(mm *Model) tea.Cmd { return mm.openSince() }}
		d.para([]Span{sp("Δ ", stStale), sp(m.sinceNote(), stSub)}, "  ", "    ", r)
	}
	if b.Stale {
		d.blank()
		d.para([]Span{sp("⚠ Stale: ", stStale.Bold(true)), sp("written at "+shortSHA(b.StaleFrom)+", before the PR moved to "+shortSHA(m.sess.PR.HeadSHA)+". "+m.briefingPendingText(), stSub)}, "  ", "    ", Row{})
	}

	// 1. TL;DR
	d.heading("TL;DR")
	if b.Ready && b.TLDR != "" {
		d.para([]Span{sp(b.TLDR, stText)}, "  ", "  ", briefRow("tldr", nil))
	} else {
		d.para(pendingSpans(m.briefingPendingText()), "  ", "  ", Row{})
	}

	// 2. Reading order
	d.heading("Reading order")
	for i, rs := range b.ReadingOrder {
		p := rs.Path
		open := func(mm *Model) tea.Cmd { mm.openFile(p); mm.focus = FocusCentre; return nil }
		fd := m.sess.Files[p]
		dir, base := path.Split(p)
		spans := []Span{sp(fmt.Sprintf("%2d  ", i+1), stDim), sp(dir, stSub), sp(base, stAccent)}
		if fd != nil {
			spans = append(spans, sp(fmt.Sprintf("  +%d", fd.Added), stAdded), sp(fmt.Sprintf(" −%d", fd.Removed), stRemoved))
		}
		spans = append(spans, m.markerSpans(m.threadsForFile(p), m.feedbackForFile(p))...)
		d.para(spans, "  ", "      ", briefRow("reading:"+p, open))
		if rs.Summary != "" {
			d.para([]Span{sp(rs.Summary, stText)}, "      ", "      ", briefRow("reading:"+p, open))
		} else {
			d.para(pendingSpans(m.briefingPendingText()), "      ", "      ", Row{})
		}
	}

	// 3. Contract Changes
	d.heading("Contract Changes")
	cats := []session.ContractCategory{session.CategoryEndpoint, session.CategoryExported, session.CategorySchema, session.CategoryConfig}
	for _, cat := range cats {
		var idx []int
		for i, c := range b.ContractChanges {
			// Agent-written items wait for the Briefing run.
			if c.Category == cat && (c.Computed || b.Ready) {
				idx = append(idx, i)
			}
		}
		if len(idx) == 0 {
			continue
		}
		d.subheading(cat.String(), stSubheading)
		for _, i := range idx {
			c := b.ContractChanges[i]
			ref := fmt.Sprintf("contract:%d", i)
			bullet := sp("• ", stSubheading)
			if c.Computed {
				bullet = sp("⚙ ", stGear)
			}
			d.para([]Span{bullet, sp(c.Title, stBold)}, "    ", "      ", briefRow(ref, nil))
			if c.Detail != "" {
				d.para([]Span{sp(c.Detail, stSub)}, "      ", "      ", briefRow(ref, nil))
			}
			if len(c.Anchors) > 0 {
				spans := []Span{sp("→ ", stDim)}
				var links []Link
				pos := 2
				for j, a := range c.Anchors {
					if j > 0 {
						spans = append(spans, sp(" · ", stDim))
						pos += 3
					}
					loc := a.Path
					if a.Line > 0 {
						loc = fmt.Sprintf("%s:%d", a.Path, a.Line)
					}
					aa := a
					links = append(links, Link{Start: pos, End: pos + len([]rune(loc)), Act: func(mm *Model) tea.Cmd {
						return mm.jumpToCitation(session.Citation{Path: aa.Path, Line: aa.Line})
					}})
					spans = append(spans, sp(loc, stLink))
					pos += len([]rune(loc))
				}
				r := briefRow(ref, nil)
				r.Links = links
				d.para(spans, "      ", "        ", r)
			}
		}
	}
	if !b.Ready {
		d.blank()
		d.para(pendingSpans(m.briefingPendingText()+" (Endpoints, Config and env)"), "  ", "  ", Row{})
	}

	// 4. Touched Methods
	d.heading("Touched Methods")
	if len(m.sess.Methods) == 0 {
		d.para([]Span{sp("No Touched Methods (Go, TypeScript, TSX and Ruby files only).", stDim)}, "  ", "  ", Row{})
	}
	for _, p := range m.sess.FileOrder() {
		keys := m.sess.MethodsByFile[p]
		if len(keys) == 0 {
			continue
		}
		d.subheading(p, stSubheading)
		for _, k := range keys {
			meth := m.sess.Methods[k]
			key := k
			ref := "method:" + k
			jump := func(mm *Model) tea.Cmd { mm.jumpToMethod(key); mm.focus = FocusCentre; return nil }
			status := map[session.MethodStatus]string{session.MethodAdded: "added", session.MethodRemoved: "removed", session.MethodChanged: "changed"}[meth.Status]
			spans := []Span{sp("ƒ ", stMethod), sp(meth.DisplayName(), stMethodBold), sp("  "+status, stDim)}
			spans = append(spans, m.briefingCallers(k)...)
			spans = append(spans, m.markerSpans(m.threadsForMethod(k), nil)...)
			d.para(spans, "    ", "      ", briefRow(ref, jump))
			for _, part := range [][2]string{{"does", meth.Does}, {"changed", meth.Changed}} {
				label := sp(part[0]+strings.Repeat(" ", 9-len(part[0])), stMethod)
				if part[1] != "" {
					d.para([]Span{label, sp(part[1], stText)}, "      ", "               ", briefRow(ref, nil))
				} else if b.Ready {
					d.para([]Span{label, sp("the Briefing didn't say", stDim.Italic(true))}, "      ", "               ", Row{})
				} else {
					d.para(append([]Span{label}, pendingSpans(m.briefingPendingText())...), "      ", "               ", Row{})
				}
			}
			d.blank()
		}
	}

	// 5. Call map
	d.heading("Call map")
	m.callMapRows(d)

	return m.attachTextCards(d.rows, false)
}

// markerSpans are the "●2 ✎1" counts shown after a heading-like row.
func (m *Model) markerSpans(ts []*session.Thread, fbs []*session.Feedback) []Span {
	var out []Span
	if n, st := threadMarker(ts); n > 0 {
		out = append(out, sp(fmt.Sprintf("  ●%d", n), st))
	}
	if len(fbs) > 0 {
		out = append(out, sp(fmt.Sprintf("  ✎%d", len(fbs)), stFeedback))
	}
	return out
}

func (m *Model) callMapRows(d *doc) {
	var roots, notConnected []string
	for k, meth := range m.sess.Methods {
		switch {
		case len(meth.CalleeKeys) > 0 && len(meth.CallerKeys) == 0:
			roots = append(roots, k)
		case len(meth.CalleeKeys) == 0 && len(meth.CallerKeys) == 0:
			notConnected = append(notConnected, k)
		}
	}
	byName := func(keys []string) {
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && m.sess.Methods[keys[j-1]].DisplayName() > m.sess.Methods[keys[j]].DisplayName(); j-- {
				keys[j-1], keys[j] = keys[j], keys[j-1]
			}
		}
	}
	byName(roots)
	byName(notConnected)
	seen := map[string]bool{}
	var walk func(key, lead, branch string)
	walk = func(key, lead, branch string) {
		meth := m.sess.Methods[key]
		k := key
		jump := func(mm *Model) tea.Cmd { mm.jumpToMethod(k); mm.focus = FocusCentre; return nil }
		spans := []Span{sp(lead+branch, stFaint), sp(meth.DisplayName(), stLink)}
		if seen[key] {
			spans = append(spans, sp("  ↺ above", stDim))
		}
		r := briefRow("callmap", jump)
		r.Anchor.Text = meth.DisplayName()
		d.para(spans, "    ", "    ", r)
		if seen[key] {
			return
		}
		seen[key] = true
		for i, ck := range meth.CalleeKeys {
			next := lead
			switch branch {
			case "├─ ":
				next += "│  "
			case "└─ ":
				next += "   "
			}
			if i == len(meth.CalleeKeys)-1 {
				walk(ck, next, "└─ ")
			} else {
				walk(ck, next, "├─ ")
			}
		}
	}
	for i, k := range roots {
		if i > 0 {
			d.blank()
		}
		walk(k, "", "")
	}
	if len(roots) == 0 {
		d.para([]Span{sp("No calls between Touched Methods.", stDim)}, "  ", "  ", Row{})
	}
	if len(notConnected) > 0 {
		d.blank()
		spans := []Span{sp("Not connected: ", stDim)}
		var links []Link
		pos := len([]rune("Not connected: "))
		for i, k := range notConnected {
			if i > 0 {
				spans = append(spans, sp(" · ", stDim))
				pos += 3
			}
			name := m.sess.Methods[k].DisplayName()
			kk := k
			links = append(links, Link{Start: pos, End: pos + len([]rune(name)), Act: func(mm *Model) tea.Cmd {
				mm.jumpToMethod(kk)
				mm.focus = FocusCentre
				return nil
			}})
			spans = append(spans, sp(name, stLink))
			pos += len([]rune(name))
		}
		r := briefRow("callmap", nil)
		r.Links = links
		d.para(spans, "  ", "  ", r)
	}
}

// ---- Description ----

func (m *Model) buildDescriptionRows() []Row {
	pr := m.sess.PR
	d := &doc{w: m.proseW()}
	d.rows = append(d.rows, Row{Kind: RowText, DiffRow: -1})
	d.para([]Span{sp(pr.Title, stBold)}, "  ", "  ", Row{})
	var meta []Span
	if m.repoMode {
		meta = []Span{sp(fmt.Sprintf("%s..%s", pr.BaseRef, pr.HeadRef), stDim)}
	} else {
		meta = []Span{
			sp(fmt.Sprintf("#%d", pr.Number), stAccent), sp("  ·  ", stFaint), sp(pr.Author, stText),
			sp("  ·  ", stFaint), sp(pr.HeadRef, stMethod), sp(" → ", stDim), sp(pr.BaseRef, stMethod),
			sp("  ·  ", stFaint), sp("merged "+pr.MergedAt, stSub),
		}
	}
	d.para(meta, "  ", "  ", Row{})
	if pr.URL != "" {
		d.para([]Span{sp(pr.URL, stDim)}, "  ", "  ", Row{})
	}
	d.rows = append(d.rows, Row{Kind: RowText, DiffRow: -1, Prefix: "  ", Spans: []Span{sp(strings.Repeat("─", maxInt(0, d.w-3)), stFaint)}})
	d.blank()
	// PR Comments on the PR as a whole come first, then Threads on it.
	if cs := m.generalCommentRows("  ", m.centreBodyW()); len(cs) > 0 {
		d.rows = append(d.rows, cs...)
	}
	for _, t := range m.sess.Threads {
		if t.Anchor.Type == session.AnchorPR {
			d.rows = append(d.rows, m.threadCardRows(t, "  ", m.centreBodyW())...)
		}
	}
	for _, f := range m.sess.Feedback {
		if f.Anchor.Type == session.AnchorPR {
			d.rows = append(d.rows, m.feedbackCardRows(f, "  ", m.centreBodyW())...)
		}
	}
	tmpl := Row{Kind: RowText, DiffRow: -1, Selectable: true, Anchor: session.Anchor{Type: session.AnchorDescription}}
	d.rows = append(d.rows, markdownRows(pr.Description, d.w, "  ", tmpl)...)
	return m.attachTextCards(d.rows, true)
}

// ---- cards on text views ----

// attachTextCards places the cards of Threads and Feedback anchored on this
// text view (Description, or Briefing sections) after the paragraph holding
// their text, and marks the row in the marker column.
func (m *Model) attachTextCards(rows []Row, description bool) []Row {
	type item struct {
		t *session.Thread
		f *session.Feedback
	}
	after := map[int][]item{}
	place := func(a session.Anchor, it item) {
		match := func(r Row) bool {
			if description {
				return r.Anchor.Type == session.AnchorDescription
			}
			return r.Anchor.Type == session.AnchorBriefing && r.Anchor.Ref == a.Ref
		}
		i := locateText(rows, match, a.Text)
		if i < 0 && !description {
			cat := strings.SplitN(a.Ref, ":", 2)[0]
			i = locateText(rows, func(r Row) bool {
				return r.Anchor.Type == session.AnchorBriefing && strings.SplitN(r.Anchor.Ref, ":", 2)[0] == cat
			}, a.Text)
		}
		if i < 0 {
			return
		}
		if it.f != nil {
			rows[i].Marker, rows[i].MarkerStyle = "✎", stFeedback
		} else if rows[i].Marker != "✎" {
			_, st := threadMarker([]*session.Thread{it.t})
			if rows[i].Marker == "●" {
				// keep the more urgent colour already there
				_, st = threadMarker(append(rows[i].markerThreads, it.t))
			}
			rows[i].Marker, rows[i].MarkerStyle = "●", st
			rows[i].markerThreads = append(rows[i].markerThreads, it.t)
		}
		for i+1 < len(rows) && rows[i+1].Join == JoinSoft {
			i++
		}
		after[i] = append(after[i], it)
	}
	want := session.AnchorBriefing
	if description {
		want = session.AnchorDescription
	}
	for _, t := range m.sess.Threads {
		if t.Anchor.Type == want {
			place(t.Anchor, item{t: t})
		}
	}
	for _, f := range m.sess.Feedback {
		if f.Anchor.Type == want {
			place(f.Anchor, item{f: f})
		}
	}
	if len(after) == 0 {
		return rows
	}
	w := m.centreBodyW()
	out := make([]Row, 0, len(rows)+len(after)*2)
	for i, r := range rows {
		out = append(out, r)
		for _, it := range after[i] {
			if it.t != nil {
				out = append(out, m.threadCardRows(it.t, "    ", w)...)
			} else {
				out = append(out, m.feedbackCardRows(it.f, "    ", w)...)
			}
		}
	}
	return out
}

// locateText finds the row (among those match accepts) that holds the end
// of text; if text isn't found, the last matching row.
func locateText(rows []Row, match func(Row) bool, text string) int {
	var cands []int
	for i, r := range rows {
		if match(r) {
			cands = append(cands, i)
		}
	}
	if len(cands) == 0 {
		return -1
	}
	needle := strings.TrimSpace(text)
	if i := strings.LastIndexByte(needle, '\n'); i >= 0 {
		needle = strings.TrimSpace(needle[i+1:])
	}
	try := func(n string) int {
		if n == "" {
			return -1
		}
		for _, i := range cands {
			if strings.Contains(rows[i].text(), n) {
				return i
			}
		}
		return -1
	}
	if i := try(needle); i >= 0 {
		return i
	}
	if rs := []rune(needle); len(rs) > 12 {
		if i := try(string(rs[len(rs)-12:])); i >= 0 {
			return i
		}
		if i := try(string(rs[:12])); i >= 0 {
			return i
		}
	}
	return cands[len(cands)-1]
}
