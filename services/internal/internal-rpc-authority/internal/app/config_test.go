package app

import "testing"

func TestApplyWorkloadProfileRejectsForeignVaultRole(t *testing.T) {
	config := Config{
		Mode:             ModeIssuer,
		WorkloadID:       "integration-gateway",
		WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway",
		VaultAuthRole:    "internal-rpc-authority-control-api-gateway",
	}
	if err := applyWorkloadProfile(&config); err == nil {
		t.Fatal("foreign Vault role was accepted")
	}
}

func TestApplyWorkloadProfileBindsIntegrationGatewayPaths(t *testing.T) {
	config := Config{
		Mode:             ModeIssuer,
		WorkloadID:       "integration-gateway",
		WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway",
		VaultAuthRole:    "internal-rpc-authority-integration-gateway",
	}
	if err := applyWorkloadProfile(&config); err != nil {
		t.Fatalf("apply integration gateway profile: %v", err)
	}
	if config.ReadbackCredentialVaultPath != "kv/data/mattercodex/internal-rpc-authority/integration-gateway/issuer/readback-credential" ||
		config.RestoreACKVaultPath != "kv/data/mattercodex/internal-rpc-authority/integration-gateway/issuer/restore-ack" {
		t.Fatal("integration gateway Vault paths are not pinned")
	}
}

func TestApplyWorkloadProfileBindsAutomationSchedulerPaths(t *testing.T) {
	config := Config{
		Mode:             ModeIssuer,
		WorkloadID:       "automation-scheduler",
		WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/automation-scheduler",
		VaultAuthRole:    "internal-rpc-authority-automation-scheduler",
	}
	if err := applyWorkloadProfile(&config); err != nil {
		t.Fatalf("apply automation scheduler profile: %v", err)
	}
	if config.ReadbackCredentialVaultPath != "kv/data/mattercodex/internal-rpc-authority/automation-scheduler/issuer/readback-credential" ||
		config.ReadbackPossessionVaultPath != "kv/data/mattercodex/internal-rpc-authority/automation-scheduler/issuer/readback-possession" ||
		config.RestoreRoleCredentialVaultPath != "kv/data/mattercodex/internal-rpc-authority/automation-scheduler/issuer/restore-credential" ||
		config.RestoreACKVaultPath != "kv/data/mattercodex/internal-rpc-authority/automation-scheduler/issuer/restore-ack" {
		t.Fatal("automation scheduler Vault paths are not pinned")
	}
}

func TestApplyWorkloadProfileDisablesResolverForInteractionVerifier(t *testing.T) {
	config := Config{
		Mode:             ModeVerifier,
		WorkloadID:       "interaction-gateway",
		WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/interaction-gateway",
		VaultAuthRole:    "internal-rpc-authority-interaction-gateway",
	}
	if err := applyWorkloadProfile(&config); err != nil {
		t.Fatalf("apply interaction gateway verifier profile: %v", err)
	}
	if config.ResolverEnabled ||
		config.ReadbackCredentialVaultPath != "kv/data/mattercodex/internal-rpc-authority/interaction-gateway/verifier/readback-credential" ||
		config.RestoreACKVaultPath != "kv/data/mattercodex/internal-rpc-authority/interaction-gateway/verifier/restore-ack" {
		t.Fatal("interaction gateway verifier profile is not pinned")
	}
}

func TestApplyWorkloadProfileBindsReleaseWorkloads(t *testing.T) {
	tests := []struct {
		name                      string
		mode                      Mode
		workloadID                string
		spiffeID                  string
		vaultRole                 string
		readbackCredentialPath    string
		readbackPossessionPath    string
		restoreRoleCredentialPath string
		restoreACKPath            string
	}{
		{
			name:                      "integration gateway verifier",
			mode:                      ModeVerifier,
			workloadID:                "integration-gateway",
			spiffeID:                  "spiffe://mattercodex.local/ns/mattercodex-system/sa/integration-gateway",
			vaultRole:                 "internal-rpc-authority-integration-gateway",
			readbackCredentialPath:    "kv/data/mattercodex/internal-rpc-authority/integration-gateway/verifier/readback-credential",
			readbackPossessionPath:    "kv/data/mattercodex/internal-rpc-authority/integration-gateway/verifier/readback-possession",
			restoreRoleCredentialPath: "kv/data/mattercodex/internal-rpc-authority/integration-gateway/verifier/restore-credential",
			restoreACKPath:            "kv/data/mattercodex/internal-rpc-authority/integration-gateway/verifier/restore-ack",
		},
		{
			name:                      "runtime controller issuer",
			mode:                      ModeIssuer,
			workloadID:                "runtime-controller",
			spiffeID:                  "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
			vaultRole:                 "internal-rpc-authority-runtime-controller",
			readbackCredentialPath:    "kv/data/mattercodex/internal-rpc-authority/runtime-controller/issuer/readback-credential",
			readbackPossessionPath:    "kv/data/mattercodex/internal-rpc-authority/runtime-controller/issuer/readback-possession",
			restoreRoleCredentialPath: "kv/data/mattercodex/internal-rpc-authority/runtime-controller/issuer/restore-credential",
			restoreACKPath:            "kv/data/mattercodex/internal-rpc-authority/runtime-controller/issuer/restore-ack",
		},
		{
			name:                      "runtime S3 restore exchanger issuer",
			mode:                      ModeIssuer,
			workloadID:                "runtime-s3-restore-exchanger",
			spiffeID:                  "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-s3-restore-exchanger",
			vaultRole:                 "internal-rpc-authority-runtime-s3-restore-exchanger",
			readbackCredentialPath:    "kv/data/mattercodex/internal-rpc-authority/runtime-s3-restore-exchanger/issuer/readback-credential",
			readbackPossessionPath:    "kv/data/mattercodex/internal-rpc-authority/runtime-s3-restore-exchanger/issuer/readback-possession",
			restoreRoleCredentialPath: "kv/data/mattercodex/internal-rpc-authority/runtime-s3-restore-exchanger/issuer/restore-credential",
			restoreACKPath:            "kv/data/mattercodex/internal-rpc-authority/runtime-s3-restore-exchanger/issuer/restore-ack",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config := Config{
				Mode:             test.mode,
				WorkloadID:       test.workloadID,
				WorkloadSPIFFEID: test.spiffeID,
				VaultAuthRole:    test.vaultRole,
				ResolverEnabled:  true,
			}
			if err := applyWorkloadProfile(&config); err != nil {
				t.Fatalf("apply workload profile: %v", err)
			}
			if config.Mode != test.mode ||
				config.WorkloadID != test.workloadID ||
				config.WorkloadSPIFFEID != test.spiffeID ||
				config.VaultAuthRole != test.vaultRole ||
				config.ReadbackCredentialVaultPath != test.readbackCredentialPath ||
				config.ReadbackPossessionVaultPath != test.readbackPossessionPath ||
				config.RestoreRoleCredentialVaultPath != test.restoreRoleCredentialPath ||
				config.RestoreACKVaultPath != test.restoreACKPath ||
				config.ResolverEnabled {
				t.Fatal("release workload profile is not pinned")
			}
		})
	}
}

func TestApplyWorkloadProfileRejectsUnknownReleaseBindings(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "wrong SPIFFE ID",
			config: Config{
				Mode:             ModeVerifier,
				WorkloadID:       "integration-gateway",
				WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/control-api-gateway",
				VaultAuthRole:    "internal-rpc-authority-integration-gateway",
			},
		},
		{
			name: "wrong Vault role",
			config: Config{
				Mode:             ModeIssuer,
				WorkloadID:       "runtime-controller",
				WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
				VaultAuthRole:    "internal-rpc-authority-runtime-restore-effect",
			},
		},
		{
			name: "wrong mode",
			config: Config{
				Mode:             ModeVerifier,
				WorkloadID:       "runtime-controller",
				WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-controller",
				VaultAuthRole:    "internal-rpc-authority-runtime-controller",
			},
		},
		{
			name: "unknown workload",
			config: Config{
				Mode:             ModeIssuer,
				WorkloadID:       "runtime-unknown",
				WorkloadSPIFFEID: "spiffe://mattercodex.local/ns/mattercodex-system/sa/runtime-unknown",
				VaultAuthRole:    "internal-rpc-authority-runtime-unknown",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := applyWorkloadProfile(&test.config); err == nil {
				t.Fatal("unregistered release binding was accepted")
			}
		})
	}
}
