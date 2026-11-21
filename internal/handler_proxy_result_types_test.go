package internal

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	token_pb "github.com/tacplus-dev/opa/tacplus/types/sys/token"
	proxypolicy_pb "github.com/tacplus-dev/opa/tacplus/types/vservices/proxypolicy"
)

// A proxy-policy result field of the wrong type fails the call (#19), as an
// unrecognised token_use does (#13): skipping it minted a token without the
// audience, claims or credential format the policy wrote. The error names the
// field, so an operator sees which one the policy got wrong.
func TestEvalProxyPolicyRejectsWrongTypedField(t *testing.T) {
	for _, tc := range []struct {
		field string
		value string // rego literal
	}{
		{"allow", `"true"`},
		{"allow", `1`},
		{"allow", `null`},
		{"url", `1`},
		{"url", `["https://example.com"]`},
		{"url", `null`},
		{"reason", `true`},
		{"reason", `{"why": "no"}`},
		{"append_aud", `1`},
		{"append_aud", `{"aud": "client-id"}`},
		{"append_aud", `["client-id", 1]`},
		{"append_aud", `["client-id", null]`},
		{"append_aud", `null`},
		{"extra_claims", `["federation_rule_id"]`},
		{"extra_claims", `"federation_rule_id"`},
		{"extra_claims", `null`},
		{"auth_type", `"saml"`},
		{"auth_type", `"TOKEN_TYPE_SAML"`},
		{"auth_type", `2`},
		{"auth_type", `1.5`},
		{"auth_type", `""`},
		{"auth_type", `true`},
		{"auth_type", `null`},
	} {
		t.Run(tc.field+"="+tc.value, func(t *testing.T) {
			members := `"allow": true, "` + tc.field + `": ` + tc.value
			if tc.field == "allow" {
				members = `"allow": ` + tc.value
			}
			h := loadProxyPolicy(t, "package proxypolicy\n\neval := {"+members+"}\n")
			resp, err := h.EvalProxyPolicy(context.Background(), nil, &proxypolicy_pb.ProxyPolicyEvalInput{})
			if err == nil {
				t.Fatalf("%s: %s: EvalProxyPolicy returned OK (%v); want an error", tc.field, tc.value, resp)
			}
			if resp != nil {
				t.Errorf("%s: %s: non-nil response alongside the error: %v", tc.field, tc.value, resp)
			}
			if got := status.Code(err); got != codes.Internal {
				t.Errorf("%s: %s: status code = %v, want %v", tc.field, tc.value, got, codes.Internal)
			}
			if !strings.Contains(status.Convert(err).Message(), tc.field) {
				t.Errorf("%s: %s: error %q does not name the field", tc.field, tc.value, status.Convert(err).Message())
			}
		})
	}
}

// Every field of the right type reaches the response, auth_type included: it
// was never read, so a policy's credential format was dropped silently too.
func TestEvalProxyPolicyResultFields(t *testing.T) {
	h := loadProxyPolicy(t, `package proxypolicy

eval := {
	"allow": true,
	"url": "https://api.example.com",
	"reason": "ok",
	"append_aud": ["client-id", ""],
	"extra_claims": {"federation_rule_id": "fr-1", "n": 2, "nested": {"list": [1, "a"]}},
	"auth_type": "TOKEN_TYPE_JWT"
}
`)
	resp, err := h.EvalProxyPolicy(context.Background(), nil, &proxypolicy_pb.ProxyPolicyEvalInput{})
	if err != nil {
		t.Fatalf("EvalProxyPolicy: %v", err)
	}
	if !resp.GetAllow() {
		t.Error("allow = false, want true")
	}
	if got := resp.GetUrl(); got != "https://api.example.com" {
		t.Errorf("url = %q", got)
	}
	if got := resp.GetReason(); got != "ok" {
		t.Errorf("reason = %q", got)
	}
	// An empty audience adds nothing, so it is left out rather than refused.
	if got := resp.GetAppendAud(); len(got) != 1 || got[0] != "client-id" {
		t.Errorf("append_aud = %v, want [client-id]", got)
	}
	claims := resp.GetExtraClaims().AsMap()
	if claims["federation_rule_id"] != "fr-1" || claims["n"] != float64(2) {
		t.Errorf("extra_claims = %v", claims)
	}
	if nested, ok := claims["nested"].(map[string]any); !ok || len(nested["list"].([]any)) != 2 {
		t.Errorf("extra_claims.nested = %v", claims["nested"])
	}
	if got := resp.GetAuthType(); got != token_pb.TokenType_TOKEN_TYPE_JWT {
		t.Errorf("auth_type = %v, want TOKEN_TYPE_JWT", got)
	}
}

// append_aud tolerates a bare string for a single audience, and auth_type takes
// the forms token_use does: the full name, the short name or the number.
func TestEvalProxyPolicyResultFieldForms(t *testing.T) {
	for _, tc := range []struct {
		name      string
		fields    string // rego object members
		appendAud []string
		authType  token_pb.TokenType
	}{
		{"bare append_aud", `"append_aud": "client-id"`, []string{"client-id"}, token_pb.TokenType_TOKEN_TYPE_UNDEFINED},
		{"auth_type short name", `"auth_type": "SSH"`, nil, token_pb.TokenType_TOKEN_TYPE_SSH},
		{"auth_type number", `"auth_type": 1`, nil, token_pb.TokenType_TOKEN_TYPE_JWT},
		{"auth_type undefined by name", `"auth_type": "TOKEN_TYPE_UNDEFINED"`, nil, token_pb.TokenType_TOKEN_TYPE_UNDEFINED},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := loadProxyPolicy(t, "package proxypolicy\n\neval := {\"allow\": true, "+tc.fields+"}\n")
			resp, err := h.EvalProxyPolicy(context.Background(), nil, &proxypolicy_pb.ProxyPolicyEvalInput{})
			if err != nil {
				t.Fatalf("EvalProxyPolicy: %v", err)
			}
			if got := resp.GetAppendAud(); strings.Join(got, ",") != strings.Join(tc.appendAud, ",") {
				t.Errorf("append_aud = %v, want %v", got, tc.appendAud)
			}
			if got := resp.GetAuthType(); got != tc.authType {
				t.Errorf("auth_type = %v, want %v", got, tc.authType)
			}
		})
	}
}
