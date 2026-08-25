package app

import "testing"

func TestApplyWorkloadProfilePinsKubernetesSecrets(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		mode       Mode
		workloadID string
		spiffeID   string
		prefix     string
		resolver   bool
	}{
		{
			name: "runtime controller issuer", mode: ModeIssuer,
			workloadID: "runtime-controller",
			spiffeID:   "spiffe://kodex.local/ns/kodex-system/sa/runtime-controller",
			prefix:     "internal-rpc-authority-runtime-controller-issuer",
		},
		{
			name: "control plane verifier", mode: ModeVerifier,
			workloadID: "control-plane",
			spiffeID:   "spiffe://kodex.local/ns/kodex-system/sa/control-plane",
			prefix:     "internal-rpc-authority-control-plane-verifier",
			resolver:   true,
		},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Config{
				Mode: test.mode, SecretBackend: string(secretBackendKubernetes),
				WorkloadID: test.workloadID, WorkloadSPIFFEID: test.spiffeID,
			}
			if err := applyWorkloadProfile(&config); err != nil {
				t.Fatalf("apply workload profile: %v", err)
			}
			if config.ReadbackCredentialSecret != test.prefix+"-readback-credential" ||
				config.ReadbackPossessionSecret != test.prefix+"-readback-possession" ||
				config.RestoreRoleCredentialSecret != test.prefix+"-restore-credential" ||
				config.RestoreACKSecret != test.prefix+"-restore-ack" ||
				config.ResolverEnabled != test.resolver {
				t.Fatal("workload Secret profile is not pinned")
			}
		})
	}
}

func TestApplyWorkloadProfileRejectsUnknownBinding(t *testing.T) {
	t.Parallel()
	config := Config{
		Mode: ModeIssuer, SecretBackend: string(secretBackendKubernetes),
		WorkloadID:       "runtime-unknown",
		WorkloadSPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/runtime-unknown",
	}
	if err := applyWorkloadProfile(&config); err == nil {
		t.Fatal("unregistered workload binding was accepted")
	}
}
