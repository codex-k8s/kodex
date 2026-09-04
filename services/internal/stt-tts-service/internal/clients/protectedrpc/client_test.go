package protectedrpc

import (
	"errors"
	"testing"

	sttv1 "github.com/codex-k8s/kodex/libs/go/sttapi/gen/stt/v1"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

func TestDelegatedProofFailsClosedBeforeRPC(t *testing.T) {
	client := &Client{}
	_, err := client.BindDelegated(t.Context(), value.Principal{}, "request", "correlation",
		sttv1.TranscriptionPolicyProjectionService_ResolveTranscriptionPolicy_FullMethodName, "platform.stt.policy.resolve")
	if !errors.Is(err, errs.ErrDelegatedProofPending) {
		t.Fatalf("ожидался закрытый отказ continuation proof, получено %v", err)
	}
}

func TestDelegatedProofRejectsUnknownOperation(t *testing.T) {
	client := &Client{}
	if _, err := client.BindDelegated(t.Context(), value.Principal{}, "request", "correlation", "/unknown", "unknown"); err == nil {
		t.Fatal("незарегистрированная операция принята")
	}
}
