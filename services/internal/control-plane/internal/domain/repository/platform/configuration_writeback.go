package platform

import (
	"context"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

type ConfigurationWriteBackEffectInput struct {
	Lease                                                          entity.ConfigurationWriteBackLease
	Effect, CandidateCommitSHA, CandidateTreeSHA, CandidateBlobSHA string
	ParentCommitSHA, ContentSHA256, PullRequestRef, PullRequestURL string
	BaseBlobSHA                                                    string
}

type ConfigurationWriteBackRepository interface {
	GetConfigurationWriteBack(context.Context, value.Principal, string) (entity.ConfigurationWriteBackView, error)
	ListConfigurationWriteBacks(context.Context, value.Principal, string, query.Filter) ([]entity.ConfigurationWriteBack, string, int64, error)
	ClaimConfigurationWriteBackWork(context.Context, value.Principal, string, int32) ([]entity.ConfigurationWriteBackWork, error)
	RenewConfigurationWriteBackWork(context.Context, value.Principal, entity.ConfigurationWriteBackLease) (entity.ConfigurationWriteBackLease, error)
	BeginConfigurationWriteBackEffect(context.Context, value.Principal, ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, bool, error)
	CompleteConfigurationWriteBackEffect(context.Context, value.Principal, ConfigurationWriteBackEffectInput) (entity.ConfigurationWriteBack, error)
	FailConfigurationWriteBackWork(context.Context, value.Principal, entity.ConfigurationWriteBackLease, string) (entity.ConfigurationWriteBack, error)
}
