package callback

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// repositoryBundleRejoinInvalid is a closed diagnostic with no source bytes, lease secrets or provider error chain.
// repositoryBundleRejoinInvalid — закрытая диагностика без source bytes, lease secrets либо provider error chain.
const repositoryBundleRejoinInvalid = "repository bundle rejoin receipt is invalid"

// repositoryBundleRejoinReceiptRef bounds an owner receipt locator, never permission to read a foreign receipt.
// repositoryBundleRejoinReceiptRef ограничивает owner receipt locator, не предоставляя permission на чужой receipt.
var repositoryBundleRejoinReceiptRef = regexp.MustCompile(`^erc_[A-Za-z0-9_-]{8,92}$`)

// repositoryBundleRejoinResultKey validates only the public spelling of an owner-derived published result slot.
// repositoryBundleRejoinResultKey проверяет лишь public spelling назначенного owner published result slot.
var repositoryBundleRejoinResultKey = regexp.MustCompile(`^[A-Za-z0-9_-]{1,96}$`)

// repositoryBundleRejoin checks the new owner envelope against trusted immutable RunnerInput,
// then independently reads the existing native receipt. It never changes the old proposal,
// waits for another callback, creates a gate or calls any provider mutation.
// repositoryBundleRejoin сверяет новый owner envelope с trusted immutable RunnerInput,
// затем читает existing native receipt. Исходный proposal не меняется; новый callback,
// Gate либо provider mutation не запрашивается.
func (server *Server) repositoryBundleRejoin(ctx context.Context, input runtimecontract.RunnerInput, grant runtimecontract.RunnerIntegrationGrant,
	request *cp.PrepareRepositoryBundlePublicationRequest, response *cp.PrepareRepositoryBundlePublicationResponse,
) (any, error) {
	proposal, proof := response.GetProposal(), response.GetRejoin()
	if proposal == nil || proof == nil || len(response.ProtoReflect().GetUnknown()) != 0 || len(proposal.ProtoReflect().GetUnknown()) != 0 || len(proof.ProtoReflect().GetUnknown()) != 0 ||
		proof.GetSchema() != repositorybundle.RejoinSchema || proof.GetMode() != repositorybundle.RejoinMode ||
		input.RunRef == "" || input.NodeRef == "" || input.RuntimeRevisionRef == "" || input.SessionRef == "" || input.TurnRef == "" || input.Attempt < 1 ||
		proof.GetCurrentRunRef() != input.RunRef || proof.GetCurrentNodeRef() != input.NodeRef || proof.GetCurrentRuntimeRevisionRef() != input.RuntimeRevisionRef || proof.GetCurrentLeaseRef() != input.LeaseRef || proof.GetCurrentGeneration() != input.LeaseGeneration ||
		proof.GetCurrentRuntimeAttempt() != input.Attempt || proof.GetCurrentSessionRef() != input.SessionRef || proof.GetCurrentTurnRef() != input.TurnRef || proof.GetBusinessAttempt() < 1 || proof.GetBusinessAttempt() > 4 ||
		grant.ConnectionVersion < 1 || proof.GetConnectionVersion() != grant.ConnectionVersion || proposal.GetConnectionVersion() != grant.ConnectionVersion ||
		proposal.GetProjectRef() != input.ProjectRef || proposal.GetGrantRef() != grant.Ref || proposal.GetGrantVersion() != grant.GrantVersion || proposal.GetConnectionRef() != grant.ConnectionRef ||
		!protoBundlePinsEqual(proof.GetCurrentArchive(), request.GetArchive()) || !protoBundlePinsEqual(proof.GetCurrentManifest(), request.GetManifest()) ||
		!repositorybundle.ValidDigest(proof.GetSemanticDigest()) || !repositorybundle.ValidDigest(proof.GetReceiptDigest()) || !repositoryBundleRejoinReceiptRef.MatchString(proof.GetReceiptRef()) || !repositoryBundleRejoinResultKey.MatchString(proof.GetResultKey()) ||
		proposal.GetState() != cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_SUCCEEDED || proposal.GetVersion() < 1 || proposal.GetRef() == "" || proposal.GetGateRef() == "" ||
		!safeInvocationRef(proposal.GetInvocationRef()) || proposal.GetOriginRunRef() == "" || proposal.GetOriginRuntimeRevisionRef() == "" ||
		proposal.GetProposalBranch() != "kodex/bundle/"+proposal.GetRef() || proposal.GetBaseCommitSha() != request.GetBaseCommitSha() || !validRepositoryBundleCommit(proposal.GetBaseCommitSha()) ||
		!validRepositoryBundleCommit(proposal.GetCandidateCommitSha()) || !validRepositoryBundleCommit(proposal.GetCandidateTreeSha()) ||
		!validRepositoryBundleOriginalPin(proposal.GetArchive(), repositorybundle.MaximumArchiveBytes) || !validRepositoryBundleOriginalPin(proposal.GetManifest(), repositorybundle.MaximumManifestBytes) ||
		proposal.GetFileCount() < 1 || proposal.GetFileCount() > repositorybundle.MaximumFiles || proposal.GetTotalBytes() < 1 || proposal.GetTotalBytes() > repositorybundle.MaximumContentBytes ||
		!repositorybundle.ValidContinuation(proposal.GetContinuationState(), proposal.GetContinuationCode(), proposal.GetContinuationNodeRef()) {
		return nil, status.Error(codes.Unavailable, repositoryBundleRejoinInvalid)
	}
	state, err := server.control.Runtime.GetIntegrationInvocation(ctx, &cp.GetIntegrationInvocationRequest{InvocationRef: proposal.GetInvocationRef()})
	if err != nil {
		return nil, status.Error(status.Code(err), "read repository bundle rejoin receipt")
	}
	if state == nil || len(state.ProtoReflect().GetUnknown()) != 0 || state.GetState() != "SUCCEEDED" || state.GetEffectReceiptRef() != proof.GetReceiptRef() || len(state.GetResultSummary()) > 64<<10 || repositorybundle.Digest([]byte(state.GetResultSummary())) != proof.GetReceiptDigest() {
		return nil, status.Error(codes.Unavailable, repositoryBundleRejoinInvalid)
	}
	var fact struct {
		CommitSHA      string `json:"commit_sha"`
		TreeSHA        string `json:"tree_sha"`
		ProposalBranch string `json:"proposal_branch"`
		ManifestSHA256 string `json:"manifest_sha256"`
		FileCount      int32  `json:"file_count"`
		TotalBytes     int64  `json:"total_bytes"`
	}
	if json.Unmarshal([]byte(state.GetResultSummary()), &fact) != nil || fact.CommitSHA != proposal.GetCandidateCommitSha() || fact.TreeSHA != proposal.GetCandidateTreeSha() || fact.ProposalBranch != proposal.GetProposalBranch() || fact.ManifestSHA256 != proposal.GetManifest().GetSha256() || fact.FileCount != proposal.GetFileCount() || fact.TotalBytes != proposal.GetTotalBytes() {
		return nil, status.Error(codes.Unavailable, repositoryBundleRejoinInvalid)
	}
	canonical, err := json.Marshal(map[string]any{"commit_sha": fact.CommitSHA, "tree_sha": fact.TreeSHA, "proposal_branch": fact.ProposalBranch, "manifest_sha256": fact.ManifestSHA256, "file_count": fact.FileCount, "total_bytes": fact.TotalBytes})
	if err != nil || !bytes.Equal(canonical, []byte(state.GetResultSummary())) {
		return nil, status.Error(codes.Unavailable, repositoryBundleRejoinInvalid)
	}
	return repositoryBundleToolResult{ProposalRef: proposal.GetRef(), InvocationRef: proposal.GetInvocationRef(), GateRef: proposal.GetGateRef(), ManifestSHA256: request.GetManifest().GetSha256(), State: proposal.GetState().String(), FileCount: proposal.GetFileCount(), TotalBytes: proposal.GetTotalBytes(),
		Mode: repositorybundle.RejoinMode, ReceiptRef: proof.GetReceiptRef(), ReceiptDigest: proof.GetReceiptDigest(), CommitSHA: fact.CommitSHA, TreeSHA: fact.TreeSHA, ProposalBranch: fact.ProposalBranch, ResultKey: proof.GetResultKey(), SemanticDigest: proof.GetSemanticDigest(), OriginalManifestSHA256: fact.ManifestSHA256}, nil
}

// validRepositoryBundleCommit accepts only full lowercase Git SHA1 identities used by the existing bundle protocol.
// validRepositoryBundleCommit допускает только полный lowercase Git SHA1 существующего bundle protocol.
func validRepositoryBundleCommit(value string) bool {
	if len(value) != 40 {
		return false
	}
	for _, c := range value {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// validRepositoryBundleOriginalPin validates bounded immutable old metadata without substituting the fresh source pins.
// validRepositoryBundleOriginalPin проверяет bounded immutable старые metadata без подмены свежих source pins.
func validRepositoryBundleOriginalPin(pin *cp.RepositoryBundleArtifactPin, maximum int64) bool {
	return pin != nil && len(pin.ProtoReflect().GetUnknown()) == 0 && pin.GetRef() != "" && pin.GetRevision() > 0 && pin.GetVersion() > 0 && pin.GetSizeBytes() > 0 && pin.GetSizeBytes() <= maximum && repositorybundle.ValidDigest(pin.GetSha256())
}
