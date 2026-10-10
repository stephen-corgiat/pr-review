// Package highlight tokenises whole files with chroma and turns each line
// into a Lip Gloss-styled string, indexed by old and new line number so
// multi-line constructs (heredocs, block comments, multi-line SQL strings...)
// stay correct even though only a diff is shown.
package highlight

import (
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"

	lg "charm.land/lipgloss/v2"
)

// StyleName is the chroma style used, resolved once at startup (see Setup).
var StyleName = "catppuccin-mocha"

var activeStyle *chroma.Style

// Setup resolves the style to use: override if non-empty and valid, else
// catppuccin-mocha, else monokai. Returns the resolved name for reporting.
func Setup(override string) string {
	if override != "" {
		if s := styles.Get(override); s != nil && s.Name == override {
			StyleName = override
			activeStyle = s
			return StyleName
		}
	}
	if s := styles.Registry["catppuccin-mocha"]; s != nil {
		StyleName = "catppuccin-mocha"
		activeStyle = s
		return StyleName
	}
	StyleName = "monokai"
	activeStyle = styles.Get("monokai")
	return StyleName
}

// LexerFor picks a lexer by filename, falling back to content analysis then
// plaintext. It returns the lexer's config name for reporting (e.g. "Vue" or
// "HTML" if chroma has no Vue lexer).
func LexerFor(path, content string) (chroma.Lexer, string) {
	l := lexers.Match(path)
	if l == nil {
		l = lexers.Analyse(content)
	}
	if l == nil {
		l = lexers.Fallback
	}
	return l, l.Config().Name
}

// Span is one run of text with a single style.
type Span struct {
	Text  string
	Style lg.Style
}

// File holds a whole file's lines, each already turned into styled spans.
type File struct {
	Lines [][]Span // 1-based: Lines[0] is unused, Lines[1] is line 1
	Lexer string
}

// Tokenise highlights a whole file's content and indexes the result by line
// number (1-based).
func Tokenise(path, content string) File {
	if activeStyle == nil {
		Setup("")
	}
	l, lexName := LexerFor(path, content)
	it, err := l.Tokenise(nil, content)
	if err != nil {
		return plainFile(content)
	}
	var tokens []chroma.Token
	for t := it(); t != chroma.EOF; t = it() {
		tokens = append(tokens, t)
	}
	lines := chroma.SplitTokensIntoLines(tokens)
	out := File{Lines: make([][]Span, len(lines)+1), Lexer: lexName}
	for i, line := range lines {
		spans := make([]Span, 0, len(line))
		for _, tok := range line {
			text := tok.Value
			text = strings.TrimSuffix(text, "\n")
			if text == "" {
				continue
			}
			spans = append(spans, Span{Text: text, Style: styleFor(tok.Type)})
		}
		out.Lines[i+1] = spans
	}
	return out
}

func plainFile(content string) File {
	lines := strings.Split(content, "\n")
	out := File{Lines: make([][]Span, len(lines)+1), Lexer: "plaintext"}
	for i, l := range lines {
		out.Lines[i+1] = []Span{{Text: l}}
	}
	return out
}

func styleFor(tt chroma.TokenType) lg.Style {
	e := activeStyle.Get(tt)
	st := lg.NewStyle()
	if e.Colour.IsSet() {
		st = st.Foreground(lg.Color(e.Colour.String()))
	}
	if e.Bold == chroma.Yes {
		st = st.Bold(true)
	}
	if e.Italic == chroma.Yes {
		st = st.Italic(true)
	}
	if e.Underline == chroma.Yes {
		st = st.Underline(true)
	}
	return st
}

// AddedTint and RemovedTint are the dim background tints for whole added and
// removed diff rows.
const (
	AddedTint   = "#1e3a2a"
	RemovedTint = "#3d1f24"
)

// RenderLine renders one line's spans, tinting the background for added or
// removed rows. width<=0 means "no truncation here" (caller handles width).
func RenderLine(spans []Span, bgTint string) string {
	var sb strings.Builder
	for _, sp := range spans {
		st := sp.Style
		if bgTint != "" {
			st = st.Background(lg.Color(bgTint))
		}
		sb.WriteString(st.Render(sp.Text))
	}
	return sb.String()
}

// PlainText returns a line's plain text, with no styling.
func PlainText(spans []Span) string {
	var sb strings.Builder
	for _, sp := range spans {
		sb.WriteString(sp.Text)
	}
	return sb.String()
}

// TokeniseLang highlights a snippet (a Sample Data value) by language name
// ("go", "json", "http"...), falling back to analysis, then plaintext.
func TokeniseLang(lang, content string) File {
	if activeStyle == nil {
		Setup("")
	}
	l := lexers.Get(lang)
	if l == nil {
		l = lexers.Analyse(content)
	}
	if l == nil {
		return plainFile(content)
	}
	it, err := l.Tokenise(nil, content)
	if err != nil {
		return plainFile(content)
	}
	var tokens []chroma.Token
	for t := it(); t != chroma.EOF; t = it() {
		tokens = append(tokens, t)
	}
	lines := chroma.SplitTokensIntoLines(tokens)
	out := File{Lines: make([][]Span, len(lines)+1), Lexer: l.Config().Name}
	for i, line := range lines {
		for _, tok := range line {
			if text := strings.TrimSuffix(tok.Value, "\n"); text != "" {
				out.Lines[i+1] = append(out.Lines[i+1], Span{Text: text, Style: styleFor(tok.Type)})
			}
		}
	}
	return out
}
