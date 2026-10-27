package httptransport

import (
	"crypto/sha256"
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

// maximumAgentAvatarBytes caps the advertised avatar upload length at five MiB before opening the owner stream; actual byte-count agreement is checked separately during forwarding.
// maximumAgentAvatarBytes ограничивает объявленную длину аватара пятью МиБ до открытия потока владельца; совпадение фактического числа байтов проверяется отдельно при передаче.
const maximumAgentAvatarBytes = 5 << 20

// UploadAgentAvatar binds route-project context, permits supported image media types and an explicit bounded length, then streams mutation metadata, counted/hashed chunks and commit to the owner before projecting the result. Body/read mismatches fail explicitly; early returns here do not add a separate stream abort/CloseSend, and image admission remains owner-side.
// UploadAgentAvatar привязывает проект маршрута, допускает поддерживаемые image media types и явную ограниченную длину, затем передаёт metadata mutation, подсчитанные и хешированные chunks и commit владельцу до проекции. Несовпадение длины и ошибка чтения отклоняются; ранние возвраты здесь не добавляют отдельный abort/CloseSend, допуск изображения остаётся у владельца.
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
	if writeArtifactUploadError(w, err) {
		return
	}
	if err = stream.Send(&controlplanev1.UploadAgentAvatarRequest{Part: &controlplanev1.UploadAgentAvatarRequest_Commit{Commit: &controlplanev1.UploadArtifactCommit{
		SizeBytes: received, Sha256: digest,
	}}}); err != nil {
		writeRPCProblem(w, err)
		return
	}
	response, err := stream.CloseAndRecv()
	writeRPCResult(w, http.StatusOK, response, err, "agent", "")
}

// SetAgentAvatar decodes the artifact selector and mutation context for the dedicated owner avatar command, then projects the agent; the reference alone does not prove artifact eligibility.
// SetAgentAvatar разбирает селектор файла и mutation context отдельной команды аватара владельца, затем проецирует сотрудника; ссылка сама допуск файла не доказывает.
func (server *Server) SetAgentAvatar(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.SetAgentAvatarParams) {
	body, mutation, ok := decodeMutationBody[generated.AgentAvatarInput](w, r, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.SetAgentAvatar(r.Context(), &controlplanev1.SetAgentAvatarRequest{
		Mutation: mutation, AgentRef: ref, ArtifactRef: body.ArtifactRef,
	})
	writeRPCResult(w, http.StatusOK, response, err, "agent", "")
}

// RemoveAgentAvatar invokes the dedicated owner removal with mutation/version pins and returns the public agent; it does not directly remove underlying artifact storage.
// RemoveAgentAvatar вызывает отдельное удаление аватара владельца с mutation/version pins и возвращает публичного сотрудника; хранилище исходного файла напрямую не удаляет.
func (server *Server) RemoveAgentAvatar(w http.ResponseWriter, r *http.Request, ref generated.AgentRef, p generated.RemoveAgentAvatarParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RemoveAgentAvatar(r.Context(), &controlplanev1.RemoveAgentAvatarRequest{Mutation: mutation, AgentRef: ref})
	writeRPCResult(w, http.StatusOK, response, err, "agent", "")
}

// GetAgentAvatarContent first reads owner-visible agent metadata, then redirects a referenced artifact avatar to the escaped preview route or emits a deterministic PNG fallback with a private five-minute cache. Fallback encoding errors after headers are ignored; artifact eligibility is rechecked by its own route.
// GetAgentAvatarContent сначала читает видимую metadata сотрудника, затем перенаправляет аватар-файл на escaped preview route либо выдаёт детерминированный PNG с частным кэшем пять минут. Ошибка encoding после заголовков игнорируется; допуск файла повторно проверяет его маршрут.
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

// writeFallbackAvatar hashes a versioned prefix plus reference to draw a fixed64x64 colored grid and returns PNG encoding errors. It uses no external image/network, and the hash is visual determinism rather than a signature or access proof.
// writeFallbackAvatar хеширует версионированный префикс и ссылку для цветной сетки64x64 и возвращает ошибки PNG encoding. Внешние изображения и сеть не используются; хеш даёт повторяемость вида, а не подпись и доказательство доступа.
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

// ListProviderDefinitions validates catalog input, requires nonnil definitions and valid models pinned to each definition key, then projects the public page without treating catalog readiness as account authorization.
// ListProviderDefinitions проверяет вход каталога, ненулевые определения и допустимые модели с точным ключом определения, затем проецирует страницу без принятия готовности каталога за авторизацию аккаунта.
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

// ListProviderAccounts validates usage context and optional nonzero state, reads owner-filtered accounts and checks each usage-aware projection before public output; selection eligibility is not inferred from authentication alone.
// ListProviderAccounts проверяет usage context и необязательное ненулевое состояние, читает аккаунты владельца и сверяет каждую usage-aware проекцию до выдачи; допуск выбора не выводится только из аутентификации.
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

// GetProviderAccount validates optional usage selectors and requires the returned account to satisfy that context and match the exact route reference before public projection.
// GetProviderAccount проверяет необязательные usage-селекторы и требует соответствие ответа этому контексту и точной ссылке маршрута до публичной проекции.
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

// CreateProviderAccount decodes definition/name with a create idempotency context, invokes the dedicated owner command and projects HTTP201 without authorizing the provider or assigning external credentials.
// CreateProviderAccount разбирает definition/name и контекст идемпотентного create, вызывает отдельную команду владельца и проецирует HTTP201 без авторизации провайдера и назначения внешних credentials.
func (server *Server) CreateProviderAccount(w http.ResponseWriter, r *http.Request, p generated.CreateProviderAccountParams) {
	body, mutation, ok := decodeMutationBody[generated.ProviderAccountCreateInput](w, r, p.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Command.CreateProviderAccount(r.Context(), &controlplanev1.CreateProviderAccountRequest{
		Mutation: mutation, DefinitionKey: string(body.DefinitionKey), Name: body.Name,
	})
	writeRPCResult(w, http.StatusCreated, response, err, "account", "")
}

// StartProviderAccountDeviceAuthorization forwards mutation/version pins to the specialized owner device-code start, preserves device-specific problem mapping and returns HTTP202 without completing interactive authentication.
// StartProviderAccountDeviceAuthorization передаёт mutation/version pins специальному началу device-code владельца, сохраняет профиль device-ошибок и возвращает HTTP202 без завершения интерактивного входа.
func (server *Server) StartProviderAccountDeviceAuthorization(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.StartProviderAccountDeviceAuthorizationParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.StartProviderAccountDeviceAuthorization(r.Context(), &controlplanev1.StartProviderAccountDeviceAuthorizationRequest{Mutation: mutation, AccountRef: ref})
	if err != nil {
		writeProviderDeviceProblem(w, err)
		return
	}
	writeMessage(w, http.StatusAccepted, response, "account", "")
}

// AuthorizeProviderAccountApiKey requires a nonblank key, clears the decoded string field, passes mutable bytes only to the owner authorization RPC and best-effort zeros that byte slice on return. This does not guarantee erasure of immutable decoder/string copies or change the owner's credential policy.
// AuthorizeProviderAccountApiKey требует непустой ключ, очищает строковое поле decoded input, передаёт изменяемые байты только RPC авторизации владельца и пытается обнулить slice при возврате. Это не гарантирует очистку неизменяемых decoder/string copies и не меняет политику credentials владельца.
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
	writeRPCResult(w, http.StatusOK, response, err, "account", "")
}

// RefreshProviderAccountAuthorization sends exact account and mutation pins to the owner refresh command and returns an accepted public account response; HTTP202 is not proof that external refresh has completed.
// RefreshProviderAccountAuthorization передаёт точный аккаунт и mutation pins команде обновления владельца и возвращает принятый публичный ответ; HTTP202 не доказывает завершение внешнего refresh.
func (server *Server) RefreshProviderAccountAuthorization(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.RefreshProviderAccountAuthorizationParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RefreshProviderAccountAuthorization(r.Context(), &controlplanev1.RefreshProviderAccountAuthorizationRequest{Mutation: mutation, AccountRef: ref})
	writeRPCResult(w, http.StatusAccepted, response, err, "account", "")
}

// VerifyProviderAccountDeviceAuthorization invokes explicit owner verification with mutation pins, requires a nonnil exact-account reply and projects HTTP200; it does not infer success from merely possessing a device code.
// VerifyProviderAccountDeviceAuthorization вызывает явную проверку владельца с mutation pins, требует ненулевой ответ точного аккаунта и проецирует HTTP200; наличие device code само по себе успех не доказывает.
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
	writeProviderAccountCommandResponse(w, http.StatusOK, response, ref)
}

// ReauthorizeProviderAccountDeviceCode preserves the device-specific error profile and exact-account response check around the owner reauthorization command, returning HTTP202 without locally changing authorization state.
// ReauthorizeProviderAccountDeviceCode сохраняет профиль device-ошибок и проверку точного аккаунта вокруг команды повторного входа владельца, выдавая HTTP202 без локальной смены состояния авторизации.
func (server *Server) ReauthorizeProviderAccountDeviceCode(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.ReauthorizeProviderAccountDeviceCodeParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.ReauthorizeProviderAccountDeviceCode(r.Context(), &controlplanev1.ReauthorizeProviderAccountDeviceCodeRequest{Mutation: mutation, AccountRef: ref})
	if err != nil {
		writeProviderDeviceProblem(w, err)
		return
	}
	writeProviderAccountCommandResponse(w, http.StatusAccepted, response, ref)
}

// DeleteProviderAccount delegates deletion to the owner with mutation pins, requires a nonnil matching account reply and projects its state; this handler does not itself erase credentials or claim cleanup is complete.
// DeleteProviderAccount поручает удаление владельцу с mutation pins, требует ненулевой ответ того же аккаунта и проецирует состояние; сам handler credentials не стирает и завершение очистки не утверждает.
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
	writeProviderAccountCommandResponse(w, http.StatusOK, response, ref)
}

// RevokeProviderAccount forwards exact account and mutation context to owner revocation and projects the response without retrying or directly editing credential storage.
// RevokeProviderAccount передаёт точный аккаунт и mutation context отзыву владельца и проецирует ответ без повторов и прямого изменения хранилища credentials.
func (server *Server) RevokeProviderAccount(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.RevokeProviderAccountParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RevokeProviderAccount(r.Context(), &controlplanev1.RevokeProviderAccountRequest{Mutation: mutation, AccountRef: ref})
	writeRPCResult(w, http.StatusOK, response, err, "account", "")
}

// SetProviderAccountEnabled decodes the enabled flag and mutation/version pins for the dedicated owner command; enabling display state does not independently authenticate or admit the account.
// SetProviderAccountEnabled разбирает enabled и mutation/version pins отдельной команды владельца; включение состояния само по себе аккаунт не аутентифицирует и не допускает.
func (server *Server) SetProviderAccountEnabled(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.SetProviderAccountEnabledParams) {
	body, mutation, ok := decodeMutationBody[generated.EnabledInput](w, r, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.SetProviderAccountEnabled(r.Context(), &controlplanev1.SetProviderAccountEnabledRequest{Mutation: mutation, AccountRef: ref, Enabled: body.Enabled})
	writeRPCResult(w, http.StatusOK, response, err, "account", "")
}

// SetProviderAccountConcurrency requires a limit1..256 and an explicit expected version before invoking the owner command and projecting its response; running execution claims and capacity reconciliation remain owner-managed.
// SetProviderAccountConcurrency требует limit1..256 и явную ожидаемую версию до команды владельца и проекции ответа; claims действующих запусков и сверка ёмкости остаются у владельца.
func (server *Server) SetProviderAccountConcurrency(w http.ResponseWriter, r *http.Request, ref generated.ProviderAccountRef, p generated.SetProviderAccountConcurrencyParams) {
	body, ok := decodeJSON[generated.ProviderAccountConcurrencyInput](w, r)
	if !ok {
		return
	}
	if body.MaximumConcurrentExecutions < 1 || body.MaximumConcurrentExecutions > 256 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	if mutation.ExpectedVersion == nil {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Command.SetProviderAccountConcurrency(r.Context(), &controlplanev1.SetProviderAccountConcurrencyRequest{
		Mutation: mutation, AccountRef: ref, MaximumConcurrentExecutions: body.MaximumConcurrentExecutions,
	})
	writeRPCResult(w, http.StatusOK, response, err, "account", "")
}

// ListPromptTemplateVariables forwards optional project, agent, runtime revision, query, source and page selectors to the shared variable-catalog implementation without expanding a prompt.
// ListPromptTemplateVariables передаёт необязательные project, agent, runtime revision, query, source и страницу общей реализации каталога переменных без раскрытия prompt.
func (server *Server) ListPromptTemplateVariables(w http.ResponseWriter, r *http.Request, p generated.ListPromptTemplateVariablesParams) {
	server.listTemplateVariables(w, r, stringValue(p.ProjectRef), stringValue(p.AgentRef), stringValue(p.RuntimeRevisionRef), stringValue(p.Query), stringValue(p.Source), p.PageSize, p.PageToken)
}

// ValidatePromptTemplate validates bounded template/context/target selection before the owner query, then requires a valid result and exact returned context/selection pins. It returns validation JSON without applying configuration or granting runtime authority.
// ValidatePromptTemplate проверяет ограниченные template, context и target до запроса владельцу, затем требует допустимый результат и точные context/selection pins. Возвращает JSON проверки без применения конфигурации и выдачи runtime-полномочий.
func (server *Server) ValidatePromptTemplate(w http.ResponseWriter, r *http.Request, _ generated.ValidatePromptTemplateParams) {
	body, ok := decodeJSON[generated.PromptTemplateInput](w, r)
	if !ok {
		return
	}
	context, valid := promptContextInput(body.Context)
	if !valid || !validPromptTemplateInput(body.Template, promptOptional(body.TargetKind), stringValue(body.TargetRef), stringValue(body.ExpectedContextDigest), context) {
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

// PreviewPromptTemplate validates bounded input and selected context, delegates optional full materialization to the owner and checks returned context pins plus continuation runtime-diff session identity. It uses the specialized prompt error profile and never applies the preview.
// PreviewPromptTemplate проверяет ограниченный ввод и выбранный контекст, поручает необязательную полную материализацию владельцу и сверяет context pins и session identity runtime-diff продолжения. Сохраняет специальный профиль prompt-ошибок и preview не применяет.
func (server *Server) PreviewPromptTemplate(w http.ResponseWriter, r *http.Request, _ generated.PreviewPromptTemplateParams) {
	body, ok := decodeJSON[generated.PromptTemplatePreviewInput](w, r)
	if !ok {
		return
	}
	context, valid := promptContextInput(body.Context)
	if !valid || !validPromptTemplateInput(body.Template, promptOptional(body.TargetKind), stringValue(body.TargetRef), stringValue(body.ExpectedContextDigest), context) {
		writeLocalProblem(w, 400, "INVALID_REQUEST", false)
		return
	}
	response, err := server.control.Query.PreviewPromptTemplate(r.Context(), &controlplanev1.PreviewPromptTemplateRequest{
		Template: body.Template, TargetKind: promptOptional(body.TargetKind), TargetRef: stringValue(body.TargetRef), Context: context, ExpectedContextDigest: stringValue(body.ExpectedContextDigest),
		IncludeFullMaterialization: body.IncludeFullMaterialization != nil && *body.IncludeFullMaterialization,
	})
	if err != nil {
		writePromptProblem(w, err)
		return
	}
	result, valid := promptPreviewView(response, body.IncludeFullMaterialization != nil && *body.IncludeFullMaterialization)
	if !valid || !validPromptContextReadback(promptOptional(body.TargetKind), stringValue(body.TargetRef), stringValue(body.ExpectedContextDigest), response.GetContextPin()) || !validPromptSelectedPin(context, response.GetContextPin()) || response.GetRuntimeDiff() != nil && promptOptional(body.TargetKind) == "SESSION_CONTINUATION" && response.GetRuntimeDiff().GetSessionRef() != stringValue(body.TargetRef) {
		writeLocalProblem(w, 502, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// DeleteRuntimeEnvironment sends selected environment and mutation pins to the owner delete command and projects public state, without deleting cluster workloads directly.
// DeleteRuntimeEnvironment передаёт выбранное окружение и mutation pins команде удаления владельца и проецирует состояние без прямого удаления cluster workloads.
func (server *Server) DeleteRuntimeEnvironment(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentRef, p generated.DeleteRuntimeEnvironmentParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.DeleteRuntimeEnvironment(r.Context(), &controlplanev1.DeleteRuntimeEnvironmentRequest{Mutation: mutation, EnvironmentRef: ref})
	writeRPCResult(w, http.StatusOK, response, err, "environment", "")
}

// SetRuntimeEnvironmentEnabled decodes an explicit flag and mutation context for owner enable/disable, returning public metadata rather than provisioning or stopping pods itself.
// SetRuntimeEnvironmentEnabled разбирает явный флаг и mutation context включения либо отключения владельца, возвращая metadata вместо самостоятельного создания и остановки подов.
func (server *Server) SetRuntimeEnvironmentEnabled(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentRef, p generated.SetRuntimeEnvironmentEnabledParams) {
	body, mutation, ok := decodeMutationBody[generated.EnabledInput](w, r, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.SetRuntimeEnvironmentEnabled(r.Context(), &controlplanev1.SetRuntimeEnvironmentEnabledRequest{Mutation: mutation, EnvironmentRef: ref, Enabled: body.Enabled})
	writeRPCResult(w, http.StatusOK, response, err, "environment", "")
}

// GetRuntimeEnvironmentReadiness reads the selected environment's owner readiness view and public-projects it without performing runtime probes or changing admission state.
// GetRuntimeEnvironmentReadiness читает readiness выбранного окружения у владельца и публично проецирует его без runtime probes и изменения допуска.
func (server *Server) GetRuntimeEnvironmentReadiness(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentRef) {
	response, err := server.control.Query.GetRuntimeEnvironmentReadiness(r.Context(), &controlplanev1.GetRuntimeEnvironmentReadinessRequest{EnvironmentRef: ref})
	writeReadRPCResult(w, response, err, "readiness", "")
}

// ListRuntimeEnvironmentAgents reads the owner's paginated agent relationships for an exact environment and query without rebinding or launching any agent.
// ListRuntimeEnvironmentAgents читает страницу связей сотрудников точного окружения с поиском у владельца без перепривязки и запуска.
func (server *Server) ListRuntimeEnvironmentAgents(w http.ResponseWriter, r *http.Request, ref generated.RuntimeEnvironmentRef, p generated.ListRuntimeEnvironmentAgentsParams) {
	response, err := server.control.Query.ListRuntimeEnvironmentAgents(r.Context(), &controlplanev1.ListRuntimeEnvironmentAgentsRequest{
		EnvironmentRef: ref, Query: stringValue(p.Query), Page: page(p.PageSize, p.PageToken),
	})
	writeReadRPCResult(w, response, err, "", "agents")
}

// DeleteSchedule delegates the exact schedule deletion with mutation/version pins and projects the result; timer/occurrence cancellation and terminal events belong to the owner transaction.
// DeleteSchedule поручает удаление точной автоматизации с mutation/version pins и проецирует результат; отмена таймеров и occurrence и terminal events принадлежат транзакции владельца.
func (server *Server) DeleteSchedule(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef, p generated.DeleteScheduleParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.DeleteSchedule(r.Context(), &controlplanev1.DeleteScheduleRequest{Mutation: mutation, ScheduleRef: ref})
	writeRPCResult(w, http.StatusOK, response, err, "schedule", "")
}

// ListScheduleRevisions forwards exact schedule/page selectors and projects revision metadata with the established terminal-cursor policy, without activating a revision.
// ListScheduleRevisions передаёт точные автоматизацию и страницу и проецирует metadata ревизий с прежней terminal-cursor политикой без активации ревизии.
func (server *Server) ListScheduleRevisions(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef, p generated.ListScheduleRevisionsParams) {
	response, err := server.control.Query.ListScheduleRevisions(r.Context(), &controlplanev1.ListScheduleRevisionsRequest{ScheduleRef: ref, Page: page(p.PageSize, p.PageToken)})
	writeReadRPCResult(w, response, err, "", "revisions")
}

// ListScheduleRuns reads paginated occurrences of the selected schedule, retaining the public page cursor without claiming or retrying an occurrence.
// ListScheduleRuns читает страницу occurrence выбранной автоматизации, сохраняя публичный курсор без claim и retry occurrence.
func (server *Server) ListScheduleRuns(w http.ResponseWriter, r *http.Request, ref generated.ScheduleRef, p generated.ListScheduleRunsParams) {
	response, err := server.control.Query.ListScheduleRuns(r.Context(), &controlplanev1.ListScheduleRunsRequest{ScheduleRef: ref, Page: page(p.PageSize, p.PageToken)})
	writeReadRPCResult(w, response, err, "", "occurrences")
}

// UpdateIntegrationConnection decodes public configuration/name and mutation pins, converts configuration with structpb while ignoring its conversion error as before, then invokes the dedicated owner update. It does not install credentials or execute remote tools.
// UpdateIntegrationConnection разбирает публичную конфигурацию, имя и mutation pins, преобразует structpb с прежним игнорированием ошибки, затем вызывает отдельный update владельца. Credentials не устанавливает и удалённые инструменты не выполняет.
func (server *Server) UpdateIntegrationConnection(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.UpdateIntegrationConnectionParams) {
	body, mutation, ok := decodeMutationBody[generated.IntegrationConnectionUpdateInput](w, r, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	configuration, _ := structpb.NewStruct(body.PublicConfiguration)
	response, err := server.control.Command.UpdateIntegrationConnection(r.Context(), &controlplanev1.UpdateIntegrationConnectionRequest{
		Mutation: mutation, ConnectionRef: ref, Name: body.Name, PublicConfiguration: configuration,
	})
	writeRPCResult(w, http.StatusOK, response, err, "connection", "")
}

// DeleteIntegrationConnection delegates exact connection deletion with mutation/version pins and projects the result without bypassing owner lifecycle, secret revocation or dependency policy.
// DeleteIntegrationConnection поручает удаление точного подключения с mutation/version pins и проецирует результат без обхода lifecycle, отзыва секретов и политики зависимостей владельца.
func (server *Server) DeleteIntegrationConnection(w http.ResponseWriter, r *http.Request, ref generated.ConnectionRef, p generated.DeleteIntegrationConnectionParams) {
	mutation, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.DeleteIntegrationConnection(r.Context(), &controlplanev1.DeleteIntegrationConnectionRequest{Mutation: mutation, ConnectionRef: ref})
	writeRPCResult(w, http.StatusOK, response, err, "connection", "")
}

// ListRoleImageRecipeRevisions binds the route project to request context and reads the exact recipe's historical page without building, admitting or promoting an image.
// ListRoleImageRecipeRevisions привязывает проект маршрута к контексту и читает историю точного рецепта без сборки, допуска и продвижения образа.
func (server *Server) ListRoleImageRecipeRevisions(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, recipeRef generated.RecipeRef, p generated.ListRoleImageRecipeRevisionsParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	response, err := server.control.Query.ListRoleImageRecipeRevisions(r.Context(), &controlplanev1.ListRoleImageRecipeRevisionsRequest{RecipeRef: recipeRef, Page: page(p.PageSize, p.PageToken)})
	writeReadRPCResult(w, response, err, "", "revisions")
}

// PromoteRoleImage binds route-project context, decodes artifact/provenance pins and mutation/version context, then requests owner promotion and returns an accepted receipt. The request is not itself a signature/admission proof, and HTTP202 does not assert publication is complete.
// PromoteRoleImage привязывает проект маршрута, разбирает artifact/provenance pins и mutation/version context, затем запрашивает promotion владельца и выдаёт принятый receipt. Сам запрос не является доказательством подписи и допуска, HTTP202 не утверждает завершение публикации.
func (server *Server) PromoteRoleImage(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, recipeRef generated.RecipeRef, p generated.PromoteRoleImageParams) {
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	body, mutation, ok := decodeMutationBody[generated.RoleImagePromotionInput](w, r, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.PromoteRoleImage(r.Context(), &controlplanev1.PromoteRoleImageRequest{
		Mutation: mutation, RecipeRef: recipeRef, ImageArtifactRef: body.ImageArtifactRef,
		ExpectedProvenanceSha256: body.ExpectedProvenanceSha256,
	})
	writeRPCResult(w, http.StatusAccepted, response, err, "receipt", "")
}
