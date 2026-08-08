package management

import (
	"context"
	"encoding/json"
	"time"

	domainrepo "github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/repository/gateway"
	"github.com/codex-k8s/matter-codex/services/external/integration-gateway/internal/domain/types/entity"
)

type StartAuthorizationCommand struct {
	Scope           domainrepo.Scope
	Connection      entity.ManagedProviderConnection
	Authorization   entity.ProviderAuthorization
	IdempotencyHash string
	RequestHash     string
	Audit           entity.AuditEvent
}

type RestartAuthorizationCommand struct {
	Scope                                                   domainrepo.Scope
	Operation                                               string
	PreviousID                                              string
	ExpectedVersion                                         uint64
	ExpectedConnectionVersion, ExpectedConnectionGeneration uint64
	Authorization                                           entity.ProviderAuthorization
	IdempotencyHash                                         string
	RequestHash                                             string
	Audit                                                   entity.AuditEvent
}

type CancelAuthorizationCommand struct {
	Scope           domainrepo.Scope
	AuthorizationID string
	ExpectedVersion uint64
	IdempotencyHash string
	RequestHash     string
	At              time.Time
	Audit           entity.AuditEvent
}

type RevokeConnectionCommand struct {
	Scope              domainrepo.Scope
	ConnectionID       string
	ExpectedVersion    uint64
	ExpectedGeneration uint64
	IdempotencyHash    string
	RequestHash        string
	At                 time.Time
	Audit              entity.AuditEvent
}

type ManagePoolCommand struct {
	Scope           domainrepo.Scope
	Action          string
	ExpectedVersion uint64
	Pool            entity.ManagedProviderPool
	IdempotencyHash string
	RequestHash     string
	Audit           entity.AuditEvent
}

type ConfigureIntegrationCommand struct {
	Scope           domainrepo.Scope
	ExpectedVersion uint64
	Configuration   entity.IntegrationConfiguration
	IdempotencyHash string
	RequestHash     string
	Audit           entity.AuditEvent
}

type ManageGitBindingCommand struct {
	Scope           domainrepo.Scope
	Action          string
	ExpectedVersion uint64
	Binding         entity.GitSourceBinding
	IdempotencyHash string
	RequestHash     string
	Audit           entity.AuditEvent
}

type ReconcileGitCommand struct {
	Scope                  domainrepo.Scope
	BindingID              string
	ExpectedVersion        uint64
	ExpectedSourceRevision uint64
	Reconciliation         entity.GitReconciliation
	IdempotencyHash        string
	RequestHash            string
	Audit                  entity.AuditEvent
}

type CreateTestCommand struct {
	Scope           domainrepo.Scope
	Receipt         entity.IntegrationTestReceipt
	Connection      entity.ManagedProviderConnection
	IdempotencyHash string
	RequestHash     string
	Audit           entity.AuditEvent
	At              time.Time
}

type EffectCompletion struct {
	Scope           domainrepo.Scope
	EffectID        string
	LeaseID         string
	LeaseFence      uint64
	Status          string
	FailureCategory string
	At              time.Time
}

type EffectFailure struct {
	Scope           domainrepo.Scope
	EffectID        string
	LeaseID         string
	LeaseFence      uint64
	Status          string
	FailureCategory string
	At              time.Time
}

type ProviderSyncCompletion struct {
	Scope                                 domainrepo.Scope
	EffectID, LeaseID                     string
	LeaseFence                            uint64
	ExpectedVersion, ExpectedGeneration   uint64
	ControlPlaneID                        string
	ControlPlaneVersion                   uint64
	ControlPlaneDigest, ObservationDigest string
	CredentialBindingDigest               string
	ObservedAt                            time.Time
}

type (
	PoolSyncCompletion struct {
		Scope                       domainrepo.Scope
		EffectID, LeaseID           string
		LeaseFence, ExpectedVersion uint64
		ControlPlaneID              string
		ControlPlaneVersion         uint64
		ControlPlaneDigest          string
		At                          time.Time
	}
	TestCompletion struct {
		Scope                    domainrepo.Scope
		EffectID, LeaseID        string
		LeaseFence               uint64
		TestID, Category, Digest string
		At                       time.Time
	}
	GitFetchCompletion struct {
		Scope             domainrepo.Scope
		EffectID, LeaseID string
		LeaseFence        uint64
		InputDigest       string
		Binding           entity.GitSourceBinding
		Reconciliation    entity.GitReconciliation
		At                time.Time
	}
	GitApplyCompletion struct {
		Scope                        domainrepo.Scope
		EffectID, LeaseID            string
		LeaseFence                   uint64
		ReconciliationID, ReadbackID string
		BindingID                    string
		BindingVersion               uint64
		BindingGeneration            uint64
		SourceRevision               uint64
		SourceDigest                 string
		ReadbackVersion              uint64
		ReadbackDigest               string
		At                           time.Time
	}
)

type Repository interface {
	ReplayManagement(context.Context, domainrepo.Scope, string, string, string) ([]byte, bool, error)
	StartAuthorization(context.Context, StartAuthorizationCommand) (entity.ProviderAuthorization, bool, error)
	RestartAuthorization(context.Context, RestartAuthorizationCommand) (entity.ProviderAuthorization, bool, error)
	CancelAuthorization(context.Context, CancelAuthorizationCommand) (entity.ProviderAuthorization, bool, error)
	GetAuthorization(context.Context, domainrepo.Scope, string) (entity.ProviderAuthorization, error)
	GetLatestAuthorization(context.Context, domainrepo.Scope, string) (entity.ProviderAuthorization, error)
	GetCredentialGeneration(context.Context, domainrepo.Scope, string, uint64) (entity.CredentialGeneration, error)
	ListCredentialGenerations(context.Context, domainrepo.Scope, string) ([]entity.CredentialGeneration, error)
	GetManagedConnection(context.Context, domainrepo.Scope, string) (entity.ManagedProviderConnection, error)
	ListConnections(context.Context, domainrepo.Scope, []string, int, string) ([]entity.ManagedProviderConnection, string, error)
	RevokeConnection(context.Context, RevokeConnectionCommand) (entity.ManagedProviderConnection, bool, error)
	ManagePool(context.Context, ManagePoolCommand) (entity.ManagedProviderPool, bool, error)
	GetPool(context.Context, domainrepo.Scope, string) (entity.ManagedProviderPool, error)
	ListPools(context.Context, domainrepo.Scope, int, string) ([]entity.ManagedProviderPool, string, error)
	ConfigureIntegration(context.Context, ConfigureIntegrationCommand) (entity.IntegrationConfiguration, bool, error)
	GetIntegrationConfiguration(context.Context, domainrepo.Scope, string) (entity.IntegrationConfiguration, error)
	GetIntegrationConfigurationVersion(context.Context, domainrepo.Scope, string, uint64) (entity.IntegrationConfiguration, error)
	ListIntegrationConfigurations(context.Context, domainrepo.Scope, int, string) ([]entity.IntegrationConfiguration, string, error)
	CreateTest(context.Context, CreateTestCommand) (entity.IntegrationTestReceipt, bool, error)
	GetTest(context.Context, domainrepo.Scope, string) (entity.IntegrationTestReceipt, error)
	ManageGitBinding(context.Context, ManageGitBindingCommand) (entity.GitSourceBinding, bool, error)
	GetGitBinding(context.Context, domainrepo.Scope, string) (entity.GitSourceBinding, error)
	ListGitBindings(context.Context, domainrepo.Scope, int, string) ([]entity.GitSourceBinding, string, error)
	CreateGitReconciliation(context.Context, ReconcileGitCommand) (entity.GitReconciliation, bool, error)
	GetGitReconciliation(context.Context, domainrepo.Scope, string) (entity.GitReconciliation, error)
	ListApprovals(context.Context, domainrepo.Scope, []string, int, string) ([]entity.Approval, string, error)
	GetApproval(context.Context, domainrepo.Scope, string) (entity.Approval, error)
	NextManagementScope(context.Context) (domainrepo.Scope, bool, error)
	ClaimManagementEffect(context.Context, domainrepo.Scope, time.Time, time.Duration) (entity.ManagementEffect, bool, error)
	BeginManagementEffectDispatch(context.Context, domainrepo.Scope, string, string, uint64) error
	AdvanceProviderRevoke(context.Context, domainrepo.Scope, string, string, uint64, string, time.Time) (entity.ManagementEffect, error)
	RenewManagementEffect(context.Context, domainrepo.Scope, string, string, uint64, time.Duration) error
	ManagementEffectSucceeded(context.Context, domainrepo.Scope, string) (bool, error)
	CompleteManagementEffect(context.Context, EffectCompletion) error
	FailManagementEffect(context.Context, EffectFailure) error
	CompleteProviderSync(context.Context, ProviderSyncCompletion) (entity.ManagedProviderConnection, error)
	CompletePoolSync(context.Context, PoolSyncCompletion) (entity.ManagedProviderPool, error)
	CompleteTest(context.Context, TestCompletion) (entity.IntegrationTestReceipt, error)
	CompleteGitFetch(context.Context, GitFetchCompletion) (entity.GitReconciliation, error)
	CompleteGitApply(context.Context, GitApplyCompletion) (entity.GitReconciliation, error)
	MarkAuthorizationCode(context.Context, domainrepo.Scope, string, string, uint64, []byte, time.Time, time.Time) error
	AuthorizationCancelled(context.Context, domainrepo.Scope, string, string, uint64) (bool, error)
	CompleteAuthorization(context.Context, domainrepo.Scope, string, string, string, uint64, entity.CredentialGeneration, string, string, time.Time) error
	FailAuthorization(context.Context, domainrepo.Scope, string, string, uint64, string, time.Time) error
	GetEffectResource(context.Context, domainrepo.Scope, entity.ManagementEffect) (json.RawMessage, error)
	CheckManagement(context.Context) error
}
