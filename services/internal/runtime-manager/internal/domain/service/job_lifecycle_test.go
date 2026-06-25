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
	"github.com/codex-k8s/kodex/services/internal/runtime-manager/internal/domain/types/value"
)

func TestJobLifecycleCreatesClaimsProgressesAndFails(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, repo := newTestServiceWithPlacementResolver(resolver)
	projectID := mustUUID("00000000-0000-0000-0000-000000000501")
	buildSpec := testBuildExecutionSpec("access-manager")
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:            enum.JobTypeBuild,
		Priority:           enum.JobPriorityHigh,
		ProjectID:          &projectID,
		BuildExecutionSpec: &buildSpec,
		Meta:               commandMeta(mustUUID("00000000-0000-0000-0000-000000000502"), 0),
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
	buildSpec := testBuildExecutionSpec("access-manager")
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:            enum.JobTypeBuild,
		Priority:           enum.JobPriorityNormal,
		BuildExecutionSpec: &buildSpec,
		Meta:               commandMeta(mustUUID("00000000-0000-0000-0000-000000000506"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(): %v", err)
	}
	claim, err := svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeBuild},
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

func TestFailJobCanPersistTimedOutStatus(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	buildSpec := testBuildExecutionSpec("runtime-manager")
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:            enum.JobTypeBuild,
		Priority:           enum.JobPriorityNormal,
		BuildExecutionSpec: &buildSpec,
		Meta:               commandMeta(mustUUID("00000000-0000-0000-0000-000000000518"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(): %v", err)
	}
	claim, err := svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeBuild},
		LeaseOwner: "worker/runtime-timeout",
		LeaseUntil: testNow.Add(10 * time.Minute),
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000519"), 0),
	})
	if err != nil {
		t.Fatalf("ClaimRunnableJob(): %v", err)
	}

	timedOut, err := svc.FailJob(context.Background(), FailJobInput{
		JobID:        job.ID,
		LeaseToken:   claim.LeaseToken,
		ErrorCode:    "kubernetes_job_timeout",
		ErrorMessage: "Kubernetes Job timed out",
		ShortLogTail: "build did not finish",
		TimedOut:     true,
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000523"), claim.Job.Version),
	})
	if err != nil {
		t.Fatalf("FailJob(timeout): %v", err)
	}
	if timedOut.Status != enum.JobStatusTimedOut || timedOut.LastErrorCode != "kubernetes_job_timeout" {
		t.Fatalf("timed out job = %#v, want timed_out status and timeout code", timedOut)
	}
	if repo.events[len(repo.events)-1].EventType != eventJobFailed {
		t.Fatalf("last event = %s, want job failed event with timed_out status payload", repo.events[len(repo.events)-1].EventType)
	}
}

func TestClaimRunnableJobReplayDoesNotClaimAnotherJob(t *testing.T) {
	t.Parallel()

	svc, repo := newTestService()
	for index, idText := range []string{"00000000-0000-0000-0000-000000000520", "00000000-0000-0000-0000-000000000521"} {
		buildSpec := testBuildExecutionSpec("access-manager")
		buildSpec.ImageTag = "0.1." + string(rune('0'+index))
		_, err := svc.CreateJob(context.Background(), CreateJobInput{
			JobType:            enum.JobTypeBuild,
			Priority:           enum.JobPriorityNormal,
			BuildExecutionSpec: &buildSpec,
			Meta:               commandMeta(mustUUID(idText), 0),
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

func TestAgentRunJobTypeWithoutExecutionSpecStaysWaiting(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, _ := newTestServiceWithPlacementResolver(resolver)
	agentRunID := mustUUID("00000000-0000-0000-0000-000000000531")
	projectID := mustUUID("00000000-0000-0000-0000-000000000532")
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:      enum.JobTypeAgentRun,
		Priority:     enum.JobPriorityHigh,
		AgentRunID:   &agentRunID,
		ProjectID:    &projectID,
		JobInputJSON: []byte(`{}`),
		Meta:         commandMeta(mustUUID("00000000-0000-0000-0000-000000000533"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(agent_run): %v", err)
	}
	if job.JobType != enum.JobTypeAgentRun || !sameUUIDPtr(job.AgentRunID, &agentRunID) {
		t.Fatalf("agent run job = %#v, want agent_run with agent_run_id", job)
	}
	if job.LastErrorCode != agentRunExecutionSpecRequiredCode || job.NextAction != agentRunExecutionSpecRequiredAction {
		t.Fatalf("agent run job diagnostic = %q/%q, want execution spec requirement", job.LastErrorCode, job.NextAction)
	}

	list, err := svc.ListJobs(context.Background(), ListJobsInput{
		JobTypes:   []enum.JobType{enum.JobTypeAgentRun},
		AgentRunID: &agentRunID,
		Meta:       value.QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if err != nil {
		t.Fatalf("ListJobs(agent_run): %v", err)
	}
	if len(list.Jobs) != 1 || list.Jobs[0].JobType != enum.JobTypeAgentRun {
		t.Fatalf("ListJobs(agent_run) = %#v, want one agent_run job", list.Jobs)
	}

	_, err = svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeAgentRun},
		LeaseOwner: "worker/agent-run",
		LeaseUntil: testNow.Add(10 * time.Minute),
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000534"), 0),
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("ClaimRunnableJob(agent_run without spec) err = %v, want not found", err)
	}
}

func TestBuildDeployJobsWithoutExecutionSpecStayWaiting(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	tests := []struct {
		name       string
		jobType    enum.JobType
		errorCode  string
		nextAction string
		commandID  uuid.UUID
	}{
		{
			name:       "build",
			jobType:    enum.JobTypeBuild,
			errorCode:  buildExecutionSpecRequiredCode,
			nextAction: buildExecutionSpecRequiredAction,
			commandID:  mustUUID("00000000-0000-0000-0000-000000000550"),
		},
		{
			name:       "deploy",
			jobType:    enum.JobTypeDeploy,
			errorCode:  deployExecutionSpecRequiredCode,
			nextAction: deployExecutionSpecRequiredAction,
			commandID:  mustUUID("00000000-0000-0000-0000-000000000551"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			job, err := svc.CreateJob(context.Background(), CreateJobInput{
				JobType:  tt.jobType,
				Priority: enum.JobPriorityHigh,
				Meta:     commandMeta(tt.commandID, 0),
			})
			if err != nil {
				t.Fatalf("CreateJob(%s): %v", tt.jobType, err)
			}
			if job.LastErrorCode != tt.errorCode || job.NextAction != tt.nextAction {
				t.Fatalf("job diagnostic = %q/%q, want %q/%q", job.LastErrorCode, job.NextAction, tt.errorCode, tt.nextAction)
			}
		})
	}

	_, err := svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeBuild, enum.JobTypeDeploy},
		LeaseOwner: "worker/build-deploy",
		LeaseUntil: testNow.Add(10 * time.Minute),
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000552"), 0),
	})
	if !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("ClaimRunnableJob(build/deploy without spec) err = %v, want not found", err)
	}
}

func TestBuildDeployJobsRequireSafeTypedInput(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	buildSpec := testBuildExecutionSpec("access-manager")
	deploySpec := testDeployExecutionSpec("access-manager")
	tests := []struct {
		name       string
		jobType    enum.JobType
		payload    []byte
		buildSpec  *BuildExecutionSpecInput
		deploySpec *DeployExecutionSpecInput
		commandID  uuid.UUID
	}{
		{
			name:      "build raw payload",
			jobType:   enum.JobTypeBuild,
			payload:   []byte(`{"target":"api"}`),
			commandID: mustUUID("00000000-0000-0000-0000-000000000553"),
		},
		{
			name:      "deploy raw payload",
			jobType:   enum.JobTypeDeploy,
			payload:   []byte(`{"target":"prod"}`),
			commandID: mustUUID("00000000-0000-0000-0000-000000000554"),
		},
		{
			name:      "build spec with raw payload",
			jobType:   enum.JobTypeBuild,
			payload:   []byte(`{"target":"api"}`),
			buildSpec: &buildSpec,
			commandID: mustUUID("00000000-0000-0000-0000-000000000555"),
		},
		{
			name:    "build unsafe secret ref",
			jobType: enum.JobTypeBuild,
			buildSpec: func() *BuildExecutionSpecInput {
				copy := buildSpec
				copy.AllowedSecretRefs = []RuntimeJobExecutionRefInput{{Kind: "registry", Ref: "secret://runtime/secret-value"}}
				return &copy
			}(),
			commandID: mustUUID("00000000-0000-0000-0000-000000000556"),
		},
		{
			name:    "deploy invalid manifest digest",
			jobType: enum.JobTypeDeploy,
			deploySpec: func() *DeployExecutionSpecInput {
				copy := deploySpec
				copy.ManifestDigest = "sha256:not-hex"
				return &copy
			}(),
			commandID: mustUUID("00000000-0000-0000-0000-000000000557"),
		},
		{
			name:    "deploy missing manifest bundle",
			jobType: enum.JobTypeDeploy,
			deploySpec: func() *DeployExecutionSpecInput {
				copy := deploySpec
				copy.ManifestBundleRef = ""
				return &copy
			}(),
			commandID: mustUUID("00000000-0000-0000-0000-000000000563"),
		},
		{
			name:    "deploy missing rollout targets",
			jobType: enum.JobTypeDeploy,
			deploySpec: func() *DeployExecutionSpecInput {
				copy := deploySpec
				copy.RolloutTargets = nil
				return &copy
			}(),
			commandID: mustUUID("00000000-0000-0000-0000-000000000564"),
		},
		{
			name:       "deploy spec on build job",
			jobType:    enum.JobTypeBuild,
			deploySpec: &deploySpec,
			commandID:  mustUUID("00000000-0000-0000-0000-000000000565"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateJob(context.Background(), CreateJobInput{
				JobType:             tt.jobType,
				Priority:            enum.JobPriorityHigh,
				JobInputJSON:        tt.payload,
				BuildExecutionSpec:  tt.buildSpec,
				DeployExecutionSpec: tt.deploySpec,
				Meta:                commandMeta(tt.commandID, 0),
			})
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("CreateJob(%s unsafe input) err = %v, want invalid argument", tt.jobType, err)
			}
		})
	}
}

func TestBuildDeployExecutionSpecsArePersistedAsTypedJobInput(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	buildSpec := testBuildExecutionSpec("runtime-manager")
	buildJob, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:            enum.JobTypeBuild,
		Priority:           enum.JobPriorityHigh,
		BuildExecutionSpec: &buildSpec,
		Meta:               commandMeta(mustUUID("00000000-0000-0000-0000-000000000559"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(build spec): %v", err)
	}
	extractedBuildSpec, ok := BuildExecutionSpecFromJobInput(buildJob.JobInputJSON)
	if !ok || extractedBuildSpec.ServiceKey != buildSpec.ServiceKey || extractedBuildSpec.BuildContextDigest != buildSpec.BuildContextDigest {
		t.Fatalf("BuildExecutionSpecFromJobInput() = %+v, %v", extractedBuildSpec, ok)
	}
	if buildJob.LastErrorCode != "" || buildJob.NextAction != "" {
		t.Fatalf("build job diagnostic = %q/%q, want executable typed spec", buildJob.LastErrorCode, buildJob.NextAction)
	}

	deploySpec := testDeployExecutionSpec("runtime-manager")
	deployJob, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:             enum.JobTypeDeploy,
		Priority:            enum.JobPriorityHigh,
		DeployExecutionSpec: &deploySpec,
		Meta:                commandMeta(mustUUID("00000000-0000-0000-0000-000000000560"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(deploy spec): %v", err)
	}
	extractedDeploySpec, ok := DeployExecutionSpecFromJobInput(deployJob.JobInputJSON)
	if !ok ||
		extractedDeploySpec.ServiceKey != deploySpec.ServiceKey ||
		extractedDeploySpec.DeployPlanFingerprint != deploySpec.DeployPlanFingerprint ||
		extractedDeploySpec.ManifestBundleDigest != deploySpec.ManifestBundleDigest ||
		len(extractedDeploySpec.RolloutTargets) != 1 ||
		len(extractedDeploySpec.ExpectedImageRefs) != 1 {
		t.Fatalf("DeployExecutionSpecFromJobInput() = %+v, %v", extractedDeploySpec, ok)
	}
	if deployJob.LastErrorCode != "" || deployJob.NextAction != "" {
		t.Fatalf("deploy job diagnostic = %q/%q, want executable typed spec", deployJob.LastErrorCode, deployJob.NextAction)
	}
}

func TestDeployJobWithExecutionSpecCanBeClaimed(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	deploySpec := testDeployExecutionSpec("runtime-manager")
	meta := commandMeta(mustUUID("00000000-0000-0000-0000-000000000561"), 0)
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:             enum.JobTypeDeploy,
		Priority:            enum.JobPriorityBlocking,
		DeployExecutionSpec: &deploySpec,
		Meta:                meta,
	})
	if err != nil {
		t.Fatalf("CreateJob(deploy spec): %v", err)
	}
	if job.Status != enum.JobStatusPending || job.LastErrorCode != "" || job.NextAction != "" {
		t.Fatalf("deploy job = %#v, want pending executable job", job)
	}

	replay, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:             enum.JobTypeDeploy,
		Priority:            enum.JobPriorityBlocking,
		DeployExecutionSpec: &deploySpec,
		Meta:                meta,
	})
	if err != nil {
		t.Fatalf("CreateJob(deploy replay): %v", err)
	}
	if replay.ID != job.ID || replay.Version != job.Version || replay.LastErrorCode != "" {
		t.Fatalf("deploy replay = %#v, want same waiting job", replay)
	}

	read, err := svc.GetJob(context.Background(), GetJobInput{
		JobID: job.ID,
		Meta:  value.QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if err != nil {
		t.Fatalf("GetJob(deploy): %v", err)
	}
	if read.ID != job.ID || read.JobType != enum.JobTypeDeploy || read.LastErrorCode != "" {
		t.Fatalf("GetJob(deploy) = %#v, want typed waiting deploy job", read)
	}
	if extracted, ok := DeployExecutionSpecFromJobInput(read.JobInputJSON); !ok || extracted.DeployPlanFingerprint != deploySpec.DeployPlanFingerprint {
		t.Fatalf("DeployExecutionSpecFromJobInput(read) = %+v, %v", extracted, ok)
	}

	list, err := svc.ListJobs(context.Background(), ListJobsInput{
		JobTypes: []enum.JobType{enum.JobTypeDeploy},
		Meta:     value.QueryMeta{Actor: value.Actor{Type: "service", ID: "agent-manager"}},
	})
	if err != nil {
		t.Fatalf("ListJobs(deploy): %v", err)
	}
	if len(list.Jobs) != 1 || list.Jobs[0].ID != job.ID || list.Jobs[0].LastErrorCode != "" {
		t.Fatalf("ListJobs(deploy) = %#v, want one waiting deploy job", list.Jobs)
	}

	claim, err := svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeDeploy},
		LeaseOwner: "worker/deploy",
		LeaseUntil: testNow.Add(10 * time.Minute),
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000562"), 0),
	})
	if err != nil {
		t.Fatalf("ClaimRunnableJob(deploy spec): %v", err)
	}
	if claim.Job.ID != job.ID || claim.Job.JobType != enum.JobTypeDeploy || claim.LeaseToken == "" {
		t.Fatalf("claim = %#v, want claimed executable deploy job with token", claim)
	}
}

func TestAgentRunJobWithExecutionSpecCanBeClaimed(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, repo := newTestServiceWithPlacementResolver(resolver)
	agentRunID := mustUUID("00000000-0000-0000-0000-000000000544")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "agent/default",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "sha256:workspace-policy",
		AgentRunID:            &agentRunID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000545"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	spec := testAgentRunExecutionSpec(agentRunID, slot.ID)
	repo.workspaceMaterializations[spec.ExpectedMaterializationID] = entity.WorkspaceMaterialization{
		Base:        entity.Base{ID: spec.ExpectedMaterializationID, Version: 1, CreatedAt: testNow, UpdatedAt: testNow},
		SlotID:      slot.ID,
		Status:      enum.WorkspaceMaterializationStatusCompleted,
		Fingerprint: spec.ExpectedMaterializationFingerprint,
	}
	repo.slots[slot.ID] = readyAgentRunSlot(slot, spec)
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:               enum.JobTypeAgentRun,
		Priority:              enum.JobPriorityHigh,
		AgentRunExecutionSpec: &spec,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000546"), 0),
	})
	if err != nil {
		t.Fatalf("CreateJob(agent_run spec): %v", err)
	}
	if job.JobType != enum.JobTypeAgentRun || !sameUUIDPtr(job.AgentRunID, &agentRunID) || !sameUUIDPtr(job.SlotID, &slot.ID) {
		t.Fatalf("agent run job refs = %#v, want spec refs", job)
	}
	if job.LastErrorCode != "" || job.NextAction != "" {
		t.Fatalf("agent run job diagnostic = %q/%q, want executable spec without waiting diagnostic", job.LastErrorCode, job.NextAction)
	}
	if !agentRunJobInputHasExecutionSpec(job.JobInputJSON) {
		t.Fatalf("agent run job input = %s, want typed execution spec", string(job.JobInputJSON))
	}

	claim, err := svc.ClaimRunnableJob(context.Background(), ClaimRunnableJobInput{
		JobTypes:   []enum.JobType{enum.JobTypeAgentRun},
		LeaseOwner: "worker/agent-run",
		LeaseUntil: testNow.Add(10 * time.Minute),
		Meta:       commandMeta(mustUUID("00000000-0000-0000-0000-000000000547"), 0),
	})
	if err != nil {
		t.Fatalf("ClaimRunnableJob(agent_run spec): %v", err)
	}
	if claim.Job.ID != job.ID || claim.Job.JobType != enum.JobTypeAgentRun || claim.LeaseToken == "" {
		t.Fatalf("claim = %#v, want claimed executable agent_run job with token", claim)
	}
}

func TestAgentRunJobTypeRequiresSafeInput(t *testing.T) {
	t.Parallel()

	svc, _ := newTestService()
	agentRunID := mustUUID("00000000-0000-0000-0000-000000000535")
	slotID := mustUUID("00000000-0000-0000-0000-000000000538")
	spec := testAgentRunExecutionSpec(agentRunID, slotID)
	tests := []struct {
		name       string
		agentRunID *uuid.UUID
		slotID     *uuid.UUID
		payload    []byte
		spec       *AgentRunExecutionSpecInput
		commandID  uuid.UUID
	}{
		{
			name:       "missing agent run id",
			agentRunID: nil,
			payload:    []byte(`{}`),
			commandID:  mustUUID("00000000-0000-0000-0000-000000000536"),
		},
		{
			name:       "raw prompt payload",
			agentRunID: &agentRunID,
			payload:    []byte(`{"prompt":"run this private task","token":"secret-value"}`),
			commandID:  mustUUID("00000000-0000-0000-0000-000000000537"),
		},
		{
			name:       "typed spec with legacy raw payload",
			agentRunID: &agentRunID,
			slotID:     &slotID,
			payload:    []byte(`{"target":"agent"}`),
			spec:       &spec,
			commandID:  mustUUID("00000000-0000-0000-0000-000000000538"),
		},
		{
			name:       "typed spec missing workspace ref",
			agentRunID: &agentRunID,
			slotID:     &slotID,
			payload:    []byte(`{}`),
			spec: func() *AgentRunExecutionSpecInput {
				copy := spec
				copy.WorkspaceRef = ""
				return &copy
			}(),
			commandID: mustUUID("00000000-0000-0000-0000-000000000539"),
		},
		{
			name:       "typed spec missing workspace pvc ref",
			agentRunID: &agentRunID,
			slotID:     &slotID,
			payload:    []byte(`{}`),
			spec: func() *AgentRunExecutionSpecInput {
				copy := spec
				copy.WorkspacePVCRef = ""
				return &copy
			}(),
			commandID: mustUUID("00000000-0000-0000-0000-000000000530"),
		},
		{
			name:       "typed spec mismatched slot id",
			agentRunID: &agentRunID,
			slotID:     uuidPtr(mustUUID("00000000-0000-0000-0000-000000000540")),
			payload:    []byte(`{}`),
			spec:       &spec,
			commandID:  mustUUID("00000000-0000-0000-0000-000000000541"),
		},
		{
			name:       "codex spec unsupported instruction ref",
			agentRunID: &agentRunID,
			slotID:     &slotID,
			payload:    []byte(`{}`),
			spec: func() *AgentRunExecutionSpecInput {
				copy := spec
				codexSpec := testCodexSessionExecutionSpec(copy, agentRunID)
				codexSpec.InstructionObjectRef = "object://instructions/agent-run-531"
				copy.CodexSessionExecutionSpec = &codexSpec
				return &copy
			}(),
			commandID: mustUUID("00000000-0000-0000-0000-000000000551"),
		},
		{
			name:       "codex spec unsafe instruction ref",
			agentRunID: &agentRunID,
			slotID:     &slotID,
			payload:    []byte(`{}`),
			spec: func() *AgentRunExecutionSpecInput {
				copy := spec
				codexSpec := testCodexSessionExecutionSpec(copy, agentRunID)
				codexSpec.InstructionObjectRef = "object://instructions/prompt_body_secret_value"
				copy.CodexSessionExecutionSpec = &codexSpec
				return &copy
			}(),
			commandID: mustUUID("00000000-0000-0000-0000-000000000542"),
		},
		{
			name:       "codex spec unsafe secret ref",
			agentRunID: &agentRunID,
			slotID:     &slotID,
			payload:    []byte(`{}`),
			spec: func() *AgentRunExecutionSpecInput {
				copy := spec
				codexSpec := testCodexSessionExecutionSpec(copy, agentRunID)
				codexSpec.AllowedSecretRefs = []AgentRunExecutionRefInput{
					{Kind: "runtime_api", Ref: "secret://runtime/secret-value"},
				}
				copy.CodexSessionExecutionSpec = &codexSpec
				return &copy
			}(),
			commandID: mustUUID("00000000-0000-0000-0000-000000000543"),
		},
		{
			name:       "codex spec invalid instruction digest",
			agentRunID: &agentRunID,
			slotID:     &slotID,
			payload:    []byte(`{}`),
			spec: func() *AgentRunExecutionSpecInput {
				copy := spec
				codexSpec := testCodexSessionExecutionSpec(copy, agentRunID)
				codexSpec.InstructionObjectDigest = "sha256:not-hex"
				copy.CodexSessionExecutionSpec = &codexSpec
				return &copy
			}(),
			commandID: mustUUID("00000000-0000-0000-0000-000000000544"),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateJob(context.Background(), CreateJobInput{
				JobType:               enum.JobTypeAgentRun,
				Priority:              enum.JobPriorityHigh,
				SlotID:                tt.slotID,
				AgentRunID:            tt.agentRunID,
				JobInputJSON:          tt.payload,
				AgentRunExecutionSpec: tt.spec,
				Meta:                  commandMeta(tt.commandID, 0),
			})
			if !errors.Is(err, errs.ErrInvalidArgument) {
				t.Fatalf("CreateJob(agent_run unsafe input) err = %v, want invalid argument", err)
			}
		})
	}
}

func TestAgentRunExecutionSpecRequiresCompletedMaterialization(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, repo := newTestServiceWithPlacementResolver(resolver)
	agentRunID := mustUUID("00000000-0000-0000-0000-000000000542")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "agent/default",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "sha256:workspace-policy",
		AgentRunID:            &agentRunID,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000543"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	spec := testAgentRunExecutionSpec(agentRunID, slot.ID)
	repo.slots[slot.ID] = readyAgentRunSlot(slot, spec)
	repo.workspaceMaterializations[spec.ExpectedMaterializationID] = entity.WorkspaceMaterialization{
		Base:        entity.Base{ID: spec.ExpectedMaterializationID, Version: 1, CreatedAt: testNow, UpdatedAt: testNow},
		SlotID:      slot.ID,
		Status:      enum.WorkspaceMaterializationStatusRunning,
		Fingerprint: spec.ExpectedMaterializationFingerprint,
	}
	_, err = svc.CreateJob(context.Background(), CreateJobInput{
		JobType:               enum.JobTypeAgentRun,
		Priority:              enum.JobPriorityHigh,
		AgentRunExecutionSpec: &spec,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000544"), 0),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CreateJob(agent_run running materialization) err = %v, want conflict", err)
	}

	repo.workspaceMaterializations[spec.ExpectedMaterializationID] = entity.WorkspaceMaterialization{
		Base:        entity.Base{ID: spec.ExpectedMaterializationID, Version: 1, CreatedAt: testNow, UpdatedAt: testNow},
		SlotID:      slot.ID,
		Status:      enum.WorkspaceMaterializationStatusCompleted,
		Fingerprint: "sha256:other",
	}
	_, err = svc.CreateJob(context.Background(), CreateJobInput{
		JobType:               enum.JobTypeAgentRun,
		Priority:              enum.JobPriorityHigh,
		AgentRunExecutionSpec: &spec,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000545"), 0),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CreateJob(agent_run stale materialization) err = %v, want conflict", err)
	}
}

func TestAgentRunExecutionSpecRequiresCurrentSlotBinding(t *testing.T) {
	t.Parallel()

	resolver := defaultPlacementResolver()
	svc, repo := newTestServiceWithPlacementResolver(resolver)
	agentRunID := mustUUID("00000000-0000-0000-0000-000000000550")
	slot, err := svc.ReserveSlot(context.Background(), ReserveSlotInput{
		RuntimeProfile:        "agent/default",
		RuntimeMode:           enum.RuntimeModeFullEnv,
		WorkspacePolicyDigest: "sha256:workspace-policy",
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000551"), 0),
	})
	if err != nil {
		t.Fatalf("ReserveSlot(): %v", err)
	}
	spec := testAgentRunExecutionSpec(agentRunID, slot.ID)
	repo.workspaceMaterializations[spec.ExpectedMaterializationID] = entity.WorkspaceMaterialization{
		Base:        entity.Base{ID: spec.ExpectedMaterializationID, Version: 1, CreatedAt: testNow, UpdatedAt: testNow},
		SlotID:      slot.ID,
		Status:      enum.WorkspaceMaterializationStatusCompleted,
		Fingerprint: spec.ExpectedMaterializationFingerprint,
	}
	slotWithoutRun := readyAgentRunSlot(slot, spec)
	slotWithoutRun.AgentRunID = nil
	repo.slots[slot.ID] = slotWithoutRun
	_, err = svc.CreateJob(context.Background(), CreateJobInput{
		JobType:               enum.JobTypeAgentRun,
		Priority:              enum.JobPriorityHigh,
		AgentRunExecutionSpec: &spec,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000552"), 0),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CreateJob(agent_run nil slot binding) err = %v, want conflict", err)
	}

	otherRunID := mustUUID("00000000-0000-0000-0000-000000000553")
	mismatchedSlot := readyAgentRunSlot(slot, spec)
	mismatchedSlot.AgentRunID = &otherRunID
	repo.slots[slot.ID] = mismatchedSlot
	_, err = svc.CreateJob(context.Background(), CreateJobInput{
		JobType:               enum.JobTypeAgentRun,
		Priority:              enum.JobPriorityHigh,
		AgentRunExecutionSpec: &spec,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000554"), 0),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CreateJob(agent_run mismatched slot binding) err = %v, want conflict", err)
	}

	staleSlot := readyAgentRunSlot(slot, spec)
	staleSlot.Fingerprint = "sha256:stale"
	repo.slots[slot.ID] = staleSlot
	_, err = svc.CreateJob(context.Background(), CreateJobInput{
		JobType:               enum.JobTypeAgentRun,
		Priority:              enum.JobPriorityHigh,
		AgentRunExecutionSpec: &spec,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000555"), 0),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CreateJob(agent_run stale slot fingerprint) err = %v, want conflict", err)
	}

	failedSlot := readyAgentRunSlot(slot, spec)
	failedSlot.Status = enum.SlotStatusFailed
	repo.slots[slot.ID] = failedSlot
	_, err = svc.CreateJob(context.Background(), CreateJobInput{
		JobType:               enum.JobTypeAgentRun,
		Priority:              enum.JobPriorityHigh,
		AgentRunExecutionSpec: &spec,
		Meta:                  commandMeta(mustUUID("00000000-0000-0000-0000-000000000556"), 0),
	})
	if !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("CreateJob(agent_run failed slot) err = %v, want conflict", err)
	}
}

func TestAgentRunExecutionSpecPreservesCodexSessionExecutionSpec(t *testing.T) {
	t.Parallel()

	agentRunID := mustUUID("00000000-0000-0000-0000-000000000531")
	slotID := mustUUID("00000000-0000-0000-0000-000000000532")
	spec := testAgentRunExecutionSpec(agentRunID, slotID)
	codexSpec := testCodexSessionExecutionSpec(spec, agentRunID)
	spec.CodexSessionExecutionSpec = &codexSpec

	normalized, err := normalizeAgentRunExecutionSpec(spec)
	if err != nil {
		t.Fatalf("normalizeAgentRunExecutionSpec() err = %v", err)
	}
	payload, err := marshalAgentRunExecutionSpec(normalized)
	if err != nil {
		t.Fatalf("marshalAgentRunExecutionSpec() err = %v", err)
	}
	extracted, ok := AgentRunExecutionSpecFromJobInput(payload)
	if !ok || extracted.CodexSessionExecutionSpec == nil {
		t.Fatalf("AgentRunExecutionSpecFromJobInput() = %+v, %v", extracted, ok)
	}
	if extracted.CodexSessionExecutionSpec.InstructionObjectRef != spec.CodexSessionExecutionSpec.InstructionObjectRef {
		t.Fatalf("InstructionObjectRef = %q, want %q", extracted.CodexSessionExecutionSpec.InstructionObjectRef, spec.CodexSessionExecutionSpec.InstructionObjectRef)
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
	buildSpec := testBuildExecutionSpec("access-manager")
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:            enum.JobTypeBuild,
		Priority:           enum.JobPriorityNormal,
		SlotID:             &slot.ID,
		BuildExecutionSpec: &buildSpec,
		Meta:               commandMeta(mustUUID("00000000-0000-0000-0000-000000000541"), 0),
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
	buildSpec := testBuildExecutionSpec("access-manager")
	_, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:            enum.JobTypeBuild,
		Priority:           enum.JobPriorityNormal,
		BuildExecutionSpec: &buildSpec,
		Meta:               commandMeta(mustUUID("00000000-0000-0000-0000-000000000542"), 0),
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
	buildSpec := testBuildExecutionSpec("access-manager")

	_, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:            enum.JobTypeBuild,
		Priority:           enum.JobPriorityNormal,
		ProjectID:          &projectID,
		BuildExecutionSpec: &buildSpec,
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
		JobType:            enum.JobTypeBuild,
		Priority:           enum.JobPriorityNormal,
		ProjectID:          &projectID,
		BuildExecutionSpec: &buildSpec,
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
	buildSpec := testBuildExecutionSpec("access-manager")
	job, err := svc.CreateJob(context.Background(), CreateJobInput{
		JobType:            enum.JobTypeBuild,
		Priority:           enum.JobPriorityNormal,
		BuildExecutionSpec: &buildSpec,
		Meta:               commandMeta(mustUUID("00000000-0000-0000-0000-000000000524"), 0),
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

func testBuildExecutionSpec(serviceKey string) BuildExecutionSpecInput {
	return BuildExecutionSpecInput{
		SourceRef:            "git://github.com/codex-k8s/kodex",
		SourceCommitSHA:      "0123456789abcdef0123456789abcdef01234567",
		ServiceKey:           serviceKey,
		ImageRef:             "registry.local/kodex/" + serviceKey,
		ImageTag:             "0.1.0",
		BuildContextRef:      "stack://services/" + serviceKey + "/context",
		BuildContextDigest:   "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		DockerfileRef:        "stack://services/" + serviceKey + "/Dockerfile",
		DockerfileDigest:     "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		DockerfileTarget:     "prod",
		BuilderImageRef:      "image://kaniko-executor:1.24.0",
		BuildPlanFingerprint: "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
		AllowedSecretRefs: []RuntimeJobExecutionRefInput{
			{Kind: "registry", Ref: "secret://runtime/registry-push"},
		},
		OutputRefs: []RuntimeJobExecutionRefInput{
			{Kind: "image_ref", Ref: "runtime://artifacts/images/" + serviceKey},
		},
	}
}

func testDeployExecutionSpec(serviceKey string) DeployExecutionSpecInput {
	return DeployExecutionSpecInput{
		SourceRef:             "git://github.com/codex-k8s/kodex",
		SourceCommitSHA:       "0123456789abcdef0123456789abcdef01234567",
		ServiceKey:            serviceKey,
		ImageRef:              "registry.local/kodex/" + serviceKey,
		ImageTag:              "0.1.0",
		ImageDigest:           "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
		ManifestRef:           "manifest://deploy/base/" + serviceKey,
		ManifestDigest:        "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		KustomizationRef:      "kustomize://deploy/base/" + serviceKey,
		KustomizationDigest:   "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		TargetNamespace:       "kodex",
		TargetClusterRef:      "fleet://clusters/00000000-0000-0000-0000-000000000777",
		DeployPlanFingerprint: "sha256:9999999999999999999999999999999999999999999999999999999999999999",
		ManifestBundleRef:     "manifest-bundle://self-deploy/" + serviceKey,
		ManifestBundleDigest:  "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RolloutTargets: []DeployRolloutTargetInput{
			{
				Kind:      "deployment",
				Ref:       "k8s://deployments/" + serviceKey,
				Namespace: "kodex",
				Name:      serviceKey,
				Digest:    "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			},
		},
		ExpectedImageRefs: []DeployExpectedImageRefInput{
			{
				ContainerName: serviceKey,
				ImageRef:      "registry.local/kodex/" + serviceKey,
				ImageDigest:   "sha256:dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd",
			},
		},
		AllowedSecretRefs: []RuntimeJobExecutionRefInput{
			{Kind: "kubernetes", Ref: "secret://fleet/platform-default"},
		},
		OutputRefs: []RuntimeJobExecutionRefInput{
			{Kind: "rollout", Ref: "runtime://artifacts/deploy/" + serviceKey},
		},
	}
}

func testAgentRunExecutionSpec(agentRunID uuid.UUID, slotID uuid.UUID) AgentRunExecutionSpecInput {
	return AgentRunExecutionSpecInput{
		AgentRunID:                         agentRunID,
		SlotID:                             slotID,
		ExpectedMaterializationID:          mustUUID("00000000-0000-0000-0000-000000000548"),
		ExpectedMaterializationFingerprint: "sha256:materialized-workspace",
		WorkspaceRef:                       "runtime://workspace/00000000-0000-0000-0000-000000000549",
		WorkspaceMountRef:                  "mount://workspace/00000000-0000-0000-0000-000000000549",
		WorkspacePVCRef:                    "k8s://pvc/runtime-workspace-549",
		ContextRef:                         "runtime://workspace/00000000-0000-0000-0000-000000000549/.kodex/context/agent-run.json",
		ContextDigest:                      "sha256:agent-run-context",
		RunnerProfileRef:                   "runner-profile://codex-agent/default",
		RunnerImageRef:                     "image://codex-agent@sha256:runner",
		RunnerMode:                         enum.AgentRunRunnerModeCodexAgent,
		AllowedSecretRefs: []AgentRunExecutionRefInput{
			{Kind: "runtime_api", Ref: "secret://runtime/agent-token"},
		},
		ReportingTargetRefs: []AgentRunExecutionRefInput{
			{Kind: "agent_run_state", Ref: "agent-manager://runs/" + agentRunID.String()},
		},
	}
}

func testCodexSessionExecutionSpec(spec AgentRunExecutionSpecInput, agentRunID uuid.UUID) CodexSessionExecutionSpecInput {
	return CodexSessionExecutionSpecInput{
		InstructionObjectRef:    "workspace://.kodex/execution/instruction.txt",
		InstructionObjectDigest: "sha256:eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee",
		ResultSchemaRef:         "workspace://.kodex/execution/result.schema.json",
		ResultSchemaDigest:      "sha256:ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff",
		WorkspaceSnapshotRef:    "runtime://workspace-snapshots/agent-run-531",
		HookEndpointRef:         "hook://codex-hook-ingress/agent-runner",
		CallbackRefs: []AgentRunExecutionRefInput{
			{Kind: "agent_run_state", Ref: "agent-manager://runs/" + agentRunID.String()},
		},
		TimeoutSeconds:   1800,
		RunnerProfileRef: spec.RunnerProfileRef,
		RunnerMode:       enum.AgentRunRunnerModeCodexAgent,
		OutputRefs: []AgentRunExecutionRefInput{
			{Kind: "last_message", Ref: "object://codex-output/last-message"},
		},
		ResultRefs: []AgentRunExecutionRefInput{
			{Kind: "result_metadata", Ref: "object://codex-output/result-metadata"},
		},
		AllowedSecretRefs: []AgentRunExecutionRefInput{
			{Kind: "runtime_api", Ref: "secret://runtime/agent-token"},
		},
	}
}

func readyAgentRunSlot(slot entity.Slot, spec AgentRunExecutionSpecInput) entity.Slot {
	slot.Status = enum.SlotStatusReady
	slot.AgentRunID = &spec.AgentRunID
	slot.Fingerprint = spec.ExpectedMaterializationFingerprint
	return slot
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
