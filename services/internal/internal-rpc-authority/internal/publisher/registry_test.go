package publisher

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// TestCanonicalDeliveryRegistriesLoad checks exact107 and frozen106 membership from read-only files.
// Current web profiles share the base source and retain separate full-render qualification.
// TestCanonicalDeliveryRegistriesLoad проверяет точный состав107 и frozen106 из файлов только для чтения.
// Текущие web-профили используют общий base source и отдельную квалификацию полного render.
func TestCanonicalDeliveryRegistriesLoad(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь теста")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "..", ".."))
	// baseStartupTargets preserves all106 bindings;107 adds only the interaction issuer.
	// baseStartupTargets сохраняет все bindings106;107 добавляет только interaction issuer.
	baseStartupTargets := []string{
		"role-image-builder.authorization-issuer",
		"automation-scheduler.authorization-issuer",
		"secret-broker.authorization-issuer",
		"secret-broker.authorization-verifier",
		"session-archive.authorization-issuer",
		"control-api-gateway.authorization-issuer",
		"control-plane.authorization-issuer",
		"control-plane.authorization-verifier",
		"control-plane.authority-proof-resolver",
		"integration-gateway.authorization-issuer",
		"runtime-controller.authorization-issuer",
		"stt-tts-service.authorization-issuer",
		"stt-tts-service.authorization-verifier",
		"email-bridge.authorization-issuer",
	}
	for _, testCase := range []struct {
		relative             string
		wantStartupReadbacks int
		wantRegistered       int
		requiredTargets      []string
	}{
		{relative: "deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml", wantStartupReadbacks: 15, wantRegistered: 17,
			requiredTargets: append(append([]string(nil), baseStartupTargets...), "interaction-gateway.authorization-issuer")},
		{relative: "tools/release/fixtures/authority106-key-delivery-targets.yaml", wantStartupReadbacks: 14, wantRegistered: 16,
			requiredTargets: baseStartupTargets},
	} {
		source, err := os.ReadFile(filepath.Join(repositoryRoot, testCase.relative))
		if err != nil {
			t.Fatalf("прочитать реестр %s: %v", testCase.relative, err)
		}
		projected := filepath.Join(t.TempDir(), filepath.Base(testCase.relative))
		if err := os.WriteFile(projected, source, 0o444); err != nil {
			t.Fatalf("материализовать projected реестр %s: %v", testCase.relative, err)
		}
		registry, err := LoadRegistry(projected)
		if err != nil {
			t.Fatalf("реестр %s не прошёл runtime-валидацию: %v", testCase.relative, err)
		}
		if len(registry.Targets) != testCase.wantRegistered {
			t.Fatalf("registry %s contains %d registered targets, expected %d", testCase.relative, len(registry.Targets), testCase.wantRegistered)
		}
		wanted := make(map[string]bool, testCase.wantRegistered)
		for _, targetID := range testCase.requiredTargets {
			wanted[targetID] = true
		}
		for _, targetID := range []string{"image-admission.authorization-issuer", "image-promotion.authorization-issuer"} {
			wanted[targetID] = false
		}
		for targetID := range registry.Targets {
			if _, ok := wanted[targetID]; !ok {
				t.Fatalf("registry %s contains unexpected target %s", testCase.relative, targetID)
			}
		}
		for _, targetID := range testCase.requiredTargets {
			if _, ok := registry.Targets[targetID]; !ok {
				t.Fatalf("реестр %s не содержит цель %s", testCase.relative, targetID)
			}
		}
		if got := registry.StartupReadbackTargetCount(); got != testCase.wantStartupReadbacks {
			t.Fatalf(
				"реестр %s содержит %d обязательных startup readback, ожидалось %d",
				testCase.relative,
				got,
				testCase.wantStartupReadbacks,
			)
		}
		startupTargets := registry.StartupReadbackTargets()
		startupTargetKeys := make([]string, 0, len(startupTargets))
		for _, target := range startupTargets {
			targetID := target.WorkloadID + "." + strings.ReplaceAll(strings.ToLower(target.Role), "_", "-")
			if !wanted[targetID] || target.WorkloadGeneration != 1 {
				t.Fatalf("registry %s contains unexpected startup binding %s generation %d", testCase.relative, targetID, target.WorkloadGeneration)
			}
			startupTargetKeys = append(
				startupTargetKeys,
				target.WorkloadID+"\x00"+target.Role+"\x00"+
					strconv.FormatUint(target.WorkloadGeneration, 10),
			)
			if target.WorkloadID == "image-admission" ||
				target.WorkloadID == "image-promotion" {
				t.Fatalf(
					"реестр %s включил динамическую цель %s в startup readback",
					testCase.relative,
					target.WorkloadID,
				)
			}
		}
		if !sort.StringsAreSorted(startupTargetKeys) {
			t.Fatalf("реестр %s вернул недетерминированный порядок startup readback", testCase.relative)
		}
	}
}

// TestRegistryRequiresExplicitStartupReadbackPolicy removes one startup flag from a canonical registry
// and requires loader rejection rather than treating omission as false.
// TestRegistryRequiresExplicitStartupReadbackPolicy удаляет один флаг проверки при запуске из
// канонического реестра и требует отказ загрузчика вместо трактовки отсутствия как false.
func TestRegistryRequiresExplicitStartupReadbackPolicy(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь теста")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "..", ".."))
	source, err := os.ReadFile(filepath.Join(
		repositoryRoot,
		"deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml",
	))
	if err != nil {
		t.Fatalf("прочитать канонический реестр: %v", err)
	}
	mutated := strings.Replace(
		string(source),
		"    startup_readback_required: true\n",
		"",
		1,
	)
	projected := filepath.Join(t.TempDir(), "key-delivery-targets.yaml")
	if err := os.WriteFile(projected, []byte(mutated), 0o444); err != nil {
		t.Fatalf("материализовать поврежденный реестр: %v", err)
	}
	if _, err := LoadRegistry(projected); err == nil {
		t.Fatal("реестр без явной startup readback policy принят")
	}
}
