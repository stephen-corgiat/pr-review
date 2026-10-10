package engine_test

// The callers analysis: it runs after open and after Update, off
// the client's goroutine; its result is cached per head and never logged;
// a cached head doesn't compute again; an Update cancels a running one.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stephen-corgiat/pr-review/internal/app"
	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/gocallers"
	"github.com/stephen-corgiat/pr-review/internal/engine"
	"github.com/stephen-corgiat/pr-review/internal/eventlog"
	"github.com/stephen-corgiat/pr-review/internal/forge"
	"github.com/stephen-corgiat/pr-review/internal/paths"
	"github.com/stephen-corgiat/pr-review/internal/session"
	"github.com/stephen-corgiat/pr-review/internal/testrepo"
)

// fakeCallers records each analysis (the worktree's HEAD when it ran, and
// the methods) and gives every method one caller. Runs listed in block
// wait until they're cancelled.
type fakeCallers struct {
	mu        sync.Mutex
	heads     []string
	keys      [][]string
	block     map[int]bool // by run number, from 1
	cancelled chan string
}

func newFakeCallers() *fakeCallers {
	return &fakeCallers{block: map[int]bool{}, cancelled: make(chan string, 10)}
}

func (f *fakeCallers) run(ctx context.Context, wt string, ms []gocallers.Method) (gocallers.Result, error) {
	head := testrepo.Git(nopTB{}, wt, "rev-parse", "HEAD")
	var keys []string
	for _, m := range ms {
		keys = append(keys, m.Key)
	}
	f.mu.Lock()
	f.heads = append(f.heads, head)
	f.keys = append(f.keys, keys)
	n := len(f.heads)
	block := f.block[n]
	f.mu.Unlock()
	if block {
		<-ctx.Done()
		f.cancelled <- head
		return nil, ctx.Err()
	}
	res := gocallers.Result{}
	for _, m := range ms {
		res[m.Key] = gocallers.Callers{Callers: []gocallers.Caller{{Func: "calc.Use", Path: "calc/use.go", Line: 3}}}
	}
	return res, nil
}

// started waits until n runs have started (a blocked run must be the
// first to start, not just the first launched).
func (f *fakeCallers) started(t *testing.T, n int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for len(f.runs()) < n {
		if time.Now().After(deadline) {
			t.Fatalf("%d runs started, want %d", len(f.runs()), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// wasCancelled waits for a blocked run to be cancelled and returns its head.
func (f *fakeCallers) wasCancelled(t *testing.T) string {
	t.Helper()
	select {
	case h := <-f.cancelled:
		return h
	case <-time.After(10 * time.Second):
		t.Fatal("the analysis wasn't cancelled")
		return ""
	}
}

func (f *fakeCallers) runs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.heads...)
}

// nopTB lets testrepo.Git run off the test's goroutine (it only calls
// Helper and, on a failure, Fatalf).
type nopTB struct{ testing.TB }

func (nopTB) Helper()               {}
func (nopTB) Fatalf(string, ...any) { panic("git failed") }

type callersSetup struct {
	r  *testrepo.Repo
	f  *forge.Fake
	fc *fakeCallers
	o  app.PR
}

func newCallersSetup(t *testing.T, fc engine.CallersFunc) *callersSetup {
	t.Helper()
	r := testrepo.New(t)
	f := forge.NewFake("ana", forge.PR{Number: 7, OwnerRepo: "acme/calc", Title: "Clamp results", Author: "ana",
		HeadRef: "feature", BaseRef: "main", HeadSHA: r.Head, BaseSHA: r.Base, State: "open"})
	ag := &citingAgent{}
	o := app.PR{Checkout: r.Dir, Number: 7, Forge: f, DataDir: t.TempDir(), Callers: fc,
		Agent: func(paths.Session, session.Opened) (engine.Agent, error) { return ag, nil }}
	return &callersSetup{r: r, f: f, o: o}
}

func (s *callersSetup) open(t *testing.T) *engine.Engine {
	t.Helper()
	e, _, _, err := s.o.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(e.Close)
	return e
}

func hasType(evs []session.Event, typ string) bool {
	return strings.Contains(" "+types(evs)+" ", " "+typ+" ")
}

func goKeys(st *session.Session) []string {
	var out []string
	for _, m := range st.GoMethods() {
		out = append(out, m.Key)
	}
	return out
}

func TestCallersRunAfterOpenAndAreCachedPerHead(t *testing.T) {
	fc := newFakeCallers()
	s := newCallersSetup(t, fc.run)
	e := s.open(t)
	st := e.State()
	if st.CallersState != session.CallersOff {
		t.Fatalf("nothing runs until Start")
	}
	evs := e.Start()
	if hasType(evs, "callers_ready") || st.CallersState != session.CallersRunning {
		t.Fatalf("Start: %s, state %d: want the analysis running", types(evs), st.CallersState)
	}
	evs = e.WaitCallers(10 * time.Second)
	if !hasType(evs, "callers_ready") || st.CallersState != session.CallersOK {
		t.Fatalf("after the run: %s, state %d", types(evs), st.CallersState)
	}
	if runs := fc.runs(); !reflect.DeepEqual(runs, []string{s.r.Head}) {
		t.Fatalf("runs at %v, want once at the head", runs)
	}
	want := []string{"calc/calc.go::Add", "calc/calc.go::Scale", "calc/calc.go::clamp", "calc/calc_test.go::TestAdd"}
	if got := goKeys(st); !reflect.DeepEqual(got, want) || !reflect.DeepEqual(fc.keys[0], want) {
		t.Fatalf("Go Touched Methods %v (analysed %v), want %v", got, fc.keys[0], want)
	}
	if c, cs, ok := st.MethodCallers("calc/calc.go::clamp"); !ok || cs != session.CallersOK || len(c.Callers) != 1 {
		t.Fatalf("clamp's callers: %+v %d %v", c, cs, ok)
	}

	// The cache: <session dir>/callers/<head>.json. The log has nothing.
	path := e.CallersCachePath(s.r.Head)
	if filepath.Base(filepath.Dir(path)) != "callers" || filepath.Base(path) != s.r.Head+".json" {
		t.Fatalf("cache path %s", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("not cached: %v", err)
	}
	log, err := os.ReadFile(filepath.Join(filepath.Dir(filepath.Dir(path)), eventlog.FileName))
	if err != nil || strings.Contains(string(log), "callers") {
		t.Fatalf("the event log mentions callers (%v)", err)
	}
	e.Close()

	// Opening at the cached head shows them at once, without computing.
	e2 := s.open(t)
	evs = e2.Start()
	if !hasType(evs, "callers_ready") || e2.State().CallersState != session.CallersOK || e2.CallersRunning() {
		t.Fatalf("reopened: %s, state %d", types(evs), e2.State().CallersState)
	}
	if ready := evs[0].Data.(session.CallersReady); !ready.Cached || len(ready.Callers) != 4 {
		t.Fatalf("reopened: %+v, want the cached result", ready)
	}
	if runs := fc.runs(); len(runs) != 1 {
		t.Fatalf("a cached head computed again: runs %v", runs)
	}
}

func TestCallersRunAgainAfterUpdate(t *testing.T) {
	fc := newFakeCallers()
	s := newCallersSetup(t, fc.run)
	e := s.open(t)
	e.Start()
	e.WaitCallers(10 * time.Second)
	e.Pump(2 * time.Second) // the Briefing
	b := s.r.Push(t)
	s.f.SetHead(7, b)
	evs, _, err := e.Update(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	st := e.State()
	if hasType(evs, "callers_ready") || st.CallersState != session.CallersRunning {
		t.Fatalf("after Update: %s, state %d: want it computing at the new head", types(evs), st.CallersState)
	}
	evs = e.WaitCallers(10 * time.Second)
	if !hasType(evs, "callers_ready") || st.CallersState != session.CallersOK {
		t.Fatalf("after the run: %s", types(evs))
	}
	if runs := fc.runs(); !reflect.DeepEqual(runs, []string{s.r.Head, b}) {
		t.Fatalf("runs at %v, want A then B", runs)
	}
	// At B calc_test.go is gone: its method isn't analysed.
	if keys := fc.keys[1]; len(keys) != 3 || strings.Contains(strings.Join(keys, " "), "TestAdd") {
		t.Fatalf("analysed at B: %v", keys)
	}
	for _, h := range []string{s.r.Head, b} {
		if _, err := os.Stat(e.CallersCachePath(h)); err != nil {
			t.Errorf("no cache for %.7s: %v", h, err)
		}
	}
}

func TestUpdateCancelsARunningAnalysis(t *testing.T) {
	fc := newFakeCallers()
	fc.block[1] = true
	s := newCallersSetup(t, fc.run)
	e := s.open(t)
	e.Start()
	fc.started(t, 1)
	e.Pump(2 * time.Second) // the Briefing (the analysis keeps running)
	if !e.CallersRunning() {
		t.Fatalf("the analysis should still be running")
	}
	b := s.r.Push(t)
	s.f.SetHead(7, b)
	run, err := e.BeginUpdate()
	if err != nil {
		t.Fatal(err)
	}
	if h := fc.wasCancelled(t); h != s.r.Head {
		t.Fatalf("cancelled the run at %.7s", h)
	}
	if e.CallersRunning() {
		t.Fatal("still running after the cancel")
	}
	p, err := run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.ApplyUpdate(p); err != nil {
		t.Fatal(err)
	}
	evs := e.WaitCallers(10 * time.Second)
	if !hasType(evs, "callers_ready") || e.State().CallersState != session.CallersOK {
		t.Fatalf("after the Update: %s", types(evs))
	}
	if runs := fc.runs(); !reflect.DeepEqual(runs, []string{s.r.Head, b}) {
		t.Fatalf("runs %v", runs)
	}
	if _, err := os.Stat(e.CallersCachePath(s.r.Head)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the cancelled run was cached (%v)", err)
	}
}

// An Update that fails (no new head) cancelled the analysis: it starts
// again at the same head.
func TestAFailedUpdateRestartsTheAnalysis(t *testing.T) {
	fc := newFakeCallers()
	fc.block[1] = true
	s := newCallersSetup(t, fc.run)
	e := s.open(t)
	e.Start()
	fc.started(t, 1)
	e.Pump(2 * time.Second)
	if _, _, err := e.Update(context.Background()); !errors.Is(err, engine.ErrNoNewHead) {
		t.Fatalf("Update: %v, want no new head", err)
	}
	fc.wasCancelled(t)
	evs := e.WaitCallers(10 * time.Second)
	if !hasType(evs, "callers_ready") {
		t.Fatalf("after the failed Update: %s", types(evs))
	}
	if runs := fc.runs(); !reflect.DeepEqual(runs, []string{s.r.Head, s.r.Head}) {
		t.Fatalf("runs %v, want the head twice", runs)
	}
}

// A whole-analysis failure is shown and not cached; a crash is one.
func TestCallersFailure(t *testing.T) {
	calls := 0
	s := newCallersSetup(t, func(context.Context, string, []gocallers.Method) (gocallers.Result, error) {
		calls++
		panic("boom")
	})
	e := s.open(t)
	e.Start()
	evs := e.WaitCallers(10 * time.Second)
	st := e.State()
	if !hasType(evs, "callers_unavailable") || st.CallersState != session.CallersFailed || !strings.Contains(st.CallersErr, "boom") {
		t.Fatalf("%s state %d %q", types(evs), st.CallersState, st.CallersErr)
	}
	if c, cs, ok := st.MethodCallers("calc/calc.go::Add"); !ok || cs != session.CallersFailed || c.Unavailable == "" {
		t.Fatalf("Add: %+v %d %v", c, cs, ok)
	}
	if _, err := os.Stat(e.CallersCachePath(s.r.Head)); err == nil {
		t.Fatal("a failure was cached")
	}
}

// The real analysis through the engine, on the scratch repo.
func TestGoCallersThroughTheEngine(t *testing.T) {
	s := newCallersSetup(t, engine.GoCallers)
	e := s.open(t)
	e.Start()
	e.WaitCallers(60 * time.Second)
	st := e.State()
	if st.CallersState != session.CallersOK {
		t.Fatalf("state %d %q", st.CallersState, st.CallersErr)
	}
	got := map[string][]string{}
	for _, k := range goKeys(st) {
		c, _, _ := st.MethodCallers(k)
		if c.Unavailable != "" {
			t.Errorf("%s unavailable: %s", k, c.Unavailable)
		}
		for _, x := range c.Callers {
			d := x.Func
			if x.Test {
				d += " test"
			}
			got[k] = append(got[k], d)
		}
		sort.Strings(got[k])
	}
	want := map[string][]string{
		"calc/calc.go::Add":   {"calc.TestAdd test"},
		"calc/calc.go::clamp": {"calc.Add", "calc.Scale"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callers %v, want %v", got, want)
	}
}

// -repo mode (and any engine without a CallersFunc) doesn't run it.
func TestNoCallersWithoutTheAnalysis(t *testing.T) {
	e, _ := openEngine(t, nil, nil)
	e.Start()
	if e.State().CallersState != session.CallersOff || e.CallersRunning() {
		t.Fatal("ran without a CallersFunc")
	}
	if _, _, ok := e.State().MethodCallers("calc/calc.go::Add"); ok {
		t.Fatal("MethodCallers says covered")
	}
}
