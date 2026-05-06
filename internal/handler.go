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
	proxypolicy_fe "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/s2sm/grpc/servers/vservices/proxypolicy"
	userentitlements_fe "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/s2sm/grpc/servers/vservices/userentitlements"
	entitlements_pb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/generated/entitlements"
	opaPb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/libs/opa"
	x509Pb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/libs/x509"
	proxypolicy_pb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/vservices/proxypolicy"
	userentitlements_pb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/vservices/userentitlements"
	pb "gitlab.com/secondstarcavalier/schematic/platform/services/uncleopa/tacplus/types/services/uncleopa"
)

// Handler implements the service-manager Handler interface for uncleopa.
// It manages OPA policy evaluators, reloading them when state updates
// deliver new policy bundles.
type Handler struct {
	mu         sync.RWMutex
	evaluators map[string]*PolicyEvaluator // keyed by entry_point
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

// ProxypolicyOpaEvalGrpcImpl returns this handler as the proxypolicy.OpaEval implementation.
func (h *Handler) ProxypolicyOpaEvalGrpcImpl() proxypolicy_fe.OpaEvalHandler {
	return h
}

// UserentitlementsOpaEvalGrpcImpl returns this handler as the userentitlements.OpaEval implementation.
func (h *Handler) UserentitlementsOpaEvalGrpcImpl() userentitlements_fe.OpaEvalHandler {
	return h
}

// ProxypolicyOpaEvalConfig is called by the SM with initial config and on every update.
// Loads the proxy_policy OPA bundle.
func (h *Handler) ProxypolicyOpaEvalConfig(cfg *pb.Config) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if policy := cfg.GetProxyPolicy(); policy != nil {
		if err := h.loadPolicy(policy); err != nil {
			return fmt.Errorf("proxy policy: %w", err)
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

// EvalProxyPolicy implements proxypolicy_fe.OpaEvalHandler — evaluates the
// proxy policy for the given caller claims and routing metadata.
//
// The policy receives {claims, metadata} and returns:
//   - allow: bool — whether the request is permitted
//   - url: string — the resolved destination URL (empty = use service default)
//   - reason: string — denial reason (when allow = false)
//   - aud: string — optional audience override for the minted token
//   - extra_claims: object — additional JWT claims to inject
//   - auth_type: string — credential format ("jwt" or "saml")
func (h *Handler) EvalProxyPolicy(ctx context.Context, caller *servicemanager.Caller, req *proxypolicy_pb.ProxyPolicyEvalInput) (*proxypolicy_pb.ProxyPolicyEvalResult, error) {
	eval, err := h.getEvaluator("data.proxypolicy.eval")
	if err != nil {
		return nil, status.Errorf(codes.FailedPrecondition, "no proxy policy loaded: %v", err)
	}

	var claims map[string]interface{}
	if s := req.GetClaims(); s != nil {
		claims = s.AsMap()
	} else {
		claims = make(map[string]interface{})
	}
	input := map[string]any{
		"claims":   claims,
		"metadata": req.GetMetadata(),
	}

	zap.L().Info("EvalProxyPolicy", zap.String("metadata", req.GetMetadata()))

	result, err := eval.Evaluate(input)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "OPA eval: %v", err)
	}

	obj, ok := result.(map[string]interface{})
	if !ok {
		return nil, status.Errorf(codes.Internal, "proxy policy returned non-object result")
	}

	resp := &proxypolicy_pb.ProxyPolicyEvalResult{}

	if allowRaw, ok := obj["allow"]; ok {
		if allow, ok := allowRaw.(bool); ok {
			resp.Allow = allow
		}
	}

	if reasonRaw, ok := obj["reason"]; ok {
		if reason, ok := reasonRaw.(string); ok {
			resp.Reason = reason
		}
	}

	if urlRaw, ok := obj["url"]; ok {
		if u, ok := urlRaw.(string); ok {
			resp.Url = u
		}
	}

	if audRaw, ok := obj["aud"]; ok {
		if aud, ok := audRaw.(string); ok {
			resp.Aud = aud
		}
	}

	zap.L().Info("EvalProxyPolicy result",
		zap.Bool("allow", resp.Allow),
		zap.String("url", resp.Url),
		zap.String("reason", resp.Reason))

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
