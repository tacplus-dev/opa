# OPA Progress

## 2026-06-17

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
