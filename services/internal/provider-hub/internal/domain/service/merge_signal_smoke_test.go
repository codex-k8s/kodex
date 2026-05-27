package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/provider-hub/internal/domain/types/value"
	providergithub "github.com/codex-k8s/kodex/services/internal/provider-hub/internal/provider/github"
)

const (
	githubBootstrapMergedFixturePath = "../../../../../../fixtures/provider-webhooks/github_pull_request_bootstrap_merged.json"
	githubAdoptionMergedFixturePath  = "../../../../../../fixtures/provider-webhooks/github_pull_request_adoption_merged.json"
)

type mergeSignalSmokeCase struct {
	name                  string
	fixturePath           string
	deliveryID            string
	projectID             uuid.UUID
	repositoryID          uuid.UUID
	operationID           uuid.UUID
	repositoryFullName    string
	repositoryProviderID  string
	workItemProviderID    string
	workItemURL           string
	kind                  enum.RepositoryMergeSignalKind
	operationType         enum.ProviderOperationType
	operationTargetKind   string
	pullRequestNumber     int64
	pullRequestProviderID string
	baseBranch            string
	headBranch            string
	mergeCommitSHA        string
	mergedAt              time.Time
	rawSentinel           string
	expectedEventType     string
}

func TestSmokeFixtureGitHubMergeSignalPath(t *testing.T) {
	t.Parallel()

	for _, tc := range mergeSignalSmokeCases() {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fixture := readMergeSignalFixture(t, tc.fixturePath)
			service, repository := newMergeSignalSmokeService(tc, time.Date(2026, 5, 27, 12, 55, 0, 0, time.UTC), false)

			webhook, err := service.IngestWebhookEvent(context.Background(), IngestWebhookEventInput{
				ProviderSlug:         enum.ProviderSlugGitHub,
				DeliveryID:           tc.deliveryID,
				EventName:            "pull_request",
				RepositoryProviderID: tc.repositoryProviderID,
				ReceivedAt:           time.Date(2026, 5, 27, 12, 55, 0, 0, time.UTC),
				PayloadJSON:          fixture,
				Meta: value.CommandMeta{
					CommandID: stableUUID("merge-signal-smoke-command", tc.name),
					Actor:     value.Actor{Type: "service", ID: "smoke-provider-merge-signal"},
				},
			})
			if err != nil {
				t.Fatalf("IngestWebhookEvent(): %v", err)
			}
			if webhook.ProcessingStatus != enum.WebhookProcessingStatusProcessed {
				t.Fatalf("webhook status = %s, want processed", webhook.ProcessingStatus)
			}

			signalKey := repositoryMergeSignalKey(enum.ProviderSlugGitHub, tc.kind, tc.workItemProviderID)
			readResult, err := service.GetRepositoryMergeSignal(context.Background(), GetRepositoryMergeSignalInput{SignalKey: signalKey})
			if err != nil {
				t.Fatalf("GetRepositoryMergeSignal(): %v", err)
			}
			assertMergeSignalSmokeResult(t, tc, signalKey, readResult)
			assertSafeMergeSignalOutputsDoNotLeakFixture(t, tc, fixture, readResult, repository)
		})
	}
}

func TestSmokeFixtureGitHubMergeSignalReplayAndConflictDiagnostics(t *testing.T) {
	t.Parallel()

	tc := mergeSignalSmokeCases()[1]
	fixture := readMergeSignalFixture(t, tc.fixturePath)
	service, repository := newMergeSignalSmokeService(tc, time.Date(2026, 5, 27, 13, 0, 0, 0, time.UTC), true)

	for _, deliveryID := range []string{"smoke-adoption-merged-replay-1", "smoke-adoption-merged-replay-2"} {
		_, err := service.IngestWebhookEvent(context.Background(), IngestWebhookEventInput{
			ProviderSlug:         enum.ProviderSlugGitHub,
			DeliveryID:           deliveryID,
			EventName:            "pull_request",
			RepositoryProviderID: tc.repositoryProviderID,
			ReceivedAt:           time.Date(2026, 5, 27, 13, 0, 0, 0, time.UTC),
			PayloadJSON:          fixture,
			Meta:                 value.CommandMeta{CommandID: stableUUID("merge-signal-smoke-replay", deliveryID)},
		})
		if err != nil {
			t.Fatalf("IngestWebhookEvent(%s): %v", deliveryID, err)
		}
	}
	if count := repository.outboxEventCount(tc.expectedEventType); count != 1 {
		t.Fatalf("merge outbox count = %d, want 1 after replay", count)
	}

	conflictingFixture := withMergeCommitSHA(t, fixture, "1111111111111111111111111111111111111111")
	_, err := service.IngestWebhookEvent(context.Background(), IngestWebhookEventInput{
		ProviderSlug:         enum.ProviderSlugGitHub,
		DeliveryID:           "smoke-adoption-merged-conflict",
		EventName:            "pull_request",
		RepositoryProviderID: tc.repositoryProviderID,
		ReceivedAt:           time.Date(2026, 5, 27, 13, 0, 0, 0, time.UTC),
		PayloadJSON:          conflictingFixture,
		Meta:                 value.CommandMeta{CommandID: stableUUID("merge-signal-smoke-conflict", tc.name)},
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("conflicting IngestWebhookEvent() err = %v, want %v", err, errs.ErrConflict)
	}
	for _, forbidden := range []string{tc.rawSentinel, string(bytes.TrimSpace(conflictingFixture))} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("conflict diagnostic leaked raw payload: %q", err.Error())
		}
	}
}

func mergeSignalSmokeCases() []mergeSignalSmokeCase {
	return []mergeSignalSmokeCase{
		{
			name:                  "bootstrap",
			fixturePath:           githubBootstrapMergedFixturePath,
			deliveryID:            "smoke-bootstrap-merged",
			projectID:             uuid.MustParse("00000000-0000-4000-8000-000000000001"),
			repositoryID:          uuid.MustParse("00000000-0000-4000-8000-000000000002"),
			operationID:           uuid.MustParse("00000000-0000-4000-8000-000000000088"),
			repositoryFullName:    "kodex-smoke/repository",
			repositoryProviderID:  "9001001",
			workItemProviderID:    "github:kodex-smoke/repository:pull_request:88",
			workItemURL:           "https://example.invalid/kodex-smoke/repository/pull/88",
			kind:                  enum.RepositoryMergeSignalKindBootstrap,
			operationType:         enum.ProviderOperationCreateBootstrapPullRequest,
			operationTargetKind:   "bootstrap_pull_request",
			pullRequestNumber:     88,
			pullRequestProviderID: "900188",
			baseBranch:            "main",
			headBranch:            "kodex/bootstrap",
			mergeCommitSHA:        "0123456789abcdef0123456789abcdef01234567",
			mergedAt:              time.Date(2026, 5, 27, 12, 34, 56, 0, time.UTC),
			rawSentinel:           "RAW_PROVIDER_PAYLOAD_DO_NOT_LEAK_BOOTSTRAP",
			expectedEventType:     providerEventRepositoryBootstrapMerged,
		},
		{
			name:                  "adoption",
			fixturePath:           githubAdoptionMergedFixturePath,
			deliveryID:            "smoke-adoption-merged",
			projectID:             uuid.MustParse("00000000-0000-4000-8000-000000000011"),
			repositoryID:          uuid.MustParse("00000000-0000-4000-8000-000000000012"),
			operationID:           uuid.MustParse("00000000-0000-4000-8000-000000000089"),
			repositoryFullName:    "kodex-smoke/existing-repository",
			repositoryProviderID:  "9002001",
			workItemProviderID:    "github:kodex-smoke/existing-repository:pull_request:89",
			workItemURL:           "https://example.invalid/kodex-smoke/existing-repository/pull/89",
			kind:                  enum.RepositoryMergeSignalKindAdoption,
			operationType:         enum.ProviderOperationCreateAdoptionPullRequest,
			operationTargetKind:   "adoption_pull_request",
			pullRequestNumber:     89,
			pullRequestProviderID: "900289",
			baseBranch:            "main",
			headBranch:            "kodex/adoption",
			mergeCommitSHA:        "fedcba9876543210fedcba9876543210fedcba98",
			mergedAt:              time.Date(2026, 5, 27, 12, 44, 56, 0, time.UTC),
			rawSentinel:           "RAW_PROVIDER_PAYLOAD_DO_NOT_LEAK_ADOPTION",
			expectedEventType:     providerEventRepositoryAdoptionMerged,
		},
	}
}

func newMergeSignalSmokeService(tc mergeSignalSmokeCase, now time.Time, enforceSignalIdempotency bool) (*Service, *fakeRepository) {
	workItemID := stableUUID("work-item", string(enum.ProviderSlugGitHub), tc.workItemProviderID)
	operationRef := "provider-hub:operation:" + tc.operationID.String()
	repository := &fakeRepository{
		enforceMergeSignalIdempotency: enforceSignalIdempotency,
		workItemProjection: entity.ProviderWorkItemProjection{
			Base:               entity.Base{ID: workItemID, Version: 2, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
			ProviderSlug:       enum.ProviderSlugGitHub,
			ProviderWorkItemID: tc.workItemProviderID,
			ProjectID:          &tc.projectID,
			RepositoryID:       &tc.repositoryID,
			RepositoryFullName: tc.repositoryFullName,
			Kind:               enum.WorkItemKindPullRequest,
			Number:             tc.pullRequestNumber,
			URL:                tc.workItemURL,
			State:              "open",
			WorkItemType:       string(tc.kind),
			WatermarkStatus:    enum.WorkItemWatermarkStatusValid,
			WatermarkJSON: []byte(`{"kind":"pull_request","managed_by":"kodex","provider_operation_ref":"` + operationRef +
				`","source_ref":"` + tc.headBranch + `","work_type":"repository_` + string(tc.kind) + `"}`),
			SyncedAt:    now.Add(-time.Hour),
			DriftStatus: enum.WorkItemDriftStatusFresh,
		},
		relationship: entity.ProviderRelationship{
			ID:                stableUUID("relationship", workItemID.String(), relationshipProjectRepositoryBinding, "project-catalog:project:"+tc.projectID.String()+":repository:"+tc.repositoryID.String()),
			SourceWorkItemID:  workItemID,
			TargetProviderRef: "project-catalog:project:" + tc.projectID.String() + ":repository:" + tc.repositoryID.String(),
			RelationshipType:  relationshipProjectRepositoryBinding,
			Source:            enum.RelationshipSourceManual,
			Confidence:        enum.RelationshipConfidenceConfirmed,
			CreatedAt:         now.Add(-time.Hour),
		},
		recordedProviderOperation: entity.ProviderOperation{
			Base:               entity.Base{ID: tc.operationID, Version: 1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
			ProviderSlug:       enum.ProviderSlugGitHub,
			OperationType:      tc.operationType,
			TargetRef:          repositoryTargetRef(enum.ProviderSlugGitHub, tc.repositoryID.String()) + "#" + tc.operationTargetKind + ":" + tc.headBranch,
			Status:             enum.ProviderOperationStatusSucceeded,
			ResultRef:          tc.workItemURL,
			ProviderObjectID:   tc.workItemProviderID,
			RepositoryFullName: tc.repositoryFullName,
			StartedAt:          now.Add(-time.Hour),
		},
	}
	return NewWithRuntime(repository, fixedClock{now: now}, smokeSequenceIDs(tc.name), providergithub.New(providergithub.Config{})), repository
}

func readMergeSignalFixture(t *testing.T, path string) []byte {
	t.Helper()
	fixture, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return fixture
}

func smokeSequenceIDs(name string) *sequenceIDs {
	ids := make([]uuid.UUID, 0, 48)
	for i := 0; i < 48; i++ {
		ids = append(ids, stableUUID("merge-signal-smoke", name, strconv.Itoa(i)))
	}
	return &sequenceIDs{ids: ids}
}

func assertMergeSignalSmokeResult(t *testing.T, tc mergeSignalSmokeCase, signalKey string, readResult RepositoryMergeSignalResult) {
	t.Helper()
	if readResult.Status != enum.ProviderOwnedDataStatusReady || readResult.MergeSignal == nil {
		t.Fatalf("read result = %+v, want ready merge signal", readResult)
	}
	signal := readResult.MergeSignal
	if signal.SignalKey != signalKey ||
		signal.Kind != tc.kind ||
		signal.ProviderSlug != enum.ProviderSlugGitHub ||
		signal.RepositoryFullName != tc.repositoryFullName ||
		signal.ProviderRepositoryID != tc.repositoryProviderID ||
		signal.ProviderWorkItemID != tc.workItemProviderID ||
		signal.PullRequestNumber != tc.pullRequestNumber ||
		signal.PullRequestProviderID != tc.pullRequestProviderID ||
		signal.PullRequestURL != tc.workItemURL ||
		signal.BaseBranch != tc.baseBranch ||
		signal.HeadBranch != tc.headBranch ||
		signal.MergeCommitSHA != tc.mergeCommitSHA ||
		signal.SourceRef != tc.headBranch ||
		signal.RelatedProviderOperationRef != "provider-hub:operation:"+tc.operationID.String() ||
		signal.Status != enum.RepositoryMergeSignalStatusMerged ||
		!signal.MergedAt.Equal(tc.mergedAt) ||
		signal.Version != 1 {
		t.Fatalf("merge signal = %+v, want provider-owned smoke refs", signal)
	}
	if signal.ProjectID == nil || *signal.ProjectID != tc.projectID || signal.RepositoryID == nil || *signal.RepositoryID != tc.repositoryID {
		t.Fatalf("project/repository refs = %v/%v, want smoke refs", signal.ProjectID, signal.RepositoryID)
	}
}

func assertSafeMergeSignalOutputsDoNotLeakFixture(t *testing.T, tc mergeSignalSmokeCase, fixture []byte, readResult RepositoryMergeSignalResult, repository *fakeRepository) {
	t.Helper()
	if !bytes.Contains(repository.recordedWebhook.PayloadJSON, []byte(tc.rawSentinel)) {
		t.Fatalf("webhook inbox payload does not contain fixture sentinel; test cannot prove safe output redaction")
	}
	forbidden := []string{tc.rawSentinel, "payload_json", "\"body\""}
	checkJSONDoesNotContain(t, "merge signal read result", readResult, forbidden)
	for _, event := range repository.recordedProviderEvents {
		checkBytesDoNotContain(t, "provider event payload", event.PayloadJSON, forbidden)
	}
	for _, event := range repository.recordedOutboxEvents {
		checkBytesDoNotContain(t, "outbox payload "+event.EventType, event.Payload, forbidden)
	}

	mergeOutbox := findOutboxEvent(repository.recordedOutboxEvents, tc.expectedEventType)
	if mergeOutbox.ID == uuid.Nil ||
		mergeOutbox.AggregateType != providerAggregateRepositoryMergeSignal ||
		mergeOutbox.AggregateID != readResult.MergeSignal.ID {
		t.Fatalf("merge outbox = %+v, want repository merge signal outbox event", mergeOutbox)
	}
	var payload value.ProviderEventPayload
	if err := json.Unmarshal(mergeOutbox.Payload, &payload); err != nil {
		t.Fatalf("unmarshal merge outbox payload: %v", err)
	}
	if payload.SignalKey != readResult.MergeSignal.SignalKey ||
		payload.SignalKind != string(tc.kind) ||
		payload.RepositoryFullName != tc.repositoryFullName ||
		payload.BaseBranch != tc.baseBranch ||
		payload.HeadBranch != tc.headBranch ||
		payload.MergeCommitSHA != tc.mergeCommitSHA ||
		payload.Status != string(enum.RepositoryMergeSignalStatusMerged) ||
		payload.Version != 1 {
		t.Fatalf("merge outbox payload = %+v, want safe provider-owned fields", payload)
	}
	if bytes.Contains(mergeOutbox.Payload, bytes.TrimSpace(fixture)) {
		t.Fatalf("merge outbox payload leaked canonical webhook fixture")
	}
}

func findOutboxEvent(events []entity.OutboxEvent, eventType string) entity.OutboxEvent {
	for _, event := range events {
		if event.EventType == eventType {
			return event
		}
	}
	return entity.OutboxEvent{}
}

func checkJSONDoesNotContain(t *testing.T, name string, value any, forbidden []string) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %s: %v", name, err)
	}
	checkBytesDoNotContain(t, name, raw, forbidden)
}

func checkBytesDoNotContain(t *testing.T, name string, raw []byte, forbidden []string) {
	t.Helper()
	text := string(raw)
	for _, forbiddenValue := range forbidden {
		if strings.Contains(text, forbiddenValue) {
			t.Fatalf("%s leaked %q: %s", name, forbiddenValue, text)
		}
	}
}

func withMergeCommitSHA(t *testing.T, fixture []byte, mergeCommitSHA string) []byte {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(fixture, &payload); err != nil {
		t.Fatalf("unmarshal fixture: %v", err)
	}
	pullRequest, ok := payload["pull_request"].(map[string]any)
	if !ok {
		t.Fatal("fixture misses pull_request object")
	}
	pullRequest["merge_commit_sha"] = mergeCommitSHA
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return raw
}
