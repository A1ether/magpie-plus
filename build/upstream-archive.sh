#!/usr/bin/env bash
# Keeps a copy of yetone/magpie's source in this repository, so magpie plus
# survives upstream deleting its repository.
#
# Every upstream branch and tag is mirrored under vendor/upstream/:
#
#   refs/heads/vendor/upstream/<branch>   every branch, upstream/main among them
#   refs/tags/vendor/upstream/<tag>       every tag, one per upstream release
#
# They are namespaced so they cannot start a release, test or Docker run: a
# tag named v* hands off to Release (.github/workflows/release.yml), and a
# push to main runs Test. A plain `git clone` fetches every branch, and the
# tags that point into them, so a clone of this repository alone holds the
# upstream source even if github.com/yetone/magpie is gone.
#
# The archive only moves forward. A branch upstream force-pushed, a tag it
# moved, and a ref it deleted are refused or left in place, never followed:
# following them would drop the work the archive exists to keep.
#
# Usage:
#   build/upstream-archive.sh           fetch upstream, push what is new
#   build/upstream-archive.sh --check   fetch upstream, report what is missing
#
# Env:
#   UPSTREAM_URL     default https://github.com/yetone/magpie.git
#   UPSTREAM_REMOTE  default upstream
#   ARCHIVE_REMOTE   default origin
set -euo pipefail

mode=push
case "${1:-}" in
  "") ;;
  --check) mode=check ;;
  --help | -h)
    sed -n '2,27p' "$0" | sed 's/^# \{0,1\}//'
    exit 0
    ;;
  *)
    echo "unknown argument: $1 (try --help)" >&2
    exit 2
    ;;
esac

upstream_url=${UPSTREAM_URL:-https://github.com/yetone/magpie.git}
upstream_remote=${UPSTREAM_REMOTE:-upstream}
archive_remote=${ARCHIVE_REMOTE:-origin}

die() {
  printf '%s\n' "$*" >&2
  exit 1
}

cd "$(git rev-parse --show-toplevel)"

# The archive pushes whole histories, which a shallow repository cannot:
# git refuses with "shallow update not allowed". CI checks out shallow
# unless told otherwise, and this repository once was.
if [ "$(git rev-parse --is-shallow-repository)" = true ]; then
  echo "this repository is shallow: fetching its full history first"
  git fetch --unshallow "$archive_remote" ||
    die "could not unshallow from $archive_remote"
fi

if ! git remote get-url "$upstream_remote" >/dev/null 2>&1; then
  git remote add "$upstream_remote" "$upstream_url"
fi

# Heads and tags are fetched into the namespace they are archived from, so
# the working clone's own branches and tags are not touched. --prune drops
# a ref upstream deleted from *here*; it is never dropped from the archive.
git fetch --prune --no-tags "$upstream_remote" \
  '+refs/heads/*:refs/vendor/upstream/heads/*' \
  '+refs/tags/*:refs/vendor/upstream/tags/*' ||
  die "could not fetch $upstream_remote ($(git remote get-url "$upstream_remote"))"

heads=$(git for-each-ref --format='%(refname)' refs/vendor/upstream/heads)
[ -n "$heads" ] || die "$upstream_remote gave no branches"

push_failed=0
if [ "$mode" = push ]; then
  # No --force and no --prune: a ref that would move backwards, or a tag
  # that moved upstream, is refused, and every other ref still goes.
  # GitHub hung up on the first archive's 67MB of objects and 1723 refs over
  # HTTP/2 ("Error in the HTTP2 framing layer", then "unable to rewind rpc
  # post data"); HTTP/1.1 with room for the whole request went through.
  git -c http.version=HTTP/1.1 -c http.postBuffer=268435456 push "$archive_remote" \
    'refs/vendor/upstream/heads/*:refs/heads/vendor/upstream/*' \
    'refs/vendor/upstream/tags/*:refs/tags/vendor/upstream/*' ||
    push_failed=1
fi

# What matters is what a clone of the archive would get, so it is checked
# from the remote, not from what push said.
want=$(mktemp "${TMPDIR:-/tmp}/upstream-archive-want.XXXXXX")
have=$(mktemp "${TMPDIR:-/tmp}/upstream-archive-have.XXXXXX")
trap 'rm -f "$want" "$have"' EXIT

git for-each-ref --format='%(objectname) %(refname)' \
  refs/vendor/upstream/heads refs/vendor/upstream/tags |
  sed -e 's#^\([0-9a-f]*\) refs/vendor/upstream/heads/#\1 refs/heads/vendor/upstream/#' \
    -e 's#^\([0-9a-f]*\) refs/vendor/upstream/tags/#\1 refs/tags/vendor/upstream/#' \
    >"$want"

git ls-remote "$archive_remote" \
  'refs/heads/vendor/upstream/*' 'refs/tags/vendor/upstream/*' |
  awk '$2 !~ /\^\{\}$/' >"$have"

missing=$(awk '
  FILENAME == ARGV[1] { have[$2] = $1; next }
  !($2 in have) {
    if (++gone <= 20) print "  missing: " $2
    bad = 1; next
  }
  have[$2] != $1 {
    if (++moved <= 20) print "  differs: " $2 " (upstream " $1 ", archive " have[$2] ")"
    bad = 1
  }
  END {
    if (gone > 20) print "  ... and " (gone - 20) " more missing"
    if (moved > 20) print "  ... and " (moved - 20) " more differing"
    exit bad
  }
' "$have" "$want") || true

n_heads=$(printf '%s\n' "$heads" | grep -c . || true)
n_tags=$(git for-each-ref --format='x' refs/vendor/upstream/tags | grep -c . || true)

if [ -n "$missing" ]; then
  printf '%s\n' "$missing" >&2
  printf 'not archived at %s: %s branch(es) and %s tag(s) fetched, and the refs above are missing or differ\n' \
    "$archive_remote" "$n_heads" "$n_tags" >&2
  exit 1
fi

printf '%s: %s branch(es) and %s tag(s) of %s\n' \
  "$archive_remote" "$n_heads" "$n_tags" "$(git remote get-url "$upstream_remote")"

if [ "$push_failed" -ne 0 ]; then
  echo "some refs were refused (see above); the archive keeps what it had" >&2
  exit 1
fi
