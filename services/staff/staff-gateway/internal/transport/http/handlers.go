package httptransport

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

type handlers struct {
	interactionHub InteractionHubClient
	openAPI        *OpenAPIContract
}

func newHandlers(interactionHub InteractionHubClient, openAPI *OpenAPIContract) handlers {
	return handlers{interactionHub: interactionHub, openAPI: openAPI}
}

func (h handlers) listOwnerInboxItems(w http.ResponseWriter, req *http.Request) {
	handleOwnerInboxQuery(w, req, ListOwnerInboxItemsRequest, h.interactionHub.ListOwnerInboxItems, ListOwnerInboxItemsResponse)
}

func (h handlers) getOwnerInboxItem(w http.ResponseWriter, req *http.Request) {
	handleOwnerInboxQuery(w, req, GetOwnerInboxItemRequest, h.interactionHub.GetOwnerInboxItem, OwnerInboxItemResponse)
}

func (h handlers) respondOwnerInboxItem(w http.ResponseWriter, req *http.Request) {
	body, safeErr := decodeOwnerInboxRespondBody(req)
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	input, err := RecordInteractionResponseRequest(req, body)
	if err != nil {
		WriteSafeError(w, req, err)
		return
	}
	response, callErr := h.interactionHub.RecordInteractionResponse(req.Context(), input)
	if callErr != nil {
		WriteSafeError(w, req, interactionHubError(callErr))
		return
	}
	output, err := OwnerInboxRespondResponse(response, requestIDFromContext(req.Context()))
	if err != nil {
		WriteSafeError(w, req, err)
		return
	}
	writeJSON(w, http.StatusOK, output)
}

func decodeOwnerInboxRespondBody(req *http.Request) (OwnerInboxRespondBody, *SafeError) {
	var body OwnerInboxRespondBody
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		return OwnerInboxRespondBody{}, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request body is invalid", false)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); err != io.EOF {
		return OwnerInboxRespondBody{}, NewSafeError(http.StatusBadRequest, CodeInvalidRequest, "request body must contain one JSON object", false)
	}
	return body, nil
}

func (h handlers) openAPISpec(w http.ResponseWriter, req *http.Request) {
	data, err := h.openAPI.Read()
	if err != nil {
		WriteSafeError(w, req, WrapSafeError(http.StatusInternalServerError, CodeDownstreamUnavailable, "OpenAPI spec is unavailable", true, err))
		return
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func handleOwnerInboxQuery[Request any, Response any, Output any](
	w http.ResponseWriter,
	req *http.Request,
	build func(*http.Request) (*Request, *SafeError),
	call func(context.Context, *Request) (*Response, error),
	cast func(*Response, string) (Output, *SafeError),
) {
	input, safeErr := build(req)
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	response, err := call(req.Context(), input)
	if err != nil {
		WriteSafeError(w, req, interactionHubError(err))
		return
	}
	output, safeErr := cast(response, requestIDFromContext(req.Context()))
	if safeErr != nil {
		WriteSafeError(w, req, safeErr)
		return
	}
	writeJSON(w, http.StatusOK, output)
}
