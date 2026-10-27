package internal

import (
	"encoding/json"
	"strings"
	"testing"

	"gitlab.com/seabeam/schematic/platform/services/opa/model/grants"
	entitlements_pb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/generated/entitlements"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCollectEntitlementTokensAcceptsFlatAndWrappedGrants(t *testing.T) {
	tokens, _, azp, scope, err := collectEntitlementTokens(map[string]any{
		"grants": []any{
			"ECHO_RPCS_ANY",
			map[string]any{"token": "ANTHROPIC_RPCS_ANY"},
		},
		"azp": []any{"delegate-a"},
	})
	if err != nil {
		t.Fatalf("collectEntitlementTokens returned error: %v", err)
	}

	if want := []string{"ECHO_RPCS_ANY", "ANTHROPIC_RPCS_ANY"}; !sameStrings(tokens, want) {
		t.Fatalf("tokens = %v, want %v", tokens, want)
	}
	if want := []string{"delegate-a"}; !sameStrings(azp, want) {
		t.Fatalf("azp = %v, want %v", azp, want)
	}
	if scope != "" {
		t.Fatalf("principal_scope = %q, want empty when the policy stamped none", scope)
	}
}

// principal_scope is a SCALAR on the same object as grants, and every key that
// is not explicitly handled is swept into the grant list. Without its own case
// the scope VALUE would arrive as a grant token -- a symbol no catalog declares,
// and one that reads as an entitlement rather than as the width of one. Assert
// both halves: it lands in the scope, and it does NOT land in the grants.
func TestCollectEntitlementTokensKeepsPrincipalScopeOutOfGrants(t *testing.T) {
	tokens, _, _, scope, err := collectEntitlementTokens(map[string]any{
		"grants":          []any{"ECHO_RPCS_ANY"},
		"principal_scope": "PRINCIPAL_SCOPE_ANY",
	})
	if err != nil {
		t.Fatalf("collectEntitlementTokens returned error: %v", err)
	}

	if want := []string{"ECHO_RPCS_ANY"}; !sameStrings(tokens, want) {
		t.Fatalf("tokens = %v, want %v", tokens, want)
	}
	if scope != "PRINCIPAL_SCOPE_ANY" {
		t.Fatalf("principal_scope = %q, want PRINCIPAL_SCOPE_ANY", scope)
	}
}

// data_grants (BIG GOAL 3 item 7/9) is a SEPARATE claim from grants: it names
// what a caller may see RAW on a MAY_REDACT field, not what RPCs it may call.
// Before this test, a policy's data_grants entry had no dedicated case in
// collectEntitlementObject and fell into the catch-all sweep, which folds a
// key's VALUE into the grant list exactly like the principal_scope case above
// warns against -- so a data_grants token ended up in the JWT's grants claim
// and never in its own data_grants claim, and every MAY_REDACT field's egress
// check (which reads data_grants specifically) saw none.
func TestCollectEntitlementTokensKeepsDataGrantsOutOfGrants(t *testing.T) {
	tokens, dataGrants, _, _, err := collectEntitlementTokens(map[string]any{
		"grants":      []any{"ECHO_RPCS_ANY"},
		"data_grants": []any{"ACCOUNTINGRECORD_ACCOUNTING_RECORD_PRINCIPAL_SUB"},
	})
	if err != nil {
		t.Fatalf("collectEntitlementTokens returned error: %v", err)
	}

	if want := []string{"ECHO_RPCS_ANY"}; !sameStrings(tokens, want) {
		t.Fatalf("tokens = %v, want %v (data_grants must not leak into grants)", tokens, want)
	}
	if want := []string{"ACCOUNTINGRECORD_ACCOUNTING_RECORD_PRINCIPAL_SUB"}; !sameStrings(dataGrants, want) {
		t.Fatalf("dataGrants = %v, want %v", dataGrants, want)
	}
}

// The claim the guard reads is the NORMALIZED JSON, not the policy's raw output,
// so data_grants has to survive normalization too, as its own claim.
func TestNormalizeTokenEntitlementsJSONCarriesDataGrants(t *testing.T) {
	out, err := normalizeTokenEntitlementsJSON(map[string]any{
		"grants":      []any{"ECHO_RPCS_ANY"},
		"data_grants": []any{"ACCOUNTINGRECORD_ACCOUNTING_RECORD_PRINCIPAL_SUB"},
	})
	if err != nil {
		t.Fatalf("normalizeTokenEntitlementsJSON returned error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("normalized JSON does not parse: %v", err)
	}
	dg, ok := got["data_grants"].([]any)
	if !ok || len(dg) != 1 || dg[0] != "ACCOUNTINGRECORD_ACCOUNTING_RECORD_PRINCIPAL_SUB" {
		t.Fatalf("normalized data_grants = %v, want [ACCOUNTINGRECORD_ACCOUNTING_RECORD_PRINCIPAL_SUB]", got["data_grants"])
	}
	grantsList, ok := got["grants"].([]any)
	if !ok || len(grantsList) != 1 || grantsList[0] != "ECHO_RPCS_ANY" {
		t.Fatalf("normalized grants = %v, want [ECHO_RPCS_ANY] (data_grants must not leak in)", got["grants"])
	}
}

// The claim the guard reads is the NORMALIZED JSON, not the policy's raw output,
// so the scope has to survive normalization too.
func TestNormalizeTokenEntitlementsJSONCarriesPrincipalScope(t *testing.T) {
	out, err := normalizeTokenEntitlementsJSON(map[string]any{
		"grants":          []any{"ECHO_RPCS_ANY"},
		"principal_scope": "PRINCIPAL_SCOPE_ANY",
	})
	if err != nil {
		t.Fatalf("normalizeTokenEntitlementsJSON returned error: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("normalized JSON does not parse: %v", err)
	}
	if got["principal_scope"] != "PRINCIPAL_SCOPE_ANY" {
		t.Fatalf("normalized principal_scope = %v, want PRINCIPAL_SCOPE_ANY", got["principal_scope"])
	}

	// Absent from the policy output means absent from the claim: the guard fails
	// closed to self on an absent claim, and an empty string here would be a
	// third spelling of the same thing.
	out, err = normalizeTokenEntitlementsJSON(map[string]any{"grants": []any{"ECHO_RPCS_ANY"}})
	if err != nil {
		t.Fatalf("normalizeTokenEntitlementsJSON returned error: %v", err)
	}
	got = nil
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("normalized JSON does not parse: %v", err)
	}
	if _, ok := got["principal_scope"]; ok {
		t.Fatalf("principal_scope present in %s, want absent", string(out))
	}
}

// The typed proto is what crosses the wire to kernel; a field that normalizes
// correctly and then does not survive protojson would still reach nobody.
func TestTokenEntitlementsFromValueCarriesPrincipalScope(t *testing.T) {
	te, _, err := tokenEntitlementsFromValue(map[string]any{
		"grants":          []any{"ECHO_RPCS_ANY"},
		"principal_scope": "PRINCIPAL_SCOPE_ANY",
	})
	if err != nil {
		t.Fatalf("tokenEntitlementsFromValue returned error: %v", err)
	}
	if got := te.GetPrincipalScope(); got != "PRINCIPAL_SCOPE_ANY" {
		t.Fatalf("PrincipalScope = %q, want PRINCIPAL_SCOPE_ANY", got)
	}
}

func TestTokenEntitlementsFromValueAcceptsFlatGrants(t *testing.T) {
	te, normalizedJSON, err := tokenEntitlementsFromValue(map[string]any{
		"grants": []any{"ECHO_RPCS_ANY"},
	})
	if err != nil {
		t.Fatalf("tokenEntitlementsFromValue returned error: %v", err)
	}
	if !json.Valid(normalizedJSON) {
		t.Fatalf("normalized JSON is invalid: %s", string(normalizedJSON))
	}
	if got := firstGrantToken(t, te); got != "ECHO_RPCS_ANY" {
		t.Fatalf("first grant = %q, want ECHO_RPCS_ANY", got)
	}
}

// This test used to be TestTokenEntitlementsFromValueAcceptsLegacyServiceMap and
// asserted that {"echo": ["ECHO_RPCS_ANY"]} produced a grant -- i.e. it asserted
// the catch-all sweep, which is the finding. That shape is what let data_grants
// and principal_scope leak into the grants claim, and no shipped policy emits it
// (every rego bundle in the tree returns grants/data_grants/azp/principal_scope
// by name). It is now refused along with every other unrecognized key.
func TestTokenEntitlementsFromValueRejectsLegacyServiceMap(t *testing.T) {
	_, _, err := tokenEntitlementsFromValue(map[string]any{
		"echo": []any{"ECHO_RPCS_ANY"},
	})
	if err == nil {
		t.Fatal("tokenEntitlementsFromValue accepted a legacy per-service map; want an error")
	}
	if !strings.Contains(err.Error(), "echo") {
		t.Fatalf("error %q does not name the unrecognized field", err)
	}
}

// The old version of this test asserted ANTHROPIC_ANY was present. It was --
// as a string in a hand-written list, for a service that had left the platform.
// The token was absent from the generated catalog and normalized to nothing, so
// the assertion certified a symbol that granted nothing. Assert the property
// that actually matters instead: every token this returns must be a real
// package wildcard the generated catalog knows and can expand.
func TestAllEntitlementsUseGeneratedGrantNames(t *testing.T) {
	tokens := allEntitlementGrantTokens()
	if len(tokens) == 0 {
		t.Fatal("allEntitlementGrantTokens() returned nothing")
	}
	for _, token := range tokens {
		g, ok := grants.Lookup(token)
		if !ok {
			t.Errorf("token %q is not in the generated grant catalog", token)
			continue
		}
		if g.Capability != "ANY" {
			t.Errorf("token %q is not a package-wide wildcard (capability %q)", token, g.Capability)
		}
		if len(grants.Expand(token)) == 0 {
			t.Errorf("token %q expands to no RPCs -- it grants nothing", token)
		}
	}
	// And none may be missing: every package in the catalog contributes its
	// wildcard. This used to name STRATUM_ANY (the wildcard every prod/user role
	// is minted with), but the catalog is whatever tree opa is generated from:
	// the whole fleet in a fleet build (grpc-servers: all), only its own
	// contract and depends in this repository's build since tacplus 2.4, where
	// stratum is not in the run. Completeness holds in both, and in a fleet
	// build it still demands STRATUM_ANY.
	for _, token := range grants.Tokens() {
		g, _ := grants.Lookup(token)
		want := strings.ToUpper(g.Package) + "_ANY"
		if !containsString(tokens, want) {
			t.Errorf("allEntitlementGrantTokens() missing %s (catalog has %s): %v", want, token, tokens)
		}
	}
}

func firstGrantToken(t *testing.T, te *entitlements_pb.TokenEntitlements) string {
	t.Helper()
	fd := te.ProtoReflect().Descriptor().Fields().ByName("grants")
	if fd == nil {
		t.Fatal("TokenEntitlements has no grants field")
	}
	grants := te.ProtoReflect().Get(fd).List()
	if grants.Len() != 1 {
		t.Fatalf("grant count = %d, want 1", grants.Len())
	}
	value := grants.Get(0)
	if fd.Kind() == protoreflect.StringKind {
		return value.String()
	}
	msg := value.Message()
	tokenFd := msg.Descriptor().Fields().ByName("token")
	if tokenFd == nil {
		t.Fatal("EntitlementGrant has no token field")
	}
	return msg.Get(tokenFd).String()
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func sameStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}
