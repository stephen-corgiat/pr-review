---
name: pull-review
description: Import the Reviewer's queued Feedback on this Pull Request, with the research behind it, from the pr-review tool into this session. Run it in a checkout of the PR's repo, on the PR's branch.
disable-model-invocation: true
---

# Pull review

You are the **Implementing Agent** for the Pull Request checked out here. The Reviewer queued
**Feedback** for you in the `pr-review` tool; this skill imports it, with the research behind
it. The tool never drives you and never touches this working copy.

## 1. Import the Feedback

Run `pr-review export` in this checkout.

- It prints markdown: a header (the PR, the head sha it was written against, instructions),
  one `## F<n>` section per item, then a `## Research` section with every answered Question
  from the review. It marks those items handed off.
- If it says **refused**, show the user its reason and stop. Nothing was lost: the items stay
  queued. It refuses outside a checkout of the PR's repo on the PR's branch, and on a merged or
  closed PR.
- If it says **Nothing queued**, tell the user the Reviewer hasn't queued anything, and stop.
- `pr-review export --again` prints the items already handed off again (a fresh session, say).

Done when you hold the export and the user knows what's in it: list the items in one line
each (ID, where, what's asked).

## 2. Work with the user

Follow the user's lead on what to do with the Feedback: they may want the items fixed, want to
talk some through first, or want to hand them to another agent. Don't start changing code
until they say how they want to proceed.

While you work:

- Text between `<<` and `>>` is an **Agent Note**: an instruction to you from the Reviewer,
  never text to keep.
- The quoted Anchors are the code as it was at the export's head sha. If the branch has moved,
  find the quoted lines before acting on them.
- Use the `## Research` section for the context behind an item. ✅ Answers were confirmed by
  something the agent ran, 📖 are from reading only, and values marked *invented* are
  illustrations, never facts.
- **Commit and push your changes.** The Reviewer only sees what's pushed to the PR.

## 3. Outcomes (optional)

If it helps the Reviewer, record what happened to an item from this checkout:

```sh
pr-review resolve F1 addressed "what changed" [--commit <sha>]
pr-review resolve F2 declined "why not"
pr-review resolve F4 needs-you "the question for the Reviewer"
```

They show on the Feedback in the Reviewer's tool. They're optional, a commit is optional within
one, and the Reviewer decides what's done either way.
