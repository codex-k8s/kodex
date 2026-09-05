package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type ConfigurationSourceCompletion struct {
	Lease                              entity.ManagedConfigurationSourceLease
	CommitSHA, ContentSHA256, Ancestry string
	Content                            []byte
}

type ConfigurationSourceWorkRepository interface {
	ClaimConfigurationSourceWork(context.Context, value.Principal, string, int32) ([]entity.ManagedConfigurationSourceWork, error)
	RenewConfigurationSourceWork(context.Context, value.Principal, entity.ManagedConfigurationSourceLease) (entity.ManagedConfigurationSourceLease, error)
	CompleteConfigurationSourceWork(context.Context, value.Principal, ConfigurationSourceCompletion) (entity.ManagedConfigurationGitSource, error)
	FailConfigurationSourceWork(context.Context, value.Principal, entity.ManagedConfigurationSourceLease, string) (entity.ManagedConfigurationGitSource, error)
}
