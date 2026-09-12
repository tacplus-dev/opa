# opa Security & Quality Audit — Consolidated Report
2026-09-12 — multimodal review: Fable 5.1 (Claude), Codex CLI (GPT), Grok 4.6 (via GitHub Copilot CLI)
Scope: `internal/` (4 Go files). `model/` (tacplus-generated) and `bin/` excluded.

`opa` wraps Open Policy Agent to resolve authorization/entitlement decisions for the platform —
kernel calls it to resolve namespace assignment and grants. This report has the strongest
cross-model agreement of any service audited today: multiple findings were independently
discovered by all three tools, and I personally verified the top one directly in source.

---

## High — real, cross-confirmed, worth fixing first

### 1. Catch-all field sweep turns any unrecognized policy-result field into a grant token [VERIFIED, found independently by all three tools]
**File:** `internal/handler.go:295-378` (`collectEntitlementObject`/`collectEntitlementTokensInto`)
I read this code directly. After handling five known fields (`token`, `grants`, `data_grants`,
`azp`, `principal_scope`), every *other* key in the policy's returned object is recursed into —
and if its value is a bare string or list of strings, it lands directly in the actual JWT grants
list. **The code's own comments admit this exact bug has already happened twice**: `data_grants`
and `principal_scope` both previously leaked into the grants claim this way, each time patched by
adding one more name to the exclusion list rather than fixing the underlying design (an allow-list
parser, or rejecting unknown fields outright). Grok additionally demonstrated a concrete new
instance: a nested `{"meta": {"principal_scope": "PRINCIPAL_SCOPE_ANY"}}` in a policy result would
stamp `ANY` scope via the same sweep, since nesting isn't excluded either. **Any future field added
to the `TokenEntitlements` schema will reintroduce this bug a third time** unless the design itself
changes.

### 2. Entitlement evaluation fails soft on error — kernel can't tell "granted nothing" from "evaluation broke" [Fable + Grok cross-confirmed]
**File:** `internal/handler.go:149-156, 184-187, 196-216`
Three distinct failure modes (policy undefined for this input, `all_entitlements` expansion
failure, malformed `token_entitlements`) all return `(resp, nil)` — a successful RPC response with
empty grants, after `Namespace`/`AuthorizedProjects`/`DefaultProject` have already been populated.
The sibling RPC, `EvalProxyPolicy`, fails closed with `codes.Internal` on the equivalent situation
(a non-object result) — the same service treats the same class of failure two different ways
depending on which entry point was called. Grok separately notes that an empty namespace on this
path is documented elsewhere as the kernel's cluster-root fallback — meaning a broken policy
evaluation could resolve to broader scope, not just "nothing," depending on how kernel interprets
the empty response.

### 3. Config reload can silently keep the old policy in force [Fable + Grok cross-confirmed]
**File:** `internal/handler.go:35, 70-89, 110, 425, 502-515, 532, 565-575`
`EvalEntitlement` and `EvalProxyPolicy` always look up hardcoded entry points
(`data.entitlements.eval` / `data.proxypolicy.eval`), but `loadPolicy` stores whatever entry point
a pushed config actually declares, with no check that it matches what the handler expects. A config
update that declares a different entry point (or a config mistake that reuses the *other* slot's
entry point) compiles successfully, logs "loaded OPA policy," and either leaves the previous
evaluator in charge with the update silently ignored, or replaces the wrong evaluator entirely. An
operator pushing an intended policy fix would have no signal that it never took effect.

---

## Medium
- **Numeric `token_use` is silently dropped to UNSPECIFIED** (Fable, empirically verified at
  runtime) — OPA's JSON decoder returns `json.Number`, never `float64`/`int64`, so the numeric
  branches in `parseTokenUse` are dead code; a policy returning `token_use: 6` gets the same
  handling as if it had returned nothing, contrary to the handler's own documented contract.
- **`EvalProxyPolicy` never forwards the policy's `auth_type` result** (Codex + Grok cross-confirmed)
  — a policy that specifies e.g. `TOKEN_TYPE_SAML` gets silently downgraded to
  `TOKEN_TYPE_UNDEFINED` in the response kernel actually receives.
- **RPC context is discarded; evaluation always runs on `context.Background()`** (Codex + Fable +
  Grok — all three independently) — a canceled or timed-out caller doesn't stop the evaluation;
  combined with policies that can call `http.send`/DNS built-ins (capabilities aren't restricted),
  a slow or dead external call inside a policy blocks the handler with no way to shed the load.

## Low
- Full policy evaluation output is logged at INFO despite explicit reasoning ten lines earlier in
  the same file about why claim values must not be logged — a policy that echoes `email`/`sub`
  into its result puts them straight into the log sink (Fable).
- A leftover `%T` debug format specifier on the same log line (Fable).

## What all three tools checked and found clean
Concurrent access to the evaluator map (correct `RWMutex` usage, `PreparedEvalQuery` is safe for
concurrent use), the `allow` default (missing/non-bool denies, doesn't fail open), `append_aud`
handling, `extra_claims` conversion, bootstrap-then-kernel load ordering, and the rego-file
collection walk.

## Recommended next step
Fix #1 first — it's a genuinely recurring bug class in the platform's actual authorization
decision point, and the fix (an explicit allow-list of recognized fields, rejecting/logging
anything else, instead of a catch-all sweep with an ever-growing exclusion list) closes the whole
class rather than the next individual instance. #2 and #3 are both real correctness/observability
gaps worth fixing in the same pass given they're in the same handler.

## Raw per-tool output
- Codex: `codex.out.md` (3 findings)
- Fable 5.1: `fable.out.md` (6 findings, with an explicit "checked and found clean" section)
- Grok 4.6: `grok.out.md` (first pass, cut short by Copilot CLI's internal timeout) + `grok2.out.md`
  (completed continuation, 3 headline findings + additional correctness/drift notes)
