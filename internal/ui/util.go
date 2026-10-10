package ui

import (
	"fmt"
	"strconv"
	"strings"

	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Catppuccin Mocha, to sit with the chroma style.
const (
	cText     = "#cdd6f4"
	cSubtext1 = "#bac2de"
	cSubtext0 = "#a6adc8"
	cOverlay1 = "#7f849c"
	cOverlay0 = "#6c7086"
	cSurface2 = "#585b70"
	cSurface1 = "#45475a"
	cSurface0 = "#313244"
	cMantle   = "#181825"
	cCrust    = "#11111b"
	cBlue     = "#89b4fa"
	cLavender = "#b4befe"
	cTeal     = "#94e2d5"
	cGreen    = "#a6e3a1"
	cYellow   = "#f9e2af"
	cPeach    = "#fab387"
	cRed      = "#f38ba8"
	cMauve    = "#cba6f7"
	cPink     = "#f5c2e7"

	bgAdded         = "#1e3a2a"
	bgRemoved       = "#3d1f24"
	bgAddedEmph     = "#2f6143"
	bgRemovedEmph   = "#6e2c38"
	bgCursor        = cSurface0
	bgCursorAdded   = "#2a5139"
	bgCursorRemoved = "#552a32"
	bgSel           = cSurface2
	bgCard          = "#262637"
	bgSearch        = "#6b5a1e" // every search match: amber, light text stays readable
	bgSearchCur     = cYellow   // the current match (with dark text)
	bgMethodBand    = "#1f2233" // the note block above a Touched Method
)

func fg(c string) lg.Style { return lg.NewStyle().Foreground(lg.Color(c)) }

var (
	stText       = fg(cText)
	stSub        = fg(cSubtext0)
	stDim        = fg(cOverlay0)
	stFaint      = fg(cSurface2)
	stBold       = fg(cText).Bold(true)
	stAccent     = fg(cBlue).Bold(true)
	stLink       = fg(cLavender).Underline(true)
	stHeading    = fg(cBlue).Bold(true)
	stSubheading = fg(cPeach).Bold(true)
	stMethod     = fg(cMauve)
	stMethodBold = fg(cMauve).Bold(true)
	stNeedsYou   = fg(cRed).Bold(true)
	stNew        = fg(cBlue).Bold(true)
	stRunning    = fg(cYellow)
	stDraft      = fg(cSubtext0)
	stRead       = fg(cOverlay1)
	stAdded      = fg(cGreen)
	stRemoved    = fg(cRed)
	stModified   = fg(cPeach)
	stGear       = fg(cTeal)
	stVerified   = fg(cGreen).Bold(true)
	stFeedback   = fg(cPink)
	stKey        = fg(cBlue).Bold(true)
)

func stripANSI(s string) string { return ansi.Strip(s) }

func cellWidth(s string) int { return ansi.StringWidth(s) }

func runeWidth(r rune) int {
	if r < 0x80 {
		if r < 0x20 {
			return 0
		}
		return 1
	}
	return ansi.StringWidth(string(r))
}

func clamp(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// padTo pads or truncates s (styled or plain) to exactly width cells.
func padTo(s string, width int) string {
	if width <= 0 {
		return ""
	}
	w := cellWidth(s)
	if w > width {
		return ansi.Truncate(s, width, "")
	}
	if w < width {
		return s + strings.Repeat(" ", width-w)
	}
	return s
}

// truncEllipsis truncates s to width cells, with "…" when it doesn't fit.
func truncEllipsis(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if cellWidth(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	return ansi.Truncate(s, width-1, "") + "…"
}

// truncLeft keeps the right-hand end of s, prefixing "…".
func truncLeft(s string, width int) string {
	if cellWidth(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	rs := []rune(s)
	for i := range rs {
		if cellWidth(string(rs[i:])) <= width-1 {
			return "…" + string(rs[i:])
		}
	}
	return "…"
}

// bgSeq is the SGR sequence that sets a truecolor background.
func bgSeq(hex string) string {
	h := strings.TrimPrefix(hex, "#")
	if len(h) != 6 {
		return ""
	}
	r, _ := strconv.ParseUint(h[0:2], 16, 8)
	g, _ := strconv.ParseUint(h[2:4], 16, 8)
	b, _ := strconv.ParseUint(h[4:6], 16, 8)
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

// paintBG pads line to width and gives every cell that has no background of
// its own the bg colour, by re-setting it after each SGR reset.
func paintBG(line string, bg string, width int) string {
	line = padTo(line, width)
	seq := bgSeq(bg)
	line = strings.ReplaceAll(line, "\x1b[m", "\x1b[m"+seq)
	line = strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+seq)
	return seq + line + "\x1b[m"
}

// firstLine returns s up to its first newline.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// oneLine collapses whitespace runs (including newlines) to single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func shortenPath(p string) string {
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return strings.Join(parts[len(parts)-2:], "/")
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
