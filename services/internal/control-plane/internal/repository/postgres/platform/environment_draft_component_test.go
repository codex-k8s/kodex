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
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
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
	invalid, err := invoke(command.ValidateRuntimeEnvironmentDraft, "draft-validate-incomplete", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil || invalid.RuntimeEnvironmentDraft.State != "INVALID" {
		t.Fatalf("invalid draft validation: %v", err)
	}
	draft = invalid.RuntimeEnvironmentDraft
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
	valid, err := invoke(command.ValidateRuntimeEnvironmentDraft, "draft-validate-complete", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil || valid.RuntimeEnvironmentDraft.State != "VALID" || valid.RuntimeEnvironmentDraft.ValidationDigest == "" {
		t.Fatalf("validate complete: %v", err)
	}
	draft = valid.RuntimeEnvironmentDraft
	publicationVersion := draft.Version
	published, err := invoke(command.PublishRuntimeEnvironmentDraft, "draft-publish-complete", &publicationVersion, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil || published.RuntimeEnvironmentDraft.State != "PUBLISHED" || published.RuntimeEnvironment == nil || published.RuntimeEnvironment.CurrentVersion.Digest != draft.ValidationDigest {
		t.Fatalf("publish draft: %v", err)
	}
	replay, err := invoke(command.PublishRuntimeEnvironmentDraft, "draft-publish-complete", &publicationVersion, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil || replay.RuntimeEnvironment.Ref != published.RuntimeEnvironment.Ref {
		t.Fatalf("publication replay: %v", err)
	}
	if _, err := invoke(command.SaveRuntimeEnvironmentDraft, "draft-save-published", &published.RuntimeEnvironmentDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, Specification: spec}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("published draft was edited: %v", err)
	}
	readback, err := service.GetRuntimeEnvironmentDraft(ctx, owner, draft.Ref)
	if err != nil || readback.PublishedEnvironmentRef != published.RuntimeEnvironment.Ref {
		t.Fatalf("draft readback: %v", err)
	}
	target := published.RuntimeEnvironment
	change, err := invoke(command.CreateRuntimeEnvironmentDraft, "draft-target-create", nil, command.RuntimeEnvironmentDraftInput{
		ProjectRef: projectRef, EnvironmentRef: target.Ref, ExpectedEnvironmentVersion: target.Version, Specification: spec})
	if err != nil { t.Fatal(err) }
	changeDraft := change.RuntimeEnvironmentDraft
	checked, err := invoke(command.ValidateRuntimeEnvironmentDraft, "draft-target-validate", &changeDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: changeDraft.Ref})
	if err != nil || checked.RuntimeEnvironmentDraft.State != "VALID" { t.Fatalf("target draft validation: %v", err) }
	if _, err := service.Execute(ctx, command.Command{Kind: command.PublishRuntimeEnvironment, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "draft-target-concurrent-publish", ExpectedVersion: &target.Version},
		Payload: environmentDraftPayload(*checked.RuntimeEnvironmentDraft)}); err != nil { t.Fatal(err) }
	if _, err := invoke(command.PublishRuntimeEnvironmentDraft, "draft-target-stale-publish", &checked.RuntimeEnvironmentDraft.Version,
		command.RuntimeEnvironmentDraftInput{DraftRef: changeDraft.Ref}); !errors.Is(err, errs.ErrVersionMismatch) { t.Fatalf("stale target publication: %v", err) }
	discarded, err := invoke(command.DiscardRuntimeEnvironmentDraft, "draft-target-discard", &checked.RuntimeEnvironmentDraft.Version,
		command.RuntimeEnvironmentDraftInput{DraftRef: changeDraft.Ref})
	if err != nil || discarded.RuntimeEnvironmentDraft.State != "DISCARDED" { t.Fatalf("discard draft: %v", err) }
}
