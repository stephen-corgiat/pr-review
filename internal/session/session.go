package session

import (
	"fmt"
	"path"
	"path/filepath"
	"sort"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis"
	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/gocallers"
	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/golang"
	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/treesitter"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
)

const MaxRunning = 3

// FileContents is one changed file at the base and the head.
type FileContents struct {
	Old, New     string
	OldOK, NewOK bool // the file exists on that side
}

// Session is the whole state of one Review Session: the Pull Request's
// diff and Touched Methods (computed from git when it opens, never logged)
// plus everything the event log records. Only Apply changes the logged
// part.
type Session struct {
	PR           PullRequest
	RepoMode     bool
	Fixture      bool   // fixture mode: the canned agent
	Checkout     string // the Reviewer's checkout it was opened from
	WorktreePath string
	ScratchPath  string
	Model        string
	Effort       string
	// Finished is "merged" or "closed" once the PR finished after the
	// Review Session was created: its worktree is gone and it's read-only.
	Finished string

	Files        map[string]*FileDiff
	FileGitOrder []string // git diff order, for use until the Briefing lands
	ReadingOrder []string // set once the Briefing's agent parts land
	Contents     map[string]FileContents

	Briefing      *Briefing
	BriefingError string  // the Briefing run failed
	BriefingRun   RunInfo // the Briefing's agent session, which Questions fork
	Methods       map[string]*Method
	MethodsByFile map[string][]string // path -> method keys, in file order
	DeclLines     map[string][]int    // analysed files: new-side first line of each declaration ]/[ stops on

	// Comments are the PR Comments, oldest first (not logged).
	Comments []*PRComment

	// The Go Touched Methods' exact callers at the head (not logged):
	// see callers.go.
	CallersState CallersStatus
	Callers      gocallers.Result
	CallersErr   string

	Threads         []*Thread
	Feedback        []*Feedback
	threadSeq       int
	usedFeedbackIDs map[string]bool

	// TotalCost is every agent run's own cost added up.
	TotalCost float64

	// PrevHead is the head the Reviewer last updated from (Update), and
	// UpdateCommits the commits since it; "" before any Update.
	PrevHead      string
	UpdateCommits int
	// Since is the diff PrevHead..head ("Since last review"), computed
	// from git like the diff (never logged); SinceOrder its git order.
	Since      map[string]*FileDiff
	SinceOrder []string

	// What the last check of the PR's head found (not logged): the head on
	// GitHub, how many commits it's ahead of ours, and how many commits
	// the local branch has that aren't pushed.
	RemoteHead    string
	RemoteCommits int
	Unpushed      int

	lastNotes *BriefingNotes // the latest Briefing's agent-written parts

	// ReviewerIsAuthor: the Forge's authenticated user opened the PR. The
	// send dialog defaults to Hand to agent, and GitHub allows only a
	// Comment review. Found when the engine opens (never logged).
	ReviewerIsAuthor bool
	// ReviewerLogin is the Forge's authenticated user ("" when unknown).
	ReviewerLogin string
}

// ReadOnly: no agent runs and nothing can be sent.
func (s *Session) ReadOnly() bool { return s.Finished != "" }

// PRState is open, merged or closed (open when unknown).
func (s *Session) PRState() string {
	if s.PR.State == "" {
		return "open"
	}
	return s.PR.State
}

// CanSend: Feedback can go to GitHub or the Implementing Agent only while
// the PR is open.
func (s *Session) CanSend() bool { return s.PRState() == "open" && !s.ReadOnly() }

func (s *Session) threadByID(id string) *Thread {
	for _, t := range s.Threads {
		if t.ID == id {
			return t
		}
	}
	return nil
}

// CommentByID finds a PR Comment.
func (s *Session) CommentByID(id string) *PRComment {
	for _, c := range s.Comments {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// ThreadByID is the exported lookup.
func (s *Session) ThreadByID(id string) *Thread { return s.threadByID(id) }

func (s *Session) removeThread(id string) {
	for i, t := range s.Threads {
		if t.ID == id {
			s.Threads = append(s.Threads[:i], s.Threads[i+1:]...)
			return
		}
	}
}

// Counts summarises header/overview badges.
func (s *Session) Counts() Counts {
	c := Counts{MaxRunning: MaxRunning}
	for _, t := range s.Threads {
		if t.Unread {
			c.New++
		}
		switch t.Status {
		case StatusFailed:
			c.Failed++
		case StatusDraft:
			c.Drafts++
		case StatusRunning:
			c.Running++
		case StatusQueued:
			c.Queued++
		}
	}
	for _, f := range s.Feedback {
		switch f.Refine {
		case RefineRunning:
			c.Running++
		case RefineQueued:
			c.Queued++
		}
	}
	return c
}

// NextThreadID is the ID the next new Thread gets.
func (s *Session) NextThreadID() string { return fmt.Sprintf("T%d", s.threadSeq+1) }

// NextFeedbackID is the lowest ID never used, deleted Feedback included
// (the fixture seeds are F1, F2, F4, so the first new one is F3).
func (s *Session) NextFeedbackID() string {
	for n := 1; ; n++ {
		id := fmt.Sprintf("F%d", n)
		if !s.usedFeedbackIDs[id] {
			return id
		}
	}
}

// NewHead: the PR's head on GitHub is past the Review Session's.
func (s *Session) NewHead() bool { return s.RemoteHead != "" && s.RemoteHead != s.PR.HeadSHA }

// ReplaceDiff swaps in the diff, Touched Methods and computed Briefing of
// n (built at a new head), in place: the client holds this *Session.
func (s *Session) ReplaceDiff(n *Session) {
	s.Files, s.FileGitOrder, s.Contents = n.Files, n.FileGitOrder, n.Contents
	s.Methods, s.MethodsByFile, s.DeclLines = n.Methods, n.MethodsByFile, n.DeclLines
	s.Briefing, s.ReadingOrder = n.Briefing, nil
}

// SetSince sets the "Since last review" diff.
func (s *Session) SetSince(files map[string]*FileDiff, order []string) {
	s.Since, s.SinceOrder = files, order
}

// ---- Building the diff view ----

// BuildSince is the plain diff from repo.Base to repo.Head (no Touched
// Methods): "Since last review".
func BuildSince(repo *gitsrc.Repo) (map[string]*FileDiff, []string, error) {
	list, err := repo.NameStatus()
	if err != nil {
		return nil, nil, err
	}
	files := map[string]*FileDiff{}
	var order []string
	for _, fs := range list {
		fd := &FileDiff{Path: fs.Path}
		switch fs.Status {
		case "added":
			fd.Status = FileAdded
		case "deleted":
			fd.Status = FileDeleted
		}
		if bin, _ := repo.IsBinary(fs.Path); bin {
			fd.Binary = true
		} else {
			lines, err := repo.FullDiff(fs.Path)
			if err != nil {
				return nil, nil, fmt.Errorf("diff %s: %w", fs.Path, err)
			}
			for _, l := range lines {
				dl := DiffLine{OldNo: l.OldNo, NewNo: l.NewNo, Text: l.Text}
				switch l.Kind {
				case gitsrc.LineAdded:
					dl.Kind = LineAdded
					fd.Added++
				case gitsrc.LineRemoved:
					dl.Kind = LineRemoved
					fd.Removed++
				}
				fd.All = append(fd.All, dl)
			}
			fd.Hunks = mergeHunks(fd.All)
		}
		files[fs.Path] = fd
		order = append(order, fs.Path)
	}
	return files, order, nil
}

// analyzers are the code analyses, by file extension: Go with go/parser,
// TypeScript, TSX and Ruby with tree-sitter. A file none of them reads gets
// the plain diff.
func analyzers(goModulePath string) []codeanalysis.Analyzer {
	return []codeanalysis.Analyzer{golang.New(goModulePath), treesitter.New()}
}

// Empty is a state with no diff: enough to replay a log for what the log
// alone says (Feedback and their statuses, say).
func Empty() *Session {
	return &Session{
		Files:         map[string]*FileDiff{},
		Contents:      map[string]FileContents{},
		Methods:       map[string]*Method{},
		MethodsByFile: map[string][]string{},
		DeclLines:     map[string][]int{},
	}
}

// Build computes the Pull Request's diff, Touched Methods and the Briefing's
// computed parts from git. Nothing else: the rest comes from the event log.
func Build(repo *gitsrc.Repo) (*Session, []string, error) {
	var notes []string
	s := Empty()
	if abs, err := filepath.Abs(repo.Dir); err == nil {
		s.WorktreePath = abs
	} else {
		s.WorktreePath = repo.Dir
	}

	fileList, err := repo.NameStatus()
	if err != nil {
		return nil, nil, err
	}

	analyzers := analyzers(repo.GoModPath())
	var pairs []codeanalysis.FilePair

	for _, fs := range fileList {
		s.FileGitOrder = append(s.FileGitOrder, fs.Path)
		fd := &FileDiff{Path: fs.Path}
		switch fs.Status {
		case "added":
			fd.Status = FileAdded
		case "deleted":
			fd.Status = FileDeleted
		default:
			fd.Status = FileModified
		}
		bin, _ := repo.IsBinary(fs.Path)
		fd.Binary = bin
		if !bin {
			lines, err := repo.FullDiff(fs.Path)
			if err != nil {
				return nil, nil, fmt.Errorf("diff %s: %w", fs.Path, err)
			}
			for _, l := range lines {
				dl := DiffLine{OldNo: l.OldNo, NewNo: l.NewNo, Text: l.Text}
				switch l.Kind {
				case gitsrc.LineAdded:
					dl.Kind = LineAdded
					fd.Added++
				case gitsrc.LineRemoved:
					dl.Kind = LineRemoved
					fd.Removed++
				default:
					dl.Kind = LineContext
				}
				fd.All = append(fd.All, dl)
			}
			fd.Hunks = mergeHunks(fd.All)

			oldContent, oldOK := repo.Show(repo.Base, fs.Path)
			newContent, newOK := repo.Show(repo.Head, fs.Path)
			s.Contents[fs.Path] = FileContents{Old: oldContent, New: newContent, OldOK: oldOK, NewOK: newOK}

			if codeanalysis.Reads(fs.Path, analyzers...) {
				fp := codeanalysis.FilePair{
					Path: fs.Path, Old: oldContent, OldExists: oldOK,
					New: newContent, NewExists: newOK,
					ChangedOld: map[int]bool{}, ChangedNew: map[int]bool{},
				}
				for _, l := range lines {
					switch l.Kind {
					case gitsrc.LineAdded:
						fp.ChangedNew[l.NewNo] = true
					case gitsrc.LineRemoved:
						fp.ChangedOld[l.OldNo] = true
					}
				}
				pairs = append(pairs, fp)
			}
		}
		s.Files[fs.Path] = fd
	}

	an := codeanalysis.Run(pairs, analyzers...)
	for _, note := range an.ParseNotes {
		if fd := s.Files[note.Path]; fd != nil {
			fd.ParseErr = note.Err
		}
		notes = append(notes, fmt.Sprintf("%s: parse error (%s)", note.Path, note.Err))
	}
	for p, ls := range an.DeclLines {
		s.DeclLines[p] = ls
	}

	// Build Methods, in file order (git order for now; re-sorted with
	// reading order once the Briefing lands).
	byPath := map[string][]codeanalysis.Method{}
	for _, m := range an.Methods {
		byPath[m.Path] = append(byPath[m.Path], m)
	}
	for _, path := range s.FileGitOrder {
		ms := byPath[path]
		sort.SliceStable(ms, func(i, j int) bool {
			return startLine(ms[i]) < startLine(ms[j])
		})
		for _, cm := range ms {
			m := &Method{
				Key: cm.Key, Path: cm.Path, Name: cm.Name, Owner: cm.Owner,
				Status:   fromStatus(cm.Status),
				NewStart: cm.NewStart, NewEnd: cm.NewEnd, OldStart: cm.OldStart, OldEnd: cm.OldEnd,
				NewDeclLine: cm.NewDeclLine, OldDeclLine: cm.OldDeclLine,
			}
			s.Methods[m.Key] = m
			s.MethodsByFile[path] = append(s.MethodsByFile[path], m.Key)
		}
	}
	for _, e := range an.Edges {
		caller, callee := s.Methods[e.Caller], s.Methods[e.Callee]
		if caller == nil || callee == nil {
			continue
		}
		// The call-graph header rows use the bare name (the mock
		// screen shows "↑ BounceWebhook", not "↑ App.BounceWebhook").
		caller.Callees = append(caller.Callees, callee.Name)
		callee.Callers = append(callee.Callers, caller.Name)
		caller.CalleeKeys = append(caller.CalleeKeys, callee.Key)
		callee.CallerKeys = append(callee.CallerKeys, caller.Key)
	}

	s.Briefing = buildBriefing(s, an)
	return s, notes, nil
}

func startLine(m codeanalysis.Method) int {
	if m.NewStart != 0 {
		return m.NewStart
	}
	return m.OldStart
}

// mergeHunks builds hunk windows with 3 lines of context, merging overlaps.
func mergeHunks(all []DiffLine) []Hunk {
	var runs [][2]int
	i := 0
	for i < len(all) {
		if all[i].Kind == LineContext {
			i++
			continue
		}
		j := i
		for j < len(all) && all[j].Kind != LineContext {
			j++
		}
		runs = append(runs, [2]int{i, j})
		i = j
	}
	var hunks []Hunk
	for _, r := range runs {
		start := r[0] - 3
		if start < 0 {
			start = 0
		}
		end := r[1] + 3
		if end > len(all) {
			end = len(all)
		}
		if len(hunks) > 0 && start <= hunks[len(hunks)-1].End {
			hunks[len(hunks)-1].End = end
			continue
		}
		hunks = append(hunks, Hunk{Start: start, End: end})
	}
	return hunks
}

func pkgDirLast(p string) string {
	d := path.Dir(p)
	if d == "." {
		return ""
	}
	return path.Base(d)
}
