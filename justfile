# `generate` is for local development. `gates` is what the nightly scan and every
# land onto main run (just gates).

TACPLUS := env_var_or_default("TACPLUS_BIN", "tacplus")

default: generate

generate:
  #!/usr/bin/env bash
  set -euo pipefail
  {{TACPLUS}} build --tag latest

# What every change (ports, releases) must pass: regenerate with the floor's
# compiler (generated code lives only in tacplus/, so generation touching any
# file git sees is a failure), check no upstream reference crept back, then
# build (static, as the release image does), vet and test.
gates:
  #!/usr/bin/env bash
  set -euo pipefail
  tracked="$(git ls-files tacplus)"
  test -z "$tracked" || { echo "gates: generated files are tracked:" >&2; echo "$tracked" >&2; exit 1; }
  # tacplus-dev works without its upstream (#3): no reference to it outside the provenance,
  # the dated records and the planning files (the brackets keep this line from matching itself).
  upstream="$(git grep -n -I -i -E 'sea[b]eam|sona[r]|scott[y]' -- . ':!PROVENANCE.md' ':!decisions/' ':!ROADMAP.md' ':!HISTORY.md' ':!STATE.md' ':!PROPOSALS.md' || true)"
  test -z "$upstream" || { echo "gates: upstream references:" >&2; echo "$upstream" >&2; exit 1; }
  before="$(git status --porcelain)"
  {{TACPLUS}} build --tag latest
  if [[ "$(git status --porcelain)" != "$before" ]]; then
    echo "gates: generation changed files outside tacplus/:" >&2
    git status --porcelain >&2
    exit 1
  fi
  CGO_ENABLED=0 go build -trimpath -o bin/opa .
  go vet ./...
  go test ./...
  echo "gates: ok"
