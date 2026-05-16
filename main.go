package main

import (
	"log"
	"os"

	handler "gitlab.com/secondstarcavalier/schematic/platform/services/opa/internal"
	servicemanager "gitlab.com/secondstarcavalier/schematic/platform/services/opa/tacplus/s2sm"
	pb "gitlab.com/secondstarcavalier/schematic/platform/services/opa/tacplus/types/services/opa"
)

func main() {
	sm, err := servicemanager.New()
	if err != nil {
		log.Fatalf("servicemanager: %v", err)
	}

	h := handler.NewHandler()

	// Load OPA policies from the local bootstrap config file.
	// The kernel may not yet forward fields added after the kernel image was built,
	// so we read them directly from the mounted config file as a fallback.
	// Kernel-provided policies will override these if they are non-nil.
	bootstrapPath := os.Getenv("BOOTSTRAP_CONFIG")
	if bootstrapPath != "" {
		data, readErr := os.ReadFile(bootstrapPath)
		if readErr != nil {
			log.Fatalf("bootstrap: failed to read %s: %v", bootstrapPath, readErr)
		}
		cfg, parseErr := pb.ConfigFromBytes(data)
		if parseErr != nil {
			log.Fatalf("bootstrap: failed to parse config: %v", parseErr)
		}
		if policy := cfg.GetProxyPolicy(); policy != nil {
			log.Printf("bootstrap: loading proxy_policy (entry_point=%s)", policy.GetEntryPoint())
			h.LoadBootstrapPolicy(policy)
		} else {
			log.Printf("bootstrap: no proxy_policy in config")
		}
		if policy := cfg.GetEntitlementsPolicy(); policy != nil {
			log.Printf("bootstrap: loading entitlements_policy (entry_point=%s)", policy.GetEntryPoint())
			h.LoadBootstrapPolicy(policy)
		}
	}

	if err := sm.Run(h); err != nil {
		log.Fatalf("opa: %v", err)
	}
}
