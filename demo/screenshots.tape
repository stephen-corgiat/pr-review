# The README screenshots. It follows demo/review.tape's keystrokes exactly (the replay
# answers are matched on the Question, the anchored code and the Feedback text), on a taller
# terminal so whole cards fit, and adds a Trace. demo/crop.sh cuts the raw shots in
# .scratch/shots/ down to the pictures in docs/media/.
#
# Render it with `mise run screenshots` (see demo/README.md). It never writes demo.gif.

Output .scratch/shots/screenshots.gif
Require pr-review

Set Shell bash
Set FontFamily "JetBrainsMono Nerd Font, DejaVu Sans Mono"
Set FontSize 15
Set Width 1600
Set Height 1800
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
Screenshot .scratch/shots/briefing.png

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
Screenshot .scratch/shots/sample-data.png
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
Screenshot .scratch/shots/selection.png
Type "d"
Sleep 1200ms
Hide
Wait+Screen /Define[^\n]*answered/
Show
Type "z"
Sleep 3500ms
Screenshot .scratch/shots/define.png

# Trace the same lines: what reaches them, and what they call. Collapse every card first, so
# only the Trace opens.
Type "z"
Sleep 500ms
Type@20ms "/if c.Subtotal() < d.MinSubtotal"
Enter
Sleep 600ms
Escape
Sleep 300ms
Type "vjj"
Sleep 1200ms
Type "t"
Sleep 1200ms
Hide
Wait+Screen /Trace[^\n]*answered/
Show
Sleep 800ms
Type "jj"
Sleep 300ms
Enter
Sleep 2500ms
Screenshot .scratch/shots/trace.png
Type "z"
Sleep 600ms

# Write Feedback on the same lines, asking for an example.
Type@20ms "/if c.Subtotal() < d.MinSubtotal"
Enter
Sleep 600ms
Escape
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
Enter
Sleep 1500ms
Screenshot .scratch/shots/feedback.png
Enter
Sleep 600ms

# Refine: the agent writes the example into the Feedback.
Type "r"
Sleep 1200ms
Hide
Sleep 10s
Show
Type "j"
Sleep 1500ms
Screenshot .scratch/shots/revision.png
Sleep 600ms
Type "A"
Sleep 1500ms

# Send it.
Type "P"
Sleep 1500ms
Screenshot .scratch/shots/send.png
Tab
Tab
Tab
Sleep 1000ms
Enter
Sleep 3000ms
