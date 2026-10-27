# Local development builds only — not used in CI/CD.

TACPLUS := env_var_or_default("TACPLUS_BIN", "tacplus")

default: generate

generate:
  #!/usr/bin/env bash
  set -euo pipefail
  {{TACPLUS}} build --tag latest

# What every change (ports, releases) must pass: regenerate with the floor's
# compiler (generated code lives only in model/, so generation touching any
# file git sees is a failure), then build, vet and test.
gates:
  #!/usr/bin/env bash
  set -euo pipefail
  tracked="$(git ls-files model)"
  test -z "$tracked" || { echo "gates: generated files are tracked:" >&2; echo "$tracked" >&2; exit 1; }
  before="$(git status --porcelain)"
  {{TACPLUS}} build --tag latest
  if [[ "$(git status --porcelain)" != "$before" ]]; then
    echo "gates: generation changed files outside model/:" >&2
    git status --porcelain >&2
    exit 1
  fi
  go build -o bin/opa .
  go vet ./...
  go test ./...
  echo "gates: ok"
