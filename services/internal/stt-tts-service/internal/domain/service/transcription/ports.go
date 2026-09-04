package transcription

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

type PolicyResolver interface {
	Resolve(context.Context, value.Principal, string, string) (value.Policy, error)
}

type CredentialProjector interface {
	Project(context.Context, value.Principal, string, string, value.Policy) (value.Credential, error)
}

type Provider interface {
	Transcribe(context.Context, value.ProviderRequest) (string, error)
	CheckLocal(context.Context) error
	CheckEgress(context.Context) error
}

type Observer interface {
	Observe(value.Stage, value.ErrorClass)
}

type ObserverFunc func(value.Stage, value.ErrorClass)

func (observe ObserverFunc) Observe(stage value.Stage, class value.ErrorClass) {
	observe(stage, class)
}
