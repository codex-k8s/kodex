package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) SavePromptTemplateDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.SavePromptTemplateDraftParams) {
	body, mutation, ok := readManagedDraftSave(w, r, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE)
	if !ok {
		return
	}
	result, err := server.control.Command.SavePromptTemplateDraft(r.Context(), &controlplanev1.SavePromptTemplateDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef, ContentFormat: string(body.ContentFormat), Content: *body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE, &body)
}

func (server *Server) DiscardPromptTemplateDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.DiscardPromptTemplateDraftParams) {
	mutation, ok := managedDraftMutation(w, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.DiscardPromptTemplateDraft(r.Context(), &controlplanev1.DiscardPromptTemplateDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE, nil)
}

func (server *Server) SaveRoleImageRevisionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.SaveRoleImageRevisionDraftParams) {
	body, mutation, ok := readManagedDraftSave(w, r, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE)
	if !ok {
		return
	}
	result, err := server.control.Command.SaveRoleImageRevisionDraft(r.Context(), &controlplanev1.SaveRoleImageRevisionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef, ContentFormat: string(body.ContentFormat), Content: *body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE, &body)
}

func (server *Server) DiscardRoleImageRevisionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.DiscardRoleImageRevisionDraftParams) {
	mutation, ok := managedDraftMutation(w, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.DiscardRoleImageRevisionDraft(r.Context(), &controlplanev1.DiscardRoleImageRevisionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE, nil)
}

func (server *Server) SaveIntegrationDefinitionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.SaveIntegrationDefinitionDraftParams) {
	body, mutation, ok := readManagedDraftSave(w, r, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION)
	if !ok {
		return
	}
	result, err := server.control.Command.SaveIntegrationDefinitionDraft(r.Context(), &controlplanev1.SaveIntegrationDefinitionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef, ContentFormat: string(body.ContentFormat), Content: *body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION, &body)
}

func (server *Server) DiscardIntegrationDefinitionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.DiscardIntegrationDefinitionDraftParams) {
	mutation, ok := managedDraftMutation(w, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.DiscardIntegrationDefinitionDraft(r.Context(), &controlplanev1.DiscardIntegrationDefinitionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION, nil)
}

func (server *Server) SaveSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.SaveSystemSTTConfigurationDraftParams) {
	body, mutation, ok := readManagedDraftSave(w, r, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch, controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT)
	if !ok {
		return
	}
	result, err := server.control.Command.SaveSystemSTTConfigurationDraft(r.Context(), &controlplanev1.SaveSystemSTTConfigurationDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef, ContentFormat: string(body.ContentFormat), Content: *body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT, &body)
}

func (server *Server) DiscardSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.DiscardSystemSTTConfigurationDraftParams) {
	mutation, ok := managedDraftMutation(w, configurationRef, revisionRef, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.DiscardSystemSTTConfigurationDraft(r.Context(), &controlplanev1.DiscardSystemSTTConfigurationDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedDraftResult(w, result, configurationRef, revisionRef, mutation.GetExpectedVersion(), controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_SYSTEM_STT, nil)
}

func managedDraftMutation(w http.ResponseWriter, configurationRef, revisionRef, key, etag string) (*controlplanev1.MutationContext, bool) {
	if !opaqueHTTPReference.MatchString(configurationRef) || !opaqueHTTPReference.MatchString(revisionRef) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return nil, false
	}
	return requireVersionedMutation(w, key, etag)
}

func readManagedDraftSave(w http.ResponseWriter, r *http.Request, configurationRef, revisionRef, key, etag string, kind controlplanev1.ManagedConfigurationKind) (generated.ManagedConfigurationDraftSaveInput, *controlplanev1.MutationContext, bool) {
	body, ok := decodeJSON[generated.ManagedConfigurationDraftSaveInput](w, r)
	if !ok {
		return body, nil, false
	}
	validFormat := body.ContentFormat == "JSON" || body.ContentFormat == "YAML" || body.ContentFormat == "TOML"
	if kind == controlplanev1.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_PROMPT_TEMPLATE {
		validFormat = body.ContentFormat == "TEXT"
	}
	if !validFormat || body.Content == nil || len(*body.Content) > 256<<10 || !utf8.ValidString(*body.Content) || strings.ContainsRune(*body.Content, 0) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return body, nil, false
	}
	mutation, ok := managedDraftMutation(w, configurationRef, revisionRef, key, etag)
	return body, mutation, ok
}

func writeManagedDraftResult(w http.ResponseWriter, result managedConfigurationResponse, configurationRef, revisionRef string, version int64, kind controlplanev1.ManagedConfigurationKind, saved *generated.ManagedConfigurationDraftSaveInput) {
	configuration, revision := result.GetConfiguration(), result.GetRevision()
	valid := configuration.GetRef() == configurationRef && configuration.GetVersion() == version+1 &&
		configuration.GetKind() == kind && configuration.GetManagedBy() == controlplanev1.ManagedConfigurationOwner_MANAGED_CONFIGURATION_OWNER_UI
	if current := configuration.GetCurrentRevision(); current != nil {
		valid = valid && current.GetState() == controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_PUBLISHED && current.GetRef() != revisionRef
	}
	if saved == nil {
		valid = valid && revision.GetRef() == revisionRef && revision.GetState() == controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DISCARDED
	} else {
		content := strings.TrimSpace(*saved.Content)
		digest := sha256.Sum256([]byte(content))
		valid = valid && opaqueHTTPReference.MatchString(revision.GetRef()) && revision.GetRef() != revisionRef &&
			revision.GetParentRevisionRef() == revisionRef && revision.GetState() == controlplanev1.ManagedConfigurationState_MANAGED_CONFIGURATION_STATE_DRAFT &&
			revision.GetContentFormat() == string(saved.ContentFormat) && revision.GetContent() == content && revision.GetDigest() == hex.EncodeToString(digest[:])
	}
	if !valid {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}
