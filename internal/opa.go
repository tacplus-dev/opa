package internal

import (
	"context"
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/v1/ast"
	"github.com/open-policy-agent/opa/v1/rego"
	"go.uber.org/zap"

	fsPb "github.com/tacplus-dev/opa/tacplus/types/sys/filesystem"
	opaPb "github.com/tacplus-dev/opa/tacplus/types/sys/opalib"
)

// networkBuiltins are the built-ins that reach outside opa's process. A policy
// decides entitlements and proxy routing from its input alone: one that calls
// these could reach anything opa's pod can (an SSRF from a policy edit) and
// stall kernel's sign-in on a slow host, so they are not in the capabilities a
// bundle is compiled against, and a bundle that calls one fails to load.
var networkBuiltins = map[string]bool{
	"http.send":          true,
	"net.lookup_ip_addr": true,
}

// policyCapabilities is this OPA version's capabilities without the network
// built-ins, and with no host allowed (AllowNet empty, not nil: nil allows any),
// which also keeps the compiler from fetching remote JSON schema refs.
func policyCapabilities() *ast.Capabilities {
	caps := ast.CapabilitiesForThisVersion()
	builtins := make([]*ast.Builtin, 0, len(caps.Builtins))
	for _, b := range caps.Builtins {
		if !networkBuiltins[b.Name] {
			builtins = append(builtins, b)
		}
	}
	caps.Builtins = builtins
	caps.AllowNet = []string{}
	return caps
}

// PolicyEvaluator evaluates OPA policies to determine user profiles
// from IDP claims. It wraps a prepared OPA query for efficient repeated
// evaluation.
type PolicyEvaluator struct {
	query rego.PreparedEvalQuery
}

// NewPolicyEvaluator creates a PolicyEvaluator from an OpaPolicy proto.
// It extracts .rego files from the policy's filesystem.Directory bundle,
// loads them as OPA modules, and prepares the entry_point query against
// policyCapabilities: a bundle calling a network built-in is refused here.
func NewPolicyEvaluator(policy *opaPb.OpaPolicy) (*PolicyEvaluator, error) {
	if policy.GetEntryPoint() == "" {
		return nil, fmt.Errorf("OPA policy missing entry_point")
	}

	// Collect all .rego files from the bundle directory tree.
	modules := collectRegoFiles(policy.GetBundle(), "")
	if len(modules) == 0 {
		return nil, fmt.Errorf("OPA policy bundle contains no .rego files")
	}

	// Build rego options: query + all modules.
	opts := []func(*rego.Rego){
		rego.Query(policy.GetEntryPoint()),
		rego.Capabilities(policyCapabilities()),
	}
	for path, src := range modules {
		opts = append(opts, rego.Module(path, src))
	}

	query, err := rego.New(opts...).PrepareForEval(context.Background())
	if err != nil {
		return nil, fmt.Errorf("OPA prepare query: %w", err)
	}

	zap.L().Info("OPA policy evaluator ready",
		zap.String("entry_point", policy.GetEntryPoint()),
		zap.Int("modules", len(modules)),
		zap.String("version", policy.GetVersion()),
	)

	return &PolicyEvaluator{query: query}, nil
}

// Evaluate runs the OPA policy with the given input map and returns
// the raw result value. Callers are responsible for interpreting the
// result based on the evaluation context. ctx is the RPC's: the evaluation
// stops at its deadline or cancellation.
func (pe *PolicyEvaluator) Evaluate(ctx context.Context, input map[string]any) (any, error) {
	results, err := pe.query.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return nil, fmt.Errorf("OPA eval: %w", err)
	}

	if len(results) == 0 || len(results[0].Expressions) == 0 {
		return nil, nil
	}

	return results[0].Expressions[0].Value, nil
}

// collectRegoFiles recursively walks a filesystem.Directory proto and
// returns a map of path->content for all .rego files.
func collectRegoFiles(dir *fsPb.Directory, prefix string) map[string]string {
	modules := make(map[string]string)
	if dir == nil {
		return modules
	}

	dirPath := prefix
	if dir.GetName() != "" {
		if dirPath != "" {
			dirPath += "/"
		}
		dirPath += dir.GetName()
	}

	for _, f := range dir.GetFiles() {
		if strings.HasSuffix(f.GetName(), ".rego") {
			path := f.GetName()
			if dirPath != "" {
				path = dirPath + "/" + f.GetName()
			}
			modules[path] = string(f.GetContent())
		}
	}

	for _, subDir := range dir.GetDirectories() {
		for k, v := range collectRegoFiles(subDir, dirPath) {
			modules[k] = v
		}
	}

	return modules
}
