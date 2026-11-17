# History: opa

Append-only: add entries at the end, never edit or remove one (CI enforces it). One line each:
`- <YYYY-MM-DD> <who: looper|agent|jody> <kind: issue|decision|release|deploy> <one line> (<links>)`

- 2026-10-30 looper issue tacplus floor 2.4 is behind the latest released line (2.5): move the floor to 2.5 (#7, 136d99b)
- 2026-11-06 looper issue Declare the tacplus floor: 2.4 (#1)
- 2026-11-06 looper issue 37 known vulnerabilities in grpc v1.79.3 and golang.org/x/{net,crypto,text,sys}: bump to patched releases (#4)
- 2026-11-08 looper issue tacplus floor 2.5 is behind the latest released line (2.6): move the floor to 2.6 and commit tacplus.lock (#8)
- 2026-11-09 looper issue Work without SeaBeam: no gitlab.com/seabeam module paths, sources or registries (#3)
- 2026-11-15 looper release v0.1.3 (https://github.com/tacplus-dev/opa/releases/tag/v0.1.3)
- 2026-11-15 looper issue Pin the build: floor 2.6 and a committed tacplus.lock, so the build pipeline and its security checks run on this repository (#9, a475fb7)
- 2026-11-15 looper issue No gates: the justfile has no `gates` recipe, so `just gates` fails on main (#5)
- 2026-11-15 looper issue Dockerfile: base image alpine:3.22 is behind the latest Alpine line (3.24) (#6)
- 2026-11-15 looper issue Policies evaluate without network built-ins and within the RPC's deadline (#11)
- 2026-11-15 looper issue Release image runs as root: semgrep missing-user-entrypoint (HIGH) on Dockerfile:30 fails the build's security checks (#10)
- 2026-11-15 looper release v0.2.0 (https://github.com/tacplus-dev/opa/releases/tag/v0.2.0)
- 2026-11-16 looper release v0.2.1 (https://github.com/tacplus-dev/opa/releases/tag/v0.2.1)
- 2026-11-16 looper issue parseTokenUse ignores json.Number: a policy's numeric token_use (documented as accepted) always becomes TOKEN_USE_UNSPECIFIED (#13)
- 2026-11-16 looper issue New non-blocking findings: gosec G706 (LOW, log injection) at main.go:32, :39, :47 (#16)
- 2026-11-16 looper issue GO-2026-6859 (net/http, stdlib) reachable: the pinned build:2.6.1 builds with Go < 1.27.4; move tacplus.lock to 2.6.8 (#14)
- 2026-11-16 looper release v0.3.0 (https://github.com/tacplus-dev/opa/releases/tag/v0.3.0)
- 2026-11-16 looper issue Release: tag vMAJOR.MINOR.PATCH (start at v0.1.0) and publish its image (#2)
- 2026-11-17 looper issue New non-blocking findings: GO-2026-5932 now fixed in golang.org/x/crypto v0.59.0, and trivy DS-0026 (no HEALTHCHECK) on Dockerfile (#18)
