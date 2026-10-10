package ui

import (
	"image/color"
	"strconv"
	"strings"
	"unicode/utf8"

	lg "charm.land/lipgloss/v2"
)

// ---- glamour (ANSI) output to spans ----

// ansiToSpans parses SGR-styled text (glamour's output) into spans, so it
// can be wrapped, selected and highlighted like everything else.
func ansiToSpans(s string) []Span {
	var spans []Span
	st := lg.NewStyle()
	var buf strings.Builder
	flush := func() {
		if buf.Len() > 0 {
			spans = append(spans, Span{Text: buf.String(), Style: st})
			buf.Reset()
		}
	}
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) {
			switch s[i+1] {
			case '[':
				j := i + 2
				for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
					j++
				}
				if j < len(s) && s[j] == 'm' {
					flush()
					st = applySGR(st, s[i+2:j])
				}
				i = j + 1
				continue
			case ']':
				j := i + 2
				for j < len(s) && s[j] != 0x07 && !(s[j] == 0x1b && j+1 < len(s) && s[j+1] == '\\') {
					j++
				}
				if j < len(s) && s[j] == 0x1b {
					j++
				}
				i = j + 1
				continue
			}
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == '\t' {
			buf.WriteString("    ")
		} else {
			buf.WriteRune(r)
		}
		i += size
	}
	flush()
	return spans
}

func applySGR(st lg.Style, params string) lg.Style {
	if params == "" {
		return lg.NewStyle()
	}
	ps := strings.Split(params, ";")
	for k := 0; k < len(ps); k++ {
		n, _ := strconv.Atoi(ps[k])
		switch {
		case n == 0:
			st = lg.NewStyle()
		case n == 1:
			st = st.Bold(true)
		case n == 2:
			st = st.Faint(true)
		case n == 3:
			st = st.Italic(true)
		case n == 4:
			st = st.Underline(true)
		case n == 7:
			st = st.Reverse(true)
		case n == 27:
			st = st.Reverse(false)
		case n == 9:
			st = st.Strikethrough(true)
		case n == 22:
			st = st.Bold(false).Faint(false)
		case n == 23:
			st = st.Italic(false)
		case n == 24:
			st = st.Underline(false)
		case n == 29:
			st = st.Strikethrough(false)
		case n >= 30 && n <= 37:
			st = st.Foreground(lg.ANSIColor(n - 30))
		case n >= 90 && n <= 97:
			st = st.Foreground(lg.ANSIColor(n - 90 + 8))
		case n >= 40 && n <= 47:
			st = st.Background(lg.ANSIColor(n - 40))
		case n >= 100 && n <= 107:
			st = st.Background(lg.ANSIColor(n - 100 + 8))
		case n == 39:
			st = st.UnsetForeground()
		case n == 49:
			st = st.UnsetBackground()
		case n == 38 || n == 48:
			var c color.Color
			if k+2 < len(ps) && ps[k+1] == "5" {
				v, _ := strconv.Atoi(ps[k+2])
				c = lg.ANSIColor(v)
				k += 2
			} else if k+4 < len(ps) && ps[k+1] == "2" {
				r, _ := strconv.Atoi(ps[k+2])
				g, _ := strconv.Atoi(ps[k+3])
				b, _ := strconv.Atoi(ps[k+4])
				c = color.RGBA{uint8(r), uint8(g), uint8(b), 255}
				k += 4
			}
			if c != nil {
				if n == 38 {
					st = st.Foreground(c)
				} else {
					st = st.Background(c)
				}
			}
		}
	}
	return st
}
