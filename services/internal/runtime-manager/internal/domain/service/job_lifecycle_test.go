package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/enum"
)

func TestJobLifecycleCreatesClaimsProgressesAndFails(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, repo := newTestServiceWithPlacementResolver(resolver)
	projectID := mustUUID("00000000-0000-0000-0000-000000000501")
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:      enum.JobTypeBuild,
		Priority:     enum.JobPriorityHigh,
		ProjectID:    &projectID,
		JobInputJSON: []byte(`{"target":"api"}`),
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000502"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(): %v", err)
	}
	if job.Status != enum.JobStatusPending || job.CommandID == "" {
		t.Fatalf("created job = %#v, want pending with command id", job)
	}
	if job.FleetScopeID == nil || *job.FleetScopeID != testFleetScopeID || job.ClusterID == nil || *job.ClusterID != testClusterID {
		t.Fatalf("job placement = %v/%v, want resolver refs", job.FleetScopeID, job.ClusterID)
	}
	if len(resolver.requests) != 1 || resolver.requests[0].RuntimeMode != enum.RuntimeModePlatformJob {
		t.Fatalf("placement resolver requests = %#v, want one platform job request", resolver.requests)
	}
	if len(repo.events) != 1 || repo.events[0].EventType != eventJobCreated {
		t.Fatalf("events = %#v, want job created", repo.events)
	}

	claim, err := svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeBuild},
		LeaseOwner: "worker/runtime-1",
		LeaseUntil: testNow.Add(10 * time.Minute),
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000503"), 0),
	})
	if err != nil {
		t.Fatalf("ClaimRunnableJob(): %v", err)
	}
	if claim.Job.ID != job.ID || claim.Job.Status != enum.JobStatusClaimed || claim.LeaseToken == "" {
		t.Fatalf("claim = %#v, want claimed original job with token", claim)
	}
	if claim.Job.LeaseTokenHash == claim.LeaseToken {
		t.Fatalf("lease token was stored as plain text")
	}
	if repo.events[len(repo.events)-1].EventType != eventJobStarted {
		t.Fatalf("last event = %s, want job started", repo.events[len(repo.events)-1].EventType)
	}

	progress, err := svc.ReportJobStepProgress(context.Background(), ReportJobStepProgressInput{
		JobID:        job.ID,
		LeaseToken:   claim.LeaseToken,
		StepKey:      "build-image",
		Status:       enum.JobStepStatusSucceeded,
		ShortLogTail: "image pushed",
		ExternalRef:  "job/build-image",
		ArtifactRefs: []RuntimeArtifactRefInput{
			{
				ArtifactType: enum.RuntimeArtifactTypeImageRef,
				ExternalRef:  "registry.local/api@sha256:abc",
				Digest:       "sha256:abc",
				MetadataJSON: []byte(`{"repository":"api"}`),
			},
		},
		Meta: commandMeta(mustUUID("00000000-0000-0000-0000-000000000504"), claim.Job.Version),
	})
	if err != nil {
		t.Fatalf("ReportJobStepProgress(): %v", err)
	}
	if progress.Status != enum.JobStatusRunning || len(progress.Steps) != 1 || progress.Steps[0].Version != 1 {
		t.Fatalf("progress job = %#v, want running with one step", progress)
	}
	if len(repo.runtimeArtifactRefs) != 1 {
		t.Fatalf("artifact refs = %d, want 1", len(repo.runtimeArtifactRefs))
	}
	if repo.events[len(repo.events)-1].EventType != eventJobStepUpdated {
		t.Fatalf("last event = %s, want job step updated", repo.events[len(repo.events)-1].EventType)
	}

	failed, err := svc.FailJob(context.Background(), FailJobInput{
		JobID:        job.ID,
		LeaseToken:   claim.LeaseToken,
		ErrorCode:    "IMAGE_SCAN_FAILED",
		ErrorMessage: "critical vulnerability",
		ShortLogTail: "scanner failed",
		FullLogRef:   "k8s://jobs/build-image/logs",
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000505"), progress.Version),
	})
	if err != nil {
		t.Fatalf("FailJob(): %v", err)
	}
	if failed.Status != enum.JobStatusFailed || failed.NextAction == "" || failed.FullLogRef == "" {
		t.Fatalf("failed job = %#v, want failed with next action and full log ref", failed)
	}
	if failed.LeaseTokenHash != "" || failed.LeaseUntil != nil {
		t.Fatalf("failed job lease = %s/%v, want cleared", failed.LeaseTokenHash, failed.LeaseUntil)
	}
	if repo.events[len(repo.events)-1].EventType != eventJobFailed {
		t.Fatalf("last event = %s, want job failed", repo.events[len(repo.events)-1].EventType)
	}
}

func TestJobLeaseTokenRequiredForWorkerMutations(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:      enum.JobTypeDeploy,
		Priority:     enum.JobPriorityNormal,
		JobInputJSON: []byte(`{"target":"stage"}`),
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000506"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(): %v", err)
	}
	claim, err := svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		LeaseOwner: "worker/runtime-2",
		LeaseUntil: testNow.Add(10 * time.Minute),
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000507"), 0),
	})
	if err != nil {
		t.Fatalf("ClaimRunnableJob(): %v", err)
	}

	_, err = svc.CompleteJob(context.Background(), CompleteJobInput{
		JobID:        job.ID,
		LeaseToken:   "wrong-token",
		ShortLogTail: "done",
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000508"), claim.Job.Version),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CompleteJob() err = %v, want conflict for wrong token", err)
	}
}

func TestClaimRunnableJobReplayDoesNotClaimAnotherJob(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	for index, idText := range []string{"00000000-0000-0000-0000-000000000520", "00000000-0000-0000-0000-000000000521"} {
		_, err := svc.CreateJob(context.Background(), CreateJobInput{
			JobType:      enum.JobTypeBuild,
			Priority:     enum.JobPriorityNormal,
			JobInputJSON: []byte(`{"target":"api"}`),
			Meta:         commandMeta(mustUUID(idText), 0),
		})
		if err != nil {
			t.Fatalf("CreateJob(%d): %v", index, err)
		}
	}
	meta := commandMeta(mustUUID("00000000-0000-0000-0000-000000000522"), 0)
	claim, err := svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeBuild},
		LeaseOwner: "worker/runtime-claim",
		LeaseUntil: testNow.Add(10 * time.Minute),
		Meta:       meta,
	})
	if err != nil {
		t.Fatalf("ClaimRunnableJob(): %v", err)
	}
	_, err = svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeBuild},
		LeaseOwner: "worker/runtime-claim",
		LeaseUntil: testNow.Add(10 * time.Minute),
		Meta:       meta,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("replay ClaimRunnableJob() err = %v, want conflict", err)
	}
	pending := 0
	for _, job := range repo.jobs {
		if job.ID != claim.Job.ID && job.Status == enum.JobStatusPending {
			pending++
		}
	}
	if pending != 1 {
		t.Fatalf("pending jobs after claim replay = %d, want second job untouched", pending)
	}
}

func TestCreateJobWithSlotReusesSlotPlacementWithoutResolver(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, _ := newTestServiceWithPlacementResolver(resolver)
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "go-backend",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "policy-sha",
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000540"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	resolver.err = errs.ErrPlacementRejected
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:      enum.JobTypeBuild,
		Priority:     enum.JobPriorityNormal,
		SlotID:       &slot.ID,
		JobInputJSON: []byte(`{"target":"api"}`),
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000541"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(): %v", err)
	}
	if !sameUUIDPtr(job.FleetScopeID, slot.FleetScopeID) || !sameUUIDPtr(job.ClusterID, slot.ClusterID) {
		t.Fatalf("job placement = %v/%v, want slot placement %v/%v", job.FleetScopeID, job.ClusterID, slot.FleetScopeID, slot.ClusterID)
	}
	if len(resolver.requests) != 1 {
		t.Fatalf("placement resolver calls = %d, want only reserve slot call", len(resolver.requests))
	}
}

func TestCreateJobAuthorizesBeforePlacement(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, _ := newTestServiceWithAuthorizerAndPlacementResolver(denyAuthorizer{}, resolver)
	_, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:      enum.JobTypeBuild,
		Priority:     enum.JobPriorityNormal,
		JobInputJSON: []byte(`{"target":"api"}`),
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000542"), 0),
	})
	if !errors.Is(err, errs.ErrForbidden) {
		t.Fatalf("CreateJob() err = %v, want forbidden", err)
	}
	if len(resolver.requests) != 0 {
		t.Fatalf("placement resolver calls = %d, want none before authorization", len(resolver.requests))
	}
}

func TestCreateJobReplayRejectsChangedPlacementInput(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, _ := newTestServiceWithPlacementResolver(resolver)
	projectID := mustUUID("00000000-0000-0000-0000-000000000543")
	meta := commandMeta(mustUUID("00000000-0000-0000-0000-000000000544"), 0)

	_, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:      enum.JobTypeBuild,
		Priority:     enum.JobPriorityNormal,
		ProjectID:    &projectID,
		JobInputJSON: []byte(`{"target":"api"}`),
		PlacementConstraints: PlacementConstraintsInput{
			RequiredCapabilities: []string{"standard"},
			MetadataJSON:         []byte(`{"regions":["eu-1"]}`),
		},
		Meta: meta,
	})
	if err != nil {
		t.Fatalf("first CreateJob(): %v", err)
	}
	_, err = svc.CreateJob(context.Background(), CreateJobInput{
		JobType:      enum.JobTypeBuild,
		Priority:     enum.JobPriorityNormal,
		ProjectID:    &projectID,
		JobInputJSON: []byte(`{"target":"api"}`),
		PlacementConstraints: PlacementConstraintsInput{
			RequiredCapabilities: []string{"gpu"},
			MetadataJSON:         []byte(`{"regions":["eu-1"]}`),
		},
		Meta: meta,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("changed placement replay err = %v, want conflict", err)
	}
	if len(resolver.requests) != 1 {
		t.Fatalf("placement resolver calls = %d, want no fleet call on conflicting replay", len(resolver.requests))
	}
}

func TestJobInputJSONKeepsLargeNumbers(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	payload := []byte(`{"policy_version":1234567890123456789}`)
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:      enum.JobTypeHousekeeping,
		Priority:     enum.JobPriorityNormal,
		JobInputJSON: payload,
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000523"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(): %v", err)
	}
	if string(job.JobInputJSON) != string(payload) {
		t.Fatalf("job input json = %s, want %s", job.JobInputJSON, payload)
	}
}

func TestJobInputJSONRejectsNull(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	_, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:      enum.JobTypeHousekeeping,
		Priority:     enum.JobPriorityNormal,
		JobInputJSON: []byte(`null`),
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000527"), 0),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("CreateJob() err = %v, want invalid argument for null job input", err)
	}
}

func TestShortLogTailKeepsValidUTF8(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:      enum.JobTypeBuild,
		Priority:     enum.JobPriorityNormal,
		JobInputJSON: []byte(`{"target":"api"}`),
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000524"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(): %v", err)
	}
	claim, err := svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		LeaseOwner: "worker/runtime-utf8",
		LeaseUntil: testNow.Add(10 * time.Minute),
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000525"), 0),
	})
	if err != nil {
		t.Fatalf("ClaimRunnableJob(): %v", err)
	}
	logTail := strings.Repeat("ошибка-", maxShortLogTailBytes)
	progress, err := svc.ReportJobStepProgress(context.Background(), ReportJobStepProgressInput{
		JobID:        job.ID,
		LeaseToken:   claim.LeaseToken,
		StepKey:      "utf8-log",
		Status:       enum.JobStepStatusRunning,
		ShortLogTail: logTail,
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000526"), claim.Job.Version),
	})
	if err != nil {
		t.Fatalf("ReportJobStepProgress(): %v", err)
	}
	if len(progress.ShortLogTail) > maxShortLogTailBytes || !utf8.ValidString(progress.ShortLogTail) {
		t.Fatalf("job short log tail is invalid: len=%d valid=%v", len(progress.ShortLogTail), utf8.ValidString(progress.ShortLogTail))
	}
	if len(progress.Steps) != 1 || len(progress.Steps[0].ShortLogTail) > maxShortLogTailBytes || !utf8.ValidString(progress.Steps[0].ShortLogTail) {
		t.Fatalf("step short log tail is invalid: steps=%#v", progress.Steps)
	}
}

func TestRuntimeArtifactMetadataRejectsNull(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	slotID := mustUUID("00000000-0000-0000-0000-000000000528")
	projectID := mustUUID("00000000-0000-0000-0000-000000000529")
	svc.repository.(*fakeRepository).slots[slotID] = entitySlot(slotID, projectID)
	_, err := svc.RecordRuntimeArtifactRef(context.Background(), RecordRuntimeArtifactRefInput{
		SlotID: &slotID,
		ArtifactRef: RuntimeArtifactRefInput{
			ArtifactType: enum.RuntimeArtifactTypeLogRef,
			ExternalRef:  "k8s://pods/runtime/log",
			MetadataJSON: []byte(`null`),
		},
		Meta: commandMeta(mustUUID("00000000-0000-0000-0000-000000000530"), 0),
	})
	if !errors.Is(err, errs.ErrInvalidArgument) {
		t.Fatalf("RecordRuntimeArtifactRef() err = %v, want invalid argument for null metadata", err)
	}
}

func TestRecordRuntimeArtifactRefIsIdempotent(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	slotID := mustUUID("00000000-0000-0000-0000-000000000509")
	projectID := mustUUID("00000000-0000-0000-0000-000000000510")
	svc.repository.(*fakeRepository).slots[slotID] = entitySlot(slotID, projectID)
	meta := commandMeta(mustUUID("00000000-0000-0000-0000-000000000511"), 0)
	first, err := svc.RecordRuntimeArtifactRef(context.Background(), RecordRuntimeArtifactRefInput{
		SlotID: &slotID,
		ArtifactRef: RuntimeArtifactRefInput{
			ArtifactType: enum.RuntimeArtifactTypeLogRef,
			ExternalRef:  "k8s://pods/runtime/log",
			MetadataJSON: []byte(`{}`),
		},
		Meta: meta,
	})
	if err != nil {
		t.Fatalf("RecordRuntimeArtifactRef(): %v", err)
	}
	replay, err := svc.RecordRuntimeArtifactRef(context.Background(), RecordRuntimeArtifactRefInput{
		SlotID: &slotID,
		ArtifactRef: RuntimeArtifactRefInput{
			ArtifactType: enum.RuntimeArtifactTypeLogRef,
			ExternalRef:  "k8s://pods/runtime/log",
			MetadataJSON: []byte(`{}`),
		},
		Meta: meta,
	})
	if err != nil {
		t.Fatalf("replay RecordRuntimeArtifactRef(): %v", err)
	}
	if replay.ID != first.ID {
		t.Fatalf("replay id = %s, want %s", replay.ID, first.ID)
	}
	_, err = svc.RecordRuntimeArtifactRef(context.Background(), RecordRuntimeArtifactRefInput{
		SlotID: &slotID,
		ArtifactRef: RuntimeArtifactRefInput{
			ArtifactType: enum.RuntimeArtifactTypeLogRef,
			ExternalRef:  "k8s://pods/runtime/other-log",
			MetadataJSON: []byte(`{}`),
		},
		Meta: meta,
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("conflicting replay err = %v, want conflict", err)
	}
}

func entitySlot(slotID uuid.UUID, projectID uuid.UUID) entity.Slot {
	return entity.Slot{
		Base:           entity.Base{ID: slotID, Version: 1, CreatedAt: testNow, UpdatedAt: testNow},
		Status:         enum.SlotStatusReady,
		RuntimeMode:    enum.RuntimeModeFullEnv,
		ProjectID:      &projectID,
		RuntimeProfile: "go-backend",
	}
}
