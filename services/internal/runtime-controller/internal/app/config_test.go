package app

import (
	"strings"
	"testing"
)

func TestLoadConfigSeparatesControlAndRuntimeNamespaces(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "kodex-system")
	t.Setenv("RUNTIME_CONTROLLER_RUNTIME_NAMESPACE", "kodex-runtime")
	t.Setenv("POD_UID", "runtime-controller-test")
	t.Setenv("POD_IP", "10.0.0.10")
	t.Setenv("RUNTIME_CONTROLLER_PROMOTED_ROLE_IMAGE_REPOSITORY", "registry.example/kodex/roles")
	t.Setenv("RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_REVISION", "1")
	t.Setenv("RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_SHA256", strings.Repeat("a", 64))

	config, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if config.ControlNamespace != "kodex-system" || config.RuntimeNamespace != "kodex-runtime" {
		t.Fatalf("namespace boundary = control:%q runtime:%q", config.ControlNamespace, config.RuntimeNamespace)
	}
}

func TestLoadConfigRejectsSharedControlAndRuntimeNamespace(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "kodex-system")
	t.Setenv("RUNTIME_CONTROLLER_RUNTIME_NAMESPACE", "kodex-system")
	t.Setenv("POD_UID", "runtime-controller-test")
	t.Setenv("POD_IP", "10.0.0.10")
	t.Setenv("RUNTIME_CONTROLLER_PROMOTED_ROLE_IMAGE_REPOSITORY", "registry.example/kodex/roles")
	t.Setenv("RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_REVISION", "1")
	t.Setenv("RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_SHA256", strings.Repeat("a", 64))

	if _, err := loadConfig(); err == nil {
		t.Fatal("shared control and runtime namespace was accepted")
	}
}

func TestLoadConfigRejectsUnknownProviderAppArmorProfile(t *testing.T) {
	t.Setenv("POD_NAMESPACE", "kodex-system")
	t.Setenv("RUNTIME_CONTROLLER_RUNTIME_NAMESPACE", "kodex-runtime")
	t.Setenv("POD_UID", "runtime-controller-test")
	t.Setenv("POD_IP", "10.0.0.10")
	t.Setenv("RUNTIME_CONTROLLER_PROMOTED_ROLE_IMAGE_REPOSITORY", "registry.example/kodex/roles")
	t.Setenv("RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_REVISION", "1")
	t.Setenv("RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_SHA256", strings.Repeat("a", 64))
	t.Setenv("RUNTIME_CONTROLLER_PROVIDER_APPARMOR_PROFILE", "owner-defined-profile")

	if _, err := loadConfig(); err == nil {
		t.Fatal("unknown provider AppArmor profile was accepted")
	}
}
