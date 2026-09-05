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
			name: "email bridge issuer", mode: ModeIssuer,
			workloadID: "email-bridge",
			spiffeID:   "spiffe://kodex.local/ns/kodex-system/sa/email-bridge",
			prefix:     "internal-rpc-authority-email-bridge-issuer",
		},
		{
			name: "interaction gateway issuer", mode: ModeIssuer,
			workloadID: "interaction-gateway",
			spiffeID:   "spiffe://kodex.local/ns/kodex-system/sa/interaction-gateway",
			prefix:     "internal-rpc-authority-interaction-gateway-issuer",
		},
		{
			name: "runtime controller issuer", mode: ModeIssuer,
			workloadID: "runtime-controller",
			spiffeID:   "spiffe://kodex.local/ns/kodex-system/sa/runtime-controller",
			prefix:     "internal-rpc-authority-runtime-controller-issuer",
		},
		{
			name: "session archive issuer", mode: ModeIssuer,
			workloadID: "session-archive",
			spiffeID:   "spiffe://kodex.local/ns/kodex-system/sa/session-archive",
			prefix:     "internal-rpc-authority-session-archive-issuer",
		},
		{
			name: "control plane verifier", mode: ModeVerifier,
			workloadID: "control-plane",
			spiffeID:   "spiffe://kodex.local/ns/kodex-system/sa/control-plane",
			prefix:     "internal-rpc-authority-control-plane-verifier",
			resolver:   true,
		},
		{
			name: "control plane issuer", mode: ModeIssuer,
			workloadID: "control-plane",
			spiffeID:   "spiffe://kodex.local/ns/kodex-system/sa/control-plane",
			prefix:     "internal-rpc-authority-control-plane-issuer",
		},
		{
			name: "secret broker verifier", mode: ModeVerifier,
			workloadID: "secret-broker",
			spiffeID:   "spiffe://kodex.local/ns/kodex-system/sa/secret-broker",
			prefix:     "internal-rpc-authority-secret-broker-verifier",
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

func TestOptionalIssuerRejectsForeignSPIFFEAndVerifierRole(t *testing.T) {
	t.Parallel()
	for _, workload := range []string{"email-bridge", "interaction-gateway"} {
		for _, config := range []Config{
			{Mode: ModeIssuer, WorkloadID: workload, WorkloadSPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/control-plane"},
			{Mode: ModeVerifier, WorkloadID: workload, WorkloadSPIFFEID: "spiffe://kodex.local/ns/kodex-system/sa/" + workload},
		} {
			config.SecretBackend = string(secretBackendKubernetes)
			if err := applyWorkloadProfile(&config); err == nil {
				t.Fatal("foreign identity or unsupported verifier profile was accepted")
			}
		}
	}
}
