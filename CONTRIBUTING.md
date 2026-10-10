# Contributing

Open a Pull Request against `master`. Contributions are accepted under the [Apache License 2.0](LICENSE), as the license's section 5 describes. Add yourself to [`CONTRIBUTORS.md`](CONTRIBUTORS.md) in your first PR.

The domain vocabulary (Review Session, Touched Method, Feedback, Revision, …) is in [`GLOSSARY.md`](GLOSSARY.md), and the reasoning behind the larger decisions is in [`docs/adr/`](docs/adr/). Use the glossary's words in code, tests and commit messages.

## Building and testing

You need Go 1.27 (`mise install` sets it up from `mise.toml`), git and `gh`.

```sh
mise run install    # go install .
mise run test       # go vet ./... and go test ./... (the UI scenarios take about 6 minutes)
PR_REVIEW_LIVE=1 go test -run TestLive ./internal/claudeagent/   # the live claude smoke test; costs a few cents
```

The tests never call the real agent or GitHub. They run on a fixture Pull Request, described below.

## Layout

| Path | What |
|---|---|
| `main.go` | the CLI: the TUI, `export`, `resolve`, `skill install`, `prune` |
| `internal/app` | opening a Review Session from a PR number or link, `export` / `resolve`, completion |
| `internal/engine` | the engine: commands in, events out; the agent queue and the cost guard |
| `internal/session` | the Review Session's domain types, events and the reducer that replays them |
| `internal/eventlog` | the append-only JSONL event log |
| `internal/ui` | the Bubble Tea TUI, and its scenario tests |
| `internal/claudeagent`, `internal/fixtureagent` | the live `claude -p` agent and the offline fixture agent |
| `internal/forge` | GitHub through `gh` and `git`, plus a fake for tests |
| `internal/codeanalysis` | Touched Methods: Go (`go/parser`), TypeScript, TSX and Ruby (tree-sitter), exact Go callers |
| `internal/fixturerepo` | builds the git repo behind the fixture PR |
| `testdata/dispatchly/` | the fixture PR: a small made-up Go app and the agent's canned outputs (see its README) |
| `tools/build_fixtures.py` | regenerates those canned outputs, resolving every line number from the source |
| `skills/pull-review/` | the bundled `/pull-review` skill |
| `demo/` | renders the README's GIFs with [VHS](https://github.com/charmbracelet/vhs) (`mise run demo-gif`; see `demo/README.md`) |

## The fixture PR

`testdata/dispatchly/` holds a made-up Go app in three snapshots and a made-up Pull Request on it (`c1..c2`). `go test ./internal/fixtureagent` checks that every line the canned answers point at exists, so after editing the source, run `python3 tools/build_fixtures.py` and the tests.
