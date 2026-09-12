## Findings

1. **High — Security:** [internal/handler.go:369](</Users/jody/workspace/gitlab/secondstarcavalier/schematic/platform/services/opa/internal/handler.go:369>) — Unknown fields inside `token_entitlements` are recursively interpreted as grant tokens instead of being rejected or ignored.  
   **Failure scenario:** A policy returns `token_entitlements: {"grants": ["LIMITED_GRANT"], "reason": "ADMIN_ANY"}` or nests a valid grant token under any newly added field; the catch-all traversal at lines 369–378 adds `ADMIN_ANY` to the JWT grants even though the policy did not place it in `grants`. This is the same defect pattern the code comments document for the previously mishandled `data_grants` and `principal_scope` fields, and every future scalar/list field risks becoming an authorization grant.

2. **Medium — Correctness / contract drift:** [internal/handler.go:453](</Users/jody/workspace/gitlab/secondstarcavalier/schematic/platform/services/opa/internal/handler.go:453>) — `EvalProxyPolicy` never copies the policy’s documented `auth_type` result into `resp.AuthType`.  
   **Failure scenario:** A proxy policy allows a request with `auth_type: "TOKEN_TYPE_SAML"`; the handler returns `TOKEN_TYPE_UNDEFINED`, so kernel propagates the wrong credential type and the caller receives a JWT/undefined response instead of the requested SAML assertion semantics. The generated result type contains `AuthType`, and the handler’s own contract documents this field at line 422.

3. **Low — Resource-management anti-pattern:** [internal/opa.go:61](</Users/jody/workspace/gitlab/secondstarcavalier/schematic/platform/services/opa/internal/opa.go:61>) — Policy evaluation always uses `context.Background()`, discarding the RPC context accepted by both handlers.  
   **Failure scenario:** If a caller times out or disconnects during an expensive evaluation, OPA continues consuming CPU until evaluation completes; repeated canceled requests can accumulate useless concurrent work and degrade the authorization service.

No confirmed fail-open behavior, caller-controlled policy injection, evaluator-map race, or malformed-request panic was found. Policy preparation/evaluation failures and malformed proxy `allow` results deny or return errors.

I could not execute `go test ./internal` because the read-only sandbox prevented Go from creating its temporary build directory; no files were modified.
