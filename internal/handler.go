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

	"gitlab.com/seabeam/schematic/platform/services/opa/model/chassis/servicemanager"
	"gitlab.com/seabeam/schematic/platform/services/opa/model/grants"
	proxypolicy_fe "gitlab.com/seabeam/schematic/platform/services/opa/model/s2sm/grpc/servers/vservices/proxypolicy"
	userentitlements_fe "gitlab.com/seabeam/schematic/platform/services/opa/model/s2sm/grpc/servers/vservices/userentitlements"
	entitlements_pb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/generated/entitlements"
	pb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/services/opa"
	opaPb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/sys/opalib"
	token_pb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/sys/token"
	x509Pb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/sys/x509"
	proxypolicy_pb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/vservices/proxypolicy"
	userentitlements_pb "gitlab.com/seabeam/schematic/platform/services/opa/model/types/vservices/userentitlements"
)

// The two entry points this service evaluates. They are compiled into
// EvalEntitlement and EvalProxyPolicy as literals, so a pushed config that
// declares anything else would load an evaluator nothing ever looks up --
// leaving the previously loaded policy quietly in force while the log said
// "loaded OPA policy". loadPolicy checks the declared entry point against the
// slot being loaded and refuses the mismatch instead.
const (
	// EntitlementsEntryPoint is the rego query EvalEntitlement evaluates.
	EntitlementsEntryPoint = "data.entitlements.eval"
	// ProxyPolicyEntryPoint is the rego query EvalProxyPolicy evaluates.
	ProxyPolicyEntryPoint = "data.proxypolicy.eval"
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
// wantEntryPoint is the entry point the slot this policy was read from is
// evaluated at (EntitlementsEntryPoint or ProxyPolicyEntryPoint); a policy
// declaring a different one is refused rather than loaded under a name no
// handler looks up.
func (h *Handler) LoadBootstrapPolicy(policy *opaPb.OpaPolicy, wantEntryPoint string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.loadPolicy(policy, wantEntryPoint); err != nil {
		zap.L().Error("failed to load bootstrap OPA policy", zap.String("entry_point", policy.GetEntryPoint()), zap.Error(err))
		return err
	}
	return nil
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
		if err := h.loadPolicy(policy, ProxyPolicyEntryPoint); err != nil {
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
		if err := h.loadPolicy(policy, EntitlementsEntryPoint); err != nil {
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
	eval, err := h.getEvaluator(EntitlementsEntryPoint)
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

	// Fail closed, exactly as the sibling EvalProxyPolicy does on the same
	// situation. A nil/non-object result means the policy is undefined for this
	// input -- the rule did not match and there is no default. Returning an OK
	// response with empty grants made that indistinguishable from "this caller
	// is entitled to nothing", and the response would still carry the namespace/
	// authorized_projects set below, so a broken evaluation resolved to a real
	// (possibly broader) scope with no grants rather than to a refusal.
	obj, ok := result.(map[string]interface{})
	if !ok {
		return nil, status.Errorf(codes.Internal, "entitlements policy returned a non-object result (%T) -- policy undefined for this input", result)
	}

	// namespace: the authorization-tree position the policy assigns to this
	// caller (e.g. "/tenants/drpepper"). Kernel sets it as the JWT namespace
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
				return nil, status.Errorf(codes.Internal, "failed to expand all_entitlements: %v", err)
			}
			resp.TokenEntitlements = te
			zap.L().Info("EvalEntitlement: expanded all_entitlements to explicit grants",
				zap.String("token_entitlements", string(normalizedJSON)))
			return resp, nil
		}
	}

	// A policy that entitles this caller to nothing says so with an empty object
	// ({"token_entitlements": {}}, which every shipped policy's `default eval`
	// does). An ABSENT key is a malformed result, not a deliberate empty one, so
	// it fails closed alongside the unparseable case below rather than minting a
	// token with no grants.
	teRaw, ok := obj["token_entitlements"]
	if !ok {
		return nil, status.Errorf(codes.Internal, "entitlements policy result has no token_entitlements field")
	}

	te, normalizedJSON, err := tokenEntitlementsFromValue(teRaw)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "malformed token_entitlements: %v", err)
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
	tokens, dataGrants, azp, principalScope, err := collectEntitlementTokens(raw)
	if err != nil {
		return nil, err
	}
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

// The ONLY keys an entitlement object may carry, and the only keys a grant
// element in wrapped form may carry. These are allow-lists, deliberately, and
// they are the whole point of this parser.
//
// What was here before handled the five known keys and then swept every OTHER
// key in the object into the grant list, recursing into nested values as it
// went. That sweep put a key's VALUE into the JWT grants claim -- a symbol no
// catalog declares -- and it did so twice for real fields: data_grants landed
// in grants instead of its own claim (so a MAY_REDACT field's egress check,
// which reads data_grants specifically, saw none), and principal_scope landed
// in grants instead of stamping the scope. Each was fixed by adding one more
// name to the sweep's exclusion list, which fixes that field and nothing else:
// the NEXT field added to TokenEntitlements reintroduces the bug, and a nested
// {"meta": {"principal_scope": "PRINCIPAL_SCOPE_ANY"}} slipped through even
// with both exclusions in place, because nesting was never excluded either.
//
// So: enumerate what this code understands and REJECT the rest. Rejecting
// rather than skipping, for two reasons. It is the same fail-closed posture the
// rest of this handler now takes -- an entitlement result we cannot fully
// interpret is an evaluation we should not mint a token from, not one to
// silently trim. And it is the only behaviour that gives an operator a signal:
// a skipped field is a log line nobody reads, while a rejected one surfaces at
// the RPC boundary the moment the policy is pushed. The cost is that adding a
// field to TokenEntitlements now requires adding it here -- which is exactly
// the coupling the two previous silent-leak bugs came from NOT having.
var (
	entitlementObjectFields = map[string]bool{
		"grants":          true,
		"data_grants":     true,
		"azp":             true,
		"principal_scope": true,
	}
	grantElementFields = map[string]bool{
		"token": true,
	}
)

// collectEntitlementTokens flattens the shape the policy returned into the four
// things TokenEntitlements carries: the RPC grant tokens, the data-grant
// tokens, the azp list, and principal_scope. Any key, or any value shape, it
// does not recognize is an error.
func collectEntitlementTokens(raw any) ([]string, []string, []string, string, error) {
	var fields entitlementFields
	if err := parseEntitlementObject(raw, &fields); err != nil {
		return nil, nil, nil, "", err
	}
	return dedupeStrings(fields.tokens), dedupeStrings(fields.dataGrants), dedupeStrings(fields.azp), fields.principalScope, nil
}

type entitlementFields struct {
	tokens         []string
	dataGrants     []string
	azp            []string
	principalScope string
}

func parseEntitlementObject(raw any, out *entitlementFields) error {
	obj, err := asObject(raw, "token_entitlements")
	if err != nil {
		return err
	}
	if obj == nil {
		return nil
	}
	if err := rejectUnknownKeys(obj, entitlementObjectFields, "token_entitlements"); err != nil {
		return err
	}

	if v, ok := obj["grants"]; ok {
		if err := parseGrants(v, &out.tokens); err != nil {
			return err
		}
	}
	if v, ok := obj["data_grants"]; ok {
		if err := parseStringList(v, "data_grants", &out.dataGrants); err != nil {
			return err
		}
	}
	if v, ok := obj["azp"]; ok {
		if err := parseStringList(v, "azp", &out.azp); err != nil {
			return err
		}
	}
	if v, ok := obj["principal_scope"]; ok && v != nil {
		scope, ok := v.(string)
		if !ok {
			return fmt.Errorf("token_entitlements.principal_scope must be a string, got %T", v)
		}
		out.principalScope = scope
	}
	return nil
}

// parseGrants accepts a grant list in either shape the platform has shipped: a
// flat string list, or a list of {"token": "..."} wrappers (mixed is fine). A
// wrapper is itself allow-listed, so the nested-object escape the sweep left
// open is closed at this level too.
func parseGrants(raw any, out *[]string) error {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		appendNonEmpty(out, v)
	case []string:
		for _, item := range v {
			appendNonEmpty(out, item)
		}
	case []any:
		for _, item := range v {
			if err := parseGrants(item, out); err != nil {
				return err
			}
		}
	case map[string]any, map[string]string:
		obj, err := asObject(v, "token_entitlements.grants entry")
		if err != nil {
			return err
		}
		if err := rejectUnknownKeys(obj, grantElementFields, "token_entitlements.grants entry"); err != nil {
			return err
		}
		if token, ok := obj["token"]; ok && token != nil {
			str, ok := token.(string)
			if !ok {
				return fmt.Errorf("token_entitlements.grants entry token must be a string, got %T", token)
			}
			appendNonEmpty(out, str)
		}
	default:
		return fmt.Errorf("token_entitlements.grants must be a string or a list of grant tokens, got %T", raw)
	}
	return nil
}

// parseStringList reads a flat repeated-string field (data_grants, azp),
// tolerating a bare string for the single-element case.
func parseStringList(raw any, field string, out *[]string) error {
	switch v := raw.(type) {
	case nil:
		return nil
	case string:
		appendNonEmpty(out, v)
	case []string:
		for _, item := range v {
			appendNonEmpty(out, item)
		}
	case []any:
		for _, item := range v {
			if item == nil {
				continue
			}
			str, ok := item.(string)
			if !ok {
				return fmt.Errorf("token_entitlements.%s entries must be strings, got %T", field, item)
			}
			appendNonEmpty(out, str)
		}
	default:
		return fmt.Errorf("token_entitlements.%s must be a string or list of strings, got %T", field, raw)
	}
	return nil
}

// rejectUnknownKeys is the allow-list check itself. It runs at EVERY level an
// object appears at, not just the top one. Unknown keys are reported sorted so
// the error an operator sees is stable.
func rejectUnknownKeys(obj map[string]any, allowed map[string]bool, where string) error {
	var unknown []string
	for key := range obj {
		if !allowed[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%s has unrecognized field(s) %v; recognized fields are %v", where, unknown, sortedKeys(allowed))
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// asObject normalizes the object shapes OPA and the internal callers produce.
// nil (a policy that stamped nothing) is an empty object, not an error.
func asObject(raw any, where string) (map[string]any, error) {
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return v, nil
	case map[string]string:
		obj := make(map[string]any, len(v))
		for k, item := range v {
			obj[k] = item
		}
		return obj, nil
	default:
		return nil, fmt.Errorf("%s must be an object, got %T", where, raw)
	}
}

func appendNonEmpty(out *[]string, value string) {
	if value != "" {
		*out = append(*out, value)
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
	eval, err := h.getEvaluator(ProxyPolicyEntryPoint)
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

// loadPolicy creates or replaces the PolicyEvaluator for wantEntryPoint.
//
// The policy's own declared entry_point must equal wantEntryPoint. A config
// that declares something else -- a typo, or the OTHER slot's entry point --
// used to compile fine and be stored under the name it declared, so the
// evaluator the handler actually looks up was either left untouched (the update
// silently ignored) or, in the swapped case, replaced by the wrong bundle. Both
// were reported as "loaded OPA policy". Refusing the load surfaces it to the
// operator as a config error and leaves the previously loaded policy in force.
func (h *Handler) loadPolicy(policy *opaPb.OpaPolicy, wantEntryPoint string) error {
	ep := policy.GetEntryPoint()
	if ep == "" {
		return fmt.Errorf("policy missing entry_point")
	}
	if ep != wantEntryPoint {
		return fmt.Errorf("policy declares entry_point %q but this slot is evaluated at %q; refusing the load, the previously loaded policy stays in force", ep, wantEntryPoint)
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
