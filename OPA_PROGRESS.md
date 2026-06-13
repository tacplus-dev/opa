# OPA Progress

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
