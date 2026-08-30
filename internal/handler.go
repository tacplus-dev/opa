package internal

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"sync"

	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	structpb "google.golang.org/protobuf/types/known/structpb"

	"gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/chassis/servicemanager"
	"gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/grants"
	proxypolicy_fe "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/s2sm/grpc/servers/vservices/proxypolicy"
	userentitlements_fe "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/s2sm/grpc/servers/vservices/userentitlements"
	entitlements_pb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/generated/entitlements"
	pb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/services/opa"
	opaPb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/sys/opalib"
	token_pb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/sys/token"
	x509Pb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/sys/x509"
	proxypolicy_pb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/vservices/proxypolicy"
	userentitlements_pb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/vservices/userentitlements"
)

// Handler implements the service-manager Handler interface for opa.
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
	// namespace is the caller's tenant namespace (v3: IDP+OPA at the tenant).
	// Forward it so the policy can branch on tenant when computing entitlements,
	// authorized projects, and the default project.
	input := map[string]any{"claims": claims, "namespace": req.GetNamespace()}

	// Log the claim KEY NAMES, never the claim map. `input` carries the IDP
	// claims verbatim -- sub, email, groups -- and zap.Any encodes every one of
	// them into the log line at INFO. The chassis logger this service already
	// runs on (logger.InstallFromConfig, wired in by grpcserver) redacts
	// CREDENTIALS at the sink -- authorization, cookie, *-token, *-secret,
	// Bearer -- and nothing else, so identity claims pass straight through it.
	// The key names carry the diagnostic value here (which claims the IDP sent,
	// which the policy had to work with); the values are the PII.
	claimKeys := make([]string, 0, len(claims))
	for k := range claims {
		claimKeys = append(claimKeys, k)
	}
	sort.Strings(claimKeys)
	zap.L().Info("EvalEntitlement input",
		zap.String("namespace", req.GetNamespace()),
		zap.Strings("claim_keys", claimKeys))

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

	// namespace: the authorization-tree position the policy assigns to this
	// caller (e.g. "/local/dev/drpepper"). Kernel sets it as the JWT namespace
	// claim; empty means fall back to the cluster-root namespace. Set it before
	// any early return so it is carried regardless of the entitlement path.
	if nsRaw, ok := obj["namespace"]; ok {
		if ns, ok := nsRaw.(string); ok {
			resp.Namespace = ns
		}
	}

	// authorized_projects + default_project (v3): the project leaf names the
	// caller may obtain an access token for, and the one to scope to at login.
	// Set before any early return so they are carried on every entitlement path.
	if apRaw, ok := obj["authorized_projects"]; ok {
		if apList, ok := apRaw.([]interface{}); ok {
			for _, p := range apList {
				if ps, ok := p.(string); ok {
					resp.AuthorizedProjects = append(resp.AuthorizedProjects, ps)
				}
			}
		}
	}
	if dpRaw, ok := obj["default_project"]; ok {
		if dp, ok := dpRaw.(string); ok {
			resp.DefaultProject = dp
		}
	}

	// all_entitlements: true — expand to explicit flat grant strings.
	// The JWT must always contain named entitlements (not a boolean wildcard)
	// so that multi-tenancy can constrain what "all" means per tenant.
	if allRaw, ok := obj["all_entitlements"]; ok {
		if allBool, ok := allRaw.(bool); ok && allBool {
			te, normalizedJSON, err := tokenEntitlementsFromValue(map[string]any{
				"grants": allEntitlementGrantTokens(),
			})
			if err != nil {
				zap.L().Error("EvalEntitlement: failed to expand all_entitlements", zap.Error(err))
				return resp, nil
			}
			resp.TokenEntitlements = te
			zap.L().Info("EvalEntitlement: expanded all_entitlements to explicit grants",
				zap.String("token_entitlements", string(normalizedJSON)))
			return resp, nil
		}
	}

	teRaw, ok := obj["token_entitlements"]
	if !ok {
		zap.L().Warn("EvalEntitlement: no token_entitlements in result, returning empty entitlements")
		return resp, nil
	}

	te, normalizedJSON, err := tokenEntitlementsFromValue(teRaw)
	if err != nil {
		zap.L().Warn("EvalEntitlement: failed to unmarshal token_entitlements",
			zap.Error(err))
		return resp, nil
	}

	resp.TokenEntitlements = te

	zap.L().Info("EvalEntitlement response", zap.String("token_entitlements", string(normalizedJSON)))

	return resp, nil
}

// allEntitlementGrantTokens returns the package-wide wildcard grant for every
// package in the GENERATED grant catalog — the explicit grant list that
// all_entitlements:true expands to.
//
// It used to be a hand-written list, and the list rotted: MCP_ANY,
// ANTHROPIC_ANY, GITHUB_ANY, GITLAB_ANY and OPENAI_ANY named services that had
// left the platform, so they were not in the catalog and normalized to nothing
// — an "all entitlements" token that granted a fraction of what it claimed,
// with no error anywhere. Reading the catalog instead means a service joining
// or leaving the fleet is picked up by regeneration, not by remembering to edit
// this function.
//
// Capability wildcards (<PKG>_READ_ANY and friends) are deliberately excluded:
// each is a subset of its package's ANY, so including them would add nothing
// and make the emitted list noisier.
func allEntitlementGrantTokens() []string {
	var out []string
	for _, token := range grants.Tokens() {
		if g, ok := grants.Lookup(token); ok && g.Capability == "ANY" {
			out = append(out, token)
		}
	}
	return out
}

func tokenEntitlementsFromValue(raw any) (*entitlements_pb.TokenEntitlements, []byte, error) {
	normalizedJSON, err := normalizeTokenEntitlementsJSON(raw)
	if err != nil {
		return nil, nil, err
	}
	te := &entitlements_pb.TokenEntitlements{}
	unmarshalOpts := protojson.UnmarshalOptions{DiscardUnknown: true}
	if err := unmarshalOpts.Unmarshal(normalizedJSON, te); err != nil {
		return nil, normalizedJSON, err
	}
	return te, normalizedJSON, nil
}

func normalizeTokenEntitlementsJSON(raw any) ([]byte, error) {
	tokens, dataGrants, azp, principalScope := collectEntitlementTokens(raw)
	out := map[string]any{}
	if tokenEntitlementGrantsAreStrings() {
		out["grants"] = tokens
	} else {
		grants := make([]map[string]string, 0, len(tokens))
		for _, token := range tokens {
			grants = append(grants, map[string]string{"token": token})
		}
		out["grants"] = grants
	}
	// data_grants is always a flat repeated string on TokenEntitlements (unlike
	// grants, it has no dual token-object shape), so it needs no
	// tokenEntitlementGrantsAreStrings() branch.
	if len(dataGrants) > 0 {
		out["data_grants"] = dataGrants
	}
	if len(azp) > 0 {
		out["azp"] = azp
	}
	// principal_scope rides the same claim bag as the grants: it is how wide the
	// caller's authority over PRINCIPALS is, and the generated
	// (spec.owner_default) guard reads it off service_entitlements. Emitted only
	// when the policy stamped one, so a policy that stamps nothing leaves the
	// claim absent and every caller fails closed to self.
	if principalScope != "" {
		out["principal_scope"] = principalScope
	}
	return json.Marshal(out)
}

func tokenEntitlementGrantsAreStrings() bool {
	fd := (&entitlements_pb.TokenEntitlements{}).ProtoReflect().Descriptor().Fields().ByName("grants")
	return fd != nil && fd.Kind() == protoreflect.StringKind
}

// collectEntitlementTokens flattens whatever shape the policy returned into the
// four things TokenEntitlements carries: the RPC grant tokens, the data-grant
// tokens, the azp list, and principal_scope. data_grants is threaded alongside
// grants (rather than left to the catch-all sweep below, which would otherwise
// fold its VALUE into the grant list as a grant token nobody ever declared) —
// exactly the bug that made a policy's data_grants entry land in the JWT's
// grants claim instead of its own data_grants claim, so a MAY_REDACT field's
// egress check (which reads data_grants specifically) never saw it.
func collectEntitlementTokens(raw any) ([]string, []string, []string, string) {
	tokens := make([]string, 0)
	dataGrants := make([]string, 0)
	azp := make([]string, 0)
	principalScope := ""
	collectEntitlementTokensInto(raw, &tokens, &dataGrants, &azp, &principalScope)
	return dedupeStrings(tokens), dedupeStrings(dataGrants), dedupeStrings(azp), principalScope
}

func collectEntitlementTokensInto(raw any, tokens *[]string, dataGrants *[]string, azp *[]string, principalScope *string) {
	switch v := raw.(type) {
	case nil:
		return
	case string:
		if v != "" {
			*tokens = append(*tokens, v)
		}
	case []string:
		for _, item := range v {
			if item != "" {
				*tokens = append(*tokens, item)
			}
		}
	case []any:
		for _, item := range v {
			collectEntitlementTokensInto(item, tokens, dataGrants, azp, principalScope)
		}
	case map[string]string:
		if token := v["token"]; token != "" {
			*tokens = append(*tokens, token)
		}
	case map[string]any:
		collectEntitlementObject(v, tokens, dataGrants, azp, principalScope)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return
		}
		var decoded any
		if err := json.Unmarshal(encoded, &decoded); err == nil {
			collectEntitlementTokensInto(decoded, tokens, dataGrants, azp, principalScope)
		}
	}
}

func collectEntitlementObject(obj map[string]any, tokens *[]string, dataGrants *[]string, azp *[]string, principalScope *string) {
	if token, ok := obj["token"].(string); ok && token != "" {
		*tokens = append(*tokens, token)
	}
	if grants, ok := obj["grants"]; ok {
		collectEntitlementTokensInto(grants, tokens, dataGrants, azp, principalScope)
	}
	if dg, ok := obj["data_grants"]; ok {
		collectStringsInto(dg, dataGrants)
	}
	if azpRaw, ok := obj["azp"]; ok {
		collectStringsInto(azpRaw, azp)
	}
	if scope, ok := obj["principal_scope"].(string); ok && scope != "" {
		*principalScope = scope
	}
	keys := make([]string, 0, len(obj))
	for key := range obj {
		if key != "token" && key != "grants" && key != "data_grants" && key != "azp" && key != "principal_scope" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		collectEntitlementTokensInto(obj[key], tokens, dataGrants, azp, principalScope)
	}
}

func collectStringsInto(raw any, out *[]string) {
	switch v := raw.(type) {
	case string:
		if v != "" {
			*out = append(*out, v)
		}
	case []string:
		for _, item := range v {
			if item != "" {
				*out = append(*out, item)
			}
		}
	case []any:
		for _, item := range v {
			collectStringsInto(item, out)
		}
	}
}

func dedupeStrings(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, item := range in {
		if item == "" || seen[item] {
			continue
		}
		seen[item] = true
		out = append(out, item)
	}
	return out
}

// EvalProxyPolicy implements proxypolicy_fe.OpaEvalHandler — evaluates the
// proxy policy for the given caller claims and routing metadata.
//
// The policy receives {claims, metadata} and returns:
//   - allow: bool — whether the request is permitted
//   - url: string — resolved destination URL (empty = use service default)
//   - reason: string — denial reason (when allow = false)
//   - append_aud: array[string] — audiences to append to the minted token (never replaces original)
//   - extra_claims: object — additional JWT claims to inject
//   - auth_type: string — credential format ("jwt" or "saml")
//   - token_use: string or int — token_use claim; must be "TOKEN_USE_EXTERNAL"/6 or unset
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

	if allowRaw, ok := obj["allow"]; !ok {
		zap.L().Warn("EvalProxyPolicy: policy result missing 'allow' field, defaulting to deny")
	} else if allow, ok := allowRaw.(bool); ok {
		resp.Allow = allow
	} else {
		zap.L().Warn("EvalProxyPolicy: 'allow' field is not a bool, defaulting to deny",
			zap.Any("allow_value", allowRaw))
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

	if appendAudRaw, ok := obj["append_aud"]; ok {
		switch v := appendAudRaw.(type) {
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok && s != "" {
					resp.AppendAud = append(resp.AppendAud, s)
				}
			}
		case string:
			// Tolerate a bare string for single-audience policies.
			if v != "" {
				resp.AppendAud = append(resp.AppendAud, v)
			}
		}
	}

	if tokenUseRaw, ok := obj["token_use"]; ok {
		resp.TokenUse = parseTokenUse(tokenUseRaw)
	}

	// extra_claims: additional JWT claims the kernel stamps into the minted
	// external token (e.g. anthropic federation_rule_id). The rego returns a
	// plain object; convert it to a protobuf Struct. Without this mapping the
	// claims are silently dropped before reaching the kernel.
	if extraRaw, ok := obj["extra_claims"]; ok {
		if m, ok := extraRaw.(map[string]interface{}); ok {
			if s, err := structpb.NewStruct(m); err == nil {
				resp.ExtraClaims = s
			} else {
				zap.L().Warn("EvalProxyPolicy: failed to convert extra_claims to struct",
					zap.Error(err))
			}
		}
	}

	zap.L().Info("EvalProxyPolicy result",
		zap.Bool("allow", resp.Allow),
		zap.String("url", resp.Url),
		zap.String("reason", resp.Reason),
		zap.Bool("extra_claims", resp.ExtraClaims != nil))

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

// parseTokenUse converts an OPA-returned token_use value (string name or number) to the
// token.TokenUse enum. Returns TOKEN_USE_UNSPECIFIED for unrecognized values.
// OPA policies may return the string name ("TOKEN_USE_EXTERNAL") or the integer value (6).
func parseTokenUse(raw interface{}) token_pb.TokenUse {
	switch v := raw.(type) {
	case string:
		if tu, err := token_pb.ParseTokenUse(v); err == nil {
			return tu
		}
		// Also accept short form without prefix.
		if tu, err := token_pb.ParseTokenUse("TOKEN_USE_" + v); err == nil {
			return tu
		}
	case float64:
		return token_pb.TokenUse(int32(v))
	case int64:
		return token_pb.TokenUse(int32(v))
	}
	return token_pb.TokenUse_TOKEN_USE_UNSPECIFIED
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
