// Package command contains the in-process logical SubmitHookEvent boundary.
package command

import (
	"context"
	"fmt"

	hookservice "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/types/value"
)

// Handler exposes logical hook operations without selecting HTTP or gRPC transport.
type Handler struct {
	service *hookservice.Service
}

// SubmitHookEventRequest carries a normalized envelope into the logical boundary.
type SubmitHookEventRequest struct {
	Envelope value.HookEnvelope
}

// SubmitHookEventResponse carries the normalized hook handler result.
type SubmitHookEventResponse struct {
	Result         value.HookHandlerResult
	Duplicate      bool
	RoutesAccepted int
}

// NewHandler creates a logical SubmitHookEvent handler.
func NewHandler(service *hookservice.Service) *Handler {
	return &Handler{service: service}
}

// Ready reports whether the logical boundary is composed.
func (h *Handler) Ready() bool {
	return h != nil && h.service != nil && h.service.Ready()
}

// SubmitHookEvent executes the logical command without exposing a physical endpoint.
func (h *Handler) SubmitHookEvent(ctx context.Context, request SubmitHookEventRequest) (SubmitHookEventResponse, error) {
	if !h.Ready() {
		return SubmitHookEventResponse{}, fmt.Errorf("logical SubmitHookEvent handler is not ready")
	}
	result, err := h.service.SubmitHookEvent(ctx, hookservice.SubmitHookEventInput{Envelope: request.Envelope})
	if err != nil {
		return SubmitHookEventResponse{}, err
	}
	return SubmitHookEventResponse{
		Result:         result.HandlerResult,
		Duplicate:      result.Duplicate,
		RoutesAccepted: result.RoutesAccepted,
	}, nil
}
