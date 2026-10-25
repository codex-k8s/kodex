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

// configurationDefinitionVersion recognizes a positive-major three-part numeric definition version
// without prerelease/build suffixes. Other source identity and digest checks remain separate.
// configurationDefinitionVersion распознаёт трёхчастную числовую версию определения с
// положительным major без prerelease/build суффиксов. Остальные проверки identity и digest
// источника выполняются отдельно.
var configurationDefinitionVersion = regexp.MustCompile(`^[1-9][0-9]*\.[0-9]+\.[0-9]+$`)

// configurationCopyFields rechecks the generated union JSON for allowed outer keys, forbidden null
// values and exactly one configuration versus recipe/shipped selector. Role-image and
// integration-definition copies have different allowed fields; this closes additionalProperties
// gaps in the generated union.
// configurationCopyFields повторно проверяет JSON generated union на допустимые внешние ключи,
// запрет null и ровно один selector configuration либо recipe/shipped. Копирование role image и
// integration definition имеет разные поля; это закрывает пробел additionalProperties generated
// union.
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

// configurationCopyString reads a selected raw JSON field only when it decodes as a string,
// returning empty text for missing or nonstring fields so the caller can apply its field-specific
// requirement.
// configurationCopyString читает выбранное сырое JSON поле только при строковом декодировании,
// возвращая пустой текст для отсутствующего или нестрокового поля для предметной проверки caller.
func configurationCopyString(fields map[string]json.RawMessage, key string) string {
	var value string
	if json.Unmarshal(fields[key], &value) != nil {
		return ""
	}
	return value
}

// configurationCopyMutation requires a nonempty If-Match before delegating mutation-header
// parsing. Missing version selection produces a local 400; owner lookup and source-version
// enforcement remain in the command.
// configurationCopyMutation требует непустой If-Match перед общим разбором mutation заголовков.
// Отсутствие выбора версии даёт локальный 400; поиск владельца и проверка версии источника
// остаются у команды.
func configurationCopyMutation(w http.ResponseWriter, key, etag string) (*cp.MutationContext, bool) {
	if etag == "" {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return requireMutation(w, key, etag)
}

// CopyRoleImageConfiguration validates a named project copy with exactly one recipe/configuration
// source, attaches project routing and forwards the source oneof with mutation headers. Only a
// nonnil owner result is projected as 201; requested source refs grant no access.
// CopyRoleImageConfiguration проверяет именованную проектную копию с одним источником
// recipe/configuration, добавляет проектную маршрутизацию и передаёт source oneof с mutation
// заголовками. Только ненулевой ответ владельца проецируется как 201; refs источника доступа не
// дают.
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

// CopyIntegrationDefinitionConfiguration accepts either an existing configuration ref or a
// strictly shaped shipped definition key/version/digest, plus a bounded name and mutation headers.
// The specialized owner command creates the copy; this gateway never detaches Git ownership
// itself.
// CopyIntegrationDefinitionConfiguration принимает ref существующей конфигурации либо строгие
// key/version/digest встроенного определения с ограниченным именем и mutation заголовками. Копию
// создаёт специализированная команда владельца; gateway сам не снимает Git владение.
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

// configurationArchiveMutation validates optional empty JSON, the opaque configuration locator
// and versioned mutation headers in that exact order. It returns no mutation after a local
// refusal and neither chooses the specialized owner command nor authorizes the locator itself.
// configurationArchiveMutation проверяет необязательный пустой JSON, opaque locator конфигурации
// и versioned mutation заголовки именно в таком порядке. После локального отказа mutation
// не возвращается; специализированную команду владельца и допуск locator helper не выбирает.
func configurationArchiveMutation(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, key, etag string) (*cp.MutationContext, bool) {
	if _, ok := decodeOptionalJSON[struct{}](w, r); !ok {
		return nil, false
	}
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return configurationCopyMutation(w, key, etag)
}

// ArchiveRoleImageConfiguration accepts an optional empty body and a valid configuration locator,
// requires mutation/version selection and delegates role-image configuration archival to its
// specialized owner command.
// ArchiveRoleImageConfiguration принимает необязательное пустое тело и корректный locator
// конфигурации, требует mutation и выбор версии и поручает архивирование конфигурации role image
// специализированной команде владельца.
func (server *Server) ArchiveRoleImageConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.ArchiveRoleImageConfigurationParams) {
	mutation, ok := configurationArchiveMutation(w, r, ref, p.IdempotencyKey, p.IfMatch)
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

// ArchiveIntegrationDefinitionConfiguration validates the optional empty body, configuration
// locator and mutation headers before delegating integration-definition configuration archival; it
// does not share a generic command with role-image lifecycle.
// ArchiveIntegrationDefinitionConfiguration проверяет необязательное пустое тело, locator
// конфигурации и mutation заголовки перед архивированием integration definition владельцем; общий
// generic command с lifecycle role image не используется.
func (server *Server) ArchiveIntegrationDefinitionConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.ArchiveIntegrationDefinitionConfigurationParams) {
	mutation, ok := configurationArchiveMutation(w, r, ref, p.IdempotencyKey, p.IfMatch)
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

// writeArchivedConfiguration requires a valid owner configuration view with Archived true, then
// emits the configuration envelope with its version ETag and no-store policy. An unexpected active
// result becomes 502.
// writeArchivedConfiguration требует корректную проекцию конфигурации владельца с Archived true,
// затем выдаёт envelope с ETag версии и no-store. Неожиданно активный результат даёт 502.
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
