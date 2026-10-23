package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// CreateAttachmentSetDraft Validates the route project first, decodes the body and idempotency header, then invokes the specialized draft command. Purpose and artifact references remain untrusted input for owner-side eligibility; the response is a created attachment-set envelope.
// CreateAttachmentSetDraft Сначала проверяет проект маршрута, затем разбирает тело и ключ идемпотентности и вызывает отдельную команду черновика. Цель и ссылки на артефакты остаются входными данными для проверки владельцем; ответ выдаётся как созданный набор вложений.
func (server *Server) CreateAttachmentSetDraft(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.CreateAttachmentSetDraftParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	body, mutation, ok := decodeMutationBody[generated.AttachmentSetDraftInput](writer, request, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Command.CreateAttachmentSetDraft(request.Context(), &controlplanev1.CreateAttachmentSetDraftRequest{
		Mutation: mutation, ProjectRef: projectRef, Purpose: attachmentSetPurposeProto(string(body.Purpose)),
		ArtifactRefs: sliceOrEmpty(body.ArtifactRefs),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "attachmentSet", "")
}

// CreateOrganizationAttachmentSetDraft Creates an organization-scoped draft through its own RPC without synthesizing a project reference. Body decoding precedes mutation validation, and organization ownership is resolved by the trusted service context.
// CreateOrganizationAttachmentSetDraft Создаёт организационный черновик отдельным RPC без выдуманной ссылки проекта. Тело разбирается до контекста изменения; принадлежность организации определяется доверенным контекстом сервиса.
func (server *Server) CreateOrganizationAttachmentSetDraft(writer http.ResponseWriter, request *http.Request, parameters generated.CreateOrganizationAttachmentSetDraftParams) {
	body, mutation, ok := decodeMutationBody[generated.AttachmentSetDraftInput](writer, request, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Command.CreateOrganizationAttachmentSetDraft(request.Context(), &controlplanev1.CreateOrganizationAttachmentSetDraftRequest{
		Mutation: mutation, Purpose: attachmentSetPurposeProto(string(body.Purpose)),
		ArtifactRefs: sliceOrEmpty(body.ArtifactRefs),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "attachmentSet", "")
}

// AddAttachmentSetItems Sends exact set and artifact references with mutation pins and an optional insertion position, defaulting to zero. The owner command checks eligibility, ordering and version; this adapter neither fetches nor admits file contents.
// AddAttachmentSetItems Передаёт точные ссылки набора и артефактов с контекстом изменения и необязательной позицией вставки, по умолчанию нулевой. Владелец проверяет допуск, порядок и версию; адаптер не читает и не допускает содержимое файлов.
func (server *Server) AddAttachmentSetItems(writer http.ResponseWriter, request *http.Request, ref generated.AttachmentSetRef, parameters generated.AddAttachmentSetItemsParams) {
	body, mutation, ok := decodeMutationBody[generated.AttachmentSetAddItemsInput](writer, request, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	position := int64(0)
	if body.InsertAfterPosition != nil {
		position = *body.InsertAfterPosition
	}
	response, err := server.control.Command.AddAttachmentSetItems(request.Context(), &controlplanev1.AddAttachmentSetItemsRequest{
		Mutation: mutation, AttachmentSetRef: ref, ArtifactRefs: body.ArtifactRefs, InsertAfterPosition: position,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "attachmentSet", "")
}

// RemoveAttachmentSetItems Removes the selected references through the dedicated set command after body and version-header parsing. It changes set membership through the owner service rather than directly deleting stored artifacts.
// RemoveAttachmentSetItems Удаляет выбранные ссылки отдельной командой набора после разбора тела и заголовка версии. Состав набора меняется через сервис-владелец, без прямого удаления хранимых артефактов.
func (server *Server) RemoveAttachmentSetItems(writer http.ResponseWriter, request *http.Request, ref generated.AttachmentSetRef, parameters generated.RemoveAttachmentSetItemsParams) {
	body, mutation, ok := decodeMutationBody[generated.AttachmentSetRemoveItemsInput](writer, request, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.RemoveAttachmentSetItems(request.Context(), &controlplanev1.RemoveAttachmentSetItemsRequest{
		Mutation: mutation, AttachmentSetRef: ref, ArtifactRefs: body.ArtifactRefs,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "attachmentSet", "")
}

// FinalizeAttachmentSet Passes the exact set reference, expected version and idempotency key to the owner finalization command. No request body or local state transition is invented; the owner response retains the existing created status and envelope.
// FinalizeAttachmentSet Передаёт точную ссылку набора, ожидаемую версию и ключ идемпотентности команде завершения владельца. Тело запроса и локальный переход не выдумываются; ответ сохраняет прежний статус создания и оболочку.
func (server *Server) FinalizeAttachmentSet(writer http.ResponseWriter, request *http.Request, ref generated.AttachmentSetRef, parameters generated.FinalizeAttachmentSetParams) {
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
	if !ok {
		return
	}
	response, err := server.control.Command.FinalizeAttachmentSet(request.Context(), &controlplanev1.FinalizeAttachmentSetRequest{
		Mutation: mutation, AttachmentSetRef: ref,
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "attachmentSet", "")
}

// GetAttachmentSet Reads one set with pagination, normalizes the protobuf view and exposes a nonempty next-page token beside the set envelope. RPC and normalization errors stop before JSON publication; authorization remains with the owner query.
// GetAttachmentSet Читает один набор с пагинацией, нормализует protobuf-представление и выдаёт непустой token следующей страницы рядом с набором. Ошибки RPC и нормализации останавливают выдачу JSON; авторизация остаётся в запросе владельца.
func (server *Server) GetAttachmentSet(writer http.ResponseWriter, request *http.Request, ref generated.AttachmentSetRef, parameters generated.GetAttachmentSetParams) {
	response, err := server.control.Query.GetAttachmentSet(request.Context(), &controlplanev1.GetAttachmentSetRequest{
		AttachmentSetRef: ref, Page: page(parameters.PageSize, parameters.PageToken),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	value, err := messageMap(response)
	if err != nil {
		writeLocalProblem(writer, http.StatusInternalServerError, "INTERNAL", false)
		return
	}
	output := map[string]any{"attachmentSet": value["attachmentSet"]}
	if pageValue, ok := value["page"].(map[string]any); ok {
		if token, ok := pageValue["nextPageToken"].(string); ok && token != "" {
			output["nextPageToken"] = token
		}
	}
	writeJSON(writer, http.StatusOK, output)
}

// attachmentSetPurposeProto Maps the public purpose suffix to the generated protobuf enum; unknown names retain the zero enum value for downstream validation rather than acquiring a fallback purpose.
// attachmentSetPurposeProto Сопоставляет публичный суффикс цели с generated enum protobuf; неизвестное имя сохраняет нулевое значение для последующей проверки, не получая цель по умолчанию.
func attachmentSetPurposeProto(value string) controlplanev1.AttachmentSetPurpose {
	return controlplanev1.AttachmentSetPurpose(controlplanev1.AttachmentSetPurpose_value["ATTACHMENT_SET_PURPOSE_"+value])
}
