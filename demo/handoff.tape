# The hand-off demo: the Feedback from review.tape goes to the Author's Claude
# Code session, which pulls it in with /pull-review, fixes it, pushes, and
# records the Outcome; back in pr-review, the Feedback says what happened.
#
# Render it with `mise run demo-gif-handoff` (see demo/README.md). The pr-review
# side replays demo/recording; the Claude Code session is live. It runs in the
# made-up repo render.sh builds ($DEMO_REPO), on the PR's branch, and its
# pushes go to a local bare repo, so nothing reaches GitHub.
#
# Claude's turn runs between Hide and Show; its end is Claude Code's
# "· done <clock>" line.

Output docs/media/handoff.gif
Require pr-review
Require claude

Set Shell bash
Set FontFamily "JetBrainsMono Nerd Font, DejaVu Sans Mono"
Set FontSize 15
Set Width 1100
Set Height 700
Set Padding 8
Set TypingSpeed 45ms
Set WaitTimeout 120s

Hide
Type `export PATH="$DEMO_PATH:$PATH" PS1='$ ' && cd "$DEMO_REPO"` Enter
Type `claude() { command claude --model claude-opus-5-5 --setting-sources project --permission-mode auto "$@"; }` Enter
Type "clear" Enter

# Off camera: the Feedback from the first demo, refined and accepted.
Type "pr-review 42" Enter
Wait+Screen /\$0\.(0[1-9]|[1-9])/
Type "jjj" Enter
Sleep 1s
Type "/if c.Subtotal() < d.MinSubtotal" Enter
Sleep 500ms
Escape
Sleep 500ms
Type "vjj"
Sleep 300ms
Type "f"
Sleep 500ms
Type@5ms "Removing a book after this check keeps the discount, even below the minimum. Check it when pricing instead: <<example in Total>>"
Enter
Sleep 500ms
Type " "
Sleep 300ms
Type "r"
Wait+Screen /✦ Revision/
Sleep 500ms
Type "A"
Sleep 1s
Show
Sleep 2500ms

# Hand it to the Author's agent.
Type "P"
Sleep 1500ms
Type "l"
Sleep 600ms
Enter
Sleep 800ms
Tab
Sleep 600ms
Enter
Sleep 3000ms
Enter
Sleep 500ms
Type "q"
Hide
Type "clear" Enter
Show

# The Author's Claude Code session, on the PR's branch.
Type "claude"
Sleep 800ms
Enter
Hide
# The repo is a new folder, so Claude Code asks whether to trust it, and "No,
# exit" is the default: move to "Yes" explicitly (a number key doesn't pick
# it). Then wait for the session before typing into it.
Wait+Screen@60s /Yes, I trust this folder/
# The prompt draws before it reads keys: a Down sent at once is lost.
Sleep 1500ms
Down
Wait+Screen@10s /❯ Yes, I trust this folder/
Sleep 300ms
Enter
Wait+Screen@60s /Claude Code v\d/
Sleep 2s
Show
Sleep 1000ms
# One turn: the instruction rides along as the skill's arguments. Claude Code
# ends every turn with "✻ <verb> for <time> · done <clock>", and this session
# has only the one turn, so that line is the end, however the tool output
# above it is folded or scrolled.
Type "/pull-review"
Sleep 1000ms
Type " Address the feedback in the review."
Sleep 1000ms
Enter
Hide
Wait+Screen@1800s /· done \d{1,2}:\d{2}/
Sleep 2s
Show
Sleep 9000ms

# Back in pr-review: the Outcome is on the Feedback.
Hide
Type "/exit" Enter
Sleep 2s
Type "clear" Enter
Show
Type "pr-review 42"
Sleep 800ms
Enter
Sleep 2500ms
Type "2"
Sleep 800ms
Tab
Sleep 300ms
Tab
Sleep 400ms
Enter
Sleep 5000ms
Type "D"
Sleep 3000ms
