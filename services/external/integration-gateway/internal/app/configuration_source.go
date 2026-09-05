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

const maximumConfigurationSourceOperation = 20 * time.Second

type configurationSourceAdapter interface {
	ReadConfigurationSource(context.Context, *cp.ManagedConfigurationSourceWork) (integration.ConfigurationSourceResult, error)
}

func processConfigurationSourceWork(ctx context.Context, control cp.ManagedConfigurationSourceWorkServiceClient, adapter configurationSourceAdapter, metrics *businessmetrics.Metrics, config Config) (int, error) {
	if control == nil {
		return 0, errors.New("configuration source owner is unavailable")
	}
	claimContext, cancelClaim := context.WithTimeout(ctx, config.RequestTimeout)
	claimed, err := control.ClaimManagedConfigurationSourceWork(claimContext, &cp.ClaimManagedConfigurationSourceWorkRequest{Claimant: config.InstanceID, Limit: 1})
	cancelClaim()
	if err != nil {
		return 0, err
	}
	if claimed == nil || len(claimed.ProtoReflect().GetUnknown()) != 0 || len(claimed.GetWork()) > 1 {
		return 0, errors.New("configuration source claim response is invalid")
	}
	if len(claimed.GetWork()) == 0 {
		return 0, nil
	}
	work := claimed.GetWork()[0]
	lease := work.GetLease()
	if work == nil || lease == nil || lease.GetClaimant() != config.InstanceID || lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil || work.GetDeadline() == nil || work.GetDeadline().CheckValid() != nil {
		return 0, errors.New("configuration source lease is invalid")
	}
	budget := min(config.OperationTimeout, maximumConfigurationSourceOperation)
	deadline := time.Now().Add(budget)
	finalizeBefore := lease.GetExpiresAt().AsTime().Add(-config.RequestTimeout)
	if work.GetDeadline().AsTime().Add(-config.RequestTimeout).Before(finalizeBefore) {
		finalizeBefore = work.GetDeadline().AsTime().Add(-config.RequestTimeout)
	}
	if finalizeBefore.Before(deadline) {
		deadline = finalizeBefore
	}
	if !deadline.After(time.Now()) {
		return 0, errors.New("configuration source lease budget is insufficient")
	}
	operation, cancelOperation := context.WithDeadline(ctx, deadline)
	result, operationErr := adapter.ReadConfigurationSource(operation, work)
	if operation.Err() != nil {
		operationErr = operation.Err()
	}
	cancelOperation()
	defer clear(result.Content)
	metrics.ConfigurationSource(operationErr == nil)
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	finalize, cancelFinalize := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancelFinalize()
	if operationErr != nil {
		response, err := control.FailManagedConfigurationSourceWork(finalize, &cp.FailManagedConfigurationSourceWorkRequest{Lease: proto.Clone(lease).(*cp.ManagedConfigurationSourceLease), FailureCode: integration.ConfigurationSourceFailure(operationErr)})
		if err != nil {
			return 0, err
		}
		if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validConfigurationSourceReceipt(response.GetSource(), work) ||
			(response.GetSource().GetState() != cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_QUEUED && response.GetSource().GetState() != cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_SYNC_BLOCKED) {
			return 0, errors.New("configuration source failure receipt is missing")
		}
		return 1, nil
	}
	response, err := control.CompleteManagedConfigurationSourceWork(finalize, &cp.CompleteManagedConfigurationSourceWorkRequest{Lease: proto.Clone(lease).(*cp.ManagedConfigurationSourceLease), CommitSha: result.CommitSHA, ContentSha256: result.ContentSHA256, Content: result.Content, Ancestry: result.Ancestry})
	if err != nil {
		return 0, err
	}
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validConfigurationSourceReceipt(response.GetSource(), work) ||
		(response.GetSource().GetState() != cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_READY && response.GetSource().GetState() != cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_SYNC_BLOCKED) {
		return 0, errors.New("configuration source completion receipt is missing")
	}
	if response.GetSource().GetState() == cp.ManagedConfigurationSourceState_MANAGED_CONFIGURATION_SOURCE_STATE_READY && (response.GetSource().GetAcceptedCommitSha() != result.CommitSHA || response.GetSource().GetAcceptedContentSha256() != result.ContentSHA256) {
		return 0, errors.New("configuration source accepted content does not match")
	}
	return 1, nil
}

func validConfigurationSourceReceipt(source *cp.ManagedConfigurationGitSource, work *cp.ManagedConfigurationSourceWork) bool {
	return source != nil && len(source.ProtoReflect().GetUnknown()) == 0 && source.GetRef() == work.GetSourceRef() && source.GetVersion() > 0 && source.GetGeneration() == work.GetLease().GetSourceGeneration() && source.GetConnectionRef() == work.GetConnectionRef() && source.GetProviderKey() == work.GetDefinitionKey() && source.GetRepositoryRef() == work.GetRepositoryRef() && source.GetRefName() == work.GetRefName() && source.GetPath() == work.GetPath()
}
