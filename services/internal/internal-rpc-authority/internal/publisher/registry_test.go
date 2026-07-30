package publisher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestLoadRegistryПринимаетФактическийProductionConfigMap(t *testing.T) {
	path := productionRegistryPath(t)
	registry, err := LoadRegistry(path)
	if err != nil {
		t.Fatalf("LoadRegistry() error = %v", err)
	}
	if len(registry.Targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(registry.Targets))
	}
	issuer := registry.Targets["control-api-gateway.authorization-issuer"]
	if issuer.Namespace != "mattercodex-system" ||
		issuer.ServiceAccount != "control-api-gateway" ||
		issuer.AuthPrivateKeyVaultPath == "" ||
		issuer.ProofTrustVaultPath == "" ||
		issuer.RestoreACKKeyGeneration != 1 ||
		issuer.ReadbackChallengeMethod == "" {
		t.Fatalf("issuer target is incomplete: %#v", issuer)
	}
	verifier := registry.Targets["control-plane.authorization-verifier"]
	if verifier.AuthPrivateKeyVaultPath != "" ||
		verifier.ProofTrustVaultPath != "" ||
		verifier.ManifestTrustVaultPath == "" ||
		verifier.AuthoritySnapshotSecret == "" ||
		verifier.ReadbackAttestationMethod == "" {
		t.Fatalf("verifier target is incomplete: %#v", verifier)
	}
}

func TestLoadRegistryЗакрытоОтклоняетНеизвестноеПолеProductionSchema(t *testing.T) {
	raw, err := os.ReadFile(productionRegistryPath(t))
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(
		string(raw),
		"    namespace: mattercodex-system\n",
		"    namespace: mattercodex-system\n    caller_authority: forbidden\n",
		1,
	)
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := os.WriteFile(path, []byte(mutated), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistry(path); err == nil {
		t.Fatal("LoadRegistry() accepted unknown caller-controlled authority")
	}
}

func TestLoadRegistryЗакрытоОтклоняетПодменуServiceAccount(t *testing.T) {
	raw, err := os.ReadFile(productionRegistryPath(t))
	if err != nil {
		t.Fatal(err)
	}
	mutated := strings.Replace(
		string(raw),
		"    service_account: control-api-gateway\n",
		"    service_account: internal-rpc-authority-publisher\n",
		1,
	)
	path := filepath.Join(t.TempDir(), "registry.yaml")
	if err := os.WriteFile(path, []byte(mutated), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRegistry(path); err == nil {
		t.Fatal("LoadRegistry() accepted workload/SPIFFE/ServiceAccount mismatch")
	}
}

func productionRegistryPath(t *testing.T) string {
	t.Helper()
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() failed")
	}
	return filepath.Clean(filepath.Join(
		filepath.Dir(source),
		"..", "..", "..", "..", "..",
		"deploy", "k8s", "base", "internal-rpc-authority-publisher",
		"key-delivery-targets.yaml",
	))
}
