# Tags queries

The `tags.scm` files are the grammars' own, copied unchanged at the commits
gotreesitter v0.55.1 builds its grammars from (`grammars/languages.lock`),
each with its licence:

| File | Upstream | Commit |
|---|---|---|
| `typescript/tags.scm` | tree-sitter/tree-sitter-typescript `queries/tags.scm` | `75b3874edb2dc714fb1fd77a32013d0f8699989f` |
| `javascript/tags.scm` | tree-sitter/tree-sitter-javascript `queries/tags.scm` | `58404d8cf191d69f2674a8fd507bd5776f46cb11` |
| `ruby/tags.scm` | tree-sitter/tree-sitter-ruby `queries/tags.scm` | `ad907a69da0c8a4f7a943a7fe012712208da6dee` |

TypeScript and TSX use the TypeScript file with the JavaScript one appended,
as tree-sitter-typescript's `tree-sitter.json` says.

`query.go` adapts them when it loads them, rather than editing the files:

- It drops each pattern's leading `(comment)* @doc .` and the `#strip!` and
  `#select-adjacent!` directives on `@doc`. With that prefix, gotreesitter
  only matches a definition that is its parent's first child, so most
  methods were missed. The analyzer walks the tree for leading comments
  instead.
- Ruby's last pattern (a bare identifier is a call unless it's a local)
  needs `#is-not? local`, which gotreesitter doesn't evaluate, so every
  identifier matched. The analyzer ignores calls that aren't `call` nodes.

`typescript/extra.scm` is ours: constructors (which the JavaScript file
leaves out on purpose) and class fields holding a function.
