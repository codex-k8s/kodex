package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"google.golang.org/protobuf/proto"
)

// errRepositoryBundle is a fixed safe diagnostic; archive bytes, credential values and provider errors must not enter the owner result through it.
// errRepositoryBundle — фиксированная безопасная диагностика; archive bytes, credential values и provider errors через неё в owner result не попадают.
var errRepositoryBundle = errors.New("repository bundle publication failed")

// RepositoryBundleExecution holds one immutable owner work snapshot and private verified source/credential bytes; it does not issue approvals or persist owner receipts.
// RepositoryBundleExecution хранит один immutable owner work snapshot и private verified source/credential bytes; approvals и owner receipts ему не принадлежат.
type RepositoryBundleExecution struct {
	adapter    *Adapter
	work       *cp.RepositoryBundlePublicationWork
	manifest   repositorybundle.Manifest
	files      []repositorybundle.File
	credential []byte
	remote     string
	git        *writeBackGitWorkspace
	candidate  *RepositoryBundleCandidate
}

// OpenRepositoryBundle validates typed owner pins, manifest/archive integrity, exact provider scope and human-each-effect capability before loading a credential. No Git write or approval occurs here.
// OpenRepositoryBundle проверяет typed owner pins, manifest/archive integrity, exact provider scope и human-each-effect capability до чтения credential. Git write и approval здесь не происходят.
func (adapter *Adapter) OpenRepositoryBundle(ctx context.Context, work *cp.RepositoryBundlePublicationWork, archive []byte) (*RepositoryBundleExecution, error) {
	if !RepositoryBundleWireKnown(work) || work.GetLease() == nil || work.GetProposal() == nil || work.GetInvocation() == nil || work.GetCommitTime() == nil || work.GetCommitTime().CheckValid() != nil || work.GetDeadline() == nil || work.GetDeadline().CheckValid() != nil {
		return nil, errRepositoryBundle
	}
	p, lease, invocation := work.GetProposal(), work.GetLease(), work.GetInvocation()
	if len(p.ProtoReflect().GetUnknown()) != 0 || len(lease.ProtoReflect().GetUnknown()) != 0 || p.GetContinuationState() != "" || p.GetContinuationCode() != "" || p.GetContinuationNodeRef() != "" || p.GetArchive() == nil || p.GetManifest() == nil || p.GetCreatedAt() == nil || p.GetCreatedAt().CheckValid() != nil || !p.GetCreatedAt().AsTime().Equal(work.GetCommitTime().AsTime()) ||
		lease.GetProposalRef() != p.GetRef() || lease.GetAttempt() < 1 || lease.GetGeneration() < 1 || lease.GetClaimant() == "" || lease.GetFence() == "" || lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil || !lease.GetExpiresAt().AsTime().After(time.Now()) || !work.GetDeadline().AsTime().After(time.Now()) ||
		p.GetProposalBranch() != "kodex/bundle/"+p.GetRef() || !validWriteBackRef(p.GetProposalBranch()) || p.GetVersion() < 1 || p.GetConnectionVersion() < 1 || p.GetGrantVersion() < 1 || !repositorybundle.ValidDigest(p.GetApprovalDigest()) || len(p.GetBaseCommitSha()) != 40 || !sourceCommitPattern.MatchString(p.GetBaseCommitSha()) {
		return nil, errRepositoryBundle
	}
	mode := work.GetMode()
	if mode != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE && mode != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY ||
		mode == cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE && p.GetState() != cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_CLAIMED ||
		mode == cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_RECOVER_READ_ONLY && (p.GetState() != cp.RepositoryBundlePublicationState_REPOSITORY_BUNDLE_PUBLICATION_STATE_UNKNOWN_OUTCOME || work.GetEffectStartedAt() == nil || work.GetEffectStartedAt().CheckValid() != nil || len(p.GetCandidateCommitSha()) != 40 || !sourceCommitPattern.MatchString(p.GetCandidateCommitSha()) || len(p.GetCandidateTreeSha()) != 40 || !sourceCommitPattern.MatchString(p.GetCandidateTreeSha())) {
		return nil, errRepositoryBundle
	}
	if invocation.GetInvocationRef() != p.GetInvocationRef() || invocation.GetConnectionRef() != p.GetConnectionRef() || invocation.GetGrantRef() != p.GetGrantRef() || invocation.GetGrantVersion() != p.GetGrantVersion() || invocation.GetOperation() != repositorybundle.Operation || invocation.GetCapabilityKey() != invocation.GetOperation() || invocation.GetApprovalPolicy() != cp.IntegrationApprovalPolicy_INTEGRATION_APPROVAL_POLICY_HUMAN_EACH_EFFECT || invocation.GetRisk() != cp.IntegrationRisk_INTEGRATION_RISK_WRITE || invocation.GetCredentialRevision() == nil || invocation.GetPublicConfiguration() == nil {
		return nil, errRepositoryBundle
	}
	if int64(len(work.GetManifest())) != p.GetManifest().GetSizeBytes() || repositorybundle.Digest(work.GetManifest()) != p.GetManifest().GetSha256() {
		return nil, errRepositoryBundle
	}
	manifest, err := repositorybundle.ParseManifest(work.GetManifest())
	if err != nil || len(manifest.Files) != int(p.GetFileCount()) || manifest.TotalBytes != p.GetTotalBytes() {
		return nil, errRepositoryBundle
	}
	var files []repositorybundle.File
	if mode == cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE {
		if int64(len(archive)) != p.GetArchive().GetSizeBytes() || repositorybundle.Digest(archive) != p.GetArchive().GetSha256() {
			return nil, errRepositoryBundle
		}
		manifest, files, err = repositorybundle.Open(archive, work.GetManifest())
		if err != nil {
			return nil, errRepositoryBundle
		}
	} else if len(archive) != 0 {
		return nil, errRepositoryBundle
	}
	opened := false
	defer func() {
		if !opened {
			for _, file := range files {
				clear(file.Bytes)
			}
		}
	}()

	request := RequestFromInvocation(invocation)
	definition, capability, canonicalInput, configuration, err := adapter.validateInvocation(request)
	if err != nil {
		return nil, errRepositoryBundle
	}
	archivePin := repositorybundle.ArtifactPin{Ref: p.GetArchive().GetRef(), Revision: p.GetArchive().GetRevision(), Version: p.GetArchive().GetVersion(), SHA256: p.GetArchive().GetSha256(), Size: p.GetArchive().GetSizeBytes()}
	manifestPin := repositorybundle.ArtifactPin{Ref: p.GetManifest().GetRef(), Revision: p.GetManifest().GetRevision(), Version: p.GetManifest().GetVersion(), SHA256: p.GetManifest().GetSha256(), Size: p.GetManifest().GetSizeBytes()}
	expectedInput, marshalErr := json.Marshal(repositorybundle.InvocationInput(archivePin, manifestPin, p.GetBaseCommitSha()))
	expectedCanonical, canonicalErr := capability.ValidateInput(expectedInput)
	if marshalErr != nil || canonicalErr != nil || !bytes.Equal(canonicalInput, expectedCanonical) || capability.Operation != repositorybundle.Operation || capability.Risk != "WRITE" || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" || definition.Spec.Adapter != "GITHUB" || definition.Metadata.Key != "github" ||
		p.GetRepositoryRef() != strings.ToLower(configuration["owner"]+"/"+configuration["repository"]) ||
		!definition.HasNetworkDestination(integrationpackage.NetworkDestination{Key: "github_git", Source: "STATIC", Hostname: "github.com", Port: 443, TLS: "REQUIRED"}) {
		return nil, errRepositoryBundle
	}
	credential, err := adapter.readCredential(ctx, request.Credential)
	if err != nil {
		return nil, err
	}
	remote := "https://github.com/" + url.PathEscape(configuration["owner"]) + "/" + url.PathEscape(configuration["repository"]) + ".git"
	opened = true
	return &RepositoryBundleExecution{adapter: adapter, work: proto.Clone(work).(*cp.RepositoryBundlePublicationWork), manifest: manifest, files: files, credential: credential, remote: remote}, nil
}

// Close joins no background work because this object starts none, removes its bounded private Git scratch and clears owned source/credential byte buffers.
// Close не завершает background work, поскольку объект его не создаёт, удаляет bounded private Git scratch и очищает принадлежащие ему source/credential byte buffers.
func (execution *RepositoryBundleExecution) Close() {
	if execution.git != nil {
		execution.git.close()
	}
	clear(execution.credential)
	for _, file := range execution.files {
		clear(file.Bytes)
	}
	clear(execution.work.Manifest)
}

// workspace uses the existing credential-file/TLS/proxy/process-group primitive with pinned owner author/time metadata; user code and Git hooks are never executed.
// workspace использует существующий credential-file/TLS/proxy/process-group primitive с pinned owner author/time metadata; user code и Git hooks не исполняются.
func (execution *RepositoryBundleExecution) workspace(ctx context.Context) (*writeBackGitWorkspace, error) {
	return newWriteBackGitWorkspace(ctx, execution.remote, execution.adapter.proxyURL, execution.credential, "Kodex", "repository-bundle@kodex.invalid", execution.work.GetCommitTime().AsTime())
}

// PrepareCandidate permits execute mode only, verifies destination absence, and remembers the exact locally prepared full-set candidate before owner Begin.
// PrepareCandidate допускает лишь execute mode, проверяет отсутствие destination и запоминает exact locally prepared full-set candidate до owner Begin.
func (execution *RepositoryBundleExecution) PrepareCandidate(ctx context.Context) (RepositoryBundleCandidate, error) {
	if execution.work.GetMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE || execution.git != nil {
		return RepositoryBundleCandidate{}, errRepositoryBundle
	}
	workspace, err := execution.workspace(ctx)
	if err != nil {
		return RepositoryBundleCandidate{}, err
	}
	execution.git = workspace
	current, err := workspace.bundleRef(ctx, execution.work.GetProposal().GetProposalBranch())
	if err != nil || current != "" {
		return RepositoryBundleCandidate{}, errRepositoryBundle
	}
	candidate, err := workspace.bundleCandidate(ctx, execution.work.GetProposal().GetBaseCommitSha(), execution.manifest, execution.files)
	if err != nil {
		return RepositoryBundleCandidate{}, err
	}
	execution.candidate = &candidate
	return candidate, nil
}

// PushCandidate permits one exact remembered execute candidate after the caller's durable Begin ACK. CAS targets only the absent server-assigned proposal ref and performs no retry.
// PushCandidate допускает exact remembered execute candidate после durable Begin ACK caller. CAS направлен лишь в отсутствующую server-assigned proposal ref, без retry.
func (execution *RepositoryBundleExecution) PushCandidate(ctx context.Context, candidate RepositoryBundleCandidate) error {
	if execution.work.GetMode() != cp.IntegrationInvocationWorkMode_INTEGRATION_INVOCATION_WORK_MODE_EXECUTE || execution.git == nil || execution.candidate == nil || *execution.candidate != candidate {
		return errRepositoryBundle
	}
	// Consume the local effect permission even when the provider response is lost.
	// Local effect permission расходуется даже при потере provider response.
	execution.candidate = nil
	return execution.git.push(ctx, execution.work.GetProposal().GetProposalBranch(), candidate.CommitSHA)
}

// VerifyCandidate uses an independent new Git object database, not the prepared local candidate cache, to prove remote ref/parent/tree and every file byte. It is safe in read-only recovery and never pushes on absence.
// VerifyCandidate использует независимую новую Git object database, не prepared local candidate cache, доказывая remote ref/parent/tree и bytes каждого file. Допустим в read-only recovery и никогда не делает push при отсутствии.
func (execution *RepositoryBundleExecution) VerifyCandidate(ctx context.Context, candidate RepositoryBundleCandidate) error {
	reader, err := execution.workspace(ctx)
	if err != nil {
		return err
	}
	defer reader.close()
	return reader.verifyBundle(ctx, execution.work.GetProposal().GetProposalBranch(), execution.work.GetProposal().GetBaseCommitSha(), candidate, execution.manifest)
}
