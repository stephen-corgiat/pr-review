#!/usr/bin/env bash
# Build the demo's made-up repo, acme/bookstore, into DIR:
#
#   demo/bookstore.sh DIR
#
# main is demo/bookstore/base; the discount-codes branch adds one commit,
# demo/bookstore/pr: that's PR #42 in demo/recording/forge.json. Authors, dates
# and config are fixed, so the SHAs are the same everywhere and match forge.json.
# origin names GitHub, because pr-review matches the repo by its remote, but the
# repo doesn't exist there: nothing is ever fetched, since the commits are here.
set -euo pipefail
src=$(cd "$(dirname "$0")/bookstore" && pwd)
dir=${1:?usage: demo/bookstore.sh DIR}
[ ! -e "$dir" ] || { echo "demo: $dir already exists" >&2; exit 1; }

export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME="Jordan Lee" GIT_AUTHOR_EMAIL=jordan@acme.example
export GIT_COMMITTER_NAME="Jordan Lee" GIT_COMMITTER_EMAIL=jordan@acme.example

# commit TREE DATE MESSAGE: make the work tree TREE's files and commit them.
commit() {
	git -C "$dir" rm -rq --ignore-unmatch .
	cp -r "$src/$1/." "$dir/"
	git -C "$dir" add -A
	GIT_AUTHOR_DATE=$2 GIT_COMMITTER_DATE=$2 git -C "$dir" commit -q -m "$3"
}

git init -q -b main "$dir"
commit base 2026-09-14T10:00:00Z "Storefront API: catalog, cart and shipping"
git -C "$dir" checkout -q -b discount-codes
commit pr 2026-10-06T15:30:00Z "Add discount codes at checkout"
git -C "$dir" checkout -q main
git -C "$dir" remote add origin https://github.com/acme/bookstore.git
