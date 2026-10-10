package session

import (
	"sort"
	"strings"

	"github.com/stephen-corgiat/pr-review/internal/codeanalysis/gocallers"
)

// The exact callers of the Go Touched Methods are derived data,
// like the diff: the engine computes them (or reads them from the cache)
// for the head, and they're never logged.

// CallersStatus is where the callers analysis is for the current head.
type CallersStatus int

const (
	CallersOff     CallersStatus = iota // not run here (-repo mode, tests): the name-matched row
	CallersRunning                      // computing
	CallersOK                           // Callers holds a result
	CallersFailed                       // the whole analysis failed: CallersErr says why
)

// CallersReady and CallersUnavailable are the engine's events for a
// finished analysis. They're never written to the event log.
type (
	CallersReady struct {
		Head    string
		Callers gocallers.Result
		Cached  bool // read from the cache, not computed
	}
	CallersUnavailable struct {
		Head  string
		Error string
	}
)

func (CallersReady) EventType() string       { return "callers_ready" }
func (CallersUnavailable) EventType() string { return "callers_unavailable" }

// GoMethods are the Go Touched Methods that exist at the head (added or
// changed), sorted by Key: the callers analysis's input.
func (s *Session) GoMethods() []gocallers.Method {
	var out []gocallers.Method
	for _, m := range s.Methods {
		if !IsGoMethod(m) || m.Status == MethodRemoved || m.NewDeclLine == 0 {
			continue
		}
		out = append(out, gocallers.Method{Key: m.Key, Path: m.Path, Name: m.Name, Owner: m.Owner, Line: m.NewDeclLine})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

// IsGoMethod: the Touched Method is in a .go file.
func IsGoMethod(m *Method) bool { return m != nil && strings.HasSuffix(m.Path, ".go") }

// SetCallers records the analysis's state for the head (the engine calls
// it; the client only reads).
func (s *Session) SetCallers(st CallersStatus, res gocallers.Result, err string) {
	s.CallersState, s.Callers, s.CallersErr = st, res, err
}

// MethodCallers is a Touched Method's exact callers, when the analysis
// covers it: Go methods at the head, once it's ready. ok is false for
// TypeScript, TSX, Ruby and removed methods, and when it isn't running.
func (s *Session) MethodCallers(key string) (c gocallers.Callers, st CallersStatus, ok bool) {
	m := s.Methods[key]
	if !IsGoMethod(m) || m.Status == MethodRemoved || s.CallersState == CallersOff {
		return gocallers.Callers{}, CallersOff, false
	}
	switch s.CallersState {
	case CallersOK:
		c, found := s.Callers[key]
		if !found {
			return gocallers.Callers{Unavailable: "not analysed"}, CallersOK, true
		}
		return c, CallersOK, true
	case CallersFailed:
		return gocallers.Callers{Unavailable: s.CallersErr}, CallersFailed, true
	}
	return gocallers.Callers{}, s.CallersState, true
}
