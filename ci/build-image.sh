#!/usr/bin/env bash
# Runs a command in the build image tacplus.lock pins, by digest, on the checkout in the current directory
# (.github/workflows): its tacplus runs exactly the pinned release, so the same commit builds the same way
# (tacplus-dev/roadmap decisions/deterministic-builds-2026-10-27.md). A newer patch arrives only as a commit that
# moves the lock. The checkout and these scripts are mounted at their own paths. HOME is the container's /tmp, so
# the caches (tacplus, Go) stay outside the checkout and the gates' check that generation changed nothing under
# version control sees only the checkout. It needs no credential: GH_TOKEN, when set, is passed through for
# tacplus's reads of github.com (`depends`, from tacplus-dev/registry), which are public, so give it a read-only one.
#
# Usage:  ci/build-image.sh CMD [ARG...]     e.g. ci/build-image.sh just gates
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
lock="$PWD/tacplus.lock"
[[ -f "$lock" ]] || { echo "build-image.sh: no tacplus.lock: run \`tacplus pin\` in the build image of the floor tacplus.yaml declares and commit it" >&2; exit 1; }
image="$(sed -n -E 's/^build_image:[[:space:]]*"?([^"[:space:]]+)"?.*$/\1/p' "$lock" | head -1)"
[[ "$image" == *@sha256:* ]] || { echo "build-image.sh: tacplus.lock names no build_image by digest" >&2; exit 1; }

mounts=(-v "$PWD:$PWD")
[[ "$here" == "$PWD"/* ]] || mounts+=(-v "$here:$here:ro")
env=(-e HOME=/tmp)
[[ -z "${GH_TOKEN:-}" ]] || env+=(-e GH_TOKEN)
echo "build-image.sh: $* in $image"
docker run --rm -u "$(id -u):$(id -g)" "${env[@]}" "${mounts[@]}" -w "$PWD" "$image" "$@"
