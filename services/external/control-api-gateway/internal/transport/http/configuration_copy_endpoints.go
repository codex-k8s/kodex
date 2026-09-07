package httptransport

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

var configurationDefinitionVersion = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)

// Union generator не проверяет additionalProperties: boundary декодирует
// закрытую форму повторно, включая запрет null/неоднозначных source selectors.
func configurationCopyFields(value json.Marshaler, role bool) (map[string]json.RawMessage, bool) {
	raw, err := value.MarshalJSON()
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(raw, &fields) != nil || fields == nil {
		return nil, false
	}
	for key, value := range fields {
		if bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return nil, false
		}
		switch key {
		case "name", "configurationRef":
		case "projectRef", "recipeRef":
			if !role {
				return nil, false
			}
		case "shipped":
			if role {
				return nil, false
			}
		default:
			return nil, false
		}
	}
	_, configured := fields["configurationRef"]
	_, other := fields["shipped"]
	if role {
		_, other = fields["recipeRef"]
	}
	return fields, configured != other
}

func configurationCopyString(fields map[string]json.RawMessage, key string) string {
	var value string
	if json.Unmarshal(fields[key], &value) != nil {
		return ""
	}
	return value
}

func configurationCopyMutation(w http.ResponseWriter, key, etag string) (*cp.MutationContext, bool) {
	if etag == "" {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return requireMutation(w, key, etag)
}

func (server *Server) CopyRoleImageConfiguration(w http.ResponseWriter, r *http.Request, p generated.CopyRoleImageConfigurationParams) {
	body, ok := decodeJSON[generated.RoleImageConfigurationCopyInput](w, r)
	if !ok {
		return
	}
	fields, ok := configurationCopyFields(body, true)
	name, project := configurationCopyString(fields, "name"), configurationCopyString(fields, "projectRef")
	ref := configurationCopyString(fields, "configurationRef")
	recipe := configurationCopyString(fields, "recipeRef")
	if !ok || strings.TrimSpace(name) == "" || len(name) > 160 || !opaqueHTTPReference.MatchString(project) || !(opaqueHTTPReference.MatchString(ref) || opaqueHTTPReference.MatchString(recipe)) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	r, ok = withProjectReference(w, r, project)
	if !ok {
		return
	}
	mutation, ok := configurationCopyMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	request := &cp.CopyRoleImageConfigurationRequest{Mutation: mutation, ProjectRef: project, Name: name}
	if recipe != "" {
		request.Source = &cp.CopyRoleImageConfigurationRequest_RecipeRef{RecipeRef: recipe}
	} else {
		request.Source = &cp.CopyRoleImageConfigurationRequest_ConfigurationRef{ConfigurationRef: ref}
	}
	result, err := server.control.Command.CopyRoleImageConfiguration(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if result == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

func (server *Server) CopyIntegrationDefinitionConfiguration(w http.ResponseWriter, r *http.Request, p generated.CopyIntegrationDefinitionConfigurationParams) {
	body, ok := decodeJSON[generated.IntegrationDefinitionConfigurationCopyInput](w, r)
	if !ok {
		return
	}
	fields, ok := configurationCopyFields(body, false)
	name, ref := configurationCopyString(fields, "name"), configurationCopyString(fields, "configurationRef")
	if !ok || strings.TrimSpace(name) == "" || len(name) > 160 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	request := &cp.CopyIntegrationDefinitionConfigurationRequest{Name: name}
	if raw, present := fields["shipped"]; present {
		var source generated.ShippedIntegrationDefinitionCopySource
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&source) != nil || strings.TrimSpace(source.Key) == "" || len(source.Key) > 100 || len(source.DefinitionVersion) > 32 || !configurationDefinitionVersion.MatchString(source.DefinitionVersion) || !validManagedDigest(source.Digest) {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		request.Source = &cp.CopyIntegrationDefinitionConfigurationRequest_Shipped{Shipped: &cp.ShippedIntegrationDefinitionCopySource{Key: source.Key, DefinitionVersion: source.DefinitionVersion, Digest: source.Digest}}
	} else {
		if !opaqueHTTPReference.MatchString(ref) {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
			return
		}
		request.Source = &cp.CopyIntegrationDefinitionConfigurationRequest_ConfigurationRef{ConfigurationRef: ref}
	}
	mutation, ok := configurationCopyMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	request.Mutation = mutation
	result, err := server.control.Command.CopyIntegrationDefinitionConfiguration(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if result == nil {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

func (server *Server) ArchiveRoleImageConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.ArchiveRoleImageConfigurationParams) {
	if _, ok := decodeOptionalJSON[struct{}](w, r); !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := configurationCopyMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.ArchiveRoleImageConfiguration(r.Context(), &cp.ArchiveRoleImageConfigurationRequest{Mutation: mutation, ConfigurationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeArchivedConfiguration(w, result.GetConfiguration())
}

func (server *Server) ArchiveIntegrationDefinitionConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.ArchiveIntegrationDefinitionConfigurationParams) {
	if _, ok := decodeOptionalJSON[struct{}](w, r); !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := configurationCopyMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.ArchiveIntegrationDefinitionConfiguration(r.Context(), &cp.ArchiveIntegrationDefinitionConfigurationRequest{Mutation: mutation, ConfigurationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeArchivedConfiguration(w, result.GetConfiguration())
}

func writeArchivedConfiguration(w http.ResponseWriter, value *cp.ManagedConfigurationSet) {
	configuration, err := managedConfigurationView(value)
	if err != nil || !configuration.Archived {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", configuration.Version))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, generated.ManagedConfigurationDetachment{Configuration: configuration})
}
