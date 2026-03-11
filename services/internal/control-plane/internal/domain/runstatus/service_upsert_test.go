package runstatus

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/codex-k8s/codex-k8s/libs/go/crypto/tokencrypt"
	floweventdomain "github.com/codex-k8s/codex-k8s/libs/go/domain/flowevent"
	mcpdomain "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/mcp"
	agentrunrepo "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/repository/agentrun"
	platformtokenrepo "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/repository/platformtoken"
	staffrunrepo "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/repository/staffrun"
	entitytypes "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/types/entity"
	querytypes "github.com/codex-k8s/codex-k8s/services/internal/control-plane/internal/domain/types/query"
)

func TestUpsertRunStatusComment_MergesTrackedCommentStateBeforeFallbackEdit(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tokenCrypt, err := tokencrypt.NewService("00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	if err != nil {
		t.Fatalf("tokencrypt.NewService: %v", err)
	}
	botTokenEncrypted, err := tokenCrypt.EncryptString("bot-token")
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}

	runPayload, err := json.Marshal(querytypes.RunPayload{
		Repository: querytypes.RunPayloadRepository{FullName: "codex-k8s/codex-k8s"},
		Issue:      &querytypes.RunPayloadIssue{Number: 258, HTMLURL: "https://github.com/codex-k8s/codex-k8s/issues/258"},
		PullRequest: &querytypes.RunPayloadPullRequest{
			Number:  307,
			HTMLURL: "https://github.com/codex-k8s/codex-k8s/pull/307",
		},
		Trigger: &querytypes.RunPayloadTrigger{
			Source: triggerSourcePullRequestReview,
			Label:  "run:dev:revise",
			Kind:   triggerKindDev,
		},
	})
	if err != nil {
		t.Fatalf("json.Marshal(runPayload): %v", err)
	}

	trackedPayload, err := json.Marshal(runStatusCommentUpsertedPayload{
		RunID:     "run-1",
		CommentID: 401,
	})
	if err != nil {
		t.Fatalf("json.Marshal(trackedPayload): %v", err)
	}

	existingCommentBody := testRunStatusCommentBody(t, commentState{
		RunID:              "run-1",
		Phase:              PhaseFinished,
		RunStatus:          runStatusSucceeded,
		RepositoryFullName: "codex-k8s/codex-k8s",
		PullRequestURL:     "https://github.com/codex-k8s/codex-k8s/pull/307",
		TriggerKind:        triggerKindDev,
		TriggerLabel:       "run:dev:revise",
		PromptLocale:       localeRU,
	})

	github := &runstatusTestGitHub{
		listIssueCommentsFunc: func(context.Context, mcpdomain.GitHubListIssueCommentsParams) ([]mcpdomain.GitHubIssueComment, error) {
			return nil, nil
		},
		getIssueCommentFunc: func(context.Context, mcpdomain.GitHubGetIssueCommentParams) (mcpdomain.GitHubIssueComment, error) {
			return mcpdomain.GitHubIssueComment{
				ID:   401,
				Body: existingCommentBody,
				URL:  "https://github.com/codex-k8s/codex-k8s/pull/307#issuecomment-401",
			}, nil
		},
		editIssueCommentFunc: func(_ context.Context, params mcpdomain.GitHubEditIssueCommentParams) (mcpdomain.GitHubIssueComment, error) {
			return mcpdomain.GitHubIssueComment{
				ID:   params.CommentID,
				Body: params.Body,
				URL:  "https://github.com/codex-k8s/codex-k8s/pull/307#issuecomment-401",
			}, nil
		},
		createIssueCommentFunc: func(context.Context, mcpdomain.GitHubCreateIssueCommentParams) (mcpdomain.GitHubIssueComment, error) {
			t.Fatal("CreateIssueComment must not be called on tracked comment fallback")
			return mcpdomain.GitHubIssueComment{}, nil
		},
	}

	service, err := NewService(Config{
		PublicBaseURL: "https://platform.codex-k8s.dev",
		DefaultLocale: localeRU,
	}, Dependencies{
		Runs: &runstatusTestRunsRepository{
			run: agentrunrepo.Run{
				ID:            "run-1",
				CorrelationID: "corr-1",
				RunPayload:    runPayload,
			},
		},
		Platform: &runstatusTestPlatformTokenRepository{
			item: platformtokenrepo.PlatformGitHubTokens{BotTokenEncrypted: botTokenEncrypted},
		},
		TokenCrypt: tokenCrypt,
		GitHub:     github,
		Kubernetes: runstatusTestKubernetesClient{},
		StaffRuns: &runstatusTestStaffRunsRepository{
			events: []staffrunrepo.FlowEvent{
				{
					CorrelationID: "corr-1",
					EventType:     string(floweventdomain.EventTypeRunStatusCommentUpserted),
					PayloadJSON:   trackedPayload,
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	result, err := service.UpsertRunStatusComment(ctx, UpsertCommentParams{
		RunID:        "run-1",
		Phase:        PhaseFinished,
		TriggerKind:  triggerKindDev,
		PromptLocale: localeRU,
		RunStatus:    runStatusFailed,
	})
	if err != nil {
		t.Fatalf("UpsertRunStatusComment: %v", err)
	}
	if result.CommentID != 401 {
		t.Fatalf("unexpected comment id: got %d want 401", result.CommentID)
	}
	if github.getIssueCommentCalls != 1 {
		t.Fatalf("expected GetIssueComment to be called once, got %d", github.getIssueCommentCalls)
	}
	if github.editIssueCommentCalls != 1 {
		t.Fatalf("expected EditIssueComment to be called once, got %d", github.editIssueCommentCalls)
	}

	editedState, ok := extractStateMarker(github.lastEditedBody)
	if !ok {
		t.Fatalf("edited comment body does not contain state marker: %q", github.lastEditedBody)
	}
	if editedState.RunStatus != runStatusSucceeded {
		t.Fatalf("expected merged terminal status %q, got %q", runStatusSucceeded, editedState.RunStatus)
	}
}

type runstatusTestRunsRepository struct {
	run agentrunrepo.Run
}

func (r *runstatusTestRunsRepository) CreatePendingIfAbsent(context.Context, agentrunrepo.CreateParams) (agentrunrepo.CreateResult, error) {
	return agentrunrepo.CreateResult{}, nil
}

func (r *runstatusTestRunsRepository) GetByID(context.Context, string) (agentrunrepo.Run, bool, error) {
	return r.run, true, nil
}

func (r *runstatusTestRunsRepository) CancelActiveByID(context.Context, string) (bool, error) {
	return false, nil
}

func (r *runstatusTestRunsRepository) ListRecentByProject(context.Context, string, string, int, int) ([]agentrunrepo.RunLookupItem, error) {
	return nil, nil
}

func (r *runstatusTestRunsRepository) SearchRecentByProjectIssueOrPullRequest(context.Context, string, string, int64, int64, int) ([]agentrunrepo.RunLookupItem, error) {
	return nil, nil
}

func (r *runstatusTestRunsRepository) ListRunIDsByRepositoryIssue(context.Context, string, int64, int) ([]string, error) {
	return nil, nil
}

func (r *runstatusTestRunsRepository) ListRunIDsByRepositoryPullRequest(context.Context, string, int64, int) ([]string, error) {
	return nil, nil
}

type runstatusTestPlatformTokenRepository struct {
	item entitytypes.PlatformGitHubTokens
}

func (r *runstatusTestPlatformTokenRepository) Get(context.Context) (entitytypes.PlatformGitHubTokens, bool, error) {
	return r.item, true, nil
}

func (r *runstatusTestPlatformTokenRepository) Upsert(context.Context, platformtokenrepo.UpsertParams) (entitytypes.PlatformGitHubTokens, error) {
	return r.item, nil
}

type runstatusTestStaffRunsRepository struct {
	events []staffrunrepo.FlowEvent
}

func (r *runstatusTestStaffRunsRepository) ListAll(context.Context, int) ([]staffrunrepo.Run, error) {
	return nil, nil
}

func (r *runstatusTestStaffRunsRepository) ListForUser(context.Context, string, int) ([]staffrunrepo.Run, error) {
	return nil, nil
}

func (r *runstatusTestStaffRunsRepository) ListJobsAll(context.Context, staffrunrepo.ListFilter) ([]staffrunrepo.Run, error) {
	return nil, nil
}

func (r *runstatusTestStaffRunsRepository) ListJobsForUser(context.Context, string, staffrunrepo.ListFilter) ([]staffrunrepo.Run, error) {
	return nil, nil
}

func (r *runstatusTestStaffRunsRepository) ListWaitsAll(context.Context, staffrunrepo.ListFilter) ([]staffrunrepo.Run, error) {
	return nil, nil
}

func (r *runstatusTestStaffRunsRepository) ListWaitsForUser(context.Context, string, staffrunrepo.ListFilter) ([]staffrunrepo.Run, error) {
	return nil, nil
}

func (r *runstatusTestStaffRunsRepository) GetByID(context.Context, string) (staffrunrepo.Run, bool, error) {
	return staffrunrepo.Run{}, false, nil
}

func (r *runstatusTestStaffRunsRepository) GetLogsByRunID(context.Context, string) (staffrunrepo.RunLogs, bool, error) {
	return staffrunrepo.RunLogs{}, false, nil
}

func (r *runstatusTestStaffRunsRepository) ListEventsByCorrelation(context.Context, string, int) ([]staffrunrepo.FlowEvent, error) {
	return r.events, nil
}

func (r *runstatusTestStaffRunsRepository) DeleteFlowEventsByProjectID(context.Context, string) error {
	return nil
}

func (r *runstatusTestStaffRunsRepository) GetCorrelationByRunID(context.Context, string) (string, string, bool, error) {
	return "", "", false, nil
}

type runstatusTestKubernetesClient struct{}

func (runstatusTestKubernetesClient) DeleteManagedRunNamespace(context.Context, string) (bool, error) {
	return false, nil
}

func (runstatusTestKubernetesClient) NamespaceExists(context.Context, string) (bool, error) {
	return false, nil
}

func (runstatusTestKubernetesClient) JobExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (runstatusTestKubernetesClient) FindManagedRunNamespaceByRunID(context.Context, string) (string, bool, error) {
	return "", false, nil
}

type runstatusTestGitHub struct {
	listIssueCommentsFunc  func(context.Context, mcpdomain.GitHubListIssueCommentsParams) ([]mcpdomain.GitHubIssueComment, error)
	getIssueCommentFunc    func(context.Context, mcpdomain.GitHubGetIssueCommentParams) (mcpdomain.GitHubIssueComment, error)
	createIssueCommentFunc func(context.Context, mcpdomain.GitHubCreateIssueCommentParams) (mcpdomain.GitHubIssueComment, error)
	editIssueCommentFunc   func(context.Context, mcpdomain.GitHubEditIssueCommentParams) (mcpdomain.GitHubIssueComment, error)

	getIssueCommentCalls  int
	editIssueCommentCalls int
	lastEditedBody        string
}

func (g *runstatusTestGitHub) ListIssueComments(ctx context.Context, params mcpdomain.GitHubListIssueCommentsParams) ([]mcpdomain.GitHubIssueComment, error) {
	if g.listIssueCommentsFunc == nil {
		return nil, nil
	}
	return g.listIssueCommentsFunc(ctx, params)
}

func (g *runstatusTestGitHub) GetIssueComment(ctx context.Context, params mcpdomain.GitHubGetIssueCommentParams) (mcpdomain.GitHubIssueComment, error) {
	g.getIssueCommentCalls++
	if g.getIssueCommentFunc == nil {
		return mcpdomain.GitHubIssueComment{}, nil
	}
	return g.getIssueCommentFunc(ctx, params)
}

func (g *runstatusTestGitHub) CreateIssueComment(ctx context.Context, params mcpdomain.GitHubCreateIssueCommentParams) (mcpdomain.GitHubIssueComment, error) {
	if g.createIssueCommentFunc == nil {
		return mcpdomain.GitHubIssueComment{}, nil
	}
	return g.createIssueCommentFunc(ctx, params)
}

func (g *runstatusTestGitHub) EditIssueComment(ctx context.Context, params mcpdomain.GitHubEditIssueCommentParams) (mcpdomain.GitHubIssueComment, error) {
	g.editIssueCommentCalls++
	g.lastEditedBody = params.Body
	if g.editIssueCommentFunc == nil {
		return mcpdomain.GitHubIssueComment{}, nil
	}
	return g.editIssueCommentFunc(ctx, params)
}

func (g *runstatusTestGitHub) DeleteIssueComment(context.Context, mcpdomain.GitHubDeleteIssueCommentParams) error {
	return nil
}

func (g *runstatusTestGitHub) ListIssueReactions(context.Context, mcpdomain.GitHubListIssueReactionsParams) ([]mcpdomain.GitHubIssueReaction, error) {
	return nil, nil
}

func (g *runstatusTestGitHub) CreateIssueReaction(context.Context, mcpdomain.GitHubCreateIssueReactionParams) (mcpdomain.GitHubIssueReaction, error) {
	return mcpdomain.GitHubIssueReaction{}, nil
}

func (g *runstatusTestGitHub) ListIssueLabels(context.Context, mcpdomain.GitHubListIssueLabelsParams) ([]mcpdomain.GitHubLabel, error) {
	return nil, nil
}

func (g *runstatusTestGitHub) AddLabels(context.Context, mcpdomain.GitHubMutateLabelsParams) ([]mcpdomain.GitHubLabel, error) {
	return nil, nil
}
