package internal

import (
	"context"
	"fmt"
	"strings"

	"github.com/open-policy-agent/opa/v1/rego"
	"go.uber.org/zap"

	fsPb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/sys/filesystem"
	opaPb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/model/types/sys/opalib"
)

// PolicyEvaluator evaluates OPA policies to determine user profiles
// from IDP claims. It wraps a prepared OPA query for efficient repeated
// evaluation.
type PolicyEvaluator struct {
	query rego.PreparedEvalQuery
}

// NewPolicyEvaluator creates a PolicyEvaluator from an OpaPolicy proto.
// It extracts .rego files from the policy's filesystem.Directory bundle,
// loads them as OPA modules, and prepares the entry_point query.
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
// result based on the evaluation context.
func (pe *PolicyEvaluator) Evaluate(input map[string]any) (any, error) {
	results, err := pe.query.Eval(context.Background(), rego.EvalInput(input))
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
