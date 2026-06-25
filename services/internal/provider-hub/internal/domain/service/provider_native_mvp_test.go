package service

import (
	"context"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/libs/go/secretresolver"
	providerrepo "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/repository/provider"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providerclient "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/client"
	providergithub "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/github"
)

const (
	githubIssueOpenedFixturePath        = "../../../../../../fixtures/provider-webhooks/github_issues_opened.json"
	githubPullRequestOpenedFixturePath  = "../../../../../../fixtures/provider-webhooks/github_pull_request_opened.json"
	githubIssueCommentFixturePath       = "../../../../../../fixtures/provider-webhooks/github_issue_comment_created.json"
	githubPullRequestReviewFixturePath  = "../../../../../../fixtures/provider-webhooks/github_pull_request_review_submitted.json"
	githubPushProviderNativeFixturePath = "../../../../../../fixtures/provider-webhooks/github_push_provider_native_change.json"
)

type providerNativeMVPWebhookCase struct {
	name                  string
	fixturePath           string
	eventName             string
	deliveryID            string
	rawSentinel           string
	expectedProviderEvent string
	assertProjection      func(*testing.T, providerrepo.ProjectionUpdate)
}

func TestProviderNativeMVPFixturesIngestSafeGitHubArtifacts(t *testing.T) {
	t.Parallel()

	for _, tc := range providerNativeMVPWebhookCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fixture := readProviderNativeMVPFixture(t, tc.fixturePath)
			now := time.Date(2026, 6, 19, 10, 10, 0, 0, time.UTC)
			repository := &fakeRepository{}
			service := NewWithRuntime(
				repository,
				fixedClock{now: now},
				providerNativeMVPSequenceIDs(tc.name),
				providergithub.New(providergithub.Config{}),
			)

			webhook, err := service.IngestWebhookEvent(context.Background(), IngestWebhookEventInput{
				ProviderSlug:         enum.ProviderSlugGitHub,
				DeliveryID:           tc.deliveryID,
				EventName:            tc.eventName,
				RepositoryProviderID: "9101001",
				ReceivedAt:           now,
				PayloadJSON:          fixture,
				Meta: value.CommandMeta{
					CommandID: stableUUID("provider-native-mvp-command", tc.name),
					Actor:     value.Actor{Type: "service", ID: "provider-native-mvp-test"},
				},
			})
			if err != nil {
				t.Fatalf("IngestWebhookEvent(): %v", err)
			}
			if webhook.ProcessingStatus != enum.WebhookProcessingStatusProcessed {
				t.Fatalf("webhook status = %s, want processed", webhook.ProcessingStatus)
			}
			tc.assertProjection(t, repository.recordedProjection)
			assertProviderNativeMVPSafeOutputs(t, tc, fixture, repository)
		})
	}
}

func TestProviderNativeMVPWriteOperationsReachSharedPipeline(t *testing.T) {
	t.Parallel()

	target := ProviderTarget{
		ProviderSlug:       enum.ProviderSlugGitHub,
		RepositoryFullName: "kodex-smoke/provider-native-work",
		WorkItemKind:       enum.WorkItemKindPullRequest,
		Number:             702,
	}
	repositoryTarget := ProviderTarget{
		ProviderSlug:       enum.ProviderSlugGitHub,
		RepositoryFullName: "kodex-smoke/provider-native-work",
	}
	projectID := uuid.MustParse("00000000-0000-4000-8000-000000001100")
	repositoryID := uuid.MustParse("00000000-0000-4000-8000-000000001101")

	cases := []struct {
		name             string
		operationType    enum.ProviderOperationType
		resultRef        string
		call             func(*Service, uuid.UUID, uuid.UUID) (ProviderOperationResult, error)
		assertExecutor   func(*testing.T, providerclient.WriteRequest)
		expectedRisk     value.ProviderOperationRiskLevel
		expectedFieldSet []string
	}{
		{
			name:          "create_comment",
			operationType: enum.ProviderOperationCreateComment,
			resultRef:     "https://example.invalid/kodex-smoke/provider-native-work/pull/702#issuecomment-9101903",
			call: func(service *Service, externalAccountID uuid.UUID, commandID uuid.UUID) (ProviderOperationResult, error) {
				return service.CreateComment(context.Background(), CreateCommentInput{
					Target:            target,
					Body:              "  Bounded provider-native progress comment.  ",
					ExternalAccountID: externalAccountID,
					Meta:              providerNativeMVPCommandMeta(commandID, value.ProviderOperationRiskLevelLow, "body"),
				})
			},
			assertExecutor: func(t *testing.T, request providerclient.WriteRequest) {
				t.Helper()
				if request.CreateComment == nil ||
					request.CreateComment.Body != "Bounded provider-native progress comment." ||
					request.CreateComment.Target.Number != 702 {
					t.Fatalf("executor request = %+v, want create comment payload", request)
				}
			},
			expectedRisk:     value.ProviderOperationRiskLevelLow,
			expectedFieldSet: []string{"body"},
		},
		{
			name:          "create_pull_request",
			operationType: enum.ProviderOperationCreatePullRequest,
			resultRef:     "https://example.invalid/kodex-smoke/provider-native-work/pull/703",
			call: func(service *Service, externalAccountID uuid.UUID, commandID uuid.UUID) (ProviderOperationResult, error) {
				return service.CreatePullRequest(context.Background(), CreatePullRequestInput{
					ProjectID:         projectID,
					RepositoryID:      repositoryID,
					ProviderSlug:      enum.ProviderSlugGitHub,
					RepositoryTarget:  repositoryTarget,
					Title:             "  Implement provider-native follow-up  ",
					Body:              "  Bounded pull request body.  ",
					HeadBranch:        "kodex/provider-native-follow-up",
					BaseBranch:        "main",
					Labels:            []string{"type:dev"},
					ExternalAccountID: externalAccountID,
					Meta: providerNativeMVPCommandMeta(
						commandID,
						value.ProviderOperationRiskLevelMedium,
						"title",
						"body",
						"head_branch",
						"base_branch",
						"draft",
						"labels",
					),
				})
			},
			assertExecutor: func(t *testing.T, request providerclient.WriteRequest) {
				t.Helper()
				if request.CreatePullRequest == nil ||
					request.CreatePullRequest.Title != "Implement provider-native follow-up" ||
					request.CreatePullRequest.HeadBranch != "kodex/provider-native-follow-up" ||
					request.CreatePullRequest.BaseBranch != "main" {
					t.Fatalf("executor request = %+v, want create pull request payload", request)
				}
			},
			expectedRisk:     value.ProviderOperationRiskLevelMedium,
			expectedFieldSet: []string{"base_branch", "body", "draft", "head_branch", "labels", "title"},
		},
		{
			name:          "create_review_signal",
			operationType: enum.ProviderOperationCreateReviewSignal,
			resultRef:     "https://example.invalid/kodex-smoke/provider-native-work/pull/702#pullrequestreview-9101904",
			call: func(service *Service, externalAccountID uuid.UUID, commandID uuid.UUID) (ProviderOperationResult, error) {
				return service.CreateReviewSignal(context.Background(), CreateReviewSignalInput{
					Target:            target,
					Kind:              enum.ReviewSignalKindApproval,
					Body:              "  Provider-native review approved.  ",
					ExternalAccountID: externalAccountID,
					Meta:              providerNativeMVPCommandMeta(commandID, value.ProviderOperationRiskLevelMedium, "kind", "body"),
				})
			},
			assertExecutor: func(t *testing.T, request providerclient.WriteRequest) {
				t.Helper()
				if request.CreateReviewSignal == nil ||
					request.CreateReviewSignal.Kind != providerclient.ReviewSignalKindApproval ||
					request.CreateReviewSignal.Body != "Provider-native review approved." ||
					request.CreateReviewSignal.Target.Number != 702 {
					t.Fatalf("executor request = %+v, want create review signal payload", request)
				}
			},
			expectedRisk:     value.ProviderOperationRiskLevelMedium,
			expectedFieldSet: []string{"body", "kind"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, 6, 19, 10, 20, 0, 0, time.UTC)
			operationID := stableUUID("provider-native-mvp-operation", tc.name)
			operationOutboxID := stableUUID("provider-native-mvp-operation-outbox", tc.name)
			externalAccountID := stableUUID("provider-native-mvp-account", tc.name)
			commandID := stableUUID("provider-native-mvp-write-command", tc.name)
			secretResolver := &fakeSecretResolver{secret: secretresolver.NewSecretValue([]byte("write-token"))}
			executor := &fakeWriteExecutor{result: providerclient.WriteResult{ResultRef: tc.resultRef}}
			repository := &fakeRepository{}
			service := NewWithDependencies(Dependencies{
				Repository:             repository,
				Clock:                  fixedClock{now: now},
				IDGenerator:            &sequenceIDs{ids: []uuid.UUID{operationID, operationOutboxID}},
				AccountUsageResolver:   fakeAccountUsageResolver{},
				SecretResolver:         secretResolver,
				ProviderWriteExecutors: []providerclient.WriteExecutor{executor},
			})

			result, err := tc.call(service, externalAccountID, commandID)
			if err != nil {
				t.Fatalf("%s(): %v", tc.name, err)
			}
			if secretResolver.calls != 1 || executor.calls != 1 {
				t.Fatalf("secret calls = %d executor calls = %d, want one provider write", secretResolver.calls, executor.calls)
			}
			tc.assertExecutor(t, executor.request)
			if repository.recordedProviderOperation.ID != operationID ||
				repository.recordedProviderOperation.OperationType != tc.operationType ||
				repository.recordedProviderOperation.Status != enum.ProviderOperationStatusSucceeded ||
				repository.recordedProviderOperation.ResultRef != tc.resultRef ||
				repository.recordedProviderOperation.OperationPolicyContext.RiskLevel != tc.expectedRisk {
				t.Fatalf("operation = %+v, want succeeded %s", repository.recordedProviderOperation, tc.operationType)
			}
			if !sameStringSet(repository.recordedProviderOperation.OperationPolicyContext.ChangedFields, tc.expectedFieldSet) {
				t.Fatalf("changed fields = %+v, want %+v", repository.recordedProviderOperation.OperationPolicyContext.ChangedFields, tc.expectedFieldSet)
			}
			if len(repository.recordedOutboxEvents) != 1 || repository.recordedOutboxEvents[0].EventType != providerEventOperationCompleted {
				t.Fatalf("outbox = %+v, want completed operation event", repository.recordedOutboxEvents)
			}
			if result.ProviderOperation == nil || result.ProviderOperation.ID != operationID || result.Result.ResultRef != tc.resultRef {
				t.Fatalf("result = %+v, want safe provider operation result", result)
			}
		})
	}
}

func providerNativeMVPWebhookCases() []providerNativeMVPWebhookCase {
	return []providerNativeMVPWebhookCase{
		{
			name:                  "issue",
			fixturePath:           githubIssueOpenedFixturePath,
			eventName:             "issues",
			deliveryID:            "provider-native-issue-opened",
			rawSentinel:           "RAW_WEBHOOK_ONLY_DO_NOT_LEAK_ISSUE",
			expectedProviderEvent: providerEventWorkItemSynced,
			assertProjection: func(t *testing.T, projection providerrepo.ProjectionUpdate) {
				t.Helper()
				assertProviderNativeMVPWorkItem(t, projection.WorkItem, enum.WorkItemKindIssue, "github:kodex-smoke/provider-native-work:issue:701", 701)
				if projection.WorkItem.WorkItemType != "dev" || projection.WorkItem.WatermarkStatus != enum.WorkItemWatermarkStatusValid {
					t.Fatalf("work item = %+v, want dev issue with valid watermark", projection.WorkItem)
				}
				if len(projection.Comments) != 0 || projection.ChangeSignal != nil || projection.MergeSignal != nil {
					t.Fatalf("projection = %+v, want only issue work item", projection)
				}
			},
		},
		{
			name:                  "pull_request",
			fixturePath:           githubPullRequestOpenedFixturePath,
			eventName:             "pull_request",
			deliveryID:            "provider-native-pull-request-opened",
			rawSentinel:           "RAW_WEBHOOK_ONLY_DO_NOT_LEAK_PULL_REQUEST",
			expectedProviderEvent: providerEventWorkItemSynced,
			assertProjection: func(t *testing.T, projection providerrepo.ProjectionUpdate) {
				t.Helper()
				assertProviderNativeMVPWorkItem(t, projection.WorkItem, enum.WorkItemKindPullRequest, "github:kodex-smoke/provider-native-work:pull_request:702", 702)
				if projection.WorkItem.WorkItemType != "dev" || projection.WorkItem.WatermarkStatus != enum.WorkItemWatermarkStatusValid {
					t.Fatalf("work item = %+v, want dev pull request with valid watermark", projection.WorkItem)
				}
				if len(projection.Relationships) != 1 || projection.Relationships[0].RelationshipType != relationshipSource {
					t.Fatalf("relationships = %+v, want one source relationship", projection.Relationships)
				}
				if len(projection.Comments) != 0 || projection.ChangeSignal != nil || projection.MergeSignal != nil {
					t.Fatalf("projection = %+v, want only pull request work item and relationship", projection)
				}
			},
		},
		{
			name:                  "issue_comment",
			fixturePath:           githubIssueCommentFixturePath,
			eventName:             "issue_comment",
			deliveryID:            "provider-native-issue-comment-created",
			rawSentinel:           "RAW_WEBHOOK_ONLY_DO_NOT_LEAK_COMMENT",
			expectedProviderEvent: providerEventCommentSynced,
			assertProjection: func(t *testing.T, projection providerrepo.ProjectionUpdate) {
				t.Helper()
				assertProviderNativeMVPWorkItem(t, projection.WorkItem, enum.WorkItemKindIssue, "github:kodex-smoke/provider-native-work:issue:701", 701)
				if len(projection.Comments) != 1 {
					t.Fatalf("comments = %+v, want one comment projection", projection.Comments)
				}
				comment := projection.Comments[0]
				if comment.ProviderCommentID != "9101903" ||
					comment.Kind != enum.CommentKindComment ||
					comment.ReviewState != "" ||
					comment.Summary != "Bounded provider-native progress comment." ||
					comment.BodyDigest == "" {
					t.Fatalf("comment = %+v, want safe comment projection", comment)
				}
			},
		},
		{
			name:                  "pull_request_review",
			fixturePath:           githubPullRequestReviewFixturePath,
			eventName:             "pull_request_review",
			deliveryID:            "provider-native-pull-request-review",
			rawSentinel:           "RAW_WEBHOOK_ONLY_DO_NOT_LEAK_REVIEW",
			expectedProviderEvent: providerEventCommentSynced,
			assertProjection: func(t *testing.T, projection providerrepo.ProjectionUpdate) {
				t.Helper()
				assertProviderNativeMVPWorkItem(t, projection.WorkItem, enum.WorkItemKindPullRequest, "github:kodex-smoke/provider-native-work:pull_request:702", 702)
				if len(projection.Comments) != 1 {
					t.Fatalf("comments = %+v, want one review projection", projection.Comments)
				}
				review := projection.Comments[0]
				if review.ProviderCommentID != "9101904" ||
					review.Kind != enum.CommentKindReview ||
					review.ReviewState != enum.ReviewStateApproved ||
					review.Summary != "Provider-native review approved." ||
					review.BodyDigest == "" {
					t.Fatalf("review = %+v, want safe approved review projection", review)
				}
			},
		},
		{
			name:                  "push",
			fixturePath:           githubPushProviderNativeFixturePath,
			eventName:             "push",
			deliveryID:            "provider-native-push-main",
			rawSentinel:           "RAW_WEBHOOK_ONLY_DO_NOT_LEAK_PUSH",
			expectedProviderEvent: providerEventRepositoryChanged,
			assertProjection: func(t *testing.T, projection providerrepo.ProjectionUpdate) {
				t.Helper()
				if projection.WorkItem != nil || len(projection.Comments) != 0 || projection.MergeSignal != nil {
					t.Fatalf("projection = %+v, want repository change only", projection)
				}
				signal := projection.ChangeSignal
				if signal == nil {
					t.Fatal("change signal is nil, want provider-owned repository change signal")
				}
				if signal.Kind != enum.RepositoryChangeSignalKindPush ||
					signal.RepositoryFullName != "kodex-smoke/provider-native-work" ||
					signal.ProviderRepositoryID != "9101001" ||
					signal.BaseBranch != "main" ||
					signal.CommitSHA != "2222222222222222222222222222222222222222" ||
					signal.PathSummaryStatus != enum.RepositoryChangePathSummaryStatusReady ||
					signal.ChangedPathCount != 3 ||
					!signal.DeployRelevantChanged ||
					!strings.HasPrefix(signal.PathDigest, "sha256:") ||
					!strings.HasPrefix(signal.ChangeFingerprint, "sha256:") {
					t.Fatalf("change signal = %+v, want safe repository refs and path summary", signal)
				}
				if len(signal.PathCategories) != 3 {
					t.Fatalf("path categories = %+v, want three bounded category counters", signal.PathCategories)
				}
			},
		},
	}
}

func assertProviderNativeMVPWorkItem(t *testing.T, workItem *entity.ProviderWorkItemProjection, kind enum.WorkItemKind, providerID string, number int64) {
	t.Helper()
	if workItem == nil {
		t.Fatal("work item projection is nil")
	}
	if workItem.ProviderSlug != enum.ProviderSlugGitHub ||
		workItem.ProviderWorkItemID != providerID ||
		workItem.RepositoryFullName != "kodex-smoke/provider-native-work" ||
		workItem.Kind != kind ||
		workItem.Number != number ||
		workItem.BodyDigest == "" ||
		workItem.DriftStatus != enum.WorkItemDriftStatusFresh {
		t.Fatalf("work item = %+v, want safe GitHub %s projection", workItem, kind)
	}
}

func assertProviderNativeMVPSafeOutputs(t *testing.T, tc providerNativeMVPWebhookCase, fixture []byte, repository *fakeRepository) {
	t.Helper()
	if !strings.Contains(string(repository.recordedWebhook.PayloadJSON), string(value.WebhookPayloadStorageSafeEnvelope)) ||
		repository.recordedWebhook.PayloadDigest == "" {
		t.Fatalf("webhook inbox payload = %s digest = %q, want safe envelope with digest", repository.recordedWebhook.PayloadJSON, repository.recordedWebhook.PayloadDigest)
	}
	forbidden := []string{tc.rawSentinel, string(fixture), "ssh_url", "RAW_WEBHOOK_ONLY_DO_NOT_LEAK"}
	checkBytesDoNotContain(t, "webhook inbox payload", repository.recordedWebhook.PayloadJSON, forbidden)
	checkJSONDoesNotContain(t, "provider projection", repository.recordedProjection, forbidden)

	if len(repository.recordedProviderEvents) != 1 || repository.recordedProviderEvents[0].EventType != tc.expectedProviderEvent {
		t.Fatalf("provider events = %+v, want one %s event", repository.recordedProviderEvents, tc.expectedProviderEvent)
	}
	for _, event := range repository.recordedProviderEvents {
		checkBytesDoNotContain(t, "provider event payload", event.PayloadJSON, forbidden)
	}
	for _, event := range repository.recordedOutboxEvents {
		checkBytesDoNotContain(t, "outbox payload "+event.EventType, event.Payload, forbidden)
	}
	assertProviderNativeMVPOutboxShape(t, tc, repository.recordedOutboxEvents)
}

func assertProviderNativeMVPOutboxShape(t *testing.T, tc providerNativeMVPWebhookCase, events []entity.OutboxEvent) {
	t.Helper()
	if findOutboxEvent(events, providerEventWebhookReceived).ID == uuid.Nil ||
		findOutboxEvent(events, providerEventWebhookNormalized).ID == uuid.Nil {
		t.Fatalf("outbox events = %+v, want received and normalized events", events)
	}
	if findOutboxEvent(events, tc.expectedProviderEvent).ID == uuid.Nil {
		t.Fatalf("outbox events = %+v, want %s", events, tc.expectedProviderEvent)
	}
	for _, event := range events {
		var payload value.ProviderEventPayload
		if err := json.Unmarshal(event.Payload, &payload); err != nil {
			t.Fatalf("unmarshal outbox payload %s: %v", event.EventType, err)
		}
		if payload.ProviderSlug != string(enum.ProviderSlugGitHub) ||
			payload.RepositoryFullName == "" && event.EventType != providerEventWebhookReceived {
			t.Fatalf("outbox payload = %+v, want safe provider refs", payload)
		}
	}
}

func readProviderNativeMVPFixture(t *testing.T, path string) []byte {
	t.Helper()
	fixture, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return fixture
}

func providerNativeMVPSequenceIDs(name string) *sequenceIDs {
	ids := make([]uuid.UUID, 0, 32)
	for i := 0; i < 32; i++ {
		ids = append(ids, stableUUID("provider-native-mvp", name, strconv.Itoa(i)))
	}
	return &sequenceIDs{ids: ids}
}

func providerNativeMVPCommandMeta(commandID uuid.UUID, risk value.ProviderOperationRiskLevel, changedFields ...string) value.CommandMeta {
	return value.CommandMeta{
		CommandID: commandID,
		Actor:     value.Actor{Type: "agent", ID: "provider-native-mvp"},
		OperationPolicyContext: value.ProviderOperationPolicyContext{
			RoleKey:       "agent-manager",
			RiskLevel:     risk,
			ChangedFields: changedFields,
			PolicyVersion: "2026-06-19",
		},
	}
}

func sameStringSet(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	seen := make(map[string]int, len(left))
	for _, item := range left {
		seen[item]++
	}
	for _, item := range right {
		seen[item]--
		if seen[item] < 0 {
			return false
		}
	}
	return true
}
