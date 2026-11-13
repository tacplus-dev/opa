#!/usr/bin/env bash
# The release binaries (.github/workflows/release.yml), run in the pinned build image (ci/build-image.sh) on the
# checkout of the tag: generate with the pinned tacplus, then build opa static for each architecture, as
# `just gates` builds it, to bin/opa-linux-<arch>.
#
# Usage:  ci/release-build.sh ARCH...      e.g. ci/release-build.sh arm64 amd64
set -euo pipefail

tacplus build --tag latest
for arch in "$@"; do
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -trimpath -o "bin/opa-linux-$arch" .
  echo "bin/opa-linux-$arch"
done
