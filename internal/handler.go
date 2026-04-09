package internal

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/libs/servicemanager"
	uncleopa_fe "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/s2sm/grpc/servers/opa/userentitlements"
	opaPb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/libs/opa"
	x509Pb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/libs/x509"
	userentitlements_pb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/opa/userentitlements"
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

// EvalEntitlement implements OpaEvalHandler — evaluates entitlement profiles
// from IDP claims via the loaded OPA policy.
func (h *Handler) EvalEntitlement(ctx context.Context, caller *servicemanager.Caller, req *userentitlements_pb.EntitlementEvalInput) (*userentitlements_pb.EntitlementEvalResult, error) {
	eval, err := h.getEvaluator("data.entitlements.profiles")
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "no policy loaded: %v", err)
	}

	input := make(map[string]any)
	for k, v := range req.GetClaims() {
		input[k] = v
	}

	result, err := eval.Evaluate(input)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "OPA eval: %v", err)
	}

	var profiles []string
	if list, ok := result.([]interface{}); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				profiles = append(profiles, s)
			}
		}
	}

	return &userentitlements_pb.EntitlementEvalResult{Profiles: profiles}, nil
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
