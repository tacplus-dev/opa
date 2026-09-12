Audit complete. The scratch test I used to confirm one finding was removed and `internal/` is unchanged. Findings ranked by severity; I've dropped everything I could not confirm (for example, `extra_claims` with numeric values works fine because `structpb.NewValue` accepts `json.Number`).

## High

**1. Numeric `token_use` is silently dropped to UNSPECIFIED.** `internal/handler.go:550-553`. OPA returns numbers as `json.Number`, never `float64` or `int64`, so both numeric cases in `parseTokenUse` are dead code. I confirmed this at runtime: a policy returning `token_use: 6` yields `TOKEN_USE_UNSPECIFIED`. The doc comment on `EvalProxyPolicy` (line 423) and on `parseTokenUse` (line 539) both promise integer 6 is accepted. Failure scenario: a proxy policy that sets `token_use: 6` expecting an EXTERNAL token gets whatever the kernel does for "unset", with no warning logged. Whether that is more or less permissive depends on the kernel, but the handler is not forwarding what the policy specified, at the authorization decision point.

**2. Entitlement evaluation fails soft: policy failures return a successful, partially-populated result.** `internal/handler.go:152-156`, `194-197`, `211-216`. Three distinct failure modes all return `resp, nil`:
- The policy is undefined for this input (OPA returns no result, so `Evaluate` returns `nil, nil`), which is the normal Rego outcome when no rule body matches.
- `all_entitlements` expansion fails.
- `token_entitlements` is malformed.

In the last two cases `Namespace`, `AuthorizedProjects` and `DefaultProject` have already been copied in before the early return, so the kernel receives an OK response carrying a namespace assignment and a project list but no grants. The kernel cannot distinguish "policy evaluated and granted nothing" from "policy broke". Contrast `EvalProxyPolicy`, which returns `codes.Internal` on a non-object result at line 450. One RPC on this handler fails closed with an error, the other fails soft with success.

## Medium

**3. The two config slots share one evaluator map keyed only by entry point, and the entry points the RPCs use are hardcoded.** `internal/handler.go:35`, `110`, `425`, `532`. `EvalEntitlement` always looks up `data.entitlements.eval` and `EvalProxyPolicy` always looks up `data.proxypolicy.eval`, but `loadPolicy` stores whatever entry point the config declares. Two concrete consequences:
- A kernel update that pushes a proxy policy with entry point `data.proxypolicy.allow` compiles, logs "loaded OPA policy", and is never consulted. Requests keep hitting the previous evaluator, or fail with FailedPrecondition if there was none. The update appears to succeed.
- Nothing stops the `proxy_policy` config field from carrying entry point `data.entitlements.eval`, in which case it replaces the entitlements evaluator. A config mistake in one slot silently changes decisions in the other.

**4. Request context is discarded; evaluation runs on `context.Background()` with no deadline.** `internal/opa.go:62`, called from `handler.go:143` and `443` which both receive `ctx` and ignore it. The evaluator is built with default capabilities, so `http.send` and DNS built-ins are available. A policy that calls out to a slow or dead host blocks the gRPC handler until that call times out on its own, and a client that cancels or hits its deadline does not stop the evaluation. Same for a pathological policy that loops on large input. Goroutines accumulate under load with no way to shed them.

## Low

**5. The full policy result is logged at INFO, contradicting the redaction reasoning ten lines above it.** `internal/handler.go:148`. Lines 126 to 133 explain in detail why claim values must not be logged, then line 148 logs the entire policy output with `zap.Any`. The output is derived from the claims, and a policy that echoes `email` or `sub` into its result (for example into `default_project` or a debugging field) puts them straight into the sink. The existing logging test only exercises a policy that does not echo claims, so it would not catch this. The `%T` field on the same line is leftover debugging output.

**6. Catch-all key sweep in `collectEntitlementObject` folds any unrecognized value into grants.** `internal/handler.go:369-378`. The comments on lines 300 to 307 and the test at `handler_test.go:37` document two bugs already caused by this sweep (`principal_scope` and `data_grants` landing in the grants claim). Each fix added one more exclusion to the list on line 371. The next scalar field added to `TokenEntitlements` will land in the JWT grants claim again until someone notices. This is kept alive only by the legacy service-map shape tested at `handler_test.go:161`.

## Not reported

I checked and found no problems with: concurrent access to the evaluator map (RWMutex is used correctly and `PreparedEvalQuery` is safe for concurrent use); the `allow` default (missing or non-bool allow denies); the `append_aud` handling; `extra_claims` conversion; the bootstrap-then-kernel load ordering in `main.go`; and the `collectRegoFiles` walk.
