package publisher

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCanonicalDeliveryRegistriesLoad(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь теста")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "..", ".."))
	for _, testCase := range []struct {
		relative             string
		wantStartupReadbacks int
	}{
		{
			relative:             "deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml",
			wantStartupReadbacks: 8,
		},
		{
			relative:             "deploy/k8s/profiles/web-with-mattermost/key-delivery-targets.yaml",
			wantStartupReadbacks: 8,
		},
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
		if len(registry.Targets) == 0 {
			t.Fatalf("реестр %s не содержит целей", testCase.relative)
		}
		if got := registry.StartupReadbackTargetCount(); got != testCase.wantStartupReadbacks {
			t.Fatalf(
				"реестр %s содержит %d обязательных startup readback, ожидалось %d",
				testCase.relative,
				got,
				testCase.wantStartupReadbacks,
			)
		}
	}
}

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
