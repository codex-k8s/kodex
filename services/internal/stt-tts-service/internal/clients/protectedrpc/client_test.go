package protectedrpc

import (
	"testing"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

// TestDelegatedProofRejectsMissingVerifiedParent refuses a registered child operation when no accepted parent authorization exists in context.
// RU: TestDelegatedProofRejectsMissingVerifiedParent отклоняет зарегистрированную дочернюю операцию без принятой родительской авторизации в context.
func TestDelegatedProofRejectsMissingVerifiedParent(t *testing.T) {
	client := &Client{}
	_, err := client.BindDelegated(t.Context(), value.Principal{}, "request", "correlation",
		sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName, "platform.stt.policy.resolve")
	if err == nil {
		t.Fatal("continuation без проверенного parent принят")
	}
}

// TestDelegatedProofRejectsUnknownOperation rejects an unregistered method/operation pair instead of constructing an arbitrary continuation.
// RU: TestDelegatedProofRejectsUnknownOperation отклоняет незарегистрированную пару метода/операции вместо создания произвольного continuation.
func TestDelegatedProofRejectsUnknownOperation(t *testing.T) {
	client := &Client{}
	if _, err := client.BindDelegated(t.Context(), value.Principal{}, "request", "correlation", "/unknown", "unknown"); err == nil {
		t.Fatal("незарегистрированная операция принята")
	}
}
