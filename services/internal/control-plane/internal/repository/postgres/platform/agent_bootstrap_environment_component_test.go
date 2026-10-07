package platform

import (
	"context"
	_ "embed"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/agent_bootstrap_environment_readback.sql
var queryAgentBootstrapEnvironmentReadback string

func TestAgentBootstrapPreservesPublishedProjectEnvironmentComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	r, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConfigureProviderCredential(ProviderCredentialConfig{SecretName: "runtime-provider-openai-default-r1", SecretUID: "10000000-0000-4000-8000-000000000001", SecretResourceVersion: "1", ContentSHA256: strings.Repeat("e", 64)}); err != nil {
		t.Fatal(err)
	}
	images := RoleImageConfig{PolicyRevision: 1, RoleRuntimeContractRevision: 1, PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64), BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3, StagingRepository: "registry.invalid/staging", PromotedRepository: "registry.invalid/roles", DefaultImageReference: "registry.invalid/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32))}
	if err := r.ConfigureRoleImages(images); err != nil {
		t.Fatal(err)
	}
	if err := r.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, r)
	owner := resolvedTestPrincipal(t, ctx, r, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.agents.create"}, "control-api-gateway")
	s, err := platformservice.New(r)
	if err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := s.Execute(ctx, command.Command{Kind: kind, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "bootstrap-isolation-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("native %s: %v", kind, err)
		}
		return result
	}
	project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Bootstrap isolation", Language: "en"}).Project
	helper := execute(command.CreateProjectAssistant, "helper", nil, command.ProjectAssistantInput{ProjectRef: project.Ref, Name: "Helper", Purpose: "Own published environment", Instructions: "Use only explicitly configured project resources."}).ProjectAssistant
	view, err := s.GetAgentRuntimeConfiguration(ctx, owner, helper.AgentRef)
	if err != nil {
		t.Fatal(err)
	}
	environment := view.Environment
	spec := entity.RuntimeEnvironmentDraftSpecification{Name: environment.Name, Description: "Owner custom environment", ImageArtifactRef: environment.CurrentVersion.Image.ArtifactRef, Values: []entity.RuntimeEnvironmentValue{{Name: "OWNER_CUSTOM_MODE", Value: "preserve-exact"}}, Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy()}
	draft := execute(command.CreateRuntimeEnvironmentDraft, "draft", &environment.Version, command.RuntimeEnvironmentDraftInput{ProjectRef: project.Ref, EnvironmentRef: environment.Ref, ExpectedEnvironmentVersion: environment.Version, Specification: spec}).RuntimeEnvironmentDraft
	draft = execute(command.ValidateRuntimeEnvironmentDraft, "validate", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref}).RuntimeEnvironmentDraft
	if draft.State != "VALID" {
		t.Fatal("custom helper environment did not validate")
	}
	plan := execute(command.PrepareEnvironmentDraftImpact, "impact", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref}).RevisionImpactPlan
	page, err := s.GetRevisionImpactPlan(ctx, owner, plan.Ref, "", query.Page{Size: 100})
	if err != nil || len(page.Items) != 1 || page.Items[0].ConsumerRef != helper.AgentRef {
		t.Fatal("helper publication did not resolve exact consumer")
	}
	published := execute(command.PublishRuntimeEnvironmentDraft, "publish", &draft.Version, command.RuntimeEnvironmentDraftInput{DraftRef: draft.Ref, PlanRef: plan.Ref, SelectedItemRefs: []string{page.Items[0].Ref}}).RuntimeEnvironment
	if published.CurrentVersion.Ref == environment.CurrentVersion.Ref || published.CurrentVersion.Digest != draft.ValidationDigest {
		t.Fatal("owner custom publication did not become exact current")
	}
	snapshot := func() [4]string {
		t.Helper()
		var result [4]string
		if err := pool.QueryRow(ctx, queryAgentBootstrapEnvironmentReadback, helper.AgentRef).Scan(&result[0], &result[1], &result[2], &result[3]); err != nil {
			t.Fatal(err)
		}
		return result
	}
	before := snapshot()
	// Даже совпадающий default image не разрешает служебный UPDATE updated_at.
	execute(command.CreateAgent, "same-image-role", nil, command.AgentInput{ProjectRef: project.Ref, Name: "Same image", Purpose: "Current preservation", RoleDescription: "Synthetic role", Instructions: "Use only explicitly configured project resources."})
	if snapshot() != before {
		t.Fatal("same-image CREATE_AGENT rewrote the published environment")
	}
	resolved, err := r.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	ownerScope, err := r.resolveScope(ctx, resolved)
	if err != nil {
		t.Fatal(err)
	}
	effects := func() [5]int {
		t.Helper()
		var result [5]int
		if err := pool.QueryRow(ctx, queryAssistantRunScopeEffects, ownerScope.organizationID).Scan(&result[0], &result[1], &result[2], &result[3], &result[4]); err != nil {
			t.Fatal(err)
		}
		return result
	}
	// Новый default image у producer отличается от уже опубликованного owner image.
	// Это не разрешает CREATE_AGENT менять существующий environment current.
	images.DefaultImageReference = "registry.invalid/roles/system@sha256:" + strings.Repeat("f", 64)
	if err := r.ConfigureRoleImages(images); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Manager", "Architect", "Developer", "Documentation", "Security", "Lexical"} {
		key := fmt.Sprintf("role-%d", i)
		input := command.AgentInput{ProjectRef: project.Ref, Name: name, Purpose: "Isolated role", RoleDescription: "Synthetic role", RuntimeRef: "builtin-safe-runtime", Instructions: "Use only explicitly configured project resources."}
		agent := execute(command.CreateAgent, key, nil, input).Agent
		if snapshot() != before {
			t.Fatal("CREATE_AGENT rewrote helper published environment, current, binding or version history")
		}
		fresh, err := s.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
		if err != nil || fresh.Environment.Ref != published.Ref || fresh.EnvironmentBinding.VersionRef != published.CurrentVersion.Ref || fresh.Environment.CurrentVersion.Digest != published.CurrentVersion.Digest || len(fresh.Environment.CurrentVersion.Values) != 1 || fresh.Environment.CurrentVersion.Values[0].Value != "preserve-exact" {
			t.Fatal("new role did not bind exact existing published owner environment")
		}
		createdEffects := effects()
		replay := execute(command.CreateAgent, key, nil, input).Agent
		if replay.Ref != agent.Ref || replay.RoleDefinitionRef != agent.RoleDefinitionRef || snapshot() != before || effects() != createdEffects {
			t.Fatal("creation replay changed refs or owner environment")
		}
	}
	// Пустой новый project по-прежнему получает новую initial revision и exact pin.
	second := execute(command.CreateProject, "empty-project", nil, command.ProjectInput{Name: "Fresh bootstrap environment", Language: "en"}).Project
	agent := execute(command.CreateAgent, "fresh-role", nil, command.AgentInput{ProjectRef: second.Ref, Name: "Fresh", Purpose: "Initial environment", RoleDescription: "Synthetic role", Instructions: "Use only explicitly configured project resources."}).Agent
	fresh, err := s.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil || fresh.Environment.Ref == published.Ref || fresh.Environment.CurrentVersion.Ref == "" || fresh.EnvironmentBinding.VersionRef != fresh.Environment.CurrentVersion.Ref || fresh.Environment.CurrentVersion.Image.Digest != "sha256:"+strings.Repeat("f", 64) {
		t.Fatal("new project lost bootstrap initialization or exact pin")
	}
}
