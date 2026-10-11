#!/usr/bin/env bash
# Cut the raw screenshots from demo/screenshots.tape (.scratch/shots/, 1600x1800) down to
# the pictures in docs/media/. The replay is deterministic, so fixed crop boxes work; if
# you change the tape or the terminal size, look at the raw shots and adjust the boxes.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
raw=$root/.scratch/shots out=$root/docs/media

# crop NAME W H X Y: a W x H box at (X, Y) of the raw shot NAME.png, saved as docs/media/NAME.png.
crop() {
	ffmpeg -v error -y -i "$raw/$1.png" -vf "crop=$2:$3:$4:$5" "$out/$1.png"
}

if [ -f "$root/demo/crop.boxes" ]; then
	while read -r name w h x y; do
		case $name in '' | '#'*) continue ;; esac
		crop "$name" "$w" "$h" "$x" "$y"
	done <"$root/demo/crop.boxes"
else
	echo "demo/crop.boxes isn't there yet: the raw shots are in $raw" >&2
fi
