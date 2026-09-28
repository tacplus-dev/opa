package internal

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/services/opa"
	fsPb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/sys/filesystem"
	opaPb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/sys/opalib"
	userentitlements_pb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/vservices/userentitlements"
)

// ---------------------------------------------------------------------------
// Finding 1 — the catch-all field sweep.
// ---------------------------------------------------------------------------

// The sweep folded any unrecognized key's VALUE into the JWT grants claim. Two
// tenants' policies are exercised here because the leak is cross-tenant in
// effect: tenant B's policy carrying a field tenant A's does not must not be
// able to mint tenant B a grant token out of that field's value while tenant A,
// emitting only recognized fields, behaves identically before and after.
func TestCollectEntitlementTokensRejectsUnrecognizedFields(t *testing.T) {
	// Tenant A: only recognized fields. Must still parse, unchanged.
	tokens, dataGrants, azp, scope, err := collectEntitlementTokens(map[string]any{
		"grants":          []any{"ECHO_RPCS_ANY", map[string]any{"token": "ANTHROPIC_RPCS_ANY"}},
		"data_grants":     []any{"ACCOUNTINGRECORD_ACCOUNTING_RECORD_PRINCIPAL_SUB"},
		"azp":             []any{"delegate-a"},
		"principal_scope": "PRINCIPAL_SCOPE_ANY",
	})
	if err != nil {
		t.Fatalf("tenant-a: recognized fields rejected: %v", err)
	}
	if want := []string{"ECHO_RPCS_ANY", "ANTHROPIC_RPCS_ANY"}; !sameStrings(tokens, want) {
		t.Errorf("tenant-a grants = %v, want %v", tokens, want)
	}
	if want := []string{"ACCOUNTINGRECORD_ACCOUNTING_RECORD_PRINCIPAL_SUB"}; !sameStrings(dataGrants, want) {
		t.Errorf("tenant-a data_grants = %v, want %v", dataGrants, want)
	}
	if want := []string{"delegate-a"}; !sameStrings(azp, want) {
		t.Errorf("tenant-a azp = %v, want %v", azp, want)
	}
	if scope != "PRINCIPAL_SCOPE_ANY" {
		t.Errorf("tenant-a principal_scope = %q, want PRINCIPAL_SCOPE_ANY", scope)
	}

	// Tenant B: the same object plus fields this code does not understand, at
	// the top level and nested one and two levels down. Under the old sweep the
	// top-level "tenant_note" value became a grant token, and the nested
	// {"meta": {"principal_scope": ...}} stamped ANY scope without the policy
	// ever having named principal_scope at the level that means it.
	for name, result := range map[string]map[string]any{
		"top-level unknown scalar": {
			"grants":      []any{"ECHO_RPCS_ANY"},
			"tenant_note": "SENTINEL_ANY",
		},
		"top-level unknown list": {
			"grants":       []any{"ECHO_RPCS_ANY"},
			"future_field": []any{"KERNEL_ANY"},
		},
		"nested principal_scope under an unknown key": {
			"grants": []any{"ECHO_RPCS_ANY"},
			"meta":   map[string]any{"principal_scope": "PRINCIPAL_SCOPE_ANY"},
		},
		"unknown key inside a wrapped grant element": {
			"grants": []any{map[string]any{"token": "ECHO_RPCS_ANY", "principal_scope": "PRINCIPAL_SCOPE_ANY"}},
		},
		"nested object two levels down": {
			"grants": []any{[]any{map[string]any{"data_grants": []any{"KERNEL_ANY"}}}},
		},
	} {
		gotTokens, _, _, gotScope, err := collectEntitlementTokens(result)
		if err == nil {
			t.Errorf("%s: accepted, grants=%v scope=%q; want an error", name, gotTokens, gotScope)
			continue
		}
		if gotTokens != nil || gotScope != "" {
			t.Errorf("%s: error path still produced grants=%v scope=%q", name, gotTokens, gotScope)
		}
	}
}

// The allow-list has to name exactly the fields TokenEntitlements carries: a
// field in the proto but missing here would be silently unusable, and a field
// here but not in the proto is dead. This is the check that keeps the coupling
// honest as the proto changes.
func TestEntitlementAllowListMatchesTokenEntitlementsProto(t *testing.T) {
	for _, want := range []string{"grants", "data_grants", "azp", "principal_scope"} {
		if !entitlementObjectFields[want] {
			t.Errorf("TokenEntitlements field %q is not in the allow-list", want)
		}
	}
	if len(entitlementObjectFields) != 4 {
		t.Errorf("allow-list = %v, want exactly the four TokenEntitlements fields", sortedKeys(entitlementObjectFields))
	}
}

// ---------------------------------------------------------------------------
// Finding 2 — EvalEntitlement must fail closed on evaluation failure.
// ---------------------------------------------------------------------------

func loadEntitlementsPolicy(t *testing.T, rego string) *Handler {
	t.Helper()
	h := NewHandler()
	if err := h.loadPolicy(&opaPb.OpaPolicy{
		EntryPoint: EntitlementsEntryPoint,
		Bundle: &fsPb.Directory{
			Files: []*fsPb.File{{Name: "main.rego", Content: []byte(rego)}},
		},
	}, EntitlementsEntryPoint); err != nil {
		t.Fatalf("loadPolicy: %v", err)
	}
	return h
}

// All three failure modes used to return (resp, nil): an OK RPC carrying the
// namespace/authorized_projects the policy DID resolve plus an empty grant set,
// which kernel cannot tell from "this caller is entitled to nothing". The
// sibling EvalProxyPolicy already fails closed with codes.Internal on the
// equivalent situation; these now match it.
func TestEvalEntitlementFailsClosedOnEvaluationFailure(t *testing.T) {
	cases := []struct {
		name string
		rego string
	}{{
		// Policy undefined for this input: no rule matches and there is no
		// default, so OPA yields nothing and the result is not an object.
		name: "policy undefined for this input",
		rego: `package entitlements

eval := {"token_entitlements": {"grants": ["ECHO_RPCS_ANY"]}} if {
	input.claims.never_set == "no"
}
`,
	}, {
		name: "malformed token_entitlements",
		rego: `package entitlements

eval := {
	"namespace":          "/tenants/drpepper",
	"token_entitlements": {"grants": ["ECHO_RPCS_ANY"], "surprise": "SENTINEL_ANY"},
}
`,
	}, {
		name: "token_entitlements absent",
		rego: `package entitlements

eval := {"namespace": "/tenants/drpepper", "authorized_projects": ["proj-a"]}
`,
	}}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := loadEntitlementsPolicy(t, tc.rego)
			resp, err := h.EvalEntitlement(context.Background(), nil, &userentitlements_pb.EntitlementEvalInput{
				Namespace: "/tenants/drpepper",
			})
			if err == nil {
				t.Fatalf("EvalEntitlement returned OK with namespace=%q grants=%v; want an error",
					resp.GetNamespace(), resp.GetTokenEntitlements().GetGrants())
			}
			if resp != nil {
				t.Errorf("EvalEntitlement returned a non-nil response alongside the error: %v", resp)
			}
			if got := status.Code(err); got != codes.Internal {
				t.Errorf("status code = %v, want %v (matching EvalProxyPolicy)", got, codes.Internal)
			}
		})
	}
}

// The fail-closed change must not turn a legitimate evaluation into an error:
// a policy that resolves grants, and one that deliberately entitles the caller
// to nothing via an empty token_entitlements object, both still succeed.
func TestEvalEntitlementStillSucceedsOnWellFormedResults(t *testing.T) {
	h := loadEntitlementsPolicy(t, `package entitlements

eval := {
	"namespace":           "/tenants/drpepper",
	"authorized_projects": ["proj-a"],
	"default_project":     "proj-a",
	"token_entitlements":  {"grants": ["ECHO_RPCS_ANY"], "principal_scope": "PRINCIPAL_SCOPE_ANY"},
}
`)
	resp, err := h.EvalEntitlement(context.Background(), nil, &userentitlements_pb.EntitlementEvalInput{})
	if err != nil {
		t.Fatalf("EvalEntitlement: %v", err)
	}
	if got := resp.GetNamespace(); got != "/tenants/drpepper" {
		t.Errorf("namespace = %q, want /tenants/drpepper", got)
	}
	if got := resp.GetTokenEntitlements().GetPrincipalScope(); got != "PRINCIPAL_SCOPE_ANY" {
		t.Errorf("principal_scope = %q, want PRINCIPAL_SCOPE_ANY", got)
	}

	h = loadEntitlementsPolicy(t, `package entitlements

eval := {"namespace": "/tenants/drpepper", "token_entitlements": {}}
`)
	resp, err = h.EvalEntitlement(context.Background(), nil, &userentitlements_pb.EntitlementEvalInput{})
	if err != nil {
		t.Fatalf("EvalEntitlement on a deliberate empty entitlement: %v", err)
	}
	if n := len(resp.GetTokenEntitlements().GetGrants()); n != 0 {
		t.Errorf("grant count = %d, want 0", n)
	}
}

// ---------------------------------------------------------------------------
// Finding 3 — a reload declaring a different entry point must be refused.
// ---------------------------------------------------------------------------

func configWithEntitlementsPolicy(policy *opaPb.OpaPolicy) *pb.Config {
	return &pb.Config{EntitlementsPolicy: policy}
}

func entitlementsPolicyReturning(entryPoint, grant string) *opaPb.OpaPolicy {
	return &opaPb.OpaPolicy{
		EntryPoint: entryPoint,
		Bundle: &fsPb.Directory{
			Files: []*fsPb.File{{
				Name:    "main.rego",
				Content: []byte("package entitlements\n\neval := {\"token_entitlements\": {\"grants\": [\"" + grant + "\"]}}\n"),
			}},
		},
	}
}

func TestLoadPolicyRejectsMismatchedEntryPoint(t *testing.T) {
	h := NewHandler()
	h.mu.Lock()
	if err := h.loadPolicy(entitlementsPolicyReturning(EntitlementsEntryPoint, "ECHO_RPCS_ANY"), EntitlementsEntryPoint); err != nil {
		h.mu.Unlock()
		t.Fatalf("initial loadPolicy: %v", err)
	}
	h.mu.Unlock()

	// The mismatch that matters most: a config that reuses the OTHER slot's
	// entry point. It compiles, so it used to be stored under "data.proxypolicy.eval"
	// and logged as "loaded OPA policy" while EvalEntitlement kept evaluating
	// the bundle from before the push.
	err := h.UserentitlementsOpaEvalConfig(configWithEntitlementsPolicy(
		entitlementsPolicyReturning(ProxyPolicyEntryPoint, "KERNEL_ANY")))
	if err == nil {
		t.Fatal("reload with a mismatched entry point was accepted; want an error")
	}
	for _, want := range []string{ProxyPolicyEntryPoint, EntitlementsEntryPoint} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}

	// The mismatched bundle must not have been stored anywhere, under either name.
	h.mu.RLock()
	_, wrongSlot := h.evaluators[ProxyPolicyEntryPoint]
	h.mu.RUnlock()
	if wrongSlot {
		t.Error("the refused bundle was stored under the entry point it declared")
	}

	// The previously loaded policy stays in force: still ECHO_RPCS_ANY, not the
	// KERNEL_ANY the refused push carried.
	resp, evalErr := h.EvalEntitlement(context.Background(), nil, &userentitlements_pb.EntitlementEvalInput{})
	if evalErr != nil {
		t.Fatalf("EvalEntitlement after a refused reload: %v", evalErr)
	}
	if got := resp.GetTokenEntitlements().GetGrants(); len(got) != 1 || got[0] != "ECHO_RPCS_ANY" {
		t.Fatalf("grants after a refused reload = %v, want [ECHO_RPCS_ANY] (the pre-reload policy)", got)
	}
}

// A matching entry point still reloads, replacing the evaluator in place.
func TestLoadPolicyAcceptsMatchingEntryPoint(t *testing.T) {
	h := NewHandler()
	if err := h.UserentitlementsOpaEvalConfig(configWithEntitlementsPolicy(
		entitlementsPolicyReturning(EntitlementsEntryPoint, "ECHO_RPCS_ANY"))); err != nil {
		t.Fatalf("initial config: %v", err)
	}
	if err := h.UserentitlementsOpaEvalConfig(configWithEntitlementsPolicy(
		entitlementsPolicyReturning(EntitlementsEntryPoint, "KERNEL_ANY"))); err != nil {
		t.Fatalf("reload: %v", err)
	}
	resp, err := h.EvalEntitlement(context.Background(), nil, &userentitlements_pb.EntitlementEvalInput{})
	if err != nil {
		t.Fatalf("EvalEntitlement: %v", err)
	}
	if got := resp.GetTokenEntitlements().GetGrants(); len(got) != 1 || got[0] != "KERNEL_ANY" {
		t.Fatalf("grants after reload = %v, want [KERNEL_ANY]", got)
	}
}
