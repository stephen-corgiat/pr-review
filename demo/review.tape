# The README demo: review acme/bookstore #42 (made up; demo/bookstore.sh builds it), ask for Sample
# Data and a Definition, write Feedback with an <<example …>> placeholder, let the agent fill it in,
# send it.
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
Type `export PATH="$DEMO_PATH:$PATH" PS1='$ ' && cd "$DEMO_REPO" && clear` Enter
Show

# Open the PR; the Briefing arrives.
Type "pr-review 42"
Sleep 1500ms
Enter
Sleep 1200ms
Hide
Wait+Screen /\$0\.(0[1-9]|[1-9])/
Show
Sleep 4000ms

# Open the cart, where the code is applied and the Total priced.
Type "jjj"
Sleep 400ms
Enter
Sleep 1800ms

# Select the new Total and ask for Sample Data flowing through it.
Type@20ms "/subtotal := c.Subtotal"
Enter
Sleep 600ms
Escape
Sleep 300ms
Type@40ms "vjjjj"
Sleep 1200ms
Type "x"
Sleep 1200ms
Hide
Wait+Screen /● 1 new/
Show
Type "z"
Sleep 800ms
Ctrl+D
Sleep 4500ms
Type "z"
Sleep 800ms

# Select the minimum-subtotal check and ask what it means.
Type@20ms "/if c.Subtotal() < d.MinSubtotal"
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

# Write Feedback on the same lines, asking for an example.
Type "kk"
Sleep 300ms
Type "vjj"
Sleep 800ms
Type "f"
Sleep 600ms
Type@35ms "Removing a book after this check keeps the discount, even below the minimum. Check it when pricing instead: <<example in Total>>"
Sleep 800ms
Enter
Sleep 1200ms
Type " "
Sleep 1000ms

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
Type "A"
Sleep 1500ms

# Send it.
Type "P"
Sleep 1500ms
Tab
Tab
Tab
Sleep 1000ms
Enter
Sleep 3000ms
