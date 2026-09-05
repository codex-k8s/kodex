package integration

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"google.golang.org/protobuf/proto"
)

type configurationWriteBackError struct {
	failure cp.ManagedConfigurationGitWriteBackFailure
}

func (*configurationWriteBackError) Error() string { return "configuration writeback failed" }
func writeBackFailure(code cp.ManagedConfigurationGitWriteBackFailure) error {
	return &configurationWriteBackError{failure: code}
}
func ConfigurationWriteBackFailure(err error) cp.ManagedConfigurationGitWriteBackFailure {
	var safe *configurationWriteBackError
	if errors.As(err, &safe) {
		return safe.failure
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_DEADLINE_EXCEEDED
	}
	return cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_UNAVAILABLE
}

type ConfigurationWriteBackCandidate struct{ CommitSHA, TreeSHA, BlobSHA, BaseBlobSHA string }
type ConfigurationWriteBackPullRequest struct{ Ref, URL string }

type ConfigurationWriteBackExecution struct {
	adapter       *Adapter
	work          *cp.ManagedConfigurationGitWriteBackWork
	definition    integrationpackage.Package
	configuration map[string]string
	credential    []byte
	git           *writeBackGitWorkspace
	remote        string
	deadline      time.Time
	candidate     *ConfigurationWriteBackCandidate
}

func (execution *ConfigurationWriteBackExecution) Close() {
	if execution.git != nil {
		execution.git.close()
	}
	clear(execution.credential)
	clear(execution.work.ProposedContent)
}

func (adapter *Adapter) OpenConfigurationWriteBack(ctx context.Context, work *cp.ManagedConfigurationGitWriteBackWork) (*ConfigurationWriteBackExecution, error) {
	invalid := func() (*ConfigurationWriteBackExecution, error) {
		return nil, writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_RESPONSE_INVALID)
	}
	if work == nil || len(work.ProtoReflect().GetUnknown()) != 0 || work.GetLease() == nil || work.GetProposal() == nil || work.GetDeadline() == nil || work.GetDeadline().CheckValid() != nil {
		return invalid()
	}
	lease, p := work.GetLease(), work.GetProposal()
	if len(lease.ProtoReflect().GetUnknown()) != 0 || len(p.ProtoReflect().GetUnknown()) != 0 || lease.GetProposalRef() != p.GetRef() || lease.GetAttempt() < 1 || lease.GetClaimGeneration() < 1 || lease.GetClaimant() == "" || lease.GetFence() == "" || lease.GetExpiresAt() == nil || lease.GetExpiresAt().CheckValid() != nil || !lease.GetExpiresAt().AsTime().After(time.Now()) || !work.GetDeadline().AsTime().After(time.Now()) {
		return invalid()
	}
	if p.GetVersion() < 1 || p.GetConfigurationVersion() < 1 || p.GetSourceVersion() < 1 || p.GetConnectionVersion() < 1 || p.GetConfigurationRef() == "" || p.GetSourceRef() == "" || p.GetConnectionRef() == "" || p.GetApprovalDigest() == "" || p.GetApprovedAt() == nil || p.GetApprovedAt().CheckValid() != nil || p.GetCreatedAt() == nil || p.GetCreatedAt().CheckValid() != nil {
		return invalid()
	}
	if p.GetKind() != cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_ROLE_IMAGE && p.GetKind() != cp.ManagedConfigurationKind_MANAGED_CONFIGURATION_KIND_INTEGRATION_DEFINITION {
		return invalid()
	}
	if p.GetContentFormat() != "JSON" && p.GetContentFormat() != "YAML" || !sourceCommitPattern.MatchString(p.GetBaseCommitSha()) || !validRepositoryPath(p.GetPath(), false) || len(p.GetPath()) > 512 || p.GetProposalBranch() != "kodex/writeback/"+p.GetRef() || !validWriteBackRef(p.GetSourceRefName()) || !validWriteBackRef(p.GetProposalBranch()) || p.GetProposalBranch() == p.GetSourceRefName() {
		return invalid()
	}
	if len(work.GetProposedContent()) == 0 || len(work.GetProposedContent()) > maximumSourceContentBytes || sourceContentDigest(work.GetProposedContent()) != p.GetProposedContentSha256() || len(p.GetBaseContentSha256()) != 64 || work.GetEffectMarker() != "kodex-configuration-writeback:"+p.GetRef() || work.GetCommitMessage() != "Update managed configuration" || work.GetCommitAuthorName() != "Kodex" || work.GetCommitAuthorEmail() != "configuration@kodex.invalid" || work.GetCommitTime() == nil || work.GetCommitTime().CheckValid() != nil || !work.GetCommitTime().AsTime().Equal(p.GetCreatedAt().AsTime()) {
		return invalid()
	}
	if work.GetMode() != cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE && work.GetMode() != cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY {
		return invalid()
	}
	if work.GetMode() == cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE && p.GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_CLAIMED || work.GetMode() == cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY && p.GetState() != cp.ManagedConfigurationGitWriteBackState_MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_UNKNOWN_OUTCOME {
		return invalid()
	}
	if work.GetEffect() != cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_BRANCH && work.GetEffect() != cp.ManagedConfigurationGitWriteBackEffect_MANAGED_CONFIGURATION_GIT_WRITE_BACK_EFFECT_PULL_REQUEST {
		return invalid()
	}
	if work.GetMode() == cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_RECOVER_READ_ONLY && (work.GetEffectStartedAt() == nil || work.GetEffectStartedAt().CheckValid() != nil || !sourceCommitPattern.MatchString(p.GetCandidateCommitSha())) {
		return invalid()
	}
	request := Request{DefinitionPackage: work.GetDefinitionPackage(), DefinitionKey: work.GetDefinitionKey(), DefinitionVersion: work.GetDefinitionVersion(), DefinitionDigest: work.GetDefinitionDigest(), ConnectionRef: p.GetConnectionRef(), Credential: credentialFromProto(work.GetCredentialRevision())}
	definition, err := adapter.validateDefinition(request)
	if err != nil || work.GetPublicConfiguration() == nil {
		return invalid()
	}
	configuration, err := normalizeStringMap(work.GetPublicConfiguration().AsMap())
	if err != nil || definition.ValidateConfiguration(configuration) != nil {
		return invalid()
	}
	remote := ""
	operations := []string{}
	switch definition.Spec.Adapter {
	case "GITHUB":
		if p.GetRepositoryRef() != configuration["owner"]+"/"+configuration["repository"] || !definition.HasNetworkDestination(integrationpackage.NetworkDestination{Key: "github_git", Source: "STATIC", Hostname: "github.com", Port: 443, TLS: "REQUIRED"}) {
			return invalid()
		}
		remote = "https://github.com/" + url.PathEscape(configuration["owner"]) + "/" + url.PathEscape(configuration["repository"]) + ".git"
		operations = []string{"github.branch.create", "github.repository.content.update", "github.pull_request.create"}
	case "GITLAB":
		base, err := parseProviderBaseURL(configuration["base_url"])
		if err != nil || p.GetRepositoryRef() != configuration["project_path"] {
			return invalid()
		}
		base.Path = strings.TrimSuffix(base.Path, "/") + "/" + configuration["project_path"] + ".git"
		remote = base.String()
		operations = []string{"gitlab.branch.create", "gitlab.commit.create", "gitlab.merge_request.create"}
	default:
		return invalid()
	}
	deadline := work.GetDeadline().AsTime()
	if lease.GetExpiresAt().AsTime().Before(deadline) {
		deadline = lease.GetExpiresAt().AsTime()
	}
	for _, operation := range operations {
		capability, ok := definition.Capability(operation)
		if !ok || capability.Operation != operation || capability.Risk == "READ" || capability.ApprovalPolicy != "HUMAN_EACH_EFFECT" {
			return invalid()
		}
		if selected := time.Now().Add(time.Duration(capability.Execution.TimeoutSeconds) * time.Second); selected.Before(deadline) {
			deadline = selected
		}
	}
	readOperation := "github.repository.content.read"
	if definition.Spec.Adapter == "GITLAB" {
		readOperation = "gitlab.repository.file.read"
	}
	readCapability, ok := definition.Capability(readOperation)
	if !ok || readCapability.Risk != "READ" || readCapability.ApprovalPolicy != "NONE" {
		return invalid()
	}
	if selected := time.Now().Add(time.Duration(readCapability.Execution.TimeoutSeconds) * time.Second); selected.Before(deadline) {
		deadline = selected
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	credential, err := adapter.readCredential(ctx, request.Credential)
	if err != nil {
		return nil, writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_CREDENTIAL_REJECTED)
	}
	return &ConfigurationWriteBackExecution{adapter: adapter, work: proto.Clone(work).(*cp.ManagedConfigurationGitWriteBackWork), definition: definition, configuration: configuration, credential: credential, remote: remote, deadline: deadline}, nil
}

func validWriteBackRef(ref string) bool {
	return ref != "" && len(ref) <= 256 && !strings.HasPrefix(ref, "-") && !strings.HasPrefix(ref, "/") && !strings.HasSuffix(ref, "/") && !strings.HasSuffix(ref, ".") && !strings.HasSuffix(ref, ".lock") && !strings.Contains(ref, "..") && !strings.Contains(ref, "//") && !strings.Contains(ref, "@{") && !strings.ContainsAny(ref, " ~^:?*[\\\x00\r\n\t")
}

func (execution *ConfigurationWriteBackExecution) prepareGit(ctx context.Context) error {
	if execution.git != nil {
		return nil
	}
	w := execution.work
	workspace, err := newWriteBackGitWorkspace(ctx, execution.remote, execution.adapter.proxyURL, execution.credential, w.GetCommitAuthorName(), w.GetCommitAuthorEmail(), w.GetCommitTime().AsTime())
	if err != nil {
		return err
	}
	execution.git = workspace
	return nil
}

func (execution *ConfigurationWriteBackExecution) PrepareCandidate(ctx context.Context) (ConfigurationWriteBackCandidate, error) {
	ctx, cancel := context.WithDeadline(ctx, execution.deadline)
	defer cancel()
	if execution.work.GetMode() != cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE {
		return ConfigurationWriteBackCandidate{}, errWriteBackGit
	}
	if err := execution.prepareGit(ctx); err != nil {
		return ConfigurationWriteBackCandidate{}, err
	}
	p := execution.work.GetProposal()
	source, proposal, err := execution.git.refs(ctx, p.GetSourceRefName(), p.GetProposalBranch())
	if err != nil {
		return ConfigurationWriteBackCandidate{}, err
	}
	if source != p.GetBaseCommitSha() {
		return ConfigurationWriteBackCandidate{}, writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_SOURCE_CHANGED)
	}
	if proposal != "" {
		return ConfigurationWriteBackCandidate{}, writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_BRANCH_CONFLICT)
	}
	candidate, err := execution.git.candidate(ctx, p.GetBaseCommitSha(), p.GetPath(), p.GetBaseContentSha256(), execution.work.GetCommitMessage(), execution.work.GetProposedContent())
	if err != nil {
		return ConfigurationWriteBackCandidate{}, err
	}
	result := ConfigurationWriteBackCandidate{CommitSHA: candidate.Commit, TreeSHA: candidate.Tree, BlobSHA: candidate.Blob, BaseBlobSHA: candidate.BaseBlob}
	if execution.definition.Spec.Adapter == "GITHUB" {
		if err := execution.validateInput("github.branch.create", map[string]any{"branch": p.GetProposalBranch(), "sha": p.GetBaseCommitSha()}); err != nil {
			return ConfigurationWriteBackCandidate{}, err
		}
		if err := execution.validateInput("github.repository.content.update", map[string]any{"branch": p.GetProposalBranch(), "path": p.GetPath(), "message": execution.work.GetCommitMessage(), "content_base64": base64.StdEncoding.EncodeToString(execution.work.GetProposedContent()), "sha": result.BaseBlobSHA}); err != nil {
			return ConfigurationWriteBackCandidate{}, err
		}
	} else {
		if err := execution.validateInput("gitlab.branch.create", map[string]any{"branch": p.GetProposalBranch(), "ref": p.GetBaseCommitSha()}); err != nil {
			return ConfigurationWriteBackCandidate{}, err
		}
		if err := execution.validateInput("gitlab.commit.create", map[string]any{"branch": p.GetProposalBranch(), "action": "update", "file_path": p.GetPath(), "content": string(execution.work.GetProposedContent()), "commit_message": execution.work.GetCommitMessage()}); err != nil {
			return ConfigurationWriteBackCandidate{}, err
		}
	}
	execution.candidate = &result
	return result, nil
}

func (execution *ConfigurationWriteBackExecution) PushCandidate(ctx context.Context, candidate ConfigurationWriteBackCandidate) error {
	ctx, cancel := context.WithDeadline(ctx, execution.deadline)
	defer cancel()
	if execution.git == nil || execution.candidate == nil || *execution.candidate != candidate || execution.work.GetMode() != cp.ManagedConfigurationGitWriteBackWorkMode_MANAGED_CONFIGURATION_GIT_WRITE_BACK_WORK_MODE_EXECUTE {
		return errWriteBackGit
	}
	p := execution.work.GetProposal()
	source, proposal, err := execution.git.refs(ctx, p.GetSourceRefName(), p.GetProposalBranch())
	if err != nil {
		return err
	}
	if source != p.GetBaseCommitSha() || proposal != "" {
		return writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_BRANCH_CONFLICT)
	}
	return execution.git.push(ctx, p.GetProposalBranch(), candidate.CommitSHA)
}

func (execution *ConfigurationWriteBackExecution) validateInput(operation string, input map[string]any) error {
	capability, ok := execution.definition.Capability(operation)
	if !ok {
		return errWriteBackGit
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return errWriteBackGit
	}
	_, err = capability.ValidateInput(raw)
	if err != nil {
		return writeBackFailure(cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_CONTENT_INVALID)
	}
	return nil
}
