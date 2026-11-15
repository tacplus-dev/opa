package internal

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/tacplus-dev/opa/tacplus/types/services/opa"
	fsPb "github.com/tacplus-dev/opa/tacplus/types/sys/filesystem"
	opaPb "github.com/tacplus-dev/opa/tacplus/types/sys/opalib"
	proxypolicy_pb "github.com/tacplus-dev/opa/tacplus/types/vservices/proxypolicy"
	userentitlements_pb "github.com/tacplus-dev/opa/tacplus/types/vservices/userentitlements"
)

// An entitlement or proxy decision is a function of its input. A bundle that
// calls a network built-in could reach anything opa's pod can (an SSRF from a
// policy edit) and stall kernel's sign-in on a slow host, so it is refused when
// it is pushed, and the policy already loaded stays in force.
func TestPushRefusesBundleCallingNetworkBuiltins(t *testing.T) {
	for builtin, body := range map[string]string{
		"http.send":          `eval := http.send({"method": "GET", "url": "http://169.254.169.254/"})`,
		"net.lookup_ip_addr": `eval := net.lookup_ip_addr("kernel.system.svc")`,
	} {
		t.Run(builtin, func(t *testing.T) {
			h := NewHandler()
			if err := h.UserentitlementsOpaEvalConfig(configWithEntitlementsPolicy(entitlementsPolicyReturning(EntitlementsEntryPoint, "ECHO_RPCS_ANY"))); err != nil {
				t.Fatalf("initial push: %v", err)
			}

			err := h.UserentitlementsOpaEvalConfig(configWithEntitlementsPolicy(&opaPb.OpaPolicy{
				EntryPoint: EntitlementsEntryPoint,
				Bundle: &fsPb.Directory{
					Files: []*fsPb.File{{Name: "main.rego", Content: []byte("package entitlements\n\n" + body + "\n")}},
				},
			}))
			if err == nil {
				t.Fatalf("push of a bundle calling %s accepted; want it refused", builtin)
			}
			if !strings.Contains(err.Error(), builtin) {
				t.Errorf("error %q does not name %s", err, builtin)
			}

			resp, err := h.EvalEntitlement(context.Background(), nil, &userentitlements_pb.EntitlementEvalInput{})
			if err != nil {
				t.Fatalf("EvalEntitlement after the refused push: %v", err)
			}
			if got := resp.GetTokenEntitlements().GetGrants(); len(got) != 1 {
				t.Errorf("grants = %v, want the previously loaded policy's ECHO_RPCS_ANY", got)
			}
		})
	}
}

// slowRego never finishes in a test's lifetime: 10^10 iterations. Only the
// RPC's context can stop it.
const slowRego = `
eval := {"n": count([1 | some i in numbers.range(1, 100000); some j in numbers.range(1, 100000)])}
`

// Both handlers evaluate on the RPC's context: an evaluation still running at
// the RPC's deadline stops there and returns DEADLINE_EXCEEDED, and one whose
// caller went away returns CANCELED, instead of holding the RPC indefinitely
// on context.Background().
func TestEvaluationPastTheRPCDeadlineReturnsDeadlineExceeded(t *testing.T) {
	h := NewHandler()
	if err := h.loadPolicy(&opaPb.OpaPolicy{
		EntryPoint: EntitlementsEntryPoint,
		Bundle:     &fsPb.Directory{Files: []*fsPb.File{{Name: "main.rego", Content: []byte("package entitlements\n" + slowRego)}}},
	}, EntitlementsEntryPoint); err != nil {
		t.Fatalf("load entitlements policy: %v", err)
	}
	if err := h.ProxypolicyOpaEvalConfig(&pb.Config{ProxyPolicy: &opaPb.OpaPolicy{
		EntryPoint: ProxyPolicyEntryPoint,
		Bundle:     &fsPb.Directory{Files: []*fsPb.File{{Name: "main.rego", Content: []byte("package proxypolicy\n" + slowRego)}}},
	}}); err != nil {
		t.Fatalf("load proxy policy: %v", err)
	}

	rpcs := map[string]func(context.Context) error{
		"EvalEntitlement": func(ctx context.Context) error {
			_, err := h.EvalEntitlement(ctx, nil, &userentitlements_pb.EntitlementEvalInput{})
			return err
		},
		"EvalProxyPolicy": func(ctx context.Context) error {
			_, err := h.EvalProxyPolicy(ctx, nil, &proxypolicy_pb.ProxyPolicyEvalInput{})
			return err
		},
	}
	for name, rpc := range rpcs {
		t.Run(name+"/deadline", func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			start := time.Now()
			err := rpc(ctx)
			if got := status.Code(err); got != codes.DeadlineExceeded {
				t.Fatalf("status = %v (%v), want %v", got, err, codes.DeadlineExceeded)
			}
			if elapsed := time.Since(start); elapsed > 5*time.Second {
				t.Errorf("returned %v after the deadline; the evaluation was not stopped at it", elapsed)
			}
		})
		t.Run(name+"/cancelled", func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			time.AfterFunc(50*time.Millisecond, cancel)
			if got := status.Code(rpc(ctx)); got != codes.Canceled {
				t.Fatalf("status = %v, want %v", got, codes.Canceled)
			}
		})
	}
}
