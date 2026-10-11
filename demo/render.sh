#!/usr/bin/env bash
# Render a README demo GIF from its tape.
#
#   demo/render.sh [review|handoff|screenshots] [record]
#
#   review    docs/media/demo.gif from demo/review.tape (the default)
#   screenshots  the README's screenshots from demo/screenshots.tape, cropped by
#             demo/crop.sh into docs/media/; it never writes demo.gif
#   handoff   docs/media/handoff.gif from demo/handoff.tape: hand the Feedback to
#             a live Claude Code session, which runs /pull-review and fixes it
#   record    run the live agent for whatever demo/recording lacks
#             (delete a file there to re-record it)
#
# The pr-review side replays demo/recording, so it's free and the same every
# time. The Claude Code session in the handoff demo is live: it costs a
# session's worth of tokens and plays out a little differently each render.
#
# The PR is made up: demo/bookstore.sh builds its repo, acme/bookstore, fresh
# each time. It needs vhs, ttyd and ffmpeg.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tape=review mode=replay
for a in "$@"; do
	case $a in
	review | handoff | screenshots) tape=$a ;;
	record) mode=record ;;
	*) echo "usage: demo/render.sh [review|handoff|screenshots] [record]" >&2 && exit 2 ;;
	esac
done
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
(cd "$root" && go build -o "$work/bin/pr-review" .)

# A fresh Review Session and default settings every time; never your real ones.
# The tapes put DEMO_PATH first again once their shell has started (a shell rc
# may reorder PATH): this pr-review, and the real Go toolchain rather than a
# version manager's shim, which may have no version set in the agent's
# worktree or the Author's checkout. Go's caller analysis needs it.
export DEMO_PATH="$work/bin:$(cd "$root" && go env GOROOT)/bin"
export PATH="$DEMO_PATH:$PATH"
export PR_REVIEW_DATA_DIR="$work/data" PR_REVIEW_CONFIG_DIR="$work/config"
export PR_REVIEW_REPLAY="$mode:$root/demo/recording"

# The PR's repo, built from demo/bookstore. Its origin names GitHub (pr-review
# matches the repo by its remote), but the commits are all here, so nothing is
# fetched.
export DEMO_REPO="$work/bookstore"
"$root/demo/bookstore.sh" "$DEMO_REPO"

if [ "$tape" = handoff ]; then
	# The Author's checkout is the same repo, on the PR's branch. Its pushes go
	# to a local bare repo: nothing reaches GitHub. The bare repo starts with both
	# branches, and the checkout has origin/* refs tracking them, as a clone
	# would: otherwise the push makes a new branch (which Claude remarks on) and
	# pr-review reports the pushed commit as "not pushed".
	git init -q --bare "$work/pushed.git"
	git -C "$DEMO_REPO" push -q "$work/pushed.git" main discount-codes
	git -C "$DEMO_REPO" remote set-url --push origin "$work/pushed.git"
	for b in main discount-codes; do git -C "$DEMO_REPO" update-ref "refs/remotes/origin/$b" "$b"; done
	git -C "$DEMO_REPO" checkout -q discount-codes
	git -C "$DEMO_REPO" branch -q -u origin/discount-codes
	# /pull-review as a project skill, so the session needs no user settings.
	pr-review skill install --dir "$DEMO_REPO/.claude/skills" >/dev/null
	printf '/.claude/skills/pull-review/\n' >>"$DEMO_REPO/.git/info/exclude"
fi

cd "$root"
mkdir -p .scratch/shots
vhs "demo/$tape.tape"
case $tape in
review) ls -l docs/media/demo.gif ;;
handoff) ls -l docs/media/handoff.gif ;;
screenshots) demo/crop.sh ;;
esac
