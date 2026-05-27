package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providergithub "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/github"
)

const githubBootstrapMergedFixturePath = "../../../../../../fixtures/provider-webhooks/github_pull_request_bootstrap_merged.json"

func TestSmokeFixtureGitHubBootstrapMergeSignalPath(t *testing.T) {
	t.Parallel()

	fixture, err := os.ReadFile(githubBootstrapMergedFixturePath)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	projectID := uuid.MustParse("00000000-0000-4000-8000-000000000001")
	repositoryID := uuid.MustParse("00000000-0000-4000-8000-000000000002")
	operationID := uuid.MustParse("00000000-0000-4000-8000-000000000088")
	workItemProviderID := "github:kodex-smoke/repository:pull_request:88"
	workItemID := stableUUID("work-item", string(enum.ProviderSlugGitHub), workItemProviderID)
	now := time.Date(2026, 5, 27, 12, 35, 0, 0, time.UTC)
	mergedAt := time.Date(2026, 5, 27, 12, 34, 56, 0, time.UTC)
	repository := &fakeRepository{
		workItemProjection: entity.ProviderWorkItemProjection{
			Base:               entity.Base{ID: workItemID, Version: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
			ProviderSlug:       enum.ProviderSlugGitHub,
			ProviderWorkItemID: workItemProviderID,
			ProjectID:          &projectID,
			RepositoryID:       &repositoryID,
			RepositoryFullName: "kodex-smoke/repository",
			Kind:               enum.WorkItemKindPullRequest,
			Number:             88,
			URL:                "https://example.invalid/kodex-smoke/repository/pull/88",
			State:              "open",
			WorkItemType:       "bootstrap",
			WatermarkStatus:    enum.WorkItemWatermarkStatusValid,
			WatermarkJSON:      []byte(`{"kind":"pull_request","managed_by":"kodex","provider_operation_ref":"provider-hub:operation:00000000-0000-4000-8000-000000000088","source_ref":"kodex/bootstrap","work_type":"repository_bootstrap"}`),
			SyncedAt:           now.Add(-time.Hour),
			DriftStatus:        enum.WorkItemDriftStatusFresh,
		},
		relationship: entity.ProviderRelationship{
			ID:                stableUUID("relationship", workItemID.String(), relationshipProjectRepositoryBinding, "project-catalog:project:"+projectID.String()+":repository:"+repositoryID.String()),
			SourceWorkItemID:  workItemID,
			TargetProviderRef: "project-catalog:project:" + projectID.String() + ":repository:" + repositoryID.String(),
			RelationshipType:  relationshipProjectRepositoryBinding,
			Source:            enum.RelationshipSourceManual,
			Confidence:        enum.RelationshipConfidenceConfirmed,
			CreatedAt:         now.Add(-time.Hour),
		},
		recordedProviderOperation: entity.ProviderOperation{
			Base:               entity.Base{ID: operationID, Version: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
			ProviderSlug:       enum.ProviderSlugGitHub,
			OperationType:      enum.ProviderOperationCreateBootstrapPullRequest,
			TargetRef:          repositoryTargetRef(enum.ProviderSlugGitHub, repositoryID.String()) + "#bootstrap_pull_request:kodex/bootstrap",
			Status:             enum.ProviderOperationStatusSucceeded,
			ResultRef:          "https://example.invalid/kodex-smoke/repository/pull/88",
			ProviderObjectID:   workItemProviderID,
			RepositoryFullName: "kodex-smoke/repository",
			StartedAt:          now.Add(-time.Hour),
		},
	}
	service := NewWithRuntime(
		repository,
		fixedClock{now: now},
		&sequenceIDs{ids: []uuid.UUID{
			uuid.MustParse("00000000-0000-4000-8000-000000000101"),
			uuid.MustParse("00000000-0000-4000-8000-000000000102"),
			uuid.MustParse("00000000-0000-4000-8000-000000000103"),
			uuid.MustParse("00000000-0000-4000-8000-000000000104"),
			uuid.MustParse("00000000-0000-4000-8000-000000000105"),
			uuid.MustParse("00000000-0000-4000-8000-000000000106"),
			uuid.MustParse("00000000-0000-4000-8000-000000000107"),
		}},
		providergithub.New(providergithub.Config{}),
	)

	webhook, err := service.IngestWebhookEvent(context.Background(), IngestWebhookEventInput{
		ProviderSlug:         enum.ProviderSlugGitHub,
		DeliveryID:           "smoke-bootstrap-merged",
		EventName:            "pull_request",
		RepositoryProviderID: "9001001",
		ReceivedAt:           now,
		PayloadJSON:          fixture,
		Meta: value.CommandMeta{
			CommandID: uuid.MustParse("00000000-0000-4000-8000-000000000201"),
			Actor:     value.Actor{Type: "service", ID: "smoke-provider-merge-signal"},
		},
	})
	if err != nil {
		t.Fatalf("IngestWebhookEvent(): %v", err)
	}
	if webhook.ProcessingStatus != enum.WebhookProcessingStatusProcessed {
		t.Fatalf("webhook status = %s, want processed", webhook.ProcessingStatus)
	}
	signalKey := "provider:github:repository_merge:bootstrap:" + workItemProviderID
	readResult, err := service.GetRepositoryMergeSignal(context.Background(), GetRepositoryMergeSignalInput{SignalKey: signalKey})
	if err != nil {
		t.Fatalf("GetRepositoryMergeSignal(): %v", err)
	}
	if readResult.Status != enum.ProviderOwnedDataStatusReady || readResult.MergeSignal == nil {
		t.Fatalf("read result = %+v, want ready merge signal", readResult)
	}
	signal := readResult.MergeSignal
	if signal.SignalKey != signalKey ||
		signal.Kind != enum.RepositoryMergeSignalKindBootstrap ||
		signal.ProviderSlug != enum.ProviderSlugGitHub ||
		signal.RepositoryFullName != "kodex-smoke/repository" ||
		signal.PullRequestNumber != 88 ||
		signal.BaseBranch != "main" ||
		signal.HeadBranch != "kodex/bootstrap" ||
		signal.MergeCommitSHA != "0123456789abcdef0123456789abcdef01234567" ||
		signal.SourceRef != "kodex/bootstrap" ||
		signal.RelatedProviderOperationRef != "provider-hub:operation:"+operationID.String() ||
		signal.Status != enum.RepositoryMergeSignalStatusMerged ||
		!signal.MergedAt.Equal(mergedAt) ||
		signal.Version != 1 {
		t.Fatalf("merge signal = %+v, want provider-owned smoke refs", signal)
	}
	if signal.ProjectID == nil || *signal.ProjectID != projectID || signal.RepositoryID == nil || *signal.RepositoryID != repositoryID {
		t.Fatalf("project/repository refs = %v/%v, want smoke refs", signal.ProjectID, signal.RepositoryID)
	}
	var mergeOutbox entity.OutboxEvent
	for _, event := range repository.recordedOutboxEvents {
		if event.EventType == providerEventRepositoryBootstrapMerged {
			mergeOutbox = event
			break
		}
	}
	if mergeOutbox.ID == uuid.Nil || mergeOutbox.AggregateType != providerAggregateRepositoryMergeSignal || mergeOutbox.AggregateID != signal.ID {
		t.Fatalf("merge outbox = %+v, want repository merge signal outbox event", mergeOutbox)
	}
	var payload value.ProviderEventPayload
	if err := json.Unmarshal(mergeOutbox.Payload, &payload); err != nil {
		t.Fatalf("unmarshal merge outbox payload: %v", err)
	}
	if payload.SignalKey != signalKey ||
		payload.SignalKind != string(enum.RepositoryMergeSignalKindBootstrap) ||
		payload.RepositoryFullName != "kodex-smoke/repository" ||
		payload.BaseBranch != "main" ||
		payload.HeadBranch != "kodex/bootstrap" ||
		payload.MergeCommitSHA != "0123456789abcdef0123456789abcdef01234567" ||
		payload.Status != string(enum.RepositoryMergeSignalStatusMerged) ||
		payload.Version != 1 {
		t.Fatalf("merge outbox payload = %+v, want safe provider-owned fields", payload)
	}
}
