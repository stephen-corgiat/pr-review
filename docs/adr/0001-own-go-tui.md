---
status: accepted
---

# Our own Go + Bubble Tea v2 TUI, not Hunk or OpenTUI

The Review Session UI is our own terminal UI in Go, on Bubble Tea v2, Lip Gloss v2, chroma and Glamour. Hunk looks like the obvious host, because it already does syntax-highlighted diffs, mouse selection and inline agent notes. But a Hunk host can't render our own cards, can't edit agent notes, has no place for the PR description or the Briefing, and doesn't persist anything. Its extension API is experimental and changes every week or two. Forking Hunk means owning about 97k lines of TypeScript that release every one or two weeks. OpenTUI gives drag, double-click and triple-click selection for free. But it's pre-1.0, needs Bun or Node's experimental FFI, lacks grammars for Go's companions Ruby and TypeScript in its own highlighter, and Hunk's diff components on it have no slot for inline Thread cards. Go keeps the engine, the UI and the agent adapters in one language and one static binary. The price is hand-building mouse selection (click timing, word and line expansion, painting) and a diff view that highlights the full source. **The condition:** the first prototype proves the selection and the action bar. If they feel worse than Hunk's, we switch to OpenTUI before building further. See `research/tui-stack.md` in the project notes.
