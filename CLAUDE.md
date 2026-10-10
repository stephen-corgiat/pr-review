# pr-review

The PR Review Assistant: a private TUI workspace where a Reviewer builds their understanding of a Pull Request by asking an agent anchored Questions, and refines Feedback before it reaches the Author. Vocabulary is in `GLOSSARY.md`; decisions are in `docs/adr/`.

## Layout

The Go module (`github.com/stephen-corgiat/pr-review`) is at the repo root: `main.go` and `internal/`. `README.md` has the getting-started guide and a package map. `demo/` renders the README's demo GIF (`mise run demo-gif`).

## Agent skills

### Domain docs

Single-context: `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
