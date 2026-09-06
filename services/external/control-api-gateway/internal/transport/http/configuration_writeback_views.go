package httptransport

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"unicode/utf8"

	cp "github.com/codex-k8s/kodex/libs/go/controlplaneapi/gen/controlplane/v1"
	generated "github.com/codex-k8s/kodex/services/external/control-api-gateway/internal/transport/http/generated"
)

func writeBackContentDigest(content string) string {
	digest := sha256.Sum256([]byte(content))
	return hex.EncodeToString(digest[:])
}
func validWriteBackContent(content string, empty bool) bool {
	return (empty || len(content) > 0) && len(content) <= 262144 && utf8.ValidString(content) && !strings.ContainsRune(content, 0)
}
func validWriteBackCommit(value string) bool {
	return (len(value) == 40 || len(value) == 64) && strings.Trim(value, "0123456789abcdef") == ""
}

func configurationWriteBackView(v *cp.ManagedConfigurationGitWriteBack) (generated.ConfigurationWriteBack, bool) {
	result := generated.ConfigurationWriteBack{}
	if v == nil || !validManagedVersion(v.Version) || !validManagedVersion(v.ConfigurationVersion) || !validManagedVersion(v.SourceVersion) || !validManagedVersion(v.ConnectionVersion) || !validGitSourceLocation(v.RepositoryRef, v.SourceRefName, v.Path) || !validGitSourceLocation(v.RepositoryRef, v.ProposalBranch, v.Path) || v.ProposalBranch == v.SourceRefName || !validWriteBackCommit(v.BaseCommitSha) || v.CreatedAt == nil || v.CreatedAt.CheckValid() != nil || v.ExpiresAt == nil || v.ExpiresAt.CheckValid() != nil || !v.ExpiresAt.AsTime().After(v.CreatedAt.AsTime()) {
		return result, false
	}
	for _, ref := range []string{v.Ref, v.ConfigurationRef, v.SourceRef, v.ConnectionRef} {
		if !fileTargetRef(ref) {
			return result, false
		}
	}
	for _, digest := range []string{v.BaseContentSha256, v.ProposedContentSha256, v.ApprovalDigest} {
		if !validManagedDigest(digest) {
			return result, false
		}
	}
	kind := strings.TrimPrefix(v.Kind.String(), "MANAGED_CONFIGURATION_KIND_")
	if kind != "ROLE_IMAGE" && kind != "INTEGRATION_DEFINITION" || v.ContentFormat != "JSON" && v.ContentFormat != "YAML" {
		return result, false
	}
	state := strings.TrimPrefix(v.State.String(), "MANAGED_CONFIGURATION_GIT_WRITE_BACK_STATE_")
	switch state {
	case "WAITING_APPROVAL", "QUEUED", "CLAIMED", "EFFECT_STARTED", "SUCCEEDED", "REJECTED", "CANCELLED", "EXPIRED", "FAILED", "UNKNOWN_OUTCOME":
	default:
		return result, false
	}
	result = generated.ConfigurationWriteBack{Ref: v.Ref, Version: v.Version, ConfigurationRef: v.ConfigurationRef, Kind: generated.ConfigurationWriteBackKind(kind), ConfigurationVersion: v.ConfigurationVersion, SourceRef: v.SourceRef, SourceVersion: v.SourceVersion, ConnectionRef: v.ConnectionRef, ConnectionVersion: v.ConnectionVersion, RepositoryRef: v.RepositoryRef, SourceRefName: v.SourceRefName, Path: v.Path, BaseCommitSha: v.BaseCommitSha, BaseContentSha256: v.BaseContentSha256, ProposedContentSha256: v.ProposedContentSha256, ContentFormat: generated.ConfigurationWriteBackContentFormat(v.ContentFormat), ProposalBranch: v.ProposalBranch, ApprovalDigest: v.ApprovalDigest, State: generated.ConfigurationWriteBackState(state), CreatedAt: v.CreatedAt.AsTime(), ExpiresAt: v.ExpiresAt.AsTime(), NextActions: []generated.ConfigurationWriteBackAction{}}
	if v.FailureCode != cp.ManagedConfigurationGitWriteBackFailure_MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_UNSPECIFIED {
		failure := strings.TrimPrefix(v.FailureCode.String(), "MANAGED_CONFIGURATION_GIT_WRITE_BACK_FAILURE_")
		switch failure {
		case "UNAVAILABLE", "CREDENTIAL_REJECTED", "ACCESS_DENIED", "SOURCE_CHANGED", "CONTENT_INVALID", "RESPONSE_INVALID", "AUTHORITY_CHANGED", "DEADLINE_EXCEEDED", "BRANCH_CONFLICT", "OUTCOME_UNCONFIRMED":
		default:
			return result, false
		}
		value := generated.ConfigurationWriteBackFailureCode(failure)
		result.FailureCode = &value
	}
	if v.CandidateCommitSha != "" {
		if !validWriteBackCommit(v.CandidateCommitSha) {
			return result, false
		}
		result.CandidateCommitSha = &v.CandidateCommitSha
	}
	if v.PullRequestRef != "" {
		if !validSearchText(v.PullRequestRef, 1, 256) {
			return result, false
		}
		result.PullRequestRef = &v.PullRequestRef
	}
	if v.PullRequestUrl != "" {
		parsed, err := url.Parse(v.PullRequestUrl)
		if err != nil || len(v.PullRequestUrl) > 2048 || !utf8.ValidString(v.PullRequestUrl) || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
			return result, false
		}
		result.PullRequestUrl = &v.PullRequestUrl
	}
	if v.ApprovedAt != nil {
		if v.ApprovedAt.CheckValid() != nil {
			return result, false
		}
		value := v.ApprovedAt.AsTime()
		result.ApprovedAt = &value
	}
	if v.CompletedAt != nil {
		if v.CompletedAt.CheckValid() != nil {
			return result, false
		}
		value := v.CompletedAt.AsTime()
		result.CompletedAt = &value
	}
	if v.BranchConfirmedAt != nil {
		if v.BranchConfirmedAt.CheckValid() != nil || v.CandidateCommitSha == "" {
			return result, false
		}
		value := v.BranchConfirmedAt.AsTime()
		result.BranchConfirmedAt = &value
	}
	if v.PullRequestConfirmedAt != nil {
		if v.PullRequestConfirmedAt.CheckValid() != nil || v.BranchConfirmedAt == nil || v.PullRequestRef == "" || v.PullRequestUrl == "" {
			return result, false
		}
		value := v.PullRequestConfirmedAt.AsTime()
		result.PullRequestConfirmedAt = &value
	}
	if state == "SUCCEEDED" && (v.BranchConfirmedAt == nil || v.PullRequestConfirmedAt == nil || v.CompletedAt == nil || result.FailureCode != nil) {
		return result, false
	}
	if (state == "UNKNOWN_OUTCOME" || state == "FAILED") && result.FailureCode == nil {
		return result, false
	}
	if len(v.NextActions) != 3 {
		return result, false
	}
	seen := map[string]bool{}
	for _, item := range v.NextActions {
		if item == nil {
			return result, false
		}
		action := strings.TrimPrefix(item.Action.String(), "MANAGED_CONFIGURATION_GIT_WRITE_BACK_ACTION_")
		reason := strings.TrimPrefix(item.Reason.String(), "MANAGED_CONFIGURATION_GIT_WRITE_BACK_ACTION_REASON_")
		switch action {
		case "APPROVE", "REJECT", "CANCEL":
		default:
			return result, false
		}
		switch reason {
		case "NONE", "FORBIDDEN", "STATE", "SOURCE_CHANGED", "EXPIRED", "OUTCOME_UNKNOWN":
		default:
			return result, false
		}
		if seen[action] || item.Enabled != (reason == "NONE") {
			return result, false
		}
		seen[action] = true
		result.NextActions = append(result.NextActions, generated.ConfigurationWriteBackAction{Action: generated.ConfigurationWriteBackActionAction(action), Enabled: item.Enabled, Reason: generated.ConfigurationWriteBackActionReason(reason)})
	}
	return result, true
}
