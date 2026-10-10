// Package harness drives a Bubble Tea v2 model without a terminal, so views
// can be snapshotted from tests or `go run . -snapshot`. Port of v1's
// harness, plus mouse tokens.
package harness

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Event is either a message to deliver, or a pause before the next one.
type Event struct {
	Msg  tea.Msg
	Wait time.Duration
}

// Run sends a WindowSizeMsg, runs Init, feeds events (pausing briefly after
// each so commands settle), waits settle, and returns the final model and
// View().Content. Timers (tea.Tick) fire in real time.
//
// Calls on the same model share one message queue, and Init runs only on
// the first: commands still running when one Run returns (an agent run, a
// timer) deliver into the next, as they would in a real program.
func Run(m tea.Model, w, h int, events []Event, settle time.Duration) (tea.Model, string) {
	r := runnerFor(m)
	msgs := r.msgs
	cmdType := reflect.TypeOf((tea.Cmd)(nil))
	var exec func(tea.Cmd)
	var deliver func(tea.Msg)
	deliver = func(msg tea.Msg) {
		switch mm := msg.(type) {
		case nil, tea.QuitMsg:
			return
		case tea.BatchMsg:
			for _, c := range mm {
				exec(c)
			}
			return
		}
		if v := reflect.ValueOf(msg); v.Kind() == reflect.Slice && v.Type().Elem() == cmdType {
			for i := 0; i < v.Len(); i++ {
				if c, _ := v.Index(i).Interface().(tea.Cmd); c != nil {
					deliver(c())
				}
			}
			return
		}
		msgs <- msg
	}
	exec = func(cmd tea.Cmd) {
		if cmd != nil {
			go func() { deliver(cmd()) }()
		}
	}
	step := func(msg tea.Msg) {
		var cmd tea.Cmd
		m, cmd = m.Update(msg)
		exec(cmd)
	}
	drain := func(d time.Duration) {
		deadline := time.After(d)
		for {
			select {
			case msg := <-msgs:
				step(msg)
			case <-deadline:
				return
			}
		}
	}

	if !r.inited {
		r.inited = true
		exec(m.Init())
	}
	step(tea.WindowSizeMsg{Width: w, Height: h})
	drain(60 * time.Millisecond)
	for _, ev := range events {
		if ev.Wait > 0 {
			drain(ev.Wait)
			continue
		}
		step(ev.Msg)
		drain(40 * time.Millisecond)
	}
	drain(settle)
	return m, m.View().Content
}

type runner struct {
	msgs   chan tea.Msg
	inited bool
}

var (
	runnersMu sync.Mutex
	runners   = map[tea.Model]*runner{}
)

func runnerFor(m tea.Model) *runner {
	runnersMu.Lock()
	defer runnersMu.Unlock()
	r := runners[m]
	if r == nil {
		r = &runner{msgs: make(chan tea.Msg, 4096)}
		runners[m] = r
	}
	return r
}

// ParseEvents turns a space-separated event spec into Events. Tokens:
//   - key names as Bubble Tea prints them ("j", "J", "enter", "esc", "tab",
//     "space", "ctrl+d", "alt+enter", "shift+tab")
//   - a double-quoted string, typed character by character
//   - mouse tokens: "click:X,Y", "dclick:X,Y", "tclick:X,Y" (2 or 3 clicks
//     within 400ms), "drag:X1,Y1-X2,Y2" (click, motion steps, release),
//     "wheelup:X,Y", "wheeldown:X,Y", "backward:X,Y" / "forward:X,Y" (the
//     mouse's Back / Forward buttons), "move:X,Y" (hover, no button). A drag that starts on a pane border
//     resizes it.
//   - "wait:500ms"
func ParseEvents(spec string) ([]Event, error) {
	var out []Event
	for _, tok := range tokenize(spec) {
		switch {
		case strings.HasPrefix(tok, `"`):
			for _, r := range strings.Trim(tok, `"`) {
				out = append(out, Event{Msg: runeKeyMsg(r)})
			}
		case tok == "focus" || tok == "blur":
			// The terminal gaining or losing focus.
			if tok == "focus" {
				out = append(out, Event{Msg: tea.FocusMsg{}})
			} else {
				out = append(out, Event{Msg: tea.BlurMsg{}})
			}
		case strings.HasPrefix(tok, "wait:"):
			d, err := time.ParseDuration(strings.TrimPrefix(tok, "wait:"))
			if err != nil {
				return nil, fmt.Errorf("bad wait %q: %w", tok, err)
			}
			out = append(out, Event{Wait: d})
		case strings.HasPrefix(tok, "click:"):
			x, y, err := parseXY(strings.TrimPrefix(tok, "click:"))
			if err != nil {
				return nil, err
			}
			out = append(out, clickEvents(x, y, 1)...)
		case strings.HasPrefix(tok, "dclick:"):
			x, y, err := parseXY(strings.TrimPrefix(tok, "dclick:"))
			if err != nil {
				return nil, err
			}
			out = append(out, clickEvents(x, y, 2)...)
		case strings.HasPrefix(tok, "tclick:"):
			x, y, err := parseXY(strings.TrimPrefix(tok, "tclick:"))
			if err != nil {
				return nil, err
			}
			out = append(out, clickEvents(x, y, 3)...)
		case strings.HasPrefix(tok, "drag:"):
			evs, err := dragEvents(strings.TrimPrefix(tok, "drag:"))
			if err != nil {
				return nil, err
			}
			out = append(out, evs...)
		case strings.HasPrefix(tok, "backward:"), strings.HasPrefix(tok, "forward:"):
			name, pos, _ := strings.Cut(tok, ":")
			x, y, err := parseXY(pos)
			if err != nil {
				return nil, err
			}
			b := tea.MouseBackward
			if name == "forward" {
				b = tea.MouseForward
			}
			out = append(out,
				Event{Msg: tea.MouseClickMsg{X: x, Y: y, Button: b}},
				Event{Msg: tea.MouseReleaseMsg{X: x, Y: y, Button: b}})
		case strings.HasPrefix(tok, "move:"):
			x, y, err := parseXY(strings.TrimPrefix(tok, "move:"))
			if err != nil {
				return nil, err
			}
			out = append(out, Event{Msg: tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseNone}})
		case strings.HasPrefix(tok, "wheelup:"):
			x, y, err := parseXY(strings.TrimPrefix(tok, "wheelup:"))
			if err != nil {
				return nil, err
			}
			out = append(out, Event{Msg: tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelUp}})
		case strings.HasPrefix(tok, "wheeldown:"):
			x, y, err := parseXY(strings.TrimPrefix(tok, "wheeldown:"))
			if err != nil {
				return nil, err
			}
			out = append(out, Event{Msg: tea.MouseWheelMsg{X: x, Y: y, Button: tea.MouseWheelDown}})
		default:
			msg, err := namedKey(tok)
			if err != nil {
				return nil, err
			}
			out = append(out, Event{Msg: msg})
		}
	}
	return out, nil
}

// MustParseEvents is ParseEvents for tests.
func MustParseEvents(spec string) []Event {
	e, err := ParseEvents(spec)
	if err != nil {
		panic(err)
	}
	return e
}

func clickEvents(x, y, count int) []Event {
	var out []Event
	for i := 0; i < count; i++ {
		out = append(out,
			Event{Msg: tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft}},
			Event{Msg: tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft}},
		)
	}
	return out
}

func dragEvents(spec string) ([]Event, error) {
	parts := strings.Split(spec, "-")
	if len(parts) != 2 {
		return nil, fmt.Errorf("bad drag spec %q", spec)
	}
	x1, y1, err := parseXY(parts[0])
	if err != nil {
		return nil, err
	}
	x2, y2, err := parseXY(parts[1])
	if err != nil {
		return nil, err
	}
	var out []Event
	out = append(out, Event{Msg: tea.MouseClickMsg{X: x1, Y: y1, Button: tea.MouseLeft}})
	steps := max(abs(x2-x1), abs(y2-y1))
	if steps == 0 {
		steps = 1
	}
	for i := 1; i <= steps; i++ {
		x := x1 + (x2-x1)*i/steps
		y := y1 + (y2-y1)*i/steps
		out = append(out, Event{Msg: tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}})
	}
	out = append(out, Event{Msg: tea.MouseReleaseMsg{X: x2, Y: y2, Button: tea.MouseLeft}})
	return out, nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func parseXY(s string) (int, int, error) {
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("bad coordinate %q", s)
	}
	x, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	y, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	return x, y, nil
}

func runeKeyMsg(r rune) tea.KeyPressMsg {
	if r == ' ' {
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	}
	return tea.KeyPressMsg{Text: string(r), Code: r}
}

var namedCodes = map[string]rune{
	"enter": tea.KeyEnter, "esc": tea.KeyEscape, "escape": tea.KeyEscape,
	"tab": tea.KeyTab, "backspace": tea.KeyBackspace, "delete": tea.KeyDelete,
	"up": tea.KeyUp, "down": tea.KeyDown, "left": tea.KeyLeft, "right": tea.KeyRight,
	"home": tea.KeyHome, "end": tea.KeyEnd, "pgup": tea.KeyPgUp, "pgdown": tea.KeyPgDown, "pgdn": tea.KeyPgDown,
	"space": tea.KeySpace,
	"f1":    tea.KeyF1, "f2": tea.KeyF2, "f3": tea.KeyF3, "f4": tea.KeyF4,
}

func namedKey(tok string) (tea.KeyPressMsg, error) {
	if strings.Contains(tok, "+") {
		parts := strings.Split(tok, "+")
		last := parts[len(parts)-1]
		var mod tea.KeyMod
		for _, p := range parts[:len(parts)-1] {
			switch p {
			case "ctrl":
				mod |= tea.ModCtrl
			case "alt":
				mod |= tea.ModAlt
			case "shift":
				mod |= tea.ModShift
			}
		}
		var code rune
		if c, ok := namedCodes[last]; ok {
			code = c
		} else if len([]rune(last)) == 1 {
			code = []rune(last)[0]
		} else {
			return tea.KeyPressMsg{}, fmt.Errorf("unknown key %q", tok)
		}
		return tea.KeyPressMsg{Code: code, Mod: mod}, nil
	}
	if tok == "space" {
		return runeKeyMsg(' '), nil // terminals send the space's text too
	}
	if c, ok := namedCodes[tok]; ok {
		return tea.KeyPressMsg{Code: c}, nil
	}
	if len([]rune(tok)) == 1 {
		r := []rune(tok)[0]
		return tea.KeyPressMsg{Text: string(r), Code: r}, nil
	}
	return tea.KeyPressMsg{}, fmt.Errorf("unknown key %q", tok)
}

func tokenize(spec string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range spec {
		switch {
		case r == '"':
			cur.WriteRune(r)
			if inQuote {
				out = append(out, cur.String())
				cur.Reset()
			}
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			if cur.Len() > 0 {
				out = append(out, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteRune(r)
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}
