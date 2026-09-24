package platform

import (
	"context"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testEnvironmentDraft(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Draft owner", CallerWorkload: "control-api-gateway", Operation: "platform.command.runtime-environment-drafts.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	var imageRef, projectRef string
	if err := pool.QueryRow(ctx, `SELECT image.ref, project.ref FROM control_plane.image_artifacts image JOIN control_plane.projects project ON project.id = image.project_id
WHERE project.name = 'Role image promotion' AND image.promotion_state = 'PROMOTED' AND image.admission_state = 'ACCEPTED' ORDER BY image.ref LIMIT 1`).Scan(&imageRef, &projectRef); err != nil {
		t.Fatal(err)
	}
	invoke := func(kind command.Kind, key string, version *int64, payload command.RuntimeEnvironmentDraftInput) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: version}, Payload: payload})
	}
	created, err := invoke(command.CreateRuntimeEnvironmentDraft, "draft-create-incomplete", nil, command.RuntimeEnvironmentDraftInput{ProjectRef: projectRef})
	if err != nil || created.RuntimeEnvironmentDraft == nil || created.RuntimeEnvironmentDraft.State != "DRAFT" {
		t.Fatalf("create draft: %v", err)
	}
	draft := created.RuntimeEnvironmentDraft
	if draft.SavedAt.IsZero() || draft.BaseVersionRef != "" || draft.BaseRevision != 0 {
		t.Fatal("new draft provenance mismatch")
	}
	invalid, err := invoke(command.ValidateRuntimeEnvironmentDraft, "draft-validate-incomplete", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil || invalid.RuntimeEnvironmentDraft.State != "INVALID" {
		t.Fatalf("invalid draft validation: %v", err)
	}
	draft = invalid.RuntimeEnvironmentDraft
	if !draft.SavedAt.Equal(created.RuntimeEnvironmentDraft.SavedAt) {
		t.Fatal("validation changed saved timestamp")
	}
	spec := entity.RuntimeEnvironmentDraftSpecification{Name: "Validated draft environment", ImageArtifactRef: imageRef,
		Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy(), Values: []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "draft"}}}
	stale := draft.Version - 1
	if _, err := invoke(command.SaveRuntimeEnvironmentDraft, "draft-stale-save", &stale, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, Specification: spec}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale draft save: %v", err)
	}
	saved, err := invoke(command.SaveRuntimeEnvironmentDraft, "draft-save-complete", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, Specification: spec})
	if err != nil {
		t.Fatal(err)
	}
	draft = saved.RuntimeEnvironmentDraft
	if !draft.SavedAt.After(created.RuntimeEnvironmentDraft.SavedAt) {
		t.Fatal("save did not advance timestamp")
	}
	valid, err := invoke(command.ValidateRuntimeEnvironmentDraft, "draft-validate-complete", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil || valid.RuntimeEnvironmentDraft.State != "VALID" || valid.RuntimeEnvironmentDraft.ValidationDigest == "" {
		t.Fatalf("validate complete: %v", err)
	}
	draft = valid.RuntimeEnvironmentDraft
	if !draft.SavedAt.Equal(saved.RuntimeEnvironmentDraft.SavedAt) {
		t.Fatal("validation changed saved timestamp")
	}
	publicationVersion := draft.Version
	prepared, err := invoke(command.PrepareEnvironmentDraftImpact, "draft-impact-new", &publicationVersion, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil || prepared.RevisionImpactPlan == nil || prepared.RevisionImpactPlan.Total != 0 {
		t.Fatalf("prepare new environment impact: %v", err)
	}
	publishInput := command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: prepared.RevisionImpactPlan.Ref, SelectedItemRefs: []string{}}
	published, err := invoke(command.PublishRuntimeEnvironmentDraft, "draft-publish-complete", &publicationVersion, publishInput)
	if err != nil || published.RuntimeEnvironmentDraft.State != "PUBLISHED" || published.RuntimeEnvironment == nil || published.RuntimeEnvironment.CurrentVersion.Digest != draft.ValidationDigest {
		t.Fatalf("publish draft: %v", err)
	}
	replay, err := invoke(command.PublishRuntimeEnvironmentDraft, "draft-publish-complete", &publicationVersion, publishInput)
	if err != nil || replay.RuntimeEnvironment.Ref != published.RuntimeEnvironment.Ref {
		t.Fatalf("publication replay: %v", err)
	}
	if !published.RuntimeEnvironmentDraft.SavedAt.Equal(draft.SavedAt) || !replay.RuntimeEnvironmentDraft.SavedAt.Equal(draft.SavedAt) {
		t.Fatal("publication or replay changed saved timestamp")
	}
	if _, err := invoke(command.SaveRuntimeEnvironmentDraft, "draft-save-published", &published.RuntimeEnvironmentDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, Specification: spec}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("published draft was edited: %v", err)
	}
	readback, err := service.GetRuntimeEnvironmentDraft(ctx, owner, draft.Ref)
	if err != nil || readback.PublishedEnvironmentRef != published.RuntimeEnvironment.Ref {
		t.Fatalf("draft readback: %v", err)
	}
	target := published.RuntimeEnvironment
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	assistantTx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	assistantOperation, err := repository.hydrateAssistantEnvironmentOperation(ctx, assistantTx, ownerScope, projectRef,
		entity.AssistantPlanOperation{Type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION", Key: "draft-assistant-revision",
			Title: "Revise environment", Summary: "Revise environment", Parameters: map[string]any{
				"environmentRef": target.Ref, "description": "Assistant revision",
			}})
	if err != nil {
		_ = assistantTx.Rollback(ctx)
		t.Fatalf("hydrate exact environment revision: %v", err)
	}
	matching, err := repository.assistantEnvironmentSnapshotMatches(ctx, assistantTx, ownerScope, projectRef, assistantOperation)
	if err != nil || !matching || assistantOperation.Target.Ref != target.Ref {
		_ = assistantTx.Rollback(ctx)
		t.Fatalf("environment revision snapshot: matching=%v err=%v", matching, err)
	}
	if err := assistantTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assistantOperation, err = normalizeAssistantOperation(assistantOperation)
	if err != nil {
		t.Fatal(err)
	}
	planned, err := assistantOperationCommand(assistantOperation)
	if err != nil {
		t.Fatal(err)
	}
	planned.Principal = owner
	planned.Mutation.IdempotencyKey = "draft-assistant-revision-apply"
	assistantDraft, err := service.Execute(ctx, planned)
	if err != nil || assistantDraft.RuntimeEnvironmentDraft == nil ||
		assistantDraft.RuntimeEnvironmentDraft.EnvironmentRef != target.Ref ||
		assistantDraft.RuntimeEnvironmentDraft.BaseVersionRef != target.CurrentVersion.Ref ||
		assistantDraft.RuntimeEnvironmentDraft.Specification.Description != "Assistant revision" ||
		len(assistantDraft.RuntimeEnvironmentDraft.Specification.Values) != len(spec.Values) {
		t.Fatalf("assistant revision did not preserve the exact base: %#v %v", assistantDraft.RuntimeEnvironmentDraft, err)
	}
	unchanged, err := service.GetRuntimeEnvironment(ctx, owner, target.Ref)
	if err != nil || unchanged.Version != target.Version || unchanged.Description != target.Description {
		t.Fatalf("assistant revision changed published environment: %#v %v", unchanged, err)
	}
	change, err := invoke(command.CreateRuntimeEnvironmentDraft, "draft-target-create", nil, command.RuntimeEnvironmentDraftInput{
		ProjectRef: projectRef, EnvironmentRef: target.Ref, ExpectedEnvironmentVersion: target.Version, Specification: spec})
	if err != nil {
		t.Fatal(err)
	}
	changeDraft := change.RuntimeEnvironmentDraft
	if changeDraft.BaseVersionRef != target.CurrentVersion.Ref || changeDraft.BaseRevision != target.CurrentVersion.Revision {
		t.Fatal("draft omitted exact immutable base")
	}
	checked, err := invoke(command.ValidateRuntimeEnvironmentDraft, "draft-target-validate", &changeDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: changeDraft.Ref})
	if err != nil || checked.RuntimeEnvironmentDraft.State != "VALID" {
		t.Fatalf("target draft validation: %v", err)
	}
	stalePlan, err := invoke(command.PrepareEnvironmentDraftImpact, "draft-impact-before-stale", &checked.RuntimeEnvironmentDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: changeDraft.Ref})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.PublishRuntimeEnvironment, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "draft-target-concurrent-publish", ExpectedVersion: &target.Version},
		Payload:  environmentDraftPayload(*checked.RuntimeEnvironmentDraft)}); err != nil {
		t.Fatal(err)
	}
	if _, err := invoke(command.PublishRuntimeEnvironmentDraft, "draft-target-stale-publish", &checked.RuntimeEnvironmentDraft.Version,
		command.RuntimeEnvironmentDraftInput{DraftRef: changeDraft.Ref, PlanRef: stalePlan.RevisionImpactPlan.Ref}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale target publication: %v", err)
	}
	discarded, err := invoke(command.DiscardRuntimeEnvironmentDraft, "draft-target-discard", &checked.RuntimeEnvironmentDraft.Version,
		command.RuntimeEnvironmentDraftInput{DraftRef: changeDraft.Ref})
	if err != nil || discarded.RuntimeEnvironmentDraft.State != "DISCARDED" {
		t.Fatalf("discard draft: %v", err)
	}
	retained, err := service.GetRuntimeEnvironmentDraft(ctx, owner, changeDraft.Ref)
	if err != nil || retained.BaseVersionRef != changeDraft.BaseVersionRef || retained.BaseRevision != changeDraft.BaseRevision || !retained.SavedAt.Equal(changeDraft.SavedAt) {
		t.Fatalf("draft provenance changed after concurrent publication and discard: %v", err)
	}
	firstAgent := createLifecycleAgent(t, ctx, service, owner, projectRef, "draft-pin-first", "Draft pin first")
	secondAgent := createLifecycleAgent(t, ctx, service, owner, projectRef, "draft-pin-second", "Draft pin second")
	bind := func(agent entity.Agent, version int64, revision, key string) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: command.BindAgentRuntimeEnvironment, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &version},
			Payload:  command.RuntimeEnvironmentBindingInput{AgentRef: agent.Ref, EnvironmentRef: target.Ref, VersionRef: revision}})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	boundFirst := bind(firstAgent, firstAgent.Version, target.CurrentVersion.Ref, "draft-pin-bind-first")
	bind(secondAgent, secondAgent.Version, target.CurrentVersion.Ref, "draft-pin-bind-second")
	current, err := service.GetRuntimeEnvironment(ctx, owner, target.Ref)
	if err != nil {
		t.Fatal(err)
	}
	changed := environmentDraftPayload(*checked.RuntimeEnvironmentDraft)
	changed.Values = []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "new-revision"}}
	newVersion, err := service.Execute(ctx, command.Command{Kind: command.PublishRuntimeEnvironment, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "draft-pin-publish", ExpectedVersion: &current.Version}, Payload: changed})
	if err != nil {
		t.Fatal(err)
	}
	for _, agent := range []entity.Agent{firstAgent, secondAgent} {
		view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
		if err != nil || view.EnvironmentBinding.VersionRef != target.CurrentVersion.Ref || view.Environment.CurrentVersion.Digest != target.CurrentVersion.Digest {
			t.Fatalf("publication changed pinned consumer: %v", err)
		}
	}
	impact, err := service.GetRuntimeEnvironmentImpact(ctx, owner, target.Ref, newVersion.RuntimeEnvironment.CurrentVersion.Ref, "", query.Page{Size: 1})
	if err != nil || impact.Total != 2 || len(impact.Consumers) != 1 || impact.NextPageToken == "" {
		t.Fatalf("impact first page: %#v %v", impact, err)
	}
	lastPage, err := service.GetRuntimeEnvironmentImpact(ctx, owner, target.Ref, newVersion.RuntimeEnvironment.CurrentVersion.Ref, "", query.Page{Size: 1, Token: impact.NextPageToken})
	if err != nil || lastPage.Total != 2 || len(lastPage.Consumers) != 1 || lastPage.NextPageToken != "" || lastPage.Consumers[0].AgentRef == impact.Consumers[0].AgentRef {
		t.Fatalf("impact last page: %#v %v", lastPage, err)
	}
	if _, err := service.GetRuntimeEnvironmentImpact(ctx, owner, target.Ref, target.CurrentVersion.Ref, "", query.Page{Size: 1, Token: impact.NextPageToken}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("impact cursor crossed target revision: %v", err)
	}
	if _, err := service.GetRuntimeEnvironmentImpact(ctx, owner, target.Ref, impact.TargetVersionRef, "changed", query.Page{Size: 1, Token: impact.NextPageToken}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("impact cursor crossed search: %v", err)
	}
	filtered, err := service.GetRuntimeEnvironmentImpact(ctx, owner, target.Ref, impact.TargetVersionRef, impact.Consumers[0].AgentRef, query.Page{Size: 1})
	if err != nil || filtered.Total != 1 || len(filtered.Consumers) != 1 || filtered.NextPageToken != "" {
		t.Fatalf("environment impact SQL search: total=%d err=%v", filtered.Total, err)
	}
	selections := append(impact.Consumers, lastPage.Consumers...)
	staleSelections := append([]entity.RuntimeEnvironmentConsumer(nil), selections...)
	staleSelections[1].BindingVersion++
	batch := command.Command{Kind: command.RebindRuntimeEnvironment, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "draft-pin-stale-batch", ExpectedVersion: &impact.EnvironmentVersion},
		Payload:  command.RuntimeEnvironmentRebindInput{EnvironmentRef: target.Ref, VersionRef: impact.TargetVersionRef, Consumers: staleSelections}}
	if _, err := service.Execute(ctx, batch); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale selected batch accepted: %v", err)
	}
	for _, agent := range []entity.Agent{firstAgent, secondAgent} {
		view, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
		if err != nil || view.EnvironmentBinding.VersionRef != target.CurrentVersion.Ref {
			t.Fatalf("failed batch was partially committed: %v", err)
		}
	}
	var selected entity.RuntimeEnvironmentConsumer
	for _, item := range selections {
		if item.AgentRef == firstAgent.Ref {
			selected = item
		}
	}
	if selected.AgentVersion != boundFirst.RuntimeConfiguration.AgentVersion {
		t.Fatal("impact agent version mismatch")
	}
	batch.Mutation.IdempotencyKey = "draft-pin-selected-rebind"
	batch.Payload = command.RuntimeEnvironmentRebindInput{EnvironmentRef: target.Ref, VersionRef: impact.TargetVersionRef, Consumers: []entity.RuntimeEnvironmentConsumer{selected}}
	rebound, err := service.Execute(ctx, batch)
	if err != nil || len(rebound.EnvironmentBindings) != 1 || rebound.EnvironmentBindings[0].VersionRef != newVersion.RuntimeEnvironment.CurrentVersion.Ref {
		t.Fatal("selected consumer did not move to exact revision")
	}
	replayed, err := service.Execute(ctx, batch)
	if err != nil || len(replayed.EnvironmentBindings) != 1 || replayed.EnvironmentBindings[0] != rebound.EnvironmentBindings[0] {
		t.Fatalf("batch replay: %v", err)
	}
	untouched, err := service.GetAgentRuntimeConfiguration(ctx, owner, secondAgent.Ref)
	if err != nil || untouched.EnvironmentBinding.VersionRef != target.CurrentVersion.Ref {
		t.Fatalf("unselected consumer changed: %v", err)
	}
	testEnvironmentPrepublicationImpact(t, ctx, repository, service, owner, projectRef, target.Ref, spec, firstAgent.Ref, secondAgent.Ref)
	thirdAgent := createLifecycleAgent(t, ctx, service, owner, projectRef, "draft-assistant-bind-agent", "Assistant bind agent")
	freshTarget, err := service.GetRuntimeEnvironment(ctx, owner, target.Ref)
	if err != nil {
		t.Fatal(err)
	}
	assistantBindingTx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	assistantBinding, err := repository.hydrateAssistantAgentEnvironmentBinding(ctx, assistantBindingTx, ownerScope, projectRef,
		entity.AssistantPlanOperation{Type: "BIND_AGENT_RUNTIME_ENVIRONMENT", Key: "draft-assistant-bind",
			Title: "Bind ready environment", Summary: "Bind exact current version",
			Parameters: map[string]any{"agentRef": thirdAgent.Ref, "environmentRef": target.Ref}})
	if err != nil {
		projection, projectionErr := repository.resolveAssistantContext(ctx, assistantBindingTx, ownerScope,
			entity.AssistantContextDescriptor{EntityKind: "AGENT", EntityRef: thirdAgent.Ref}, projectRef)
		_ = assistantBindingTx.Rollback(ctx)
		t.Fatalf("hydrate assistant binding: %v (context=%v allowed=%v)", err, projectionErr, projection.AllowedOperations)
	}
	matching, err = repository.assistantAgentBindingSnapshotMatches(ctx, assistantBindingTx, ownerScope, projectRef, assistantBinding)
	if err != nil || !matching || assistantString(assistantBinding.After, "versionRef") != freshTarget.CurrentVersion.Ref {
		_ = assistantBindingTx.Rollback(ctx)
		t.Fatalf("assistant binding snapshot: %v %v", matching, err)
	}
	forgedEdit := assistantBinding
	forgedEdit.Parameters = cloneAssistantFields(assistantBinding.Parameters)
	forgedEdit.Parameters["secretValue"] = "forged"
	if _, err := repository.rehydrateEditedAssistantBinding(ctx, assistantBindingTx, ownerScope, projectRef,
		assistantBinding, forgedEdit); !errors.Is(err, errs.ErrForbidden) {
		_ = assistantBindingTx.Rollback(ctx)
		t.Fatalf("binding edit accepted a secret field: %v", err)
	}
	forgedEdit.Parameters = cloneAssistantFields(assistantBinding.Parameters)
	forgedEdit.Parameters["versionRef"] = "renvv_forged"
	rehydrated, err := repository.rehydrateEditedAssistantBinding(ctx, assistantBindingTx, ownerScope, projectRef,
		assistantBinding, forgedEdit)
	if err != nil || assistantString(rehydrated.Parameters, "versionRef") != freshTarget.CurrentVersion.Ref {
		_ = assistantBindingTx.Rollback(ctx)
		t.Fatalf("binding edit did not restore server version: %v", err)
	}
	forgedEdit.Parameters = cloneAssistantFields(assistantBinding.Parameters)
	forgedEdit.Parameters["environmentRef"] = assistantString(assistantBinding.Before, "environmentRef")
	if _, err := repository.rehydrateEditedAssistantBinding(ctx, assistantBindingTx, ownerScope, projectRef,
		assistantBinding, forgedEdit); err == nil {
		_ = assistantBindingTx.Rollback(ctx)
		t.Fatal("binding edit accepted a non-project environment")
	}
	if _, err := repository.hydrateAssistantAgentEnvironmentBinding(ctx, assistantBindingTx, ownerScope, "prj_other",
		entity.AssistantPlanOperation{Type: "BIND_AGENT_RUNTIME_ENVIRONMENT", Parameters: map[string]any{
			"agentRef": thirdAgent.Ref, "environmentRef": target.Ref,
		}}); err == nil {
		_ = assistantBindingTx.Rollback(ctx)
		t.Fatal("cross-project assistant binding accepted")
	}
	if err := assistantBindingTx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	assistantBinding, err = normalizeAssistantOperation(assistantBinding)
	if err != nil {
		t.Fatal(err)
	}
	plannedBinding, err := assistantOperationCommand(assistantBinding)
	if err != nil {
		t.Fatal(err)
	}
	plannedBinding.Principal = owner
	plannedBinding.Mutation.IdempotencyKey = "draft-assistant-bind-apply"
	boundByAssistant, err := service.Execute(ctx, plannedBinding)
	if err != nil || boundByAssistant.RuntimeConfiguration == nil ||
		boundByAssistant.RuntimeConfiguration.EnvironmentBinding.VersionRef != freshTarget.CurrentVersion.Ref {
		t.Fatalf("assistant binding did not pin ready revision: %v", err)
	}
	staleTx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	matching, err = repository.assistantAgentBindingSnapshotMatches(ctx, staleTx, ownerScope, projectRef, assistantBinding)
	_ = staleTx.Rollback(ctx)
	if err != nil || matching {
		t.Fatalf("stale assistant binding remained valid: %v %v", matching, err)
	}
}
