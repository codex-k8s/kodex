package platform

import (
	"context"
	_ "embed"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
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

//go:embed testdata/sql/organization_environment_reparent.sql
var queryOrganizationEnvironmentReparent string

//go:embed testdata/sql/organization_environment_binding_reparent.sql
var queryOrganizationEnvironmentBindingReparent string

func TestOrganizationRuntimeEnvironmentComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	config, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse isolated fixture configuration")
	}
	config.ConnConfig.Tracer = organizationImageQueryTracer{t: t}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-6.1-sol", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1", ContentSHA256: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles",
		DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.organization.runtime-environment-drafts.create",
	}, "control-api-gateway")
	owner.CredentialAuthenticatedAt = time.Now().UTC()
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	s, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := repository.getAssistantTx(ctx, tx, s)
	if err != nil {
		t.Fatal(err)
	}
	view, err := repository.getRuntimeConfigurationViewTx(ctx, tx, s, assistant.Ref)
	_ = tx.Rollback(ctx)
	if err != nil || view.Environment.ScopeKind != "ORGANIZATION" || view.Environment.OrganizationRef == "" || view.Environment.CurrentVersion.Image.ArtifactRef == "" {
		t.Fatalf("exact organization bootstrap environment: %v", err)
	}
	spec := entity.RuntimeEnvironmentDraftSpecification{Name: "Organization environment", ImageArtifactRef: view.Environment.CurrentVersion.Image.ArtifactRef,
		Values: []entity.RuntimeEnvironmentValue{{Name: "MODE", Value: "safe"}}, Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy()}
	invoke := func(p value.Principal, kind command.Kind, key string, version *int64, payload command.RuntimeEnvironmentDraftInput) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: kind, Principal: p, Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: version}, Payload: payload})
	}
	created, err := invoke(owner, command.CreateOrganizationRuntimeEnvironmentDraft, "org-env-create", nil, command.RuntimeEnvironmentDraftInput{Specification: spec})
	if err != nil || created.RuntimeEnvironmentDraft == nil || created.RuntimeEnvironmentDraft.ScopeKind != "ORGANIZATION" || created.RuntimeEnvironmentDraft.ProjectRef != "" {
		t.Fatalf("organization draft create: %v", err)
	}
	draft := created.RuntimeEnvironmentDraft
	if _, err := invoke(owner, command.CreateRuntimeEnvironmentDraft, "org-env-generic-create", nil, command.RuntimeEnvironmentDraftInput{ScopeKind: "ORGANIZATION", Specification: spec}); !errors.Is(err, errs.ErrInvalid) && !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("generic project create accepted organization owner: %v", err)
	}
	stale := draft.Version + 1
	if _, err := invoke(owner, command.SaveRuntimeEnvironmentDraft, "org-env-stale", &stale, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, Specification: spec}); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("draft OCC: %v", err)
	}
	valid, err := invoke(owner, command.ValidateRuntimeEnvironmentDraft, "org-env-validate", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil || valid.RuntimeEnvironmentDraft.State != "VALID" {
		t.Fatalf("organization draft validate: %v", err)
	}
	draft = valid.RuntimeEnvironmentDraft
	prepared, err := invoke(owner, command.PrepareEnvironmentDraftImpact, "org-env-impact", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref})
	if err != nil || prepared.RevisionImpactPlan == nil || prepared.RevisionImpactPlan.Total != 0 {
		t.Fatalf("organization draft impact: %v", err)
	}
	publish := command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: prepared.RevisionImpactPlan.Ref}
	published, err := invoke(owner, command.PublishRuntimeEnvironmentDraft, "org-env-publish", &draft.Version, publish)
	if err != nil || published.RuntimeEnvironment == nil || published.RuntimeEnvironment.ScopeKind != "ORGANIZATION" || published.RuntimeEnvironment.ProjectRef != "" || published.RuntimeEnvironment.CurrentVersion.Digest != draft.ValidationDigest {
		t.Fatalf("organization draft publish: %v", err)
	}
	replay, err := invoke(owner, command.PublishRuntimeEnvironmentDraft, "org-env-publish", &draft.Version, publish)
	if err != nil || replay.RuntimeEnvironment.Ref != published.RuntimeEnvironment.Ref {
		t.Fatalf("organization publication replay: %v", err)
	}
	if _, err := service.GetRuntimeEnvironment(ctx, owner, published.RuntimeEnvironment.Ref); err != nil {
		t.Fatalf("published organization read: %v", err)
	}
	if _, err := service.GetRuntimeEnvironmentDraft(ctx, owner, draft.Ref); err != nil {
		t.Fatalf("published draft read: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "org-env-project"}, Payload: command.ProjectInput{Name: "Environment boundary", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("project fixture: %v", err)
	}
	profile, err := service.Execute(ctx, command.Command{Kind: command.CreateProjectAssistant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "org-env-project-assistant"}, Payload: command.ProjectAssistantInput{ProjectRef: project.Project.Ref,
			Name: "Project assistant", Purpose: "Synthetic environment boundary", Instructions: "Use only project resources."}})
	if err != nil || profile.ProjectAssistant == nil {
		t.Fatalf("project assistant fixture: %v", err)
	}
	projectView, err := service.GetAgentRuntimeConfiguration(ctx, owner, profile.ProjectAssistant.AgentRef)
	if err != nil || projectView.Environment.ScopeKind != "PROJECT" || projectView.Environment.CurrentVersion.Image.ArtifactRef == "" {
		t.Fatalf("project environment fixture: %v", err)
	}
	var projectID string
	if err := pool.QueryRow(ctx, queryOrganizationImageComponentProject, s.organizationID, project.Project.Ref).Scan(&projectID); err != nil {
		t.Fatal(err)
	}
	signed := owner
	signed.ProjectRef = projectID
	if _, err := service.GetRuntimeEnvironmentDraft(ctx, signed, draft.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("signed project read organization draft: %v", err)
	}
	if _, _, err := service.ListRuntimeEnvironmentVersions(ctx, signed, query.Filter{ResourceRef: published.RuntimeEnvironment.Ref}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("signed project listed organization revisions: %v", err)
	}
	if _, err := invoke(signed, command.PublishRuntimeEnvironmentDraft, "org-env-publish", &draft.Version, publish); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("signed project replayed organization receipt: %v", err)
	}
	if _, err := invoke(owner, command.CreateOrganizationRuntimeEnvironmentDraft, "org-env-project-base", nil,
		command.RuntimeEnvironmentDraftInput{EnvironmentRef: projectView.Environment.Ref, ExpectedEnvironmentVersion: projectView.Environment.Version, Specification: spec}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("organization draft used project environment: %v", err)
	}
	foreignImage := spec
	foreignImage.ImageArtifactRef = projectView.Environment.CurrentVersion.Image.ArtifactRef
	foreignCreated, err := invoke(owner, command.CreateOrganizationRuntimeEnvironmentDraft, "org-env-project-image", nil, command.RuntimeEnvironmentDraftInput{Specification: foreignImage})
	if err != nil {
		t.Fatal(err)
	}
	foreignChecked, err := invoke(owner, command.ValidateRuntimeEnvironmentDraft, "org-env-project-image-check", &foreignCreated.RuntimeEnvironmentDraft.Version,
		command.RuntimeEnvironmentDraftInput{DraftRef: foreignCreated.RuntimeEnvironmentDraft.Ref})
	if err != nil || foreignChecked.RuntimeEnvironmentDraft.State != "INVALID" {
		t.Fatalf("organization draft admitted project image: %v", err)
	}
	if _, err := pool.Exec(ctx, queryOrganizationEnvironmentReparent, published.RuntimeEnvironment.Ref, projectID); err == nil {
		t.Fatal("organization environment owner was mutable")
	}
	if _, err := pool.Exec(ctx, queryOrganizationEnvironmentBindingReparent, profile.ProjectAssistant.AgentRef, published.RuntimeEnvironment.Ref); err == nil {
		t.Fatal("project assistant bound organization environment")
	}
	for _, scopeKind := range []string{"ORGANIZATION", "PROJECT"} {
		projectOwner := projectID
		if scopeKind == "ORGANIZATION" {
			projectOwner = ""
		}
		prefix := strings.ToLower(scopeKind)
		var descriptor entity.RuntimeSecretRevisionDescriptor
		var secretRef string
		if err := pool.QueryRow(ctx, queryAssistantCredentialSecretSeed, pgx.StrictNamedArgs{
			"secret_ref": "sec_env_scope_" + prefix, "revision_ref": "secr_env_scope_" + prefix,
			"organization_id": s.organizationID, "actor_id": s.actorID, "project_id": projectOwner, "scope_kind": scopeKind,
			"name": "env-scope-" + prefix, "secret_name": "runtime-env-scope-" + prefix + "-r1",
			"secret_uid": "10000000-0000-4000-8000-000000000111", "content_sha256": strings.Repeat("a", 64),
		}).Scan(&secretRef, &descriptor.Revision, &descriptor.Namespace, &descriptor.SecretName, &descriptor.SecretKey,
			&descriptor.SecretUID, &descriptor.SecretResourceVersion, &descriptor.ContentSHA256); err != nil {
			t.Fatal(err)
		}
		secretSpec := spec
		secretSpec.Name = "Scoped secret " + prefix
		secretSpec.SecretBindings = []entity.RuntimeSecretBinding{{Name: "TOKEN", SecretRef: secretRef, Revision: descriptor.Revision}}
		secretDraft, err := invoke(owner, command.CreateOrganizationRuntimeEnvironmentDraft, "org-env-secret-"+prefix, nil,
			command.RuntimeEnvironmentDraftInput{Specification: secretSpec})
		if err != nil {
			t.Fatal(err)
		}
		secretChecked, err := invoke(owner, command.ValidateRuntimeEnvironmentDraft, "org-env-secret-check-"+prefix, &secretDraft.RuntimeEnvironmentDraft.Version,
			command.RuntimeEnvironmentDraftInput{DraftRef: secretDraft.RuntimeEnvironmentDraft.Ref})
		expected := "INVALID"
		if scopeKind == "ORGANIZATION" {
			expected = "VALID"
		}
		if err != nil || secretChecked.RuntimeEnvironmentDraft.State != expected {
			t.Fatalf("organization secret owner scope %s: %v", scopeKind, err)
		}
	}
	selfTx, err := pool.BeginTx(ctx, pgx.TxOptions{AccessMode: pgx.ReadOnly})
	if err != nil {
		t.Fatal(err)
	}
	selfOperation, err := repository.hydrateAssistantEnvironmentOperation(ctx, selfTx, s, "", entity.AssistantPlanOperation{
		Type: "PREPARE_RUNTIME_ENVIRONMENT_REVISION", Key: "org-env-self", Title: "Настроить окружение Kodex", Summary: "Подготовить полный черновик окружения", Parameters: map[string]any{
			"environmentRef": view.Environment.Ref, "systemAssistantRef": assistant.Ref, "description": "Synthetic self configuration",
			"imageArtifactRef": view.Environment.CurrentVersion.Image.ArtifactRef,
			"secretBindings":   []any{map[string]any{"name": "TOKEN", "secretRef": "sec_env_scope_organization", "revision": float64(1)}},
		}})
	_ = selfTx.Rollback(ctx)
	if err != nil {
		t.Fatalf("hydrate organization self configuration: %v", err)
	}
	selfOperation, err = normalizeAssistantOperation(selfOperation)
	if err != nil {
		t.Fatal(err)
	}
	selfCommand, err := assistantOperationCommand(selfOperation)
	if err != nil || selfCommand.Kind != command.CreateOrganizationRuntimeEnvironmentDraft {
		t.Fatalf("self configuration used direct publication: %v", err)
	}
	selfCommand.Principal = owner
	selfCommand.Mutation.IdempotencyKey = "org-env-self-apply"
	selfResult, err := service.Execute(ctx, selfCommand)
	if err != nil || selfResult.RuntimeEnvironmentDraft == nil || selfResult.RuntimeEnvironmentDraft.EnvironmentRef != view.Environment.Ref || selfResult.RuntimeEnvironmentDraft.ScopeKind != "ORGANIZATION" ||
		len(selfResult.RuntimeEnvironmentDraft.Specification.SecretBindings) != 1 || selfResult.RuntimeEnvironmentDraft.Specification.ImageArtifactRef != view.Environment.CurrentVersion.Image.ArtifactRef {
		t.Fatalf("organization self configuration lost complete draft inputs: %v", err)
	}
	if _, err := invoke(owner, command.SaveRuntimeEnvironmentDraft, "org-env-terminal-save", &published.RuntimeEnvironmentDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, Specification: spec}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("published draft edited: %v", err)
	}
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, s.organizationID, s.actorID, "MEMBER"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetRuntimeEnvironmentDraft(ctx, owner, draft.Ref); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked owner read organization draft: %v", err)
	}
	if _, err := invoke(owner, command.PublishRuntimeEnvironmentDraft, "org-env-publish", &draft.Version, publish); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("revoked owner receipt replay: %v", err)
	}
	if _, err := pool.Exec(ctx, queryOrganizationImageComponentOwner, s.organizationID, s.actorID, "OWNER"); err != nil {
		t.Fatal(err)
	}
	web := spec
	web.Name = "Privileged organization environment"
	web.Policy.Network.WebAccess = runtimecontract.RuntimeWebAccess{Mode: runtimecontract.RuntimeWebAccessFullPublic}
	webCreated, err := invoke(owner, command.CreateOrganizationRuntimeEnvironmentDraft, "org-env-web-create", nil, command.RuntimeEnvironmentDraftInput{Specification: web})
	if err != nil {
		t.Fatal(err)
	}
	webDraft := webCreated.RuntimeEnvironmentDraft
	oldOwner := owner
	oldOwner.CredentialAuthenticatedAt = time.Now().UTC().Add(-10 * time.Minute)
	if _, err := invoke(oldOwner, command.ValidateRuntimeEnvironmentDraft, "org-env-web-stale", &webDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: webDraft.Ref}); !errors.Is(err, errs.ErrFreshAuthenticationRequired) {
		t.Fatalf("stale authentication admitted organization web access: %v", err)
	}
	checked, err := invoke(owner, command.ValidateRuntimeEnvironmentDraft, "org-env-web-valid", &webDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: webDraft.Ref})
	if err != nil || checked.RuntimeEnvironmentDraft.State != "VALID" {
		t.Fatalf("fresh organization web access validation: %v", err)
	}
	if _, err := invoke(oldOwner, command.ValidateRuntimeEnvironmentDraft, "org-env-web-valid", &webDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: webDraft.Ref}); !errors.Is(err, errs.ErrFreshAuthenticationRequired) {
		t.Fatalf("stale authentication replayed privileged validation: %v", err)
	}
	discarded, err := invoke(owner, command.DiscardRuntimeEnvironmentDraft, "org-env-web-discard", &checked.RuntimeEnvironmentDraft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: webDraft.Ref})
	if err != nil || discarded.RuntimeEnvironmentDraft.State != "DISCARDED" {
		t.Fatalf("organization draft discard: %v", err)
	}
	t.Run("organization secret rotation preserves explicit impact owner snapshots", func(t *testing.T) {
		testOrganizationSecretImpact(t, ctx, repository, service, pool, owner, s, assistant.Ref, spec, projectID)
	})
}
