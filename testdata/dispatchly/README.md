# dispatchly: the fixture Pull Request

A made-up Pull Request on a made-up Go app, used by the tests and the offline fixture agent. Everything here was written for this repository and is covered by its license.

- `repo/c0`, `repo/c1`, `repo/c2` are three snapshots of "dispatchly", a small newsletter manager. `internal/fixturerepo` turns them into a git repo with fixed commits. The fixture PR is `c1..c2`: "Add Beacon bounce webhooks and settings support". `c0..c1` is an earlier commit that changes a method's signature, for `-repo` mode.
- `pr.json`, `briefing.json`, `answers.json`, `feedback.json` and `revisions.json` are the agent's canned outputs for that PR. `python3 tools/build_fixtures.py` regenerates them. It looks every line number up in `repo/c2`, so editing the source and re-running keeps them true. The agent text in the script is hand-written.
- Each tree is a Go module of its own (`go vet ./...` works inside it), and Go skips `testdata` when it builds the main module.
- `go test ./internal/fixtureagent` fails if an anchor, a citation or the Briefing's method list no longer matches the source.
