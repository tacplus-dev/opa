# State: opa

Overwritten to stay current (loopers and agents). Last updated: 2026-11-12 by looper (#13).

- **Version / line:** v0.1.3 (0.1 line, source tags on green main); tacplus floor 2.6, pinned to 2.6.1 in `tacplus.lock` (#8)
- **Works:** generation with tacplus 2.6 (`depends: [kernel]`, `grpc-servers: all`), build, vet and tests: `just gates` passes in the looper (its `tacplus` wrapper runs the pinned 2.6.1 in `build:2.6.1`, tacplus-dev/looper#27).
- **Released / deployed:** v0.1.0 (2026-11-07), v0.1.1 (#8), v0.1.2 (#3), v0.1.3 (#13: a policy's numeric `token_use` is honoured, an unrecognised one fails `EvalProxyPolicy`): source tags; binaries, images and the GitHub release come from the build pipeline (tacplus-dev/looper#17).
- **Independent of SeaBeam (#3):** module `github.com/tacplus-dev/opa`, generated code in `tacplus/` (gitignored); the service's proto is this repository's own (`src: proto`), kernel's comes through `depends` from github.com/tacplus-dev/registry; nothing is fetched from gitlab.com, and `just gates` fails on any SeaBeam reference outside PROVENANCE.md, `decisions/` and the planning files. SeaBeam's `.gitlab-ci.yml` is gone (#2 owns release CI).
- **Dependencies:** `go 1.26.0`; grpc v1.84.0, x/net v0.62.0, x/crypto v0.58.0, x/text v0.43.0, x/sys v0.49.0 (#4). `govulncheck ./...` (v1.9.0) finds no reachable vulnerabilities; module mode leaves only GO-2026-5932 (x/crypto/openpgp, no fix, not imported).
- **Known gaps:** none open here; release CI and the image are #2.
- **Blocked on:** nothing.
