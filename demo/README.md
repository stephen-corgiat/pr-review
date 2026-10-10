# The README demos

Two GIFs, each rendered from a tape with [VHS](https://github.com/charmbracelet/vhs). Both re-enact reviewing [holocron](https://github.com/stephen-corgiat/holocron) PR #66.

- **`docs/media/demo.gif`** (1100×700, about 50 seconds), from `review.tape`: read the Briefing, select the streak advance in `applyDay` and ask for Sample Data, select the new guard and ask the agent to Define it, write Feedback that recommends a different approach and ends in an `<<example using …>>` placeholder, Refine it so the agent fills in the example, accept it, and send it.
- **`docs/media/handoff.gif`** (1100×700), from `handoff.tape`: the same Feedback, handed to the Author's agent instead. Send it with **Hand to agent**, start Claude Code in the Author's checkout, run `/pull-review`, ask it to fix F1, commit, push and record the Outcome, then reopen the PR in pr-review: F1 shows `✓ addressed` with the commit and note, and `D` marks it done.

```sh
mise run demo-gif           # demo.gif: replays demo/recording, free, the same every time
mise run demo-gif-handoff   # handoff.gif: the Claude Code session is live (see below)
demo/render.sh record       # demo.gif, running the live agent for anything demo/recording lacks
```

They need `ffmpeg`, a holocron checkout that has PR #66's commits (`$HOLOCRON_DIR`, default `~/Projects/holocron`) and the JetBrains Mono Nerd Font. The mise tasks fetch `vhs` and `ttyd`. A render builds `pr-review` from this checkout and uses a throwaway data directory and default settings, so it never touches your real Review Sessions.

## How they stay repeatable

`PR_REVIEW_REPLAY=record:DIR` or `replay:DIR` is a hidden switch, used only for these demos (`internal/replayagent`):

- **The agent.** Each Briefing, Question and Refine is saved to `DIR` as JSON, keyed by what was asked (the Question, the anchored code, the Feedback text), not by session ids. Replay serves those files after a short delay and never starts `claude`. Recording only calls the live agent for what's missing; delete a file to re-record it. If you change what the tape selects or types, the key changes and that call is recorded again.
- **GitHub.** PR #66 is merged, and a replay shouldn't need network access, so the PR's metadata is recorded once in `DIR/forge.json` and served as an **open** PR, to the TUI and to `pr-review export` and `resolve`. The Reviewer is `demo-reviewer`. Nothing is ever posted to GitHub, recording included: the send dialog's "Posted" message is made up.

The tapes' `Hide` / `Show` pairs cut the agent's thinking time out of the GIF, and their waits use `Wait+Screen` (a bare `Wait` only reads the cursor's line, which a full-screen TUI never moves).

## The hand-off demo's live session

`handoff.tape` can't replay Claude Code, so each render runs a real session (Opus 5.5, auto mode, on your login) that writes a migration and a service change: expect a few minutes and a session's worth of usage. It plays out a little differently every time, so look at the GIF before committing it.

`render.sh handoff` sets the session up so it can't reach anything real:

- **The Author's checkout** is a throwaway clone of `$HOLOCRON_DIR` on the PR's branch. Its `origin` still names GitHub, because `pr-review export` matches the repo by its remote, but its push URL is a local bare repo: Claude's `git push` lands there.
- **The dependencies** are a fresh `npm ci` in the clone, so the unit tests run. Never symlink them from `$HOLOCRON_DIR`: an earlier version did, and Claude's `npm ci` in the clone emptied the real `node_modules` through the links.
- **Your dev stack is out of reach.** The clone's directory is also named `holocron`, so `docker compose` would reach your containers, and the app's defaults reach your dev Postgres on `localhost:5432`. An earlier render applied a migration and ran the e2e suite against that database. The render now sets `COMPOSE_PROJECT_NAME` to a demo name and points `POSTGRES_PORT` at a closed port (the clone has no `.env`). The tape also denies docker, the e2e suite and migrations in the session.
- **`/pull-review`** is installed as a project skill in the clone, and Claude runs with `--setting-sources project`, so your user settings (status line, hooks, plugins) stay out of the GIF.

The whole session is one turn: `/pull-review` with the instruction as its arguments. The tape waits for the line Claude Code prints when a turn ends (`✻ … for 5m 7s · done 11:13 PM`). Waiting for `resolve`'s own output didn't work: under the long summary that follows it, the line is gone before VHS sees it. The clone is a new folder each time, so Claude Code asks whether to trust it. Because holocron's `.claude/settings.json` pre-approves some tools, that prompt defaults to "No, exit", so the tape waits for it and picks "Yes" explicitly. It then waits for Claude Code's banner (`Claude Code v…`) before typing. Those waits time out after a minute, so a render with an unexpected prompt fails fast instead of hanging. If your Claude Code shows another prompt first (an auto mode opt-in, say), accept it once by hand in any directory and render again.

## What's in `recording/`

The recorded Briefing, Sample Data and Define Answers, and Refine Revision from the live agent (Opus 5.5 at the default effort: $0.64 for the four calls), and the PR's metadata. They quote excerpts of holocron's source, and so does the hand-off GIF. Holocron is the maintainer's own private repo, and its owner has agreed to this use. To re-record the demos against another repo, change `HOLOCRON_DIR` and the PR number in the tapes, and delete `demo/recording` so it is recorded afresh.
