# PR Review Assistant

A private workspace where a human reviewer builds their understanding of a pull request: asking an agent questions about the change, and refining feedback before it reaches the author.

## Language

### People

**Reviewer**:
The human using the tool to understand a Pull Request. May also be its Author.

**Author**:
The person (or agent-assisted person) who opened the Pull Request.

**Implementing Agent**:
The agent session that writes the Pull Request's code, usually driven by the Author. When the Reviewer is the Author, it is who their Feedback is handed off to.
_Avoid_: coding agent, the agent (ambiguous with the agent answering Questions)

### The review

**Pull Request**:
The change under review: a GitHub pull request (draft, open, or already merged or closed for study), whose head the tool checks out into its own worktree.
_Avoid_: diff, changeset, branch (when meaning the whole change)

**PR Comment**:
A comment already on the Pull Request in GitHub, from a colleague or a bot. Read-only in the Review Session.
_Avoid_: feedback (reserved for the Reviewer's own drafts)

**Related Codebase**:
Another codebase on the Reviewer's machine that the agent may read for context, known by a short name (such as ASDF), for example a service the Pull Request's code calls or is called by.
_Avoid_: cross-repo, external repo, dependency

**Review Session**:
The Reviewer's private workspace for one Pull Request, pinned to a specific head commit, holding their Threads and Feedback.
_Avoid_: review (GitHub's term for a set of submitted comments), session

**Briefing**:
The up-front orientation to a Pull Request, limited to what is cheap to produce (partly computed by the tool, partly written by the agent): a true one-or-two-sentence TL;DR, a suggested reading order with a one-line summary of each file's change, what each Touched Method does and what the Pull Request changed in it, the Contract Changes, and a map of the calls between Touched Methods.
_Avoid_: overview, summary (when meaning the whole Briefing)

**Touched Method**:
A method or function the Pull Request adds, changes or removes.
_Avoid_: changed method, modified method

**Contract Change**:
A change to something other code or systems depend on: an API endpoint, the signature of an exported or public function or method, the database schema, or configuration and environment variables.
_Avoid_: breaking change (it may break nothing), interface change, significant change

**Stale**:
The state of something that may no longer hold. It applies to an Anchor whose lines were edited in a newer head commit, an Answer whose Anchor is Stale or whose Citations point at changed code, a Briefing from an older head commit, and a Revision whose Feedback was edited while Refine ran. A Stale Answer is only asked again when the Reviewer chooses.
_Avoid_: outdated, invalid

### Asking

**Anchor**:
What a Thread is attached to: a span of the PR description, either side of the diff, a whole file, surrounding code, the Briefing, an Answer, a PR Comment, a piece of Feedback, or the Pull Request as a whole.
_Avoid_: selection, highlight, annotation

**Question**:
A private request from the Reviewer to an agent for help understanding something in the Pull Request. Never seen by the Author.
_Avoid_: annotation, note, query, prompt

**Draft**:
A Question the Reviewer has written but not yet sent to the agent, or Feedback not yet marked ready to send. Drafted Questions are sent together when the Reviewer chooses.
_Avoid_: flag, held, pending (GitHub's word for an unsubmitted review)

**Question Kind**:
The flavour of a Question (Define, Summarize, Sample Data, Before/After, Trace, Test Cases, Research), which shapes how it is asked and how its Answer is presented. Sample Data shows example values flowing through the code at the head commit; Before/After shows how the data differs from the base.

**Answer**:
The agent's response to a Question: a gist of at most two sentences, a detail that stays collapsed until asked for, and the fields its Question Kind calls for, carrying Citations and marked Unverified or Verified.

**Citation**:
A pointer from an Answer to the code, schema, commit, ticket, document or text its claim rests on.

**Unverified**:
Said of an Answer based only on reading (code, schema, history, tickets or documents), with no Probe to confirm it.
_Avoid_: Inferred

**Verified**:
Said of an Answer confirmed by a Probe.

**Probe**:
Anything the agent runs beyond reading while answering, such as a test, console command, script or query, usually to confirm an Answer. It's listed with the Answer and is what makes an Answer Verified. In v1 the agent runs Probes in auto mode without asking, in the Review Session's own worktree, and may never edit the repo.
_Avoid_: execution, check

**Thread**:
The Questions and Answers exchanged about one Anchor, starting with the first Question and continuing through follow-ups ("I still don't get it, explain it like I'm five").
_Avoid_: conversation, chat

### Telling

**Feedback**:
Something the Reviewer intends to tell the Author about the Pull Request. It stays private until the Reviewer marks it ready and explicitly sends it: posted to the Pull Request as a review, or handed off to the Implementing Agent.
_Avoid_: comment (ambiguous with PR Comments), annotation, review

**Revision**:
The agent's proposed rewording or correction of a piece of Feedback, which the Reviewer accepts, edits or ignores. It fills the Feedback's Agent Notes and adds specifics where the wording is vague. When the Reviewer's intent is unclear, it comes back as questions instead.
_Avoid_: suggestion (GitHub's term for code a reviewer proposes to the author)

**Agent Note**:
An instruction to the agent written inside Feedback between `<<` and `>>` (outside code), such as `<<the cache key formula>>`. A Revision fills it in; it never reaches the Author as written.
_Avoid_: placeholder, TODO

**Hand-off**:
Sending ready Feedback, with the research behind it, to the Implementing Agent so it can act on it. The Implementing Agent pulls it in; the tool never drives it.
_Avoid_: own-PR mode, export (the command, not the act)

**Outcome**:
What was done about one piece of handed-off Feedback, optionally reported by whoever acts on it: addressed (optionally in a named commit), declined (with a reason), or needs the Reviewer (a question). The Reviewer can mark Feedback done, or reopen it, whatever the Outcome says.
_Avoid_: status, resolution
