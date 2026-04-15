package internal

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/libs/servicemanager"
	uncleopa_fe "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/s2sm/grpc/servers/generated/opa_eval"
	opaPb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/libs/opa"
	x509Pb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/libs/x509"
	userentitlements_pb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/vservices/userentitlements"
	pb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/services/uncleopa"
)

// Handler implements the service-manager Handler interface for uncleopa.
// It manages OPA policy evaluators, reloading them when state updates
// deliver new policy bundles.
type Handler struct {
	mu          sync.RWMutex
	evaluators  map[string]*PolicyEvaluator // keyed by entry_point
}

// NewHandler creates a new Handler.
func NewHandler() *Handler {
	return &Handler{
		evaluators: make(map[string]*PolicyEvaluator),
	}
}

// OpaEvalGrpcImpl returns this handler as the OpaEval gRPC implementation.
func (h *Handler) OpaEvalGrpcImpl() uncleopa_fe.OpaEvalHandler {
	return h
}

// OpaEvalConfig is called by the SM with initial config and on every update.
// OPA policy bundles are delivered via config (not state).
func (h *Handler) OpaEvalConfig(cfg *pb.Config) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if policy := cfg.GetEntitlementsPolicy(); policy != nil {
		if err := h.loadPolicy(policy); err != nil {
			return fmt.Errorf("entitlements policy: %w", err)
		}
	}

	return nil
}

func (h *Handler) NewTlsCert(certChain *x509Pb.X509CertChain, privateKeyDER []byte) error {
	return nil
}

func (h *Handler) Shutdown(ctx context.Context) {}

// EvalEntitlement implements OpaEvalHandler — evaluates entitlements
// from IDP claims via the loaded OPA policy.
//
// The policy returns a structured EntitlementEvalResult with:
//   - grant_all: bool (grant every RPC on every service)
//   - grant_all_<service>: bool (grant every RPC on that service)
//   - entitlements: object (explicit per-service RPC lists)
//
// Kernel expands grant_all flags and merges with explicit entitlements
// before embedding in the token.
func (h *Handler) EvalEntitlement(ctx context.Context, caller *servicemanager.Caller, req *userentitlements_pb.EntitlementEvalInput) (*userentitlements_pb.EntitlementEvalResult, error) {
	eval, err := h.getEvaluator("data.entitlements.eval")
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "no policy loaded: %v", err)
	}

	claims := make(map[string]any)
	for k, v := range req.GetClaims() {
		claims[k] = v
	}
	input := map[string]any{"claims": claims}

	zap.L().Info("EvalEntitlement input", zap.Any("input", input))

	result, err := eval.Evaluate(input)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "OPA eval: %v", err)
	}

	zap.L().Info("EvalEntitlement result", zap.Any("result", result), zap.String("type", fmt.Sprintf("%T", result)))

	resp := &userentitlements_pb.EntitlementEvalResult{}

	obj, ok := result.(map[string]interface{})
	if !ok {
		zap.L().Warn("EvalEntitlement: result is not an object, returning empty entitlements")
		return resp, nil
	}

	if v, ok := obj["grant_all"].(bool); ok {
		resp.GrantAll = v
	}
	if v, ok := obj["grant_all_sentinel"].(bool); ok {
		resp.GrantAllSentinel = v
	}
	if v, ok := obj["grant_all_gateway"].(bool); ok {
		resp.GrantAllGateway = v
	}
	if v, ok := obj["grant_all_mcp"].(bool); ok {
		resp.GrantAllMcp = v
	}
	if v, ok := obj["grant_all_eventbus"].(bool); ok {
		resp.GrantAllEventbus = v
	}
	if v, ok := obj["grant_all_fleetwood"].(bool); ok {
		resp.GrantAllFleetwood = v
	}
	if v, ok := obj["grant_all_anthropic"].(bool); ok {
		resp.GrantAllAnthropic = v
	}
	if v, ok := obj["grant_all_github"].(bool); ok {
		resp.GrantAllGithub = v
	}
	if v, ok := obj["grant_all_gitlab"].(bool); ok {
		resp.GrantAllGitlab = v
	}
	if v, ok := obj["grant_all_openai"].(bool); ok {
		resp.GrantAllOpenai = v
	}

	// Explicit entitlements are passed through as-is. Kernel handles
	// the expansion and merge with grant_all flags.
	// The OPA policy can return per-service RPC lists in the entitlements
	// field if it needs fine-grained control beyond grant_all.

	zap.L().Info("EvalEntitlement response",
		zap.Bool("grant_all", resp.GrantAll),
		zap.Bool("grant_all_sentinel", resp.GrantAllSentinel),
		zap.Bool("grant_all_gateway", resp.GrantAllGateway))

	return resp, nil
}

// loadPolicy creates or replaces a PolicyEvaluator for the given policy.
func (h *Handler) loadPolicy(policy *opaPb.OpaPolicy) error {
	ep := policy.GetEntryPoint()
	if ep == "" {
		return fmt.Errorf("policy missing entry_point")
	}

	eval, err := NewPolicyEvaluator(policy)
	if err != nil {
		return err
	}

	h.evaluators[ep] = eval
	zap.L().Info("loaded OPA policy", zap.String("entry_point", ep))
	return nil
}

// getEvaluator returns the PolicyEvaluator for the given entry_point.
func (h *Handler) getEvaluator(entryPoint string) (*PolicyEvaluator, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	eval, ok := h.evaluators[entryPoint]
	if !ok {
		return nil, fmt.Errorf("no policy loaded for entry_point %q", entryPoint)
	}
	return eval, nil
}
