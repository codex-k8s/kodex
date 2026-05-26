package command

import (
	"context"
	"testing"

	hookservice "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/domain/service"
	hookstub "github.com/codex-k8s/kodex/services/internal/codex-hook-ingress/internal/repository/stub/hook"
)

func TestHandlerReadyReflectsDomainService(t *testing.T) {
	t.Parallel()

	service := hookservice.New(hookstub.NewRepository(), hookservice.Config{}, hookservice.Dependencies{})
	handler := NewHandler(service)
	if !handler.Ready() {
		t.Fatal("Ready() = false, want true")
	}
}

func TestHandlerRejectsMissingService(t *testing.T) {
	t.Parallel()

	_, err := NewHandler(nil).SubmitHookEvent(context.Background(), SubmitHookEventRequest{})
	if err == nil {
		t.Fatal("SubmitHookEvent() error is nil, want not ready error")
	}
}
