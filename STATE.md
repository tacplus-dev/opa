# State: opa

Overwritten to stay current (loopers and agents). Last updated: 2026-11-06 by looper (#4).

- **Version / line:** unreleased (no vX.Y.Z tag yet); tacplus floor 2.5 (#7)
- **Works:** generation with tacplus 2.5 (`depends: [kernel]`, `grpc-servers: all`), build, vet and tests: `just gates` passes in the looper (its `tacplus` wrapper runs the 2.5 line's newest patch, 2.5.3, in `build:2.5`, tacplus-dev/looper#27).
- **Released / deployed:** nothing yet (#2).
- **Dependencies:** `go 1.26.0`; grpc v1.84.0, x/net v0.62.0, x/crypto v0.58.0, x/text v0.43.0, x/sys v0.49.0 (#4). `govulncheck ./...` (v1.9.0) finds no reachable vulnerabilities; module mode leaves only GO-2026-5932 (x/crypto/openpgp, no fix, not imported).
- **Known gaps:** SeaBeam module paths (#3).
- **Blocked on:** nothing.
