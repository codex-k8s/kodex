package app

import "testing"

// TestConfigSeparatesRuntimeNamespaceFromPodIdentity sets synthetic deployment/Pod variables and checks
// the fixed runtime namespace is independent of POD_NAMESPACE while claimant uses POD_UID.
// TestConfigSeparatesRuntimeNamespaceFromPodIdentity задаёт synthetic deployment/Pod variables и проверяет
// независимость fixed runtime namespace от POD_NAMESPACE, тогда как claimant использует POD_UID.
func TestConfigSeparatesRuntimeNamespaceFromPodIdentity(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	t.Setenv("POD_NAMESPACE", "kodex-system")
	t.Setenv("POD_UID", "8d47ed4c-1d6e-4b57-9e69-7e620d9422d8")
	t.Setenv("SECRET_BROKER_RUNTIME_NAMESPACE", "kodex-runtime")
	config, err := loadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if config.RuntimeNamespace != "kodex-runtime" || config.ClaimantID != "8d47ed4c-1d6e-4b57-9e69-7e620d9422d8" {
		t.Fatalf("unexpected runtime boundary: namespace=%q claimant=%q", config.RuntimeNamespace, config.ClaimantID)
	}
}

// TestConfigRejectsMissingPodUIDAndForeignRuntimeNamespace refuses an empty claimant and the platform
// namespace used as runtime target through the real environment parser/validator.
// TestConfigRejectsMissingPodUIDAndForeignRuntimeNamespace отклоняет пустой claimant и platform namespace
// вместо runtime target через реальный environment parser/validator.
func TestConfigRejectsMissingPodUIDAndForeignRuntimeNamespace(t *testing.T) {
	t.Setenv("DEPLOYMENT_ENVIRONMENT", "staging")
	t.Setenv("POD_UID", "")
	t.Setenv("SECRET_BROKER_RUNTIME_NAMESPACE", "kodex-runtime")
	if _, err := loadConfig(); err == nil {
		t.Fatal("missing POD_UID must fail closed")
	}
	t.Setenv("POD_UID", "synthetic-pod-uid")
	t.Setenv("SECRET_BROKER_RUNTIME_NAMESPACE", "kodex-system")
	if _, err := loadConfig(); err == nil {
		t.Fatal("platform namespace must not become runtime secret target")
	}
}
