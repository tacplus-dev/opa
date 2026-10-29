# State: opa

Overwritten to stay current (loopers and agents). Last updated: 2026-10-29 by looper (#7).

- **Version / line:** unreleased (no vX.Y.Z tag yet); tacplus floor 2.5 (#7)
- **Works:** generation with tacplus 2.5 (`depends: [kernel]`, `grpc-servers: all`), build, vet and tests: `just gates` passes with the released tacplus 2.5.1 binary and Go on PATH.
- **Released / deployed:** nothing yet (#2).
- **Known gaps:** `just gates` fails in the looper: its `tacplus` wrapper runs the deprecated compiler image, which has no `go` (tacplus-dev/looper#27). Dependency vulnerabilities (#4); SeaBeam module paths (#3).
- **Blocked on:** tacplus-dev/looper#27 (gates in the looper).
