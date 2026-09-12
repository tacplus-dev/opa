# opa security audit 2026-09-12 — High tier fixes

Three High findings from `CONSOLIDATED_REPORT.md`, all fixed in `internal/handler.go`:

1. **Allow-list rewrite for entitlement parsing.** `collectEntitlementObject`'s catch-all field
   sweep folded any unrecognized policy field's value straight into the JWT grants claim — the
   same bug class patched twice before by adding one more name to an exclusion list
   (`data_grants`, `principal_scope`). Replaced with `entitlementObjectFields`/`grantElementFields`
   allow-lists (`parseEntitlementObject`/`parseGrants`/`collectEntitlementTokens`): any key or
   nested shape not explicitly recognized is now a parse error, not a silent grant. Adding a field
   to `TokenEntitlements` now requires adding it to the allow-list — that coupling is the fix.

2. **`EvalEntitlement` now fails closed**, matching sibling `EvalProxyPolicy`. Three previously-soft
   failure modes now return `codes.Internal` instead of an OK response with empty grants:
   policy-undefined result (non-object), `token_entitlements` key absent, and malformed
   `token_entitlements` content. `namespace`/`authorized_projects`/`default_project` are still
   stamped before the early-return so a legitimate empty-entitlement policy
   (`{"token_entitlements": {}}`) is unaffected.

3. **Config reload now rejects an entry-point mismatch.** `loadPolicy` takes the entry point the
   slot being loaded is evaluated at (`EntitlementsEntryPoint`/`ProxyPolicyEntryPoint`) and refuses
   to store a pushed policy that declares a different one, instead of silently keeping the
   previous evaluator in force while logging success. `main.go`'s two `LoadBootstrapPolicy` call
   sites updated to pass the expected entry point and check the returned error.

Verified: `go build ./...` clean, `go vet ./internal/...` clean, `go test ./internal/...` — all
tests pass except `TestAllEntitlementsUseGeneratedGrantNames` (pre-existing, confirmed unrelated:
reproduces identically on `git stash` with none of this fix applied — a generated-grant-catalog
gap, same class as kernel's own pre-existing `IdentityServiceImplTest` grant-catalog failure).

One test case in the child's own new test file (`all_entitlements expansion failure`) was removed
before commit: it asserted an error for a policy returning both `all_entitlements: true` and a
well-formed `token_entitlements`, but the code's `all_entitlements` branch (handler.go:214-227)
returns before `token_entitlements` is ever parsed, by design — the wildcard wins and the case
never errors. The `all_entitlements` expansion-failure path itself (handler.go:220) is still
covered by fail-closed code and is exercised implicitly by every other `all_entitlements` test
already in `handler_test.go`; it isn't independently triggerable from policy input, since
`allEntitlementGrantTokens()` reads the generated catalog and always succeeds.

Not done (queued behind this per state.md's revised priority order): Medium/Low tier findings
(numeric `token_use` dropped, `auth_type` not forwarded on proxy path, RPC context discarded,
full policy output logged).
