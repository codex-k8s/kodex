package app

import (
	"context"
	"errors"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/external/integration-gateway/internal/integration"
	businessmetrics "github.com/codex-k8s/kodex/services/external/integration-gateway/internal/observability/metrics"
	"google.golang.org/protobuf/proto"
)

// errBundleWork is a safe closed diagnostic for malformed owner work or receipts; neither source content nor provider errors are logged here.
// errBundleWork — безопасная closed диагностика malformed owner work/receipts; source content и provider errors здесь не логируются.
var errBundleWork = errors.New("repository bundle owner response is invalid")

// bundleExecution separates local preparation, the sole CAS push and independent remote proof; the owner Begin ACK must precede PushCandidate.
// bundleExecution разделяет local preparation, sole CAS push и independent remote proof; owner Begin ACK предшествует PushCandidate.
type bundleExecution interface {
	// Close clears private execution buffers and Git scratch without touching original artifacts or remote refs.
	// Close очищает private execution buffers и Git scratch без original artifacts либо remote refs.
	Close()
	// PrepareCandidate builds the complete approved file set into one single-parent candidate without a provider write.
	// PrepareCandidate собирает complete approved file set в один single-parent candidate без provider write.
	PrepareCandidate(context.Context) (integration.RepositoryBundleCandidate, error)
	// PushCandidate attempts the exact remembered candidate once after a durable Begin ACK; an error never authorizes resend.
	// PushCandidate однократно отправляет exact remembered candidate после durable Begin ACK; error не разрешает resend.
	PushCandidate(context.Context, integration.RepositoryBundleCandidate) error
	// VerifyCandidate reads the exact remote ref and full file bytes using an independent object database.
	// VerifyCandidate читает exact remote ref и полные file bytes через independent object database.
	VerifyCandidate(context.Context, integration.RepositoryBundleCandidate) error
}

// bundleOpen binds private source bytes to one owner-issued work item; it has no authority to mint grants or approve the manifest.
// bundleOpen связывает private source bytes с одной owner-issued work item; grants и approval manifest ему не принадлежат.
type bundleOpen func(context.Context, *cp.RepositoryBundlePublicationWork, []byte) (bundleExecution, error)

// processRepositoryBundleWork wires the existing provider adapter and records every attempted push independently of later readback or owner commit outcomes.
// processRepositoryBundleWork подключает прежний provider adapter и учитывает каждую push attempt независимо от readback либо owner commit outcome.
func processRepositoryBundleWork(ctx context.Context, control cp.RepositoryBundlePublicationServiceClient, adapter *integration.Adapter, metrics *businessmetrics.Metrics, config Config) (int, error) {
	return processBundle(ctx, control, func(ctx context.Context, work *cp.RepositoryBundlePublicationWork, archive []byte) (bundleExecution, error) {
		return adapter.OpenRepositoryBundle(ctx, work, archive)
	}, func(success bool) { metrics.RepositoryBundle(success) }, config)
}

// processBundle reserves owner-finalization time, validates exact private content and requires a new matching Begin ACK before the only push. Lost owner/provider ACKs cause read-only recovery, never automatic resend.
// processBundle резервирует время owner-finalization, проверяет exact private content и требует новый matching Begin ACK до единственного push. Lost owner/provider ACK ведёт к read-only recovery без resend.
func processBundle(ctx context.Context, control cp.RepositoryBundlePublicationServiceClient, open bundleOpen, observe func(bool), config Config) (int, error) {
	if control == nil || config.RequestTimeout <= 0 {
		return 0, errBundleWork
	}
	request, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	claimed, err := control.ClaimRepositoryBundlePublications(request, &cp.ClaimRepositoryBundlePublicationsRequest{Claimant: config.InstanceID, Limit: 1})
	cancel()
	if err != nil {
		return 0, err
	}
	if !integration.RepositoryBundleWireKnown(claimed) || len(claimed.GetWork()) > 1 {
		return 0, errBundleWork
	}
	if len(claimed.GetWork()) == 0 {
		return 0, nil
	}
	work := claimed.GetWork()[0]
	if work == nil || work.GetLease() == nil || work.GetProposal() == nil || work.GetLease().GetClaimant() != config.InstanceID || work.GetLease().GetExpiresAt() == nil || work.GetLease().GetExpiresAt().CheckValid() != nil || work.GetDeadline() == nil || work.GetDeadline().CheckValid() != nil {
		return 0, errBundleWork
	}
	deadline := time.Now().Add(min(config.OperationTimeout, 120*time.Second))
	for _, end := range []time.Time{work.GetDeadline().AsTime(), work.GetLease().GetExpiresAt().AsTime()} {
		if limit := end.Add(-3 * config.RequestTimeout); limit.Before(deadline) {
			deadline = limit
		}
	}
	if !deadline.After(time.Now()) {
		return 0, errBundleWork
	}
	operation, cancelOperation := context.WithDeadline(ctx, deadline)
	defer cancelOperation()
	mode := work.GetMode()
	if mode != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE && mode != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY {
		return 0, errBundleWork
	}
	var archive []byte
	if mode == cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE {
		request, cancel := context.WithTimeout(operation, config.RequestTimeout)
		content, err := control.ReadRepositoryBundlePublication(request, &cp.RepositoryBundleLeaseRequest{Lease: proto.Clone(work.GetLease()).(*cp.RepositoryBundleLease)})
		cancel()
		if err != nil {
			return failBundle(ctx, control, work, config, "UNAVAILABLE")
		}
		if !integration.RepositoryBundleWireKnown(content) || !proto.Equal(content.GetMetadata(), work.GetProposal().GetArchive()) || len(content.GetArchive()) > repositorybundle.MaximumArchiveBytes || int64(len(content.GetArchive())) != work.GetProposal().GetArchive().GetSizeBytes() || repositorybundle.Digest(content.GetArchive()) != work.GetProposal().GetArchive().GetSha256() {
			return failBundle(ctx, control, work, config, "CONTENT_INVALID")
		}
		archive = content.Archive
		defer clear(archive)
	}
	execution, err := open(operation, work, archive)
	if err != nil {
		return failBundle(ctx, control, work, config, "UNAVAILABLE")
	}
	defer execution.Close()
	candidate := integration.RepositoryBundleCandidate{CommitSHA: work.GetProposal().GetCandidateCommitSha(), TreeSHA: work.GetProposal().GetCandidateTreeSha()}
	if mode == cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE {
		candidate, err = execution.PrepareCandidate(operation)
		if err != nil {
			return failBundle(ctx, control, work, config, "BASE_INVALID")
		}
		request, cancel = context.WithTimeout(ctx, config.RequestTimeout)
		begun, err := control.BeginRepositoryBundlePublication(request, bundleEffectRequest(work, candidate))
		cancel()
		if err != nil {
			return 0, err
		}
		if !integration.RepositoryBundleWireKnown(begun) || !validBundleReceipt(begun.GetProposal(), work.GetProposal()) || begun.GetProposal().GetCandidateCommitSha() != candidate.CommitSHA || begun.GetProposal().GetCandidateTreeSha() != candidate.TreeSHA || begun.GetProposal().GetState() != cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_EFFECT_STARTED && begun.GetProposal().GetState() != cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_UNKNOWN_OUTCOME {
			return 0, errBundleWork
		}
		work.Proposal = proto.Clone(begun.GetProposal()).(*cp.RepositoryBundlePublication)
		if !begun.GetAlreadyStarted() {
			if begun.GetProposal().GetState() != cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_EFFECT_STARTED {
				return 0, errBundleWork
			}
			pushErr := execution.PushCandidate(operation, candidate)
			if observe != nil {
				observe(pushErr == nil)
			}
		}
	}
	if err := execution.VerifyCandidate(operation, candidate); err != nil {
		return failBundle(ctx, control, work, config, "OUTCOME_UNCONFIRMED")
	}
	request, cancel = context.WithTimeout(ctx, config.RequestTimeout)
	completed, err := control.CompleteRepositoryBundlePublication(request, bundleEffectRequest(work, candidate))
	cancel()
	if err != nil {
		return 0, err
	}
	if !integration.RepositoryBundleWireKnown(completed) || !validBundleReceipt(completed.GetProposal(), work.GetProposal()) || completed.GetProposal().GetState() != cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_SUCCEEDED || completed.GetProposal().GetCandidateCommitSha() != candidate.CommitSHA || completed.GetProposal().GetCandidateTreeSha() != candidate.TreeSHA || completed.GetProposal().GetConfirmedAt() == nil || completed.GetProposal().GetConfirmedAt().CheckValid() != nil {
		return 0, errBundleWork
	}
	return 1, nil
}

// bundleEffectRequest binds every Begin/Complete report to the same full work epoch and immutable source/base pins, without arbitrary remote URLs or result text.
// bundleEffectRequest связывает Begin/Complete с полным work epoch и immutable source/base pins, без arbitrary remote URL либо result text.
func bundleEffectRequest(work *cp.RepositoryBundlePublicationWork, candidate integration.RepositoryBundleCandidate) *cp.RepositoryBundleEffectRequest {
	p := work.GetProposal()
	return &cp.RepositoryBundleEffectRequest{Lease: proto.Clone(work.GetLease()).(*cp.RepositoryBundleLease), CandidateCommitSha: candidate.CommitSHA, CandidateTreeSha: candidate.TreeSHA, BaseCommitSha: p.GetBaseCommitSha(), ManifestSha256: p.GetManifest().GetSha256(), ArchiveSha256: p.GetArchive().GetSha256()}
}

// validBundleReceipt requires every immutable proposal field to remain byte-for-byte identical and a strictly newer owner version; callers separately require the exact allowed transition and evidence.
// validBundleReceipt требует точного совпадения всех immutable proposal fields и новой owner version; caller отдельно требует допустимый transition и evidence.
func validBundleReceipt(next, previous *cp.RepositoryBundlePublication) bool {
	if !integration.RepositoryBundleWireKnown(next) || !integration.RepositoryBundleWireKnown(previous) || next.GetVersion() <= previous.GetVersion() {
		return false
	}
	if !repositorybundle.ValidContinuation(next.GetContinuationState(), next.GetContinuationCode(), next.GetContinuationNodeRef()) || !repositorybundle.ValidContinuation(previous.GetContinuationState(), previous.GetContinuationCode(), previous.GetContinuationNodeRef()) {
		return false
	}
	if next.GetContinuationState() != "" {
		switch next.GetState() {
		case cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_SUCCEEDED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_FAILED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_REJECTED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_CANCELLED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_EXPIRED:
		default:
			return false
		}
	}
	a, b := proto.Clone(next).(*cp.RepositoryBundlePublication), proto.Clone(previous).(*cp.RepositoryBundlePublication)
	a.Version, b.Version, a.State, b.State = 0, 0, 0, 0
	a.CandidateCommitSha, b.CandidateCommitSha, a.CandidateTreeSha, b.CandidateTreeSha = "", "", "", ""
	a.FailureCode, b.FailureCode, a.ConfirmedAt, b.ConfirmedAt = "", "", nil, nil
	a.ContinuationState, b.ContinuationState, a.ContinuationCode, b.ContinuationCode, a.ContinuationNodeRef, b.ContinuationNodeRef = "", "", "", "", "", ""
	return proto.Equal(a, b)
}

// failBundle reports one closed class with the exact lease and validates the owner terminal/UNKNOWN receipt. It never repeats a failed RPC or clears an uncertain remote effect locally.
// failBundle сообщает один closed class с exact lease и проверяет owner terminal/UNKNOWN receipt. Failed RPC не повторяется, uncertain remote effect локально не очищается.
func failBundle(ctx context.Context, control cp.RepositoryBundlePublicationServiceClient, work *cp.RepositoryBundlePublicationWork, config Config, code string) (int, error) {
	request, cancel := context.WithTimeout(ctx, config.RequestTimeout)
	defer cancel()
	failed, err := control.FailRepositoryBundlePublication(request, &cp.FailRepositoryBundlePublicationRequest{Lease: proto.Clone(work.GetLease()).(*cp.RepositoryBundleLease), FailureCode: code})
	if err != nil {
		return 0, err
	}
	if !integration.RepositoryBundleWireKnown(failed) || !validBundleReceipt(failed.GetProposal(), work.GetProposal()) {
		return 0, errBundleWork
	}
	p, old := failed.GetProposal(), work.GetProposal()
	if p.GetCandidateCommitSha() != old.GetCandidateCommitSha() || p.GetCandidateTreeSha() != old.GetCandidateTreeSha() || p.GetConfirmedAt() != nil || p.GetFailureCode() == "" {
		return 0, errBundleWork
	}
	if old.GetCandidateCommitSha() != "" && p.GetState() != cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_UNKNOWN_OUTCOME || old.GetCandidateCommitSha() == "" && p.GetState() == cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_UNKNOWN_OUTCOME {
		return 0, errBundleWork
	}
	switch failed.GetProposal().GetState() {
	case cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_FAILED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_EXPIRED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_CANCELLED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_UNKNOWN_OUTCOME:
		return 1, nil
	default:
		return 0, errBundleWork
	}
}
