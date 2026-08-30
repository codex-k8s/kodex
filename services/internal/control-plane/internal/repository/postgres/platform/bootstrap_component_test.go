package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	domainerrs "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/systemassistant"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	//go:embed testdata/sql/bootstrap_component_readback.sql
	bootstrapComponentReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_disable_system_assistant.sql
	bootstrapComponentDisableSystemAssistantQuery string
	//go:embed testdata/sql/bootstrap_component_delete_system_assistant.sql
	bootstrapComponentDeleteSystemAssistantQuery string
	//go:embed testdata/sql/bootstrap_component_replace_core_prompt.sql
	bootstrapComponentReplaceCorePromptQuery string
	//go:embed testdata/sql/bootstrap_component_replace_session_provider_account.sql
	bootstrapComponentReplaceSessionProviderAccountQuery string
	//go:embed testdata/sql/bootstrap_component_connect_integration.sql
	bootstrapComponentConnectIntegrationQuery string
	//go:embed testdata/sql/bootstrap_component_make_interaction_delivery_due.sql
	bootstrapComponentMakeInteractionDeliveryDueQuery string
	//go:embed testdata/sql/bootstrap_component_make_schedule_due.sql
	bootstrapComponentMakeScheduleDueQuery string
	//go:embed testdata/sql/bootstrap_component_schedule_occurrence_readback.sql
	bootstrapComponentScheduleOccurrenceReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_change_schedule_after_claim.sql
	bootstrapComponentChangeScheduleAfterClaimQuery string
	//go:embed testdata/sql/bootstrap_component_expire_schedule_claim.sql
	bootstrapComponentExpireScheduleClaimQuery string
	//go:embed testdata/sql/bootstrap_component_schedule_target_state_readback.sql
	bootstrapComponentScheduleTargetStateReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_schedule_archive_readback.sql
	bootstrapComponentScheduleArchiveReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_core_prompt_upgrade_readback.sql
	bootstrapComponentCorePromptUpgradeReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_warm_heartbeat_counts.sql
	bootstrapComponentWarmHeartbeatCountsQuery string
	//go:embed testdata/sql/bootstrap_component_provider_credential_readback.sql
	bootstrapComponentProviderCredentialReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_instruction_draft_readback.sql
	bootstrapComponentInstructionDraftReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_effect_receipt_count.sql
	bootstrapComponentEffectReceiptCountQuery string
	//go:embed testdata/sql/bootstrap_component_sequence_readback.sql
	bootstrapComponentSequenceReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_tool_call_outbox_readback.sql
	bootstrapComponentToolCallOutboxReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_insert_secondary_provider.sql
	bootstrapComponentInsertSecondaryProviderQuery string
	//go:embed testdata/sql/bootstrap_component_integration_invocation_effect_key.sql
	bootstrapComponentIntegrationInvocationEffectKeyQuery string
)

func finalizedAttachmentSetRef(t *testing.T, ctx context.Context, service *platformservice.Service,
	principal value.Principal, projectRef, purpose, key string, artifactRefs ...string,
) string {
	t.Helper()
	draft, err := service.Execute(ctx, command.Command{Kind: command.CreateAttachmentSetDraft, Principal: principal,
		Mutation: value.Mutation{IdempotencyKey: key + "-draft"}, Payload: command.AttachmentSetDraftInput{
			ProjectRef: projectRef, Purpose: purpose, ArtifactRefs: artifactRefs,
		}})
	if err != nil || draft.AttachmentSet == nil {
		t.Fatalf("create attachment set draft: set=%#v err=%v", draft.AttachmentSet, err)
	}
	version := draft.AttachmentSet.Version
	finalized, err := service.Execute(ctx, command.Command{Kind: command.FinalizeAttachmentSet, Principal: principal,
		Mutation: value.Mutation{IdempotencyKey: key + "-finalize", ExpectedVersion: &version},
		Payload:  command.AttachmentSetDraftInput{AttachmentSetRef: draft.AttachmentSet.Ref},
	})
	if err != nil || finalized.AttachmentSet == nil || finalized.AttachmentSet.State != "FINALIZED" {
		t.Fatalf("finalize attachment set: set=%#v err=%v", finalized.AttachmentSet, err)
	}
	return finalized.AttachmentSet.Ref
}

func TestBootstrapComponent(t *testing.T) {
	dsn := os.Getenv("KODEX_CONTROL_PLANE_TEST_DSN")
	if dsn == "" {
		t.Skip("KODEX_CONTROL_PLANE_TEST_DSN is not configured")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("open disposable PostgreSQL: %v", err)
	}
	defer pool.Close()
	repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
	if err != nil {
		t.Fatalf("construct repository: %v", err)
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName:            "runtime-provider-openai-default-r1",
		SecretUID:             "10000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "1",
		ContentSHA256:         "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
	}); err != nil {
		t.Fatalf("configure provider credential: %v", err)
	}
	if err := repository.ConfigureRoleImages(RoleImageConfig{
		PolicyRevision: 1, RoleRuntimeContractRevision: 1,
		PolicySHA256: strings.Repeat("a", 64), RoleRuntimeContractSHA256: strings.Repeat("b", 64),
		BuildLeaseDuration: time.Minute, AdmissionClaimTTL: time.Minute, PromotionClaimTTL: time.Minute, MaximumAttempts: 3,
		StagingRepository: "registry.invalid/kodex/staging", PromotedRepository: "registry.invalid/kodex/roles",
		DefaultImageReference: "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("c", 64), LeaseSigningKey: []byte(strings.Repeat("d", 32)),
	}); err != nil {
		t.Fatalf("configure role images: %v", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatalf("bootstrap attempt %d: %v", attempt+1, err)
		}
	}
	assertBootstrapReadback(t, ctx, pool)

	for name, query := range map[string]string{
		"disable system assistant":         bootstrapComponentDisableSystemAssistantQuery,
		"delete system assistant":          bootstrapComponentDeleteSystemAssistantQuery,
		"replace core prompt":              bootstrapComponentReplaceCorePromptQuery,
		"replace session provider account": bootstrapComponentReplaceSessionProviderAccountQuery,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := pool.Exec(ctx, query); err == nil {
				t.Fatal("protected system state was changed")
			}
		})
	}
	assertBootstrapReadback(t, ctx, pool)
	t.Run("authority proof revision keeps platform cursor stable", func(t *testing.T) {
		var platformBefore, proofBefore int64
		if err := pool.QueryRow(ctx, bootstrapComponentSequenceReadbackQuery).Scan(&platformBefore, &proofBefore); err != nil {
			t.Fatalf("read sequences before authority proof: %v", err)
		}
		revision, err := repository.NextAuthorityProofRevision(ctx)
		if err != nil {
			t.Fatalf("issue authority proof revision: %v", err)
		}
		var platformAfter, proofAfter int64
		if err := pool.QueryRow(ctx, bootstrapComponentSequenceReadbackQuery).Scan(&platformAfter, &proofAfter); err != nil {
			t.Fatalf("read sequences after authority proof: %v", err)
		}
		if platformAfter != platformBefore || proofAfter != proofBefore+1 || revision != uint64(proofAfter) {
			t.Fatalf("authority proof changed platform cursor: platform=%d->%d proof=%d->%d revision=%d", platformBefore, platformAfter, proofBefore, proofAfter, revision)
		}
	})
	t.Run("provider credential legacy repair creates an immutable next revision", func(t *testing.T) {
		testProviderCredentialLegacyRepair(t, ctx, repository, pool)
	})
	t.Run("OIDC candidate receives project membership without internal identifiers", func(t *testing.T) {
		testProjectMembershipCandidate(t, ctx, repository)
	})
	t.Run("instruction draft save replaces the mutable draft", func(t *testing.T) {
		testInstructionDraftSave(t, ctx, repository)
	})
	t.Run("system assistant proposes and applies typed plan", func(t *testing.T) {
		testSystemAssistantTypedPlan(t, ctx, repository)
	})
	t.Run("direct run continuation cancel and retry", func(t *testing.T) {
		testDirectRunLifecycle(t, ctx, repository)
	})
	t.Run("session archive snapshot restore and GC", func(t *testing.T) {
		testSessionArchiveLifecycle(t, ctx, repository, pool)
	})
	t.Run("provider neutral nested delegation", func(t *testing.T) {
		testNestedDelegation(t, ctx, repository)
	})
	t.Run("human gate resolves once and completes root", func(t *testing.T) {
		testHumanGateLifecycle(t, ctx, repository)
	})
	t.Run("idempotency occ and concurrent run creation", func(t *testing.T) {
		testIdempotencyOCCAndConcurrentRuns(t, ctx, repository)
	})
	t.Run("durable schedule materializes immutable occurrence", func(t *testing.T) {
		testScheduleLifecycle(t, ctx, repository)
	})
	t.Run("integration configuration and grants", func(t *testing.T) {
		testIntegrationConfigurationAndGrants(t, ctx, repository, pool)
	})
	t.Run("integration read and Human Gate decisions preserve effect cardinality", func(t *testing.T) {
		testIntegrationEffectLifecycle(t, ctx, repository, pool)
	})
	t.Run("optional interaction failure is a separate live incident", func(t *testing.T) {
		testOptionalInteractionIncident(t, ctx, repository, pool)
	})
	t.Run("system assistant core prompt upgrades forward only", func(t *testing.T) {
		testSystemAssistantCorePromptUpgrade(t, ctx, repository, pool)
	})
	t.Run("enterprise access restricts exact agent and project", func(t *testing.T) {
		testEnterpriseAccessRestriction(t, ctx, repository)
	})
	t.Run("role image lifecycle uses canonical application access", func(t *testing.T) {
		testRoleImageApplicationAccess(t, ctx, repository)
	})
	t.Run("runtime environment create rejects a missing exact image", func(t *testing.T) {
		testRuntimeEnvironmentRejectsMissingImage(t, ctx, repository)
	})
	t.Run("runtime environment privileged admission requires fresh authentication and permission", func(t *testing.T) {
		testRuntimeEnvironmentPrivilegedAdmission(t, ctx, repository)
	})
	t.Run("runtime configuration publish validates canonical provider accounts", func(t *testing.T) {
		testRuntimeConfigurationPublish(t, ctx, repository)
	})
	t.Run("runtime secret lifecycle is crash consistent", func(t *testing.T) {
		testRuntimeSecretCrashConsistency(t, ctx, repository)
	})
}

func testRuntimeConfigurationPublish(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	if _, err := repository.pool.Exec(ctx, bootstrapComponentInsertSecondaryProviderQuery); err != nil {
		t.Fatalf("insert secondary provider account: %v", err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Runtime configuration owner", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct runtime configuration service: %v", err)
	}
	createdProject, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-configuration-project-create"},
		Payload:  command.ProjectInput{Name: "Runtime configuration project", Language: "en"}})
	if err != nil || createdProject.Project == nil {
		t.Fatalf("create runtime configuration project: project=%#v err=%v", createdProject.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, createdProject.Project.Ref,
		"runtime-configuration-agent-create", "Runtime configuration specialist")
	current, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil {
		t.Fatalf("read initial runtime configuration: %v", err)
	}
	if current.Configuration.ProviderPolicy.Mode != "LEAST_USED" ||
		len(current.Configuration.ProviderPolicy.AccountCandidates) != 2 {
		t.Fatalf("bootstrap runtime policy does not contain the authorized provider pool: %#v",
			current.Configuration.ProviderPolicy)
	}
	expectedVersion := current.AgentVersion
	result, err := service.Execute(ctx, command.Command{Kind: command.PublishAgentRuntimeConfig, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-configuration-publish", ExpectedVersion: &expectedVersion},
		Payload: command.AgentRuntimeConfigurationInput{
			AgentRef: agent.Ref, RuntimeProfileRef: current.Configuration.RuntimeProfileRef,
			Model: current.Configuration.Model, ProviderPolicyMode: current.Configuration.ProviderPolicy.Mode,
			ProviderAccounts: current.Configuration.ProviderPolicy.AccountCandidates,
		}})
	if err != nil || result.RuntimeConfiguration == nil {
		t.Fatalf("publish runtime configuration: configuration=%#v err=%v", result.RuntimeConfiguration, err)
	}
	if result.RuntimeConfiguration.Configuration.Version != current.Configuration.Version+1 ||
		result.RuntimeConfiguration.AgentVersion != current.AgentVersion+1 ||
		result.RuntimeConfiguration.Configuration.Provider != current.Configuration.Provider {
		t.Fatalf("published runtime configuration readback mismatch: before=%#v after=%#v",
			current, *result.RuntimeConfiguration)
	}
}

func testRuntimeEnvironmentRejectsMissingImage(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Runtime environment owner", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct runtime environment service: %v", err)
	}
	createdProject, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-project-create"},
		Payload:  command.ProjectInput{Name: "Runtime environment project", Language: "en"}})
	if err != nil || createdProject.Project == nil {
		t.Fatalf("create runtime environment project: project=%#v err=%v", createdProject.Project, err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateRuntimeEnvironment, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-create"}, Payload: command.RuntimeEnvironmentInput{
			ProjectRef: createdProject.Project.Ref, Name: "Component environment", Description: "Runtime environment component readback",
			Values: []entity.RuntimeEnvironmentValue{{Name: "E2E_MODE", Value: "component"}},
		}})
	if !errors.Is(err, domainerrs.ErrInvalid) || created.RuntimeEnvironment != nil {
		t.Fatalf("environment without exact promoted image was accepted: environment=%#v err=%v", created.RuntimeEnvironment, err)
	}
}

func testRuntimeEnvironmentPrivilegedAdmission(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	now := time.Now().UTC()
	ownerInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Runtime environment reauthentication owner", CallerWorkload: "control-api-gateway",
		Operation: "platform.command.runtime-environments.create",
	}
	owner := resolvedTestPrincipal(t, ctx, repository, ownerInput, "control-api-gateway")
	owner.CredentialAuthenticatedAt = now
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct runtime environment reauthentication service: %v", err)
	}
	createdProject, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-reauth-project-create"},
		Payload:  command.ProjectInput{Name: "Runtime environment reauthentication", Language: "en"}})
	if err != nil || createdProject.Project == nil {
		t.Fatalf("create runtime environment reauthentication project: project=%#v err=%v", createdProject.Project, err)
	}
	project := *createdProject.Project
	agent := createLifecycleAgent(t, ctx, service, owner, project.Ref,
		"runtime-environment-reauth-agent-create", "Runtime environment reauthentication agent")
	configuration, err := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
	if err != nil || configuration.Environment.Ref == "" {
		t.Fatalf("read bootstrap runtime environment: configuration=%#v err=%v", configuration, err)
	}
	privilegedPolicy := privilegedRuntimeEnvironmentPolicy(t)

	create := func(key string, principal value.Principal, policy runtimecontract.RuntimeEnvironmentPolicy) error {
		_, executeErr := service.Execute(ctx, command.Command{Kind: command.CreateRuntimeEnvironment, Principal: principal,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.RuntimeEnvironmentInput{
				ProjectRef: project.Ref, Name: "Privileged component environment", Description: "Fresh authentication component scenario",
				Policy: policy,
			}})
		return executeErr
	}

	for _, test := range []struct {
		name            string
		authenticatedAt time.Time
	}{
		{name: "zero", authenticatedAt: time.Time{}},
		{name: "stale", authenticatedAt: now.Add(-5*time.Minute - time.Second)},
		{name: "future", authenticatedAt: now.Add(31 * time.Second)},
	} {
		principal := owner
		principal.CredentialAuthenticatedAt = test.authenticatedAt
		if executeErr := create("runtime-environment-reauth-create-"+test.name, principal, privilegedPolicy); !errors.Is(executeErr, domainerrs.ErrFreshAuthenticationRequired) {
			t.Fatalf("%s create authentication error = %v, want fresh authentication required", test.name, executeErr)
		}
	}
	if executeErr := create("runtime-environment-reauth-create-fresh", owner, privilegedPolicy); !errors.Is(executeErr, domainerrs.ErrInvalid) {
		t.Fatalf("fresh privileged create did not reach image validation: %v", executeErr)
	}

	staleOwner := owner
	staleOwner.CredentialAuthenticatedAt = now.Add(-6 * time.Minute)
	expectedVersion := configuration.Environment.Version
	publish := func(key string, principal value.Principal) error {
		_, executeErr := service.Execute(ctx, command.Command{Kind: command.PublishRuntimeEnvironment, Principal: principal,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &expectedVersion}, Payload: command.RuntimeEnvironmentInput{
				Ref: configuration.Environment.Ref, Name: configuration.Environment.Name, Description: configuration.Environment.Description,
				Policy: privilegedPolicy,
			}})
		return executeErr
	}
	if executeErr := publish("runtime-environment-reauth-publish-stale", staleOwner); !errors.Is(executeErr, domainerrs.ErrFreshAuthenticationRequired) {
		t.Fatalf("stale publish authentication error = %v, want fresh authentication required", executeErr)
	}
	if executeErr := publish("runtime-environment-reauth-publish-fresh", owner); !errors.Is(executeErr, domainerrs.ErrInvalid) {
		t.Fatalf("fresh privileged publish did not reach image validation: %v", executeErr)
	}

	candidateInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000009992", ExternalTenantID: ownerInput.ExternalTenantID,
		ExternalDisplayName: "Runtime environment project manager", CallerWorkload: "control-api-gateway",
		Operation: "platform.command.runtime-environments.create",
	}
	if _, resolveErr := repository.ResolveProofAuthority(ctx, candidateInput); !errors.Is(resolveErr, domainerrs.ErrForbidden) {
		t.Fatalf("unbound runtime environment candidate received authority: %v", resolveErr)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{
		Query: candidateInput.ExternalDisplayName, Page: query.Page{Size: 20},
	}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("list runtime environment candidate: subjects=%#v err=%v", subjects, err)
	}
	roleResult, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-project-manager-role"}, Payload: command.AccessRoleInput{
			Name: "Runtime environment project manager", PermissionKeys: []string{"project.manage"},
			AllowedScopes: []string{"PROJECT"}, ChangeComment: "component fresh authentication scenario",
		}})
	if err != nil || roleResult.AccessRole == nil {
		t.Fatalf("create runtime environment project manager role: role=%#v err=%v", roleResult.AccessRole, err)
	}
	bindingResult, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-project-manager-binding"}, Payload: command.AccessBindingInput{
			SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: roleResult.AccessRole.CurrentVersion.Ref,
			Scope: entity.AccessScope{Kind: "PROJECT", ProjectRef: project.Ref},
		}})
	if err != nil || bindingResult.AccessBinding == nil {
		t.Fatalf("bind runtime environment project manager: binding=%#v err=%v", bindingResult.AccessBinding, err)
	}
	authority, err := repository.ResolveProofAuthority(ctx, candidateInput)
	if err != nil {
		t.Fatalf("resolve runtime environment project manager: %v", err)
	}
	candidate := value.Principal{
		ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID, Permission: candidateInput.Operation,
		CorrelationRef: "runtime-environment-project-manager", CallerWorkload: "control-api-gateway",
		CredentialRevision: 1, CredentialAuthenticatedAt: now,
	}
	if executeErr := create("runtime-environment-project-manager-default", candidate, runtimecontract.DefaultRuntimeEnvironmentPolicy()); !errors.Is(executeErr, domainerrs.ErrInvalid) {
		t.Fatalf("project manager did not reach ordinary image validation: %v", executeErr)
	}
	if executeErr := create("runtime-environment-project-manager-privileged", candidate, privilegedPolicy); !errors.Is(executeErr, domainerrs.ErrNotFound) {
		t.Fatalf("project manager without privileged permission received unexpected result: %v", executeErr)
	}
	staleCandidate := candidate
	staleCandidate.CredentialAuthenticatedAt = now.Add(-6 * time.Minute)
	if executeErr := create("runtime-environment-project-manager-stale", staleCandidate, privilegedPolicy); !errors.Is(executeErr, domainerrs.ErrNotFound) {
		t.Fatalf("project manager without privileged permission received a reauthentication oracle: %v", executeErr)
	}
}

func testEnterpriseAccessRestriction(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	ownerInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Enterprise owner", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}
	owner := resolvedTestPrincipal(t, ctx, repository, ownerInput, "control-api-gateway")
	groupedOwner := ownerInput
	groupedOwner.ExternalIssuer = "https://identity.example.test/realms/kodex"
	groupedOwner.ExternalSessionRevision = 2
	groupedOwner.ExternalGroups = []string{"component-restricted-operators"}
	const concurrentResolutions = 8
	start := make(chan struct{})
	errorsByAttempt := make(chan error, concurrentResolutions)
	var resolutions sync.WaitGroup
	for range concurrentResolutions {
		resolutions.Add(1)
		go func() {
			defer resolutions.Done()
			<-start
			_, resolveErr := repository.ResolveProofAuthority(ctx, groupedOwner)
			errorsByAttempt <- resolveErr
		}()
	}
	close(start)
	resolutions.Wait()
	close(errorsByAttempt)
	for resolveErr := range errorsByAttempt {
		if resolveErr != nil {
			t.Fatalf("concurrent OIDC group synchronization failed: %v", resolveErr)
		}
	}
	var synchronizedMemberships int
	if err := repository.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM control_plane.oidc_group_memberships membership
		JOIN control_plane.oidc_groups oidc_group ON oidc_group.id = membership.group_id
		JOIN control_plane.subjects subject ON subject.id = membership.subject_id
		WHERE subject.id = $1::uuid AND oidc_group.display_name = $2
		  AND membership.subject_session_revision = $3
	`, owner.ActorID, groupedOwner.ExternalGroups[0], groupedOwner.ExternalSessionRevision).Scan(&synchronizedMemberships); err != nil || synchronizedMemberships != 1 {
		t.Fatalf("concurrent OIDC group synchronization readback: memberships=%d err=%v", synchronizedMemberships, err)
	}
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct enterprise access service: %v", err)
	}
	createProject := func(key, name string) entity.Project {
		result, createErr := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.ProjectInput{Name: name, Language: "en"}})
		if createErr != nil || result.Project == nil {
			t.Fatalf("create enterprise access project: result=%#v err=%v", result.Project, createErr)
		}
		return *result.Project
	}
	projectA := createProject("enterprise-project-a", "Enterprise project A")
	projectB := createProject("enterprise-project-b", "Enterprise project B")
	agentA := createLifecycleAgent(t, ctx, service, owner, projectA.Ref, "enterprise-agent-a", "Enterprise agent A")
	agentB := createLifecycleAgent(t, ctx, service, owner, projectA.Ref, "enterprise-agent-b", "Enterprise agent B")
	agentOtherProject := createLifecycleAgent(t, ctx, service, owner, projectB.Ref, "enterprise-agent-c", "Enterprise agent C")

	candidateInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000009003", ExternalTenantID: ownerInput.ExternalTenantID,
		ExternalDisplayName: "Restricted operator", CallerWorkload: "control-api-gateway", Operation: "platform.access.effective.explain",
	}
	if _, err := repository.ResolveProofAuthority(ctx, candidateInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("unbound OIDC identity received authority: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{
		Query: candidateInput.ExternalDisplayName, Page: query.Page{Size: 20},
	}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("list synchronized restricted OIDC identity: subjects=%#v err=%v", subjects, err)
	}
	candidateRef := subjects[0].Ref

	createRole := func(key, name string, permissions, scopes []string) entity.AccessRole {
		result, createErr := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.AccessRoleInput{
				Name: name, PermissionKeys: permissions, AllowedScopes: scopes, ChangeComment: "component scenario",
			}})
		if createErr != nil || result.AccessRole == nil {
			t.Fatalf("create enterprise access role: result=%#v err=%v", result.AccessRole, createErr)
		}
		return *result.AccessRole
	}
	projectViewer := createRole("enterprise-project-viewer", "Project viewer", []string{"project.view"}, []string{"PROJECT"})
	agentLauncher := createRole("enterprise-agent-launcher", "Exact agent launcher", []string{"agent.view", "agent.launch"}, []string{"RESOURCE_INSTANCE"})
	createBinding := func(key string, role entity.AccessRole, accessScope entity.AccessScope) {
		result, createErr := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.AccessBindingInput{
				SubjectKind: "USER", SubjectRef: candidateRef, RoleVersionRef: role.CurrentVersion.Ref, Scope: accessScope,
			}})
		if createErr != nil || result.AccessBinding == nil {
			t.Fatalf("create enterprise access binding: result=%#v err=%v", result.AccessBinding, createErr)
		}
	}
	createBinding("enterprise-bind-project-a", projectViewer, entity.AccessScope{Kind: "PROJECT", ProjectRef: projectA.Ref})
	createBinding("enterprise-bind-agent-a", agentLauncher, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: projectA.Ref, ResourceKind: "AGENT", ResourceRef: agentA.Ref})
	authority, err := repository.ResolveProofAuthority(ctx, candidateInput)
	if err != nil {
		t.Fatalf("resolve restricted OIDC identity after binding: %v", err)
	}
	candidate := value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID,
		Permission: candidateInput.Operation, CorrelationRef: "enterprise-access-candidate", CallerWorkload: "control-api-gateway", CredentialRevision: 1}
	resolvedCandidate, err := repository.ResolvePrincipal(ctx, candidate)
	if err != nil {
		t.Fatalf("resolve restricted application principal: %v", err)
	}
	var membershipRelationKind string
	if err := repository.pool.QueryRow(ctx, `
		SELECT relation.relkind::text
		FROM pg_catalog.pg_class relation
		JOIN pg_catalog.pg_namespace namespace ON namespace.oid = relation.relnamespace
		WHERE namespace.nspname = 'control_plane' AND relation.relname = 'memberships'
	`).Scan(&membershipRelationKind); err != nil || membershipRelationKind != "v" {
		t.Fatalf("membership presentation is not a view: kind=%q err=%v", membershipRelationKind, err)
	}
	var flattenedLaunchBindings int
	if err := repository.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM control_plane.memberships membership
		JOIN control_plane.subjects subject ON subject.id = membership.subject_id
		JOIN control_plane.projects project ON project.id = membership.project_id
		WHERE subject.ref = $1 AND project.ref = $2
		  AND 'LAUNCH_RUNS' = ANY(membership.permissions)
	`, resolvedCandidate.ActorID, projectA.Ref).Scan(&flattenedLaunchBindings); err != nil || flattenedLaunchBindings != 0 {
		t.Fatalf("exact Agent binding was flattened to project launch authority: count=%d err=%v", flattenedLaunchBindings, err)
	}

	explained, err := service.QueryEffectiveAccess(ctx, candidate, resolvedCandidate.ActorID,
		entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: projectA.Ref, ResourceKind: "AGENT", ResourceRef: agentA.Ref},
		[]string{"agent.launch"}, time.Time{})
	if err != nil || len(explained.Decisions) != 1 || !explained.Decisions[0].Allowed {
		t.Fatalf("exact agent explain failed: result=%#v err=%v", explained, err)
	}
	if _, err := service.QueryEffectiveAccess(ctx, candidate, resolvedCandidate.ActorID,
		entity.AccessScope{Kind: "RESOURCE_INSTANCE", ProjectRef: projectA.Ref, ResourceKind: "AGENT", ResourceRef: agentB.Ref},
		[]string{"agent.launch"}, time.Time{}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("foreign agent explain leaked resource existence: %v", err)
	}

	launch := func(key string, project entity.Project, agent entity.Agent) (command.Result, error) {
		return service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: candidate,
			Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.LaunchRunInput{
				ProjectRef: project.Ref, Task: "Run the bounded enterprise access scenario.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref},
			}})
	}
	allowed, err := launch("enterprise-launch-agent-a", projectA, agentA)
	if err != nil || allowed.Run == nil || allowed.Run.TitleSource != "SERVER_DEFAULT" || strings.TrimSpace(allowed.Run.Title) == "" {
		t.Fatalf("exact agent launch was denied: run=%#v err=%v", allowed.Run, err)
	}
	if _, err := launch("enterprise-launch-agent-b", projectA, agentB); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("other agent was not closed as not found: %v", err)
	}
	if _, err := launch("enterprise-launch-project-b", projectB, agentOtherProject); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("other project was not closed as not found: %v", err)
	}

	candidateInput.ProjectRef = projectA.Ref
	if _, err := repository.ResolveProofAuthority(ctx, candidateInput); err != nil {
		t.Fatalf("project A proof was denied: %v", err)
	}
	candidateInput.ProjectRef = projectB.Ref
	if _, err := repository.ResolveProofAuthority(ctx, candidateInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("project B proof was not denied: %v", err)
	}
}

func testInstructionDraftSave(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.instructions.create-draft",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct instruction service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "instruction-save-project"}, Payload: command.ProjectInput{
			Name: "Instruction drafts", Purpose: "Verify mutable instruction draft saves", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create instruction project: result=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "instruction-save-agent", "Instruction editor")
	firstVersion := agent.Version
	first, err := service.Execute(ctx, command.Command{Kind: command.CreateInstructions, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "instruction-save-first", ExpectedVersion: &firstVersion},
		Payload:  command.AgentInput{Ref: agent.Ref, Instructions: "First mutable instruction draft with enough content."},
	})
	if err != nil || first.Agent == nil {
		t.Fatalf("create instruction draft: result=%#v err=%v", first.Agent, err)
	}
	secondVersion := first.Agent.Version
	second, err := service.Execute(ctx, command.Command{Kind: command.CreateInstructions, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "instruction-save-second", ExpectedVersion: &secondVersion},
		Payload:  command.AgentInput{Ref: agent.Ref, Instructions: "Second mutable instruction draft replaces the first content."},
	})
	if err != nil || second.Agent == nil || second.Agent.Version != secondVersion+1 {
		t.Fatalf("replace instruction draft: result=%#v err=%v", second.Agent, err)
	}
	var count int
	var state, content string
	if err := repository.pool.QueryRow(ctx, bootstrapComponentInstructionDraftReadbackQuery, agent.Ref).Scan(&count, &state, &content); err != nil {
		t.Fatalf("read instruction draft: %v", err)
	}
	if count != 1 || state != "DRAFT" || content != "Second mutable instruction draft replaces the first content." {
		t.Fatalf("unexpected instruction draft readback: count=%d state=%s content=%q", count, state, content)
	}
}

func testProviderCredentialLegacyRepair(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	readback := func() (revision, accountVersion, count int64, uid, resourceVersion, digest string) {
		t.Helper()
		if err := pool.QueryRow(ctx, bootstrapComponentProviderCredentialReadbackQuery).Scan(
			&revision,
			&uid,
			&resourceVersion,
			&digest,
			&accountVersion,
			&count,
		); err != nil {
			t.Fatalf("read provider credential reconciliation: %v", err)
		}
		return
	}
	initialRevision, initialAccountVersion, initialCount, _, _, initialDigest := readback()
	if initialRevision != 1 || initialCount != 1 {
		t.Fatalf("unexpected initial provider credential state: revision=%d count=%d", initialRevision, initialCount)
	}
	const repairedUID = "10000000-0000-4000-8000-000000000002"
	const repairedResourceVersion = "2"
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName:            "runtime-provider-openai-default-r1",
		SecretUID:             repairedUID,
		SecretResourceVersion: repairedResourceVersion,
		ContentSHA256:         initialDigest,
	}); err != nil {
		t.Fatalf("configure repaired provider credential: %v", err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatalf("reconcile repaired provider credential: %v", err)
	}
	revision, accountVersion, count, uid, resourceVersion, digest := readback()
	if revision != 2 || accountVersion != initialAccountVersion+1 || count != 2 ||
		uid != repairedUID || resourceVersion != repairedResourceVersion || digest != initialDigest {
		t.Fatalf("unexpected repaired provider credential state: revision=%d account_version=%d count=%d uid=%s resource_version=%s digest_match=%t",
			revision, accountVersion, count, uid, resourceVersion, digest == initialDigest)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatalf("repeat provider credential reconciliation: %v", err)
	}
	repeatedRevision, repeatedAccountVersion, repeatedCount, repeatedUID, repeatedResourceVersion, repeatedDigest := readback()
	if repeatedRevision != revision || repeatedAccountVersion != accountVersion || repeatedCount != count ||
		repeatedUID != uid || repeatedResourceVersion != resourceVersion || repeatedDigest != digest {
		t.Fatal("repeated provider credential reconciliation was not idempotent")
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName:            "runtime-provider-openai-default-r1",
		SecretUID:             "10000000-0000-4000-8000-000000000003",
		SecretResourceVersion: "3",
		ContentSHA256:         strings.Repeat("f", 64),
	}); err != nil {
		t.Fatalf("configure drifted provider credential fixture: %v", err)
	}
	if err := repository.Bootstrap(ctx); err == nil {
		t.Fatal("provider credential content drift was accepted without an explicit revision")
	}
	finalRevision, finalAccountVersion, finalCount, finalUID, finalResourceVersion, finalDigest := readback()
	if finalRevision != revision || finalAccountVersion != accountVersion || finalCount != count ||
		finalUID != uid || finalResourceVersion != resourceVersion || finalDigest != digest {
		t.Fatal("rejected provider credential drift changed durable state")
	}
	if err := repository.ConfigureProviderCredential(ProviderCredentialConfig{
		SecretName:            "runtime-provider-openai-default-r1",
		SecretUID:             repairedUID,
		SecretResourceVersion: repairedResourceVersion,
		ContentSHA256:         initialDigest,
	}); err != nil {
		t.Fatalf("restore provider credential fixture: %v", err)
	}
}

func testSystemAssistantCorePromptUpgrade(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	upgrade := func(revision, prompt string) error {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := repository.reconcileSystemAssistantCorePrompt(ctx, tx, revision, prompt); err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	const upgradedRevision = "system-assistant-core-v3"
	const upgradedPrompt = "Platform-owned system assistant core prompt revision three."
	if err := upgrade(upgradedRevision, upgradedPrompt); err != nil {
		t.Fatalf("upgrade core prompt: %v", err)
	}
	if err := upgrade(upgradedRevision, upgradedPrompt); err != nil {
		t.Fatalf("repeat core prompt upgrade: %v", err)
	}
	var revision, state, desiredRevision, prompt string
	var versionNumber, promptCount, auditCount int
	if err := pool.QueryRow(ctx, bootstrapComponentCorePromptUpgradeReadbackQuery).Scan(
		&revision,
		&state,
		&desiredRevision,
		&prompt,
		&versionNumber,
		&promptCount,
		&auditCount,
	); err != nil {
		t.Fatalf("read upgraded core prompt: %v", err)
	}
	if revision != upgradedRevision || state != "RECOVERING" || desiredRevision != upgradedRevision ||
		prompt != upgradedPrompt || versionNumber != 2 || promptCount != 2 || auditCount != 1 {
		t.Fatalf("unexpected upgraded core prompt: revision=%s state=%s desired=%s version=%d prompts=%d audits=%d", revision, state, desiredRevision, versionNumber, promptCount, auditCount)
	}
	if err := upgrade(systemassistant.CorePromptRevision, systemassistant.CorePrompt()); err == nil {
		t.Fatal("core prompt rollback was accepted")
	}
}

func testOptionalInteractionIncident(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create",
	}, "control-api-gateway")
	runtimeWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	interactionWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "interaction-gateway", Operation: "platform.interactions.deliveries.claim",
	}, "interaction-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct optional interaction service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "interaction-project-create"},
		Payload: command.ProjectInput{Name: "Customer success", Purpose: "Prepare customer updates", Language: "en"},
	})
	if err != nil || project.Project == nil {
		t.Fatalf("create interaction project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "interaction-agent-create", "Customer success specialist")
	connection, err := service.Execute(ctx, command.Command{
		Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "interaction-connection-create"},
		Payload: command.ConnectionInput{DefinitionKey: "mattermost", Name: "Optional customer channel", PublicConfiguration: map[string]any{
			"base_url": "https://mattermost.example.test", "team_name": "customer-success", "channel_name": "ai-results",
		}},
	})
	if err != nil || connection.Connection == nil {
		t.Fatalf("create Mattermost connection: connection=%#v err=%v", connection.Connection, err)
	}
	connection, err = service.Execute(ctx, command.Command{
		Kind: command.ConfigureConnectionCredential, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "interaction-connection-credential", ExpectedVersion: &connection.Connection.Version},
		Payload: command.ConnectionInput{Ref: connection.Connection.Ref, MaterializationRef: "interaction-mattermost-token", CredentialRevision: &entity.IntegrationCredentialRevision{
			SecretRef: "kodex-system/kodex-integration-credentials#mattermost-token",
			SecretUID: "30000000-0000-4000-8000-000000000001", SecretResourceVersion: "1",
			ContentSHA256: strings.Repeat("a", 64),
		}},
	})
	if err != nil || connection.Connection == nil {
		t.Fatalf("configure Mattermost credential: connection=%#v err=%v", connection.Connection, err)
	}
	var connectedVersion int64
	if err := pool.QueryRow(ctx, bootstrapComponentConnectIntegrationQuery, connection.Connection.Ref).Scan(&connectedVersion); err != nil {
		t.Fatalf("materialize connected Mattermost fixture: %v", err)
	}
	granted, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "interaction-notification-grant", ExpectedVersion: &connectedVersion},
		Payload:  command.IntegrationGrantInput{ConnectionRef: connection.Connection.Ref, CapabilityKey: "mattermost.notifications", AgentRef: agent.Ref, Enabled: true},
	})
	if err != nil || granted.Connection == nil || len(granted.Connection.Grants) != 1 {
		t.Fatalf("grant Mattermost notification: connection=%#v err=%v", granted.Connection, err)
	}
	launched, err := service.Execute(ctx, command.Command{
		Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "interaction-run-launch"},
		Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Title: "Prepare account update", Task: "Prepare a concise account update.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}},
	})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch interaction run: run=%#v err=%v", launched.Run, err)
	}
	completed := claimAndCompleteRun(t, ctx, service, runtimeWorker, "interaction-run", false)
	if completed.Run == nil || completed.Run.State != "SUCCEEDED" {
		t.Fatalf("complete interaction run: run=%#v", completed.Run)
	}
	claims, err := service.ClaimInteractionDeliveries(ctx, interactionWorker, "interaction-component", 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim optional delivery: claims=%#v err=%v", claims, err)
	}
	claim := claims[0]
	failed, err := service.Execute(ctx, command.Command{
		Kind: command.CompleteInteractionDelivery, Principal: interactionWorker,
		Mutation: value.Mutation{IdempotencyKey: "interaction-delivery-failed"},
		Payload: command.InteractionDeliveryInput{
			DeliveryRef: stringMap(claim, "deliveryRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"),
			Generation: claim["generation"].(int64), Success: false, SafeErrorCode: "INTERACTION_UNAVAILABLE",
		},
	})
	if err != nil || failed.Event == nil || failed.Event.Type != "INCIDENT_LINKED" || failed.Event.Delta.Incident == nil || failed.Event.Delta.Incident.CoreAffected {
		t.Fatalf("record optional delivery incident: event=%#v err=%v", failed.Event, err)
	}
	readback, err := service.GetRun(ctx, owner, completed.Run.Ref)
	if err != nil || readback.State != "SUCCEEDED" || len(readback.Incidents) != 1 || readback.Incidents[0].State != "RECOVERING" || readback.Incidents[0].CoreAffected {
		t.Fatalf("optional failure changed core run or lost incident: run=%#v err=%v", readback, err)
	}
	events, sequence, complete, err := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: completed.Run.Ref, Limit: 100})
	if err != nil || !complete || len(events) == 0 || events[len(events)-1].Type != "INCIDENT_LINKED" || events[len(events)-1].IncidentRef != readback.Incidents[0].Ref || sequence != events[len(events)-1].Sequence {
		t.Fatalf("read incident from resumable stream: events=%#v sequence=%d complete=%v err=%v", events, sequence, complete, err)
	}
	if _, err := pool.Exec(ctx, bootstrapComponentMakeInteractionDeliveryDueQuery, stringMap(claim, "deliveryRef")); err != nil {
		t.Fatalf("make failed delivery retryable: %v", err)
	}
	retryClaims, err := service.ClaimInteractionDeliveries(ctx, interactionWorker, "interaction-component", 1)
	if err != nil || len(retryClaims) != 1 {
		t.Fatalf("claim optional delivery retry: claims=%#v err=%v", retryClaims, err)
	}
	retry := retryClaims[0]
	recovered, err := service.Execute(ctx, command.Command{
		Kind: command.CompleteInteractionDelivery, Principal: interactionWorker,
		Mutation: value.Mutation{IdempotencyKey: "interaction-delivery-recovered"},
		Payload: command.InteractionDeliveryInput{
			DeliveryRef: stringMap(retry, "deliveryRef"), LeaseRef: stringMap(retry, "leaseRef"), Fence: stringMap(retry, "fence"),
			Generation: retry["generation"].(int64), Success: true, ExternalPostRef: "post-component-001",
		},
	})
	if err != nil || recovered.Event == nil || recovered.Event.Delta.Incident == nil || recovered.Event.Delta.Incident.State != "RESOLVED" {
		t.Fatalf("resolve optional delivery incident: event=%#v err=%v", recovered.Event, err)
	}
	readback, err = service.GetRun(ctx, owner, completed.Run.Ref)
	if err != nil || readback.State != "SUCCEEDED" || len(readback.Incidents) != 1 || readback.Incidents[0].State != "RESOLVED" {
		t.Fatalf("read recovered optional delivery: run=%#v err=%v", readback, err)
	}
}

func testIntegrationConfigurationAndGrants(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct integration service: %v", err)
	}
	definitions, actions, err := service.ListIntegrationDefinitions(ctx, owner, "")
	if err != nil || len(definitions) != 7 {
		t.Fatalf("list integration definitions: definitions=%d err=%v", len(definitions), err)
	}
	if !contains(actions, "CREATE_CONNECTION") {
		t.Fatalf("owner integration collection actions=%v, want CREATE_CONNECTION", actions)
	}
	for _, definition := range definitions {
		if len(definition.ConfigurationFields) == 0 {
			t.Fatalf("definition %s has no typed configuration fields", definition.Key)
		}
		for _, capability := range definition.Capabilities {
			if !contains([]string{"READ", "WRITE", "SENSITIVE", "DESTRUCTIVE"}, capability.Risk) {
				t.Fatalf("definition %s exposes unsupported risk %s", definition.Key, capability.Risk)
			}
		}
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-invalid-configuration"},
		Payload: command.ConnectionInput{DefinitionKey: "github", Name: "Unsafe connection", PublicConfiguration: map[string]any{"owner": "example", "repository": "knowledge", "token": "must-not-enter-browser-contract"}},
	}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("unknown or secret-like public configuration field accepted: %v", err)
	}
	created, err := service.Execute(ctx, command.Command{
		Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-synthetic-create"},
		Payload: command.ConnectionInput{DefinitionKey: "synthetic", Name: "Synthetic journal", PublicConfiguration: map[string]any{"journal": "component-main"}},
	})
	if err != nil || created.Connection == nil || created.Connection.MaskedCredentialsState != "CONFIGURED" || created.Connection.State != "NOT_CONNECTED" || len(created.Connection.Capabilities) != 2 {
		t.Fatalf("create integration connection: connection=%#v err=%v", created.Connection, err)
	}
	project, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-project-create"},
		Payload: command.ProjectInput{Name: "Sales enablement", Purpose: "Prepare customer knowledge", Language: "en"},
	})
	if err != nil || project.Project == nil {
		t.Fatalf("create integration project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "integration-agent-create", "Sales knowledge curator")
	var connectedVersion int64
	if err := pool.QueryRow(ctx, bootstrapComponentConnectIntegrationQuery, created.Connection.Ref).Scan(&connectedVersion); err != nil {
		t.Fatalf("materialize tested integration fixture: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-grant-stale", ExpectedVersion: &created.Connection.Version},
		Payload: command.IntegrationGrantInput{ConnectionRef: created.Connection.Ref, CapabilityKey: "synthetic.journal.read", AgentRef: agent.Ref, Enabled: true},
	}); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("stale integration connection version accepted: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-grant-two-targets", ExpectedVersion: &connectedVersion},
		Payload: command.IntegrationGrantInput{ConnectionRef: created.Connection.Ref, CapabilityKey: "synthetic.journal.read", AgentRef: agent.Ref, WorkflowRef: "wfl_forged", Enabled: true},
	}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("grant with two targets accepted: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-grant-unknown-target", ExpectedVersion: &connectedVersion},
		Payload: command.IntegrationGrantInput{ConnectionRef: created.Connection.Ref, CapabilityKey: "synthetic.journal.read", AgentRef: "agt_foreign", Enabled: true},
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("unknown integration target accepted: %v", err)
	}
	granted, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-grant-create", ExpectedVersion: &connectedVersion},
		Payload: command.IntegrationGrantInput{ConnectionRef: created.Connection.Ref, CapabilityKey: "synthetic.journal.read", AgentRef: agent.Ref, Enabled: true},
	})
	if err != nil || granted.Connection == nil || granted.Connection.Version != connectedVersion+1 || len(granted.Connection.Grants) != 1 || granted.Connection.Grants[0].TargetName != agent.Name || !granted.Connection.Grants[0].Enabled {
		t.Fatalf("create authoritative integration grant: connection=%#v err=%v", granted.Connection, err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-grant-unknown-capability", ExpectedVersion: &granted.Connection.Version},
		Payload: command.IntegrationGrantInput{ConnectionRef: created.Connection.Ref, CapabilityKey: "github.admin", AgentRef: agent.Ref, Enabled: true},
	}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("unknown integration capability accepted: %v", err)
	}
	revoked, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeIntegrationGrant, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-grant-revoke", ExpectedVersion: &granted.Connection.Version},
		Payload: command.IntegrationGrantInput{ConnectionRef: created.Connection.Ref, CapabilityKey: "synthetic.journal.read", AgentRef: agent.Ref, Enabled: false},
	})
	if err != nil || revoked.Connection == nil || len(revoked.Connection.Grants) != 1 || revoked.Connection.Grants[0].Enabled {
		t.Fatalf("revoke integration grant: connection=%#v err=%v", revoked.Connection, err)
	}
}

func testIntegrationEffectLifecycle(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create",
	}, "control-api-gateway")
	runtimeWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	gateway := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "integration-gateway", Operation: "platform.runtime.integrations.claim",
	}, "integration-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	created, err := service.Execute(ctx, command.Command{
		Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-effect-connection"},
		Payload: command.ConnectionInput{DefinitionKey: "synthetic", Name: "Effect journal", PublicConfiguration: map[string]any{"journal": "effect-main"}},
	})
	if err != nil || created.Connection == nil {
		t.Fatalf("create effect connection: connection=%#v err=%v", created.Connection, err)
	}
	var connectedVersion int64
	if err := pool.QueryRow(ctx, bootstrapComponentConnectIntegrationQuery, created.Connection.Ref).Scan(&connectedVersion); err != nil {
		t.Fatal(err)
	}
	project, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-effect-project"},
		Payload: command.ProjectInput{Name: "Integration effects", Purpose: "Exercise protected effects", Language: "en"},
	})
	if err != nil || project.Project == nil {
		t.Fatalf("create effect project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "integration-effect-agent", "Integration operator")
	readGranted, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-effect-read-grant", ExpectedVersion: &connectedVersion},
		Payload:  command.IntegrationGrantInput{ConnectionRef: created.Connection.Ref, CapabilityKey: "synthetic.journal.read", AgentRef: agent.Ref, Enabled: true},
	})
	if err != nil || readGranted.Connection == nil || len(readGranted.Connection.Grants) != 1 ||
		readGranted.Connection.Grants[0].Risk != "READ" || readGranted.Connection.Grants[0].ApprovalPolicy != "NONE" {
		t.Fatalf("create read grant: connection=%#v err=%v", readGranted.Connection, err)
	}
	granted, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-effect-write-grant", ExpectedVersion: &readGranted.Connection.Version},
		Payload:  command.IntegrationGrantInput{ConnectionRef: created.Connection.Ref, CapabilityKey: "synthetic.journal.write", AgentRef: agent.Ref, Enabled: true},
	})
	var writeGrant *entity.IntegrationGrant
	if granted.Connection != nil {
		for index := range granted.Connection.Grants {
			if granted.Connection.Grants[index].CapabilityKey == "synthetic.journal.write" {
				writeGrant = &granted.Connection.Grants[index]
				break
			}
		}
	}
	if err != nil || granted.Connection == nil || len(granted.Connection.Grants) != 2 || writeGrant == nil ||
		writeGrant.Risk != "WRITE" || writeGrant.ApprovalPolicy != "HUMAN_EACH_EFFECT" {
		t.Fatalf("create write grant: connection=%#v err=%v", granted.Connection, err)
	}
	rejectedRun, err := service.Execute(ctx, command.Command{
		Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-effect-run"},
		Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Title: "Read and reject journal write", Task: "Read the journal and request one rejected write.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}},
	})
	if err != nil || rejectedRun.Run == nil {
		t.Fatalf("launch rejected effect run: run=%#v err=%v", rejectedRun.Run, err)
	}
	rejectedExecutionResult, err := service.Execute(ctx, command.Command{
		Kind: command.ClaimExecution, Principal: runtimeWorker, Mutation: value.Mutation{IdempotencyKey: "integration-effect-runtime-claim"},
		Payload: command.LeaseInput{WorkloadInstance: "runtime-integration-effect", Limit: 1},
	})
	if err != nil || len(rejectedExecutionResult.RuntimeItems) != 1 {
		t.Fatalf("claim rejected effect runtime: claims=%d err=%v", len(rejectedExecutionResult.RuntimeItems), err)
	}
	rejectedExecution := rejectedExecutionResult.RuntimeItems[0]
	readResolved, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{
		"run_ref": stringMap(rejectedExecution, "runRef"), "node_ref": stringMap(rejectedExecution, "nodeRef"),
		"connection_ref": created.Connection.Ref, "capability_key": "synthetic.journal.read",
		"idempotency_key": "integration-effect-read-invocation",
	}, map[string]any{})
	if err != nil || stringMap(readResolved, "state") != "READY" || stringMap(readResolved, "gateRef") != "" {
		t.Fatalf("resolve read invocation without gate: result=%#v err=%v", readResolved, err)
	}
	readClaims, err := service.ClaimIntegrationInvocations(ctx, gateway, "integration-gateway-component", 1)
	if err != nil || len(readClaims) != 1 || stringMap(readClaims[0], "capabilityKey") != "synthetic.journal.read" {
		t.Fatalf("claim read invocation without gate: claims=%#v err=%v", readClaims, err)
	}
	readClaim := readClaims[0]
	readSummary := `{"journal":"effect-main","effect_key":"` + stringMap(readClaim, "effectKey") + `","sequence":0,"value":"","count":0}`
	readResponseDigest := sha256.Sum256([]byte(readSummary))
	if completedRead, err := service.Execute(ctx, command.Command{
		Kind: command.CompleteIntegrationInvocation, Principal: gateway,
		Mutation: value.Mutation{IdempotencyKey: "integration-effect-read-complete"},
		Payload: command.IntegrationInvocationInput{
			InvocationRef: stringMap(readClaim, "invocationRef"), LeaseRef: stringMap(readClaim, "leaseRef"),
			Fence: stringMap(readClaim, "fence"), Generation: readClaim["generation"].(int64), Success: true,
			ResultSummary: readSummary, EffectKey: stringMap(readClaim, "effectKey"), InputDigest: stringMap(readClaim, "inputDigest"),
			ProviderEffectRef: "synthetic-journal:effect-main", ResponseDigest: hex.EncodeToString(readResponseDigest[:]),
		},
	}); err != nil || completedRead.Run == nil {
		t.Fatalf("complete read invocation: result=%#v err=%v", completedRead.Run, err)
	}
	rejected, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{
		"run_ref": stringMap(rejectedExecution, "runRef"), "node_ref": stringMap(rejectedExecution, "nodeRef"),
		"connection_ref": created.Connection.Ref, "capability_key": "synthetic.journal.write",
		"idempotency_key": "integration-effect-rejected-invocation",
	}, map[string]any{"value": "rejected-value"})
	if err != nil || stringMap(rejected, "state") != "WAITING_APPROVAL" || stringMap(rejected, "gateRef") == "" {
		t.Fatalf("resolve rejected invocation: result=%#v err=%v", rejected, err)
	}
	beforeRejection, err := service.ClaimIntegrationInvocations(ctx, gateway, "integration-gateway-component", 1)
	if err != nil || len(beforeRejection) != 0 {
		t.Fatalf("claim write before rejected Human Gate: claims=%#v err=%v", beforeRejection, err)
	}
	gateVersion := int64(1)
	rejection, err := service.Execute(ctx, command.Command{
		Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-effect-reject", ExpectedVersion: &gateVersion},
		Payload:  command.GateResolutionInput{GateRef: stringMap(rejected, "gateRef"), Decision: "REJECT", Comment: "Reject exact journal write"},
	})
	if err != nil || rejection.Gate == nil || rejection.Gate.State != "REJECTED" || rejection.Run == nil || rejection.Run.State != "FAILED" {
		t.Fatalf("reject integration effect: gate=%#v run=%#v err=%v", rejection.Gate, rejection.Run, err)
	}
	afterRejection, err := service.ClaimIntegrationInvocations(ctx, gateway, "integration-gateway-component", 1)
	if err != nil || len(afterRejection) != 0 {
		t.Fatalf("claim rejected effect: claims=%#v err=%v", afterRejection, err)
	}
	rejectedReadback, err := service.GetIntegrationInvocation(ctx, runtimeWorker, stringMap(rejected, "invocationRef"))
	if err != nil || stringMap(rejectedReadback, "state") != "REJECTED" ||
		stringMap(rejectedReadback, "safeErrorCode") != "INTEGRATION_REJECTED_BY_OWNER" || stringMap(rejectedReadback, "effectReceiptRef") != "" {
		t.Fatalf("read rejected invocation without effect: result=%#v err=%v", rejectedReadback, err)
	}
	var rejectedReceiptCount int
	var rejectedEffectKey string
	if err := pool.QueryRow(ctx, bootstrapComponentIntegrationInvocationEffectKeyQuery, stringMap(rejected, "invocationRef")).Scan(&rejectedEffectKey); err != nil {
		t.Fatalf("read rejected effect key: %v", err)
	}
	if err := pool.QueryRow(ctx, bootstrapComponentEffectReceiptCountQuery, rejectedEffectKey).Scan(&rejectedReceiptCount); err != nil || rejectedReceiptCount != 0 {
		t.Fatalf("rejected effect receipt count=%d err=%v", rejectedReceiptCount, err)
	}

	launched, err := service.Execute(ctx, command.Command{
		Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "integration-effect-approved-run"},
		Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Title: "Approve journal write", Task: "Write one bounded journal entry after approval.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}},
	})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch approved effect run: run=%#v err=%v", launched.Run, err)
	}
	approvedExecutionResult, err := service.Execute(ctx, command.Command{
		Kind: command.ClaimExecution, Principal: runtimeWorker, Mutation: value.Mutation{IdempotencyKey: "integration-effect-approved-runtime-claim"},
		Payload: command.LeaseInput{WorkloadInstance: "runtime-integration-effect", Limit: 1},
	})
	if err != nil || len(approvedExecutionResult.RuntimeItems) != 1 {
		t.Fatalf("claim approved effect runtime: claims=%d err=%v", len(approvedExecutionResult.RuntimeItems), err)
	}
	execution := approvedExecutionResult.RuntimeItems[0]
	resolved, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{
		"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"),
		"connection_ref": created.Connection.Ref, "capability_key": "synthetic.journal.write",
		"idempotency_key": "integration-effect-approved-invocation",
	}, map[string]any{"value": "approved-value"})
	if err != nil || stringMap(resolved, "state") != "WAITING_APPROVAL" || stringMap(resolved, "gateRef") == "" {
		t.Fatalf("resolve protected invocation: result=%#v err=%v", resolved, err)
	}
	beforeApproval, err := service.ClaimIntegrationInvocations(ctx, gateway, "integration-gateway-component", 1)
	if err != nil || len(beforeApproval) != 0 {
		t.Fatalf("claim before Human Gate: claims=%#v err=%v", beforeApproval, err)
	}
	approved, err := service.Execute(ctx, command.Command{
		Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-effect-approve", ExpectedVersion: &gateVersion},
		Payload:  command.GateResolutionInput{GateRef: stringMap(resolved, "gateRef"), Decision: "APPROVE", Comment: "Approved exact journal write"},
	})
	if err != nil || approved.Gate == nil || approved.Gate.State != "APPROVED" || approved.Run == nil || approved.Run.State != "RUNNING" {
		t.Fatalf("approve integration effect: gate=%#v run=%#v err=%v", approved.Gate, approved.Run, err)
	}
	claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "integration-gateway-component", 1)
	if err != nil || len(claims) != 1 {
		t.Fatalf("claim approved effect: claims=%#v err=%v", claims, err)
	}
	claim := claims[0]
	resultSummary := `{"journal":"effect-main","effect_key":"` + stringMap(claim, "effectKey") + `","sequence":1,"value":"approved-value","count":1}`
	responseDigest := sha256.Sum256([]byte(resultSummary))
	completion := command.IntegrationInvocationInput{
		InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"),
		Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), Success: true,
		ResultSummary: resultSummary, EffectKey: stringMap(claim, "effectKey"), InputDigest: stringMap(claim, "inputDigest"),
		ProviderEffectRef: "synthetic-journal:effect-main:1", ResponseDigest: hex.EncodeToString(responseDigest[:]),
	}
	completed, err := service.Execute(ctx, command.Command{
		Kind: command.CompleteIntegrationInvocation, Principal: gateway,
		Mutation: value.Mutation{IdempotencyKey: "integration-effect-complete"}, Payload: completion,
	})
	if err != nil || completed.Run == nil {
		t.Fatalf("complete integration effect: result=%#v err=%v", completed.Run, err)
	}
	duplicate, err := service.Execute(ctx, command.Command{
		Kind: command.CompleteIntegrationInvocation, Principal: gateway,
		Mutation: value.Mutation{IdempotencyKey: "integration-effect-complete-readback"}, Payload: completion,
	})
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("read duplicate effect receipt: duplicate=%v err=%v", duplicate.Duplicate, err)
	}
	mismatch := completion
	mismatch.ResponseDigest = strings.Repeat("f", 64)
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.CompleteIntegrationInvocation, Principal: gateway,
		Mutation: value.Mutation{IdempotencyKey: "integration-effect-complete-mismatch"}, Payload: mismatch,
	}); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("mismatched effect receipt error=%v, want forbidden", err)
	}
	var receiptCount int
	if err := pool.QueryRow(ctx, bootstrapComponentEffectReceiptCountQuery, stringMap(claim, "effectKey")).Scan(&receiptCount); err != nil || receiptCount != 1 {
		t.Fatalf("effect receipt count=%d err=%v", receiptCount, err)
	}
	afterCompletion, err := service.ClaimIntegrationInvocations(ctx, gateway, "integration-gateway-component", 1)
	if err != nil || len(afterCompletion) != 0 {
		t.Fatalf("claim completed effect retry: claims=%#v err=%v", afterCompletion, err)
	}
	run, err := service.GetRun(ctx, owner, launched.Run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-effect-cleanup", ExpectedVersion: &run.Version},
		Payload:  command.RunCommandInput{RunRef: run.Ref, Reason: "Component test cleanup"},
	}); err != nil {
		t.Fatalf("cleanup integration effect run: %v", err)
	}
}

func testProjectMembershipCandidate(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	ownerInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Installation owner", ExternalEmailHint: "o***@example.test",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}
	owner := resolvedTestPrincipal(t, ctx, repository, ownerInput, "control-api-gateway")
	lockTx, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin unrelated organization update: %v", err)
	}
	defer func() { _ = lockTx.Rollback(ctx) }()
	var lockedOrganizationID string
	if err := lockTx.QueryRow(ctx, `SELECT id::text FROM control_plane.organizations LIMIT 1 FOR UPDATE`).Scan(&lockedOrganizationID); err != nil {
		t.Fatalf("lock organization fixture: %v", err)
	}
	fastPathContext, cancelFastPath := context.WithTimeout(ctx, time.Second)
	if _, err := repository.ResolveProofAuthority(fastPathContext, ownerInput); err != nil {
		cancelFastPath()
		t.Fatalf("resolve existing owner while organization is being updated: %v", err)
	}
	cancelFastPath()
	if err := lockTx.Rollback(ctx); err != nil {
		t.Fatalf("release organization fixture lock: %v", err)
	}
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct membership service: %v", err)
	}
	created, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-project-create"},
		Payload: command.ProjectInput{Name: "Access validation", Purpose: "Validate member onboarding", Language: "en"},
	})
	if err != nil || created.Project == nil {
		t.Fatalf("create membership project: project=%#v err=%v", created.Project, err)
	}
	projectRef := created.Project.Ref
	candidateInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000003", ExternalTenantID: ownerInput.ExternalTenantID,
		ExternalDisplayName: "Alex Morgan", ExternalEmailHint: "a***@example.test",
		CallerWorkload: "control-api-gateway", Operation: "platform.query.membership-candidates.list", ProjectRef: projectRef,
	}
	if _, err := repository.ResolveProofAuthority(ctx, candidateInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("unknown OIDC subject received authority before membership: %v", err)
	}
	organizationCandidates, _, err := service.ListPlatformMembershipCandidates(ctx, owner, query.Filter{Query: "Alex", Page: query.Page{Size: 20}})
	if err != nil || len(organizationCandidates) != 1 || organizationCandidates[0].DisplayName != candidateInput.ExternalDisplayName || organizationCandidates[0].EmailMasked != candidateInput.ExternalEmailHint {
		t.Fatalf("list organization membership candidate: candidates=%#v err=%v", organizationCandidates, err)
	}
	organizationMember, err := service.Execute(ctx, command.Command{
		Kind: command.AddPlatformMembership, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "organization-membership-candidate-add"},
		Payload: command.PlatformMembershipInput{UserRef: organizationCandidates[0].Ref, Role: "OPERATOR", Active: true},
	})
	if err != nil || organizationMember.Membership == nil || !organizationMember.Membership.Active || organizationMember.Membership.Role != "OPERATOR" {
		t.Fatalf("add organization membership: membership=%#v err=%v", organizationMember.Membership, err)
	}
	if _, err := repository.ResolveProofAuthority(ctx, candidateInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("organization member received project authority before project membership: %v", err)
	}
	candidates, _, err := service.ListMembershipCandidates(ctx, owner, query.Filter{ProjectRef: projectRef, Query: "Alex", Page: query.Page{Size: 20}})
	if err != nil || len(candidates) != 1 || candidates[0].Ref != organizationCandidates[0].Ref {
		t.Fatalf("list project membership candidate: candidates=%#v err=%v", candidates, err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.AddMembership, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-system-subject-rejected"},
		Payload: command.MembershipInput{ProjectRef: projectRef, UserRef: "sys_platform", Permissions: []string{"VIEW"}, Active: true},
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("system subject accepted as project member: %v", err)
	}
	added, err := service.Execute(ctx, command.Command{
		Kind: command.AddMembership, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-candidate-add"},
		Payload: command.MembershipInput{ProjectRef: projectRef, UserRef: candidates[0].Ref, Permissions: []string{"VIEW", "MANAGE_MEMBERS"}, Active: true},
	})
	if err != nil || added.Membership == nil || !added.Membership.Active {
		t.Fatalf("add project membership: membership=%#v err=%v", added.Membership, err)
	}
	var presentationKind string
	var canonicalPermissions []string
	var projectionRows, roleVersionRows int
	if err := repository.pool.QueryRow(ctx, `
		SELECT binding.presentation_kind,
		       role_version.permission_keys,
		       (SELECT count(*) FROM control_plane.memberships membership WHERE membership.ref = binding.ref),
		       (SELECT count(*) FROM control_plane.application_role_versions version WHERE version.role_id = role.id)
		FROM control_plane.access_bindings binding
		JOIN control_plane.application_role_versions role_version ON role_version.id = binding.role_version_id
		JOIN control_plane.application_roles role ON role.id = role_version.role_id
		WHERE binding.ref = $1
	`, added.Membership.Ref).Scan(&presentationKind, &canonicalPermissions, &projectionRows, &roleVersionRows); err != nil ||
		presentationKind != "PROJECT_MEMBERSHIP" || projectionRows != 1 || roleVersionRows != 1 ||
		!contains(canonicalPermissions, "project.view") || !contains(canonicalPermissions, "access.manage") {
		t.Fatalf("project membership is not a canonical projection: kind=%q permissions=%v projection=%d versions=%d err=%v",
			presentationKind, canonicalPermissions, projectionRows, roleVersionRows, err)
	}
	candidateAuthority, err := repository.ResolveProofAuthority(ctx, candidateInput)
	if err != nil {
		t.Fatalf("resolve candidate after membership: %v", err)
	}
	candidate := value.Principal{
		ActorID: candidateAuthority.ActorID, AuthorityTenant: candidateAuthority.OrganizationID,
		Permission: candidateInput.Operation, CorrelationRef: "membership-candidate-component",
		CallerWorkload: "control-api-gateway", ProjectRef: projectRef, CredentialRevision: 1,
	}
	memberships, _, err := service.ListMemberships(ctx, candidate, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(memberships) != 2 {
		t.Fatalf("member cannot use granted project permission: memberships=%d err=%v", len(memberships), err)
	}
	actionAgent := createLifecycleAgent(t, ctx, service, owner, projectRef, "membership-action-agent", "Readback analyst")
	agents, _, err := service.ListAgents(ctx, candidate, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(agents) != 1 || agents[0].Ref != actionAgent.Ref || len(agents[0].NextActions) != 1 || agents[0].NextActions[0] != "OPEN" {
		t.Fatalf("read-only agent actions are not authoritative: agents=%#v err=%v", agents, err)
	}
	agentReadback, err := service.GetAgent(ctx, candidate, actionAgent.Ref)
	if err != nil || len(agentReadback.NextActions) != 1 || agentReadback.NextActions[0] != "OPEN" {
		t.Fatalf("read-only agent detail exposed mutations: agent=%#v err=%v", agentReadback, err)
	}
	workflowDraft := entity.WorkflowVersion{
		Name: "Readback process", Purpose: "Validate actor-scoped actions", CoordinatorAgentRef: actionAgent.Ref,
		VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600, CompletionCriteria: "A bounded result is produced", ResultSchema: map[string]any{},
		Steps: []entity.WorkflowStep{{Key: "analyze", Position: 1, Name: "Analyze", AgentRef: actionAgent.Ref, Instructions: "Analyze the bounded input.", TimeoutSeconds: 900, ExpectedResult: "A bounded result"}},
	}
	workflowResult, err := service.Execute(ctx, command.Command{
		Kind: command.CreateWorkflow, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-action-workflow"},
		Payload: command.WorkflowInput{ProjectRef: projectRef, Name: workflowDraft.Name, Purpose: workflowDraft.Purpose, CoordinatorAgentRef: actionAgent.Ref, Draft: &workflowDraft},
	})
	if err != nil || workflowResult.Workflow == nil {
		t.Fatalf("create action readback workflow: workflow=%#v err=%v", workflowResult.Workflow, err)
	}
	workflows, _, err := service.ListWorkflows(ctx, candidate, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(workflows) != 1 || len(workflows[0].NextActions) != 1 || workflows[0].NextActions[0] != "OPEN" {
		t.Fatalf("read-only workflow actions are not authoritative: workflows=%#v err=%v", workflows, err)
	}
	scheduleResult, err := service.Execute(ctx, command.Command{
		Kind: command.CreateSchedule, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-action-schedule"},
		Payload: command.ScheduleInput{ProjectRef: projectRef, Name: "Daily readback", Target: entity.RunTarget{Type: "AGENT", Ref: actionAgent.Ref}, Preset: "DAILY", TimeOfDay: "09:00", Timezone: "UTC", Input: map[string]any{"task": "Prepare a bounded daily summary."}, SessionPolicy: "NEW_EACH_RUN", NotificationPolicy: "CONTROL_CENTER_ONLY"},
	})
	if err != nil || scheduleResult.Schedule == nil {
		t.Fatalf("create action readback schedule: schedule=%#v err=%v", scheduleResult.Schedule, err)
	}
	schedules, _, err := service.ListSchedules(ctx, candidate, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(schedules) != 1 || len(schedules[0].NextActions) != 1 || schedules[0].NextActions[0] != "OPEN" {
		t.Fatalf("read-only schedule actions are not authoritative: schedules=%#v err=%v", schedules, err)
	}
	if schedules[0].TimeOfDay != "09:00" || schedules[0].CronExpression != "0 9 * * *" || schedules[0].NextRunAt == nil {
		t.Fatalf("owner-friendly schedule was not normalized: %#v", schedules[0])
	}
	scheduleDetail, err := service.GetSchedule(ctx, candidate, scheduleResult.Schedule.Ref)
	if err != nil || scheduleDetail.Ref != scheduleResult.Schedule.Ref || !reflect.DeepEqual(scheduleDetail.NextActions, []string{"OPEN"}) {
		t.Fatalf("read-only schedule detail is not authoritative: schedule=%#v err=%v", scheduleDetail, err)
	}
	readOnlyVersion := scheduleResult.Schedule.Version
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ArchiveSchedule, Principal: candidate,
		Mutation: value.Mutation{IdempotencyKey: "membership-action-schedule-archive-denied", ExpectedVersion: &readOnlyVersion},
		Payload:  command.ScheduleInput{Ref: scheduleResult.Schedule.Ref},
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("read-only actor archived schedule: %v", err)
	}
	runResult, err := service.Execute(ctx, command.Command{
		Kind: command.LaunchRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-action-run"},
		Payload: command.LaunchRunInput{ProjectRef: projectRef, Title: "Readback run", Task: "Produce a bounded readback result.", Target: entity.RunTarget{Type: "AGENT", Ref: actionAgent.Ref}},
	})
	if err != nil || runResult.Run == nil {
		t.Fatalf("create action readback run: run=%#v err=%v", runResult.Run, err)
	}
	runs, _, err := service.ListRuns(ctx, candidate, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || len(runs) != 1 || len(runs[0].NextActions) != 1 || runs[0].NextActions[0] != "OPEN" {
		t.Fatalf("read-only run actions are not authoritative: runs=%#v err=%v", runs, err)
	}
	runVersion := runResult.Run.Version
	if cancelled, cancelErr := service.Execute(ctx, command.Command{
		Kind: command.CancelRun, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-action-run-cancel", ExpectedVersion: &runVersion},
		Payload: command.RunCommandInput{RunRef: runResult.Run.Ref, Reason: "Finish action readback fixture"},
	}); cancelErr != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
		t.Fatalf("close action readback run: run=%#v err=%v", cancelled.Run, cancelErr)
	}
	auditEvents, _, err := service.ListAuditEvents(ctx, owner, query.Filter{Query: "Readback run", Page: query.Page{Size: 20}})
	if err != nil || len(auditEvents) < 2 {
		t.Fatalf("search audit by safe resource name: events=%#v err=%v", auditEvents, err)
	}
	for _, auditEvent := range auditEvents {
		if auditEvent.ResourceName != "Readback run" || auditEvent.ResourceRef != runResult.Run.Ref {
			t.Fatalf("audit readback exposed an unresolved resource: %#v", auditEvent)
		}
	}
	hiddenAuditEvents, _, err := service.ListAuditEvents(ctx, candidate, query.Filter{Query: "Readback run", Page: query.Page{Size: 20}})
	if err != nil || len(hiddenAuditEvents) != 0 {
		t.Fatalf("audit readback ignored VIEW_AUDIT eligibility: events=%#v err=%v", hiddenAuditEvents, err)
	}
	remaining, _, err := service.ListMembershipCandidates(ctx, owner, query.Filter{ProjectRef: projectRef, Query: "Alex", Page: query.Page{Size: 20}})
	if err != nil || len(remaining) != 0 {
		t.Fatalf("assigned member remained a candidate: candidates=%#v err=%v", remaining, err)
	}
	foreign, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "membership-foreign-project"},
		Payload: command.ProjectInput{Name: "Foreign access validation", Purpose: "Validate project isolation", Language: "en"},
	})
	if err != nil || foreign.Project == nil {
		t.Fatalf("create foreign project: project=%#v err=%v", foreign.Project, err)
	}
	visibleSearch, err := service.Search(ctx, candidate, query.Filter{Query: "Readback", Limit: 20})
	if err != nil || len(visibleSearch) < 3 {
		t.Fatalf("search eligible project resources: results=%#v err=%v", visibleSearch, err)
	}
	for _, result := range visibleSearch {
		if result.ProjectRef != projectRef {
			t.Fatalf("search leaked foreign project result: %#v", result)
		}
	}
	foreignSearch, err := service.Search(ctx, candidate, query.Filter{Query: "Foreign access", Limit: 20})
	if err != nil || len(foreignSearch) != 0 {
		t.Fatalf("search exposed inaccessible project: results=%#v err=%v", foreignSearch, err)
	}
	ownerSearch, err := service.Search(ctx, owner, query.Filter{Query: "Foreign access", Limit: 20})
	if err != nil || len(ownerSearch) != 1 || ownerSearch[0].Ref != foreign.Project.Ref {
		t.Fatalf("owner search omitted accessible project: results=%#v err=%v", ownerSearch, err)
	}
	foreignInput := candidateInput
	foreignInput.ProjectRef = foreign.Project.Ref
	if _, err := repository.ResolveProofAuthority(ctx, foreignInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("candidate received foreign project authority: %v", err)
	}
	foreignVersion := added.Membership.Version
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeMembership, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "membership-foreign-ref-change", ExpectedVersion: &foreignVersion},
		Payload:  command.MembershipInput{ProjectRef: foreign.Project.Ref, MembershipRef: added.Membership.Ref, Permissions: []string{"VIEW"}, Active: true},
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("foreign project membership ref was not hidden: %v", err)
	}
	platformMemberships, _, err := service.ListPlatformMemberships(ctx, owner, query.Filter{Page: query.Page{Size: 20}})
	if err != nil || len(platformMemberships) != 2 {
		t.Fatalf("list organization memberships: memberships=%#v err=%v", platformMemberships, err)
	}
	var ownerMembership entity.Membership
	for _, membership := range platformMemberships {
		if membership.Role == "OWNER" {
			ownerMembership = membership
		}
	}
	if ownerMembership.Ref == "" {
		t.Fatal("installation owner membership missing")
	}
	administratorInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000004", ExternalTenantID: ownerInput.ExternalTenantID,
		ExternalDisplayName: "Jamie Rivera", ExternalEmailHint: "j***@example.test",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.organization-memberships.change",
	}
	if _, err := repository.ResolveProofAuthority(ctx, administratorInput); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("unknown administrator candidate received authority: %v", err)
	}
	administratorCandidates, _, err := service.ListPlatformMembershipCandidates(ctx, owner, query.Filter{Query: "Jamie", Page: query.Page{Size: 20}})
	if err != nil || len(administratorCandidates) != 1 {
		t.Fatalf("list administrator candidate: candidates=%#v err=%v", administratorCandidates, err)
	}
	administratorMembership, err := service.Execute(ctx, command.Command{
		Kind: command.AddPlatformMembership, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "organization-membership-administrator-add"},
		Payload:  command.PlatformMembershipInput{UserRef: administratorCandidates[0].Ref, Role: "ADMINISTRATOR", Active: true},
	})
	if err != nil || administratorMembership.Membership == nil {
		t.Fatalf("add administrator membership: membership=%#v err=%v", administratorMembership.Membership, err)
	}
	administrator := resolvedTestPrincipal(t, ctx, repository, administratorInput, "control-api-gateway")
	ownerVersionForAdministrator := ownerMembership.Version
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ChangePlatformMembership, Principal: administrator,
		Mutation: value.Mutation{IdempotencyKey: "administrator-owner-change", ExpectedVersion: &ownerVersionForAdministrator},
		Payload:  command.PlatformMembershipInput{MembershipRef: ownerMembership.Ref, Role: "MEMBER", Active: true},
	}); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("administrator changed owner membership: %v", err)
	}
	organizationVersionForAdministrator := organizationMember.Membership.Version
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ChangePlatformMembership, Principal: administrator,
		Mutation: value.Mutation{IdempotencyKey: "administrator-owner-grant", ExpectedVersion: &organizationVersionForAdministrator},
		Payload:  command.PlatformMembershipInput{MembershipRef: organizationMember.Membership.Ref, Role: "OWNER", Active: true},
	}); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("administrator granted owner role: %v", err)
	}
	selfVersion := added.Membership.Version
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeMembership, Principal: candidate,
		Mutation: value.Mutation{IdempotencyKey: "project-membership-self-change", ExpectedVersion: &selfVersion},
		Payload:  command.MembershipInput{ProjectRef: projectRef, MembershipRef: added.Membership.Ref, Permissions: []string{"VIEW"}, Active: true},
	}); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("project member changed own permissions: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.AddMembership, Principal: candidate,
		Mutation: value.Mutation{IdempotencyKey: "project-membership-overgrant"},
		Payload:  command.MembershipInput{ProjectRef: projectRef, UserRef: administratorMembership.Membership.User.Ref, Permissions: []string{"VIEW", "LAUNCH_RUNS"}, Active: true},
	}); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("project manager granted permission it does not hold: %v", err)
	}
	projectMembershipVersion := added.Membership.Version
	changedProjectMembership, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeMembership, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "project-membership-canonical-update", ExpectedVersion: &projectMembershipVersion},
		Payload: command.MembershipInput{ProjectRef: projectRef, MembershipRef: added.Membership.Ref,
			Permissions: []string{"VIEW"}, Active: true},
	})
	if err != nil || changedProjectMembership.Membership == nil ||
		changedProjectMembership.Membership.Version != projectMembershipVersion+1 {
		t.Fatalf("change canonical project membership: membership=%#v err=%v", changedProjectMembership.Membership, err)
	}
	added.Membership = changedProjectMembership.Membership
	if err := repository.pool.QueryRow(ctx, `
		SELECT role_version.permission_keys,
		       (SELECT count(*) FROM control_plane.application_role_versions version WHERE version.role_id = role.id)
		FROM control_plane.access_bindings binding
		JOIN control_plane.application_role_versions role_version ON role_version.id = binding.role_version_id
		JOIN control_plane.application_roles role ON role.id = role_version.role_id
		WHERE binding.ref = $1
	`, added.Membership.Ref).Scan(&canonicalPermissions, &roleVersionRows); err != nil ||
		roleVersionRows != 2 || !contains(canonicalPermissions, "project.view") || contains(canonicalPermissions, "access.manage") {
		t.Fatalf("membership update did not create an immutable canonical role version: permissions=%v versions=%d err=%v",
			canonicalPermissions, roleVersionRows, err)
	}
	ownerVersion := ownerMembership.Version
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ChangePlatformMembership, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "membership-last-owner-demotion", ExpectedVersion: &ownerVersion},
		Payload:  command.PlatformMembershipInput{MembershipRef: ownerMembership.Ref, Role: "MEMBER", Active: true},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("last owner demotion was not rejected: %v", err)
	}
	organizationVersion := organizationMember.Membership.Version
	suspended, err := service.Execute(ctx, command.Command{
		Kind: command.ChangePlatformMembership, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "organization-membership-suspend", ExpectedVersion: &organizationVersion},
		Payload:  command.PlatformMembershipInput{MembershipRef: organizationMember.Membership.Ref, Role: "OPERATOR", Active: false},
	})
	if err != nil || suspended.Membership == nil || suspended.Membership.Active {
		t.Fatalf("suspend organization membership: membership=%#v err=%v", suspended.Membership, err)
	}
	withoutProject := candidateInput
	withoutProject.ProjectRef = ""
	if _, err := repository.ResolveProofAuthority(ctx, withoutProject); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("suspended organization member retained authority: %v", err)
	}
	var activePresentationBindings int
	if err := repository.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM control_plane.access_bindings binding
		JOIN control_plane.subjects subject ON subject.id = binding.subject_id
		WHERE subject.ref = $1
		  AND binding.presentation_kind IN ('PLATFORM_MEMBERSHIP', 'PROJECT_MEMBERSHIP')
		  AND binding.state = 'ACTIVE'
	`, organizationMember.Membership.User.Ref).Scan(&activePresentationBindings); err != nil || activePresentationBindings != 0 {
		t.Fatalf("suspension left active canonical membership bindings: count=%d err=%v", activePresentationBindings, err)
	}
	projectMemberships, _, err := service.ListMemberships(ctx, owner, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil {
		t.Fatalf("list project memberships after organization suspension: %v", err)
	}
	for _, membership := range projectMemberships {
		if membership.Ref == added.Membership.Ref && membership.Active {
			t.Fatal("project membership remained active after organization suspension")
		}
	}
}

func testScheduleLifecycle(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.schedules.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct schedule service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "schedule-project-create"},
		Payload: command.ProjectInput{Name: "Accounting automation", Purpose: "Prepare recurring accounting summaries", Language: "en"},
	})
	if err != nil || project.Project == nil {
		t.Fatalf("create schedule project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "schedule-accountant", "Accounting assistant")
	created, err := service.Execute(ctx, command.Command{
		Kind: command.CreateSchedule, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "schedule-create"},
		Payload: command.ScheduleInput{ProjectRef: project.Project.Ref, Name: "Daily accounting summary", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Preset: "WEEKDAYS", TimeOfDay: "09:30", Timezone: "Europe/Saratov", Input: map[string]any{"task": "Prepare a bounded accounting summary."}, SessionPolicy: "NEW_EACH_RUN", NotificationPolicy: "CONTROL_CENTER_ONLY"},
	})
	if err != nil || created.Schedule == nil || created.Schedule.CronExpression != "30 9 * * 1-5" || created.Schedule.TimeOfDay != "09:30" || created.Schedule.NextRunAt == nil {
		t.Fatalf("create normalized schedule: schedule=%#v err=%v", created.Schedule, err)
	}
	if _, err := repository.pool.Exec(ctx, bootstrapComponentMakeScheduleDueQuery, created.Schedule.Ref); err != nil {
		t.Fatalf("make schedule due: %v", err)
	}
	schedulerClaim := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "automation-scheduler", Operation: "platform.runtime.schedules.claim",
	}, "automation-scheduler")
	claims, err := service.ClaimDueSchedules(ctx, schedulerClaim, "scheduler-component", 1)
	if err != nil || len(claims) != 1 || stringMap(claims[0], "inputDigest") == "" {
		t.Fatalf("claim due schedule: claims=%#v err=%v", claims, err)
	}
	staleClaim := claims[0]
	if _, err := repository.pool.Exec(ctx, bootstrapComponentExpireScheduleClaimQuery, stringMap(staleClaim, "occurrenceRef")); err != nil {
		t.Fatalf("expire schedule claim: %v", err)
	}
	claims, err = service.ClaimDueSchedules(ctx, schedulerClaim, "scheduler-recovery-component", 1)
	if err != nil || len(claims) != 1 || stringMap(claims[0], "occurrenceRef") != stringMap(staleClaim, "occurrenceRef") || claims[0]["generation"].(int64) != staleClaim["generation"].(int64)+1 || stringMap(claims[0], "leaseRef") == stringMap(staleClaim, "leaseRef") {
		t.Fatalf("recover expired schedule claim: stale=%#v recovered=%#v err=%v", staleClaim, claims, err)
	}
	if _, err := repository.pool.Exec(ctx, bootstrapComponentChangeScheduleAfterClaimQuery, created.Schedule.Ref); err != nil {
		t.Fatalf("change schedule after claim: %v", err)
	}
	schedulerMaterialize := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "automation-scheduler", Operation: "platform.runtime.schedules.materialize",
	}, "automation-scheduler")
	_, err = service.Execute(ctx, command.Command{
		Kind: command.MaterializeOccurrence, Principal: schedulerMaterialize,
		Mutation: value.Mutation{IdempotencyKey: "schedule-occurrence-stale-materialize"},
		Payload:  command.OccurrenceInput{OccurrenceRef: stringMap(staleClaim, "occurrenceRef"), LeaseRef: stringMap(staleClaim, "leaseRef"), Fence: stringMap(staleClaim, "fence"), Generation: staleClaim["generation"].(int64)},
	})
	if !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("stale schedule claim retained authority: %v", err)
	}
	materialized, err := service.Execute(ctx, command.Command{
		Kind: command.MaterializeOccurrence, Principal: schedulerMaterialize,
		Mutation: value.Mutation{IdempotencyKey: "schedule-occurrence-materialize"},
		Payload:  command.OccurrenceInput{OccurrenceRef: stringMap(claims[0], "occurrenceRef"), LeaseRef: stringMap(claims[0], "leaseRef"), Fence: stringMap(claims[0], "fence"), Generation: claims[0]["generation"].(int64)},
	})
	if err != nil || materialized.Run == nil || materialized.Schedule == nil || materialized.Run.Source != "SCHEDULE" || materialized.Schedule.Ref != created.Schedule.Ref || materialized.Run.Input["task"] != "Prepare a bounded accounting summary." {
		t.Fatalf("materialize schedule occurrence: result=%#v err=%v", materialized, err)
	}
	var occurrenceState, runSource string
	var leaseCleared bool
	if err := repository.pool.QueryRow(ctx, bootstrapComponentScheduleOccurrenceReadbackQuery, stringMap(claims[0], "occurrenceRef")).Scan(&occurrenceState, &leaseCleared, &runSource); err != nil || occurrenceState != "MATERIALIZED" || !leaseCleared || runSource != "SCHEDULE" {
		t.Fatalf("schedule occurrence readback: state=%q lease_cleared=%t source=%q err=%v", occurrenceState, leaseCleared, runSource, err)
	}
	duplicateClaims, err := service.ClaimDueSchedules(ctx, schedulerClaim, "scheduler-component", 1)
	if err != nil || len(duplicateClaims) != 0 {
		t.Fatalf("active schedule occurrence was claimed twice: claims=%#v err=%v", duplicateClaims, err)
	}
	runVersion := materialized.Run.Version
	cancelled, err := service.Execute(ctx, command.Command{
		Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-run-cancel", ExpectedVersion: &runVersion},
		Payload:  command.RunCommandInput{RunRef: materialized.Run.Ref, Reason: "Close schedule component fixture"},
	})
	if err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
		t.Fatalf("cancel scheduled run: run=%#v err=%v", cancelled.Run, err)
	}
	if err := repository.pool.QueryRow(ctx, bootstrapComponentScheduleOccurrenceReadbackQuery, stringMap(claims[0], "occurrenceRef")).Scan(&occurrenceState, &leaseCleared, &runSource); err != nil || occurrenceState != "CANCELLED" || !leaseCleared {
		t.Fatalf("cancel schedule occurrence with run: state=%q lease_cleared=%t err=%v", occurrenceState, leaseCleared, err)
	}

	targetSchedule, err := service.Execute(ctx, command.Command{
		Kind: command.CreateSchedule, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "schedule-create-target-disable"},
		Payload: command.ScheduleInput{ProjectRef: project.Project.Ref, Name: "Target lifecycle accounting summary", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Preset: "DAILY", TimeOfDay: "10:00", Timezone: "Europe/Saratov", Input: map[string]any{"task": "Prepare a bounded lifecycle summary."}, SessionPolicy: "NEW_EACH_RUN", NotificationPolicy: "CONTROL_CENTER_ONLY"},
	})
	if err != nil || targetSchedule.Schedule == nil {
		t.Fatalf("create target lifecycle schedule: schedule=%#v err=%v", targetSchedule.Schedule, err)
	}
	if _, err := repository.pool.Exec(ctx, bootstrapComponentMakeScheduleDueQuery, targetSchedule.Schedule.Ref); err != nil {
		t.Fatalf("make target lifecycle schedule due: %v", err)
	}
	targetClaims, err := service.ClaimDueSchedules(ctx, schedulerClaim, "scheduler-target-lifecycle-component", 1)
	if err != nil || len(targetClaims) != 1 {
		t.Fatalf("claim target lifecycle schedule: claims=%#v err=%v", targetClaims, err)
	}
	agentVersion := agent.Version
	disabledAgent, err := service.Execute(ctx, command.Command{
		Kind: command.SetAgentEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-disable-target-agent", ExpectedVersion: &agentVersion},
		Payload:  command.AgentInput{Ref: agent.Ref, Enabled: false},
	})
	if err != nil || disabledAgent.Agent == nil || disabledAgent.Agent.Enabled {
		t.Fatalf("disable scheduled target agent: agent=%#v err=%v", disabledAgent.Agent, err)
	}
	var scheduleEnabled bool
	if err := repository.pool.QueryRow(ctx, bootstrapComponentScheduleTargetStateReadbackQuery, targetSchedule.Schedule.Ref, stringMap(targetClaims[0], "occurrenceRef")).Scan(&scheduleEnabled, &occurrenceState, &leaseCleared); err != nil || scheduleEnabled || occurrenceState != "CANCELLED" || !leaseCleared {
		t.Fatalf("suspend schedule with disabled target: enabled=%t state=%q lease_cleared=%t err=%v", scheduleEnabled, occurrenceState, leaseCleared, err)
	}
	claimsAfterDisable, err := service.ClaimDueSchedules(ctx, schedulerClaim, "scheduler-target-lifecycle-component", 1)
	if err != nil || len(claimsAfterDisable) != 0 {
		t.Fatalf("disabled target schedule was reclaimed: claims=%#v err=%v", claimsAfterDisable, err)
	}
	disabledAgentVersion := disabledAgent.Agent.Version
	reenabledAgent, err := service.Execute(ctx, command.Command{
		Kind: command.SetAgentEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-reenable-target-agent", ExpectedVersion: &disabledAgentVersion},
		Payload:  command.AgentInput{Ref: agent.Ref, Enabled: true},
	})
	if err != nil || reenabledAgent.Agent == nil || !reenabledAgent.Agent.Enabled {
		t.Fatalf("reenable scheduled target agent: agent=%#v err=%v", reenabledAgent.Agent, err)
	}
	if err := repository.pool.QueryRow(ctx, bootstrapComponentScheduleTargetStateReadbackQuery, targetSchedule.Schedule.Ref, stringMap(targetClaims[0], "occurrenceRef")).Scan(&scheduleEnabled, &occurrenceState, &leaseCleared); err != nil || scheduleEnabled {
		t.Fatalf("target reenable implicitly enabled schedule: enabled=%t err=%v", scheduleEnabled, err)
	}

	archiveCandidate, err := service.Execute(ctx, command.Command{
		Kind: command.CreateSchedule, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "schedule-create-archive"},
		Payload: command.ScheduleInput{ProjectRef: project.Project.Ref, Name: "Archive accounting summary", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Preset: "DAILY", TimeOfDay: "11:00", Timezone: "Europe/Saratov", Input: map[string]any{"task": "Prepare an archive lifecycle summary."}, SessionPolicy: "NEW_EACH_RUN", NotificationPolicy: "CONTROL_CENTER_ONLY"},
	})
	if err != nil || archiveCandidate.Schedule == nil {
		t.Fatalf("create archive lifecycle schedule: schedule=%#v err=%v", archiveCandidate.Schedule, err)
	}
	if _, err := repository.pool.Exec(ctx, bootstrapComponentMakeScheduleDueQuery, archiveCandidate.Schedule.Ref); err != nil {
		t.Fatalf("make archive lifecycle schedule due: %v", err)
	}
	archiveClaims, err := service.ClaimDueSchedules(ctx, schedulerClaim, "scheduler-archive-lifecycle-component", 1)
	if err != nil || len(archiveClaims) != 1 {
		t.Fatalf("claim archive lifecycle schedule: claims=%#v err=%v", archiveClaims, err)
	}
	archiveVersion := archiveCandidate.Schedule.Version
	archiveCommand := command.Command{
		Kind: command.ArchiveSchedule, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-archive", ExpectedVersion: &archiveVersion},
		Payload:  command.ScheduleInput{Ref: archiveCandidate.Schedule.Ref},
	}
	archived, err := service.Execute(ctx, archiveCommand)
	if err != nil || archived.Schedule == nil || archived.Schedule.State != "ARCHIVED" || archived.Schedule.Enabled || archived.Schedule.NextRunAt != nil || !reflect.DeepEqual(archived.Schedule.NextActions, []string{"OPEN"}) {
		t.Fatalf("archive schedule: schedule=%#v err=%v", archived.Schedule, err)
	}
	replayedArchive, err := service.Execute(ctx, archiveCommand)
	if err != nil || replayedArchive.Schedule == nil || replayedArchive.Schedule.Version != archived.Schedule.Version {
		t.Fatalf("replay schedule archive: schedule=%#v err=%v", replayedArchive.Schedule, err)
	}
	archivedDetail, err := service.GetSchedule(ctx, owner, archiveCandidate.Schedule.Ref)
	if err != nil || archivedDetail.State != "ARCHIVED" || archivedDetail.Target.Ref != agent.Ref || archivedDetail.Input["task"] != "Prepare an archive lifecycle summary." {
		t.Fatalf("read archived schedule history: schedule=%#v err=%v", archivedDetail, err)
	}
	var lifecycleState string
	var nextRunCleared bool
	var archiveAuditCount, archiveEventCount int64
	if err := repository.pool.QueryRow(ctx, bootstrapComponentScheduleArchiveReadbackQuery, archiveCandidate.Schedule.Ref, stringMap(archiveClaims[0], "occurrenceRef")).Scan(&lifecycleState, &scheduleEnabled, &nextRunCleared, &occurrenceState, &leaseCleared, &archiveAuditCount, &archiveEventCount); err != nil || lifecycleState != "ARCHIVED" || scheduleEnabled || !nextRunCleared || occurrenceState != "CANCELLED" || !leaseCleared || archiveAuditCount != 1 || archiveEventCount != 1 {
		t.Fatalf("archive lifecycle readback: lifecycle=%q enabled=%t next_run_cleared=%t occurrence=%q lease_cleared=%t audits=%d events=%d err=%v", lifecycleState, scheduleEnabled, nextRunCleared, occurrenceState, leaseCleared, archiveAuditCount, archiveEventCount, err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.UpdateSchedule, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-update-archived", ExpectedVersion: &archived.Schedule.Version},
		Payload:  command.ScheduleInput{Ref: archiveCandidate.Schedule.Ref, Name: "Archived schedule mutation", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Preset: "DAILY", TimeOfDay: "12:00", Timezone: "UTC", Input: map[string]any{}, SessionPolicy: "NEW_EACH_RUN", NotificationPolicy: "CONTROL_CENTER_ONLY"},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("archived schedule accepted update: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.SetScheduleEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-enable-archived", ExpectedVersion: &archived.Schedule.Version},
		Payload:  command.ScheduleInput{Ref: archiveCandidate.Schedule.Ref, Enabled: true},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("archived schedule was enabled: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.MaterializeOccurrence, Principal: schedulerMaterialize,
		Mutation: value.Mutation{IdempotencyKey: "schedule-archived-occurrence-materialize"},
		Payload:  command.OccurrenceInput{OccurrenceRef: stringMap(archiveClaims[0], "occurrenceRef"), LeaseRef: stringMap(archiveClaims[0], "leaseRef"), Fence: stringMap(archiveClaims[0], "fence"), Generation: archiveClaims[0]["generation"].(int64)},
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("archived schedule lease retained materialization authority: %v", err)
	}
	claimsAfterArchive, err := service.ClaimDueSchedules(ctx, schedulerClaim, "scheduler-archive-lifecycle-component", 1)
	if err != nil || len(claimsAfterArchive) != 0 {
		t.Fatalf("archived schedule produced a future claim: claims=%#v err=%v", claimsAfterArchive, err)
	}
	currentForStaleArchive, err := service.GetSchedule(ctx, owner, created.Schedule.Ref)
	if err != nil {
		t.Fatalf("read schedule before stale archive scenario: %v", err)
	}
	staleVersion := currentForStaleArchive.Version
	paused, err := service.Execute(ctx, command.Command{
		Kind: command.SetScheduleEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-pause-before-stale-archive", ExpectedVersion: &staleVersion},
		Payload:  command.ScheduleInput{Ref: created.Schedule.Ref, Enabled: false},
	})
	if err != nil || paused.Schedule == nil || paused.Schedule.Version <= staleVersion || paused.Schedule.NextRunAt != nil {
		t.Fatalf("prepare stale schedule archive: schedule=%#v err=%v", paused.Schedule, err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.ArchiveSchedule, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-stale-archive", ExpectedVersion: &staleVersion},
		Payload:  command.ScheduleInput{Ref: created.Schedule.Ref},
	}); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("stale schedule archive was not rejected by OCC: %v", err)
	}
	pausedVersion := paused.Schedule.Version
	reenabledSchedule, err := service.Execute(ctx, command.Command{
		Kind: command.SetScheduleEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-enable-after-pause", ExpectedVersion: &pausedVersion},
		Payload:  command.ScheduleInput{Ref: created.Schedule.Ref, Enabled: true},
	})
	if err != nil || reenabledSchedule.Schedule == nil || !reenabledSchedule.Schedule.Enabled || reenabledSchedule.Schedule.NextRunAt == nil {
		t.Fatalf("reenable paused schedule: schedule=%#v err=%v", reenabledSchedule.Schedule, err)
	}
}

func testIdempotencyOCCAndConcurrentRuns(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct idempotency service: %v", err)
	}
	projectCommand := command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "idempotency-project-1"}, Payload: command.ProjectInput{
			Name: "Procurement", Purpose: "Coordinate supplier selection", Language: "en",
		}}
	first, err := service.Execute(ctx, projectCommand)
	if err != nil || first.Project == nil {
		t.Fatalf("create idempotent project: result=%#v err=%v", first.Project, err)
	}
	replayed, err := service.Execute(ctx, projectCommand)
	if err != nil || replayed.Project == nil || replayed.Project.Ref != first.Project.Ref {
		t.Fatalf("replay identical project intent: result=%#v err=%v", replayed.Project, err)
	}
	different := projectCommand
	different.Payload = command.ProjectInput{Name: "Different project", Purpose: "Different intent", Language: "en"}
	if _, err := service.Execute(ctx, different); !errors.Is(err, domainerrs.ErrIdempotencyReuse) {
		t.Fatalf("reuse idempotency key with different intent: %v", err)
	}
	projectVersion := first.Project.Version
	updated, err := service.Execute(ctx, command.Command{Kind: command.UpdateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "idempotency-project-update", ExpectedVersion: &projectVersion},
		Payload:  command.ProjectInput{Ref: first.Project.Ref, Name: "Supplier procurement", Purpose: "Select and onboard suppliers", Language: "en"},
	})
	if err != nil || updated.Project == nil || updated.Project.Version != projectVersion+1 {
		t.Fatalf("update project with current version: result=%#v err=%v", updated.Project, err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.UpdateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "idempotency-project-stale-update", ExpectedVersion: &projectVersion},
		Payload:  command.ProjectInput{Ref: first.Project.Ref, Name: "Stale update", Purpose: "Must not apply", Language: "en"},
	}); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("accept stale project version: %v", err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, first.Project.Ref, "concurrent-run-agent", "Procurement analyst")
	type runResult struct {
		result command.Result
		err    error
	}
	sharedCommand := command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "concurrent-same-intent-run"}, Payload: command.LaunchRunInput{
			ProjectRef: first.Project.Ref, Title: "Evaluate shared supplier", Task: "Evaluate the same bounded supplier profile.",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Input: map[string]any{"supplier": "shared"},
		}}
	sharedResults := make(chan runResult, 2)
	for range 2 {
		go func() {
			result, executeErr := service.Execute(ctx, sharedCommand)
			sharedResults <- runResult{result: result, err: executeErr}
		}()
	}
	sharedRuns := make([]entity.Run, 0, 2)
	for range 2 {
		outcome := <-sharedResults
		if outcome.err != nil || outcome.result.Run == nil {
			t.Fatalf("create same-intent concurrent run: result=%#v err=%v", outcome.result.Run, outcome.err)
		}
		sharedRuns = append(sharedRuns, *outcome.result.Run)
	}
	if sharedRuns[0].Ref != sharedRuns[1].Ref {
		t.Fatalf("same idempotency scope created different runs: %s %s", sharedRuns[0].Ref, sharedRuns[1].Ref)
	}
	projectReadback, err := service.GetProject(ctx, owner, first.Project.Ref)
	if err != nil || projectReadback.AgentCount != 1 || projectReadback.WorkflowCount != 0 || projectReadback.ActiveRunCount != 1 || projectReadback.PendingGateCount != 0 {
		t.Fatalf("project counters after run creation: project=%#v err=%v", projectReadback, err)
	}
	projects, _, actions, err := service.ListProjects(ctx, owner, query.Filter{Page: query.Page{Size: 100}})
	if err != nil || !contains(actions, "CREATE_PROJECT") {
		t.Fatalf("list project counters: actions=%v err=%v", actions, err)
	}
	var listed *entity.Project
	for index := range projects {
		if projects[index].Ref == first.Project.Ref {
			listed = &projects[index]
			break
		}
	}
	if listed == nil || listed.AgentCount != 1 || listed.ActiveRunCount != 1 {
		t.Fatalf("listed project counters: project=%#v", listed)
	}
	sharedVersion := sharedRuns[0].Version
	if cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "concurrent-same-intent-cancel", ExpectedVersion: &sharedVersion},
		Payload:  command.RunCommandInput{RunRef: sharedRuns[0].Ref, Reason: "Component test cleanup"},
	}); err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
		t.Fatalf("cancel same-intent concurrent run: run=%#v err=%v", cancelled.Run, err)
	}
	results := make(chan runResult, 2)
	for index := 1; index <= 2; index++ {
		index := index
		go func() {
			result, executeErr := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
				Mutation: value.Mutation{IdempotencyKey: "concurrent-run-" + leftPad(index, 2)}, Payload: command.LaunchRunInput{
					ProjectRef: first.Project.Ref, Title: "Evaluate supplier " + leftPad(index, 2), Task: "Evaluate the bounded supplier profile.",
					Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Input: map[string]any{"supplier": index},
				}})
			results <- runResult{result: result, err: executeErr}
		}()
	}
	createdRuns := make([]entity.Run, 0, 2)
	for range 2 {
		outcome := <-results
		if outcome.err != nil || outcome.result.Run == nil {
			t.Fatalf("create concurrent run: result=%#v err=%v", outcome.result.Run, outcome.err)
		}
		createdRuns = append(createdRuns, *outcome.result.Run)
	}
	if createdRuns[0].Ref == createdRuns[1].Ref {
		t.Fatalf("concurrent run creation returned duplicate ref %s", createdRuns[0].Ref)
	}
	for index := range createdRuns {
		version := createdRuns[index].Version
		cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "concurrent-run-cancel-" + leftPad(index+1, 2), ExpectedVersion: &version},
			Payload:  command.RunCommandInput{RunRef: createdRuns[index].Ref, Reason: "Component test cleanup"},
		})
		if err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
			t.Fatalf("cancel concurrent run %d: run=%#v err=%v", index+1, cancelled.Run, err)
		}
	}
}

func testHumanGateLifecycle(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.owner_gates.resolve",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct gate service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-project-1"}, Payload: command.ProjectInput{
			Name: "Legal review", Purpose: "Review business documents", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create gate project: result=%#v err=%v", project.Project, err)
	}
	reviewer := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "gate-reviewer", "Legal reviewer")
	draft := entity.WorkflowVersion{Ref: "draft", Name: "Contract review", Purpose: "Prepare a contract recommendation",
		CoordinatorAgentRef: reviewer.Ref, VersionNumber: 1, Concurrency: 1, TimeoutSeconds: 3600,
		CompletionCriteria: "A recommendation is approved by the owner", ResultSchema: map[string]any{},
		Inputs: []entity.WorkflowInputField{{Key: "contract", Label: "Contract", Type: "TEXT", Required: true}},
		Steps: []entity.WorkflowStep{{Key: "review", Position: 1, Name: "Review contract", AgentRef: reviewer.Ref,
			Instructions: "Review the contract and prepare a recommendation.", TimeoutSeconds: 900,
			ExpectedResult: "A bounded recommendation", HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REJECT", "REQUEST_CHANGES", "CANCEL"}}},
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-workflow-create"}, Payload: command.WorkflowInput{
			ProjectRef: project.Project.Ref, Name: draft.Name, Purpose: draft.Purpose, CoordinatorAgentRef: reviewer.Ref, Draft: &draft,
		}})
	if err != nil || created.Workflow == nil {
		t.Fatalf("create gate workflow: result=%#v err=%v", created.Workflow, err)
	}
	workflowVersion := created.Workflow.Version
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateWorkflow, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-workflow-validate", ExpectedVersion: &workflowVersion},
		Payload:  command.WorkflowInput{Ref: created.Workflow.Ref},
	})
	if err != nil || validated.Workflow == nil || validated.Workflow.State != "VALID" {
		t.Fatalf("validate gate workflow: result=%#v err=%v", validated.Workflow, err)
	}
	workflowVersion = validated.Workflow.Version
	published, err := service.Execute(ctx, command.Command{Kind: command.PublishWorkflow, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-workflow-publish", ExpectedVersion: &workflowVersion},
		Payload:  command.WorkflowInput{Ref: created.Workflow.Ref},
	})
	if err != nil || published.Workflow == nil || published.Workflow.State != "PUBLISHED" {
		t.Fatalf("publish gate workflow: result=%#v err=%v", published.Workflow, err)
	}
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-run-launch"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Review supplier contract", Task: "Review the attached supplier terms.",
			Target: entity.RunTarget{Type: "WORKFLOW", Ref: published.Workflow.Ref}, Input: map[string]any{"contract": "supplier-terms"},
		}})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch gate workflow: run=%#v err=%v", launched.Run, err)
	}
	waiting := claimAndCompleteRun(t, ctx, service, worker, "gate-review", false)
	if waiting.Run == nil || waiting.Run.State != "WAITING_HUMAN" || len(waiting.Run.GateRefs) != 1 {
		t.Fatalf("open owner gate: run=%#v event=%#v", waiting.Run, waiting.Event)
	}
	gateRef := waiting.Run.GateRefs[0]
	gateVersion := int64(1)
	resolved, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-resolve-approve", ExpectedVersion: &gateVersion},
		Payload:  command.GateResolutionInput{GateRef: gateRef, Decision: "APPROVE", Comment: "Approved for use"},
	})
	if err != nil || resolved.Gate == nil || resolved.Gate.State != "APPROVED" || resolved.Run == nil || resolved.Run.State != "SUCCEEDED" {
		t.Fatalf("resolve terminal owner gate: gate=%#v run=%#v err=%v", resolved.Gate, resolved.Run, err)
	}
	if resolved.Graph == nil || graphNodeState(resolved.Graph.Nodes, "ROOT_PROCESS") != "SUCCEEDED" {
		t.Fatalf("terminal gate did not close the root graph: %#v", resolved.Graph)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-resolve-replay", ExpectedVersion: &gateVersion},
		Payload:  command.GateResolutionInput{GateRef: gateRef, Decision: "APPROVE", Comment: "Replay"},
	})
	if !errors.Is(err, domainerrs.ErrAlreadyResolved) {
		t.Fatalf("replayed owner gate resolution error = %v, want already resolved", err)
	}
	changeRun, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-change-run-launch"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Review revised supplier contract", Task: "Review the revised supplier terms.",
			Target: entity.RunTarget{Type: "WORKFLOW", Ref: published.Workflow.Ref}, Input: map[string]any{"contract": "supplier-terms-revised"},
		}})
	if err != nil || changeRun.Run == nil {
		t.Fatalf("launch change-request workflow: run=%#v err=%v", changeRun.Run, err)
	}
	changeWaiting := claimAndCompleteRun(t, ctx, service, worker, "gate-change-review", false)
	if changeWaiting.Run == nil || changeWaiting.Run.State != "WAITING_HUMAN" || len(changeWaiting.Run.GateRefs) != 1 {
		t.Fatalf("open change-request gate: run=%#v", changeWaiting.Run)
	}
	changeGateVersion := int64(1)
	changes, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-resolve-changes", ExpectedVersion: &changeGateVersion},
		Payload: command.GateResolutionInput{GateRef: changeWaiting.Run.GateRefs[0], Decision: "REQUEST_CHANGES",
			Comment: "Add the termination risk and propose a mitigation."},
	})
	if err != nil || changes.Gate == nil || changes.Gate.State != "CHANGES_REQUESTED" || changes.Run == nil || changes.Run.State != "RUNNING" {
		t.Fatalf("request workflow changes: gate=%#v run=%#v err=%v", changes.Gate, changes.Run, err)
	}
	if changes.Graph == nil || graphNodeState(changes.Graph.Nodes, "AGENT_EXECUTION") != "QUEUED" {
		t.Fatalf("requested changes did not requeue the agent node: %#v", changes.Graph)
	}
	reworked := claimAndCompleteRun(t, ctx, service, worker, "gate-change-rework", false)
	if reworked.Run == nil || reworked.Run.State != "WAITING_HUMAN" || len(reworked.Run.GateRefs) != 2 {
		t.Fatalf("open gate after requested changes: run=%#v", reworked.Run)
	}
	secondGateVersion := int64(1)
	finalGateRef := reworked.Run.GateRefs[len(reworked.Run.GateRefs)-1]
	final, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "gate-resolve-rework", ExpectedVersion: &secondGateVersion},
		Payload:  command.GateResolutionInput{GateRef: finalGateRef, Decision: "APPROVE", Comment: "Rework approved"},
	})
	if err != nil || final.Run == nil || final.Run.State != "SUCCEEDED" {
		t.Fatalf("approve reworked workflow: run=%#v err=%v", final.Run, err)
	}
}

func testNestedDelegation(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	toolWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.tool-call.record",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct delegation service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-project-1"}, Payload: command.ProjectInput{
			Name: "Content operations", Purpose: "Prepare and review business content", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create delegation project: result=%#v err=%v", project.Project, err)
	}
	runtimes, err := service.ListRuntimes(ctx, owner)
	if err != nil || len(runtimes) != 1 || !runtimes[0].Ready || runtimes[0].Ref != defaultRuntimeKey {
		t.Fatalf("list enabled runtime catalog: runtimes=%#v err=%v", runtimes, err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CreateAgent, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-unknown-runtime"}, Payload: command.AgentInput{
			ProjectRef: project.Project.Ref, Name: "Invalid runtime agent", Purpose: "Must not be created",
			RoleDescription: "Invalid runtime", Instructions: "This instruction is long enough for validation.", RuntimeRef: "runtime_unknown",
		}}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("create agent accepted unknown runtime: %v", err)
	}
	coordinator := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "delegation-coordinator", "Content coordinator")
	firstChild := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "delegation-researcher", "Research specialist")
	secondChild := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "delegation-editor", "Content editor")
	coordinatorVersion := coordinator.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-unknown-capability", ExpectedVersion: &coordinatorVersion},
		Payload:  command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.unknown", Enabled: true},
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("grant accepted unknown capability: %v", err)
	}
	capability, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-capability-1", ExpectedVersion: &coordinatorVersion},
		Payload:  command.AgentBindingInput{AgentRef: coordinator.Ref, BindingRef: "platform.run.delegate", Enabled: true},
	})
	if err != nil || capability.Agent == nil || capability.Agent.Name == "" || !contains(capability.Agent.Capabilities, "platform.run.delegate") {
		t.Fatalf("grant delegation capability: result=%#v err=%v", capability.Agent, err)
	}
	workflowDraft := entity.WorkflowVersion{Ref: "draft", Name: "Campaign preparation", Purpose: "Coordinate research and editing",
		CoordinatorAgentRef: coordinator.Ref, VersionNumber: 1, Concurrency: 2, TimeoutSeconds: 3600,
		Instructions: "Delegate both bounded steps and synthesize their callbacks.", CompletionCriteria: "Both child results are synthesized.", ResultSchema: map[string]any{},
		Inputs: []entity.WorkflowInputField{{Key: "campaign", Label: "Campaign", Type: "TEXT", Required: true}},
		Steps: []entity.WorkflowStep{
			{Key: "research", Position: 1, Name: "Campaign research", AgentRef: firstChild.Ref, Instructions: "Research the bounded campaign context.", TimeoutSeconds: 900, ExpectedResult: "Research notes"},
			{Key: "editing", Position: 2, Name: "Campaign editing", AgentRef: secondChild.Ref, Instructions: "Prepare the bounded campaign copy.", TimeoutSeconds: 900, ExpectedResult: "Edited copy", HumanGateAfter: true, GateDecisions: []string{"APPROVE", "REJECT", "REQUEST_CHANGES"}},
		},
	}
	createdWorkflow, err := service.Execute(ctx, command.Command{Kind: command.CreateWorkflow, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-workflow-create"}, Payload: command.WorkflowInput{
			ProjectRef: project.Project.Ref, Name: workflowDraft.Name, Purpose: workflowDraft.Purpose,
			CoordinatorAgentRef: coordinator.Ref, Draft: &workflowDraft,
		}})
	if err != nil || createdWorkflow.Workflow == nil {
		t.Fatalf("create delegation workflow: result=%#v err=%v", createdWorkflow.Workflow, err)
	}
	workflowVersion := createdWorkflow.Workflow.Version
	validatedWorkflow, err := service.Execute(ctx, command.Command{Kind: command.ValidateWorkflow, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-workflow-validate", ExpectedVersion: &workflowVersion},
		Payload:  command.WorkflowInput{Ref: createdWorkflow.Workflow.Ref},
	})
	if err != nil || validatedWorkflow.Workflow == nil || validatedWorkflow.Workflow.State != "VALID" {
		t.Fatalf("validate delegation workflow: result=%#v err=%v", validatedWorkflow.Workflow, err)
	}
	workflowVersion = validatedWorkflow.Workflow.Version
	publishedWorkflow, err := service.Execute(ctx, command.Command{Kind: command.PublishWorkflow, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-workflow-publish", ExpectedVersion: &workflowVersion},
		Payload:  command.WorkflowInput{Ref: createdWorkflow.Workflow.Ref},
	})
	if err != nil || publishedWorkflow.Workflow == nil || publishedWorkflow.Workflow.State != "PUBLISHED" {
		t.Fatalf("publish delegation workflow: result=%#v err=%v", publishedWorkflow.Workflow, err)
	}
	workflowArtifact, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "delegation-artifact-upload"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "campaign-brief.md", MediaType: "text/markdown",
		SizeBytes: int64(len("# Campaign brief\n")), Reader: strings.NewReader("# Campaign brief\n"),
	})
	if err != nil {
		t.Fatalf("upload delegation artifact: %v", err)
	}
	workflowAttachmentSetRef := finalizedAttachmentSetRef(t, ctx, service, owner, project.Project.Ref,
		"WORKFLOW_INPUT", "delegation-attachment-set", workflowArtifact.Ref)
	if _, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-launch-without-files"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Prepare campaign with artifact", Task: "Coordinate the attached campaign brief.",
			Target: entity.RunTarget{Type: "WORKFLOW", Ref: publishedWorkflow.Workflow.Ref}, Input: map[string]any{"campaign": "Autumn"},
			AttachmentSetRef: workflowAttachmentSetRef,
		}}); !errors.Is(err, domainerrs.ErrCapabilityRequired) {
		t.Fatalf("launch workflow with artifact without Files capability: %v", err)
	}
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-launch-1"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Prepare campaign brief", Task: "Coordinate research and editing.",
			Target: entity.RunTarget{Type: "WORKFLOW", Ref: publishedWorkflow.Workflow.Ref}, Input: map[string]any{"campaign": "Autumn"},
		}})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch delegation coordinator: run=%#v err=%v", launched.Run, err)
	}
	coordinatorClaim, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "delegation-coordinator-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(coordinatorClaim.RuntimeItems) != 1 {
		t.Fatalf("claim delegation coordinator: claims=%d err=%v", len(coordinatorClaim.RuntimeItems), err)
	}
	coordinatorLease := coordinatorClaim.RuntimeItems[0]
	delegationCatalog, ok := coordinatorLease["delegationTargets"].([]map[string]string)
	if !ok || len(delegationCatalog) != 2 {
		t.Fatalf("workflow coordinator did not receive the pinned target catalog: %#v", coordinatorLease["delegationTargets"])
	}
	stepByAgent := map[string]string{}
	for _, target := range delegationCatalog {
		stepByAgent[target["ref"]] = target["workflowStepKey"]
	}
	delegations := []struct {
		key   string
		agent entity.Agent
	}{
		{key: "delegation-first", agent: firstChild},
		{key: "delegation-second", agent: secondChild},
	}
	for _, item := range delegations {
		delegated, err := service.Execute(ctx, command.Command{Kind: command.DelegateExecution, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: item.key}, Payload: command.DelegateInput{
				LeaseRef: stringMap(coordinatorLease, "leaseRef"), Fence: stringMap(coordinatorLease, "fence"),
				Generation: coordinatorLease["generation"].(int64), TargetAgentRef: item.agent.Ref, WorkflowStepKey: stepByAgent[item.agent.Ref],
				Task: "Complete the assigned part of the campaign brief.", Input: map[string]any{"part": item.key},
			}})
		if err != nil || delegated.Run == nil || stringMap(delegated.Runtime, "callbackEdgeRef") == "" {
			t.Fatalf("delegate %s child: run=%#v runtime=%v err=%v", item.key, delegated.Run, delegated.Runtime, err)
		}
		toolCall, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: toolWorker,
			Mutation: value.Mutation{IdempotencyKey: item.key + "-tool-call"}, Payload: command.RunToolCallInput{
				LeaseRef: stringMap(coordinatorLease, "leaseRef"), Fence: stringMap(coordinatorLease, "fence"),
				Generation: coordinatorLease["generation"].(int64), CallRef: "tcl_" + item.key,
				Tool: "delegate_agent", CapabilityRef: "platform.run.delegate", State: "SUCCEEDED",
				SafeResult: "delegate_agent:completed", SafeParameters: map[string]any{
					"target_agent_ref": item.agent.Ref, "workflow_step_key": stepByAgent[item.agent.Ref],
				},
			}})
		if err != nil || toolCall.Event == nil || toolCall.Event.ToolCall == nil ||
			toolCall.Event.Actor.Kind != "AGENT" || toolCall.Event.ToolCall.Tool != "delegate_agent" {
			t.Fatalf("record %s delegation tool call: event=%#v err=%v", item.key, toolCall.Event, err)
		}
	}
	claimedChildren, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "delegation-children-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 2}})
	if err != nil || len(claimedChildren.RuntimeItems) != 2 {
		t.Fatalf("claim delegated children: claims=%d err=%v", len(claimedChildren.RuntimeItems), err)
	}
	childSessions := map[string]struct{}{}
	for _, lease := range claimedChildren.RuntimeItems {
		childSession := stringMap(lease, "sessionRef")
		if childSession == "" || childSession == stringMap(coordinatorLease, "sessionRef") {
			t.Fatalf("child execution reused the parent FIFO session: child=%q parent=%q", childSession, stringMap(coordinatorLease, "sessionRef"))
		}
		childSessions[childSession] = struct{}{}
	}
	if len(childSessions) != 2 {
		t.Fatalf("parallel children did not receive distinct sessions: %#v", childSessions)
	}
	coordinatorCompleted := completeClaimedExecution(t, ctx, service, worker, coordinatorLease, "delegation-coordinator", false)
	if coordinatorCompleted.Run == nil || coordinatorCompleted.Run.State != "RUNNING" || coordinatorCompleted.Graph == nil {
		t.Fatalf("coordinator completion before callbacks changed the run incorrectly: run=%#v graph=%#v", coordinatorCompleted.Run, coordinatorCompleted.Graph)
	}
	for index, lease := range claimedChildren.RuntimeItems {
		child := completeClaimedExecution(t, ctx, service, worker, lease, "delegation-child-"+leftPad(index+1, 2), false)
		if child.Run == nil || child.Run.Usage != turnUsageFixture() {
			t.Fatalf("child completion %d usage = %#v", index+1, child.Run)
		}
	}
	waitingForOwner, err := service.GetRun(ctx, owner, launched.Run.Ref)
	if err != nil || waitingForOwner.State != "WAITING_HUMAN" || len(waitingForOwner.GateRefs) != 1 {
		t.Fatalf("human-gated delegated step did not open exactly one owner gate: run=%#v err=%v", waitingForOwner, err)
	}
	for index, lease := range claimedChildren.RuntimeItems {
		replayed := completeClaimedExecution(t, ctx, service, worker, lease, "delegation-child-"+leftPad(index+1, 2), false)
		if replayed.Run == nil || replayed.Graph == nil || replayed.Run.Usage != turnUsageFixture() {
			t.Fatalf("replay child completion %d lost authoritative result: %#v", index+1, replayed)
		}
	}
	gateVersion := int64(1)
	approved, err := service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "delegation-gate-approve", ExpectedVersion: &gateVersion},
		Payload:  command.GateResolutionInput{GateRef: waitingForOwner.GateRefs[0], Decision: "APPROVE", Comment: "Campaign proposal approved"},
	})
	if err != nil || approved.Run == nil || approved.Run.State != "RUNNING" {
		t.Fatalf("approve delegated workflow gate: run=%#v err=%v", approved.Run, err)
	}
	continuationClaim, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "delegation-continuation-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(continuationClaim.RuntimeItems) != 1 {
		t.Fatalf("claim coordinator continuation: claims=%#v err=%v", continuationClaim.RuntimeItems, err)
	}
	continuationLease := continuationClaim.RuntimeItems[0]
	if stringMap(continuationLease, "sessionRef") != stringMap(coordinatorLease, "sessionRef") {
		t.Fatalf("callback continuation left the parent session: continuation=%q parent=%q", stringMap(continuationLease, "sessionRef"), stringMap(coordinatorLease, "sessionRef"))
	}
	callbackContext, ok := continuationLease["sessionContext"].([]map[string]string)
	if !ok {
		t.Fatalf("continuation lost the authoritative session context: %#v", continuationLease["sessionContext"])
	}
	callbackTurns := 0
	for _, message := range callbackContext {
		if message["role"] != "USER" && message["role"] != "ASSISTANT" {
			t.Fatalf("continuation exposed a non-canonical session role: %#v", callbackContext)
		}
		if message["content"] == "Customer response prepared" {
			callbackTurns++
		}
	}
	if callbackTurns != 2 {
		t.Fatalf("expected two exactly-once callback turns, got %d in %#v", callbackTurns, callbackContext)
	}
	if targets, _ := continuationLease["delegationTargets"].([]map[string]string); len(targets) != 0 {
		t.Fatalf("completed workflow steps remained delegatable: %#v", targets)
	}
	completed := completeClaimedExecution(t, ctx, service, worker, continuationLease, "delegation-continuation", false)
	if completed.Run == nil || completed.Run.State != "SUCCEEDED" || len(completed.Run.GateRefs) != 1 || completed.Graph == nil || len(completed.Graph.Nodes) < 6 || graphNodeState(completed.Graph.Nodes, "ROOT_PROCESS") != "SUCCEEDED" {
		t.Fatalf("complete delegation root after callback continuation: run=%#v graph=%#v", completed.Run, completed.Graph)
	}
	wantUsage := entity.TokenUsage{
		TotalTokens: 480, InputTokens: 400, CachedInputTokens: 160,
		CacheWriteInputTokens: 40, OutputTokens: 80, ReasoningOutputTokens: 20,
		ModelContextWindow: 200000,
	}
	if completed.Run.Usage != wantUsage {
		t.Fatalf("root run token usage = %#v, want %#v", completed.Run.Usage, wantUsage)
	}
	callbackEdges := 0
	continuationEdges := 0
	for _, edge := range completed.Graph.Edges {
		if edge.Type == "CALLBACK_TO" {
			callbackEdges++
		}
		if edge.Type == "CONTINUES" {
			continuationEdges++
		}
	}
	if callbackEdges != 2 || continuationEdges != 1 {
		t.Fatalf("delegation graph lost callback edges: edges=%#v", completed.Graph.Edges)
	}
	events, _, _, err := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: completed.Run.Ref, Limit: 100})
	if err != nil {
		t.Fatalf("list delegation events: %v", err)
	}
	for _, event := range events {
		if event.Delta.Run == nil || event.RunState != event.Delta.Run.State {
			t.Fatalf("event %s run state %q differs from authoritative delta %#v", event.Ref, event.RunState, event.Delta.Run)
		}
		if event.Delta.Node != nil && event.NodeState != event.Delta.Node.State {
			t.Fatalf("event %s node state %q differs from authoritative delta %q", event.Ref, event.NodeState, event.Delta.Node.State)
		}
		if event.Delta.Node == nil && event.NodeState != "" {
			t.Fatalf("event %s exposes node state %q without node delta", event.Ref, event.NodeState)
		}
	}
}

func graphNodeState(nodes []entity.RunNode, nodeType string) string {
	for _, node := range nodes {
		if node.Type == nodeType {
			return node.State
		}
	}
	return ""
}

func createLifecycleAgent(t *testing.T, ctx context.Context, service *platformservice.Service, owner value.Principal, projectRef, key, name string) entity.Agent {
	t.Helper()
	result, err := service.Execute(ctx, command.Command{Kind: command.CreateAgent, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.AgentInput{
			ProjectRef: projectRef, Name: name, Purpose: "Complete a bounded business task", RoleDescription: name,
			Instructions: "Complete only the assigned task and return a concise, verifiable result.",
		}})
	if err != nil || result.Agent == nil {
		t.Fatalf("create %s: result=%#v err=%v", key, result.Agent, err)
	}
	return *result.Agent
}

func testDirectRunLifecycle(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	runtimeReader := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.artifact.read",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct lifecycle service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-project-1"}, Payload: command.ProjectInput{
			Name: "Customer support", Purpose: "Resolve customer requests", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create lifecycle project: result=%#v err=%v", project.Project, err)
	}
	agent, err := service.Execute(ctx, command.Command{Kind: command.CreateAgent, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-agent-1"}, Payload: command.AgentInput{
			ProjectRef: project.Project.Ref, Name: "Support specialist", Purpose: "Prepare customer responses",
			RoleDescription: "Customer support specialist", Instructions: "Analyze the request and prepare a clear, safe customer response.",
		}})
	if err != nil || agent.Agent == nil {
		t.Fatalf("create lifecycle agent: result=%#v err=%v", agent.Agent, err)
	}
	uploaded, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-upload-1"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "support-policy.md", MediaType: "application/octet-stream",
		SizeBytes: int64(len("# Support policy\n")), Reader: strings.NewReader("# Support policy\n"),
	})
	if err != nil || uploaded.ScanState != "CLEAN" || uploaded.MediaType != "text/markdown" || uploaded.Revision != 1 || uploaded.Source != "CONTROL_CENTER" {
		t.Fatalf("upload knowledge artifact: artifact=%#v err=%v", uploaded, err)
	}
	uploadedSetRef := finalizedAttachmentSetRef(t, ctx, service, owner, project.Project.Ref,
		"RUN_INPUT", "lifecycle-attachment-set-without-capability", uploaded.Ref)
	if _, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-launch-without-files"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Answer with attachment", Task: "Use the attached support policy.",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Agent.Ref}, AttachmentSetRef: uploadedSetRef,
		}}); !errors.Is(err, domainerrs.ErrCapabilityRequired) {
		t.Fatalf("launch agent with artifact without Files capability: %v", err)
	}
	preview, err := service.DownloadArtifact(ctx, owner, uploaded.Ref, "PREVIEW")
	if err != nil || preview.GrantRef == "" {
		t.Fatalf("open safe artifact preview: grant=%q err=%v", preview.GrantRef, err)
	}
	previewBody, previewReadErr := io.ReadAll(preview.Reader)
	previewCloseErr := preview.Reader.Close()
	if previewReadErr != nil || previewCloseErr != nil || string(previewBody) != "# Support policy\n" {
		t.Fatalf("read safe artifact preview: body=%q read_err=%v close_err=%v", string(previewBody), previewReadErr, previewCloseErr)
	}
	quarantined, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-quarantine-1"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "unsafe.exe", MediaType: "application/octet-stream",
		SizeBytes: 2, Reader: strings.NewReader("MZ"),
	})
	if err != nil || quarantined.ScanState != "QUARANTINED" {
		t.Fatalf("quarantine executable artifact: artifact=%#v err=%v", quarantined, err)
	}
	cleanArtifacts, cleanNext, err := service.ListArtifacts(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, Query: "support-policy", ArtifactType: "TEXT", ScanState: "CLEAN",
		SourceKind: "CONTROL_CENTER", Page: query.Page{Size: 1},
	})
	if err != nil || cleanNext != "" || len(cleanArtifacts) != 1 || cleanArtifacts[0].Ref != uploaded.Ref {
		t.Fatalf("server-side artifact filters were not applied before limit: artifacts=%#v next=%q err=%v", cleanArtifacts, cleanNext, err)
	}
	firstPage, nextPageToken, err := service.ListArtifacts(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, Page: query.Page{Size: 1},
	})
	if err != nil || len(firstPage) != 1 || firstPage[0].Ref != quarantined.Ref || nextPageToken == "" {
		t.Fatalf("first artifact cursor page is unstable: artifacts=%#v next=%q err=%v", firstPage, nextPageToken, err)
	}
	secondPage, finalPageToken, err := service.ListArtifacts(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, Page: query.Page{Size: 1, Token: nextPageToken},
	})
	if err != nil || len(secondPage) != 1 || secondPage[0].Ref != uploaded.Ref || finalPageToken != "" {
		t.Fatalf("second artifact cursor page is unstable: artifacts=%#v next=%q err=%v", secondPage, finalPageToken, err)
	}
	if _, _, err := service.ListArtifacts(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, ArtifactType: "EXECUTABLE", Page: query.Page{Size: 1},
	}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("unknown artifact type was accepted: %v", err)
	}
	if _, err := service.DownloadArtifact(ctx, owner, quarantined.Ref, "DOWNLOAD"); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("download quarantined artifact must be forbidden: %v", err)
	}
	uploadedVersion := uploaded.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.ChangeArtifactBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "artifact-binding-without-capability", ExpectedVersion: &uploadedVersion},
		Payload:  command.ArtifactBindingInput{ArtifactRef: uploaded.Ref, AgentRef: agent.Agent.Ref, Enabled: true},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("bind knowledge artifact without Files capability: %v", err)
	}
	agentVersion := agent.Agent.Version
	filesCapability, err := service.Execute(ctx, command.Command{Kind: command.ChangeAgentCapability, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-agent-files-capability", ExpectedVersion: &agentVersion},
		Payload:  command.AgentBindingInput{AgentRef: agent.Agent.Ref, BindingRef: runtimecontract.ArtifactCapability, Enabled: true},
	})
	if err != nil || filesCapability.Agent == nil || !contains(filesCapability.Agent.Capabilities, runtimecontract.ArtifactCapability) {
		t.Fatalf("grant Files capability: agent=%#v err=%v", filesCapability.Agent, err)
	}
	agent.Agent = filesCapability.Agent
	bound, err := service.Execute(ctx, command.Command{Kind: command.ChangeArtifactBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "artifact-binding-1", ExpectedVersion: &uploadedVersion},
		Payload:  command.ArtifactBindingInput{ArtifactRef: uploaded.Ref, AgentRef: agent.Agent.Ref, Enabled: true},
	})
	if err != nil || bound.Artifact == nil || bound.Artifact.FileName != uploaded.FileName || len(bound.Artifact.Bindings) != 1 || bound.Artifact.Bindings[0] != agent.Agent.Ref {
		t.Fatalf("bind knowledge artifact: artifact=%#v err=%v", bound.Artifact, err)
	}
	boundAgent, err := service.GetAgent(ctx, owner, agent.Agent.Ref)
	if err != nil || len(boundAgent.KnowledgeArtifactRefs) != 1 || boundAgent.KnowledgeArtifactRefs[0] != uploaded.Ref || boundAgent.Version != agent.Agent.Version+1 {
		t.Fatalf("read normalized knowledge binding: agent=%#v err=%v", boundAgent, err)
	}
	secondRevision, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-upload-2"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "support-policy.md", MediaType: "text/markdown",
		SizeBytes: int64(len("# Updated policy\n")), Reader: strings.NewReader("# Updated policy\n"),
	})
	if err != nil || secondRevision.Revision != 2 {
		t.Fatalf("create second artifact revision: artifact=%#v err=%v", secondRevision, err)
	}
	if _, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-content-conflict"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "same.txt", MediaType: "text/plain", SizeBytes: 5, Reader: strings.NewReader("alpha"),
	}); err != nil {
		t.Fatalf("create artifact idempotency baseline: %v", err)
	}
	if _, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "artifact-content-conflict"}, platformrepo.ArtifactUpload{
		ProjectRef: project.Project.Ref, FileName: "same.txt", MediaType: "text/plain", SizeBytes: 5, Reader: strings.NewReader("bravo"),
	}); !errors.Is(err, domainerrs.ErrIdempotencyReuse) {
		t.Fatalf("same artifact key with different content: %v", err)
	}
	runAttachmentSetRef := finalizedAttachmentSetRef(t, ctx, service, owner, project.Project.Ref,
		"RUN_INPUT", "lifecycle-run-attachment-set", secondRevision.Ref)
	launch, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-launch-1"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Answer customer", TitleSource: "USER_EDITED", Task: "Prepare an answer about delivery status.",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Agent.Ref}, Input: map[string]any{"ticket": "SUP-42"},
			AttachmentSetRef: runAttachmentSetRef,
		}})
	if err != nil || launch.Run == nil || launch.Graph == nil || launch.Run.State != "RUNNING" || launch.Run.TitleSource != "USER_EDITED" || len(launch.Graph.Nodes) != 2 {
		t.Fatalf("launch direct run: run=%#v graph=%#v err=%v", launch.Run, launch.Graph, err)
	}
	readRun, readGraph, err := service.GetRunGraph(ctx, owner, launch.Run.Ref)
	if err != nil || readRun.Ref != launch.Run.Ref || len(readGraph.Nodes) != 2 {
		t.Fatalf("read materialized run graph: run=%#v graph=%#v err=%v", readRun, readGraph, err)
	}
	for _, node := range readGraph.Nodes {
		if node.MaterializationState != "MATERIALIZED" {
			t.Fatalf("read run graph node without materialization state: %#v", node)
		}
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-concurrent-session"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, SessionRef: launch.Run.SessionRef, Title: "Concurrent answer",
			Task: "This turn must wait for the current one.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Agent.Ref},
		}}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("active Session accepted a concurrent turn: %v", err)
	}
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-first-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim lifecycle execution: claims=%d err=%v", len(claimed.RuntimeItems), err)
	}
	lease := claimed.RuntimeItems[0]
	catalog, ok := lease["artifacts"].([]map[string]any)
	if !ok || len(catalog) != 2 {
		t.Fatalf("runtime artifact catalog = %#v, want input and knowledge artifacts", lease["artifacts"])
	}
	runtimeDownload, err := service.ReadExecutionArtifact(ctx, runtimeReader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), secondRevision.Ref)
	if err != nil {
		t.Fatalf("read lease-bound runtime artifact: %v", err)
	}
	runtimeBody, runtimeReadErr := io.ReadAll(runtimeDownload.Reader)
	runtimeCloseErr := runtimeDownload.Reader.Close()
	if runtimeReadErr != nil || runtimeCloseErr != nil || string(runtimeBody) != "# Updated policy\n" || runtimeDownload.Artifact.Digest != secondRevision.Digest {
		t.Fatalf("runtime artifact body=%q artifact=%#v read_err=%v close_err=%v", string(runtimeBody), runtimeDownload.Artifact, runtimeReadErr, runtimeCloseErr)
	}
	if _, err := service.ReadExecutionArtifact(ctx, runtimeReader, stringMap(lease, "leaseRef"), "wrong-fence", lease["generation"].(int64), secondRevision.Ref); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("runtime artifact accepted a stale fence: %v", err)
	}
	completed := completeClaimedExecution(t, ctx, service, worker, lease, "lifecycle-first", true)
	if completed.Run == nil || completed.Run.State != "SUCCEEDED" || len(completed.CreatedRefs) != 1 || completed.Run.Usage != turnUsageFixture() {
		t.Fatalf("complete direct run: run=%#v artifacts=%v", completed.Run, completed.CreatedRefs)
	}
	if completed.Graph == nil || graphNodeState(completed.Graph.Nodes, "ROOT_PROCESS") != "SUCCEEDED" {
		t.Fatalf("completed run left the root graph active: %#v", completed.Graph)
	}
	download, err := service.DownloadArtifact(ctx, owner, completed.CreatedRefs[0], "DOWNLOAD")
	if err != nil {
		t.Fatalf("open generated artifact download: %v", err)
	}
	if download.GrantRef == "" {
		t.Fatal("download must materialize a one-time grant")
	}
	body, readErr := io.ReadAll(download.Reader)
	closeErr := download.Reader.Close()
	if readErr != nil || closeErr != nil || string(body) != "Customer response is ready.\n" {
		t.Fatalf("download generated artifact: body=%q read_err=%v close_err=%v", string(body), readErr, closeErr)
	}
	continued, err := service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-continuation-1"}, Payload: command.SessionTurnInput{
			SessionRef: launch.Run.SessionRef, RunRef: launch.Run.Ref, Task: "Add a concise follow-up for the customer.",
		}})
	if err != nil || continued.Run == nil || continued.Graph == nil || continued.Run.State != "RUNNING" {
		t.Fatalf("continue session: run=%#v graph=%#v err=%v", continued.Run, continued.Graph, err)
	}
	continuedVersion := continued.Run.Version
	cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-cancel-1", ExpectedVersion: &continuedVersion},
		Payload:  command.RunCommandInput{RunRef: continued.Run.Ref, Reason: "No longer needed"},
	})
	if err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
		t.Fatalf("cancel continued run: run=%#v err=%v", cancelled.Run, err)
	}
	cancelledVersion := cancelled.Run.Version
	retried, err := service.Execute(ctx, command.Command{Kind: command.RetryRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-retry-1", ExpectedVersion: &cancelledVersion},
		Payload:  command.RunCommandInput{RunRef: cancelled.Run.Ref, Reason: "Retry with the same bounded input"},
	})
	if err != nil || retried.Run == nil || retried.Graph == nil || retried.Run.Attempt != 2 || retried.Run.RetryOfRunRef != cancelled.Run.Ref {
		t.Fatalf("retry cancelled run: run=%#v graph=%#v err=%v", retried.Run, retried.Graph, err)
	}
	completedRetry := claimAndCompleteRun(t, ctx, service, worker, "lifecycle-retry", false)
	events, currentSequence, complete, err := service.ListRunEvents(ctx, owner, query.Filter{ResourceRef: completedRetry.Run.Ref, Limit: 100})
	if err != nil || !complete || len(events) == 0 || currentSequence != events[len(events)-1].Sequence {
		t.Fatalf("read retry event stream: events=%d sequence=%d complete=%v err=%v", len(events), currentSequence, complete, err)
	}
	for index, event := range events {
		if event.Sequence != int64(index+1) {
			t.Fatalf("non-monotonic event sequence at %d: %d", index, event.Sequence)
		}
		if event.Delta.Run == nil || event.Delta.Run.Ref != event.RunRef || event.Delta.Run.EventSequence != event.Sequence || event.Delta.Run.GraphRevision != event.GraphRevision {
			t.Fatalf("event %d does not carry an authoritative run delta: %#v", event.Sequence, event.Delta.Run)
		}
		if event.NodeRef != "" && (event.Delta.Node == nil || event.Delta.Node.Ref != event.NodeRef) {
			t.Fatalf("event %d lost its node delta: node_ref=%s delta=%#v", event.Sequence, event.NodeRef, event.Delta.Node)
		}
		if event.EdgeRef != "" && (event.Delta.Edge == nil || event.Delta.Edge.Ref != event.EdgeRef) {
			t.Fatalf("event %d lost its edge delta: edge_ref=%s delta=%#v", event.Sequence, event.EdgeRef, event.Delta.Edge)
		}
	}
}

func claimAndCompleteRun(t *testing.T, ctx context.Context, service *platformservice.Service, worker value.Principal, key string, artifact bool) command.Result {
	t.Helper()
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: key + "-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim %s execution: claims=%d err=%v", key, len(claimed.RuntimeItems), err)
	}
	return completeClaimedExecution(t, ctx, service, worker, claimed.RuntimeItems[0], key, artifact)
}

func completeClaimedExecution(t *testing.T, ctx context.Context, service *platformservice.Service, worker value.Principal, lease map[string]any, key string, artifact bool) command.Result {
	t.Helper()
	if _, err := service.Execute(ctx, command.Command{Kind: command.ReportExecutionProgress, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: key + "-progress"}, Payload: command.LeaseInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64), Progress: "Preparing the result",
		}}); err != nil {
		t.Fatalf("report %s progress: %v", key, err)
	}
	artifacts := []command.CompletedArtifact{}
	if artifact {
		content := []byte("Customer response is ready.\n")
		digest := sha256.Sum256(content)
		artifacts = append(artifacts, command.CompletedArtifact{FileName: "customer-response.txt", MediaType: "text/plain",
			SHA256: hex.EncodeToString(digest[:]), SizeBytes: int64(len(content)), Content: content})
	}
	completed, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: key + "-complete"}, Payload: command.CompleteExecutionInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			Success: true, ResultSummary: "Customer response prepared", Artifacts: artifacts,
			Usage: turnUsageFixture(),
		}})
	if err != nil {
		t.Fatalf("complete %s execution: %v", key, err)
	}
	return completed
}

func turnUsageFixture() entity.TokenUsage {
	return entity.TokenUsage{
		TotalTokens: 120, InputTokens: 100, CachedInputTokens: 40,
		CacheWriteInputTokens: 10, OutputTokens: 20, ReasoningOutputTokens: 5,
		ModelContextWindow: 200000,
	}
}

func testSystemAssistantTypedPlan(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.assistant.turns.add",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.assistant.plan.propose",
	}, "runtime-controller")
	runtimeReader := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.artifact.read",
	}, "runtime-controller")
	toolWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.tool-call.record",
	}, "runtime-controller")
	warmWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct platform service: %v", err)
	}
	firstHeartbeat, err := service.ReportWarmRuntime(ctx, warmWorker, command.WarmRuntimeInput{
		WorkloadInstance: "runtime-test", RuntimeRevision: systemassistant.CorePromptRevision, State: "READY",
	})
	if err != nil {
		t.Fatalf("report assistant readiness: %v", err)
	}
	var firstAuditCount, firstOutboxCount int
	if err := repository.pool.QueryRow(ctx, bootstrapComponentWarmHeartbeatCountsQuery).Scan(&firstAuditCount, &firstOutboxCount); err != nil {
		t.Fatalf("read first warm heartbeat effects: %v", err)
	}
	time.Sleep(2 * time.Millisecond)
	secondHeartbeat, err := service.ReportWarmRuntime(ctx, warmWorker, command.WarmRuntimeInput{
		WorkloadInstance: "runtime-test", RuntimeRevision: systemassistant.CorePromptRevision, State: "READY",
	})
	if err != nil {
		t.Fatalf("repeat assistant heartbeat: %v", err)
	}
	var secondAuditCount, secondOutboxCount int
	if err := repository.pool.QueryRow(ctx, bootstrapComponentWarmHeartbeatCountsQuery).Scan(&secondAuditCount, &secondOutboxCount); err != nil {
		t.Fatalf("read repeated warm heartbeat effects: %v", err)
	}
	if firstHeartbeat.LastHeartbeatAt == nil || secondHeartbeat.LastHeartbeatAt == nil ||
		!secondHeartbeat.LastHeartbeatAt.After(*firstHeartbeat.LastHeartbeatAt) ||
		secondHeartbeat.Version != firstHeartbeat.Version || firstAuditCount != secondAuditCount || firstOutboxCount != secondOutboxCount {
		t.Fatalf("repeated warm heartbeat was not effect-free: first=%#v second=%#v audit=%d/%d outbox=%d/%d", firstHeartbeat, secondHeartbeat, firstAuditCount, secondAuditCount, firstOutboxCount, secondOutboxCount)
	}
	if _, err := service.ReportWarmRuntime(ctx, worker, command.WarmRuntimeInput{
		WorkloadInstance: "runtime-test", RuntimeRevision: systemassistant.CorePromptRevision, State: "READY",
	}); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("non-heartbeat operation reported warm runtime: %v", err)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-conversation-1"}, Payload: command.AssistantConversationInput{}})
	if err != nil {
		t.Fatalf("create assistant conversation: %v", err)
	}
	assistantInputBody := "Approved organization policy\n"
	assistantInput, err := service.UploadArtifact(ctx, owner, value.Mutation{IdempotencyKey: "assistant-artifact-upload-1"}, platformrepo.ArtifactUpload{
		FileName: "organization-policy.txt", MediaType: "text/plain",
		SizeBytes: int64(len(assistantInputBody)), Reader: strings.NewReader(assistantInputBody),
	})
	if err != nil || assistantInput.ProjectRef != "" || assistantInput.ScanState != "CLEAN" {
		t.Fatalf("upload organization-scoped assistant artifact: artifact=%#v err=%v", assistantInput, err)
	}
	assistantAttachmentSetRef := finalizedAttachmentSetRef(t, ctx, service, owner, "",
		"ASSISTANT_MESSAGE", "assistant-attachment-set", assistantInput.Ref)
	organizationArtifacts, _, err := service.ListArtifacts(ctx, owner, query.Filter{
		Query: "organization-policy", ArtifactType: "TEXT", ScanState: "CLEAN", SourceKind: "CONTROL_CENTER", Page: query.Page{Size: 10},
	})
	if err != nil || len(organizationArtifacts) != 1 || organizationArtifacts[0].Ref != assistantInput.Ref || organizationArtifacts[0].ProjectRef != "" {
		t.Fatalf("list organization-scoped artifacts: artifacts=%#v err=%v", organizationArtifacts, err)
	}
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve owner readback: %v", err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatalf("resolve owner scope readback: %v", err)
	}
	var conversationID, sessionID, sessionRef, projectID, projectRef string
	var conversationVersion int64
	if err := repository.pool.QueryRow(ctx, queryConfigurationAddassistantturncommandSelectAssistantConversationsOrganizationIdRefState,
		ownerScope.organizationID, created.Conversation.Ref,
	).Scan(&conversationID, &sessionID, &sessionRef, &projectID, &projectRef, &conversationVersion); err != nil {
		t.Fatalf("read assistant conversation before turn: %v", err)
	}
	turn, err := service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-turn-1"}, Payload: command.AssistantTurnInput{
			ConversationRef: created.Conversation.Ref, Content: "Create a sales project", AttachmentSetRef: assistantAttachmentSetRef,
		}})
	if err != nil || turn.Plan != nil {
		t.Fatalf("queue assistant turn without keyword fallback: plan=%#v err=%v", turn.Plan, err)
	}
	if turn.Conversation == nil || turn.Conversation.TitleSource != "SERVER_DEFAULT" ||
		turn.Conversation.TitleRevision != 1 || turn.Conversation.Context.Route != "" ||
		len(turn.Conversation.Context.AllowedOperations) != 2 {
		t.Fatalf("assistant turn returned incomplete conversation: %#v", turn.Conversation)
	}
	queuedInputVersion := assistantInput.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.DeleteArtifact, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-artifact-delete-before-claim-1", ExpectedVersion: &queuedInputVersion},
		Payload:  command.ArtifactLifecycleInput{ArtifactRef: assistantInput.Ref},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("queued assistant input was soft-deleted before claim: %v", err)
	}
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-claim-1"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim assistant execution: claims=%d err=%v", len(claimed.RuntimeItems), err)
	}
	lease := claimed.RuntimeItems[0]
	if stringMap(lease, "projectRef") != projectRef {
		t.Fatalf("assistant runtime lost project binding: got=%q want=%q", stringMap(lease, "projectRef"), projectRef)
	}
	artifactCatalog, ok := lease["artifacts"].([]map[string]any)
	if !ok || len(artifactCatalog) != 1 || stringMap(artifactCatalog[0], "ref") != assistantInput.Ref {
		t.Fatalf("assistant runtime lost organization attachment snapshot: %#v", lease["artifacts"])
	}
	inputVersion := assistantInput.Version
	deletedInput, err := service.Execute(ctx, command.Command{Kind: command.DeleteArtifact, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-artifact-delete-1", ExpectedVersion: &inputVersion},
		Payload:  command.ArtifactLifecycleInput{ArtifactRef: assistantInput.Ref},
	})
	if err != nil || deletedInput.Artifact == nil || deletedInput.Artifact.LifecycleState != "DELETED" {
		t.Fatalf("soft-delete claimed assistant input: artifact=%#v err=%v", deletedInput.Artifact, err)
	}
	runtimeInput, err := service.ReadExecutionArtifact(ctx, runtimeReader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), assistantInput.Ref)
	if err != nil {
		t.Fatalf("read soft-deleted artifact from existing runtime snapshot: %v", err)
	}
	runtimeInputBody, readErr := io.ReadAll(runtimeInput.Reader)
	closeErr := runtimeInput.Reader.Close()
	if readErr != nil || closeErr != nil || string(runtimeInputBody) != assistantInputBody {
		t.Fatalf("read assistant snapshot body=%q read_err=%v close_err=%v", string(runtimeInputBody), readErr, closeErr)
	}
	deletedVersion := deletedInput.Artifact.Version
	restoredInput, err := service.Execute(ctx, command.Command{Kind: command.RestoreArtifact, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-artifact-restore-1", ExpectedVersion: &deletedVersion},
		Payload:  command.ArtifactLifecycleInput{ArtifactRef: assistantInput.Ref},
	})
	if err != nil || restoredInput.Artifact == nil || restoredInput.Artifact.LifecycleState != "ACTIVE" {
		t.Fatalf("restore assistant input: artifact=%#v err=%v", restoredInput.Artifact, err)
	}
	restoredVersion := restoredInput.Artifact.Version
	deletedAgain, err := service.Execute(ctx, command.Command{Kind: command.DeleteArtifact, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-artifact-delete-2", ExpectedVersion: &restoredVersion},
		Payload:  command.ArtifactLifecycleInput{ArtifactRef: assistantInput.Ref},
	})
	if err != nil || deletedAgain.Artifact == nil || deletedAgain.Artifact.LifecycleState != "DELETED" {
		t.Fatalf("soft-delete restored assistant input: artifact=%#v err=%v", deletedAgain.Artifact, err)
	}
	deletedAgainVersion := deletedAgain.Artifact.Version
	if _, err := service.PurgeArtifact(ctx, owner, value.Mutation{IdempotencyKey: "assistant-artifact-purge-1", ExpectedVersion: &deletedAgainVersion}, assistantInput.Ref); err != nil {
		t.Fatalf("purge assistant input: %v", err)
	}
	if _, err := service.ReadExecutionArtifact(ctx, runtimeReader, stringMap(lease, "leaseRef"), stringMap(lease, "fence"), lease["generation"].(int64), assistantInput.Ref); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("purged assistant input remained readable from runtime snapshot: %v", err)
	}
	planResult, err := service.Execute(ctx, command.Command{Kind: command.ProposeAssistantPlan, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-plan-1"}, Payload: command.ProposeAssistantPlanInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			Summary: "Create project Sales", Operations: []entity.AssistantPlanOperation{{Key: "operation-001", Type: "CREATE_PROJECT", Action: "CREATE",
				Title: "Sales", Summary: "Create sales project", Target: entity.AssistantPlanTarget{Kind: "PROJECT", Name: "Sales"},
				Parameters: map[string]any{"name": "Sales", "purpose": "Qualify and convert leads", "language": "en"},
				Before:     map[string]any{}, After: map[string]any{"name": "Sales", "purpose": "Qualify and convert leads", "language": "en"}, Selected: true}},
		}})
	if err != nil || planResult.Plan == nil || planResult.Plan.State != "DRAFT" {
		t.Fatalf("propose assistant plan: result=%#v err=%v", planResult.Plan, err)
	}
	toolCall, err := service.Execute(ctx, command.Command{Kind: command.RecordRunToolCall, Principal: toolWorker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-tool-call-1"}, Payload: command.RunToolCallInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			CallRef: "tcl_assistant_plan_001", Tool: "propose_configuration_plan",
			CapabilityRef: "platform.configuration.plan", State: "SUCCEEDED", SafeResult: "propose_configuration_plan:completed",
			SafeParameters: map[string]any{"operation_count": 1},
		}})
	if err != nil || toolCall.Event == nil || toolCall.Event.ToolCall == nil ||
		toolCall.Event.ToolCall.Tool != "propose_configuration_plan" || toolCall.Event.ToolCall.State != "SUCCEEDED" {
		t.Fatalf("record assistant tool call: event=%#v err=%v", toolCall.Event, err)
	}
	var outboxTool string
	if err := repository.pool.QueryRow(ctx, bootstrapComponentToolCallOutboxReadbackQuery, toolCall.Event.Ref).Scan(&outboxTool); err != nil || outboxTool != "propose_configuration_plan" {
		t.Fatalf("read assistant tool call outbox projection: tool=%q err=%v", outboxTool, err)
	}
	assistantOutputBody := []byte("Assistant result\n")
	assistantOutputDigest := sha256.Sum256(assistantOutputBody)
	completed, err := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-complete-1"}, Payload: command.CompleteExecutionInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			Success: true, ResultSummary: "The configuration plan is ready for review.", Artifacts: []command.CompletedArtifact{{
				FileName: "assistant-result.txt", MediaType: "text/plain", SHA256: hex.EncodeToString(assistantOutputDigest[:]),
				SizeBytes: int64(len(assistantOutputBody)), Content: assistantOutputBody,
			}},
		}})
	if err != nil || completed.Run == nil || completed.Run.State != "SUCCEEDED" || len(completed.CreatedRefs) != 1 {
		t.Fatalf("complete direct assistant execution: run=%#v err=%v", completed.Run, err)
	}
	assistantOutput, err := service.DownloadArtifact(ctx, owner, completed.CreatedRefs[0], "DOWNLOAD")
	if err != nil {
		t.Fatalf("download organization-scoped assistant result: %v", err)
	}
	downloadedOutput, readErr := io.ReadAll(assistantOutput.Reader)
	closeErr = assistantOutput.Reader.Close()
	if readErr != nil || closeErr != nil || !bytes.Equal(downloadedOutput, assistantOutputBody) {
		t.Fatalf("read assistant result body=%q read_err=%v close_err=%v", string(downloadedOutput), readErr, closeErr)
	}
	expectedPlanVersion := int64(1)
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateAssistantPlan, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-validate-1", ExpectedVersion: &expectedPlanVersion},
		Payload:  command.AssistantPlanInput{PlanRef: planResult.Plan.Ref, Revision: planResult.Plan.Revision}})
	if err != nil || validated.Plan == nil || validated.Plan.State != "VALID" {
		t.Fatalf("validate assistant plan: result=%#v err=%v", validated.Plan, err)
	}
	expectedPlanVersion = validated.Plan.Version
	applied, err := service.Execute(ctx, command.Command{Kind: command.ApplyAssistantPlan, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-apply-1", ExpectedVersion: &expectedPlanVersion},
		Payload:  command.AssistantPlanInput{PlanRef: planResult.Plan.Ref, Revision: planResult.Plan.Revision}})
	if err != nil || applied.Plan == nil || applied.Plan.State != "APPLIED" || applied.PlanReceipt == nil || len(applied.CreatedRefs) != 1 {
		t.Fatalf("apply assistant plan: result=%#v refs=%v err=%v", applied.Plan, applied.CreatedRefs, err)
	}
}

func resolvedTestPrincipal(t *testing.T, ctx context.Context, repository *Repository, input platformrepo.ProofPrincipalInput, workload string) value.Principal {
	t.Helper()
	if workload == "control-api-gateway" {
		input.OwnerClaim = true
	}
	authority, err := repository.ResolveProofAuthority(ctx, input)
	if err != nil {
		t.Fatalf("resolve test proof authority: %v", err)
	}
	return value.Principal{ActorID: authority.ActorID, AuthorityTenant: authority.OrganizationID,
		Permission: input.Operation, CorrelationRef: input.Operation + "-component", CallerWorkload: workload, CredentialRevision: 1}
}

func assertBootstrapReadback(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()
	var organizationCount, ownerContractCount, systemAssistantCount, corePromptCount int
	var assistantRuntimeCount, capabilityCount, integrationDefinitionCount int
	var providerDefinitionCount, providerAccountCount, providerCredentialRevisionCount, completedBootstrapCount int
	if err := pool.QueryRow(ctx, bootstrapComponentReadbackQuery).Scan(
		&organizationCount, &ownerContractCount, &systemAssistantCount, &corePromptCount,
		&assistantRuntimeCount, &capabilityCount, &integrationDefinitionCount,
		&providerDefinitionCount, &providerAccountCount, &providerCredentialRevisionCount,
		&completedBootstrapCount,
	); err != nil {
		t.Fatalf("read bootstrap state: %v", err)
	}
	if organizationCount != 1 || ownerContractCount != 1 || systemAssistantCount != 1 ||
		corePromptCount != 1 || assistantRuntimeCount != 1 || capabilityCount != 8 ||
		integrationDefinitionCount != 7 || providerDefinitionCount != 1 || providerAccountCount != 1 ||
		providerCredentialRevisionCount != 1 || completedBootstrapCount != 1 {
		t.Fatalf("unexpected bootstrap state: organization=%d owner_contract=%d assistant=%d core_prompt=%d runtime=%d capabilities=%d integrations=%d provider_definitions=%d provider_accounts=%d provider_credentials=%d completed=%d",
			organizationCount, ownerContractCount, systemAssistantCount, corePromptCount,
			assistantRuntimeCount, capabilityCount, integrationDefinitionCount, providerDefinitionCount,
			providerAccountCount, providerCredentialRevisionCount, completedBootstrapCount)
	}
}
