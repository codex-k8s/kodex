package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	eventconsumer "github.com/codex-k8s/kodex/libs/go/eventconsumer"
	eventlog "github.com/codex-k8s/kodex/libs/go/eventlog"
	providerevents "github.com/codex-k8s/kodex/libs/go/platformevents/provider"
	"github.com/google/uuid"

	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/errs"
	agentservice "github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/service"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/agent-manager/internal/domain/types/enum"
)

func TestSelfDeploySignalEventHandlerCreatesPlanFromProjectSignal(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("11111111-2222-4333-8444-555555555555")
	repositoryID := uuid.MustParse("22222222-3333-4444-8555-666666666666")
	reader := &fakeSelfDeploySignalReader{result: readySelfDeploySignal(projectID, repositoryID)}
	creator := &fakeSelfDeployPlanCreator{}
	handler := selfDeploySignalEventHandler{reader: reader, creator: creator}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
		RepositoryChangeSignalID: uuid.NewString(),
		SignalKey:                "provider:github:repository_change:push:codex-k8s/kodex:main:abc123",
		ProjectID:                projectID.String(),
		RepositoryID:             repositoryID.String(),
		BaseBranch:               "main",
		ServicesPolicyChanged:    true,
		DeployRelevantChanged:    true,
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if len(creator.inputs) != 1 {
		t.Fatalf("created inputs = %d, want 1", len(creator.inputs))
	}
	input := creator.inputs[0].CreateSelfDeployPlanInput
	if input.ProviderSignalRef != "provider:github:repository_change:push:codex-k8s/kodex:main:abc123" ||
		input.ServicesYAMLDigest != "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("plan input signal/digest = %q/%q", input.ProviderSignalRef, input.ServicesYAMLDigest)
	}
	if len(input.PathCategories) != 2 || input.PathCategories[0] != enum.SelfDeployPathCategoryServicesPolicy {
		t.Fatalf("path categories = %v", input.PathCategories)
	}
	if input.Meta.IdempotencyKey != "self_deploy_signal:"+input.ProviderSignalRef {
		t.Fatalf("idempotency key = %q", input.Meta.IdempotencyKey)
	}
	if input.GovernanceContext.GatePolicyRef != "governance:gate_policy/self_deploy.owner_gate" {
		t.Fatalf("gate policy ref = %q", input.GovernanceContext.GatePolicyRef)
	}
}

func TestSelfDeploySignalEventHandlerCreatesPlanFromLiveReadySignalShape(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("63135040-fe44-4ec4-83d5-b0126dc23b32")
	repositoryID := uuid.MustParse("f287091b-5992-435d-a7d6-b5acb70e8fcc")
	commitSHA := "3144658f8e2918c8e66086783ed0b496b08c6f48"
	reader := &fakeSelfDeploySignalReader{result: agentservice.SelfDeploySignalReadResult{
		Status: agentservice.SelfDeploySignalStatusReady,
		Signal: agentservice.SelfDeploySignal{
			ProviderSignalRef:       "provider:github:repository_change:push:codex-k8s/kodex:main:" + commitSHA,
			ProviderSignalKey:       "provider:github:repository_change:push:codex-k8s/kodex:main:" + commitSHA,
			ProjectRef:              projectID.String(),
			RepositoryRef:           repositoryID.String(),
			ProviderSlug:            "github",
			RepositoryFullName:      "codex-k8s/kodex",
			SourceRef:               "refs/heads/main",
			MergeCommitSHA:          commitSHA,
			ServicesYAML:            agentservice.SelfDeploySignalServicesYAML{Ref: "project-catalog:services-policy/active:services.yaml", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			AffectedServiceKeys:     []string{"agent-manager", "project-catalog", "runtime-manager"},
			PathCategories:          []enum.SelfDeployPathCategory{enum.SelfDeployPathCategoryDocumentation, enum.SelfDeployPathCategoryOther},
			ExpectedRuntimeJobTypes: []enum.SelfDeployRuntimeJobType{enum.SelfDeployRuntimeJobTypeBuild, enum.SelfDeployRuntimeJobTypeDeploy, enum.SelfDeployRuntimeJobTypeHealthCheck},
			GovernanceRequirement:   agentservice.SelfDeployGovernanceRequirement{GateRequired: true, GatePolicyRef: "self_deploy.owner_gate"},
			SafeSummary:             "self-deploy signal ready; services=agent-manager,project-catalog,runtime-manager; deploy_relevant_changed=true",
			Version:                 1,
		},
	}}
	creator := &fakeSelfDeployPlanCreator{}
	handler := selfDeploySignalEventHandler{
		cfg:     Config{SelfDeploySignalConsumerProjectID: projectID.String(), SelfDeploySignalConsumerRepositoryID: repositoryID.String(), SelfDeploySignalConsumerTargetBranch: "main"},
		reader:  reader,
		creator: creator,
	}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
		SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:" + commitSHA,
		BaseBranch:            "main",
		DeployRelevantChanged: true,
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if len(creator.inputs) != 1 {
		t.Fatalf("created inputs = %d, want 1", len(creator.inputs))
	}
	input := creator.inputs[0].CreateSelfDeployPlanInput
	if input.ProjectRef != projectID.String() ||
		input.RepositoryRef != repositoryID.String() ||
		input.MergeCommitSHA != commitSHA {
		t.Fatalf("plan refs = project %q repository %q commit %q", input.ProjectRef, input.RepositoryRef, input.MergeCommitSHA)
	}
	if len(input.AffectedServiceKeys) != 3 ||
		len(input.PathCategories) != 2 ||
		len(input.ExpectedRuntimeJobTypes) != 3 {
		t.Fatalf("plan classified input = services %v categories %v jobs %v", input.AffectedServiceKeys, input.PathCategories, input.ExpectedRuntimeJobTypes)
	}
	if input.GovernanceContext.GatePolicyRef != "governance:gate_policy/self_deploy.owner_gate" {
		t.Fatalf("gate policy ref = %q", input.GovernanceContext.GatePolicyRef)
	}
}

func TestSelfDeploySignalEventHandlerRejectsUnsafeGovernancePolicyKey(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	repositoryID := uuid.New()
	reader := &fakeSelfDeploySignalReader{result: readySelfDeploySignal(projectID, repositoryID)}
	reader.result.Signal.GovernanceRequirement.GatePolicyRef = "secret_value"
	creator := &fakeSelfDeployPlanCreator{}
	handler := selfDeploySignalEventHandler{reader: reader, creator: creator}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
		SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:abc123",
		ProjectID:             projectID.String(),
		BaseBranch:            "main",
		DeployRelevantChanged: true,
	})})
	if result.Status != eventconsumer.ResultPoison || result.Code != "invalid_self_deploy_signal" {
		t.Fatalf("HandleEvent() = %+v, want invalid poison", result)
	}
	if len(creator.inputs) != 0 {
		t.Fatalf("created inputs = %d, want 0", len(creator.inputs))
	}
}

func TestSelfDeploySignalEventHandlerUsesConfiguredProjectScope(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("33333333-4444-4555-8666-777777777777")
	reader := &fakeSelfDeploySignalReader{result: readySelfDeploySignal(projectID, uuid.New())}
	creator := &fakeSelfDeployPlanCreator{}
	handler := selfDeploySignalEventHandler{
		cfg:     Config{SelfDeploySignalConsumerProjectID: projectID.String(), SelfDeploySignalConsumerTargetBranch: "main"},
		reader:  reader,
		creator: creator,
	}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
		SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:def456",
		BaseBranch:            "main",
		DeployRelevantChanged: true,
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if reader.inputs[0].ProjectID != projectID {
		t.Fatalf("lookup project id = %s, want %s", reader.inputs[0].ProjectID, projectID)
	}
}

func TestSelfDeploySignalEventHandlerRetriesNonReadyProjectSignal(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	reader := &fakeSelfDeploySignalReader{result: agentservice.SelfDeploySignalReadResult{
		Status:     agentservice.SelfDeploySignalStatusNeedsServicesPolicyReconcile,
		SafeReason: "services_policy_commit_not_reconciled",
	}}
	handler := selfDeploySignalEventHandler{reader: reader, creator: &fakeSelfDeployPlanCreator{}}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
		SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:abc123",
		ProjectID:             projectID.String(),
		BaseBranch:            "main",
		DeployRelevantChanged: true,
	})})
	if result.Status != eventconsumer.ResultRetry || result.Code != "retryable" {
		t.Fatalf("HandleEvent() = %+v, want retry", result)
	}
}

func TestSelfDeploySignalEventHandlerSkipsStaleProviderSignal(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	cases := []struct {
		name   string
		reader *fakeSelfDeploySignalReader
	}{
		{
			name: "safe status",
			reader: &fakeSelfDeploySignalReader{result: agentservice.SelfDeploySignalReadResult{
				Status:     agentservice.SelfDeploySignalStatusProviderSignalNotFound,
				SafeReason: "provider_signal_not_found",
			}},
		},
		{
			name:   "read not found",
			reader: &fakeSelfDeploySignalReader{err: errs.ErrNotFound},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			creator := &fakeSelfDeployPlanCreator{}
			handler := selfDeploySignalEventHandler{reader: tc.reader, creator: creator}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
				SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:stale",
				ProjectID:             projectID.String(),
				BaseBranch:            "main",
				DeployRelevantChanged: true,
			})})
			if result.Status != eventconsumer.ResultPoison || result.Code != "stale_provider_signal" {
				t.Fatalf("HandleEvent() = %+v, want stale provider poison", result)
			}
			if len(creator.inputs) != 0 {
				t.Fatalf("created inputs = %d, want 0", len(creator.inputs))
			}
		})
	}
}

func TestSelfDeploySignalEventHandlerIgnoresNotDeployRelevantEvent(t *testing.T) {
	t.Parallel()

	reader := &fakeSelfDeploySignalReader{}
	creator := &fakeSelfDeployPlanCreator{}
	handler := selfDeploySignalEventHandler{reader: reader, creator: creator}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
		SignalKey:  "provider:github:repository_change:push:codex-k8s/kodex:main:abc123",
		ProjectID:  uuid.NewString(),
		BaseBranch: "main",
	})})
	if result.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent() = %+v, want ack", result)
	}
	if len(reader.inputs) != 0 || len(creator.inputs) != 0 {
		t.Fatalf("reader inputs = %d, creator inputs = %d, want 0", len(reader.inputs), len(creator.inputs))
	}
}

func TestSelfDeploySignalEventHandlerRequiresDeployRelevantTriggerBeforeProjectRead(t *testing.T) {
	t.Parallel()

	projectID := uuid.MustParse("44444444-5555-4666-8777-888888888888")
	repositoryID := uuid.MustParse("55555555-6666-4777-8888-999999999999")
	reader := &fakeSelfDeploySignalReader{result: readySelfDeploySignal(projectID, repositoryID)}
	creator := &fakeSelfDeployPlanCreator{}
	handler := selfDeploySignalEventHandler{
		cfg: Config{
			SelfDeploySignalConsumerProjectID:    projectID.String(),
			SelfDeploySignalConsumerRepositoryID: repositoryID.String(),
			SelfDeploySignalConsumerTargetBranch: "main",
		},
		reader:  reader,
		creator: creator,
	}

	docsOnly := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
		SignalKey:  "provider:github:repository_change:push:codex-k8s/kodex:main:docs-only",
		BaseBranch: "main",
	})})
	if docsOnly.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent(docs-only) = %+v, want ack", docsOnly)
	}
	if len(reader.inputs) != 0 || len(creator.inputs) != 0 {
		t.Fatalf("docs-only reader inputs = %d, creator inputs = %d, want 0", len(reader.inputs), len(creator.inputs))
	}

	deployRelevant := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
		SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:deploy-relevant",
		BaseBranch:            "main",
		DeployRelevantChanged: true,
	})})
	if deployRelevant.Status != eventconsumer.ResultAck {
		t.Fatalf("HandleEvent(deploy-relevant) = %+v, want ack", deployRelevant)
	}
	if len(reader.inputs) != 1 || len(creator.inputs) != 1 {
		t.Fatalf("deploy-relevant reader inputs = %d, creator inputs = %d, want 1", len(reader.inputs), len(creator.inputs))
	}
	if reader.inputs[0].ProjectID != projectID {
		t.Fatalf("lookup project id = %s, want %s", reader.inputs[0].ProjectID, projectID)
	}
	if reader.inputs[0].RepositoryID == nil || *reader.inputs[0].RepositoryID != repositoryID {
		t.Fatalf("lookup repository id = %v, want %s", reader.inputs[0].RepositoryID, repositoryID)
	}
	input := creator.inputs[0].CreateSelfDeployPlanInput
	if input.GovernanceContext.GatePolicyRef != "governance:gate_policy/self_deploy.owner_gate" {
		t.Fatalf("gate policy ref = %q", input.GovernanceContext.GatePolicyRef)
	}
}

func TestSelfDeploySignalEventHandlerPoisonsMissingProjectRef(t *testing.T) {
	t.Parallel()

	handler := selfDeploySignalEventHandler{reader: &fakeSelfDeploySignalReader{}, creator: &fakeSelfDeployPlanCreator{}}

	result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
		SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:abc123",
		BaseBranch:            "main",
		DeployRelevantChanged: true,
	})})
	if result.Status != eventconsumer.ResultPoison || result.Code != "missing_self_deploy_project_ref" {
		t.Fatalf("HandleEvent() = %+v, want missing project poison", result)
	}
}

func TestSelfDeploySignalEventHandlerMapsPlanReplayAndConflict(t *testing.T) {
	t.Parallel()

	projectID := uuid.New()
	repositoryID := uuid.New()
	cases := []struct {
		name   string
		err    error
		status eventconsumer.ResultStatus
		code   string
	}{
		{name: "replay", err: nil, status: eventconsumer.ResultAck, code: ""},
		{name: "conflict", err: errs.ErrConflict, status: eventconsumer.ResultPoison, code: "conflicting_self_deploy_plan_signal"},
		{name: "temporary", err: errors.New("database unavailable"), status: eventconsumer.ResultRetry, code: "retryable"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			handler := selfDeploySignalEventHandler{
				reader:  &fakeSelfDeploySignalReader{result: readySelfDeploySignal(projectID, repositoryID)},
				creator: &fakeSelfDeployPlanCreator{err: tc.err},
			}
			result := handler.HandleEvent(context.Background(), eventconsumer.Event{StoredEvent: selfDeploySignalStoredEvent(t, providerevents.Payload{
				SignalKey:             "provider:github:repository_change:push:codex-k8s/kodex:main:abc123",
				ProjectID:             projectID.String(),
				BaseBranch:            "main",
				DeployRelevantChanged: true,
			})})
			if result.Status != tc.status || result.Code != tc.code {
				t.Fatalf("HandleEvent() = %+v, want %s/%s", result, tc.status, tc.code)
			}
		})
	}
}

func readySelfDeploySignal(projectID uuid.UUID, repositoryID uuid.UUID) agentservice.SelfDeploySignalReadResult {
	return agentservice.SelfDeploySignalReadResult{
		Status: agentservice.SelfDeploySignalStatusReady,
		Signal: agentservice.SelfDeploySignal{
			ProviderSignalRef:       "provider:github:repository_change:push:codex-k8s/kodex:main:abc123",
			ProviderSignalKey:       "provider:github:repository_change:push:codex-k8s/kodex:main:abc123",
			ProjectRef:              projectID.String(),
			RepositoryRef:           repositoryID.String(),
			ProviderSlug:            "github",
			RepositoryFullName:      "codex-k8s/kodex",
			SourceRef:               "refs/heads/main",
			MergeCommitSHA:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			ServicesYAML:            agentservice.SelfDeploySignalServicesYAML{Ref: "project-catalog:services-policy:policy-1:services.yaml", Digest: "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
			AffectedServiceKeys:     []string{"agent-manager"},
			PathCategories:          []enum.SelfDeployPathCategory{enum.SelfDeployPathCategoryServicesPolicy, enum.SelfDeployPathCategoryDeployManifest},
			ExpectedRuntimeJobTypes: []enum.SelfDeployRuntimeJobType{enum.SelfDeployRuntimeJobTypeBuild, enum.SelfDeployRuntimeJobTypeDeploy, enum.SelfDeployRuntimeJobTypeHealthCheck},
			GovernanceRequirement:   agentservice.SelfDeployGovernanceRequirement{GateRequired: true, GatePolicyRef: "self_deploy.owner_gate"},
			SafeSummary:             "self-deploy signal ready",
			Version:                 1,
		},
	}
}

func selfDeploySignalStoredEvent(t *testing.T, payload providerevents.Payload) eventlog.StoredEvent {
	t.Helper()

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return eventlog.StoredEvent{
		SequenceID: 1,
		Event: eventlog.Event{
			ID:            uuid.New(),
			EventType:     providerevents.EventRepositoryChanged,
			SourceService: selfDeploySignalSourceService,
			AggregateType: providerevents.AggregateRepositoryChangeSignal,
			AggregateID:   uuid.New(),
			SchemaVersion: providerevents.SchemaVersion,
			Payload:       encoded,
			OccurredAt:    time.Now().UTC(),
		},
		RecordedAt: time.Now().UTC(),
	}
}

type fakeSelfDeploySignalReader struct {
	inputs []agentservice.SelfDeploySignalLookupInput
	result agentservice.SelfDeploySignalReadResult
	err    error
}

func (f *fakeSelfDeploySignalReader) GetSelfDeploySignal(_ context.Context, input agentservice.SelfDeploySignalLookupInput) (agentservice.SelfDeploySignalReadResult, error) {
	f.inputs = append(f.inputs, input)
	return f.result, f.err
}

type fakeSelfDeployPlanCreator struct {
	inputs []agentservice.CreateSelfDeployPlanFromSignalInput
	err    error
}

func (f *fakeSelfDeployPlanCreator) CreateSelfDeployPlanFromSignal(_ context.Context, input agentservice.CreateSelfDeployPlanFromSignalInput) (entity.SelfDeployPlan, error) {
	f.inputs = append(f.inputs, input)
	return entity.SelfDeployPlan{}, f.err
}
