package integrationpackage

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
)

// GitHubRepositorySelection pins a configured repository to both its immutable provider ID and canonical name.
// GitHubRepositorySelection закрепляет настроенный репозиторий по неизменяемому ID провайдера и каноническому имени.
type GitHubRepositorySelection struct {
	ID       int64  `json:"id"`
	FullName string `json:"full_name"`
}

// GitHubResourceScope expresses an explicit finite selection or the current authenticated user's accessible repositories.
// GitHubResourceScope выражает явный конечный выбор либо актуально доступные аутентифицированному пользователю репозитории.
type GitHubResourceScope struct {
	Kind         string
	Repositories []GitHubRepositorySelection
}

// errGitHubResourceScope rejects malformed, ambiguous and unbounded repository selectors without exposing provider data.
// errGitHubResourceScope отклоняет некорректные, неоднозначные и неограниченные селекторы без раскрытия данных провайдера.
var errGitHubResourceScope = errors.New("GitHub resource scope is invalid")

// ValidGitHubRepositoryName accepts one bounded owner/name pair, excluding URLs, escaped separators and traversal.
// ValidGitHubRepositoryName допускает ограниченную пару owner/name и исключает URL, экранированные разделители и обход пути.
func ValidGitHubRepositoryName(name string) bool {
	parts := strings.Split(name, "/")
	if len(parts) != 2 || len(parts[0]) > 100 || len(parts[1]) > 100 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return false
		}
		for _, c := range part {
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.') {
				return false
			}
		}
	}
	return true
}

// ParseGitHubResourceScope requires explicit mutually exclusive modes; configuration never supplies credential authority.
// ParseGitHubResourceScope требует явных взаимоисключающих режимов; конфигурация не выдаёт полномочий ключа.
func ParseGitHubResourceScope(configuration map[string]string) (GitHubResourceScope, error) {
	scope := GitHubResourceScope{Kind: configuration["scope_kind"]}
	raw, present := configuration["repositories"]
	switch scope.Kind {
	case "ALL_ACCESSIBLE":
		if present {
			return GitHubResourceScope{}, errGitHubResourceScope
		}
	case "SELECTED":
		if !present || len(raw) > 32768 {
			return GitHubResourceScope{}, errGitHubResourceScope
		}
		decoder := json.NewDecoder(bytes.NewBufferString(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&scope.Repositories) != nil || decoder.Decode(new(any)) != io.EOF || len(scope.Repositories) < 1 || len(scope.Repositories) > 100 {
			return GitHubResourceScope{}, errGitHubResourceScope
		}
		ids := map[int64]bool{}
		names := map[string]bool{}
		for _, repo := range scope.Repositories {
			folded := strings.ToLower(repo.FullName)
			if repo.ID < 1 || repo.ID > 9007199254740991 || !ValidGitHubRepositoryName(repo.FullName) || ids[repo.ID] || names[folded] {
				return GitHubResourceScope{}, errGitHubResourceScope
			}
			ids[repo.ID] = true
			names[folded] = true
		}
	default:
		return GitHubResourceScope{}, errGitHubResourceScope
	}
	return scope, nil
}

// AllowsRepository checks both configured coordinates; fresh authenticated provider eligibility must still be established by the adapter.
// AllowsRepository проверяет обе настроенные координаты; адаптер дополнительно подтверждает актуальный доступ через аутентифицированный API провайдера.
func (scope GitHubResourceScope) AllowsRepository(id int64, fullName string) bool {
	if id < 1 || id > 9007199254740991 || !ValidGitHubRepositoryName(fullName) {
		return false
	}
	if scope.Kind == "ALL_ACCESSIBLE" {
		return true
	}
	if scope.Kind != "SELECTED" {
		return false
	}
	for _, repo := range scope.Repositories {
		if repo.ID == id && repo.FullName == fullName {
			return true
		}
	}
	return false
}

// githubScopeValues serializes a validated boundary deterministically; missing selectors never turn into an implicit wildcard.
// githubScopeValues детерминированно сериализует проверенную область; отсутствие селекторов не превращается в неявный wildcard.
func (capability Capability) githubScopeValues(configuration map[string]string) (map[string]string, error) {
	if len(capability.ResourceScope.ConnectionFields) != 2 || capability.ResourceScope.ConnectionFields[0] != "scope_kind" || capability.ResourceScope.ConnectionFields[1] != "repositories" {
		return nil, errGitHubResourceScope
	}
	scope, err := ParseGitHubResourceScope(configuration)
	if err != nil {
		return nil, err
	}
	result := map[string]string{"scope_kind": scope.Kind}
	if scope.Kind == "SELECTED" {
		raw, err := json.Marshal(scope.Repositories)
		if err != nil {
			return nil, errGitHubResourceScope
		}
		result["repositories"] = string(raw)
	}
	return result, nil
}

// GitHubRepositorySelectionKey provides a bounded stable display key without making it a source of permission.
// GitHubRepositorySelectionKey даёт ограниченный устойчивый ключ отображения без выдачи полномочий.
func GitHubRepositorySelectionKey(repo GitHubRepositorySelection) string {
	return strconv.FormatInt(repo.ID, 10) + ":" + repo.FullName
}

// NativeGitHubRepository resolves a stored exact repository for a background configuration source; ALL_ACCESSIBLE cannot silently select an unpinned destination.
// NativeGitHubRepository разрешает сохранённый точный репозиторий фонового источника настройки; ALL_ACCESSIBLE не выбирает незакреплённое назначение.
func NativeGitHubRepository(configuration map[string]string, fullName string) (GitHubRepositorySelection, error) {
	scope, err := ParseGitHubResourceScope(configuration)
	if err != nil || scope.Kind != "SELECTED" {
		return GitHubRepositorySelection{}, errGitHubResourceScope
	}
	for _, repo := range scope.Repositories {
		if strings.EqualFold(repo.FullName, fullName) {
			return repo, nil
		}
	}
	return GitHubRepositorySelection{}, errGitHubResourceScope
}
