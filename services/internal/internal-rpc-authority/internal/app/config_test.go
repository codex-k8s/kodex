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
