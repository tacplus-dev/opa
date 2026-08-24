package internal

import (
	"encoding/json"
	"testing"

	"gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/grants"
	entitlements_pb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/generated/entitlements"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestCollectEntitlementTokensAcceptsFlatAndWrappedGrants(t *testing.T) {
	tokens, azp := collectEntitlementTokens(map[string]any{
		"grants": []any{
			"ECHO_RPCS_ANY",
			map[string]any{"token": "ANTHROPIC_RPCS_ANY"},
		},
		"azp": []any{"delegate-a"},
	})

	if want := []string{"ECHO_RPCS_ANY", "ANTHROPIC_RPCS_ANY"}; !sameStrings(tokens, want) {
		t.Fatalf("tokens = %v, want %v", tokens, want)
	}
	if want := []string{"delegate-a"}; !sameStrings(azp, want) {
		t.Fatalf("azp = %v, want %v", azp, want)
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

func TestTokenEntitlementsFromValueAcceptsLegacyServiceMap(t *testing.T) {
	te, _, err := tokenEntitlementsFromValue(map[string]any{
		"echo": []any{"ECHO_RPCS_ANY"},
	})
	if err != nil {
		t.Fatalf("tokenEntitlementsFromValue returned error: %v", err)
	}
	if got := firstGrantToken(t, te); got != "ECHO_RPCS_ANY" {
		t.Fatalf("first grant = %q, want ECHO_RPCS_ANY", got)
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
	// The wildcard every prod/user role is minted with must be among them.
	if !containsString(tokens, "STRATUM_ANY") {
		t.Errorf("allEntitlementGrantTokens() missing STRATUM_ANY: %v", tokens)
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
