package internal

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	structpb "google.golang.org/protobuf/types/known/structpb"

	"gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/chassis/logger"
	fsPb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/sys/filesystem"
	opaPb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/sys/opalib"
	userentitlements_pb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/vservices/userentitlements"
)

// EvalEntitlement is handed the caller's IDP claims -- sub, email, groups --
// and used to log the whole input map with zap.Any at INFO. The chassis logger
// this service runs on redacts CREDENTIALS at the sink (authorization, cookie,
// *-token, *-secret, Bearer) and nothing else, so identity claims were written
// to the log sink in the clear on every entitlement evaluation.
//
// The capture below is the production log path: the same JSON encoder, with the
// encoded bytes then passed through logger.RedactCredentials -- which is what
// the chassis's redactingWriter applies to every byte the encoder emits. Any
// claim value that survives that call is a value that reaches the real sink.
func TestEvalEntitlementDoesNotLogClaimValues(t *testing.T) {
	const (
		sub   = "f47ac10b-58cc-4372-a567-0e02b2c3d479"
		email = "joeuser@drpepper.example.com"
		group = "drpepper-admins"
	)

	h := NewHandler()
	if err := h.loadPolicy(&opaPb.OpaPolicy{
		EntryPoint: EntitlementsEntryPoint,
		Bundle: &fsPb.Directory{
			Files: []*fsPb.File{{
				Name: "main.rego",
				Content: []byte(`package entitlements

eval := {"token_entitlements": {"grants": ["ECHO_RPCS_ANY"]}}
`),
			}},
		},
	}, EntitlementsEntryPoint); err != nil {
		t.Fatalf("loadPolicy: %v", err)
	}

	claims, err := structpb.NewStruct(map[string]any{
		"sub":    sub,
		"email":  email,
		"groups": []any{group},
	})
	if err != nil {
		t.Fatalf("structpb.NewStruct: %v", err)
	}

	var buf bytes.Buffer
	restore := zap.ReplaceGlobals(zap.New(zapcore.NewCore(
		zapcore.NewJSONEncoder(zap.NewProductionEncoderConfig()),
		zapcore.AddSync(&buf),
		zapcore.InfoLevel,
	)))

	_, err = h.EvalEntitlement(context.Background(), nil, &userentitlements_pb.EntitlementEvalInput{
		Claims:    claims,
		Namespace: "/tenants/drpepper",
	})
	restore()
	if err != nil {
		t.Fatalf("EvalEntitlement: %v", err)
	}

	out := logger.RedactCredentials(buf.String())

	for name, value := range map[string]string{"sub": sub, "email": email, "groups": group} {
		if strings.Contains(out, value) {
			t.Errorf("SECURITY: %s claim value %q reached the log sink:\n%s", name, value, out)
		}
	}

	// The log line has to stay useful: which claims the IDP sent, and the
	// tenant the policy ran for, are the diagnostics -- the values are not.
	for _, want := range []string{`"claim_keys":["email","groups","sub"]`, `"namespace":"/tenants/drpepper"`} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %s in log output:\n%s", want, out)
		}
	}
}
