# Questions run in auto mode, in a worktree pinned to the Review Session's head

Every agent run for a Review Session happens in a git worktree the tool owns, in the user data directory. That covers the Briefing, each Question fork, follow-ups and Refine.

**The worktree:**
- It's a detached checkout at the Review Session's head commit.
- It's never the Reviewer's own checkout, and nothing in it is ever edited or committed.
- "Update to latest" checks out the new head in the *same* worktree, because Claude Code sessions are keyed by their working directory and the Briefing's forks must keep resuming.

**Running the agent:**
- The agent runs in `claude -p --permission-mode auto`.
- Writing to the repo is denied by path-scoped rules, and each Review Session gets a writable scratch directory, so the agent can run tests and write throwaway scripts but not change the code.
- A Probe is simply something the agent ran. The Answer lists it (`ran`), and that is what makes the Answer Verified.
- There is no per-Thread permission dialog in v1. MCP servers are limited to an allowlist, and their tools are left to auto mode.

**Why:**
- **Answers always describe the head the Review Session is pinned to.** When the Reviewer is the Author, their Implementing Agent edits their checkout at the same time as Questions read code. Reading from that checkout would mix uncommitted edits into Answers.
- **Auto mode lets the agent verify claims without the Reviewer approving each command.** Session 1's design required approval for every Probe. The Reviewer judged that friction not worth it for v1, while still wanting files outside a scratch area protected.

## Considered options

- **Run Questions in the Reviewer's checkout.** Rejected: the Implementing Agent edits it at the same time, and Answers would describe code that doesn't match the Review Session's head.
- **Read-only tools only (no Bash).** Rejected: it blocks running a test or writing a small script to analyse something, which is how an Answer gets Verified.
- **A Probe permission dialog per Thread** (session 1's design, built in the prototype). Deferred: too much friction for v1. It comes back if auto mode proves too permissive.
- **Auto mode with MCP tools that write denied.** Rejected for v1 simplicity. MCP servers on the allowlist are left to auto mode.

## Consequences

- **Only committed, pushed code is reviewable.** "Latest" means the PR's head on GitHub, so the Implementing Agent has to push.
- **Gitignored files aren't in the worktree:** `.env`, `node_modules` and build outputs. A Probe that needs them fails, and the Answer stays Unverified. A repo's `post-checkout` hook, which also runs on `git worktree add`, can do the setup.
- **Paths need rewriting.** Citation and `ran` paths are rewritten from the worktree to repo-relative before they're stored.
- **Cleanup:** the worktree is removed and pruned when its PR is merged or closed, or by `prune`. The event log stays readable.
- **The first build must check that path-scoped write denials hold under auto mode.** If they don't, the worktree being disposable is the fallback protection.
- **Related Codebases are read live from their own clones, not pinned.**
