package app

import (
	"context"
	"errors"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	businessmetrics "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/observability/metrics"
	"google.golang.org/protobuf/proto"
)

const maximumConfigurationWriteBackOperation = 30 * time.Second

var errConfigurationWriteBack = errors.New("configuration writeback owner response is invalid")

type writeBackExecution interface {
	Close()
	PrepareCandidate(context.Context) (integration.ConfigurationWriteBackCandidate, error)
	PushCandidate(context.Context, integration.ConfigurationWriteBackCandidate) error
	VerifyBranch(context.Context, integration.ConfigurationWriteBackCandidate) error
	FindPullRequest(context.Context, string) (integration.ConfigurationWriteBackPullRequest, bool, error)
	CreatePullRequest(context.Context, string) (integration.ConfigurationWriteBackPullRequest, error)
}
type writeBackOpen func(context.Context, *cp.ManagedConfigurationGitWriteBackWork) (writeBackExecution, error)

func processConfigurationWriteBackWork(ctx context.Context, control cp.ManagedConfigurationGitWriteBackWorkServiceClient, adapter *integration.Adapter, metrics *businessmetrics.Metrics, config Config) (int, error) {
	return processWriteBack(ctx, control, func(ctx context.Context, work *cp.ManagedConfigurationGitWriteBackWork) (writeBackExecution, error) {
		execution, err := adapter.OpenConfigurationWriteBack(ctx, work)
		if err != nil {
			return nil, err
		}
		return &observedWriteBack{writeBackExecution: execution, metrics: metrics}, nil
	}, config)
}

type observedWriteBack struct {
	writeBackExecution
	metrics *businessmetrics.Metrics
}

func (execution *observedWriteBack) PushCandidate(ctx context.Context, candidate integration.ConfigurationWriteBackCandidate) error {
	err := execution.writeBackExecution.PushCandidate(ctx, candidate)
	execution.metrics.ConfigurationWriteBack(true, err == nil)
	return err
}
func (execution *observedWriteBack) CreatePullRequest(ctx context.Context, commit string) (integration.ConfigurationWriteBackPullRequest, error) {
	result, err := execution.writeBackExecution.CreatePullRequest(ctx, commit)
	execution.metrics.ConfigurationWriteBack(false, err == nil)
	return result, err
}

func processWriteBack(ctx context.Context, control cp.ManagedConfigurationGitWriteBackWorkServiceClient, open writeBackOpen, config Config) (int, error) {
	if control == nil {
		return 0, errConfigurationWriteBack
	}
	claimContext, cancelClaim := context.WithTimeout(ctx, config.RequestTimeout)
	response, err := control.ClaimManagedConfigurationGitWriteBackWork(claimContext, &cp.ClaimManagedConfigurationGitWriteBackWorkRequest{Claimant: config.InstanceID, Limit: 1})
	cancelClaim()
	if err != nil {
		return 0, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || len(response.GetWork()) > 1 {
		return 0, errConfigurationWriteBack
	}
	if len(response.GetWork()) == 0 {
		return 0, nil
	}
	work := response.GetWork()[0]
	if work == nil || work.GetLease() == nil || work.GetProposal() == nil || work.GetLease().GetClaimant() != config.InstanceID || work.GetLease().GetExpiresAt() == nil || work.GetLease().GetExpiresAt().CheckValid() != nil || work.GetDeadline() == nil || work.GetDeadline().CheckValid() != nil {
		return 0, errConfigurationWriteBack
	}
	deadline := time.Now().Add(min(config.OperationTimeout, maximumConfigurationWriteBackOperation))
	for _, end := range []time.Time{work.GetLease().GetExpiresAt().AsTime(), work.GetDeadline().AsTime()} {
		if end.Add(-2 * config.RequestTimeout).Before(deadline) {
			deadline = end.Add(-2 * config.RequestTimeout)
		}
	}
	if !deadline.After(time.Now()) {
		return 0, errConfigurationWriteBack
	}
	operation, cancelOperation := context.WithDeadline(ctx, deadline)
	defer cancelOperation()
	execution, err := open(operation, work)
	if err != nil {
		return failWriteBack(ctx, control, work, config, err)
	}
	defer execution.Close()
	candidate := integration.ConfigurationWriteBackCandidate{CommitSHA: work.GetProposal().GetCandidateCommitSha(), TreeSHA: work.GetCandidateTreeSha(), BlobSHA: work.GetCandidateBlobSha()}
	readOnly := work.GetMode() == cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY
	if work.GetEffect() == cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_BRANCH {
		if !readOnly {
			candidate, err = execution.PrepareCandidate(operation)
			if err != nil {
				return failWriteBack(ctx, control, work, config, err)
			}
			readOnly, err = beginWriteBack(ctx, control, work, config, candidate)
			if err != nil {
				return 0, err
			}
			if !readOnly {
				err = execution.PushCandidate(operation, candidate)
			}
		}
		// Ошибка push не является разрешением на повтор: допускается только readback.
		verifyErr := execution.VerifyBranch(operation, candidate)
		if verifyErr != nil {
			if err != nil {
				verifyErr = err
			}
			return failWriteBack(ctx, control, work, config, verifyErr)
		}
		return completeWriteBack(ctx, control, work, config, candidate, integration.ConfigurationWriteBackPullRequest{})
	}
	if work.GetEffect() != cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_PULL_REQUEST {
		return 0, errConfigurationWriteBack
	}
	if err = execution.VerifyBranch(operation, candidate); err != nil {
		return failWriteBack(ctx, control, work, config, err)
	}
	found, exists, err := execution.FindPullRequest(operation, candidate.CommitSHA)
	if err != nil {
		return failWriteBack(ctx, control, work, config, err)
	}
	if !readOnly {
		readOnly, err = beginWriteBack(ctx, control, work, config, candidate)
		if err != nil {
			return 0, err
		}
		if !readOnly && !exists {
			_, createErr := execution.CreatePullRequest(operation, candidate.CommitSHA)
			found, exists, err = execution.FindPullRequest(operation, candidate.CommitSHA)
			if err != nil || !exists {
				if createErr != nil {
					err = createErr
				}
				if err == nil {
					err = errConfigurationWriteBack
				}
				return failWriteBack(ctx, control, work, config, err)
			}
		}
	}
	if !exists {
		return failWriteBack(ctx, control, work, config, errConfigurationWriteBack)
	}
	return completeWriteBack(ctx, control, work, config, candidate, found)
}

func beginWriteBack(ctx context.Context, control cp.ManagedConfigurationGitWriteBackWorkServiceClient, work *cp.ManagedConfigurationGitWriteBackWork, config Config, candidate integration.ConfigurationWriteBackCandidate) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	response, err := control.BeginManagedConfigurationGitWriteBackEffect(ctx, &cp.BeginManagedConfigurationGitWriteBackEffectRequest{Lease: proto.Clone(work.GetLease()).(*cp.ManagedConfigurationGitWriteBackLease), Effect: work.GetEffect(), CandidateCommitSha: candidate.CommitSHA, CandidateTreeSha: candidate.TreeSHA, CandidateBlobSha: candidate.BlobSHA, BaseBlobSha: candidate.BaseBlobSHA, ParentCommitSha: work.GetProposal().GetBaseCommitSha(), ContentSha256: work.GetProposal().GetProposedContentSha256()})
	if err != nil {
		return false, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validWriteBackReceipt(response.GetProposal(), work) || response.GetProposal().GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_EFFECT_STARTED || response.GetProposal().GetCandidateCommitSha() != candidate.CommitSHA {
		return false, errConfigurationWriteBack
	}
	return response.GetAlreadyStarted(), nil
}

func failWriteBack(ctx context.Context, control cp.ManagedConfigurationGitWriteBackWorkServiceClient, work *cp.ManagedConfigurationGitWriteBackWork, config Config, cause error) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	ctx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	response, err := control.FailManagedConfigurationGitWriteBackWork(ctx, &cp.FailManagedConfigurationGitWriteBackWorkRequest{Lease: proto.Clone(work.GetLease()).(*cp.ManagedConfigurationGitWriteBackLease), FailureCode: integration.ConfigurationWriteBackFailure(cause)})
	if err != nil {
		return 0, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validWriteBackReceipt(response.GetProposal(), work) {
		return 0, errConfigurationWriteBack
	}
	switch response.GetProposal().GetState() {
	case cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_QUEUED, cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_FAILED, cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_UNKNOWN_OUTCOME:
		return 1, nil
	default:
		return 0, errConfigurationWriteBack
	}
}

func completeWriteBack(ctx context.Context, control cp.ManagedConfigurationGitWriteBackWorkServiceClient, work *cp.ManagedConfigurationGitWriteBackWork, config Config, candidate integration.ConfigurationWriteBackCandidate, pullRequest integration.ConfigurationWriteBackPullRequest) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	response, err := control.CompleteManagedConfigurationGitWriteBackEffect(ctx, &cp.CompleteManagedConfigurationGitWriteBackEffectRequest{Lease: proto.Clone(work.GetLease()).(*cp.ManagedConfigurationGitWriteBackLease), Effect: work.GetEffect(), CandidateCommitSha: candidate.CommitSHA, ContentSha256: work.GetProposal().GetProposedContentSha256(), PullRequestRef: pullRequest.Ref, PullRequestUrl: pullRequest.URL})
	if err != nil {
		return 0, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validWriteBackReceipt(response.GetProposal(), work) || response.GetProposal().GetCandidateCommitSha() != candidate.CommitSHA {
		return 0, errConfigurationWriteBack
	}
	p := response.GetProposal()
	if p.GetBranchConfirmedAt() == nil || p.GetBranchConfirmedAt().CheckValid() != nil {
		return 0, errConfigurationWriteBack
	}
	if work.GetEffect() == cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_BRANCH {
		if p.GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_QUEUED && p.GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_FAILED {
			return 0, errConfigurationWriteBack
		}
	} else if p.GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_SUCCEEDED || p.GetPullRequestRef() != pullRequest.Ref || p.GetPullRequestUrl() != pullRequest.URL || p.GetPullRequestConfirmedAt() == nil || p.GetPullRequestConfirmedAt().CheckValid() != nil {
		return 0, errConfigurationWriteBack
	}
	return 1, nil
}

func validWriteBackReceipt(p *cp.ManagedConfigurationGitWriteBack, work *cp.ManagedConfigurationGitWriteBackWork) bool {
	previous := work.GetProposal()
	if p == nil || p.GetKind() != previous.GetKind() || p.GetContentFormat() != previous.GetContentFormat() || p.GetBaseContentSha256() != previous.GetBaseContentSha256() || !proto.Equal(p.GetCreatedAt(), previous.GetCreatedAt()) || !proto.Equal(p.GetExpiresAt(), previous.GetExpiresAt()) || !proto.Equal(p.GetApprovedAt(), previous.GetApprovedAt()) {
		return false
	}
	if _, known := cp.ManagedConfigurationGitWriteBackFailure_name[int32(p.GetFailureCode())]; !known {
		return false
	}
	return p != nil && len(p.ProtoReflect().GetUnknown()) == 0 && p.GetRef() == previous.GetRef() && p.GetVersion() > previous.GetVersion() && p.GetConfigurationRef() == previous.GetConfigurationRef() && p.GetConfigurationVersion() == previous.GetConfigurationVersion() && p.GetSourceRef() == previous.GetSourceRef() && p.GetSourceVersion() == previous.GetSourceVersion() && p.GetConnectionRef() == previous.GetConnectionRef() && p.GetConnectionVersion() == previous.GetConnectionVersion() && p.GetApprovalDigest() == previous.GetApprovalDigest() && p.GetBaseCommitSha() == previous.GetBaseCommitSha() && p.GetProposedContentSha256() == previous.GetProposedContentSha256() && p.GetProposalBranch() == previous.GetProposalBranch() && p.GetRepositoryRef() == previous.GetRepositoryRef() && p.GetSourceRefName() == previous.GetSourceRefName() && p.GetPath() == previous.GetPath()
}
