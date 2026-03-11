package staff

import (
	"context"
	"testing"

	nextstepdomain "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/nextstep"
	valuetypes "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/types/value"
)

type stubNextStepGitHubMgmt struct {
	labels []string
}

func (s *stubNextStepGitHubMgmt) Preflight(context.Context, valuetypes.GitHubPreflightParams) (valuetypes.GitHubPreflightReport, error) {
	return valuetypes.GitHubPreflightReport{}, nil
}

func (s *stubNextStepGitHubMgmt) GetDefaultBranch(context.Context, string, string, string) (string, error) {
	return "main", nil
}

func (s *stubNextStepGitHubMgmt) GetFile(context.Context, string, string, string, string, string) ([]byte, bool, error) {
	return nil, false, nil
}

func (s *stubNextStepGitHubMgmt) CreatePullRequestWithFiles(context.Context, string, string, string, string, string, string, string, map[string][]byte) (int, string, error) {
	return 0, "", nil
}

func (s *stubNextStepGitHubMgmt) ListIssueLabels(context.Context, string, string, string, int) ([]string, error) {
	return append([]string(nil), s.labels...), nil
}

func (s *stubNextStepGitHubMgmt) AddIssueLabels(context.Context, string, string, string, int, []string) ([]string, error) {
	return nil, nil
}

func (s *stubNextStepGitHubMgmt) RemoveIssueLabel(context.Context, string, string, string, int, string) error {
	return nil
}

func (s *stubNextStepGitHubMgmt) EnsureEnvironment(context.Context, string, string, string, string) error {
	return nil
}

func (s *stubNextStepGitHubMgmt) ListEnvSecretNames(context.Context, string, string, string, string) (map[string]struct{}, error) {
	return map[string]struct{}{}, nil
}

func (s *stubNextStepGitHubMgmt) ListEnvVariableValues(context.Context, string, string, string, string) (map[string]string, error) {
	return map[string]string{}, nil
}

func (s *stubNextStepGitHubMgmt) UpsertEnvSecret(context.Context, string, string, string, string, string, string) error {
	return nil
}

func (s *stubNextStepGitHubMgmt) UpsertEnvVariable(context.Context, string, string, string, string, string, string) error {
	return nil
}

var _ githubManagementClient = (*stubNextStepGitHubMgmt)(nil)

func TestPreviewOrExecuteIssueStageTransition_AcceptsConfiguredReviseLabel(t *testing.T) {
	t.Parallel()

	service := &Service{
		cfg: Config{
			NextStepLabels: nextstepdomain.NewLabels(nextstepdomain.Config{
				RunQA:       "run:quality-assurance",
				RunQARevise: "run:quality-assurance:revise",
			}),
		},
		githubMgmt: &stubNextStepGitHubMgmt{
			labels: []string{"state:in-review"},
		},
	}

	result, err := service.previewOrExecuteIssueStageTransition(context.Background(), "token", "codex-k8s", "codex-k8s", 255, "run:quality-assurance:revise", false)
	if err != nil {
		t.Fatalf("previewOrExecuteIssueStageTransition() error = %v", err)
	}
	if len(result.AddedLabels) != 1 || result.AddedLabels[0] != "run:quality-assurance:revise" {
		t.Fatalf("previewOrExecuteIssueStageTransition().AddedLabels = %#v, want configured revise label", result.AddedLabels)
	}
}
