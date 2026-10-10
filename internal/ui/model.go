// Package ui is the Review Session TUI: one root Bubble Tea v2 model.
package ui

import (
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/highlight"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

type Focus int

const (
	FocusNav Focus = iota
	FocusCentre
	FocusPanel
)

type CentreView int

const (
	ViewBriefing CentreView = iota
	ViewDescription
	ViewFile
	ViewSince   // "Since last review": the diff from the last head reviewed
	ViewOutside // a file outside the PR, read-only at the head (a caller's or Citation's)
)

type PanelTab int

const (
	TabThreads PanelTab = iota
	TabFeedback
)

type Overlay int

const (
	OverlayNone Overlay = iota
	OverlayHelp
	OverlayTextbox
	OverlayConfirm
	OverlaySend
)

type TextboxKind int

const (
	TextboxAsk TextboxKind = iota
	TextboxFollowUp
	TextboxRefineAnswer // answering a questions-back Revision
)

// fbEditor is Feedback being written or edited in place, inside its card.
type fbEditor struct {
	fb    *session.Feedback
	isNew bool // cancelling a new, still-empty Feedback deletes it
	ta    textarea.Model

	rev bool // editing f's Revision, not its body
}

// Selection is a plain-text range over rendered rows, within one pane.
// Columns are rune offsets into each Row's content text.
type Selection struct {
	Active    bool
	Pane      Focus
	AnchorRow int
	AnchorCol int
	CursorRow int
	CursorCol int
	Keyboard  bool // v-mode: whole rows
	Dragging  bool
}

func (s Selection) Bounds() (r0, c0, r1, c1 int) {
	if s.AnchorRow < s.CursorRow || (s.AnchorRow == s.CursorRow && s.AnchorCol <= s.CursorCol) {
		r0, c0, r1, c1 = s.AnchorRow, s.AnchorCol, s.CursorRow, s.CursorCol
	} else {
		r0, c0, r1, c1 = s.CursorRow, s.CursorCol, s.AnchorRow, s.AnchorCol
	}
	if s.Keyboard {
		c0, c1 = 0, 1<<30
	}
	return
}

type clickState struct {
	X, Y  int
	Count int
	At    time.Time
	Pane  Focus
}

type navEntry struct {
	Kind   string // "briefing" "description" "file" "method"
	Path   string
	Method string // method key, for Kind=="method"
	Label  string
}

// zone is a clickable run of cells on one screen row, registered while
// rendering (tabs, action-bar items, dialog buttons).
type zone struct {
	X0, X1, Y int
	Fn        func(*Model) tea.Cmd
}

// Model is the root Bubble Tea model.
type Model struct {
	eng      *engine.Engine
	sess     *session.Session // eng.State(): read it, change it only through do
	repoMode bool

	holdOn       bool   // new Questions become Drafts
	startupNote  string // shown in the footer when the TUI starts
	started      bool
	lastStoreErr error
	hlNew, hlOld map[string]highlight.File // tokenised files, built on first use

	width, height int
	ready         bool
	dirty         bool // row lists need rebuilding before the next use

	focus      Focus
	centreView CentreView
	filePath   string

	navCursor   int
	navScroll   int
	navExpanded map[string]bool
	navEntries  []navEntry

	centreScroll int
	centreCursor int
	centreRows   []Row
	hunkMode     map[string]bool // path -> hunks (default true)
	gapExpanded  map[string]bool
	emph         map[string]map[int][2]int // path -> diff row -> changed rune range

	callersOpen map[string]bool        // method key -> its callers list is expanded
	callersAll  map[string]bool        // method key -> past "… N more"
	outside     map[string]outsideFile // files outside the PR, read at the head

	// The list pane under the navigator (Threads / Feedback).
	panelVisible bool
	panelTab     PanelTab
	panelScroll  int
	panelRows    []Row
	panelCursor  int

	mouseDown     bool
	mouseDownPane Focus
	mouseDownRow  int
	mouseDownCol  int

	expandedCards map[string]bool // Thread or Feedback ID -> expanded
	detailOpen    map[string]bool // "T4#1": that Answer's detail is expanded
	fbEdit        *fbEditor

	sel         Selection
	lastClick   clickState
	pending     pendingClick
	zones       []zone
	zoneDX      int    // the click's column within the zone it hit
	dialogZones []zone // relative to the open dialog's box
	dialogRect  rect

	flash    string
	flashSeq int

	overlay  Overlay
	tbKind   TextboxKind
	tbAnchor session.Anchor
	tbTarget *session.Thread
	ta       textarea.Model

	helpScroll int

	confirmMsg string
	confirmFn  func(*Model) tea.Cmd

	send   sendState
	sendTA textarea.Model

	revDiff    map[string]bool      // Feedback ID -> its Revision shows the old word diff (d)
	undo       undoStack            // the open text editor's undo history
	tbFeedback *session.Feedback    // TextboxRefineAnswer's Feedback
	askKind    session.QuestionKind // Ask…: Research or Before/After

	// Pane sizes the user dragged (0 = default). Clamped at layout time,
	// so a terminal resize keeps them within the minimums.
	leftW, listN int
	border       borderDrag
	hoverBorder  int // borderV / borderH while the pointer is over one

	search searchState

	headChecking bool   // a check of the PR's head is running
	updatingTo   string // an Update to this head is running

	// Jump history for the centre (Back / Forward).
	back, fwd []centreLoc
	jumpFrom  *centreLoc // where the jump in the current message started
}

// New builds a Model on an engine (opened, not yet started).
func New(eng *engine.Engine, repoMode bool) *Model {
	return &Model{
		eng: eng, sess: eng.State(), repoMode: repoMode,
		hlNew: map[string]highlight.File{}, hlOld: map[string]highlight.File{},
		navExpanded:   map[string]bool{},
		hunkMode:      map[string]bool{},
		gapExpanded:   map[string]bool{},
		emph:          map[string]map[int][2]int{},
		callersOpen:   map[string]bool{},
		callersAll:    map[string]bool{},
		outside:       map[string]outsideFile{},
		expandedCards: map[string]bool{},
		detailOpen:    map[string]bool{},
		revDiff:       map[string]bool{},
		panelVisible:  true,
		centreView:    ViewBriefing,
		focus:         FocusNav,
		dirty:         true,
	}
}

// Init starts the engine's agent work and listens for it.
func (m *Model) Init() tea.Cmd {
	if m.started {
		return nil
	}
	m.started = true
	cmds := []tea.Cmd{m.onEvents(m.eng.Start()), m.listen(), pollCmd(), m.fetchCommentsCmd(), headTickCmd(), m.checkHead()}
	if m.startupNote != "" {
		m.flash = m.startupNote
		m.flashSeq++
		seq := m.flashSeq
		cmds = append(cmds, tea.Tick(8*time.Second, func(time.Time) tea.Msg { return flashClearMsg{Seq: seq} }))
	}
	return tea.Batch(cmds...)
}

// Note shows a line in the footer for a few seconds after start (settings
// warnings, a read-only Review Session).
func (m *Model) Note(s string) { m.startupNote = s }

// highlighted is a file's tokenised new side (the old side for a deleted
// file) and old side, tokenised once.
func (m *Model) highlighted(path string) (newHl, oldHl highlight.File) {
	if h, ok := m.hlNew[path]; ok {
		return h, m.hlOld[path]
	}
	c := m.sess.Contents[path]
	if c.OldOK {
		oldHl = highlight.Tokenise(path, c.Old)
	}
	switch {
	case c.NewOK:
		newHl = highlight.Tokenise(path, c.New)
	case c.OldOK:
		newHl = oldHl
	}
	m.hlNew[path], m.hlOld[path] = newHl, oldHl
	return newHl, oldHl
}

type flashClearMsg struct{ Seq int }

func (m *Model) flashCmd(s string) tea.Cmd {
	m.flash = s
	m.flashSeq++
	seq := m.flashSeq
	return tea.Tick(2*time.Second, func(time.Time) tea.Msg { return flashClearMsg{Seq: seq} })
}

// rebuild recomputes every row list from session + view state.
func (m *Model) rebuild() {
	if m.width == 0 {
		return
	}
	m.buildNavEntries()
	m.buildCentreRows()
	m.findMatches()
	m.buildPanelRows()
	m.dirty = false
}

func (m *Model) ensureRows() {
	if m.dirty {
		m.rebuild()
	}
}
