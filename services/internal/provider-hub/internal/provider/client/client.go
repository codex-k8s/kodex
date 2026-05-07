// Package client defines provider adapter contracts used by provider-hub.
package client

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
)

// AccountCredential carries an ephemeral provider token resolved outside provider-hub storage.
type AccountCredential struct {
	ExternalAccountID uuid.UUID
	ProviderSlug      enum.ProviderSlug
	Token             string
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

// Adapter isolates provider-specific API details from provider-hub domain logic.
type Adapter interface {
	ProviderSlug() enum.ProviderSlug
	ProbeAccount(context.Context, AccountProbeRequest) (AccountProbeResult, error)
}
