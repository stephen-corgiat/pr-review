# pr-review

A private terminal workspace for reviewing a Pull Request. Ask an agent questions about the code, then sharpen your Feedback before the Author sees it.

![Reviewing a Pull Request in pr-review: ask the agent for Sample Data and to Define a selection, write Feedback with an <<example …>> placeholder, let the agent Refine it into a worked example, accept it, and send it as a GitHub review.](docs/media/demo.gif)

pr-review isn't an auto-review bot. The agent answers the questions you ask and tightens the Feedback you write; you decide what to say. Feedback goes out as a GitHub review, or, when the PR is yours, straight to your coding agent.

## Install

You need [git](https://git-scm.com), the [GitHub CLI](https://cli.github.com) (`gh auth login`) and [Claude Code](https://claude.com/claude-code), signed in, and Go 1.27 to build it. Linux and macOS work. Use a terminal with mouse support: [Ghostty](https://ghostty.org) is the reference.

```sh
go install github.com/stephen-corgiat/pr-review@latest
pr-review skill install      # adds /pull-review to Claude Code, for handing Feedback to an agent
```

`go install` puts `pr-review` in `$(go env GOPATH)/bin`, usually `~/go/bin`. Make sure that's on your `PATH`.

## Review a PR

In a checkout of the repo the PR belongs to, run `pr-review` for the current branch's PR, or `pr-review 123` for PR #123. From anywhere, give it the link:

```sh
pr-review https://github.com/acme/calc/pull/123
```

Your checkout is never touched. The agent works in its own worktree, pinned to the PR's head, and can run commands there, but writes to the repo are denied. The diff is taken against the PR's base, so a stacked PR shows only its own changes. Everything is saved as you go, and reopening a PR picks up where you left off.

Press `?` at any time for every key. `q` quits.

### Read the Briefing

The Briefing opens first: a TL;DR, a reading order, and what each Touched Method does and what changed in it. Read it top to bottom, or jump around. `}` and `{` move between files, `J` and `K` between Touched Methods, and `]` and `[` step through every change in the PR.

![The Briefing: a TL;DR, a reading order and the Touched Methods.](docs/media/01-briefing.png)

### Ask a Question

Select some code, by dragging, by double-clicking a word, or with `v` and `j`/`k` for whole lines. Then press a key for the kind of Question:

| Key | Asks the agent to |
|---|---|
| `d` | **Define** the selection |
| `s` | **Summarize** it and what changed |
| `x` | show **Sample Data** flowing through it |
| `t` | **Trace** what calls it and what it calls |
| `c` | find the **Test Cases** that cover it |
| `r` | answer a Question you type |

The Answer shows up as a card right at the code. A ✅ means the agent ran something to confirm it, and a 📖 means it's from reading alone. The agent's running cost is in the footer.

![A Sample Data Answer as a card at the code it was asked about, marked 📖 because the agent only read the code.](docs/media/02-sample-data.png)

![A Define Answer on a selected guard, with an example and links to the code it cites.](docs/media/03-define.png)

### Write Feedback

Select the code and press `f`. Write the Feedback the way you'd say it, and leave a note for the agent wherever you'd rather it filled in a detail: `<<quote the exact condition with its path:line>>`. A card with the Feedback and its notes sits right at the code.

![A Feedback card on the selected lines, with one agent note still to fill in.](docs/media/04-feedback.png)

Press `r` on the card to ask the agent to **Refine** it. It fills in each note from the code, and you read the Revision before you accept it with `A`. Press `space` to mark the Feedback ready.

![The agent's Revision of the Feedback: the note replaced by a worked example, with what changed and the code it cites.](docs/media/05-revision.png)

### Send it

Press `P`. **GitHub review** posts your ready Feedback to the PR as a review. **Hand to agent** is for when you're the PR's Author: GitHub won't let you review your own PR, so the Feedback goes to your coding agent instead.

![The send dialog: post to GitHub as a review, or hand the Feedback to an agent.](docs/media/06-send.png)

## Hand Feedback to your agent

![Handing Feedback to an agent: send F1 with Hand to agent, run /pull-review in a Claude Code session on the PR's branch so it fixes F1, commits, pushes and records the Outcome, then reopen the PR in pr-review to see F1 addressed and mark it done.](docs/media/handoff.gif)

After you send, open Claude Code in the PR's checkout, on its branch, and run `/pull-review`. It runs `pr-review export`, which prints your Feedback and the research behind it, and works through the items with you. As it goes, the agent can record what it did:

```sh
pr-review resolve F1 addressed "Renamed the flag" --commit abc1234
pr-review resolve F2 declined "Kept the retry; the API is flaky"
pr-review resolve F3 needs-you "Should this be configurable?"
```

Back in pr-review, each item shows its Outcome. A banner appears when the PR has new commits. `U` updates the session: Questions and Feedback whose code changed are marked Stale, and `Q` asks a Stale Question again. `S` shows only what changed since your last review. `D` marks handed-off Feedback done, and `O` reopens it.

## Configure

Everything is optional. Settings live in `~/.config/pr-review/settings.toml` (or `$PR_REVIEW_CONFIG_DIR/settings.toml`); unknown keys are reported, not fatal.

```toml
model = "claude-sonnet-5-5"     # default: claude-opus-5-5
effort = "medium"
sample_data = ["~/work/samples/orders.json"]

[budgets]                       # USD caps per agent call (defaults: 0.50 and 0.25)
briefing = 2.00
question = 2.00

[mcp]
allow = ["jira"]                # MCP servers the agent may use, by name

[[related_codebases]]           # other repos the agent may read
path = "~/work/billing"
alias = "BILLING"

[repos."github.com/acme/calc"]  # per-repo overrides, keyed by the remote
model = "claude-opus-5-5"
```

<details>
<summary>Shell completion</summary>

With completion loaded, `pr-review <tab>` lists the subcommands and the repo's open PRs, and `pr-review resolve <tab>` lists the Feedback waiting on you.

```sh
# bash: add to ~/.bashrc
source <(pr-review completion bash)

# zsh: once, into a directory on your $fpath
pr-review completion zsh > "${fpath[1]}/_pr-review"

# fish
pr-review completion fish > ~/.config/fish/completions/pr-review.fish
```

The PR list comes from `gh pr list` and is cached for 60 seconds. With no network, a tab press offers just the subcommands.

</details>

<details>
<summary>Where things are stored</summary>

Review Sessions live in the data directory, one per repo and PR: `~/.local/share/pr-review/` on Linux and `~/Library/Application Support/pr-review/` on macOS (or `$PR_REVIEW_DATA_DIR`). Each holds the session's event log, the agent's worktree and a scratch directory. `pr-review prune` removes the worktrees of PRs that are merged or closed.

A PR opened by link with no checkout around uses one of these, in order: the checkout its Review Session was opened from, the current directory's (when its `origin` is the PR's repo), or one another Review Session of that repo used. Otherwise pr-review clones the repo once into `clones/<owner>/<repo>` (blobless, nothing checked out) and reuses it.

</details>

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for building, testing and the code layout, and [`GLOSSARY.md`](GLOSSARY.md) for the words this project uses.

## License

Copyright 2026 Stephen Corgiat. Licensed under the [Apache License 2.0](LICENSE). Contributors are listed in [`CONTRIBUTORS.md`](CONTRIBUTORS.md).
