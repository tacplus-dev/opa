# Proposals: opa

Written by loopers. Each proposal: what, why, links. Jody promotes accepted ones into ROADMAP.md (via an agent);
the looper then marks it `promoted` or `declined` here with the date.

## Open
- **ROADMAP.md: floor 2.5, and #1 superseded** (2026-10-29, #7): main declares `tacplus: "2.5"`; ROADMAP.md "Now" still says floor 2.4 and that the gates wait on the compiler image, and "Next"/"Later" point every issue at #1 / tacplus-dev/tacplus#189 (closed). #7 supersedes #1's goal, and the gates no longer wait on anything: they pass in the looper since tacplus-dev/looper#27 (2026-10-30). The `Blocked-by: #1` on #3, #4 and #5 can be dropped once #1 closes.
  Update 2026-11-06 (nightly scan): the floor is now 2.5 and is behind again: tacplus 2.6 was released 2026-11-05, see #8.
- **Restrict the Rego built-ins policies may call** (2026-11-06, nightly scan): `NewPolicyEvaluator` (`internal/opa.go`) prepares every bundle with OPA's default capabilities. A pushed policy can therefore call `http.send` and the DNS built-ins, and from opa's pod at that. Those calls run on `context.Background()` (`Evaluate`), with no deadline, no cancellation and no `trace_id`. The proposal: pass `rego.Capabilities` with the network built-ins removed (or an explicit allowlist), and refuse a bundle that uses them at load. Why: an entitlement or proxy decision should be a pure function of its input. A policy that reaches out stalls kernel's login and proxy paths on a slow host, and its outbound calls would be the only downstream calls opa makes, untraced (tacplus-dev/looper#51). It's new behaviour, because a bundle that uses `http.send` today would be refused, so it needs a design and Jody's approval. Links: `decisions/security-audit-2026-09-12/CONSOLIDATED_REPORT.md` (Medium: "RPC context is discarded", which is a bug and separate, queued in `FIX-high-tier.md`), `raw-fable.md` §4.
- **ROADMAP.md: #3 done** (2026-11-06, #3): the "Work without SeaBeam" line in "Later" can go; the module is `github.com/tacplus-dev/opa` and `just gates` keeps SeaBeam references out.
- **Release CI replaces the removed `.gitlab-ci.yml`** (2026-11-06, #3): SeaBeam's pipeline (its registry, its tacplus image, a `tacplus-version:` key no tacplus.yaml has) was removed, not ported; #2 is where GitHub Actions publishes `ghcr.io/tacplus-dev/opa`. `Dockerfile.cross` (distroless, no launcher) duplicates the Dockerfile's purpose: #2 can keep one.
- **Comments that still say "schematic"** (2026-11-06, #3): `proto/services/opa.proto` ("Loaded from services/opa/ by schematic") and the Dockerfile description name the old control plane; kernel's Dockerfile does too. A naming pass across tacplus-dev, not this repository alone.

## Promoted / declined
