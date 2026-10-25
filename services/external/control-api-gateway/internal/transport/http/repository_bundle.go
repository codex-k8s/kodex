package httptransport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

// validPublicBundlePin requires complete bounded immutable source metadata and excludes alternate digest formats or unsafe browser integers.
// validPublicBundlePin требует complete bounded immutable source metadata, исключая alternate digest formats и unsafe browser integers.
func validPublicBundlePin(pin *cp.RepositoryBundleArtifactPin, maximum int64) bool {
	return pin != nil && len(pin.ProtoReflect().GetUnknown()) == 0 && strings.HasPrefix(pin.GetRef(), "art_") && fileTargetRef(pin.GetRef()) && validManagedVersion(pin.GetRevision()) && validManagedVersion(pin.GetVersion()) && repositorybundle.ValidDigest(pin.GetSha256()) && pin.GetSizeBytes() > 0 && pin.GetSizeBytes() <= maximum
}

// validPublicBundleProposal validates the closed safe aggregate before browser projection; neither a valid branch name nor an owner response alone proves remote publication.
// validPublicBundleProposal проверяет closed safe aggregate до browser projection; branch name либо owner response сами не доказывают remote publication.
func validPublicBundleProposal(p *cp.RepositoryBundlePublication, gateRef string) bool {
	if p == nil || len(p.ProtoReflect().GetUnknown()) != 0 || p.GetGateRef() != gateRef || !strings.HasPrefix(p.GetRef(), "rbp_") || p.GetProposalBranch() != "kodex/bundle/"+p.GetRef() || !validManagedVersion(p.GetVersion()) || !validManagedVersion(p.GetConnectionVersion()) || !validManagedVersion(p.GetGrantVersion()) || !validManagedDigest(p.GetApprovalDigest()) || len(p.GetBaseCommitSha()) != 40 || !validWriteBackCommit(p.GetBaseCommitSha()) || !validGitSourceLocation(p.GetRepositoryRef(), p.GetProposalBranch(), "bundle") ||
		!repositorybundle.ValidContinuation(p.GetContinuationState(), p.GetContinuationCode(), p.GetContinuationNodeRef()) ||
		!validPublicBundlePin(p.GetArchive(), repositorybundle.MaximumArchiveBytes) || !validPublicBundlePin(p.GetManifest(), repositorybundle.MaximumManifestBytes) || p.GetArchive().GetRef() == p.GetManifest().GetRef() || p.GetFileCount() < 1 || p.GetFileCount() > repositorybundle.MaximumFiles || p.GetTotalBytes() < 0 || p.GetTotalBytes() > repositorybundle.MaximumContentBytes || p.GetCreatedAt() == nil || p.GetCreatedAt().CheckValid() != nil || p.GetExpiresAt() == nil || p.GetExpiresAt().CheckValid() != nil || !p.GetExpiresAt().AsTime().After(p.GetCreatedAt().AsTime()) {
		return false
	}
	for _, ref := range []string{p.GetRef(), p.GetProjectRef(), p.GetOriginRunRef(), p.GetOriginRuntimeRevisionRef(), p.GetInvocationRef(), p.GetGateRef(), p.GetConnectionRef(), p.GetGrantRef()} {
		if !fileTargetRef(ref) {
			return false
		}
	}
	if (p.GetCandidateCommitSha() == "") != (p.GetCandidateTreeSha() == "") {
		return false
	}
	if p.GetCandidateCommitSha() != "" && (len(p.GetCandidateCommitSha()) != 40 || !validWriteBackCommit(p.GetCandidateCommitSha()) || len(p.GetCandidateTreeSha()) != 40 || !validWriteBackCommit(p.GetCandidateTreeSha())) {
		return false
	}
	if p.GetContinuationState() != "" {
		switch p.GetState() {
		case cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_SUCCEEDED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_FAILED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_REJECTED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_CANCELLED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_EXPIRED:
		default:
			return false
		}
	}
	switch p.GetState() {
	case cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_WAITING_APPROVAL, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_QUEUED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_CLAIMED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_REJECTED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_CANCELLED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_EXPIRED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_FAILED:
		if p.GetCandidateCommitSha() != "" || p.GetConfirmedAt() != nil {
			return false
		}
	case cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_EFFECT_STARTED, cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_UNKNOWN_OUTCOME:
		if p.GetCandidateCommitSha() == "" || p.GetConfirmedAt() != nil {
			return false
		}
	case cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_SUCCEEDED:
		if p.GetCandidateCommitSha() == "" || p.GetConfirmedAt() == nil || p.GetConfirmedAt().CheckValid() != nil || p.GetFailureCode() != "" {
			return false
		}
	default:
		return false
	}
	switch p.GetFailureCode() {
	case "", "UNAVAILABLE", "AUTHORITY_CHANGED", "CONTENT_INVALID", "BASE_INVALID", "DEADLINE_EXCEEDED", "BRANCH_CONFLICT", "OUTCOME_UNCONFIRMED":
		return true
	default:
		return false
	}
}

// validPublicBundlePage checks exact gate/version/digest/offset pins, every path/mode/hash and truthful page completeness. Full cross-page manifest verification belongs to the browser consumer.
// validPublicBundlePage проверяет exact gate/version/digest/offset pins, каждый path/mode/hash и truthful page completeness. Full cross-page manifest verification выполняет browser consumer.
func validPublicBundlePage(response *cp.GetRepositoryBundlePublicationResponse, request *cp.GetRepositoryBundlePublicationRequest) bool {
	if response == nil || len(response.ProtoReflect().GetUnknown()) != 0 || !validPublicBundleProposal(response.GetProposal(), request.GetGateRef()) || response.GetOffset() != request.GetOffset() || len(response.GetFiles()) == 0 && response.GetOffset() != response.GetProposal().GetFileCount() || len(response.GetFiles()) > int(request.GetLimit()) || response.GetNextOffset() != response.GetOffset()+int32(len(response.GetFiles())) || response.GetNextOffset() > response.GetProposal().GetFileCount() || response.GetComplete() != (response.GetNextOffset() == response.GetProposal().GetFileCount()) ||
		request.GetExpectedVersion() > 0 && request.GetExpectedVersion() != response.GetProposal().GetVersion() || request.GetExpectedManifestSha256() != "" && request.GetExpectedManifestSha256() != response.GetProposal().GetManifest().GetSha256() {
		return false
	}
	last := ""
	for _, file := range response.GetFiles() {
		if file == nil || len(file.ProtoReflect().GetUnknown()) != 0 || !repositorybundle.ValidPath(file.GetPath()) || file.GetPath() <= last || file.GetMode() != "100644" && file.GetMode() != "100755" || file.GetBytes() < 0 || file.GetBytes() > repositorybundle.MaximumContentBytes || !repositorybundle.ValidDigest(file.GetSha256()) {
			return false
		}
		last = file.GetPath()
	}
	return true
}

// GetOwnerGateRepositoryBundle reads only the authenticated native gate's manifest page and enforces a final exact UTF8 JSON response budget before writing headers or bytes. It exposes neither archive content nor credential descriptors.
// GetOwnerGateRepositoryBundle читает лишь manifest page authenticated native Gate и проверяет final exact UTF8 JSON budget до headers/bytes. Archive content и credential descriptors не выдаются.
func (server *Server) GetOwnerGateRepositoryBundle(w http.ResponseWriter, r *http.Request, ref generated.GateRef, params generated.GetOwnerGateRepositoryBundleParams) {
	offset, limit := 0, 16
	if params.Offset != nil {
		offset = *params.Offset
	}
	if params.Limit != nil {
		limit = *params.Limit
	}
	expected := int64(0)
	if params.ExpectedVersion != nil {
		expected = *params.ExpectedVersion
	}
	if !fileTargetRef(ref) || offset < 0 || offset > repositorybundle.MaximumFiles || limit < 1 || limit > 32 || expected < 0 || expected > maximumSafeJSONInteger || params.ExpectedManifestSha256 != nil && !repositorybundle.ValidDigest(*params.ExpectedManifestSha256) || offset > 0 && (expected < 1 || params.ExpectedManifestSha256 == nil) {
		writeLocalProblem(w, http.StatusBadRequest, "INVALID_REQUEST", false)
		return
	}
	request := &cp.GetRepositoryBundlePublicationRequest{GateRef: ref, Offset: int32(offset), Limit: int32(limit), ExpectedVersion: expected, ExpectedManifestSha256: stringValue(params.ExpectedManifestSha256)}
	response, err := server.control.RepositoryBundles.GetRepositoryBundlePublication(r.Context(), request)
	if err != nil {
		writeRPCProblem(w, err)
		return
	}
	if !validPublicBundlePage(response, request) {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	value, err := messageMap(response)
	if err != nil {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	raw, err := json.Marshal(value)
	if err != nil || len(raw)+1 > 24<<10 {
		writeLocalProblem(w, http.StatusBadGateway, "INVALID_UPSTREAM_RESPONSE", false)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("ETag", fmt.Sprintf("\"%d\"", response.GetProposal().GetVersion()))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(append(raw, '\n'))
}
