package staff

import (
	"context"
	"fmt"
	"strings"

	"github.com/codex-k8s/codex-k8s/libs/go/errs"
	repoprovider "github.com/codex-k8s/codex-k8s/libs/go/repo/provider"
	enumtypes "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/types/enum"
	valuetypes "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/types/value"
)

// ReplayGitHubRateLimitPlatformCall replays one platform-owned GitHub mutation after recovery.
func (s *Service) ReplayGitHubRateLimitPlatformCall(ctx context.Context, payload valuetypes.GitHubRateLimitPlatformCallReplayPayload) error {
	if s == nil {
		return fmt.Errorf("staff service is not configured")
	}
	if s.githubMgmt == nil {
		return fmt.Errorf("failed_precondition: github management client is not configured")
	}

	operationKind := strings.TrimSpace(string(payload.OperationKind))
	if operationKind == "" {
		return fmt.Errorf("github rate-limit platform replay payload.operation_kind is required")
	}

	repositoryFullName := strings.TrimSpace(payload.RepositoryFullName)
	if repositoryFullName == "" {
		return errs.Validation{Field: "repository_full_name", Msg: "is required"}
	}
	owner, repo, err := parseGitHubFullName(repositoryFullName)
	if err != nil {
		return errs.Validation{Field: "repository_full_name", Msg: err.Error()}
	}

	binding, ok, err := s.repos.FindByProviderOwnerName(ctx, string(repoprovider.ProviderGitHub), owner, repo)
	if err != nil {
		return err
	}
	if !ok {
		return errs.Validation{Field: "repository_full_name", Msg: "repository is not bound to any project"}
	}

	_, botToken, _, _, err := s.resolveEffectiveGitHubTokens(ctx, binding.ProjectID, binding.RepositoryID)
	if err != nil {
		return err
	}

	switch enumtypes.GitHubRateLimitPlatformReplayOperationKind(operationKind) {
	case enumtypes.GitHubRateLimitPlatformReplayOperationKindIssueStageTransition:
		_, err = s.previewOrExecuteIssueStageTransition(
			ctx,
			botToken,
			owner,
			repo,
			payload.IssueNumber,
			strings.TrimSpace(payload.TargetLabel),
			true,
		)
		if err != nil {
			return fmt.Errorf("replay github rate-limit issue stage transition: %w", err)
		}
		return nil
	default:
		return errs.Validation{Field: "operation_kind", Msg: "must be a known github rate-limit platform replay operation"}
	}
}
