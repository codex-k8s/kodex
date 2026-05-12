// Package entity contains persisted aggregate models owned by fleet-manager.
package entity

import (
	"time"

	"github.com/google/uuid"

	outboxlib "github.com/codex-k8s/kodex/libs/go/outbox"
	"github.com/codex-k8s/kodex/services/internal/fleet-manager/internal/domain/types/enum"
)

// Base stores common aggregate metadata used for optimistic concurrency.
type Base struct {
	ID        uuid.UUID
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// FleetScope is a logical placement contour for one or more Kubernetes clusters.
type FleetScope struct {
	Base
	ScopeKey     string
	ScopeType    enum.FleetScopeType
	ScopeOwnerID *uuid.UUID
	OwnerRefJSON []byte
	DisplayName  string
	Status       enum.FleetScopeStatus
	IsDefault    bool
}

// Server is a managed or external host that may back Kubernetes clusters.
type Server struct {
	Base
	ServerKey         string
	ProviderType      enum.ServerProviderType
	Status            enum.ServerStatus
	PrimaryAddressRef string
	Region            string
	CapacityClass     string
	SecretStoreType   string
	SecretStoreRef    string
}

// KubernetesCluster is a runtime placement target inside a fleet scope.
type KubernetesCluster struct {
	Base
	FleetScopeID        uuid.UUID
	ServerID            *uuid.UUID
	ClusterKey          string
	Status              enum.KubernetesClusterStatus
	IsDefault           bool
	APIEndpointRef      string
	SecretStoreType     string
	SecretStoreRef      string
	KubernetesVersion   string
	Region              string
	CapacityClass       string
	LastHealthStatus    enum.ClusterHealthStatus
	LastHealthCheckedAt *time.Time
}

// ClusterConnectivityCheck is one Kubernetes API connectivity attempt.
type ClusterConnectivityCheck struct {
	ID           uuid.UUID
	ClusterID    uuid.UUID
	Status       enum.ConnectivityCheckStatus
	StartedAt    *time.Time
	FinishedAt   *time.Time
	LatencyMS    *int64
	ErrorCode    string
	ErrorMessage string
	CreatedAt    time.Time
}

// ClusterHealthSnapshot is a bounded health summary for placement and operators.
type ClusterHealthSnapshot struct {
	ID             uuid.UUID
	ClusterID      uuid.UUID
	HealthStatus   enum.ClusterHealthStatus
	CapacityStatus enum.CapacityStatus
	SummaryJSON    []byte
	CheckedAt      time.Time
	ErrorCode      string
	ErrorMessage   string
}

// PlacementRule describes one fleet-local rule used by placement resolution.
type PlacementRule struct {
	Base
	FleetScopeID    uuid.UUID
	RuleKey         string
	Status          enum.PlacementRuleStatus
	Priority        int64
	MatchJSON       []byte
	ConstraintsJSON []byte
}

// PlacementDecision stores one resolved or rejected placement decision.
type PlacementDecision struct {
	ID                 uuid.UUID
	CommandID          *uuid.UUID
	RequestFingerprint string
	Status             enum.PlacementDecisionStatus
	FleetScopeID       *uuid.UUID
	ClusterID          *uuid.UUID
	ProjectID          *uuid.UUID
	RepositoryID       *uuid.UUID
	RuntimeMode        enum.RuntimeMode
	RuntimeProfile     string
	InputJSON          []byte
	ReasonCode         string
	ReasonMessage      string
	UsedDefaultPath    bool
	CreatedAt          time.Time
}

// CommandResult stores idempotency trail for mutating fleet-manager commands.
type CommandResult struct {
	Key            string
	CommandID      *uuid.UUID
	IdempotencyKey string
	ActorType      string
	ActorID        string
	Operation      string
	AggregateType  string
	AggregateID    uuid.UUID
	ResultPayload  []byte
	CreatedAt      time.Time
}

// OutboxEvent stores a domain event until it is published to consumers.
type OutboxEvent struct {
	outboxlib.Event
	PublishedAt         *time.Time
	NextAttemptAt       time.Time
	LockedUntil         *time.Time
	FailureKind         string
	FailedPermanentlyAt *time.Time
	LastError           string
}
