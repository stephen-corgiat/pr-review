# The README demo: review holocron #66, ask for Sample Data and a Definition, write Feedback with an
# <<example …>> placeholder, let the agent fill it in, send it.
#
# Render it with `mise run demo-gif` (see demo/README.md). It replays the
# recorded agent in demo/recording/, so it's free and the same every time.
#
# Waits use +Screen: a bare `Wait` only looks at the cursor's line, and the TUI
# draws everywhere. Anything between Hide and Show isn't in the GIF, which is
# how the agent's thinking time (live recording) and the replay delay vanish.

Output docs/media/demo.gif
Require pr-review

Set Shell bash
Set FontFamily "JetBrainsMono Nerd Font, DejaVu Sans Mono"
Set FontSize 15
Set Width 1100
Set Height 700
Set Padding 8
Set TypingSpeed 45ms
Set WaitTimeout 400s

Hide
Type "cd $HOLOCRON_DIR && PS1='$ ' && clear" Enter
Show

# Open the PR; the Briefing arrives.
Type "pr-review 66"
Sleep 1500ms
Enter
Sleep 1200ms
Hide
Wait+Screen /\$0\.(0[1-9]|[1-9])/
Show
Sleep 4000ms
Screenshot docs/media/01-briefing.png

# Open the file with the fix.
Type "jj"
Sleep 400ms
Enter
Sleep 1800ms

# Select the streak advance and ask for Sample Data flowing through it.
Type@20ms "/const nextCount"
Enter
Sleep 600ms
Escape
Sleep 300ms
Type@40ms "vjjjjjjjjjjjj"
Sleep 1200ms
Type "x"
Sleep 1200ms
Hide
Wait+Screen /Sample Data[^\n]*answered/
Show
Type "z"
Sleep 800ms
Ctrl+D
Sleep 4500ms
Screenshot docs/media/02-sample-data.png
Type "z"
Sleep 800ms

# Select the guard and ask what it means.
Type@20ms "/prior.lastQualifyingDay ==="
Enter
Sleep 600ms
Escape
Sleep 300ms
Type "vjj"
Sleep 1200ms
Type "d"
Sleep 1200ms
Hide
Wait+Screen /Define[^\n]*answered/
Show
Type "z"
Sleep 3500ms
Screenshot docs/media/03-define.png

# Write Feedback on the same lines, asking for an example.
Type "kk"
Sleep 300ms
Type "vjj"
Sleep 800ms
Type "f"
Sleep 600ms
Type@35ms "Prefer a db guard here rather than comparing in code: <<example using the transactions table and a unique index>>"
Sleep 800ms
Enter
Sleep 1200ms
Type " "
Sleep 1000ms
Screenshot docs/media/04-feedback.png

# Refine: the agent writes the example into the Feedback.
Type "r"
Sleep 1200ms
Hide
Wait+Screen /✦ Revision/
Show
Sleep 1500ms
Ctrl+D
Sleep 2000ms
Ctrl+D
Sleep 2000ms
Screenshot docs/media/05-revision.png
Type "A"
Sleep 1500ms

# Send it.
Type "P"
Sleep 1500ms
Screenshot docs/media/06-send.png
Tab
Tab
Tab
Sleep 1000ms
Enter
Sleep 3000ms
