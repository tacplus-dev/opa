# State: opa

Overwritten to stay current (loopers and agents). Last updated: 2026-11-15 by looper (#9).

- **Version / line:** v0.1.3 (0.1 line, source tags on green main); tacplus floor 2.6, pinned to 2.6.1 in `tacplus.lock` (#8)
- **Works:** generation with tacplus 2.6 (`depends: [kernel]`, `grpc-servers: all`), build, vet and tests: `just gates` passes in the pinned `build:2.6.1`, where the build pipeline (tacplus-dev/build) runs them from `tacplus.lock` (#9); the looper's `tacplus` wrapper runs the same release (tacplus-dev/looper#27).
- **Released / deployed:** v0.1.0 (2026-11-07), v0.1.1 (#8), v0.1.2 (#3), v0.1.3 (#9: the build's three HIGH security findings fixed): source tags; binaries, images and the GitHub release come from the build pipeline (tacplus-dev/looper#17).
- **Build's security checks (#9):** pass the policy: gosec G115 in `parseTokenUse` (a number outside `TokenUse` is `TOKEN_USE_UNSPECIFIED`, never truncated; #12), gosec G703 at `main.go` (below; #15) and semgrep `missing-user-entrypoint` on the Dockerfile (below; #10) are fixed in code; no `security-exceptions.yaml`, no `security-baseline.json`. Left, non-blocking: gosec G706 (LOW) at `main.go` (#16), osv GO-2026-5932 (x/crypto/openpgp, not imported, no fix).
- **Bootstrap config (#15):** `BOOTSTRAP_CONFIG` must be an absolute, clean path (no `..`, `.` or doubled separators); anything else stops the service at startup. The file is read through an `os.Root` on its directory (`internal/bootstrap`).
- **Release image runs unprivileged (#10):** the Dockerfile ends with `USER 65532:65532` (group and user `opa`, fixed IDs a pod's `securityContext` can name); opa writes nothing in its container. `just gates` fails if the Dockerfile's last `USER` is anything else.
- **Independent of SeaBeam (#3):** module `github.com/tacplus-dev/opa`, generated code in `tacplus/` (gitignored); the service's proto is this repository's own (`src: proto`), kernel's comes through `depends` from github.com/tacplus-dev/registry; nothing is fetched from gitlab.com, and `just gates` fails on any SeaBeam reference outside PROVENANCE.md, `decisions/` and the planning files. SeaBeam's `.gitlab-ci.yml` is gone (#2 owns release CI).
- **Dependencies:** `go 1.26.0`; grpc v1.84.0, x/net v0.62.0, x/crypto v0.58.0, x/text v0.43.0, x/sys v0.49.0 (#4). `govulncheck ./...` (v1.9.0) finds no reachable vulnerabilities; module mode leaves only GO-2026-5932 (x/crypto/openpgp, no fix, not imported). The pinned `build:2.6.1`'s Go predates 1.27.4 (GO-2026-6859, net/http): #14 moves the pin.
- **Known gaps:** `parseTokenUse` ignores OPA's `json.Number`, so a policy's numeric `token_use` is `TOKEN_USE_UNSPECIFIED` (#13); release CI and the image are #2.
- **Blocked on:** nothing.
