package integration

import (
	"context"
	"encoding/json"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"strings"
)

// nativeGitHubConfiguration derives private legacy handler coordinates only from the stored selected repository, without accepting caller credentials or changing the frozen work snapshot.
// nativeGitHubConfiguration выводит отдельные legacy координаты лишь из сохранённого selected repository без caller credentials и изменения frozen work snapshot.
func nativeGitHubConfiguration(configuration map[string]string, repositoryRef string) (map[string]string, error) {
	if configuration["scope_kind"] == "" {
		return configuration, nil
	}
	selected, err := integrationpackage.NativeGitHubRepository(configuration, repositoryRef)
	if err != nil || configuration["service_identity"] == "" {
		return nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	result := make(map[string]string, len(configuration)+2)
	for key, value := range configuration {
		result[key] = value
	}
	parts := strings.Split(selected.FullName, "/")
	result["owner"], result["repository"] = parts[0], parts[1]
	return result, nil
}

// requireNativeGitHubPermission reuses fresh authenticated membership and exact repository ID checks for service-only source reads and writeback work.
// requireNativeGitHubPermission повторно использует актуальные authenticated membership и exact repository ID checks для service-only source reads и writeback work.
func (adapter *Adapter) requireNativeGitHubPermission(ctx context.Context, request Request, definition integrationpackage.Package, configuration map[string]string, repositoryRef, operation string) error {
	if configuration["scope_kind"] == "" {
		return nil
	}
	selected, err := integrationpackage.NativeGitHubRepository(configuration, repositoryRef)
	if err != nil {
		return &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	capability, found := definition.Capability(operation)
	if !found {
		return &SafeError{Code: "INTEGRATION_CAPABILITY_UNSUPPORTED"}
	}
	client, cleanup, err := adapter.githubClient(ctx, request.Credential)
	if err != nil {
		return err
	}
	defer cleanup()
	raw, _ := json.Marshal(map[string]any{"repository_id": selected.ID, "repository_full_name": selected.FullName})
	_, _, err = resolveGitHubScopedRepository(ctx, client, request, capability, configuration, raw)
	return err
}
