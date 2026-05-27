package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/errs"
	projectservice "github.com/codex-k8s/kodex/services/internal/project-catalog/internal/domain/service"
)

func TestBootstrapMergeEventHandlerRecordsDiagnostic(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	repositoryID := uuid.New()
	recorder := &fakeBootstrapMergeRecorder{}
	handler := bootstrapMergeEventHandler{recorder: recorder}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: bootstrapMergeStoredEvent(t, bootstrapMergePayload(projectID, repositoryID))})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() status = %s, want ack: %+v", result.Status, result)
	}
	if len(recorder.inputs) != 1 {
		t.Fatalf("recorded inputs = %d, want 1", len(recorder.inputs))
	}
	input := recorder.inputs[0]
	if input.ProjectID != projectID || input.RepositoryID != repositoryID {
		t.Fatalf("ids = %s/%s, want %s/%s", input.ProjectID, input.RepositoryID, projectID, repositoryID)
	}
	if input.MergeSignal.SignalKind != "bootstrap" || input.MergeSignal.SignalKey != "github/bootstrap/PR_123" {
		t.Fatalf("merge signal = %+v, want bootstrap signal", input.MergeSignal)
	}
	if input.MergeSignal.SourceRef != "kodex/bootstrap" || input.MergeSignal.BaseBranch != "main" {
		t.Fatalf("refs = %+v, want safe provider source and base branch", input.MergeSignal)
	}
	if input.SignalFingerprint == "" || input.ErrorCode != bootstrapMergeMissingCheckedArtifactCode {
		t.Fatalf("diagnostic = %+v, want fingerprint and missing artifact code", input)
	}
}

func TestBootstrapMergeEventHandlerRejectsUnsafePayload(t *testing.T) {
	t.Parallel()

	handler := bootstrapMergeEventHandler{recorder: &fakeBootstrapMergeRecorder{}}
	storedEvent := bootstrapMergeStoredEvent(t, providerevents.Payload{})
	storedEvent.Payload = []byte(`{"project_id":`)

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: storedEvent})
	if result.Status != eventconsumer.ResultPoison || result.Code != "invalid_payload" {
		t.Fatalf("HandleEvent() = %+v, want invalid_payload poison", result)
	}
}

func TestBootstrapMergeEventHandlerMapsDomainErrors(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	repositoryID := uuid.New()
	cases := []struct {
		name   string
		err    error
		status eventconsumer.ResultStatus
		code   string
	}{
		{name: "conflict", err: errs.ErrConflict, status: eventconsumer.ResultPoison, code: "conflicting_signal"},
		{name: "unknown binding", err: errs.ErrNotFound, status: eventconsumer.ResultPoison, code: "unknown_binding"},
		{name: "temporary", err: errors.New("database unavailable"), status: eventconsumer.ResultRetry, code: "retryable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := bootstrapMergeEventHandler{recorder: &fakeBootstrapMergeRecorder{err: tc.err}}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: bootstrapMergeStoredEvent(t, bootstrapMergePayload(projectID, repositoryID))})
			if result.Status != tc.status || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want %s/%s", result, tc.status, tc.code)
			}
		})
	}
}

func TestBootstrapMergeEventFingerprintIgnoresDeliveryIdentity(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	repositoryID := uuid.New()
	payload := bootstrapMergePayload(projectID, repositoryID)
	first := bootstrapMergeStoredEvent(t, payload)
	second := bootstrapMergeStoredEvent(t, payload)
	second.SequenceID = first.SequenceID + 1
	second.Event.ID = uuid.New()
	second.Event.AggregateID = uuid.New()

	firstFingerprint := bootstrapMergeEventFingerprint(first, payload)
	secondFingerprint := bootstrapMergeEventFingerprint(second, payload)
	if firstFingerprint != secondFingerprint {
		t.Fatalf("fingerprints differ for delivery replay: %s != %s", firstFingerprint, secondFingerprint)
	}

	changedPayload := payload
	changedPayload.MergeCommitSHA = "fedcba9876543210fedcba9876543210fedcba98"
	changedFingerprint := bootstrapMergeEventFingerprint(second, changedPayload)
	if changedFingerprint == firstFingerprint {
		t.Fatalf("fingerprint did not change after safe provider merge commit changed: %s", changedFingerprint)
	}
}

func bootstrapMergePayload(projectID uuid.UUID, repositoryID uuid.UUID) providerevents.Payload {
	return providerevents.Payload{
		ProviderSlug:                "github",
		RepositoryMergeSignalID:     uuid.New().String(),
		SignalKey:                   "github/bootstrap/PR_123",
		SignalKind:                  "bootstrap",
		ProjectID:                   projectID.String(),
		RepositoryID:                repositoryID.String(),
		RepositoryFullName:          "codex-k8s/kodex",
		ProviderRepositoryID:        "R_123",
		BaseBranch:                  "main",
		HeadBranch:                  "kodex/bootstrap",
		SourceRef:                   "kodex/bootstrap",
		MergeCommitSHA:              "0123456789abcdef0123456789abcdef01234567",
		WatermarkDigest:             "sha256:" + strings.Repeat("a", 64),
		WorkItemProjectionID:        "projection-1",
		PullRequestProviderID:       "PR_123",
		PullRequestURL:              "https://github.com/codex-k8s/kodex/pull/123",
		RelatedProviderOperationRef: "operation-1",
		ObservedAt:                  time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC).Format(time.RFC3339),
		MergedAt:                    time.Date(2026, 5, 27, 12, 1, 0, 0, time.UTC).Format(time.RFC3339),
		Version:                     3,
	}
}

func bootstrapMergeStoredEvent(t *testing.T, payload providerevents.Payload) eventlog.StoredEvent {
	t.Helper()
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal(): %v", err)
	}
	return eventlog.StoredEvent{
		SequenceID: 1,
		Event: eventlog.Event{
			ID:            uuid.New(),
			SourceService: "provider-hub",
			EventType:     providerevents.EventRepositoryBootstrapMerged,
			SchemaVersion: providerevents.SchemaVersion,
			AggregateType: providerevents.AggregateRepositoryMergeSignal,
			AggregateID:   uuid.New(),
			Payload:       payloadBytes,
			OccurredAt:    time.Now().UTC(),
		},
		RecordedAt: time.Now().UTC(),
	}
}

type fakeBootstrapMergeRecorder struct {
	inputs []projectservice.BootstrapMergeSignalDiagnosticInput
	err    error
}

func (r *fakeBootstrapMergeRecorder) RecordBootstrapMergeSignalDiagnostic(_ context.Context, input projectservice.BootstrapMergeSignalDiagnosticInput) error {
	r.inputs = append(r.inputs, input)
	return r.err
}
