// Package transcription задаёт узкие порты владельцев policy, credential и provider.
package transcription

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/stt-tts-service/internal/domain/types/value"
)

type PolicyResolver interface {
	Resolve(context.Context, value.Principal, string) (value.Policy, error)
	Check(context.Context) error
}

type CredentialProjector interface {
	Project(context.Context, value.Principal, string, value.Policy) (value.Credential, error)
	Check(context.Context) error
}

type Provider interface {
	Transcribe(context.Context, value.ProviderRequest) (string, error)
	Check(context.Context) error
}
