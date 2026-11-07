# State: opa

Overwritten to stay current (loopers and agents). Last updated: 2026-11-07 by looper (#3).

- **Version / line:** unreleased (no vX.Y.Z tag yet); tacplus floor 2.5 (#7)
- **Works:** generation with tacplus 2.5 (`depends: [kernel]`, `grpc-servers: all`), build, vet and tests: `just gates` passes in the looper (its `tacplus` wrapper runs the 2.5 line's newest patch, 2.5.3, in `build:2.5`, tacplus-dev/looper#27).
- **Released / deployed:** nothing yet (#2).
- **Independent of SeaBeam (#3):** module `github.com/tacplus-dev/opa`, generated code in `tacplus/` (gitignored); the service's proto is this repository's own (`src: proto`), kernel's comes through `depends` from github.com/tacplus-dev/registry; nothing is fetched from gitlab.com, and `just gates` fails on any SeaBeam reference outside PROVENANCE.md, `decisions/` and the planning files. No release CI yet (#2): SeaBeam's `.gitlab-ci.yml` is gone.
- **Dependencies:** `go 1.26.0`; grpc v1.84.0, x/net v0.62.0, x/crypto v0.58.0, x/text v0.43.0, x/sys v0.49.0 (#4). `govulncheck ./...` (v1.9.0) finds no reachable vulnerabilities; module mode leaves only GO-2026-5932 (x/crypto/openpgp, no fix, not imported).
- **Known gaps:** none open here; release CI and the image are #2.
- **Blocked on:** #8: kernel's `latest` now declares tacplus 2.6, so `depends: [kernel]` fails on the 2.5 floor (`this project needs tacplus 2.6 or a later 2.x; running 2.5.12`) and `just gates` fails on main as on every branch until the floor moves. With `TACPLUS_MINOR=2.6` the gates pass (2026-11-07, #3).
