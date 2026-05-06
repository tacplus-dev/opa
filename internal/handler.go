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

	"gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/libs/servicemanager"
	backendtoken_fe "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/s2sm/grpc/servers/vservices/backendtoken"
	userentitlements_fe "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/s2sm/grpc/servers/vservices/userentitlements"
	entitlements_pb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/generated/entitlements"
	opaPb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/libs/opa"
	x509Pb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/libs/x509"
	backendtoken_pb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/vservices/backendtoken"
	userentitlements_pb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/vservices/userentitlements"
	pb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/services/uncleopa"
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

// LoadBootstrapPolicy loads an OPA policy from the bootstrap config file.
// This is used to load policies that the kernel may not yet know about
// (e.g., fields added to the proto after the kernel image was built).
// Kernel-provided policies will override this if they are non-nil.
func (h *Handler) LoadBootstrapPolicy(policy *opaPb.OpaPolicy) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.loadPolicy(policy); err != nil {
		zap.L().Warn("failed to load bootstrap OPA policy", zap.String("entry_point", policy.GetEntryPoint()), zap.Error(err))
	}
}

// BackendtokenOpaEvalGrpcImpl returns this handler as the backendtoken.OpaEval implementation.
func (h *Handler) BackendtokenOpaEvalGrpcImpl() backendtoken_fe.OpaEvalHandler {
	return h
}

// UserentitlementsOpaEvalGrpcImpl returns this handler as the userentitlements.OpaEval implementation.
func (h *Handler) UserentitlementsOpaEvalGrpcImpl() userentitlements_fe.OpaEvalHandler {
	return h
}

// BackendtokenOpaEvalConfig is called by the SM with initial config and on every update.
// Loads the backend_token_policy OPA bundle.
func (h *Handler) BackendtokenOpaEvalConfig(cfg *pb.Config) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if policy := cfg.GetBackendTokenPolicy(); policy != nil {
		if err := h.loadPolicy(policy); err != nil {
			return fmt.Errorf("backend token policy: %w", err)
		}
	}

	return nil
}

// UserentitlementsOpaEvalConfig is called by the SM with initial config and on every update.
// Loads the entitlements_policy OPA bundle.
func (h *Handler) UserentitlementsOpaEvalConfig(cfg *pb.Config) error {
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

// EvalEntitlement implements userentitlements_fe.OpaEvalHandler — evaluates entitlements
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

	var claims map[string]interface{}
	if s := req.GetClaims(); s != nil {
		claims = s.AsMap()
	} else {
		claims = make(map[string]interface{})
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
	unmarshalOpts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := unmarshalOpts.Unmarshal(teJSON, te); err != nil {
		zap.L().Warn("EvalEntitlement: failed to unmarshal token_entitlements",
			zap.Error(err), zap.String("json", string(teJSON)))
		return resp, nil
	}

	resp.TokenEntitlements = te

	zap.L().Info("EvalEntitlement response", zap.String("token_entitlements", string(teJSON)))

	return resp, nil
}

// EvalBackendToken implements backendtoken_fe.OpaEvalHandler — evaluates the
// backend API token for the given caller claims and provider via the loaded
// backend_token OPA policy.
func (h *Handler) EvalBackendToken(ctx context.Context, caller *servicemanager.Caller, req *backendtoken_pb.BackendTokenEvalInput) (*backendtoken_pb.BackendTokenEvalResult, error) {
	eval, err := h.getEvaluator("data.backend_token.eval")
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "no backend token policy loaded: %v", err)
	}

	var claims map[string]interface{}
	if s := req.GetClaims(); s != nil {
		claims = s.AsMap()
	} else {
		claims = make(map[string]interface{})
	}
	input := map[string]any{
		"claims":   claims,
		"provider": req.GetProvider(),
	}

	zap.L().Info("EvalBackendToken", zap.String("provider", req.GetProvider()))

	result, err := eval.Evaluate(input)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "OPA eval: %v", err)
	}

	obj, ok := result.(map[string]interface{})
	if !ok {
		return nil, status.Errorf(codes.Internal, "backend token policy returned non-object result")
	}

	tokenRaw, ok := obj["token"]
	if !ok {
		return nil, status.Errorf(codes.PermissionDenied, "backend token policy returned no token")
	}

	token, ok := tokenRaw.(string)
	if !ok || token == "" {
		return nil, status.Errorf(codes.PermissionDenied, "backend token policy returned empty token")
	}

	resp := &backendtoken_pb.BackendTokenEvalResult{Token: token}

	if expRaw, ok := obj["expires_at_secs"]; ok {
		switch v := expRaw.(type) {
		case float64:
			resp.ExpiresAtSecs = int64(v)
		case int64:
			resp.ExpiresAtSecs = v
		case json.Number:
			if i, err := v.Int64(); err == nil {
				resp.ExpiresAtSecs = i
			}
		}
	}

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


