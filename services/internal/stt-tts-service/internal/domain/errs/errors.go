// Package errs содержит безопасные доменные ошибки STT.
package errs

import (
	"errors"
	"time"
)

var (
	ErrInvalidRequest        = errors.New("transcription request is invalid")
	ErrAudioTooLarge         = errors.New("audio payload exceeds the configured limit")
	ErrAudioTooLong          = errors.New("audio duration exceeds the configured limit")
	ErrUnsupportedAudio      = errors.New("audio format is unsupported")
	ErrPermissionDenied      = errors.New("transcription permission is denied")
	ErrPolicyUnavailable     = errors.New("transcription policy is unavailable")
	ErrCredentialUnavailable = errors.New("provider credential is unavailable")
	ErrGrantRevoked          = errors.New("transcription grant is revoked or stale")
	ErrProviderUnavailable   = errors.New("transcription provider is unavailable")
	ErrProviderRateLimited   = errors.New("transcription provider rate limit exceeded")
	ErrProviderRejected      = errors.New("transcription provider rejected the request")
	ErrDelegatedProofPending = errors.New("delegated authorization proof is not materialized")
	ErrEgressUnavailable     = errors.New("provider egress is unavailable")
)

// ProviderRateLimit хранит только проверенный cooldown, никогда provider body.
// Нулевой RetryAfter означает отсутствие пригодного bounded hint.
type ProviderRateLimit struct{ RetryAfter time.Duration }

func (*ProviderRateLimit) Error() string { return ErrProviderRateLimited.Error() }
func (*ProviderRateLimit) Unwrap() error { return ErrProviderRateLimited }
