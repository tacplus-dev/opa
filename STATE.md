# State: opa

Overwritten to stay current (loopers and agents). Last updated: 2026-10-30 by looper (#7).

- **Version / line:** unreleased (no vX.Y.Z tag yet); tacplus floor 2.5 (#7)
- **Works:** generation with tacplus 2.5 (`depends: [kernel]`, `grpc-servers: all`), build, vet and tests: `just gates` passes in the looper (its `tacplus` wrapper runs the 2.5 line's newest patch, 2.5.3, in `build:2.5`, tacplus-dev/looper#27).
- **Released / deployed:** nothing yet (#2).
- **Known gaps:** Dependency vulnerabilities (#4); SeaBeam module paths (#3).
- **Blocked on:** nothing.
