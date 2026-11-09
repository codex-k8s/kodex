package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
)

// gitLabResolvedProjectKey is an invocation-local derived value, never an accepted deployment setting or model field; native calls use the verified numeric identity after resolution.
// gitLabResolvedProjectKey — invocation-local производное значение, никогда не deployment setting либо model field; native calls после resolution используют проверенную numeric identity.
const gitLabResolvedProjectKey = "__kodex_resolved_project_id"

// maxGitLabScopedProjectID preserves exact numeric identity through the existing JSON/Struct transport instead of rounding an unsupported larger identifier.
// maxGitLabScopedProjectID сохраняет exact numeric identity в существующем JSON/Struct transport вместо округления неподдержанного большего identifier.
const maxGitLabScopedProjectID int64 = 9007199254740991

// gitLabScopedProjectMetadata binds a provider project to its canonical namespace, rather than trusting a caller-selected path alone.
// gitLabScopedProjectMetadata связывает проект провайдера с каноническим namespace, не доверяя одному пути, выбранному caller.
type gitLabScopedProjectMetadata struct {
	ID                int64  `json:"id"`
	PathWithNamespace string `json:"path_with_namespace"`
	Namespace         struct {
		ID       int64  `json:"id"`
		FullPath string `json:"full_path"`
		Kind     string `json:"kind"`
	} `json:"namespace"`
}

// gitLabScopedGroupMetadata contains the fresh configured group's identity; a renamed or substituted group cannot widen an old connection.
// gitLabScopedGroupMetadata содержит свежую identity настроенной группы; переименованная или подменённая группа не расширяет прежнее подключение.
type gitLabScopedGroupMetadata struct {
	ID       int64  `json:"id"`
	FullPath string `json:"full_path"`
}

// resolveGitLabScopedProject checks lexical boundaries before HTTP, then authenticates canonical group/project metadata on the configured origin.
// resolveGitLabScopedProject проверяет lexical boundary до HTTP, затем получает canonical group/project metadata с авторизацией на configured origin.
// It returns private copies for existing typed handlers and strips only the typed project selector; request input and receipt pins remain unchanged.
// Возвращаются отдельные копии для прежних typed handlers, удаляется лишь typed project selector; исходные input и receipt pins не меняются.
func (adapter *Adapter) resolveGitLabScopedProject(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, canonicalInput []byte) (map[string]string, []byte, error) {
	scope, err := integrationpackage.ParseGitLabResourceScope(configuration)
	if err != nil {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	var input map[string]json.RawMessage
	var selected string
	var selectedID int64
	if json.Unmarshal(canonicalInput, &input) != nil || input == nil || json.Unmarshal(input["project_path"], &selected) != nil || selected == "" || json.Unmarshal(input["project_id"], &selectedID) != nil || selectedID < 1 || selectedID > maxGitLabScopedProjectID {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	selected, err = scope.SelectProject(selected)
	if err != nil {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	var group gitLabScopedGroupMetadata
	if scope.Kind == "GROUP" {
		group, err = adapter.readGitLabScopedGroup(ctx, request, capability, configuration, scope)
		if err != nil {
			return nil, nil, err
		}
	}
	body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet, "/api/v4/projects/"+url.PathEscape(selected), nil, nil, "")
	if err != nil {
		return nil, nil, err
	}
	var project gitLabScopedProjectMetadata
	if decodeProviderJSON(body, &project) != nil || project.ID != selectedID || project.PathWithNamespace != selected || !validGitLabScopedProject(scope, group, project) {
		return nil, nil, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	selectedConfiguration := make(map[string]string, len(configuration)+1)
	for key, value := range configuration {
		selectedConfiguration[key] = value
	}
	selectedConfiguration["project_path"] = selected
	selectedConfiguration[gitLabResolvedProjectKey] = strconv.FormatInt(project.ID, 10)
	delete(input, "project_path")
	delete(input, "project_id")
	providerInput, err := json.Marshal(input)
	if err != nil {
		return nil, nil, &SafeError{Code: "INTEGRATION_REQUEST_REJECTED"}
	}
	return selectedConfiguration, providerInput, nil
}

// readGitLabScopedGroup resolves only the configured group through a fixed GET and rejects identity aliases, missing IDs and path changes.
// readGitLabScopedGroup получает только настроенную группу фиксированным GET и отклоняет identity aliases, отсутствующие IDs и изменение пути.
func (adapter *Adapter) readGitLabScopedGroup(ctx context.Context, request Request, capability integrationpackage.Capability, configuration map[string]string, scope integrationpackage.GitLabResourceScope) (gitLabScopedGroupMetadata, error) {
	body, err := adapter.gitLabJSON(ctx, request, capability, configuration, http.MethodGet, "/api/v4/groups/"+url.PathEscape(scope.GroupPath), nil, nil, "")
	if err != nil {
		return gitLabScopedGroupMetadata{}, err
	}
	var group gitLabScopedGroupMetadata
	if decodeProviderJSON(body, &group) != nil || group.ID < 1 || group.FullPath != scope.GroupPath {
		return gitLabScopedGroupMetadata{}, &SafeError{Code: "INTEGRATION_RESPONSE_INVALID"}
	}
	return group, nil
}
