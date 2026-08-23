package publisher

import (
	"path/filepath"
	"runtime"
	"testing"
)

func TestCanonicalDeliveryRegistriesLoad(t *testing.T) {
	t.Parallel()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("не удалось определить путь теста")
	}
	repositoryRoot := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", "..", "..", ".."))
	for _, relative := range []string{
		"deploy/k8s/base/internal-rpc-authority-publisher/key-delivery-targets.yaml",
		"deploy/k8s/profiles/web-with-mattermost/key-delivery-targets.yaml",
	} {
		registry, err := LoadRegistry(filepath.Join(repositoryRoot, relative))
		if err != nil {
			t.Fatalf("реестр %s не прошёл runtime-валидацию: %v", relative, err)
		}
		if len(registry.Targets) == 0 {
			t.Fatalf("реестр %s не содержит целей", relative)
		}
	}
}
