package httptransport

import (
	"fmt"
	"net/http"
	"path"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// validGitSourceLocation checks byte bounds, combined UTF-8, explicit forbidden characters and Git-ref restrictions, plus a canonical relative POSIX path without traversal. It performs no repository lookup, connection authorization, checkout or filesystem access.
// validGitSourceLocation проверяет byte bounds, общий UTF-8, явно запрещённые символы и ограничения Git ref, а также канонический относительный POSIX path без traversal. Repository lookup, connection authorization, checkout и filesystem access не выполняет.
func validGitSourceLocation(repository, reference, filename string) bool {
	return len(repository) > 0 && len(repository) <= 256 && len(reference) > 0 && len(reference) <= 256 &&
		len(filename) > 0 && len(filename) <= 512 && utf8.ValidString(repository+reference+filename) &&
		!strings.ContainsAny(repository+reference+filename, "\x00\r\n\\") &&
		!strings.ContainsAny(reference, " ~^:?*[") && !strings.Contains(reference, "..") && !strings.Contains(reference, "@{") &&
		!strings.HasPrefix(reference, "-") && !strings.HasSuffix(reference, ".") && !strings.HasSuffix(reference, ".lock") &&
		!strings.HasPrefix(filename, "/") && path.Clean(filename) == filename && filename != "." && filename != ".." && !strings.HasPrefix(filename, "../")
}

// requireGitSourceMutation requires an opaque configuration ref and nonempty ETag, validates optional connection/version/location/format input, then delegates idempotency and version parsing. Syntactic source selectors do not establish permission to read a repository.
// requireGitSourceMutation требует opaque configuration ref и непустой ETag, проверяет необязательные connection/version/location/format и делегирует разбор идемпотентности и версии. Формат source selectors не доказывает право чтения repository.
func requireGitSourceMutation(w http.ResponseWriter, ref, key, etag string, input *cp.ManagedConfigurationGitSourceInput) (*cp.MutationContext, bool) {
	if !opaqueHTTPReference.MatchString(ref) || etag == "" ||
		input != nil && (!opaqueHTTPReference.MatchString(input.ConnectionRef) || !validManagedVersion(input.ExpectedConnectionVersion) ||
			!validGitSourceLocation(input.RepositoryRef, input.RefName, input.Path) || input.ContentFormat != "JSON" && input.ContentFormat != "YAML") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return requireMutation(w, key, etag)
}

// writeGitSourceConfiguration requires a valid summary of the exact kind/ref with advanced version, GIT ownership and QUEUED source, checking supplied connection/repository/ref/path pins when configuring. It returns the version ETag and metadata; queued state is not fetched-content or readiness proof.
// writeGitSourceConfiguration требует допустимый summary точных kind/ref с продвинутой версией, GIT ownership и QUEUED source, при настройке сверяет заданные connection/repository/ref/path pins. Возвращает ETag версии и metadata; queued не доказывает загрузку content и readiness.
func writeGitSourceConfiguration(w http.ResponseWriter, value *cp.ManagedConfigurationSet, ref string, kind cp.ManagedConfigurationKind, mutation *cp.MutationContext, input *cp.ManagedConfigurationGitSourceInput) {
	output, err := managedConfigurationSummaryView(value)
	if err != nil || value.GetRef() != ref || value.GetKind() != kind || value.GetVersion() <= mutation.GetExpectedVersion() ||
		value.GetManagedBy() != cp.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_GIT ||
		value.GetGitSource() == nil || value.GetGitSource().GetState() != cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_QUEUED ||
		input != nil && (value.GetGitSource().GetConnectionRef() != input.ConnectionRef || value.GetGitSource().GetRepositoryRef() != input.RepositoryRef ||
			value.GetGitSource().GetRefName() != input.RefName || value.GetGitSource().GetPath() != input.Path) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", output.Version))
	writeJSON(w, http.StatusOK, output)
}

// ConfigureRoleImageGitSource decodes the role-image source DTO, validates a versioned source mutation and invokes only the dedicated role-image owner command. The returned configuration must retain exact source pins and queued Git ownership; this endpoint does not fetch Git or build an image.
// ConfigureRoleImageGitSource декодирует source DTO образа, проверяет versioned source mutation и вызывает только отдельную role-image команду владельца. Ответ должен сохранить точные source pins и queued Git ownership; endpoint не загружает Git и не собирает образ.
func (server *Server) ConfigureRoleImageGitSource(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.ConfigureRoleImageGitSourceParams) {
	body, ok := decodeJSON[generated.RoleImageGitSourceInput](w, r)
	if !ok {
		return
	}
	input := &cp.ManagedConfigurationGitSourceInput{ConnectionRef: body.ConnectionRef, ExpectedConnectionVersion: body.ExpectedConnectionVersion, RepositoryRef: body.RepositoryRef, RefName: body.RefName, Path: body.Path, ContentFormat: string(body.ContentFormat)}
	mutation, ok := requireGitSourceMutation(w, ref, p.IdempotencyKey, p.IfMatch, input)
	if !ok {
		return
	}
	result, err := server.control.Command.ConfigureRoleImageGitSource(r.Context(), &cp.ConfigureRoleImageGitSourceRequest{Mutation: mutation, ConfigurationRef: ref, Source: input})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeGitSourceConfiguration(w, result.GetConfiguration(), ref, cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE, mutation, input)
}

// RefreshRoleImageGitSource requires version/idempotency and queues the dedicated owner refresh for an existing role-image configuration. The result checks exact kind/ref, advanced version and queued Git source without accepting replacement source fields from the request.
// RefreshRoleImageGitSource требует version/idempotency и ставит отдельный owner refresh существующей role-image конфигурации. Ответ проверяется на точные kind/ref, advanced version и queued Git source без принятия замены source fields из запроса.
func (server *Server) RefreshRoleImageGitSource(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.RefreshRoleImageGitSourceParams) {
	mutation, ok := requireGitSourceMutation(w, ref, p.IdempotencyKey, p.IfMatch, nil)
	if !ok {
		return
	}
	result, err := server.control.Command.RefreshRoleImageGitSource(r.Context(), &cp.RefreshRoleImageGitSourceRequest{Mutation: mutation, ConfigurationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeGitSourceConfiguration(w, result.GetConfiguration(), ref, cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE, mutation, nil)
}

// ConfigureIntegrationDefinitionGitSource maps the integration-definition source DTO to its specialized owner command after bounded source and mutation checks. It validates queued Git-owned result pins rather than reusing a role-image command or treating a repository path as authority.
// ConfigureIntegrationDefinitionGitSource преобразует source DTO integration definition в специализированную owner command после bounded source и mutation checks. Сверяет pins queued Git-owned ответа, не использует role-image command и не считает repository path полномочием.
func (server *Server) ConfigureIntegrationDefinitionGitSource(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.ConfigureIntegrationDefinitionGitSourceParams) {
	body, ok := decodeJSON[generated.IntegrationDefinitionGitSourceInput](w, r)
	if !ok {
		return
	}
	input := &cp.ManagedConfigurationGitSourceInput{ConnectionRef: body.ConnectionRef, ExpectedConnectionVersion: body.ExpectedConnectionVersion, RepositoryRef: body.RepositoryRef, RefName: body.RefName, Path: body.Path, ContentFormat: string(body.ContentFormat)}
	mutation, ok := requireGitSourceMutation(w, ref, p.IdempotencyKey, p.IfMatch, input)
	if !ok {
		return
	}
	result, err := server.control.Command.ConfigureIntegrationDefinitionGitSource(r.Context(), &cp.ConfigureIntegrationDefinitionGitSourceRequest{Mutation: mutation, ConfigurationRef: ref, Source: input})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeGitSourceConfiguration(w, result.GetConfiguration(), ref, cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION, mutation, input)
}

// RefreshIntegrationDefinitionGitSource uses the dedicated integration-definition refresh with exact configuration and mutation pins; the public result must show an advanced queued Git-owned configuration, not a completed import or admitted definition.
// RefreshIntegrationDefinitionGitSource использует отдельный integration-definition refresh с точными configuration и mutation pins; публичный ответ должен показать продвинутую queued Git-owned конфигурацию, не завершённый import и не допущенный definition.
func (server *Server) RefreshIntegrationDefinitionGitSource(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.RefreshIntegrationDefinitionGitSourceParams) {
	mutation, ok := requireGitSourceMutation(w, ref, p.IdempotencyKey, p.IfMatch, nil)
	if !ok {
		return
	}
	result, err := server.control.Command.RefreshIntegrationDefinitionGitSource(r.Context(), &cp.RefreshIntegrationDefinitionGitSourceRequest{Mutation: mutation, ConfigurationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeGitSourceConfiguration(w, result.GetConfiguration(), ref, cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION, mutation, nil)
}
