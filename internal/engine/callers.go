package engine

// The exact callers of the Go Touched Methods: computed off the
// client's goroutine after the worktree is ready at open and after each
// Update, cached per head commit in the Review Session's data directory,
// and never logged (like the diff and PR Comments, they're derived data).

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/gocallers"
	"github.com/stephen-corgiat/pr-review/internal/gitsrc"
	"github.com/stephen-corgiat/pr-review/internal/session"
)

// CallersFunc finds the callers of the Go Touched Methods in the
// worktree. An error means the whole analysis failed; one method's
// failure is in its Callers.Unavailable.
type CallersFunc func(ctx context.Context, worktree string, methods []gocallers.Method) (gocallers.Result, error)

// GoCallers is the real analysis (go/packages + go/types).
func GoCallers(ctx context.Context, worktree string, methods []gocallers.Method) (gocallers.Result, error) {
	return gocallers.Analyze(ctx, worktree, methods), nil
}

const callersKey = "callers"

// callersRun is the analysis running on a goroutine.
type callersRun struct {
	head   string
	cancel context.CancelFunc
}

// callersFile is the cache: <CacheDir>/callers/<head>.json.
type callersFile struct {
	Version int              `json:"version"`
	Head    string           `json:"head"`
	Callers gocallers.Result `json:"callers"`
}

// CallersCachePath is where the callers at head are cached ("" without a
// cache directory).
func (e *Engine) CallersCachePath(head string) string {
	if e.cfg.CacheDir == "" || head == "" {
		return ""
	}
	return filepath.Join(e.cfg.CacheDir, "callers", head+".json")
}

// CallersRunning reports whether the analysis is computing.
func (e *Engine) CallersRunning() bool { return e.callers != nil }

// startCallers shows the callers at the head: from the cache at once, or
// computed on a goroutine (CallersReady arrives on the Inbox). Without a
// CallersFunc it does nothing: the name-matched row stays.
func (e *Engine) startCallers() []session.Event {
	if e.cfg.Callers == nil {
		return nil
	}
	e.cancelCallers()
	st, head := e.st, e.st.PR.HeadSHA
	methods := st.GoMethods()
	if len(methods) == 0 {
		st.SetCallers(session.CallersOK, gocallers.Result{}, "")
		return []session.Event{e.derived(session.CallersReady{Head: head, Callers: gocallers.Result{}})}
	}
	if res, ok := e.readCallers(head, methods); ok {
		st.SetCallers(session.CallersOK, res, "")
		return []session.Event{e.derived(session.CallersReady{Head: head, Callers: res, Cached: true})}
	}
	wt := st.WorktreePath
	if fi, err := os.Stat(wt); wt == "" || err != nil || !fi.IsDir() {
		msg := "the worktree was removed"
		st.SetCallers(session.CallersFailed, nil, msg)
		return []session.Event{e.derived(session.CallersUnavailable{Head: head, Error: msg})}
	}
	st.SetCallers(session.CallersRunning, nil, "")
	ctx, cancel := context.WithCancel(e.ctx)
	e.callers = &callersRun{head: head, cancel: cancel}
	analyze, path := e.cfg.Callers, e.CallersCachePath(head)
	go func() {
		res, err := runCallers(ctx, analyze, wt, methods)
		if ctx.Err() != nil {
			return // cancelled: an Update started, or the engine closed
		}
		if err != nil {
			e.send(Input{key: callersKey, p: session.CallersUnavailable{Head: head, Error: err.Error()}})
			return
		}
		if path != "" && cacheable(res) {
			_ = writeCallers(path, callersFile{Version: gocallers.Version, Head: head, Callers: res})
		}
		e.send(Input{key: callersKey, p: session.CallersReady{Head: head, Callers: res}})
	}()
	return nil
}

// runCallers runs the analysis, turning a panic (go/types on the PR's
// code) into an error.
func runCallers(ctx context.Context, f CallersFunc, wt string, ms []gocallers.Method) (res gocallers.Result, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("the analysis crashed: %v", r)
		}
	}()
	return f(ctx, wt, ms)
}

// cacheable: every method's callers were found. An unavailable method may
// be a module that couldn't download (offline): try again next time.
func cacheable(res gocallers.Result) bool {
	for _, c := range res {
		if c.Unavailable != "" {
			return false
		}
	}
	return true
}

// cancelCallers stops a running analysis (an Update starts, or a new
// head's analysis replaces it).
func (e *Engine) cancelCallers() {
	if e.callers != nil {
		e.callers.cancel()
		e.callers = nil
	}
}

// handleCallers takes a finished analysis from the Inbox: it's dropped
// when the Review Session moved on (or the run was cancelled) meanwhile.
func (e *Engine) handleCallers(in Input) []session.Event {
	var head string
	switch p := in.p.(type) {
	case session.CallersReady:
		head = p.Head
	case session.CallersUnavailable:
		head = p.Head
	}
	if e.callers == nil || e.callers.head != head || head != e.st.PR.HeadSHA {
		return nil
	}
	e.callers.cancel()
	e.callers = nil
	switch p := in.p.(type) {
	case session.CallersReady:
		e.st.SetCallers(session.CallersOK, p.Callers, "")
	case session.CallersUnavailable:
		e.st.SetCallers(session.CallersFailed, nil, p.Error)
	}
	return []session.Event{e.derived(in.p)}
}

// derived is an event that isn't logged: no ID, never appended.
func (e *Engine) derived(p session.Payload) session.Event {
	return session.Event{Writer: e.writer, At: e.cfg.Now(), Data: p}
}

// readCallers is the cached result at head, when it's this version's and
// covers every method.
func (e *Engine) readCallers(head string, methods []gocallers.Method) (gocallers.Result, bool) {
	path := e.CallersCachePath(head)
	if path == "" {
		return nil, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var f callersFile
	if json.Unmarshal(b, &f) != nil || f.Version != gocallers.Version || f.Head != head || f.Callers == nil {
		return nil, false
	}
	for _, m := range methods {
		if _, ok := f.Callers[m.Key]; !ok {
			return nil, false
		}
	}
	return f.Callers, true
}

// writeCallers writes the cache file whole (a temp file, renamed).
func writeCallers(path string, f callersFile) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, time.Now().UnixNano())
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WaitCallers handles Inbox inputs until the callers analysis finishes or
// d passes (tests and the CLI; the TUI uses Inbox). Agent runs that finish
// meanwhile are handled too.
func (e *Engine) WaitCallers(d time.Duration) []session.Event {
	var out []session.Event
	deadline := time.After(d)
	for e.callers != nil {
		select {
		case in := <-e.inbox:
			out = append(out, e.Handle(in)...)
		case <-deadline:
			return out
		}
	}
	return out
}

// HeadFile is a file's content at the head, read with git (callers and
// Citations can point at files outside the PR).
func (e *Engine) HeadFile(path string) (string, bool) {
	head := e.st.PR.HeadSHA
	if head == "" {
		head = e.cfg.Repo.Head
	}
	return gitsrc.OpenRepo(e.cfg.Repo.Dir, "", head).Show(head, path)
}
