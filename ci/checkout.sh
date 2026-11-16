#!/usr/bin/env bash
# The source a release builds (.github/workflows/ci.yml, release.yml), in the checkout of the commit that runs
# the workflow. Copies this commit's ci/ to $RUNNER_TEMP/ci, the release tools every later step runs, then, when
# VERSION is an existing tag of origin, checks that tag out: so a tag made before these tools existed (v0.1.0)
# is built from its own source, as tagged, by today's tools. A tag that predates tacplus.lock gets the pin
# ci/locks/VERSION.lock names, as an untracked tacplus.lock (the build image runs nothing without one); with no
# such pin it is refused. A VERSION not tagged yet leaves the checkout on this commit.
#
# Usage:  ci/checkout.sh VERSION
# Prints existing=true or existing=false (a GitHub step output). Tested by ci/release_test.sh.
set -euo pipefail

version="$1"
here="$(cd "$(dirname "$0")" && pwd)"
tools="${RUNNER_TEMP:?}/ci"
rm -rf "$tools"
cp -R "$here" "$tools"

if ! git ls-remote --exit-code --tags origin "refs/tags/$version" >/dev/null; then
  echo "existing=false"
  exit 0
fi
git fetch -q --force origin "+refs/tags/$version:refs/tags/$version"
git checkout -q --detach "refs/tags/$version^{commit}"
if [[ ! -f tacplus.lock ]]; then
  lock="$tools/locks/$version.lock"
  [[ -f "$lock" ]] || { echo "checkout.sh: $version has no tacplus.lock and ci/locks/$version.lock pins none" >&2; exit 1; }
  cp "$lock" tacplus.lock
fi
echo "existing=true"
