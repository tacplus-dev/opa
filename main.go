package main

import (
	"log"
	"os"

	handler "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/internal"
	servicemanager "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/s2sm"
	pb "gitlab.com/secondstarcavalier/controlplane/services/uncleopa/tacplus/types/services/uncleopa"
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
			log.Printf("bootstrap: failed to read %s: %v", bootstrapPath, readErr)
		} else {
			cfg, parseErr := pb.ConfigFromBytes(data)
			if parseErr != nil {
				log.Printf("bootstrap: failed to parse config: %v", parseErr)
			} else if policy := cfg.GetBackendTokenPolicy(); policy != nil {
				log.Printf("bootstrap: loading backend_token_policy (entry_point=%s)", policy.GetEntryPoint())
				h.LoadBootstrapPolicy(policy)
			} else {
				log.Printf("bootstrap: no backend_token_policy in config")
			}
		}
	}

	if err := sm.Run(h); err != nil {
		log.Fatalf("uncleopa: %v", err)
	}
}
