// Package entity contains persisted aggregate models owned by provider-hub.
package entity

import (
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
)

// Base stores common aggregate metadata used for optimistic concurrency.
type Base struct {
	ID        uuid.UUID
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// ProviderAccountRuntimeState stores operational provider-side account state.
type ProviderAccountRuntimeState struct {
	Base
	ExternalAccountID uuid.UUID
	ProviderSlug      enum.ProviderSlug
	Status            enum.ProviderAccountRuntimeStatus
	LastCheckedAt     *time.Time
	LastSuccessAt     *time.Time
	LastErrorCode     string
	LastErrorMessage  string
}

// ProviderLimitSnapshot stores one observed provider rate or quota snapshot.
type ProviderLimitSnapshot struct {
	ID                uuid.UUID
	ExternalAccountID uuid.UUID
	ProviderSlug      enum.ProviderSlug
	LimitClass        string
	Remaining         *int64
	LimitValue        *int64
	ResetAt           *time.Time
	CapturedAt        time.Time
	Source            enum.ProviderLimitSource
}

// ProviderOperation stores audit and diagnostics for a provider operation.
type ProviderOperation struct {
	Base
	CommandID           string
	ActorID             *uuid.UUID
	ExternalAccountID   uuid.UUID
	ProviderSlug        enum.ProviderSlug
	OperationType       enum.ProviderOperationType
	TargetRef           string
	Status              enum.ProviderOperationStatus
	ResultRef           string
	ErrorCode           string
	ErrorMessage        string
	RateLimitSnapshotID *uuid.UUID
	StartedAt           time.Time
	FinishedAt          *time.Time
}
