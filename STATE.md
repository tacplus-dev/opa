# State: opa

Overwritten to stay current (loopers and agents). Last updated: 2026-11-07 by looper (#8).

- **Version / line:** v0.1.1 (0.1 line, source tags on green main); tacplus floor 2.6, pinned to 2.6.1 in `tacplus.lock` (#8)
- **Works:** generation with tacplus 2.6 (`depends: [kernel]`, `grpc-servers: all`), build, vet and tests: `just gates` passes in the looper (its `tacplus` wrapper runs the pinned 2.6.1 in `build:2.6.1`, tacplus-dev/looper#27).
- **Released / deployed:** v0.1.0 (2026-11-07), v0.1.1 (#8): source tags; binaries, images and the GitHub release come from the build pipeline (tacplus-dev/looper#17).
- **Dependencies:** `go 1.26.0`; grpc v1.84.0, x/net v0.62.0, x/crypto v0.58.0, x/text v0.43.0, x/sys v0.49.0 (#4). `govulncheck ./...` (v1.9.0) finds no reachable vulnerabilities; module mode leaves only GO-2026-5932 (x/crypto/openpgp, no fix, not imported).
- **Known gaps:** SeaBeam module paths (#3). `.gitlab-ci.yml` derives a `tacplus-<MAJOR>-<MINOR>` compiler image that 2.6 does not publish (#3, PROPOSALS.md).
- **Blocked on:** nothing.
