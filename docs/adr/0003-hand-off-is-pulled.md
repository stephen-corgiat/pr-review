# Hand-off is pulled by the Implementing Agent, not pushed by the tool

When the Reviewer is the Author, ready Feedback goes to the Implementing Agent rather than to GitHub. The Reviewer queues it in the TUI; the Implementing Agent pulls it in by running a skill (`/pull-review`) that calls `pr-review export`, which prints the queued Feedback and its research as markdown and marks it handed off. The agent then records an Outcome for each item with `pr-review resolve`, and those land as events in the Review Session's log (ADR 0002). We chose pull because it works in whichever agent session the Author prefers (the original implementing chat with all its context, or a fresh one, in Claude Code or later Cursor), and because it keeps the tool read-only on the Author's working copy: the tool only ever writes to its own worktree.

## Considered options

- **Push: the TUI starts the Implementing Agent itself** (`claude -p` in the working copy). Rejected: it can't reuse the Author's existing chat, it ties Hand-off to one agent backend, and it would have the tool changing code in a checkout it doesn't own.
- **Via GitHub: post the Feedback as a pending review and let the agent read it with `gh`.** Rejected: drafts leave the machine, reviewers and bots can see or be notified about them, and research Threads don't fit in review comments.
- **Export takes every ready item with no step in the TUI.** Rejected: "ready" would silently mean "sent" on your own PRs. Queuing in the TUI keeps "I've finished writing this" separate from "send it now".

## Consequences

- There is no Own PR mode. A Review Session is the same whoever the Author is; only the default destination in the send dialog changes (Hand to agent when `gh`'s user is the PR's author).
- The export format (markdown, per-item IDs, Anchors quoted at the head sha they were written against) is an interface the skill depends on, and must stay stable across tool versions.
- The export carries every answered Thread in the Review Session, not research hand-picked per Feedback item; the Implementing Agent works out which research applies (attaching by hand proved tedious in the prototype).
- Unfilled Agent Notes are allowed in a Hand-off (the Implementing Agent fills or acts on them) but block posting to GitHub.
- **Amended in session 4 (2026-09-29):**
  - **Outcomes are optional.** The skill imports the Feedback and research and only *offers* `resolve`. It doesn't prescribe a workflow: the Author may fix, grill, or hand the work to another agent.
  - **A commit is optional within an Outcome.** Tying one commit to each item isn't realistic.
  - **The Reviewer has the final say.** They can mark handed-off Feedback done, or reopen it, whatever an Outcome says.
  - **Export checks where it runs.** It refuses outside a checkout of the PR's repo on the PR's branch, and leaves the items queued. It marks them handed off only once that check passes.
