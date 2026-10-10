# The hand-off demo: the Feedback from review.tape goes to the Author's Claude
# Code session, which pulls it in with /pull-review, fixes it, pushes, and
# records the Outcome; back in pr-review, the Feedback says what happened.
#
# Render it with `mise run demo-gif-handoff` (see demo/README.md). The pr-review
# side replays demo/recording; the Claude Code session is live. It runs in a
# throwaway clone of holocron ($DEMO_CHECKOUT) whose pushes go to a local bare
# repo, so nothing reaches GitHub.
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
Type `cd "$DEMO_CHECKOUT" && export PATH="$DEMO_BIN:$PATH" PS1='$ '` Enter
# Deny anything that reaches past the clone: render.sh also points docker
# compose and Postgres nowhere, so these fail even if a rule misses.
Type `claude() { command claude --model claude-opus-5-5 --setting-sources project --permission-mode auto --disallowedTools "Bash(docker *)" "Bash(npm run e2e*)" "Bash(npm --workspace apps/e2e *)" "Bash(npx playwright *)" "Bash(npm run migration*)" "Bash(npm --workspace apps/api run migration*)" "$@"; }` Enter
Type "clear" Enter

# Off camera: the Feedback from the first demo, refined and accepted.
Type "pr-review 66" Enter
Wait+Screen /\$0\.(0[1-9]|[1-9])/
Type "jj" Enter
Sleep 1s
Type "/prior.lastQualifyingDay ===" Enter
Sleep 500ms
Escape
Sleep 500ms
Type "vjj"
Sleep 300ms
Type "f"
Sleep 500ms
Type@5ms "Prefer a db guard here rather than comparing in code: <<example using the transactions table and a unique index>>"
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
# The clone is a new folder, so Claude Code asks whether to trust it. holocron's
# .claude/settings.json pre-approves tools, which makes "No, exit" the default:
# move to "Yes" explicitly. Then wait for the session before typing into it.
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
Type " Fix F1, commit and push, then record the outcome."
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
Type "pr-review 66"
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
