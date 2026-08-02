// Package continuation задаёт узкий порт специализированного lifecycle
// integration continuation в control-plane.
package continuation

import (
	"context"
	"time"

	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/enum"
)

type State struct {
	ID                string
	Version           uint64
	Fence             uint64
	ApprovalState     string
	ExecutionState    string
	ContinuationState string
}

type Command struct {
	Action             enum.ContinuationAction
	IdempotencyKey     string
	ApplicationGrant   string
	InvocationID       string
	ApprovalID         string
	RequestDigest      string
	IntegrationID      string
	IntegrationVersion uint64
	IntegrationDigest  string
	CredentialBindings []entity.PinnedCredentialBinding
	ApprovalExpiresAt  time.Time
	ContinuationID     string
	ExpectedVersion    uint64
	ExpectedFence      uint64
	DecisionReference  string
	DecisionDigest     string
	ResultReference    string
	ResultDigest       string
	ErrorCode          string
	ErrorReference     string
	ErrorDigest        string
}

type Client interface {
	Apply(context.Context, Command) (State, error)
	Check(context.Context) error
}
