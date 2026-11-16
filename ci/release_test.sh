#!/usr/bin/env bash
# The release rules (ci/release-tags.sh, ci/release-latest.sh, ci/release-notes.sh, ci/checkout.sh), run against
# fixture repositories: which image tags a version gets, so a fleet's MAJOR.MINOR (tacplus-dev/tacplus#180) only
# ever moves forward on its own line, whether its GitHub release is Latest, which commits its notes list, and
# which source an existing tag is built from.
# Run by `just gates`; needs bash and git.
#
# Usage:  ci/release_test.sh
set -euo pipefail

ci="$(cd "$(dirname "$0")" && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
export GIT_CONFIG_GLOBAL=/dev/null GIT_CONFIG_NOSYSTEM=1
export GIT_AUTHOR_NAME=t GIT_AUTHOR_EMAIL=t@t GIT_COMMITTER_NAME=t GIT_COMMITTER_EMAIL=t@t

failed=0
check() { # name got want
  if [[ "$2" == "$3" ]]; then
    echo "ok   $1"
  else
    echo "FAIL $1: got '$2', want '$3'"
    failed=1
  fi
}

# A repository with one commit per tag, tagged in push order; each commit's subject is "change for <tag>".
fixture() { # name tag...
  local dir="$tmp/$1"
  shift
  git init -q -b main "$dir"
  git -C "$dir" commit -q --allow-empty -m first
  for tag in "$@"; do
    git -C "$dir" commit -q --allow-empty -m "change for $tag"
    git -C "$dir" tag "$tag"
  done
  echo "$dir"
}

tags() { # name version tag...
  local name="$1" version="$2"
  shift 2
  (cd "$(fixture "$name" "$@")" && "$ci/release-tags.sh" "$version")
}

check "the first release takes its line" "$(tags first v0.1.0 v0.1.0)" "0.1.0 0.1"
# The issue's acceptance: v0.1.1 moves 0.1, and not an older line.
check "a patch moves its own line and no other" "$(tags patch v0.1.1 v0.0.3 v0.1.0 v0.1.1)" "0.1.1 0.1"
check "a patch on an older line moves only that line" "$(tags older-line v0.1.1 v0.1.0 v0.2.0 v0.1.1)" "0.1.1 0.1"
# Publishing an existing tag below the line's highest (v0.1.0 after v0.1.2 was tagged), or a re-run.
check "an older patch of the line never moves it back" "$(tags backwards v0.1.0 v0.1.0 v0.1.1 v0.1.2)" "0.1.0"
check "versions sort as numbers, not text" "$(tags numeric v0.1.10 v0.1.9 v0.1.10)" "0.1.10 0.1"
check "a pre-release gets only its own tag" "$(tags pre v0.1.2-rc.1 v0.1.1 v0.1.2-rc.1)" "0.1.2-rc.1"
check "a pre-release does not hold back the line" "$(tags pre-behind v0.1.1 v0.1.2-rc.1 v0.1.1)" "0.1.1 0.1"

# ci/release-latest.sh: the GitHub release marked Latest is the highest release of all, never a patch on an
# older line, an older tag published late (v0.1.0 after v0.2.0) or a pre-release.
latest() { # name version tag...
  local name="$1" version="$2"
  shift 2
  (cd "$(fixture "$name" "$@")" && "$ci/release-latest.sh" "$version")
}
check "the first release is Latest" "$(latest latest-first v0.1.0 v0.1.0)" "--latest"
check "the highest release is Latest" "$(latest latest-highest v0.2.1 v0.1.0 v0.2.0 v0.2.1)" "--latest"
check "a patch on an older line is not Latest" "$(latest latest-older v0.1.1 v0.1.0 v0.2.0 v0.1.1)" "--latest=false"
check "an older tag published late is not Latest" "$(latest latest-late v0.1.0 v0.1.0 v0.1.1 v0.2.0)" "--latest=false"
check "Latest sorts versions as numbers" "$(latest latest-numeric v0.10.0 v0.9.0 v0.10.0)" "--latest"
check "a pre-release is a pre-release, not Latest" "$(latest latest-pre v0.2.0-rc.1 v0.1.0 v0.2.0-rc.1)" "--prerelease"
check "a pre-release does not take Latest from the highest release" "$(latest latest-pre-behind v0.1.0 v0.1.0 v0.2.0-rc.1)" "--latest"

dir="$(fixture refused v0.1)"
check "release-tags.sh refuses a version that is not MAJOR.MINOR.PATCH" \
  "$(cd "$dir" && "$ci/release-tags.sh" v0.1 >/dev/null 2>&1 && echo ran || echo refused)" refused
check "release-latest.sh refuses a version that is not MAJOR.MINOR.PATCH" \
  "$(cd "$dir" && "$ci/release-latest.sh" v0.1 >/dev/null 2>&1 && echo ran || echo refused)" refused
check "release-latest.sh refuses a version without its v" \
  "$(cd "$dir" && "$ci/release-latest.sh" 0.1.0 >/dev/null 2>&1 && echo ran || echo refused)" refused
check "release-notes.sh refuses a version that is not MAJOR.MINOR.PATCH" \
  "$(cd "$dir" && "$ci/release-notes.sh" v0.1 >/dev/null 2>&1 && echo ran || echo refused)" refused

notes() { # dir version
  (cd "$1" && "$ci/release-notes.sh" "$2" | sed -E 's/ \([0-9a-f]+\)$//')
}
dir="$(fixture notes v0.1.0 v0.2.0 v0.1.1 v0.2.1)"
check "the first release lists every commit" "$(notes "$dir" v0.1.0)" $'Changes:\n\n- change for v0.1.0\n- first'
check "a release lists the commits since the previous one" "$(notes "$dir" v0.2.1)" \
  $'Changes since v0.2.0:\n\n- change for v0.2.1\n- change for v0.1.1'
check "a patch on an older line lists from that line's previous release" "$(notes "$dir" v0.1.1)" \
  $'Changes since v0.1.0:\n\n- change for v0.1.1\n- change for v0.2.0'

# ci/checkout.sh: an existing tag is built from its own source with this commit's ci/, a tag that predates
# tacplus.lock takes its pin from ci/locks/, and a version not tagged yet stays on this commit.
remote="$tmp/remote.git"
git init -q --bare -b main "$remote"
work="$tmp/work"
git init -q -b main "$work"
git -C "$work" remote add origin "$remote"
git -C "$work" commit -q --allow-empty -m "before the lock"
git -C "$work" tag -a -m old v0.1.0
echo "build_image: build@sha256:new" > "$work/tacplus.lock"
git -C "$work" add tacplus.lock
git -C "$work" commit -q -m "the lock"
git -C "$work" tag -a -m pinned v0.1.1
mkdir -p "$work/ci/locks"
cp "$ci/checkout.sh" "$work/ci/"
echo "build_image: build@sha256:old" > "$work/ci/locks/v0.1.0.lock"
git -C "$work" add ci
git -C "$work" commit -q -m "release tools"
git -C "$work" push -q origin main --tags

runner="$tmp/runner"
checkout() { # version: what ci/checkout.sh prints in a fresh clone (shallow, without tags, as actions/checkout makes it)
  rm -rf "$tmp/clone" "$runner"
  mkdir -p "$runner"
  git clone -q --depth 1 --no-tags "file://$remote" "$tmp/clone"
  (cd "$tmp/clone" && RUNNER_TEMP="$runner" ci/checkout.sh "$1")
}

check "an existing tag is reported" "$(checkout v0.1.1)" "existing=true"
check "an existing tag is checked out" "$(git -C "$tmp/clone" rev-parse HEAD)" "$(git -C "$work" rev-parse 'v0.1.1^{commit}')"
check "a tag with its own lock keeps it" "$(cat "$tmp/clone/tacplus.lock")" "build_image: build@sha256:new"
check "this commit's ci/ is kept beside the tag" "$(cat "$runner/ci/locks/v0.1.0.lock")" "build_image: build@sha256:old"

check "an existing tag before the lock is reported" "$(checkout v0.1.0)" "existing=true"
check "a tag before the lock is checked out" "$(git -C "$tmp/clone" rev-parse HEAD)" "$(git -C "$work" rev-parse 'v0.1.0^{commit}')"
check "a tag before the lock takes its pin from ci/locks" "$(cat "$tmp/clone/tacplus.lock")" "build_image: build@sha256:old"
check "the pin is not committed" "$(git -C "$tmp/clone" status --porcelain)" "?? tacplus.lock"

rm "$work/ci/locks/v0.1.0.lock"
git -C "$work" commit -q -am "no pin for v0.1.0"
git -C "$work" push -q origin main
check "a tag before the lock with no pin is refused" "$(checkout v0.1.0 2>/dev/null && echo ran || echo refused)" refused

check "a version not tagged yet is reported" "$(checkout v0.1.2)" "existing=false"
check "a version not tagged yet stays on this commit" "$(git -C "$tmp/clone" rev-parse HEAD)" "$(git -C "$work" rev-parse HEAD)"
check "a version not tagged yet is not tagged" "$(git -C "$tmp/clone" tag -l v0.1.2)" ""

exit "$failed"
