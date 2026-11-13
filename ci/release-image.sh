#!/usr/bin/env bash
# Assembles the release image (.github/workflows/release.yml) from the Dockerfile, once per architecture, and
# publishes them as one multi-architecture image.
#
# Usage:  ci/release-image.sh VERSION REVISION [TAG...]
#
# Run in the checkout of the tag. The Dockerfile takes its binaries from beside it, as the fleet's image
# pipeline puts them there: for each architecture in ARCHS (default "arm64") this stages a build context with
# bin/opa-linux-<arch> (ci/release-build.sh) as bin/opa and bin/launcher/launcher-linux-<arch>, and builds it
# labelled with VERSION, REVISION, its commit time and this repository. With TAGs, each architecture is pushed
# by digest and $IMAGE:<TAG> (each TAG) is pointed at all of them. Without, nothing is pushed: the dry run.
set -euo pipefail

version="$1"
revision="$2"
shift 2
image="${IMAGE:-ghcr.io/tacplus-dev/opa}"
created="$(git show -s --format=%cI "$revision")"

digests=()
for arch in ${ARCHS:-arm64}; do
  case "$arch" in
    amd64 | arm64) ;;
    *) echo "release-image.sh: unsupported architecture $arch" >&2; exit 1 ;;
  esac
  ctx="$PWD/bin/image/$arch"
  rm -rf "$ctx"
  mkdir -p "$ctx/bin"
  cp Dockerfile "$ctx/"
  cp "bin/opa-linux-$arch" "$ctx/bin/opa"
  cp "bin/launcher/launcher-linux-$arch" "$ctx/"

  output="type=cacheonly"
  (($#)) && output="type=image,name=$image,push-by-digest=true,name-canonical=true,push=true"
  # The labels are set here too: a tag made before the Dockerfile had them (v0.1.0) still says what it is.
  docker buildx build --platform "linux/$arch" \
    --build-arg "VERSION=$version" --build-arg "VCS_REF=$revision" --build-arg "BUILD_DATE=$created" \
    --label "org.opencontainers.image.source=https://github.com/tacplus-dev/opa" \
    --label "org.opencontainers.image.version=$version" \
    --label "org.opencontainers.image.revision=$revision" \
    --label "org.opencontainers.image.created=$created" \
    --output "$output" --metadata-file "$ctx.json" "$ctx"
  if (($#)); then
    digest="$(jq -r '."containerimage.digest"' "$ctx.json")"
    echo "linux/$arch: $image@$digest"
    digests+=("$image@$digest")
  fi
done

(($#)) || exit 0
refs=()
for tag in "$@"; do refs+=(-t "$image:$tag"); done
docker buildx imagetools create "${refs[@]}" "${digests[@]}"
