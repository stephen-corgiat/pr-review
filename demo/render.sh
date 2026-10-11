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
# It needs a checkout of the holocron repo ($HOLOCRON_DIR, default
# ~/Projects/holocron) that has PR #66's commits, plus vhs, ttyd and ffmpeg.
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
export HOLOCRON_DIR=${HOLOCRON_DIR:-$HOME/Projects/holocron}
[ -d "$HOLOCRON_DIR/.git" ] || { echo "demo: $HOLOCRON_DIR isn't a git checkout (set HOLOCRON_DIR)" >&2; exit 1; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir "$work/bin"
(cd "$root" && go build -o "$work/bin/pr-review" .)

# A fresh Review Session and default settings every time; never your real ones.
export PATH="$work/bin:$PATH" DEMO_BIN="$work/bin"
export PR_REVIEW_DATA_DIR="$work/data" PR_REVIEW_CONFIG_DIR="$work/config"
export PR_REVIEW_REPLAY="$mode:$root/demo/recording"

if [ "$tape" = handoff ]; then
	# The Author's checkout: a throwaway clone of holocron on the PR's branch.
	# Its origin still names GitHub (pr-review matches the repo by remote), but
	# pushes go to a local bare repo: nothing reaches GitHub.
	branch=$(sed -n 's/.*"HeadRef": "\(.*\)".*/\1/p' "$root/demo/recording/forge.json")
	export DEMO_CHECKOUT="$work/holocron"
	git clone -q "$HOLOCRON_DIR" "$DEMO_CHECKOUT"
	git -C "$DEMO_CHECKOUT" fetch -q "$HOLOCRON_DIR" "refs/remotes/origin/$branch:refs/heads/$branch"
	git -C "$DEMO_CHECKOUT" checkout -q "$branch"
	git -C "$DEMO_CHECKOUT" remote set-url origin https://github.com/stephen-corgiat/holocron.git
	git init -q --bare "$work/pushed.git"
	git -C "$DEMO_CHECKOUT" remote set-url --push origin "$work/pushed.git"
	# The unit tests need the dependencies: the clone gets its own install.
	# Never symlink them from $HOLOCRON_DIR: an `npm ci` in the clone would
	# empty the real ones through the link.
	echo "demo: installing holocron's dependencies into the clone…" >&2
	(cd "$DEMO_CHECKOUT" && npm ci --no-audit --no-fund --loglevel=error >/dev/null)
	# Keep the session off your real dev stack. The clone's directory is also
	# named holocron, so docker compose would reach your containers, and the
	# app's defaults reach your dev Postgres on localhost:5432. Point both
	# nowhere (the clone has no .env, and dotenv never overrides these). The
	# tape also denies docker, the e2e suite and migrations outright.
	export COMPOSE_PROJECT_NAME=holocron-pr-review-demo POSTGRES_HOST=127.0.0.1 POSTGRES_PORT=1
	# /pull-review as a project skill, so the session needs no user settings.
	pr-review skill install --dir "$DEMO_CHECKOUT/.claude/skills" >/dev/null
	printf 'node_modules\n/.claude/skills/pull-review/\n' >>"$DEMO_CHECKOUT/.git/info/exclude"
fi

cd "$root"
mkdir -p .scratch/shots
vhs "demo/$tape.tape"
case $tape in
review) ls -l docs/media/demo.gif ;;
handoff) ls -l docs/media/handoff.gif ;;
screenshots) demo/crop.sh ;;
esac
