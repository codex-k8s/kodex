package grpc

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
)

// TestRepositoryBundleRejoinProjection preserves old proposal JSON/digest material while mapping response-only current execution proof separately.
// TestRepositoryBundleRejoinProjection сохраняет старые proposal JSON/digest bytes, отображая current execution proof отдельно.
func TestRepositoryBundleRejoinProjection(t *testing.T) {
	p := entity.RepositoryBundlePublication{Ref: "rbp_original", OriginRunRef: "run_original", OriginRuntimeRevisionRef: "rrev_original", State: "SUCCEEDED", CandidateCommitSHA: "original-commit", Archive: entity.RepositoryBundleArtifactPin{Ref: "art_original"}}
	before, _ := json.Marshal(p)
	p.Rejoin = &entity.RepositoryBundlePublicationRejoin{RunRef: "run_current", NodeRef: "nod_current", RuntimeRevisionRef: "rrev_current", LeaseRef: "lea_current", Generation: 7, RuntimeAttempt: 2, BusinessAttempt: 3, SessionRef: "ses_current", TurnRef: "trn_current", ConnectionVersion: 4, SemanticDigest: "semantic", ReceiptRef: "erc_original", ReceiptDigest: "receipt", ResultKey: "pr", Archive: entity.RepositoryBundleArtifactPin{Ref: "art_current"}}
	after, _ := json.Marshal(p)
	if !bytes.Equal(before, after) {
		t.Fatal("response-only metadata changed frozen proposal or approval bytes")
	}
	proposal, proof := castRepositoryBundle(p), castRepositoryBundleRejoin(p.Rejoin)
	if proposal.GetOriginRunRef() != p.OriginRunRef || proposal.GetOriginRuntimeRevisionRef() != p.OriginRuntimeRevisionRef || proposal.GetArchive().GetRef() != p.Archive.Ref || proof.GetSchema() != repositorybundle.RejoinSchema || proof.GetMode() != repositorybundle.RejoinMode || proof.GetCurrentRunRef() != p.Rejoin.RunRef || proof.GetCurrentNodeRef() != p.Rejoin.NodeRef || proof.GetCurrentRuntimeRevisionRef() != p.Rejoin.RuntimeRevisionRef || proof.GetCurrentLeaseRef() != p.Rejoin.LeaseRef || proof.GetCurrentGeneration() != p.Rejoin.Generation || proof.GetCurrentRuntimeAttempt() != p.Rejoin.RuntimeAttempt || proof.GetBusinessAttempt() != p.Rejoin.BusinessAttempt || proof.GetCurrentSessionRef() != p.Rejoin.SessionRef || proof.GetCurrentTurnRef() != p.Rejoin.TurnRef || proof.GetConnectionVersion() != p.Rejoin.ConnectionVersion || proof.GetSemanticDigest() != p.Rejoin.SemanticDigest || proof.GetReceiptRef() != p.Rejoin.ReceiptRef || proof.GetReceiptDigest() != p.Rejoin.ReceiptDigest || proof.GetCurrentArchive().GetRef() != p.Rejoin.Archive.Ref || proof.GetResultKey() != p.Rejoin.ResultKey {
		t.Fatal("typed rejoin projection changed original pins or omitted current proof")
	}
	if castRepositoryBundleRejoin(nil) != nil {
		t.Fatal("legacy response gained synthetic rejoin presence")
	}
}
