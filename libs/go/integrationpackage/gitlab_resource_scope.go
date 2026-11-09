package integrationpackage

import (
	"errors"
	"strings"
)

// GitLabResourceScope stores a validated owner selection; it never grants provider credentials or trusts a model-supplied namespace claim.
// GitLabResourceScope хранит проверенный выбор владельца; он не выдаёт provider credentials и не доверяет namespace из ответа модели.
type GitLabResourceScope struct {
	Kind             string
	ProjectPath      string
	GroupPath        string
	IncludeSubgroups bool
}

// errGitLabResourceScope is a bounded refusal for ambiguous selectors, unknown modes or paths outside a configured boundary.
// errGitLabResourceScope — ограниченный отказ при неоднозначном selector, неизвестном режиме или пути вне настроенной области.
var errGitLabResourceScope = errors.New("GitLab resource scope is invalid")

// gitLabScopeValues serializes only the selected, validated boundary; absent optional selectors never mean a wildcard project or a different instance.
// gitLabScopeValues сериализует только выбранную проверенную область; отсутствующие optional selectors не означают wildcard project либо другой instance.
func (capability Capability) gitLabScopeValues(configuration map[string]string) (map[string]string, error) {
	if len(capability.ResourceScope.ConnectionFields) != 5 {
		return nil, errGitLabResourceScope
	}
	expected := map[string]bool{"base_url": true, "scope_kind": true, "project_path": true, "group_path": true, "include_subgroups": true}
	for _, field := range capability.ResourceScope.ConnectionFields {
		if !expected[field] {
			return nil, errGitLabResourceScope
		}
		delete(expected, field)
	}
	scope, err := ParseGitLabResourceScope(configuration)
	if err != nil || configuration["scope_kind"] == "" || configuration["base_url"] == "" {
		return nil, errGitLabResourceScope
	}
	result := map[string]string{"base_url": configuration["base_url"], "scope_kind": scope.Kind}
	switch scope.Kind {
	case "PROJECT":
		result["project_path"] = scope.ProjectPath
	case "GROUP":
		result["group_path"] = scope.GroupPath
		result["include_subgroups"] = "false"
		if scope.IncludeSubgroups {
			result["include_subgroups"] = "true"
		}
	}
	return result, nil
}

// ParseGitLabResourceScope preserves the absent-mode legacy project choice and requires explicit, mutually exclusive group or instance selections.
// ParseGitLabResourceScope сохраняет legacy project при отсутствующем режиме и требует явного взаимоисключающего выбора group/instance.
// The caller still validates the package configuration and HTTPS destination; this parser neither performs HTTP nor proves credential permissions.
// Вызывающий код отдельно проверяет package configuration и HTTPS destination; parser не выполняет HTTP и не доказывает права credential.
func ParseGitLabResourceScope(configuration map[string]string) (GitLabResourceScope, error) {
	kind, present := configuration["scope_kind"]
	if !present {
		kind = "PROJECT"
	}
	scope := GitLabResourceScope{Kind: kind}
	_, projectPresent := configuration["project_path"]
	_, groupPresent := configuration["group_path"]
	nested, nestedPresent := configuration["include_subgroups"]
	switch kind {
	case "PROJECT":
		if !projectPresent || groupPresent || nestedPresent || !validGitLabScopePath(configuration["project_path"], true) {
			return GitLabResourceScope{}, errGitLabResourceScope
		}
		scope.ProjectPath = configuration["project_path"]
	case "GROUP":
		if projectPresent || !groupPresent || !validGitLabScopePath(configuration["group_path"], false) || nestedPresent && (nested != "true" && nested != "false") {
			return GitLabResourceScope{}, errGitLabResourceScope
		}
		scope.GroupPath = configuration["group_path"]
		scope.IncludeSubgroups = nested == "true"
	case "INSTANCE":
		if projectPresent || groupPresent || nestedPresent {
			return GitLabResourceScope{}, errGitLabResourceScope
		}
	default:
		return GitLabResourceScope{}, errGitLabResourceScope
	}
	return scope, nil
}

// SelectProject checks a typed project-path selector before provider lookup; group membership and credential authority still require fresh verified API metadata.
// SelectProject проверяет типизированный project-path до provider lookup; принадлежность группе и права credential требуют свежих проверенных API metadata.
func (scope GitLabResourceScope) SelectProject(selected string) (string, error) {
	if scope.Kind == "PROJECT" && selected == "" {
		selected = scope.ProjectPath
	}
	if !validGitLabScopePath(selected, true) {
		return "", errGitLabResourceScope
	}
	switch scope.Kind {
	case "PROJECT":
		if selected != scope.ProjectPath {
			return "", errGitLabResourceScope
		}
	case "GROUP":
		if !scope.AllowsProviderProject(selected, selected[:strings.LastIndexByte(selected, '/')]) {
			return "", errGitLabResourceScope
		}
	case "INSTANCE":
	default:
		return "", errGitLabResourceScope
	}
	return selected, nil
}

// AllowsProviderProject checks canonical project/namespace metadata from the trusted provider, excluding shared foreign projects and misleading string prefixes.
// AllowsProviderProject проверяет канонические project/namespace metadata доверенного провайдера, исключая shared foreign projects и обманчивые строковые префиксы.
// The adapter separately binds this metadata to the exact requested project and configured group response; these strings alone are not authentication.
// Adapter отдельно связывает metadata с точным запрошенным проектом и ответом настроенной группы; сами строки не являются аутентификацией.
func (scope GitLabResourceScope) AllowsProviderProject(projectPath, namespacePath string) bool {
	if !validGitLabScopePath(projectPath, true) || !validGitLabScopePath(namespacePath, false) {
		return false
	}
	last := strings.LastIndexByte(projectPath, '/')
	if last < 1 || projectPath[:last] != namespacePath {
		return false
	}
	switch scope.Kind {
	case "PROJECT":
		return projectPath == scope.ProjectPath && validGitLabScopePath(scope.ProjectPath, true)
	case "GROUP":
		return validGitLabScopePath(scope.GroupPath, false) && (namespacePath == scope.GroupPath || scope.IncludeSubgroups && strings.HasPrefix(namespacePath, scope.GroupPath+"/"))
	case "INSTANCE":
		return true
	default:
		return false
	}
}

// validGitLabScopePath admits bounded slash-separated namespace paths while rejecting URLs, encoded separators, traversal and ambiguous empty segments.
// validGitLabScopePath допускает ограниченные namespace paths с разделителем slash и отклоняет URL, encoded separators, traversal и пустые сегменты.
func validGitLabScopePath(path string, project bool) bool {
	if len(path) == 0 || len(path) > 255 {
		return false
	}
	segments := strings.Split(path, "/")
	if project && len(segments) < 2 {
		return false
	}
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for _, c := range segment {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '.' || c == '-') {
				return false
			}
		}
	}
	return true
}
