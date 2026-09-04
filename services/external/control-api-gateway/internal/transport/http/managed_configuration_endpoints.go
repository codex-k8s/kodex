package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) CreatePromptTemplateDraft(w http.ResponseWriter, r *http.Request, p generated.CreatePromptTemplateDraftParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationDraftInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireManagedDraftMutation(w, p.IdempotencyKey, stringValue(p.IfMatch), body)
	if !ok {
		return
	}
	if body.ProjectRef != nil {
		r, ok = withProjectReference(w, r, *body.ProjectRef)
		if !ok {
			return
		}
	}
	result, err := server.control.Command.CreatePromptTemplateDraft(r.Context(), &controlplanev1.CreatePromptTemplateDraftRequest{
		Mutation: mutation, ConfigurationRef: stringValue(body.ConfigurationRef), ProjectRef: stringValue(body.ProjectRef),
		Name: body.Name, ContentFormat: string(body.ContentFormat), Content: body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

func (server *Server) ValidatePromptTemplateDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.ValidatePromptTemplateDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.ValidatePromptTemplateDraft(r.Context(), &controlplanev1.ValidatePromptTemplateDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) PublishPromptTemplateDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.PublishPromptTemplateDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.PublishPromptTemplateDraft(r.Context(), &controlplanev1.PublishPromptTemplateDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) RebindPromptTemplateConsumers(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.RebindPromptTemplateConsumersParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationRebindInput](w, r)
	if !ok {
		return
	}
	consumers, ok := managedConsumerInput(w, body)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.RebindPromptTemplateConsumers(r.Context(), &controlplanev1.RebindPromptTemplateConsumersRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
		ImpactDigest: body.ImpactDigest, Consumers: consumers,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) CreateRoleImageRevisionDraft(w http.ResponseWriter, r *http.Request, p generated.CreateRoleImageRevisionDraftParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationDraftInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireManagedDraftMutation(w, p.IdempotencyKey, stringValue(p.IfMatch), body)
	if !ok {
		return
	}
	if body.ProjectRef != nil {
		r, ok = withProjectReference(w, r, *body.ProjectRef)
		if !ok {
			return
		}
	}
	result, err := server.control.Command.CreateRoleImageRevisionDraft(r.Context(), &controlplanev1.CreateRoleImageRevisionDraftRequest{
		Mutation: mutation, ConfigurationRef: stringValue(body.ConfigurationRef), ProjectRef: stringValue(body.ProjectRef),
		Name: body.Name, ContentFormat: string(body.ContentFormat), Content: body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

func (server *Server) ValidateRoleImageRevisionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.ValidateRoleImageRevisionDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.ValidateRoleImageRevisionDraft(r.Context(), &controlplanev1.ValidateRoleImageRevisionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) PublishRoleImageRevisionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.PublishRoleImageRevisionDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.PublishRoleImageRevisionDraft(r.Context(), &controlplanev1.PublishRoleImageRevisionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) RebindRoleImageConsumers(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.RebindRoleImageConsumersParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationRebindInput](w, r)
	if !ok {
		return
	}
	consumers, ok := managedConsumerInput(w, body)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.RebindRoleImageConsumers(r.Context(), &controlplanev1.RebindRoleImageConsumersRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
		ImpactDigest: body.ImpactDigest, Consumers: consumers,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) CreateIntegrationDefinitionDraft(w http.ResponseWriter, r *http.Request, p generated.CreateIntegrationDefinitionDraftParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationDraftInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireManagedDraftMutation(w, p.IdempotencyKey, stringValue(p.IfMatch), body)
	if !ok {
		return
	}
	if body.ProjectRef != nil {
		r, ok = withProjectReference(w, r, *body.ProjectRef)
		if !ok {
			return
		}
	}
	result, err := server.control.Command.CreateIntegrationDefinitionDraft(r.Context(), &controlplanev1.CreateIntegrationDefinitionDraftRequest{
		Mutation: mutation, ConfigurationRef: stringValue(body.ConfigurationRef), ProjectRef: stringValue(body.ProjectRef),
		Name: body.Name, ContentFormat: string(body.ContentFormat), Content: body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

func (server *Server) ValidateIntegrationDefinitionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.ValidateIntegrationDefinitionDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.ValidateIntegrationDefinitionDraft(r.Context(), &controlplanev1.ValidateIntegrationDefinitionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) PublishIntegrationDefinitionDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.PublishIntegrationDefinitionDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.PublishIntegrationDefinitionDraft(r.Context(), &controlplanev1.PublishIntegrationDefinitionDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) RebindIntegrationDefinitionConsumers(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.RebindIntegrationDefinitionConsumersParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationRebindInput](w, r)
	if !ok {
		return
	}
	consumers, ok := managedConsumerInput(w, body)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.RebindIntegrationDefinitionConsumers(r.Context(), &controlplanev1.RebindIntegrationDefinitionConsumersRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
		ImpactDigest: body.ImpactDigest, Consumers: consumers,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) CreateSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, p generated.CreateSystemSTTConfigurationDraftParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationDraftInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireManagedDraftMutation(w, p.IdempotencyKey, stringValue(p.IfMatch), body)
	if !ok {
		return
	}
	if body.ProjectRef != nil {
		r, ok = withProjectReference(w, r, *body.ProjectRef)
		if !ok {
			return
		}
	}
	result, err := server.control.Command.CreateSystemSTTConfigurationDraft(r.Context(), &controlplanev1.CreateSystemSTTConfigurationDraftRequest{
		Mutation: mutation, ConfigurationRef: stringValue(body.ConfigurationRef), ProjectRef: stringValue(body.ProjectRef),
		Name: body.Name, ContentFormat: string(body.ContentFormat), Content: body.Content,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

func (server *Server) ValidateSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.ValidateSystemSTTConfigurationDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.ValidateSystemSTTConfigurationDraft(r.Context(), &controlplanev1.ValidateSystemSTTConfigurationDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) PublishSystemSTTConfigurationDraft(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.PublishSystemSTTConfigurationDraftParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.PublishSystemSTTConfigurationDraft(r.Context(), &controlplanev1.PublishSystemSTTConfigurationDraftRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) RebindSystemSTTConsumers(w http.ResponseWriter, r *http.Request, configurationRef generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef, p generated.RebindSystemSTTConsumersParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationRebindInput](w, r)
	if !ok {
		return
	}
	consumers, ok := managedConsumerInput(w, body)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.RebindSystemSTTConsumers(r.Context(), &controlplanev1.RebindSystemSTTConsumersRequest{
		Mutation: mutation, ConfigurationRef: configurationRef, RevisionRef: revisionRef,
		ImpactDigest: body.ImpactDigest, Consumers: consumers,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusOK, result)
}

func (server *Server) DetachGitManagedConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.DetachGitManagedConfigurationParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.DetachGitManagedConfiguration(r.Context(), &controlplanev1.DetachGitManagedConfigurationRequest{Mutation: mutation, ConfigurationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	configuration, err := managedConfigurationView(result.GetConfiguration())
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	writeJSON(w, http.StatusOK, generated.ManagedConfigurationDetachment{Configuration: configuration})
}

func (server *Server) CopyGitManagedConfiguration(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.CopyGitManagedConfigurationParams) {
	body, ok := decodeJSON[generated.ManagedConfigurationCopyInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	result, err := server.control.Command.CopyGitManagedConfiguration(r.Context(), &controlplanev1.CopyGitManagedConfigurationRequest{Mutation: mutation, ConfigurationRef: ref, Name: body.Name})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeManagedResult(w, http.StatusCreated, result)
}

func (server *Server) ListManagedConfigurationHistory(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, p generated.ListManagedConfigurationHistoryParams) {
	result, err := server.control.Query.ListManagedConfigurationHistory(r.Context(), &controlplanev1.ListManagedConfigurationHistoryRequest{ConfigurationRef: ref, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	configuration, err := managedConfigurationView(result.GetConfiguration())
	if err != nil || result.GetTotal() < 0 || result.GetTotal() > maximumSafeJSONInteger || len(result.GetRevisions()) > 50 {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	output := generated.ManagedConfigurationHistory{Configuration: configuration, Total: result.GetTotal(), Items: make([]generated.ManagedConfigurationRevision, 0, len(result.GetRevisions()))}
	for _, revision := range result.GetRevisions() {
		item, err := managedRevisionView(revision)
		if err != nil {
			writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
			return
		}
		output.Items = append(output.Items, item)
	}
	output.NextPageToken = optionalManagedString(result.GetPage().GetNextPageToken())
	writeJSON(w, http.StatusOK, output)
}

func (server *Server) GetManagedConfigurationImpact(w http.ResponseWriter, r *http.Request, ref generated.ConfigurationRef, revisionRef generated.ConfigurationRevisionRef) {
	result, err := server.control.Query.GetManagedConfigurationImpact(r.Context(), &controlplanev1.GetManagedConfigurationImpactRequest{ConfigurationRef: ref, RevisionRef: revisionRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	impact := result.GetImpact()
	if impact == nil || impact.GetConfigurationRef() != ref || impact.GetTargetRevisionRef() != revisionRef || !validManagedDigest(impact.GetDigest()) || len(impact.GetConsumers()) > 1000 {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	output := generated.ManagedConfigurationImpact{ConfigurationRef: ref, TargetRevisionRef: revisionRef, Digest: impact.GetDigest(), Consumers: make([]generated.ManagedConfigurationConsumer, 0, len(impact.GetConsumers()))}
	for _, consumer := range impact.GetConsumers() {
		item, err := managedConsumerView(consumer)
		if err != nil {
			writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
			return
		}
		output.Consumers = append(output.Consumers, item)
	}
	writeJSON(w, http.StatusOK, output)
}

func (server *Server) GetSystemSTTConfiguration(w http.ResponseWriter, r *http.Request) {
	result, err := server.control.Query.GetSystemSTTConfiguration(r.Context(), &controlplanev1.GetSystemSTTConfigurationRequest{})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	value := result.GetConfiguration()
	if value == nil || !validManagedVersion(value.GetRevision()) || value.GetProviderCredentialGeneration() > uint64(maximumSafeJSONInteger) || value.GetPermissionKey() != "platform.stt.use" {
		writeLocalProblem(w, http.StatusBadGateway, "INTERNAL", false)
		return
	}
	writeJSON(w, http.StatusOK, generated.SystemSTTConfiguration{
		ConfigurationRef: value.GetConfigurationRef(), RevisionRef: value.GetRevisionRef(), Revision: value.GetRevision(), Digest: value.GetDigest(),
		ProviderAccountRef: value.GetProviderAccountRef(), ProviderCredentialGeneration: int64(value.GetProviderCredentialGeneration()),
		Model: value.GetModel(), Language: value.GetLanguage(), PermissionKey: generated.SystemSTTConfigurationPermissionKey(value.GetPermissionKey()),
		Ready: value.GetReady(), ReadinessBlockers: append([]string{}, value.GetReadinessBlockers()...),
	})
}
