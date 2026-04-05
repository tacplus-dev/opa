package internal

import (
	"context"
	"fmt"
	"sync"

	"go.uber.org/zap"

	servicemanager "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/s2sm"
	pb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/services/uncleopa"
	opaPb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/libs/opa"
	x509Pb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/libs/x509"
)

// Handler implements the service-manager Handler interface for uncleopa.
// It manages OPA policy evaluators, reloading them when state updates
// deliver new policy bundles.
type Handler struct {
	mu          sync.RWMutex
	evaluators  map[string]*PolicyEvaluator // keyed by entry_point
}

// NewHandler creates a new Handler.
func NewHandler(sm *servicemanager.ServiceManager) (*Handler, error) {
	return &Handler{
		evaluators: make(map[string]*PolicyEvaluator),
	}, nil
}

func (h *Handler) Init(sm *servicemanager.ServiceManager) error {
	return nil
}

// UncleOpaConfig is called by the SM when config updates arrive from kernel.
// OPA policy bundles are delivered via config (not state).
func (h *Handler) UncleOpaConfig(cfg *pb.Config) error {
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

func (h *Handler) OnStateUpdate(state *pb.UncleOpaState) error {
	return nil
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
