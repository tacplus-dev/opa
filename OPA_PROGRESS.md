# OPA Progress

## 2026-06-17

- Demo-readiness rebuild `v1.2.187-main.193` proved tacplus `v1.2.212`
  regenerated the current mint RPC name:
  - OPA build pulled tacplus digest
    `sha256:b954e70c6bca8aff75706089c746b77495fe1ef373b3bc8fa2a39dd641692fc9`,
    matching tacplus image tag `212`.
  - The generated OPA binary contains `kernel.Mint/MintX509Cert`.
  - Mock kernel logs showed the new OPA pods minting S2S tokens and publishing
    health.
- The remaining startup hang was in tacplus generated Go `Run`: it waited for
  the `ManageCert` stream to deliver an initial cert before `Listen`, even
  though `ServiceManager.New` had already obtained the serving cert through
  X509Mint. Tacplus `v1.2.213` fixes that by treating `ManageCert` as
  background renewal/handler delivery when an X509Mint cert exists.
- Corrected OPA rebuild plan: tag `v1.2.187-main.194` on main after tacplus
  `v1.2.213` is green. Expected validation: OPA pods roll to `.194`, bind
  `:8443`, and old `.188` fallback pods terminate.

- Demo-readiness rebuild started with tag `v1.2.212-main.193` on OPA
  `main` commit `a25c8b5`.
- This is intentionally a tag-only rebuild: the source defect was in tacplus
  generated Go ServiceManager startup ordering and in the older generated
  mint RPC name embedded in OPA v192 output.
- Tacplus `v1.2.212` is green and should regenerate OPA with active state-store
  selection before auto-TLS/X509Mint and with the current kernel
  `MintX509Cert` RPC name.
- OPA pipeline `2606898566` is running; next validation is that the published
  image starts in the mock cluster and binds/listens instead of hanging in
  `s2sm.New()`.

## 2026-06-13

- Added entitlement normalization so Rego policies may return flat
  `{"grants":["TOKEN"]}`, legacy `{"grants":[{"token":"TOKEN"}]}`, or old
  service maps.
- The normalizer emits the JSON shape expected by the currently generated
  `TokenEntitlements` descriptor, so both wrapper and future flat-string proto
  layouts can be supported.
- Added unit coverage for flat grants, wrapper grants, and legacy service-map
  input.
- Verification passed: `go test ./...`.
