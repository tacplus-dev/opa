package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	"gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/libs/servicemanager"
	uncleopa_fe "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/s2sm/grpc/servers/generated/opa_eval"
	entitlements_pb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/generated/entitlements"
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
// The policy returns a TokenEntitlements object directly in the
// "token_entitlements" key of the eval result. Kernel embeds it
// in the JWT service_entitlements claim without further expansion.
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

	teRaw, ok := obj["token_entitlements"]
	if !ok {
		zap.L().Warn("EvalEntitlement: no token_entitlements in result, returning empty entitlements")
		return resp, nil
	}

	teJSON, err := json.Marshal(teRaw)
	if err != nil {
		zap.L().Warn("EvalEntitlement: failed to marshal token_entitlements", zap.Error(err))
		return resp, nil
	}

	te := &entitlements_pb.TokenEntitlements{}
	if err := protojson.Unmarshal(teJSON, te); err != nil {
		zap.L().Warn("EvalEntitlement: failed to unmarshal token_entitlements",
			zap.Error(err), zap.String("json", string(teJSON)))
		return resp, nil
	}

	resp.TokenEntitlements = te

	zap.L().Info("EvalEntitlement response", zap.String("token_entitlements", string(teJSON)))

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
