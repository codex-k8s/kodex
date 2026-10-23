package app

import (
	"strings"
	"testing"
)

// configureRuntimeControllerTestEnvironment installs the shared synthetic identity and image-contract settings
// with the supplied runtime namespace; t.Setenv restores each value when the owning test finishes.
// configureRuntimeControllerTestEnvironment задаёт общие синтетические настройки идентичности и контракта образа
// с переданным пространством выполнения; t.Setenv восстанавливает каждое значение при завершении теста-владельца.
func configureRuntimeControllerTestEnvironment(t *testing.T, runtimeNamespace string) {
	t.Helper()
	t.Setenv("POD_NAMESPACE", "kodex-system")
	t.Setenv("RUNTIME_CONTROLLER_RUNTIME_NAMESPACE", runtimeNamespace)
	t.Setenv("POD_UID", "runtime-controller-test")
	t.Setenv("POD_IP", "10.0.0.10")
	t.Setenv("RUNTIME_CONTROLLER_PROMOTED_ROLE_IMAGE_REPOSITORY", "registry.example/kodex/roles")
	t.Setenv("RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_REVISION", "1")
	t.Setenv("RUNTIME_CONTROLLER_ROLE_RUNTIME_CONTRACT_SHA256", strings.Repeat("a", 64))
}

// TestLoadConfigSeparatesControlAndRuntimeNamespaces supplies synthetic environment settings and checks
// that loaded control and runtime namespaces retain their required distinct values.
// TestLoadConfigSeparatesControlAndRuntimeNamespaces задаёт синтетическое окружение и проверяет,
// что загруженные пространства управления и выполнения сохраняют обязательные раздельные значения.
func TestLoadConfigSeparatesControlAndRuntimeNamespaces(t *testing.T) {
	configureRuntimeControllerTestEnvironment(t, "kodex-runtime")

	config, err := loadConfig()
	if err != nil {
		t.Fatalf("loadConfig() error = %v", err)
	}
	if config.ControlNamespace != "kodex-system" || config.RuntimeNamespace != "kodex-runtime" {
		t.Fatalf("namespace boundary = control:%q runtime:%q", config.ControlNamespace, config.RuntimeNamespace)
	}
}

// TestLoadConfigRejectsSharedControlAndRuntimeNamespace verifies startup configuration rejects
// placing runtime workloads in the control-service namespace.
// TestLoadConfigRejectsSharedControlAndRuntimeNamespace проверяет отказ конфигурации запуска
// при размещении рабочих нагрузок выполнения в пространстве управляющих сервисов.
func TestLoadConfigRejectsSharedControlAndRuntimeNamespace(t *testing.T) {
	configureRuntimeControllerTestEnvironment(t, "kodex-system")

	if _, err := loadConfig(); err == nil {
		t.Fatal("shared control and runtime namespace was accepted")
	}
}

// TestLoadConfigRejectsUnknownProviderAppArmorProfile checks that an owner-supplied unrecognized profile name
// cannot bypass the closed provider AppArmor profile selection.
// TestLoadConfigRejectsUnknownProviderAppArmorProfile проверяет, что неизвестное имя профиля от владельца
// не обходит закрытый выбор профиля AppArmor провайдера.
func TestLoadConfigRejectsUnknownProviderAppArmorProfile(t *testing.T) {
	configureRuntimeControllerTestEnvironment(t, "kodex-runtime")
	t.Setenv("RUNTIME_CONTROLLER_PROVIDER_APPARMOR_PROFILE", "owner-defined-profile")

	if _, err := loadConfig(); err == nil {
		t.Fatal("unknown provider AppArmor profile was accepted")
	}
}
