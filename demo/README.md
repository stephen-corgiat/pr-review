# The README demos

Two GIFs and a set of screenshots, each rendered from a tape with [VHS](https://github.com/charmbracelet/vhs). They all review the same made-up PR: **acme/bookstore #42, "Add discount codes at checkout"**. It adds percent-off codes to a small Go storefront API, and it has a bug a reviewer can spot: a code's minimum subtotal is only checked when the code is applied, so removing books afterwards keeps the discount.

- **`docs/media/demo.gif`** (1100×700, about 50 seconds), from `review.tape`: read the Briefing, select the new `Total` and ask for Sample Data, select the minimum-subtotal check and ask the agent to Define it, write Feedback that ends in an `<<example in Total>>` placeholder, Refine it so the agent fills in the example, accept it, and send it.
- **`docs/media/handoff.gif`** (1100×700), from `handoff.tape`: the same Feedback, handed to the Author's agent instead. Send it with **Hand to agent**, start Claude Code on the PR's branch, run `/pull-review Address the feedback in the review.`, and let it fix the code, commit, push and record the Outcome. Then reopen the PR in pr-review: F1 shows `✓ addressed` with the commit and note, and `D` marks it done. The banner reads "1 local commit not pushed" because the replayed PR's head never moves, unlike a real one.

```sh
mise run demo-gif           # demo.gif: replays demo/recording, free, the same every time
mise run demo-gif-handoff   # handoff.gif: the Claude Code session is live (see below)
demo/render.sh record       # demo.gif, running the live agent for anything demo/recording lacks
```

They need `ffmpeg`, Go and the JetBrains Mono Nerd Font. The mise tasks fetch `vhs` and `ttyd`. A render builds `pr-review` from this checkout and uses a throwaway data directory and default settings, so it never touches your real Review Sessions.

## The made-up repo

`demo/bookstore/` holds the repo's two trees: `base/` is `main`, and `pr/` is the `discount-codes` branch. Each has its own `go.mod`, so this module's `go vet ./...` and `go test ./...` skip them. `demo/bookstore.sh DIR` builds the repo from them with fixed authors, dates and git config, so its SHAs are the same on every machine and match `demo/recording/forge.json`. `render.sh` builds a fresh one for each render.

Its `origin` names `github.com/acme/bookstore`, because pr-review matches a repo by its remote, but nothing is fetched from there: the PR's commits are already in the repo.

If you change a file in `pr/`, the head SHA changes. Rebuild the repo, put the new SHA (and the new line counts, from `git diff --shortstat main discount-codes`) in `forge.json`, and record again. The Briefing is keyed by the head SHA, so it is recorded afresh, and so is any Question whose anchored code moved. A change to `base/` changes both SHAs.

## The screenshots

`demo/screenshots.tape` renders the README's walkthrough pictures (`mise run screenshots`). It repeats `review.tape`'s keystrokes exactly, because the replayed answers are matched on the Question, the anchored code and the Feedback text. It adds a **Trace** on the minimum-subtotal check.

It runs on a bigger terminal (1600×1800) at the GIFs' font size, so the cards reach their full 110 columns and whole Answers fit. Then `demo/crop.sh` cuts each raw shot (in `.scratch/shots/`, git-ignored) down with the boxes in `demo/crop.boxes`. Every box is 1100 pixels wide, the GIFs' width, so GitHub scales the screenshots and the GIFs alike and their text comes out the same size. The replay is deterministic, so the boxes are fixed pixel coordinates: if you change the tape or the terminal size, look at the raw shots and adjust them. It never writes `demo.gif`.

A new Question needs a recording first. `demo/render.sh screenshots record` calls the live agent for whatever `demo/recording` lacks and replays the rest. When you run a recording from inside a Claude Code session, clear the `CLAUDE*` environment variables first, or the agent's banner warns about an inherited session.

## How they stay repeatable

`PR_REVIEW_REPLAY=record:DIR` or `replay:DIR` is a hidden switch, used only for these demos (`internal/replayagent`):

- **The agent.** Each Briefing, Question and Refine is saved to `DIR` as JSON, keyed by what was asked (the Question, the anchored code, the Feedback text), not by session ids. Replay serves those files after a short delay and never starts `claude`. Recording only calls the live agent for what's missing; delete a file to re-record it. If you change what the tape selects or types, the key changes and that call is recorded again.
- **GitHub.** The PR's metadata is in `DIR/forge.json`, written by hand, and served as an open PR to the TUI and to `pr-review export` and `resolve`, in recording too. (Without a `forge.json`, a recording run fetches it from the real PR once.) The Reviewer is `demo-reviewer`. Nothing is ever posted to GitHub, recording included: the send dialog's "Posted" message is made up.

The tapes' `Hide` / `Show` pairs cut the agent's thinking time out of the GIF, and their waits use `Wait+Screen` (a bare `Wait` only reads the cursor's line, which a full-screen TUI never moves). Each tape starts by putting `$DEMO_PATH` back at the front of `PATH`, because VHS starts a fresh shell and your shell rc may reorder it. `$DEMO_PATH` holds the `pr-review` just built and the real Go toolchain. A version manager's shim won't do: in the agent's worktree, outside this repo, a mise shim has no Go version, and the Touched Methods' callers come up "unavailable".

## The hand-off demo's live session

`handoff.tape` can't replay Claude Code, so each render runs a real session (Opus 5.5, auto mode, on your login). The session edits the bookstore repo, runs `go test` and commits: expect a few minutes and a session's worth of usage. It plays out a little differently every time, so look at the GIF before committing it.

`render.sh handoff` checks out the PR's branch in the freshly built repo and points `origin`'s push URL at a local bare repo, so Claude's `git push` lands there. The bare repo starts with both branches, and the checkout has `origin/*` refs tracking them, as a clone would. Otherwise the push creates a new branch, and Claude's summary points out that `origin` is a local path. `/pull-review` is installed as a project skill in the repo, and Claude runs with `--setting-sources project`, so your user settings (status line, hooks, plugins) stay out of the GIF.

The whole session is one turn: `/pull-review` with the instruction as its arguments. The tape waits for the line Claude Code prints when a turn ends (`✻ … for 5m 7s · done 11:13 PM`). Waiting for `resolve`'s own output didn't work: under the long summary that follows it, the line is gone before VHS sees it. The repo is a new folder each time, so Claude Code asks whether to trust it, and that prompt defaults to "No, exit". The tape waits for it and picks "Yes" explicitly. It then waits for Claude Code's banner (`Claude Code v…`) before typing. Those waits time out after a minute, so a render with an unexpected prompt fails fast instead of hanging. If your Claude Code shows another prompt first (an auto mode opt-in, say), accept it once by hand in any directory and render again.

## What's in `recording/`

The PR's metadata (`forge.json`), and the Briefing, the Sample Data, Define and Trace Answers, and the Refine Revision from the live agent (Opus 5.5 at the default effort: about $0.37 for the five calls).
