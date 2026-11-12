package internal

import (
	"context"
	"encoding/json"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	fsPb "github.com/tacplus-dev/opa/tacplus/types/sys/filesystem"
	opaPb "github.com/tacplus-dev/opa/tacplus/types/sys/opalib"
	token_pb "github.com/tacplus-dev/opa/tacplus/types/sys/token"
	proxypolicy_pb "github.com/tacplus-dev/opa/tacplus/types/vservices/proxypolicy"
)

func loadProxyPolicy(t *testing.T, rego string) *Handler {
	t.Helper()
	h := NewHandler()
	if err := h.loadPolicy(&opaPb.OpaPolicy{
		EntryPoint: ProxyPolicyEntryPoint,
		Bundle: &fsPb.Directory{
			Files: []*fsPb.File{{Name: "main.rego", Content: []byte(rego)}},
		},
	}, ProxyPolicyEntryPoint); err != nil {
		t.Fatalf("loadPolicy: %v", err)
	}
	return h
}

// OPA hands numbers back as json.Number, which parseTokenUse ignored (#13): a
// policy returning the documented integer form, token_use: 6, minted
// TOKEN_USE_UNSPECIFIED. These evaluate real policies, not Go values.
func TestEvalProxyPolicyTokenUseFromPolicy(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tokenUse string // rego literal
	}{
		{"integer", `6`},
		{"integer as float", `6.0`},
		{"full name", `"TOKEN_USE_EXTERNAL"`},
		{"short name", `"EXTERNAL"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := loadProxyPolicy(t, "package proxypolicy\n\neval := {\"allow\": true, \"token_use\": "+tc.tokenUse+"}\n")
			resp, err := h.EvalProxyPolicy(context.Background(), nil, &proxypolicy_pb.ProxyPolicyEvalInput{})
			if err != nil {
				t.Fatalf("EvalProxyPolicy: %v", err)
			}
			if got := resp.GetTokenUse(); got != token_pb.TokenUse_TOKEN_USE_EXTERNAL {
				t.Errorf("token_use %s: TokenUse = %v, want TOKEN_USE_EXTERNAL", tc.tokenUse, got)
			}
		})
	}
}

// An unrecognised token_use is an error, not a silent TOKEN_USE_UNSPECIFIED:
// the RPC fails closed with codes.Internal, as for a non-object result.
func TestEvalProxyPolicyRejectsUnrecognisedTokenUse(t *testing.T) {
	for _, tc := range []struct {
		name     string
		tokenUse string // rego literal
	}{
		{"over int32 (2^32 + 6)", `4294967302`},
		{"under int32", `-4294967290`},
		{"fraction", `6.5`},
		{"undefined value", `9999`},
		{"unknown name", `"TOKEN_USE_NOPE"`},
		{"empty string", `""`},
		{"bool", `true`},
		{"null", `null`},
		{"array", `[6]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := loadProxyPolicy(t, "package proxypolicy\n\neval := {\"allow\": true, \"token_use\": "+tc.tokenUse+"}\n")
			resp, err := h.EvalProxyPolicy(context.Background(), nil, &proxypolicy_pb.ProxyPolicyEvalInput{})
			if err == nil {
				t.Fatalf("token_use %s: EvalProxyPolicy returned OK with TokenUse = %v; want an error", tc.tokenUse, resp.GetTokenUse())
			}
			if resp != nil {
				t.Errorf("token_use %s: non-nil response alongside the error: %v", tc.tokenUse, resp)
			}
			if got := status.Code(err); got != codes.Internal {
				t.Errorf("token_use %s: status code = %v, want %v", tc.tokenUse, got, codes.Internal)
			}
		})
	}
}

// Leaving token_use out is still allowed and means TOKEN_USE_UNSPECIFIED.
func TestEvalProxyPolicyTokenUseUnset(t *testing.T) {
	h := loadProxyPolicy(t, "package proxypolicy\n\neval := {\"allow\": true}\n")
	resp, err := h.EvalProxyPolicy(context.Background(), nil, &proxypolicy_pb.ProxyPolicyEvalInput{})
	if err != nil {
		t.Fatalf("EvalProxyPolicy: %v", err)
	}
	if got := resp.GetTokenUse(); got != token_pb.TokenUse_TOKEN_USE_UNSPECIFIED {
		t.Errorf("TokenUse = %v, want TOKEN_USE_UNSPECIFIED", got)
	}
}

// Go numbers (callers other than OPA) get the same checks: an integer in int32
// range that is a defined TokenUse, nothing truncated (#12).
func TestParseTokenUseValues(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  interface{}
		ok   bool
	}{
		{"json.Number", json.Number("6"), true},
		{"json.Number float form", json.Number("6.0"), true},
		{"int", int(6), true},
		{"int64", int64(6), true},
		{"float64", float64(6), true},
		{"json.Number over int32", json.Number("4294967302"), false},
		{"json.Number fraction", json.Number("6.5"), false},
		{"json.Number undefined", json.Number("9999"), false},
		{"int64 over int32", int64(1<<32 + 6), false},
		{"int64 under int32", int64(-1<<32 + 6), false},
		{"float64 over int32", float64(1<<32 + 6), false},
		{"float64 fraction", 6.5, false},
		{"int64 undefined", int64(9999), false},
		{"float64 undefined", float64(9999), false},
		{"nil", nil, false},
	} {
		got, err := parseTokenUse(tc.raw)
		switch {
		case tc.ok && (err != nil || got != token_pb.TokenUse_TOKEN_USE_EXTERNAL):
			t.Errorf("%s: parseTokenUse(%v) = %v, %v; want TOKEN_USE_EXTERNAL, nil", tc.name, tc.raw, got, err)
		case !tc.ok && err == nil:
			t.Errorf("%s: parseTokenUse(%v) = %v, nil; want an error", tc.name, tc.raw, got)
		}
	}
}
