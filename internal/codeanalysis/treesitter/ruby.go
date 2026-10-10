package treesitter

import (
	sitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars/ruby"
)

// Ruby: tree-sitter-ruby's tags.scm. Methods (def x) and class methods
// (def self.x, or def x inside class << self) are Touched Methods, owned
// by the innermost class or module.
//
// Known gaps (written down as expected results in the tests):
//   - calls without parentheses or a receiver (helper, new) are missed:
//     they parse as a bare identifier, like a local variable. tags.scm
//     tells them apart with #is-not? local, which gotreesitter doesn't
//     evaluate, so only call nodes count
//   - the comment before a class's or module's first method belongs to
//     the class node (it sits before the body), so that method's range
//     starts at its def
func init() {
	languages = append(languages, &language{
		name: "ruby", family: "ruby", exts: []string{".rb", ".rake"},
		grammar:    ruby.Language,
		queries:    []string{"ruby/tags.scm"},
		skipDef:    set("alias"),
		funcs:      set("method", "singleton_method"),
		leading:    set("comment"),
		owner:      rubyOwner,
		call:       rubyCall,
		bareIsSelf: true,
		declStops:  rubyDeclStops,
	})
}

// rubyOwner is the innermost class or module around a method; a
// singleton_method, or a method inside class << self, is a class method.
func rubyOwner(n *sitter.Node, l *sitter.Language, src []byte) (string, bool) {
	singleton := n.Type(l) == "singleton_method"
	for p := n.Parent(); p != nil; p = p.Parent() {
		switch p.Type(l) {
		case "singleton_class":
			singleton = true
		case "class", "module":
			if name := p.ChildByFieldName("name", l); name != nil {
				return name.Text(src), singleton
			}
		}
	}
	return "", singleton
}

// rubyCall reads f(…), self.f and x.f from the name node; anything that
// isn't a call node's method (tags.scm's bare identifiers) is skipped.
func rubyCall(name *sitter.Node, l *sitter.Language, src []byte) (callKind, bool) {
	p := name.Parent()
	if p == nil || p.Type(l) != "call" {
		return 0, false
	}
	if m := p.ChildByFieldName("method", l); m == nil || m.StartByte() != name.StartByte() {
		return 0, false
	}
	recv := p.ChildByFieldName("receiver", l)
	switch {
	case recv == nil:
		return callBare, true
	case recv.Type(l) == "self":
		return callSelf, true
	}
	return callOther, true
}

var rubyDecls = set("method", "singleton_method", "class", "module", "singleton_class")

// rubyDeclStops are the methods, classes and modules (and constants
// assigned in them) at the top level and inside classes and modules.
func rubyDeclStops(root *sitter.Node, l *sitter.Language) []*sitter.Node {
	var out []*sitter.Node
	var walk func(n *sitter.Node)
	walk = func(n *sitter.Node) {
		for i := 0; i < n.NamedChildCount(); i++ {
			c := n.NamedChild(i)
			switch t := c.Type(l); {
			case rubyDecls[t]:
				out = append(out, c)
				if t != "method" && t != "singleton_method" {
					walk(c)
				}
			case t == "body_statement":
				walk(c)
			case t == "assignment":
				if left := c.ChildByFieldName("left", l); left != nil && left.Type(l) == "constant" {
					out = append(out, c)
				}
			}
		}
	}
	walk(root)
	return out
}
