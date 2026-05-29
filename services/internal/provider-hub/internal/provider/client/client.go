// Package client defines provider adapter contracts used by provider-hub.
package client

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
)

// ErrorKind classifies provider API failures without leaking provider tokens.
type ErrorKind string

const (
	ErrorKindRateLimited ErrorKind = "rate_limited"
	ErrorKindAuthFailed  ErrorKind = "auth_failed"
	ErrorKindNotFound    ErrorKind = "not_found"
	ErrorKindTransient   ErrorKind = "transient"
	ErrorKindConflict    ErrorKind = "conflict"
	ErrorKindValidation  ErrorKind = "validation"
	ErrorKindPermanent   ErrorKind = "permanent"
	ErrorKindUnsupported ErrorKind = "unsupported"
)

var (
	ErrRateLimited = errors.New("provider rate limited")
	ErrAuthFailed  = errors.New("provider auth failed")
	ErrNotFound    = errors.New("provider object not found")
	ErrTransient   = errors.New("provider transient error")
	ErrConflict    = errors.New("provider conflict")
	ErrValidation  = errors.New("provider validation error")
	ErrPermanent   = errors.New("provider permanent error")
	ErrUnsupported = errors.New("provider reconciliation unsupported")
)

// Error preserves provider failure class and retry hint without provider credentials.
type Error struct {
	Kind       ErrorKind
	RetryAfter time.Duration
	Cause      error
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	switch e.Kind {
	case ErrorKindRateLimited:
		return ErrRateLimited.Error()
	case ErrorKindAuthFailed:
		return ErrAuthFailed.Error()
	case ErrorKindNotFound:
		return ErrNotFound.Error()
	case ErrorKindTransient:
		return ErrTransient.Error()
	case ErrorKindConflict:
		return ErrConflict.Error()
	case ErrorKindValidation:
		return ErrValidation.Error()
	case ErrorKindUnsupported:
		return ErrUnsupported.Error()
	default:
		return ErrPermanent.Error()
	}
}

func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	if e.Cause != nil {
		return e.Cause
	}
	switch e.Kind {
	case ErrorKindRateLimited:
		return ErrRateLimited
	case ErrorKindAuthFailed:
		return ErrAuthFailed
	case ErrorKindNotFound:
		return ErrNotFound
	case ErrorKindTransient:
		return ErrTransient
	case ErrorKindConflict:
		return ErrConflict
	case ErrorKindValidation:
		return ErrValidation
	case ErrorKindUnsupported:
		return ErrUnsupported
	default:
		return ErrPermanent
	}
}

// AccountCredential carries an ephemeral provider token resolved outside provider-hub storage.
type AccountCredential struct {
	ExternalAccountID uuid.UUID
	ProviderSlug      enum.ProviderSlug
	Token             secretresolver.SecretValue
}

// AccountProbeRequest asks an adapter to verify account reachability and known limits.
type AccountProbeRequest struct {
	Credential AccountCredential
	ObservedAt time.Time
}

// AccountProbeResult returns normalized account state and limit snapshots.
type AccountProbeResult struct {
	RuntimeState   entity.ProviderAccountRuntimeState
	LimitSnapshots []entity.ProviderLimitSnapshot
}

// ReconciliationRequest asks an adapter to read provider state for one leased cursor.
type ReconciliationRequest struct {
	Credential AccountCredential
	Cursor     entity.SyncCursor
	MaxItems   int32
	ObservedAt time.Time
}

// ReconciliationResult contains provider snapshots and safe scheduling hints.
type ReconciliationResult struct {
	WorkItems           []value.ProviderWorkItemSnapshot
	Comments            []value.ProviderCommentSnapshot
	LimitSnapshots      []entity.ProviderLimitSnapshot
	NextCursorValue     string
	RateBudgetStateJSON []byte
	RetryAfter          time.Duration
}

// Adapter isolates provider-specific API details from provider-hub domain logic.
type Adapter interface {
	ProviderSlug() enum.ProviderSlug
	ProbeAccount(context.Context, AccountProbeRequest) (AccountProbeResult, error)
	Reconcile(context.Context, ReconciliationRequest) (ReconciliationResult, error)
}

// WebhookRefetchRequest описывает безопасное повторное чтение provider-факта без raw webhook body.
type WebhookRefetchRequest struct {
	Credential AccountCredential
	Webhook    entity.WebhookEvent
	Envelope   value.WebhookPayloadEnvelope
	ObservedAt time.Time
}

// WebhookRefetchResult возвращает нормализованные safe facts после provider API read.
type WebhookRefetchResult struct {
	Facts value.ProviderWebhookFacts
	OK    bool
}

// WebhookRefetcher умеет перечитать поддержанный webhook-факт по safe refs.
type WebhookRefetcher interface {
	ProviderSlug() enum.ProviderSlug
	RefetchWebhook(context.Context, WebhookRefetchRequest) (WebhookRefetchResult, error)
}
