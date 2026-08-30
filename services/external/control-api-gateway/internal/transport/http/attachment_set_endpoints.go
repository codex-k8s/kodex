package httptransport

import (
	"net/http"

	controlplanev1 "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func (server *Server) CreateAttachmentSetDraft(writer http.ResponseWriter, request *http.Request, projectRef generated.ProjectRef, parameters generated.CreateAttachmentSetDraftParams) {
	request, ok := withProjectReference(writer, request, projectRef)
	if !ok {
		return
	}
	body, ok := decodeJSON[generated.AttachmentSetDraftInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
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

func (server *Server) CreateOrganizationAttachmentSetDraft(writer http.ResponseWriter, request *http.Request, parameters generated.CreateOrganizationAttachmentSetDraftParams) {
	body, ok := decodeJSON[generated.AttachmentSetDraftInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, "")
	if !ok {
		return
	}
	response, err := server.control.Command.CreateAttachmentSetDraft(request.Context(), &controlplanev1.CreateAttachmentSetDraftRequest{
		Mutation: mutation, Purpose: attachmentSetPurposeProto(string(body.Purpose)),
		ArtifactRefs: sliceOrEmpty(body.ArtifactRefs),
	})
	if err != nil {
		writeRPCProblem(writer, err)
		return
	}
	writeMessage(writer, http.StatusCreated, response, "attachmentSet", "")
}

func (server *Server) AddAttachmentSetItems(writer http.ResponseWriter, request *http.Request, ref generated.AttachmentSetRef, parameters generated.AddAttachmentSetItemsParams) {
	body, ok := decodeJSON[generated.AttachmentSetAddItemsInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
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

func (server *Server) RemoveAttachmentSetItems(writer http.ResponseWriter, request *http.Request, ref generated.AttachmentSetRef, parameters generated.RemoveAttachmentSetItemsParams) {
	body, ok := decodeJSON[generated.AttachmentSetRemoveItemsInput](writer, request)
	if !ok {
		return
	}
	mutation, ok := requireMutation(writer, parameters.IdempotencyKey, parameters.IfMatch)
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

func attachmentSetPurposeProto(value string) controlplanev1.AttachmentSetPurpose {
	return controlplanev1.AttachmentSetPurpose(controlplanev1.AttachmentSetPurpose_value["ATTACHMENT_SET_PURPOSE_"+value])
}
