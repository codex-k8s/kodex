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

func validGitSourceLocation(repository, reference, filename string) bool {
	return len(repository) > 0 && len(repository) <= 256 && len(reference) > 0 && len(reference) <= 256 &&
		len(filename) > 0 && len(filename) <= 512 && utf8.ValidString(repository+reference+filename) &&
		!strings.ContainsAny(repository+reference+filename, "\x00\r\n\\") &&
		!strings.ContainsAny(reference, " ~^:?*[") && !strings.Contains(reference, "..") && !strings.Contains(reference, "@{") &&
		!strings.HasPrefix(reference, "-") && !strings.HasSuffix(reference, ".") && !strings.HasSuffix(reference, ".lock") &&
		!strings.HasPrefix(filename, "/") && path.Clean(filename) == filename && filename != "." && filename != ".." && !strings.HasPrefix(filename, "../")
}

func requireGitSourceMutation(w http.ResponseWriter, ref, key, etag string, input *cp.ManagedConfigurationGitSourceInput) (*cp.MutationContext, bool) {
	if !opaqueHTTPReference.MatchString(ref) || etag == "" ||
		input != nil && (!opaqueHTTPReference.MatchString(input.ConnectionRef) || !validManagedVersion(input.ExpectedConnectionVersion) ||
			!validGitSourceLocation(input.RepositoryRef, input.RefName, input.Path) || input.ContentFormat != "JSON" && input.ContentFormat != "YAML") {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return requireMutation(w, key, etag)
}

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
