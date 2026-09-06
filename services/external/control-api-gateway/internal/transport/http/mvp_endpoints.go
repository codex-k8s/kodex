package httptransport

import (
	"crypto/sha256"
	"errors"
	"image"
	"image/color"
	"image/png"
	"mime"
	"net/http"
	"net/url"
	"strings"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/structpb"
)

const maximumAgentAvatarBytes = 5 << 20

func (server *Server) UploadAgentAvatar(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, ref generated.AgentRef, p generated.UploadAgentAvatarParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "image/png" && mediaType != "image/jpeg" && mediaType != "image/webp" {
		writeLocalProblem(w, http.StatusUnsupportedMediaType, "UNSUPPORTED_MEDIA_TYPE", false)
		return
	}
	if r.ContentLength < 1 {
		writeLocalProblem(w, http.StatusLengthRequired, "CONTENT_LENGTH_REQUIRED", false)
		return
	}
	if r.ContentLength > maximumAgentAvatarBytes {
		writeLocalProblem(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", false)
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	stream, err := server.control.Command.UploadAgentAvatar(r.Context())
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if err = stream.Send(&controlplanev1.UploadAgentAvatarRequest{Part: &controlplanev1.UploadAgentAvatarRequest_Metadata{Metadata: &controlplanev1.UploadAgentAvatarMetadata{
		Mutation: mutation, ProjectRef: projectRef, AgentRef: ref, FileName: p.XFileName,
		MediaType: mediaType, SizeBytes: r.ContentLength,
	}}}); err != nil {
		writeRPCProblem(w, err)
		return
	}
	received, digest, err := forwardArtifactBody(r.Body, r.ContentLength, func(chunk []byte) error {
		return stream.Send(&controlplanev1.UploadAgentAvatarRequest{Part: &controlplanev1.UploadAgentAvatarRequest_Chunk{Chunk: chunk}})
	})
	if errors.Is(err, errArtifactContentLengthMismatch) {
		writeLocalProblem(w, http.StatusBadRequest, "CONTENT_LENGTH_MISMATCH", false)
		return
	}
	if errors.Is(err, errArtifactBodyRead) {
		writeLocalProblem(w, http.StatusBadRequest, "REQUEST_BODY_READ_FAILED", false)
		return
	}
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if err = stream.Send(&controlplanev1.UploadAgentAvatarRequest{Part: &controlplanev1.UploadAgentAvatarRequest_Commit{Commit: &controlplanev1.UploadArtifactCommit{
		SizeBytes: received, Sha256: digest,
	}}}); err != nil {
		writeRPCProblem(w, err)
		return
	}
	response, err := stream.CloseAndRecv()
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "agent", "")
}

func (server *Server) SetAgentAvatar(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.SetAgentAvatarParams) {
	body, ok := decodeJSON[generated.AgentAvatarInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.SetAgentAvatar(r.Context(), &controlplanev1.SetAgentAvatarRequest{
		Mutation: mutation, AgentRef: ref, ArtifactRef: body.ArtifactRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "agent", "")
}

func (server *Server) RemoveAgentAvatar(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.RemoveAgentAvatarParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RemoveAgentAvatar(r.Context(), &controlplanev1.RemoveAgentAvatarRequest{Mutation: mutation, AgentRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "agent", "")
}

func (server *Server) GetAgentAvatarContent(w http.ResponseWriter, r *http.Request, ref generated.AgentRef) {
	response, err := server.control.Query.GetAgent(r.Context(), &controlplanev1.GetAgentRequest{AgentRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	avatar := response.GetAgent().GetAvatar()
	if avatar.GetSource() == controlplanev1.AgentAvatar_SOURCE_ARTIFACT && avatar.GetArtifactRef() != "" {
		target := "/api/v1/artifacts/" + url.PathEscape(avatar.GetArtifactRef()) + "/content?purpose=PREVIEW"
		http.Redirect(w, r, target, http.StatusTemporaryRedirect)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.WriteHeader(http.StatusOK)
	_ = writeFallbackAvatar(w, ref)
}

func writeFallbackAvatar(w http.ResponseWriter, ref string) error {
	digest := sha256.Sum256([]byte("kodex-avatar-v1\x00" + ref))
	foreground := color.NRGBA{R: 48 + digest[0]%144, G: 48 + digest[1]%144, B: 48 + digest[2]%144, A: 255}
	background := color.NRGBA{R: 232 + digest[3]%20, G: 232 + digest[4]%20, B: 232 + digest[5]%20, A: 255}
	bitmap := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			pixel := background
			cellX, cellY := x/16, y/16
			if digest[(cellY*4+cellX)%len(digest)]&1 == 1 {
				pixel = foreground
			}
			bitmap.SetNRGBA(x, y, pixel)
		}
	}
	return png.Encode(w, bitmap)
}

func (server *Server) ListProviderDefinitions(w http.ResponseWriter, r *http.Request, p generated.ListProviderDefinitionsParams) {
	r, ok := catalogRequest(w, r, nil, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	response, err := server.control.Query.ListProviderDefinitions(r.Context(), &controlplanev1.ListProviderDefinitionsRequest{
		Page: page(p.PageSize, p.PageToken), Query: stringValue(p.Query),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	for _, definition := range response.GetDefinitions() {
		if definition == nil {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
		for _, model := range definition.Models {
			if _, valid := modelCapabilityView(model); !valid || model.ProviderDefinitionKey != definition.Key {
				writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
				return
			}
		}
	}
	writeMessage(w, http.StatusOK, response, "", "definitions")
}

func (server *Server) ListProviderAccounts(w http.ResponseWriter, r *http.Request, p generated.ListProviderAccountsParams) {
	usage, usageOK := providerUsageContext(p.UsagePurpose != nil || p.UsageAgentRef != nil || p.UsageRuntimeProfileRef != nil || p.UsageProviderDefinitionKey != nil || p.UsageModel != nil || p.UsageReasoningEffort != nil,
		stringValue(p.UsagePurpose), stringValue(p.UsageAgentRef), stringValue(p.UsageRuntimeProfileRef), stringValue(p.UsageProviderDefinitionKey), stringValue(p.UsageModel), stringValue(p.UsageReasoningEffort))
	state, stateOK := catalogStateFilter(p.State, controlplanev1.ProviderAccountState_value, "PROVIDER_ACCOUNT_STATE_")
	if !stateOK || !usageOK {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ListProviderAccounts(r.Context(), &controlplanev1.ListProviderAccountsRequest{
		Page: page(p.PageSize, p.PageToken), State: controlplanev1.ProviderAccountState(state), Query: stringValue(p.Query), DefinitionKey: stringValue(p.DefinitionKey), UsageContext: usage,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	for _, account := range response.GetAccounts() {
		if !validProviderAccountRead(account, usage) {
			writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
	}
	writeMessage(w, http.StatusOK, response, "", "accounts")
}

func (server *Server) GetProviderAccount(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.GetProviderAccountParams) {
	usage, ok := providerUsageContext(p.UsagePurpose != nil || p.UsageAgentRef != nil || p.UsageRuntimeProfileRef != nil || p.UsageProviderDefinitionKey != nil || p.UsageModel != nil || p.UsageReasoningEffort != nil,
		stringValue(p.UsagePurpose), stringValue(p.UsageAgentRef), stringValue(p.UsageRuntimeProfileRef), stringValue(p.UsageProviderDefinitionKey), stringValue(p.UsageModel), stringValue(p.UsageReasoningEffort))
	if !ok {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.GetProviderAccount(r.Context(), &controlplanev1.GetProviderAccountRequest{AccountRef: ref, UsageContext: usage})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validProviderAccountRead(response.GetAccount(), usage) || response.GetAccount().Ref != ref {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "account", "")
}

func (server *Server) CreateProviderAccount(w http.ResponseWriter, r *http.Request, p generated.CreateProviderAccountParams) {
	body, ok := decodeJSON[generated.ProviderAccountCreateInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Command.CreateProviderAccount(r.Context(), &controlplanev1.CreateProviderAccountRequest{
		Mutation: mutation, DefinitionKey: string(body.DefinitionKey), Name: body.Name,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "account", "")
}

func (server *Server) StartProviderAccountDeviceAuthorization(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.StartProviderAccountDeviceAuthorizationParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.StartProviderAccountDeviceAuthorization(r.Context(), &controlplanev1.StartProviderAccountDeviceAuthorizationRequest{Mutation: mutation, AccountRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusAccepted, response, "account", "")
}

func (server *Server) AuthorizeProviderAccountApiKey(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.AuthorizeProviderAccountApiKeyParams) {
	body, ok := decodeJSON[generated.ProviderApiKeyInput](w, r)
	if !ok || body.ApiKey == nil || strings.TrimSpace(*body.ApiKey) == "" {
		if ok {
			writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		}
		return
	}
	apiKey := []byte(*body.ApiKey)
	*body.ApiKey = ""
	defer func() {
		for index := range apiKey {
			apiKey[index] = 0
		}
	}()
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.AuthorizeProviderAccountAPIKey(r.Context(), &controlplanev1.AuthorizeProviderAccountAPIKeyRequest{Mutation: mutation, AccountRef: ref, ApiKey: apiKey})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "account", "")
}

func (server *Server) RefreshProviderAccountAuthorization(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.RefreshProviderAccountAuthorizationParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RefreshProviderAccountAuthorization(r.Context(), &controlplanev1.RefreshProviderAccountAuthorizationRequest{Mutation: mutation, AccountRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusAccepted, response, "account", "")
}

func (server *Server) VerifyProviderAccountDeviceAuthorization(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.VerifyProviderAccountDeviceAuthorizationParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.VerifyProviderAccountDeviceAuthorization(r.Context(), &controlplanev1.VerifyProviderAccountDeviceAuthorizationRequest{Mutation: mutation, AccountRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetAccount() == nil || response.GetAccount().GetRef() != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "account", "")
}

func (server *Server) ReauthorizeProviderAccountDeviceCode(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.ReauthorizeProviderAccountDeviceCodeParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ReauthorizeProviderAccountDeviceCode(r.Context(), &controlplanev1.ReauthorizeProviderAccountDeviceCodeRequest{Mutation: mutation, AccountRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetAccount() == nil || response.GetAccount().GetRef() != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusAccepted, response, "account", "")
}

func (server *Server) DeleteProviderAccount(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.DeleteProviderAccountParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.DeleteProviderAccount(r.Context(), &controlplanev1.DeleteProviderAccountRequest{Mutation: mutation, AccountRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetAccount() == nil || response.GetAccount().GetRef() != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "account", "")
}

func (server *Server) RevokeProviderAccount(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.RevokeProviderAccountParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RevokeProviderAccount(r.Context(), &controlplanev1.RevokeProviderAccountRequest{Mutation: mutation, AccountRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "account", "")
}

func (server *Server) SetProviderAccountEnabled(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.SetProviderAccountEnabledParams) {
	body, ok := decodeJSON[generated.EnabledInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.SetProviderAccountEnabled(r.Context(), &controlplanev1.SetProviderAccountEnabledRequest{Mutation: mutation, AccountRef: ref, Enabled: body.Enabled})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "account", "")
}

func (server *Server) ListPromptTemplateVariables(w http.ResponseWriter, r *http.Request, p generated.ListPromptTemplateVariablesParams) {
	server.listTemplateVariables(w, r, stringValue(p.ProjectRef), stringValue(p.AgentRef), stringValue(p.RuntimeRevisionRef), stringValue(p.Query), p.PageSize, p.PageToken)
}

func (server *Server) ValidatePromptTemplate(w http.ResponseWriter, r *http.Request, _ generated.ValidatePromptTemplateParams) {
	body, ok := decodeJSON[generated.PromptTemplateInput](w, r)
	if !ok {
		return
	}
	context, valid := promptContextInput(body.Context)
	if !valid || !promptText(body.Template, 256<<10) || !validPromptSelection(promptOptional(body.TargetKind), stringValue(body.TargetRef), stringValue(body.ExpectedContextDigest), context) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.ValidatePromptTemplate(r.Context(), &controlplanev1.ValidatePromptTemplateRequest{Template: body.Template, TargetKind: promptOptional(body.TargetKind), TargetRef: stringValue(body.TargetRef), Context: context, ExpectedContextDigest: stringValue(body.ExpectedContextDigest)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	result, valid := promptValidationView(response)
	if !valid || !validPromptContextReadback(promptOptional(body.TargetKind), stringValue(body.TargetRef), stringValue(body.ExpectedContextDigest), response.GetContextPin()) || !validPromptSelectedPin(context, response.GetContextPin()) {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) PreviewPromptTemplate(w http.ResponseWriter, r *http.Request, _ generated.PreviewPromptTemplateParams) {
	body, ok := decodeJSON[generated.PromptTemplatePreviewInput](w, r)
	if !ok {
		return
	}
	context, valid := promptContextInput(body.Context)
	if !valid || !promptText(body.Template, 256<<10) || !validPromptSelection(promptOptional(body.TargetKind), stringValue(body.TargetRef), stringValue(body.ExpectedContextDigest), context) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.PreviewPromptTemplate(r.Context(), &controlplanev1.PreviewPromptTemplateRequest{
		Template: body.Template, TargetKind: promptOptional(body.TargetKind), TargetRef: stringValue(body.TargetRef), Context: context, ExpectedContextDigest: stringValue(body.ExpectedContextDigest),
		IncludeFullMaterialization: body.IncludeFullMaterialization != nil && *body.IncludeFullMaterialization,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	result, valid := promptPreviewView(response, body.IncludeFullMaterialization != nil && *body.IncludeFullMaterialization)
	if !valid || !validPromptContextReadback(promptOptional(body.TargetKind), stringValue(body.TargetRef), stringValue(body.ExpectedContextDigest), response.GetContextPin()) || !validPromptSelectedPin(context, response.GetContextPin()) || response.GetRuntimeDiff() != nil && promptOptional(body.TargetKind) == "SESSION_CONTINUATION" && response.GetRuntimeDiff().GetSessionRef() != stringValue(body.TargetRef) {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (server *Server) DeleteRuntimeEnvironment(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentRef, p generated.DeleteRuntimeEnvironmentParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.DeleteRuntimeEnvironment(r.Context(), &controlplanev1.DeleteRuntimeEnvironmentRequest{Mutation: mutation, EnvironmentRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "environment", "")
}

func (server *Server) SetRuntimeEnvironmentEnabled(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentRef, p generated.SetRuntimeEnvironmentEnabledParams) {
	body, ok := decodeJSON[generated.EnabledInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.SetRuntimeEnvironmentEnabled(r.Context(), &controlplanev1.SetRuntimeEnvironmentEnabledRequest{Mutation: mutation, EnvironmentRef: ref, Enabled: body.Enabled})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "environment", "")
}

func (server *Server) GetRuntimeEnvironmentReadiness(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentRef) {
	response, err := server.control.Query.GetRuntimeEnvironmentReadiness(r.Context(), &controlplanev1.GetRuntimeEnvironmentReadinessRequest{EnvironmentRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "readiness", "")
}

func (server *Server) ListRuntimeEnvironmentAgents(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentRef, p generated.ListRuntimeEnvironmentAgentsParams) {
	response, err := server.control.Query.ListRuntimeEnvironmentAgents(r.Context(), &controlplanev1.ListRuntimeEnvironmentAgentsRequest{
		EnvironmentRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "agents")
}

func (server *Server) DeleteSchedule(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef, p generated.DeleteScheduleParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.DeleteSchedule(r.Context(), &controlplanev1.DeleteScheduleRequest{Mutation: mutation, ScheduleRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "schedule", "")
}

func (server *Server) ListScheduleRevisions(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef, p generated.ListScheduleRevisionsParams) {
	response, err := server.control.Query.ListScheduleRevisions(r.Context(), &controlplanev1.ListScheduleRevisionsRequest{ScheduleRef: ref, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "revisions")
}

func (server *Server) ListScheduleRuns(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef, p generated.ListScheduleRunsParams) {
	response, err := server.control.Query.ListScheduleRuns(r.Context(), &controlplanev1.ListScheduleRunsRequest{ScheduleRef: ref, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "occurrences")
}

func (server *Server) UpdateIntegrationConnection(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.UpdateIntegrationConnectionParams) {
	body, ok := decodeJSON[generated.IntegrationConnectionUpdateInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	configuration, _ := structpb.NewStruct(body.PublicConfiguration)
	response, err := server.control.Command.UpdateIntegrationConnection(r.Context(), &controlplanev1.UpdateIntegrationConnectionRequest{
		Mutation: mutation, ConnectionRef: ref, Name: body.Name, PublicConfiguration: configuration,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "connection", "")
}

func (server *Server) DeleteIntegrationConnection(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.DeleteIntegrationConnectionParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.DeleteIntegrationConnection(r.Context(), &controlplanev1.DeleteIntegrationConnectionRequest{Mutation: mutation, ConnectionRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "connection", "")
}

func (server *Server) ListRoleImageRecipeRevisions(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, recipeRef generated.RecipeRef, p generated.ListRoleImageRecipeRevisionsParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	response, err := server.control.Query.ListRoleImageRecipeRevisions(r.Context(), &controlplanev1.ListRoleImageRecipeRevisionsRequest{RecipeRef: recipeRef, Page: page(p.PageSize, p.PageToken)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "revisions")
}

func (server *Server) PromoteRoleImage(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, recipeRef generated.RecipeRef, p generated.PromoteRoleImageParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.RoleImagePromotionInput](w, r)
	if !ok {
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PromoteRoleImage(r.Context(), &controlplanev1.PromoteRoleImageRequest{
		Mutation: mutation, RecipeRef: recipeRef, ImageArtifactRef: body.ImageArtifactRef,
		ExpectedProvenanceSha256: body.ExpectedProvenanceSha256,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusAccepted, response, "receipt", "")
}
