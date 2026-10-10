package app

// Shell completion: `pr-review __complete <words…>` (hidden) holds all the
// logic, and the scripts from `pr-review completion <shell>` just call it.
// The design is Cobra's: candidates one per line (a tab then a description
// is optional), then a final ":<directive>" line. A tab press must never
// print an error into the prompt, so every failure is "no candidates".

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/eventlog"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/paths"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// Directive tells the shell what to do besides offering the candidates
// (Cobra's values).
type Directive int

const (
	DirDefault    Directive = 0  // fall back to file completion when there are no candidates
	DirNoFileComp Directive = 4  // never complete files
	DirDirs       Directive = 16 // complete directories only
)

// Candidate is one completion; Desc is shown by zsh and fish.
type Candidate struct{ Value, Desc string }

// Completion is `__complete`'s answer.
type Completion struct {
	Candidates []Candidate
	Directive  Directive
}

// String is the wire format: a candidate per line, then ":<directive>".
func (c Completion) String() string {
	var b strings.Builder
	for _, cand := range c.Candidates {
		b.WriteString(cand.Value)
		if cand.Desc != "" {
			b.WriteString("\t" + oneLineDesc(cand.Desc))
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, ":%d\n", c.Directive)
	return b.String()
}

func oneLineDesc(s string) string { return strings.Join(strings.Fields(s), " ") }

// PR list limits (see the spec: a network call per tab press otherwise).
const (
	completePRLimit    = 50
	completePRCacheFor = 60 * time.Second
	completeTimeout    = 2 * time.Second
)

// Completer answers `__complete`.
type Completer struct {
	Dir     string // the current directory
	DataDir string
	Forge   forge.Forge
	Now     func() time.Time // default time.Now
}

var (
	subcommands = []Candidate{
		{"export", "print queued Feedback for the Implementing Agent"},
		{"resolve", "record what was done about handed-off Feedback"},
		{"skill", "install /pull-review into your Claude skills"},
		{"prune", "remove the worktrees of finished Review Sessions"},
		{"completion", "print a shell completion script"},
	}
	mainFlags = []Candidate{
		{"-repo", "open a local repo's base..head"}, {"-base", "base ref (-repo mode)"},
		{"-head", "head ref (-repo mode)"}, {"-style", "the chroma highlighting style"},
	}
	// valueFlags take the next word as their value.
	valueFlags = map[string]bool{"repo": true, "base": true, "head": true, "style": true, "commit": true, "dir": true}
)

// Complete completes the last of words (the words typed after "pr-review";
// it may be empty or partial).
func (c Completer) Complete(ctx context.Context, words []string) (out Completion) {
	out.Directive = DirNoFileComp
	defer func() {
		if recover() != nil {
			out = Completion{Directive: DirNoFileComp}
		}
	}()
	if len(words) == 0 {
		words = []string{""}
	}
	cur, before := words[len(words)-1], words[:len(words)-1]
	ctx, cancel := context.WithTimeout(ctx, completeTimeout)
	defer cancel()

	sub := ""
	if len(before) > 0 {
		for _, s := range subcommands {
			if before[0] == s.Value {
				sub = s.Value
			}
		}
	}
	// The positional words so far, and the flag that wants cur as its value.
	var pos []string
	pending := ""
	rest := before
	if sub != "" {
		rest = before[1:]
	}
	for _, w := range rest {
		switch {
		case pending != "":
			pending = ""
		case strings.HasPrefix(w, "-") && len(w) > 1:
			name := strings.TrimLeft(w, "-")
			if !strings.Contains(name, "=") && valueFlags[name] {
				pending = name
			}
		default:
			pos = append(pos, w)
		}
	}

	switch pending {
	case "repo", "dir":
		return Completion{Directive: DirDirs}
	case "commit":
		return c.commits(ctx, cur)
	case "base", "head", "style":
		return Completion{Directive: DirNoFileComp}
	}
	if strings.HasPrefix(cur, "-") {
		return c.flags(sub, cur)
	}

	switch sub {
	case "":
		if len(pos) > 0 {
			return out
		}
		return c.withPrefix(cur, append(append([]Candidate(nil), subcommands...), c.openPRs(ctx)...))
	case "export":
		if len(pos) == 0 {
			return c.withPrefix(cur, []Candidate{{"--again", "also hand off again the items already handed off"}})
		}
	case "skill":
		if len(pos) == 0 {
			return c.withPrefix(cur, []Candidate{{"install", "install /pull-review"}})
		}
	case "completion":
		if len(pos) == 0 {
			return c.withPrefix(cur, []Candidate{{"bash", ""}, {"zsh", ""}, {"fish", ""}})
		}
	case "resolve":
		switch len(pos) {
		case 0:
			return c.withPrefix(cur, c.handedOff())
		case 1:
			return c.withPrefix(cur, []Candidate{
				{"addressed", "the change was made"}, {"declined", "it won't be changed (say why)"}, {"needs-you", "a question for the Reviewer"}})
		case 2:
			return out // the note
		default:
			if len(pos) == 3 {
				return c.withPrefix(cur, []Candidate{{"--commit", "the commit that addressed it"}})
			}
		}
	}
	return out
}

func (c Completer) flags(sub, cur string) Completion {
	var all []Candidate
	switch sub {
	case "":
		all = mainFlags
	case "export":
		all = []Candidate{{"--again", "also hand off again the items already handed off"}}
	case "resolve":
		all = []Candidate{{"--commit", "the commit that addressed it"}}
	case "skill":
		all = []Candidate{{"--dir", "the skills directory"}}
	}
	return c.withPrefix(cur, all)
}

func (c Completer) withPrefix(cur string, all []Candidate) Completion {
	out := Completion{Directive: DirNoFileComp}
	for _, cand := range all {
		if strings.HasPrefix(cand.Value, cur) {
			out.Candidates = append(out.Candidates, cand)
		}
	}
	return out
}

// ---- Pull Requests ----

type prCache struct {
	At  time.Time
	PRs []forge.PRSummary
}

// age reports whether a cache written at then is still fresh at now.
func age(now, then time.Time) bool {
	d := now.Sub(then)
	return d >= 0 && d < completePRCacheFor
}

func (c Completer) now() time.Time {
	if c.Now != nil {
		return c.Now()
	}
	return time.Now()
}

// openPRs are the current repo's open PRs, from a 60-second cache in the
// data directory, else from the Forge. Any failure is no candidates.
func (c Completer) openPRs(ctx context.Context) []Candidate {
	if c.Forge == nil {
		return nil
	}
	root, err := Git(ctx, c.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	repos := remoteRepos(ctx, root)
	if len(repos) == 0 {
		return nil
	}
	file := filepath.Join(c.DataDir, "completion", "prs-"+strings.ToLower(strings.ReplaceAll(repos[0], "/", "_"))+".json")
	var prs []forge.PRSummary
	var cached prCache
	if b, err := os.ReadFile(file); err == nil && json.Unmarshal(b, &cached) == nil && age(c.now(), cached.At) {
		prs = cached.PRs
	} else {
		prs, err = c.Forge.OpenPRs(ctx, completePRLimit)
		if err != nil {
			return nil
		}
		if b, err := json.Marshal(prCache{At: c.now(), PRs: prs}); err == nil {
			if os.MkdirAll(filepath.Dir(file), 0o755) == nil {
				os.WriteFile(file, b, 0o644)
			}
		}
	}
	var out []Candidate
	for _, p := range prs {
		out = append(out, Candidate{strconv.Itoa(p.Number), p.Title})
	}
	return out
}

// ---- Feedback and commits ----

// handedOff are the Feedback IDs of the current branch's Review Session
// that are handed off and not done, with their first lines. It reads the
// event log only (a replay, no engine, no network), and finds the Review
// Session as Locate does, but from the logs: the PR whose repo is one of
// the checkout's remotes and whose branch is the one checked out.
func (c Completer) handedOff() []Candidate {
	ctx := context.Background()
	root, err := Git(ctx, c.Dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil
	}
	branch, err := Git(ctx, root, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil || branch == "HEAD" {
		return nil
	}
	repos := remoteRepos(ctx, root)
	dirs, err := paths.AllSessions(c.DataDir)
	if err != nil {
		return nil
	}
	var best *session.Session
	var bestMod time.Time
	for _, d := range dirs {
		st := replayReadOnly(d.Dir())
		if st == nil || st.Fixture || st.RepoMode || st.PR.HeadRef != branch {
			continue
		}
		match := false
		for _, r := range repos {
			match = match || strings.EqualFold(r, st.PR.OwnerRepo)
		}
		if !match {
			continue
		}
		if fi, err := os.Stat(filepath.Join(d.Dir(), eventlog.FileName)); err == nil && (best == nil || fi.ModTime().After(bestMod)) {
			best, bestMod = st, fi.ModTime()
		}
	}
	if best == nil {
		return nil
	}
	var out []Candidate
	for _, f := range best.Feedback {
		if f.Status == session.FeedbackHandedOff {
			out = append(out, Candidate{f.ID, firstLineOf(f.Body)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return idNumber(out[i].Value) < idNumber(out[j].Value) })
	return out
}

func idNumber(id string) int {
	n, _ := strconv.Atoi(strings.TrimLeft(id, "F"))
	return n
}

func firstLineOf(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// replayReadOnly replays a Review Session's log into a bare state (no
// diff, no engine), without creating anything. Nil when it can't.
func replayReadOnly(dir string) (st *session.Session) {
	if _, err := os.Stat(filepath.Join(dir, eventlog.FileName)); err != nil {
		return nil
	}
	defer func() {
		if recover() != nil {
			st = nil
		}
	}()
	evs, err := eventlog.OpenExisting(dir).Load()
	if err != nil || len(evs) == 0 {
		return nil
	}
	st = session.Empty()
	for _, ev := range evs {
		st.Apply(ev)
	}
	return st
}

// commits are the branch's recent commit shas with their subjects.
func (c Completer) commits(ctx context.Context, cur string) Completion {
	out, err := Git(ctx, c.Dir, "log", "-n", "20", "--format=%h%x09%s")
	if err != nil {
		return Completion{Directive: DirNoFileComp}
	}
	var all []Candidate
	for _, l := range strings.Split(out, "\n") {
		if sha, subj, ok := strings.Cut(l, "\t"); ok {
			all = append(all, Candidate{sha, subj})
		}
	}
	return c.withPrefix(cur, all)
}
