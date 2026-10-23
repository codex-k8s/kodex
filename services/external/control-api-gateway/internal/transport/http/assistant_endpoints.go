package httptransport

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
	"google.golang.org/protobuf/types/known/structpb"
)

// maximumAssistantPlanDraftJSONBody selects the nominal two-MiB draft-plan decoder budget; the shared limited-reader decoder still has its documented duplicate-key/length limitations.
// maximumAssistantPlanDraftJSONBody задаёт номинальный бюджет decoder черновика плана в два МиБ; ограничения общего limited-reader decoder по повторным ключам и длине сохраняются.
const maximumAssistantPlanDraftJSONBody = 2 << 20

// GetSystemAssistant reads the owner-visible system assistant and uses the common public projection without starting a conversation or provider turn.
// GetSystemAssistant читает видимого системного помощника у владельца и использует общую публичную проекцию без начала диалога и хода провайдера.
func (server *Server) GetSystemAssistant(w http.ResponseWriter, r *http.Request) {
	response, err := server.control.Assistant.GetSystemAssistant(r.Context(), &controlplanev1.GetSystemAssistantRequest{})
	writeReadRPCResult(w, response, err, "assistant", "")
}

// GetProjectAssistant checks opaque project syntax and derived project context, then requires a valid exact-project profile before public output; syntax and profile metadata are not an execution grant.
// GetProjectAssistant проверяет формат проекта и производный контекст, затем требует допустимый профиль точного проекта до выдачи; формат и metadata не являются grant выполнения.
func (server *Server) GetProjectAssistant(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef) {
	if !opaqueHTTPReference.MatchString(projectRef) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	r, ok := withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	response, err := server.control.Assistant.GetProjectAssistant(r.Context(), &controlplanev1.GetProjectAssistantRequest{ProjectRef: projectRef})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validProjectAssistantProfile(response.GetProfile(), projectRef) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "profile", "")
}

// CreateProjectAssistant validates project/name/purpose and nonblank UTF-8 instructions up to32768runes, then sends create mutation context and exact project to the owner. It requires a valid returned project profile and responds HTTP201 without launching an agent.
// CreateProjectAssistant проверяет проект, имя, цель и непустые UTF-8 инструкции до32768рун, затем передаёт create mutation и точный проект владельцу. Требует допустимый профиль ответа и выдаёт HTTP201 без запуска сотрудника.
func (server *Server) CreateProjectAssistant(w http.ResponseWriter, r *http.Request, projectRef generated.ProjectRef, p generated.CreateProjectAssistantParams) {
	body, ok := decodeJSON[generated.CreateProjectAssistantJSONBody](w, r)
	if !ok {
		return
	}
	if !opaqueHTTPReference.MatchString(projectRef) || !validSearchText(body.Name, 1, 160) ||
		!validSearchText(body.Purpose, 1, 2000) || !utf8.ValidString(body.Instructions) ||
		strings.TrimSpace(body.Instructions) == "" || utf8.RuneCountInString(body.Instructions) > 32768 {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	r, ok = withProjectReference(w, r, projectRef)
	if !ok {
		return
	}
	response, err := server.control.Assistant.CreateProjectAssistant(r.Context(), &controlplanev1.CreateProjectAssistantRequest{
		Mutation: mutation, ProjectRef: projectRef, Name: body.Name, Purpose: body.Purpose, Instructions: body.Instructions,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validProjectAssistantProfile(response.GetProfile(), projectRef) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusCreated, response, "profile", "")
}

// validProjectAssistantProfile checks profile/agent reference prefixes and syntax, exact project, positive safe version, bounded name, valid timestamps and closed state. It checks returned shape, not actor authorization or current runtime readiness.
// validProjectAssistantProfile проверяет префиксы и формат профиля и сотрудника, точный проект, безопасную версию, имя, timestamps и закрытое состояние. Это форма ответа, а не авторизация actor и готовность runtime.
func validProjectAssistantProfile(profile *controlplanev1.ProjectAssistantProfile, projectRef string) bool {
	return profile != nil && strings.HasPrefix(profile.Ref, "asstp_") && opaqueHTTPReference.MatchString(profile.Ref) &&
		profile.ProjectRef == projectRef && strings.HasPrefix(profile.AgentRef, "agt_") && opaqueHTTPReference.MatchString(profile.AgentRef) &&
		validManagedVersion(profile.Version) && validSearchText(profile.Name, 1, 160) && profile.CreatedAt.CheckValid() == nil && profile.UpdatedAt.CheckValid() == nil &&
		(profile.State == "ACTIVE" || profile.State == "DISABLED" || profile.State == "ARCHIVED")
}

// assistantScopeInput maps the prefixed public assistant scope to protobuf with zero fallback; callers must validate scope semantics before relying on the result.
// assistantScopeInput преобразует публичную область помощника с префиксом в protobuf с нулевым запасным значением; семантику области заранее проверяет вызывающий.
func assistantScopeInput(scope generated.AssistantScope) controlplanev1.AssistantScope {
	return controlplanev1.AssistantScope(controlplanev1.AssistantScope_value["ASSISTANT_SCOPE_"+string(scope)])
}

// ListAssistantConversations validates catalog/state/scope/ref selectors and optionally matches the localized default title, then verifies nonnil bounded pages, UTF-8 cursor and every requested project/state/assistant pin before projection. Owner visibility is not reconstructed from client filters.
// ListAssistantConversations проверяет каталог, state/scope/ref и при необходимости локализованное исходное имя, затем сверяет ненулевую ограниченную страницу, UTF-8 cursor и все запрошенные project/state/assistant pins. Видимость владельца из клиентских фильтров не восстанавливается.
func (server *Server) ListAssistantConversations(w http.ResponseWriter, r *http.Request, p generated.ListAssistantConversationsParams) {
	state := controlplanev1.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE
	if !validSearchText(stringValue(p.Query), 0, 200) || p.State != nil && !p.State.Valid() ||
		p.AssistantScope != nil && !p.AssistantScope.Valid() || p.AssistantRef != nil && !opaqueHTTPReference.MatchString(*p.AssistantRef) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	if p.State != nil {
		state = controlplanev1.AssistantConversationState(controlplanev1.AssistantConversationState_value["ASSISTANT_CONVERSATION_STATE_"+string(*p.State)])
	}
	r, ok := catalogRequest(w, r, p.ProjectRef, p.Query, p.PageSize, p.PageToken)
	if !ok {
		return
	}
	query := stringValue(p.Query)
	matchLocalizedDefaultTitle := false
	if localizer, ok := w.(interface{ Localize(string) string }); ok && strings.TrimSpace(query) != "" {
		localized := localizer.Localize("NEW_ASSISTANT_CONVERSATION")
		matchLocalizedDefaultTitle = strings.Contains(strings.ToLower(localized), strings.ToLower(strings.TrimSpace(query)))
	}
	request := &controlplanev1.ListAssistantConversationsRequest{ProjectRef: stringValue(p.ProjectRef), Query: query, State: state, MatchLocalizedDefaultTitle: matchLocalizedDefaultTitle, Page: page(p.PageSize, p.PageToken), AssistantRef: stringValue(p.AssistantRef)}
	if p.AssistantScope != nil {
		request.AssistantScope = assistantScopeInput(*p.AssistantScope)
	}
	response, err := server.control.Assistant.ListAssistantConversations(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response == nil || len(response.Conversations) > int(page(p.PageSize, p.PageToken).PageSize) ||
		len(response.GetPage().GetNextPageToken()) > 512 || !utf8.ValidString(response.GetPage().GetNextPageToken()) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	for _, conversation := range response.Conversations {
		if conversation == nil || !opaqueHTTPReference.MatchString(conversation.Ref) || conversation.State != state ||
			p.ProjectRef != nil && conversation.GetProjectRef() != *p.ProjectRef ||
			p.AssistantScope != nil && conversation.GetAssistantScope() != request.AssistantScope ||
			p.AssistantRef != nil && conversation.GetAssistantRef() != *p.AssistantRef {
			writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
			return
		}
	}
	writeMessage(w, http.StatusOK, response, "", "conversations")
}

// ArchiveAssistantConversation requires a syntactic reference and mandatory versioned mutation, then checks the owner's exact conversation/version and ARCHIVED outcome before public output.
// ArchiveAssistantConversation требует формат ссылки и обязательную versioned mutation, затем сверяет точный диалог, версию и ARCHIVED владельца до выдачи.
func (server *Server) ArchiveAssistantConversation(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.ArchiveAssistantConversationParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.ArchiveAssistantConversation(r.Context(), &controlplanev1.ArchiveAssistantConversationRequest{Mutation: mutation, ConversationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeAssistantConversationState(w, response, ref, controlplanev1.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ARCHIVED)
}

// RestoreAssistantConversation requires exact reference/versioned mutation and validates the returned conversation identity, version and ACTIVE state rather than optimistically restoring client data.
// RestoreAssistantConversation требует точную ссылку и versioned mutation и проверяет identity, версию и ACTIVE ответа вместо оптимистического восстановления клиентских данных.
func (server *Server) RestoreAssistantConversation(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.RestoreAssistantConversationParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.RestoreAssistantConversation(r.Context(), &controlplanev1.RestoreAssistantConversationRequest{Mutation: mutation, ConversationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeAssistantConversationState(w, response, ref, controlplanev1.AssistantConversationState_ASSISTANT_CONVERSATION_STATE_ACTIVE)
}

// PurgeAssistantConversation invokes the dedicated owner purge with versioned mutation and requires its exact conversation reference before HTTP204; it does not directly erase history or claim unrelated resources are removed.
// PurgeAssistantConversation вызывает отдельный purge владельца с versioned mutation и требует точную ссылку ответа до HTTP204; историю напрямую не стирает и удаление посторонних ресурсов не утверждает.
func (server *Server) PurgeAssistantConversation(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.PurgeAssistantConversationParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.PurgeAssistantConversation(r.Context(), &controlplanev1.PurgeAssistantConversationRequest{Mutation: mutation, ConversationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetConversationRef() != ref {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// MoveAssistantConversationToProject decodes the target project under a mandatory versioned mutation and checks the exact returned conversation/project/version. Destination eligibility and ownership transfer remain owner decisions.
// MoveAssistantConversationToProject разбирает целевой проект с обязательной versioned mutation и сверяет точные conversation/project/version ответа. Допуск назначения и перенос владения остаются решениями владельца.
func (server *Server) MoveAssistantConversationToProject(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.MoveAssistantConversationToProjectParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	body, ok := decodeJSON[generated.MoveAssistantConversationToProjectJSONBody](w, r)
	if !ok {
		return
	}
	mutation, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.MoveAssistantConversationToProject(r.Context(), &controlplanev1.MoveAssistantConversationToProjectRequest{
		Mutation: mutation, ConversationRef: ref, ProjectRef: body.ProjectRef,
	})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	conversation := response.GetConversation()
	if conversation == nil || conversation.Ref != ref || conversation.ProjectRef != body.ProjectRef || !validManagedVersion(conversation.Version) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "conversation", "")
}

// CreateAssistantConversation requires a valid public scope and a project selector for PROJECT, maps optional context and sends create idempotency to the owner. Context fields and requested scope do not create actor authority.
// CreateAssistantConversation требует допустимую область и project selector для PROJECT, преобразует необязательный context и передаёт идемпотентный create владельцу. Поля context и область полномочия actor не создают.
func (server *Server) CreateAssistantConversation(w http.ResponseWriter, r *http.Request, p generated.CreateAssistantConversationParams) {
	body, ok := decodeJSON[generated.CreateAssistantConversationJSONBody](w, r)
	if !ok {
		return
	}
	if !body.AssistantScope.Valid() || body.AssistantScope == generated.AssistantScopePROJECT && (body.ProjectRef == nil || !opaqueHTTPReference.MatchString(*body.ProjectRef)) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Assistant.CreateAssistantConversation(r.Context(), &controlplanev1.CreateAssistantConversationRequest{Mutation: m, ProjectRef: stringValue(body.ProjectRef), Context: assistantContextInput(body.Context), AssistantScope: assistantScopeInput(body.AssistantScope)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusCreated, response, "conversation", "")
}

// UpdateAssistantConversationTitle forwards decoded title, exact conversation and mutation/version pins to the owner without generating or assigning title revision locally.
// UpdateAssistantConversationTitle передаёт разобранное имя, точный диалог и mutation/version pins владельцу без локальной генерации и назначения title revision.
func (server *Server) UpdateAssistantConversationTitle(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.UpdateAssistantConversationTitleParams) {
	body, ok := decodeJSON[generated.UpdateAssistantConversationTitleJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.UpdateAssistantConversationTitle(r.Context(), &controlplanev1.UpdateAssistantConversationTitleRequest{Mutation: m, ConversationRef: ref, Title: body.Title})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "conversation", "")
}

// AddAssistantTurn first reads and checks actual bounded UTF-8 bytes, then closed-decodes and validates shared turn content. It defaults to QUEUE unless INTERRUPT_ACTIVE is explicitly selected and returns HTTP202 after owner admission; it neither runs the model nor declares the turn completed.
// AddAssistantTurn сначала читает и проверяет фактические ограниченные UTF-8 байты, затем закрыто разбирает и проверяет content общим контрактом. По умолчанию выбирает QUEUE, кроме явного INTERRUPT_ACTIVE, и выдаёт HTTP202 после допуска владельца; модель не запускает и завершение хода не объявляет.
func (server *Server) AddAssistantTurn(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.AddAssistantTurnParams) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maximumJSONBody+1))
	if err != nil || len(raw) > maximumJSONBody || !utf8.Valid(raw) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	body, ok := decodeJSON[generated.AddAssistantTurnJSONBody](w, r)
	if !ok {
		return
	}
	if !runtimecontract.ValidAssistantTurnContent(body.Content) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	m, _ := requireMutation(w, p.IdempotencyKey, "")
	deliveryMode := controlplanev1.AssistantTurnDeliveryMode_ASSISTANT_TURN_DELIVERY_MODE_QUEUE
	if body.DeliveryMode != nil && string(*body.DeliveryMode) == "INTERRUPT_ACTIVE" {
		deliveryMode = controlplanev1.AssistantTurnDeliveryMode_ASSISTANT_TURN_DELIVERY_MODE_INTERRUPT_ACTIVE
	}
	response, err := server.control.Assistant.AddAssistantTurn(r.Context(), &controlplanev1.AddAssistantTurnRequest{Mutation: m, ConversationRef: ref, Content: body.Content, AttachmentSetRef: stringValue(body.AttachmentSetRef), Context: assistantContextInput(body.Context), DeliveryMode: deliveryMode})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusAccepted, response, "conversation", "")
}

// CancelAssistantTurn requires valid conversation syntax/versioned mutation and checks exact returned conversation plus cancelled/run-ref consistency. Cancellation graph, claims and provider interruption belong to the owner, not this transport.
// CancelAssistantTurn требует формат диалога и versioned mutation и сверяет диалог и согласованность cancelled/run-ref ответа. Граф отмены, claims и прерывание провайдера принадлежат владельцу, а не transport.
func (server *Server) CancelAssistantTurn(w http.ResponseWriter, r *http.Request, ref generated.ConversationRef, p generated.CancelAssistantTurnParams) {
	if !opaqueHTTPReference.MatchString(ref) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	m, ok := requireVersionedMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.CancelAssistantTurn(r.Context(), &controlplanev1.CancelAssistantTurnRequest{Mutation: m, ConversationRef: ref})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if response.GetConversationRef() != ref || response.GetCancelled() && !opaqueHTTPReference.MatchString(response.GetRunRef()) || !response.GetCancelled() && response.GetRunRef() != "" {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}

// ApplyAssistantPlan forwards explicit plan revision with mutation/version context to owner apply, without treating edited parameters or a chat acknowledgement as approval authority.
// ApplyAssistantPlan передаёт явную ревизию плана с mutation/version context применению владельца без принятия изменённых параметров и подтверждения в чате за полномочия.
func (server *Server) ApplyAssistantPlan(w http.ResponseWriter, r *http.Request, ref generated.PlanRef, p generated.ApplyAssistantPlanParams) {
	body, decoded := decodeJSON[generated.ApplyAssistantPlanJSONBody](w, r)
	if !decoded {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.ApplyAssistantPlan(r.Context(), &controlplanev1.ApplyAssistantPlanRequest{Mutation: m, PlanRef: ref, Revision: body.Revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}

// UpdateAssistantPlanDraft uses the larger nominal decoder budget, maps proposed operations and mutation pins, then delegates draft update to the owner; mapped before/after data is not trusted execution state.
// UpdateAssistantPlanDraft использует больший номинальный бюджет decoder, преобразует предлагаемые операции и mutation pins и поручает update черновика владельцу; before/after не являются доверенным состоянием исполнения.
func (server *Server) UpdateAssistantPlanDraft(w http.ResponseWriter, r *http.Request, ref generated.PlanRef, p generated.UpdateAssistantPlanDraftParams) {
	body, ok := decodeJSONWithLimit[generated.UpdateAssistantPlanDraftJSONBody](w, r, maximumAssistantPlanDraftJSONBody)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.UpdateAssistantPlanDraft(r.Context(), &controlplanev1.UpdateAssistantPlanDraftRequest{Mutation: m, PlanRef: ref, Summary: body.Summary, Operations: assistantPlanOperationsInput(body.Operations)})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "plan", "")
}

// ValidateAssistantPlan forwards exact plan/revision and mutation context to the owner validation command, returning its public plan without applying changes.
// ValidateAssistantPlan передаёт точные plan/revision и mutation context проверке владельца, возвращая публичный план без применения изменений.
func (server *Server) ValidateAssistantPlan(w http.ResponseWriter, r *http.Request, ref generated.PlanRef, p generated.ValidateAssistantPlanParams) {
	body, ok := decodeJSON[generated.ValidateAssistantPlanJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.ValidateAssistantPlan(r.Context(), &controlplanev1.ValidateAssistantPlanRequest{Mutation: m, PlanRef: ref, Revision: body.Revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "plan", "")
}

// RejectAssistantPlan sends explicit plan revision and mutation/version pins to owner rejection; terminal/delegation effects remain in the owner transaction.
// RejectAssistantPlan передаёт явную ревизию плана и mutation/version pins отклонению владельца; terminal и delegation effects остаются в его транзакции.
func (server *Server) RejectAssistantPlan(w http.ResponseWriter, r *http.Request, ref generated.PlanRef, p generated.RejectAssistantPlanParams) {
	body, ok := decodeJSON[generated.RejectAssistantPlanJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.RejectAssistantPlan(r.Context(), &controlplanev1.RejectAssistantPlanRequest{Mutation: m, PlanRef: ref, Revision: body.Revision})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "", "")
}

// assistantContextInput preserves absent context and maps route/resource/version plus requested operation enums, allocating a new operation slice but retaining the optional version pointer. This is an untrusted selection descriptor, not actor eligibility.
// assistantContextInput сохраняет отсутствие context и преобразует route/resource/version и запрошенные operation enums, создавая новый список операций, но сохраняя pointer версии. Это непроверенный descriptor выбора, а не допуск actor.
func assistantContextInput(input *generated.AssistantContextDescriptor) *controlplanev1.AssistantContextDescriptor {
	if input == nil {
		return nil
	}
	result := &controlplanev1.AssistantContextDescriptor{Route: input.Route, EntityKind: input.EntityKind,
		EntityRef: input.EntityRef, EntityName: input.EntityName, EntityVersion: input.EntityVersion}
	for _, operation := range input.AllowedOperations {
		result.AllowedOperations = append(result.AllowedOperations,
			controlplanev1.AssistantPlanOperation_Type(controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+string(operation)]))
	}
	return result
}

// assistantPlanOperationsInput maps proposed typed operations/targets and JSON before/after/parameters while preserving ignored Struct conversion errors and optional version pointers. It does not validate, approve or execute the proposed operations.
// assistantPlanOperationsInput преобразует предлагаемые операции, targets и JSON before/after/parameters с прежним игнорированием Struct-ошибок и сохранением pointers версий. Операции не проверяет, не подтверждает и не выполняет.
func assistantPlanOperationsInput(items []generated.AssistantPlanOperationInput) []*controlplanev1.AssistantPlanOperation {
	result := make([]*controlplanev1.AssistantPlanOperation, 0, len(items))
	for _, item := range items {
		parameters, _ := structpb.NewStruct(item.Parameters)
		before, _ := structpb.NewStruct(item.Before)
		after, _ := structpb.NewStruct(item.After)
		targetRef := ""
		if item.Target.Ref != nil {
			targetRef = string(*item.Target.Ref)
		}
		targetVersion := item.Target.Version
		result = append(result, &controlplanev1.AssistantPlanOperation{Ref: string(item.Ref),
			Type:   controlplanev1.AssistantPlanOperation_Type(controlplanev1.AssistantPlanOperation_Type_value["TYPE_"+string(item.Type)]),
			Action: controlplanev1.AssistantPlanOperation_Action(controlplanev1.AssistantPlanOperation_Action_value["ACTION_"+string(item.Action)]),
			Title:  item.Title, Summary: item.Summary, TargetKind: item.Target.Kind, TargetRef: targetRef,
			TargetName: item.Target.Name, TargetVersion: targetVersion, ExpectedVersion: item.ExpectedVersion, Parameters: parameters, Before: before,
			After: after, Selected: item.Selected})
	}
	return result
}

// UpdateSystemAssistantOwnerInstructions decodes owner instructions and mutation pins for the dedicated owner update, without editing immutable platform instructions or bypassing confirmation policy.
// UpdateSystemAssistantOwnerInstructions разбирает owner instructions и mutation pins отдельного update владельца без изменения неизменяемых платформенных инструкций и обхода политики подтверждения.
func (server *Server) UpdateSystemAssistantOwnerInstructions(w http.ResponseWriter, r *http.Request, p generated.UpdateSystemAssistantOwnerInstructionsParams) {
	body, ok := decodeJSON[generated.UpdateSystemAssistantOwnerInstructionsJSONBody](w, r)
	if !ok {
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.UpdateAssistantOwnerInstructions(r.Context(), &controlplanev1.UpdateAssistantOwnerInstructionsRequest{Mutation: m, Instructions: body.OwnerInstructions})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "assistant", "")
}

// CommandSystemAssistant accepts only RECOVER and forwards mutation/version pins to the dedicated owner recovery command; it does not restart arbitrary workloads or bypass account readiness.
// CommandSystemAssistant принимает только RECOVER и передаёт mutation/version pins отдельному восстановлению владельца; произвольные workloads не перезапускает и readiness аккаунта не обходит.
func (server *Server) CommandSystemAssistant(w http.ResponseWriter, r *http.Request, p generated.CommandSystemAssistantParams) {
	body, ok := decodeJSON[generated.CommandSystemAssistantJSONBody](w, r)
	if !ok {
		return
	}
	if body.Action != generated.RECOVER {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	m, ok := requireMutation(w, p.IdempotencyKey, p.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Assistant.RecoverSystemAssistant(r.Context(), &controlplanev1.RecoverSystemAssistantRequest{Mutation: m})
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	writeMessage(w, http.StatusOK, response, "assistant", "")
}
