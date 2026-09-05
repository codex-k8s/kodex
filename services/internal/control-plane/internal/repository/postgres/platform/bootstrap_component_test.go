package platform

import (
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
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
	roleimagerepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/roleimage"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/systemassistant"
	"github.com/jackc/pgx/v5"
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
	//go:embed testdata/sql/bootstrap_component_insert_warm_failover_provider.sql
	bootstrapComponentInsertWarmFailoverProviderQuery string
	//go:embed testdata/sql/bootstrap_component_integration_invocation_effect_key.sql
	bootstrapComponentIntegrationInvocationEffectKeyQuery string
	//go:embed testdata/sql/bootstrap_component_runtime_provider_readback.sql
	bootstrapComponentRuntimeProviderReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_provider_account_readback.sql
	bootstrapComponentProviderAccountReadbackQuery string
	//go:embed testdata/sql/bootstrap_component_rotate_provider_credential.sql
	bootstrapComponentRotateProviderCredentialQuery string
	//go:embed testdata/sql/bootstrap_component_runtime_environment_reconcile_readback.sql
	bootstrapComponentRuntimeEnvironmentReconcileReadbackQuery string
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
	// Общая расширенная матрица выполняется последовательно; runtime deadlines не меняются.
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
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
	t.Run("catalog owner probe", func(t *testing.T) { testCatalogOwnerProbe(t, ctx, repository) })

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
	t.Run("model catalog is version bound", func(t *testing.T) { testModelCatalogVersion(t, ctx, repository) })
	t.Run("config overlay published history and rollback", func(t *testing.T) { testConfigOverlayHistory(t, ctx, repository) })
	t.Run("STT catalog requires organization management before configuration", func(t *testing.T) { testSTTCatalogAuthority(t, ctx, repository) })
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
	t.Run("memory records owner lifecycle", func(t *testing.T) {
		testMemoryRecords(t, ctx, repository)
	})
	t.Run("email receipt reconciliation is fresh exact and non retrying", func(t *testing.T) {
		testEmailReceiptReconciliation(t, ctx, repository, pool)
	})
	t.Run("email worker watermark rejects rollback", func(t *testing.T) {
		testEmailWorkerWatermark(t, ctx, repository)
	})
	t.Run("email configuration is immutable and revokes old readers", func(t *testing.T) {
		testEmailConfiguration(t, ctx, repository)
	})
	t.Run("email credentials are immutable owner bound and replayable", func(t *testing.T) {
		testEmailCredentials(t, ctx, repository)
	})
	t.Run("skill bundle draft owner lifecycle", func(t *testing.T) {
		testSkillBundleDraft(t, ctx, repository)
	})
	t.Run("provider credential legacy repair creates an immutable next revision", func(t *testing.T) {
		testProviderCredentialLegacyRepair(t, ctx, repository, pool)
		seedObservedCatalogFixture(t, ctx, repository)
	})
	t.Run("provider credential refresh is fenced idempotent and capacity bounded", func(t *testing.T) {
		testProviderCredentialRefreshAndCapacity(t, ctx, repository, pool)
	})
	t.Run("provider account actions follow exact application access", func(t *testing.T) {
		testProviderAccountApplicationAccess(t, ctx, repository)
	})
	t.Run("OIDC candidate receives project membership without internal identifiers", func(t *testing.T) {
		testProjectMembershipCandidate(t, ctx, repository)
	})
	t.Run("instruction draft save replaces the mutable draft", func(t *testing.T) {
		testInstructionDraftSave(t, ctx, repository)
	})
	t.Run("system assistant proposes and applies typed plan", func(t *testing.T) {
		prepareObservedWarmFixture(t, ctx, repository)
		testSystemAssistantTypedPlan(t, ctx, repository)
	})
	t.Run("assistant history search archive and actor cursor", func(t *testing.T) { testAssistantHistoryArchive(t, ctx, repository) })
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
	t.Run("schedule readback hydrates current revision and continuation session", func(t *testing.T) {
		testScheduleContractReadback(t, ctx, repository)
	})
	t.Run("durable schedule materializes immutable occurrence", func(t *testing.T) {
		testScheduleLifecycle(t, ctx, repository)
	})
	t.Run("schedule race retry prompt snapshot and expiry", func(t *testing.T) {
		testAutomationScheduler(t, ctx, repository)
	})
	t.Run("integration configuration and grants", func(t *testing.T) {
		testIntegrationConfigurationAndGrants(t, ctx, repository, pool)
	})
	t.Run("integration read and Human Gate decisions preserve effect cardinality", func(t *testing.T) {
		testIntegrationEffectLifecycle(t, ctx, repository, pool)
	})
	t.Run("interaction health checks are isolated from generic worker", func(t *testing.T) {
		testInteractionHealthRouting(t, ctx, repository, pool)
	})
	t.Run("enterprise access restricts exact agent and project", func(t *testing.T) {
		testEnterpriseAccessRestriction(t, ctx, repository)
	})
	t.Run("role image lifecycle uses canonical application access", func(t *testing.T) {
		testRoleImageApplicationAccess(t, ctx, repository)
	})
	t.Run("role image admission closes stale policy claims", func(t *testing.T) {
		testRoleImageAdmissionPolicyRotation(t, ctx, repository)
	})
	t.Run("role image promotion binds exact admitted artifact and receipt", func(t *testing.T) {
		testRoleImagePromotionLifecycle(t, ctx, repository)
	})
	t.Run("runtime environment lifecycle returns a complete terminal snapshot", func(t *testing.T) {
		testRuntimeEnvironmentLifecycle(t, ctx, repository, pool)
	})
	t.Run("runtime environment draft publication is validated and version pinned", func(t *testing.T) {
		testEnvironmentDraft(t, ctx, repository, pool)
	})
	t.Run("catalog SQL eligibility matches authoritative evaluator", func(t *testing.T) {
		testCatalogSQLParity(t, ctx, repository, pool)
	})
	t.Run("managed configuration lifecycle is immutable and selectively rebound", func(t *testing.T) {
		testManagedConfigurationLifecycle(t, ctx, repository, pool)
	})
	t.Run("managed draft save and discard preserve immutable history", func(t *testing.T) {
		testManagedDraftLifecycle(t, ctx, repository)
	})
	t.Run("runtime environment create rejects a missing exact image", func(t *testing.T) {
		testRuntimeEnvironmentRejectsMissingImage(t, ctx, repository)
	})
	t.Run("runtime environment privileged admission requires fresh authentication and permission", func(t *testing.T) {
		testRuntimeEnvironmentPrivilegedAdmission(t, ctx, repository)
	})
	t.Run("stale role runtime contract rejects launch before durable state", func(t *testing.T) {
		testStaleRoleRuntimeContractRejectsLaunch(t, ctx, repository, pool)
	})
	t.Run("runtime configuration publish validates canonical provider accounts", func(t *testing.T) {
		testRuntimeConfigurationPublish(t, ctx, repository)
	})
	t.Run("session provider affinity survives policy mutation and fails closed on revoke", func(t *testing.T) {
		testSessionProviderAffinityAfterPolicyMutation(t, ctx, repository, pool)
	})
	t.Run("runtime secret lifecycle is crash consistent", func(t *testing.T) {
		testRuntimeSecretCrashConsistency(t, ctx, repository)
	})
	t.Run("runtime secret drafts preserve staged lifecycle and cleanup fences", func(t *testing.T) {
		testRuntimeSecretDraftLifecycle(t, ctx, repository)
	})
	t.Run("provider auth rejection requires exact credential reauthorization", func(t *testing.T) {
		testProviderAuthRejectionLifecycle(t, ctx, repository, pool)
	})
	t.Run("system assistant runtime image creates an immutable environment revision", func(t *testing.T) {
		testSystemAssistantRuntimeEnvironmentReconciliation(t, ctx, repository, pool)
	})
	t.Run("system assistant warm runtime fails over through provider policy", func(t *testing.T) {
		testSystemAssistantWarmRuntimeProviderFailover(t, ctx, repository, pool)
	})
	t.Run("system assistant core prompt upgrades forward only", func(t *testing.T) {
		testSystemAssistantCorePromptUpgrade(t, ctx, repository, pool)
	})
	t.Run("provider credential cleanup is durable fenced and exact", func(t *testing.T) {
		testProviderCredentialCleanupLifecycle(t, ctx, repository, pool)
	})
	t.Run("interaction identity is owner bound scoped and revocable", func(t *testing.T) { testInteractionIdentity(t, ctx, repository, pool) })
	t.Run("integration connection tests bind exact workload before replay", func(t *testing.T) { testIntegrationTestAuthority(t, ctx, repository) })
	t.Run("secret revision impact selected rebind and retention", func(t *testing.T) { testSecretImpact(t, ctx, repository, pool) })
	t.Run("STT system roles advance immutably", func(t *testing.T) { testSTTRoleMigration(t, ctx, pool) })
}

func testManagedConfigurationLifecycle(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Managed configuration owner", CallerWorkload: "control-api-gateway",
		Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct managed configuration service: %v", err)
	}
	projectResult, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-configuration-project"},
		Payload:  command.ProjectInput{Name: "Managed configuration project", Language: "ru"}})
	if err != nil || projectResult.Project == nil {
		t.Fatalf("create managed configuration project: project=%#v err=%v", projectResult.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, projectResult.Project.Ref,
		"managed-configuration-agent", "Managed configuration agent")
	payload := command.ManagedConfigurationInput{ProjectRef: projectResult.Project.Ref,
		Name: "Customer support prompt", ContentFormat: "TEXT", Content: "Проект {{ .project.ref }}, задача {{ .task }}."}
	createCommand := command.Command{Kind: command.CreatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-create"}, Payload: payload}
	created, err := service.Execute(ctx, createCommand)
	if err != nil || created.ManagedConfiguration == nil || created.ManagedRevision == nil || created.ManagedRevision.State != "DRAFT" {
		t.Fatalf("create managed prompt draft: result=%#v err=%v", created, err)
	}
	replayed, err := service.Execute(ctx, createCommand)
	if err != nil || replayed.ManagedRevision == nil || replayed.ManagedRevision.Ref != created.ManagedRevision.Ref {
		t.Fatalf("replay managed prompt draft: result=%#v err=%v", replayed, err)
	}
	staleVersion := created.ManagedConfiguration.Version + 1
	if _, err := service.Execute(ctx, command.Command{Kind: command.ValidatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-stale", ExpectedVersion: &staleVersion},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref}}); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("stale managed prompt validation was not rejected: %v", err)
	}
	version := created.ManagedConfiguration.Version
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-validate", ExpectedVersion: &version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref}})
	if err != nil || validated.ManagedRevision == nil || validated.ManagedRevision.State != "VALID" {
		t.Fatalf("validate managed prompt: result=%#v err=%v", validated, err)
	}
	version = validated.ManagedConfiguration.Version
	published, err := service.Execute(ctx, command.Command{Kind: command.PublishPromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-publish", ExpectedVersion: &version},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref, RevisionRef: created.ManagedRevision.Ref}})
	if err != nil || published.ManagedRevision == nil || published.ManagedRevision.State != "PUBLISHED" {
		t.Fatalf("publish managed prompt: result=%#v err=%v", published, err)
	}
	impact, err := service.GetManagedConfigurationImpact(ctx, owner, created.ManagedConfiguration.Ref, created.ManagedRevision.Ref, query.Filter{})
	if err != nil || impact.Digest == "" || len(impact.Consumers) != 0 {
		t.Fatalf("preview managed prompt impact: impact=%#v err=%v", impact, err)
	}
	version = published.ManagedConfiguration.Version
	rebound, err := service.Execute(ctx, command.Command{Kind: command.RebindPromptTemplate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-rebind", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			RevisionRef: created.ManagedRevision.Ref, ImpactDigest: impact.Digest,
			Consumers: []entity.ManagedConfigurationConsumer{{Kind: "AGENT", Ref: agent.Ref}}}})
	if err != nil || rebound.ManagedConfiguration == nil || rebound.ManagedConfiguration.Version != version+1 {
		t.Fatalf("rebind managed prompt: result=%#v err=%v", rebound, err)
	}
	version = rebound.ManagedConfiguration.Version
	if _, err := service.Execute(ctx, command.Command{Kind: command.CreatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-create-stale"},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			Name: payload.Name, ContentFormat: "TEXT", Content: "Неизвестная {{ .missing.variable }}."}}); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("managed prompt draft without expected version was not rejected: %v", err)
	}
	invalidDraft, err := service.Execute(ctx, command.Command{Kind: command.CreatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-create-invalid", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			Name: payload.Name, ContentFormat: "TEXT", Content: "Неизвестная {{ .missing.variable }}."}})
	if err != nil || invalidDraft.ManagedRevision == nil || invalidDraft.ManagedRevision.State != "DRAFT" {
		t.Fatalf("create invalid managed prompt draft: result=%#v err=%v", invalidDraft, err)
	}
	version = invalidDraft.ManagedConfiguration.Version
	invalidDraft, err = service.Execute(ctx, command.Command{Kind: command.ValidatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-validate-invalid", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			RevisionRef: invalidDraft.ManagedRevision.Ref}})
	if err != nil || invalidDraft.ManagedRevision == nil || invalidDraft.ManagedRevision.State != "INVALID" {
		t.Fatalf("validate invalid managed prompt draft: result=%#v err=%v", invalidDraft, err)
	}
	version = invalidDraft.ManagedConfiguration.Version
	correctedDraft, err := service.Execute(ctx, command.Command{Kind: command.CreatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-create-corrected", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			Name: payload.Name, ContentFormat: "TEXT", Content: "Исправлено для {{ .project.ref }}."}})
	if err != nil || correctedDraft.ManagedRevision == nil || correctedDraft.ManagedRevision.State != "DRAFT" {
		t.Fatalf("create corrected managed prompt draft: result=%#v err=%v", correctedDraft, err)
	}
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve managed configuration owner: %v", err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatalf("resolve managed configuration owner scope: %v", err)
	}
	effective, err := repository.GetEffectivePromptTemplate(ctx, resolvedOwner, agent.Ref)
	if err != nil || effective.Ref != created.ManagedRevision.Ref || effective.Content != payload.Content {
		t.Fatalf("read effective managed prompt: prompt=%#v err=%v", effective, err)
	}
	correctedVersion := correctedDraft.ManagedConfiguration.Version
	correctedValidated, err := service.Execute(ctx, command.Command{Kind: command.ValidatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-validate-corrected", ExpectedVersion: &correctedVersion},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			RevisionRef: correctedDraft.ManagedRevision.Ref}})
	if err != nil || correctedValidated.ManagedRevision == nil || correctedValidated.ManagedRevision.State != "VALID" {
		t.Fatalf("validate corrected managed prompt: result=%#v err=%v", correctedValidated, err)
	}
	correctedVersion = correctedValidated.ManagedConfiguration.Version
	correctedPublished, err := service.Execute(ctx, command.Command{Kind: command.PublishPromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-publish-corrected", ExpectedVersion: &correctedVersion},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			RevisionRef: correctedDraft.ManagedRevision.Ref}})
	if err != nil || correctedPublished.ManagedRevision == nil || correctedPublished.ManagedRevision.State != "PUBLISHED" {
		t.Fatalf("publish corrected managed prompt: result=%#v err=%v", correctedPublished, err)
	}
	effective, err = repository.GetEffectivePromptTemplate(ctx, resolvedOwner, agent.Ref)
	if err != nil || effective.Ref != created.ManagedRevision.Ref || effective.Content != payload.Content {
		t.Fatalf("publish changed consumer before selective rebind: prompt=%#v err=%v", effective, err)
	}
	correctedImpact, err := service.GetManagedConfigurationImpact(ctx, owner, created.ManagedConfiguration.Ref, correctedDraft.ManagedRevision.Ref, query.Filter{})
	if err != nil || len(correctedImpact.Consumers) != 1 || correctedImpact.Consumers[0].RevisionRef != created.ManagedRevision.Ref {
		t.Fatalf("corrected managed prompt impact: impact=%#v err=%v", correctedImpact, err)
	}
	correctedVersion = correctedPublished.ManagedConfiguration.Version
	correctedRebound, err := service.Execute(ctx, command.Command{Kind: command.RebindPromptTemplate, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-prompt-rebind-corrected", ExpectedVersion: &correctedVersion},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			RevisionRef: correctedDraft.ManagedRevision.Ref, ImpactDigest: correctedImpact.Digest,
			Consumers: []entity.ManagedConfigurationConsumer{{Kind: "AGENT", Ref: agent.Ref}}}})
	if err != nil || correctedRebound.ManagedConfiguration == nil {
		t.Fatalf("selectively rebind corrected managed prompt: result=%#v err=%v", correctedRebound, err)
	}
	effective, err = repository.GetEffectivePromptTemplate(ctx, resolvedOwner, agent.Ref)
	if err != nil || effective.Ref != correctedDraft.ManagedRevision.Ref || effective.Content != "Исправлено для {{ .project.ref }}." {
		t.Fatalf("read selectively rebound managed prompt: prompt=%#v err=%v", effective, err)
	}
	search, total, nextPageToken, err := service.Search(ctx, owner, query.Filter{
		ProjectRef: projectResult.Project.Ref, Query: "Managed", Page: query.Page{Size: 1},
	})
	if err != nil || len(search) != 1 || total < 2 || nextPageToken == "" {
		t.Fatalf("search first page: items=%#v total=%d next=%q err=%v", search, total, nextPageToken, err)
	}
	for _, statement := range []string{
		`UPDATE control_plane.agents SET updated_at = clock_timestamp() WHERE ref = $1`,
		`UPDATE control_plane.workflows SET updated_at = clock_timestamp() WHERE ref = $1`,
		`UPDATE control_plane.projects SET updated_at = clock_timestamp() WHERE ref = $1`,
		`UPDATE control_plane.runs SET updated_at = clock_timestamp() WHERE ref = $1`,
	} {
		if _, err := pool.Exec(ctx, statement, search[0].Ref); err != nil {
			t.Fatalf("mutate search display timestamp between pages: %v", err)
		}
	}
	secondPage, secondTotal, _, err := service.Search(ctx, owner, query.Filter{
		ProjectRef: projectResult.Project.Ref, Query: "Managed", Page: query.Page{Size: 1, Token: nextPageToken},
	})
	if err != nil || len(secondPage) != 1 || secondTotal != total || secondPage[0].Ref == search[0].Ref {
		t.Fatalf("search second page: items=%#v total=%d err=%v", secondPage, secondTotal, err)
	}
	if _, _, _, err := service.Search(ctx, owner, query.Filter{
		ProjectRef: projectResult.Project.Ref, Query: "configuration", Page: query.Page{Size: 1, Token: nextPageToken},
	}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("search accepted a cursor from another filter: %v", err)
	}
	rootNodes, rootTotal, _, err := service.ListVFSNodes(ctx, owner, query.Filter{
		ProjectRef: projectResult.Project.Ref, ResourceRef: "/projects", Page: query.Page{Size: 20},
	})
	if err != nil || rootTotal != 1 || len(rootNodes) != 1 || rootNodes[0].EntityRef != projectResult.Project.Ref {
		t.Fatalf("list VFS project root: nodes=%#v total=%d err=%v", rootNodes, rootTotal, err)
	}
	agentNodes, agentTotal, _, err := service.ListVFSNodes(ctx, owner, query.Filter{
		ProjectRef:  projectResult.Project.Ref,
		ResourceRef: "/projects/" + projectResult.Project.Ref + "/entities/agents", Page: query.Page{Size: 20},
	})
	if err != nil || agentTotal != 1 || len(agentNodes) != 1 || agentNodes[0].EntityRef != agent.Ref {
		t.Fatalf("list VFS agents: nodes=%#v total=%d err=%v", agentNodes, agentTotal, err)
	}
	foundNodes, foundTotal, _, err := service.SearchVFS(ctx, owner, query.Filter{
		ProjectRef: projectResult.Project.Ref, Query: "Managed configuration agent", Page: query.Page{Size: 20},
	})
	if err != nil || foundTotal != 1 || len(foundNodes) != 1 || foundNodes[0].EntityRef != agent.Ref {
		t.Fatalf("search VFS agents: nodes=%#v total=%d err=%v", foundNodes, foundTotal, err)
	}
	models, modelTotal, modelNext, err := service.ListModelCapabilities(ctx, owner, "openai-codex", "", query.Filter{Page: query.Page{Size: 2}})
	if err != nil || len(models) != 2 || modelTotal != 3 || modelNext == "" || models[0].ID != "future-model" || models[0].DefaultReasoningEffort != "adaptive" {
		t.Fatalf("list model capabilities: models=%#v total=%d next=%q err=%v", models, modelTotal, modelNext, err)
	}
	secondModels, secondTotal, secondNext, err := service.ListModelCapabilities(ctx, owner, "openai-codex", "", query.Filter{Page: query.Page{Size: 2, Token: modelNext}})
	if err != nil || len(secondModels) != 1 || secondTotal != modelTotal || secondNext == modelNext {
		t.Fatalf("continue model capabilities: models=%#v total=%d next=%q err=%v", secondModels, secondTotal, secondNext, err)
	}
	if _, _, _, err := service.ListModelCapabilities(ctx, owner, "openai-codex", "", query.Filter{Query: "gpt", Page: query.Page{Size: 2, Token: modelNext}}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("model catalog accepted a cursor from another filter: %v", err)
	}
	configuration, history, historyTotal, next, err := service.ListManagedConfigurationHistory(ctx, owner, created.ManagedConfiguration.Ref, query.Page{Size: 2})
	if err != nil || configuration.Ref != created.ManagedConfiguration.Ref || len(history) != 2 || historyTotal != 3 || next == "" {
		t.Fatalf("read managed prompt history: configuration=%#v history=%#v total=%d next=%q err=%v", configuration, history, historyTotal, next, err)
	}
	for _, revision := range history {
		if revision.Content == "" {
			t.Fatalf("owner managed prompt history was unexpectedly redacted: %#v", revision)
		}
	}
	_, remainingHistory, remainingTotal, remainingNext, err := service.ListManagedConfigurationHistory(ctx, owner, created.ManagedConfiguration.Ref, query.Page{Size: 2, Token: next})
	if err != nil || len(remainingHistory) != 1 || remainingTotal != historyTotal || remainingNext != "" {
		t.Fatalf("continue managed prompt history: history=%#v total=%d next=%q err=%v", remainingHistory, remainingTotal, remainingNext, err)
	}
	testManagedPromptHistoryRedaction(t, ctx, repository, service, owner, projectResult.Project.Ref, created.ManagedConfiguration.Ref)
	testManagedGitOwnership(t, ctx, service, pool, owner, *correctedRebound.ManagedConfiguration, effective.Ref)
	var sttProviderAccountRef string
	if err := pool.QueryRow(ctx, `
		SELECT account.ref
		FROM control_plane.provider_accounts account
		WHERE account.organization_id = $1::uuid
		  AND account.state = 'AUTHORIZED' AND account.enabled
		  AND account.current_credential_revision_id IS NOT NULL
		ORDER BY account.ref LIMIT 1
	`, ownerScope.organizationID).Scan(&sttProviderAccountRef); err != nil {
		t.Fatalf("select eligible system STT provider account: %v", err)
	}
	sttContent := fmt.Sprintf(`{"name":"System STT","stt":{"enabled":true,"providerAccountRef":%q,"model":"gpt-transcribe","language":"ru","permissionKey":"platform.stt.use","parameters":{"keywords":["Kodex"],"prompt":"Names","temperature":0.2,"chunkingStrategy":"auto"}}}`, sttProviderAccountRef)
	sttCreated, err := service.Execute(ctx, command.Command{Kind: command.CreateSystemSTTDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-system-stt-create"},
		Payload:  command.ManagedConfigurationInput{Name: "System STT", ContentFormat: "JSON", Content: sttContent}})
	if err != nil || sttCreated.ManagedConfiguration == nil || sttCreated.ManagedRevision == nil {
		t.Fatalf("create system STT draft: result=%#v err=%v", sttCreated, err)
	}
	sttVersion := sttCreated.ManagedConfiguration.Version
	sttValidated, err := service.Execute(ctx, command.Command{Kind: command.ValidateSystemSTTDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-system-stt-validate", ExpectedVersion: &sttVersion},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: sttCreated.ManagedConfiguration.Ref, RevisionRef: sttCreated.ManagedRevision.Ref}})
	if err != nil || sttValidated.ManagedRevision == nil || sttValidated.ManagedRevision.State != "VALID" {
		t.Fatalf("validate system STT draft: result=%#v err=%v", sttValidated, err)
	}
	sttVersion = sttValidated.ManagedConfiguration.Version
	sttPublished, err := service.Execute(ctx, command.Command{Kind: command.PublishSystemSTTDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-system-stt-publish", ExpectedVersion: &sttVersion},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: sttCreated.ManagedConfiguration.Ref, RevisionRef: sttCreated.ManagedRevision.Ref}})
	if err != nil || sttPublished.ManagedRevision == nil || sttPublished.ManagedRevision.State != "PUBLISHED" {
		t.Fatalf("publish system STT draft: result=%#v err=%v", sttPublished, err)
	}
	sttImpact, err := service.GetManagedConfigurationImpact(ctx, owner, sttCreated.ManagedConfiguration.Ref, sttCreated.ManagedRevision.Ref, query.Filter{})
	if err != nil || sttImpact.Digest == "" {
		t.Fatalf("preview system STT impact: impact=%#v err=%v", sttImpact, err)
	}
	sttVersion = sttPublished.ManagedConfiguration.Version
	sttRebound, err := service.Execute(ctx, command.Command{Kind: command.RebindSystemSTT, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-system-stt-rebind", ExpectedVersion: &sttVersion},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: sttCreated.ManagedConfiguration.Ref,
			RevisionRef: sttCreated.ManagedRevision.Ref, ImpactDigest: sttImpact.Digest,
			Consumers: []entity.ManagedConfigurationConsumer{{Kind: "STT_SERVICE", Ref: "stt-tts-service"}}}})
	if err != nil || sttRebound.ManagedConfiguration == nil || sttRebound.ManagedConfiguration.Version != sttVersion+1 {
		t.Fatalf("rebind system STT consumer: result=%#v err=%v", sttRebound, err)
	}
	if _, err := pool.Exec(ctx, `
INSERT INTO control_plane.provider_authorization_attempts
    (ref, organization_id, provider_account_id, method, state, created_by)
SELECT 'pauth_component_stt_api_key', $1::uuid, account.id, 'API_KEY', 'AUTHORIZED', $2::uuid
FROM control_plane.provider_accounts account
WHERE account.organization_id = $1::uuid AND account.ref = $3
`, ownerScope.organizationID, owner.ActorID, sttProviderAccountRef); err != nil {
		t.Fatalf("materialize system STT API key authorization fixture: %v", err)
	}
	sttConfiguration, err := service.GetSystemSTTConfiguration(ctx, owner)
	if err != nil || !sttConfiguration.Ready || sttConfiguration.RevisionRef != sttCreated.ManagedRevision.Ref ||
		sttConfiguration.ProviderAccountRef != sttProviderAccountRef || sttConfiguration.ProviderCredentialGeneration == 0 {
		t.Fatalf("read system STT configuration: configuration=%#v err=%v", sttConfiguration, err)
	}
	if !sttConfiguration.Enabled || sttConfiguration.MaximumAudioBytes != 10<<20 || sttConfiguration.MaximumAudioDurationMilliseconds != 120000 ||
		len(sttConfiguration.Parameters.Keywords) != 1 || sttConfiguration.Parameters.Keywords[0] != "Kodex" || sttConfiguration.Parameters.Prompt != "Names" ||
		sttConfiguration.Parameters.Temperature != 0.2 || sttConfiguration.Parameters.ChunkingStrategy != "auto" {
		t.Fatal("immutable STT parameters or recommended limits were lost")
	}
	var sttProjectID string
	if err := pool.QueryRow(ctx, `SELECT id::text FROM control_plane.projects WHERE ref = $1`, projectResult.Project.Ref).Scan(&sttProjectID); err != nil {
		t.Fatalf("read system STT project identity: %v", err)
	}
	broker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "secret-broker", Operation: "platform.credential-projections.stt.resolve",
	}, "secret-broker")
	projectionInput := platformrepo.TranscriptionCredentialProjectionInput{
		Authority: platformrepo.CredentialProjectionAuthority{
			ActorID: owner.ActorID, TenantID: ownerScope.organizationID, ProjectID: sttProjectID,
			SourceRevision: 9, SourceDigestSHA256: strings.Repeat("a", 64), ProofJTI: "9671137c-0288-4446-803e-f3c2d13dcbe8",
			CallerWorkloadID: "stt-tts-service", CallerFullMethod: sttProjectionMethod,
			CallerCredentialRevision: 3, ExpiresAt: time.Now().UTC().Add(30 * time.Second),
		},
		ProviderAccountRef: sttProviderAccountRef, ProviderCredentialGeneration: sttConfiguration.ProviderCredentialGeneration,
		ConfigRevision: uint64(sttConfiguration.Revision), ConfigDigestSHA256: sttConfiguration.Digest,
	}
	credentialProjection, err := service.ResolveTranscriptionCredentialProjection(ctx, broker, projectionInput)
	if err != nil || credentialProjection.ProviderCredential.AccountRef != sttProviderAccountRef ||
		uint64(credentialProjection.ProviderCredential.CredentialRevision) != sttConfiguration.ProviderCredentialGeneration {
		t.Fatalf("resolve exact system STT credential: projection=%#v err=%v", credentialProjection, err)
	}
	organizationProjection := projectionInput
	organizationProjection.Authority.ProjectID = ""
	if _, err := service.ResolveTranscriptionCredentialProjection(ctx, broker, organizationProjection); err != nil {
		t.Fatalf("resolve organization scoped STT credential: %v", err)
	}
	changedConfig := projectionInput
	changedConfig.ConfigDigestSHA256 = strings.Repeat("f", 64)
	if _, err := service.ResolveTranscriptionCredentialProjection(ctx, broker, changedConfig); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("changed system STT config was accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.provider_accounts SET enabled = false, state = 'REVOKED', current_credential_revision_id = NULL WHERE ref = $1`, sttProviderAccountRef); err != nil {
		t.Fatalf("revoke system STT account fixture: %v", err)
	}
	if _, err := service.ResolveTranscriptionCredentialProjection(ctx, broker, projectionInput); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("revoked system STT account was accepted: %v", err)
	}
	if _, err := pool.Exec(ctx, `
UPDATE control_plane.provider_accounts account
SET enabled = true,
    state = 'AUTHORIZED',
    current_credential_revision_id = credential.id
FROM control_plane.provider_credential_revisions credential
WHERE account.ref = $1
  AND credential.ref = $2
  AND credential.provider_account_id = account.id`, sttProviderAccountRef, credentialProjection.ProviderCredential.CredentialRevisionRef); err != nil {
		t.Fatalf("restore system STT account fixture: %v", err)
	}
	var environmentRef, environmentProjectRef string
	if err := pool.QueryRow(ctx, `
SELECT environment.ref, project.ref
FROM control_plane.runtime_environment_sets environment
JOIN control_plane.projects project ON project.id = environment.project_id
WHERE environment.organization_id = $1::uuid
  AND environment.name = 'Runtime lifecycle second'
  AND environment.state = 'ACTIVE'
LIMIT 1`, ownerScope.organizationID).Scan(&environmentRef, &environmentProjectRef); err != nil {
		t.Fatalf("read runtime environment consumer fixture: %v", err)
	}
	roleImage := publishAndRebindManagedConfiguration(t, ctx, service, owner,
		"managed-role-image", command.CreateRoleImageRevisionDraft, command.ValidateRoleImageRevision,
		command.PublishRoleImageRevision, command.RebindRoleImage,
		command.ManagedConfigurationInput{ProjectRef: environmentProjectRef, Name: "Runtime role image",
			ContentFormat: "JSON", Content: `{"name":"Runtime role image","baseImage":"registry.invalid/base@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","packages":["ca-certificates"]}`},
		entity.ManagedConfigurationConsumer{Kind: "RUNTIME_ENVIRONMENT", Ref: environmentRef})
	runtimeReader := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.role-image-configuration.get",
	}, "runtime-controller")
	roleImageBinding, err := service.GetEffectiveManagedConfiguration(ctx, runtimeReader, "ROLE_IMAGE", "RUNTIME_ENVIRONMENT", environmentRef)
	if err != nil || roleImageBinding.Revision.Ref != roleImage.ManagedRevision.Ref ||
		roleImageBinding.Configuration.Ref != roleImage.ManagedConfiguration.Ref || roleImageBinding.ConsumerRef != environmentRef {
		t.Fatalf("read pinned role image configuration: binding=%#v err=%v", roleImageBinding, err)
	}
	foreignRuntimeReader := runtimeReader
	foreignRuntimeReader.AuthorityTenant = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	if foreign, foreignErr := service.GetEffectiveManagedConfiguration(ctx, foreignRuntimeReader, "ROLE_IMAGE", "RUNTIME_ENVIRONMENT", environmentRef); foreignErr == nil || foreign.Ref != "" {
		t.Fatalf("foreign tenant read role image binding: binding=%#v err=%v", foreign, foreignErr)
	}

	connection, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-definition-connection-create"},
		Payload: command.ConnectionInput{DefinitionKey: "synthetic", Name: "Managed definition consumer",
			PublicConfiguration: map[string]any{"journal": "managed-definition"}}})
	if err != nil || connection.Connection == nil {
		t.Fatalf("create managed definition connection consumer: connection=%#v err=%v", connection.Connection, err)
	}
	integrationDefinition := publishAndRebindManagedConfiguration(t, ctx, service, owner,
		"managed-integration-definition", command.CreateIntegrationDefinition, command.ValidateIntegrationDefinition,
		command.PublishIntegrationDefinition, command.RebindIntegrationDefinition,
		command.ManagedConfigurationInput{Name: "Synthetic managed definition",
			ContentFormat: "JSON", Content: string(asJSON(repository.integrationDefinitions["synthetic"]))},
		entity.ManagedConfigurationConsumer{Kind: "INTEGRATION_CONNECTION", Ref: connection.Connection.Ref})
	testIntegrationDefinitionRebindAuthority(t, ctx, repository, service, owner, integrationDefinition, *connection.Connection)
	integrationReader := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "integration-gateway", Operation: "platform.runtime.integration-definition.get",
	}, "integration-gateway")
	integrationBinding, err := service.GetEffectiveManagedConfiguration(ctx, integrationReader, "INTEGRATION_DEFINITION", "INTEGRATION_CONNECTION", connection.Connection.Ref)
	if err != nil || integrationBinding.Revision.Ref != integrationDefinition.ManagedRevision.Ref ||
		integrationBinding.Configuration.Ref != integrationDefinition.ManagedConfiguration.Ref || integrationBinding.ConsumerRef != connection.Connection.Ref {
		t.Fatalf("read pinned integration definition: binding=%#v err=%v", integrationBinding, err)
	}
	if _, err := service.GetEffectiveManagedConfiguration(ctx, integrationReader, "PROMPT_TEMPLATE", "INTEGRATION_CONNECTION", connection.Connection.Ref); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("generic managed configuration kind escalation was accepted: %v", err)
	}
	testManagedIntegrationPackageExecution(t, ctx, repository, service, owner, connection.Connection.Ref, integrationDefinition)
	testConfigurationSourceLifecycle(t, ctx, repository, service, owner)
	gitContent := "Git-owned prompt for {{ .project.ref }}."
	gitDigest := sha256.Sum256([]byte(gitContent))
	var gitConfigurationID, gitRevisionID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO control_plane.managed_configuration_sets
		    (ref, organization_id, project_id, kind, name, managed_by, source, source_revision, created_by)
		SELECT 'mcfg_gitprompt01', $1::uuid, project.id, 'PROMPT_TEMPLATE', 'Git prompt', 'GIT',
		       'git://configuration/prompts/git-prompt', '0123456789abcdef', $2::uuid
		FROM control_plane.projects project
		WHERE project.organization_id = $1::uuid AND project.ref = $3
		RETURNING id::text
	`, ownerScope.organizationID, ownerScope.actorID, projectResult.Project.Ref).Scan(&gitConfigurationID); err != nil {
		t.Fatalf("create Git-owned configuration fixture: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO control_plane.managed_configuration_revisions
		    (ref, organization_id, configuration_set_id, revision, state, content_format, content, digest, created_by, validated_at, published_at)
		VALUES ('mrev_gitprompt01', $1::uuid, $2::uuid, 1, 'PUBLISHED', 'TEXT', $3, $4, $5::uuid, clock_timestamp(), clock_timestamp())
		RETURNING id::text
	`, ownerScope.organizationID, gitConfigurationID, gitContent, hex.EncodeToString(gitDigest[:]), ownerScope.actorID).Scan(&gitRevisionID); err != nil {
		t.Fatalf("create Git-owned revision fixture: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.managed_configuration_sets SET current_revision_id = $1::uuid WHERE id = $2::uuid`, gitRevisionID, gitConfigurationID); err != nil {
		t.Fatalf("bind Git-owned revision fixture: %v", err)
	}
	gitVersion := int64(1)
	copied, err := service.Execute(ctx, command.Command{Kind: command.CopyGitManagedConfiguration, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-git-copy", ExpectedVersion: &gitVersion},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: "mcfg_gitprompt01", Name: "Copied Git prompt"}})
	if err != nil || copied.ManagedConfiguration == nil || copied.ManagedConfiguration.ManagedBy != "UI" || copied.ManagedRevision == nil ||
		copied.ManagedRevision.State != "DRAFT" || copied.ManagedRevision.ParentRevisionRef != "mrev_gitprompt01" {
		t.Fatalf("copy Git-owned configuration: result=%#v err=%v", copied, err)
	}
	detached, err := service.Execute(ctx, command.Command{Kind: command.DetachGitManagedConfiguration, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-git-detach", ExpectedVersion: &gitVersion},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: "mcfg_gitprompt01"}})
	if err != nil || detached.ManagedConfiguration == nil || detached.ManagedConfiguration.ManagedBy != "UI" || detached.ManagedConfiguration.Version != 2 {
		t.Fatalf("detach Git-owned configuration: result=%#v err=%v", detached, err)
	}
	detachedVersion := detached.ManagedConfiguration.Version
	detachedValidated, err := service.Execute(ctx, command.Command{Kind: command.ValidatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-detached-validate", ExpectedVersion: &detachedVersion},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: "mcfg_gitprompt01", RevisionRef: detached.ManagedRevision.Ref}})
	if err != nil || detachedValidated.ManagedRevision == nil || detachedValidated.ManagedRevision.State != "VALID" {
		t.Fatalf("validate detached configuration: %v", err)
	}
	detachedVersion = detachedValidated.ManagedConfiguration.Version
	detachedPublished, err := service.Execute(ctx, command.Command{Kind: command.PublishPromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-detached-publish", ExpectedVersion: &detachedVersion},
		Payload:  command.ManagedConfigurationInput{ConfigurationRef: "mcfg_gitprompt01", RevisionRef: detached.ManagedRevision.Ref}})
	if err != nil || detachedPublished.ManagedRevision == nil || detachedPublished.ManagedRevision.State != "PUBLISHED" {
		t.Fatalf("publish detached configuration: %v", err)
	}
	detachedVersion = detachedPublished.ManagedConfiguration.Version
	detachedDraft, err := service.Execute(ctx, command.Command{Kind: command.CreatePromptTemplateDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-detached-draft", ExpectedVersion: &detachedVersion},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: "mcfg_gitprompt01", Name: "Git prompt",
			ContentFormat: "TEXT", Content: "Detached prompt for {{ .project.ref }}."}})
	if err != nil || detachedDraft.ManagedRevision == nil || detachedDraft.ManagedRevision.State != "DRAFT" || detachedDraft.ManagedRevision.ParentRevisionRef != detached.ManagedRevision.Ref {
		t.Fatalf("edit detached configuration: result=%#v err=%v", detachedDraft, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.managed_configuration_revisions SET content = 'mutated' WHERE ref = $1`, created.ManagedRevision.Ref); err == nil {
		t.Fatal("published managed prompt was mutable")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM control_plane.managed_configuration_revisions WHERE ref = $1`, created.ManagedRevision.Ref); err == nil {
		t.Fatal("published managed prompt was deletable")
	}
	testManagedImpactPagination(t, ctx, service, owner, projectResult.Project.Ref, correctedRebound)
}

func testManagedPromptHistoryRedaction(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
	service *platformservice.Service,
	owner value.Principal,
	projectRef, configurationRef string,
) {
	t.Helper()
	input := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000009994", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Prompt history reader", CallerWorkload: "control-api-gateway",
		Operation: "platform.query.managed-configurations.history.list", ProjectRef: projectRef,
	}
	if _, err := repository.ResolveProofAuthority(ctx, input); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("unbound prompt history reader received authority: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{
		Query: input.ExternalDisplayName, Page: query.Page{Size: 20},
	}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("resolve prompt history reader subject: subjects=%#v err=%v", subjects, err)
	}
	role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-history-reader-role"}, Payload: command.AccessRoleInput{
			Name: "Prompt metadata reader", PermissionKeys: []string{"project.view"},
			AllowedScopes: []string{"PROJECT"}, ChangeComment: "component prompt history redaction",
		}})
	if err != nil || role.AccessRole == nil {
		t.Fatalf("create prompt history role: role=%#v err=%v", role.AccessRole, err)
	}
	binding, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "managed-history-reader-binding"}, Payload: command.AccessBindingInput{
			SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.AccessRole.CurrentVersion.Ref,
			Scope: entity.AccessScope{Kind: "PROJECT", ProjectRef: projectRef},
		}})
	if err != nil || binding.AccessBinding == nil {
		t.Fatalf("bind prompt history reader: binding=%#v err=%v", binding.AccessBinding, err)
	}
	reader := resolvedTestPrincipal(t, ctx, repository, input, "control-api-gateway")
	filter := query.Filter{Category: "PROMPT_TEMPLATE", Page: query.Page{Size: 1}}
	seenConfigurations := map[string]bool{}
	var catalogTotal int64
	for {
		items, count, next, err := service.ListManagedConfigurations(ctx, reader, filter)
		if err != nil || count < 1 {
			t.Fatalf("managed catalog read: total=%d err=%v", count, err)
		}
		catalogTotal = count
		for _, item := range items {
			if item.ProjectRef != projectRef || seenConfigurations[item.Ref] || item.CurrentRevision != nil && item.CurrentRevision.Content != "" {
				t.Fatal("managed catalog leaked content, another project, or repeated a row")
			}
			seenConfigurations[item.Ref] = true
		}
		if next == "" {
			break
		}
		if next == filter.Page.Token {
			t.Fatal("managed catalog cursor did not advance")
		}
		changed := filter
		changed.ProjectRef, changed.Page.Token = projectRef, next
		if _, _, _, err := service.ListManagedConfigurations(ctx, reader, changed); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("managed cursor accepted another filter: %v", err)
		}
		filter.Page.Token = next
	}
	if int64(len(seenConfigurations)) != catalogTotal || !seenConfigurations[configurationRef] {
		t.Fatal("managed catalog total or visibility mismatch")
	}
	_, history, total, _, err := service.ListManagedConfigurationHistory(ctx, reader, configurationRef, query.Page{Size: 20})
	if err != nil || total < 1 || len(history) < 1 {
		t.Fatalf("read redacted prompt history: history=%#v total=%d err=%v", history, total, err)
	}
	for _, revision := range history {
		if revision.Content != "" {
			t.Fatalf("prompt history content leaked without prompt.full.view: %#v", revision)
		}
	}
}

func testIntegrationDefinitionRebindAuthority(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
	service *platformservice.Service,
	owner value.Principal,
	definition command.Result,
	connection entity.IntegrationConnection,
) {
	t.Helper()
	input := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000009995", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Definition scope manager", CallerWorkload: "control-api-gateway",
		Operation: "platform.command.integration-definitions.rebind",
	}
	if _, err := repository.ResolveProofAuthority(ctx, input); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("unbound definition manager received authority: %v", err)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{
		Query: input.ExternalDisplayName, Page: query.Page{Size: 20},
	}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("resolve definition manager subject: subjects=%#v err=%v", subjects, err)
	}
	role, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "definition-scope-manager-role"}, Payload: command.AccessRoleInput{
			Name: "Definition scope manager", PermissionKeys: []string{"organization.manage", "project.manage"},
			AllowedScopes: []string{"ORGANIZATION"}, ChangeComment: "component definition rebind authority",
		}})
	if err != nil || role.AccessRole == nil {
		t.Fatalf("create definition scope manager role: role=%#v err=%v", role.AccessRole, err)
	}
	binding, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "definition-scope-manager-binding"}, Payload: command.AccessBindingInput{
			SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: role.AccessRole.CurrentVersion.Ref,
			Scope: entity.AccessScope{Kind: "ORGANIZATION"},
		}})
	if err != nil || binding.AccessBinding == nil {
		t.Fatalf("bind definition scope manager: binding=%#v err=%v", binding.AccessBinding, err)
	}
	manager := resolvedTestPrincipal(t, ctx, repository, input, "control-api-gateway")
	impact, err := service.GetManagedConfigurationImpact(ctx, owner, definition.ManagedConfiguration.Ref, definition.ManagedRevision.Ref, query.Filter{})
	if err != nil {
		t.Fatalf("read definition impact for authority test: %v", err)
	}
	version := definition.ManagedConfiguration.Version
	_, err = service.Execute(ctx, command.Command{Kind: command.RebindIntegrationDefinition, Principal: manager,
		Mutation: value.Mutation{IdempotencyKey: "definition-scope-manager-rebind", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: definition.ManagedConfiguration.Ref,
			RevisionRef: definition.ManagedRevision.Ref, ImpactDigest: impact.Digest,
			Consumers: []entity.ManagedConfigurationConsumer{{Kind: "INTEGRATION_CONNECTION", Ref: connection.Ref}}},
	})
	if !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("definition rebind without exact integration.manage error = %v", err)
	}
}

func publishAndRebindManagedConfiguration(
	t *testing.T,
	ctx context.Context,
	service *platformservice.Service,
	principal value.Principal,
	key string,
	createKind, validateKind, publishKind, rebindKind command.Kind,
	input command.ManagedConfigurationInput,
	consumer entity.ManagedConfigurationConsumer,
) command.Result {
	t.Helper()
	created, err := service.Execute(ctx, command.Command{Kind: createKind, Principal: principal,
		Mutation: value.Mutation{IdempotencyKey: key + "-create"}, Payload: input})
	if err != nil || created.ManagedConfiguration == nil || created.ManagedRevision == nil || created.ManagedRevision.State != "DRAFT" {
		t.Fatalf("create %s draft: result=%#v err=%v", key, created, err)
	}
	version := created.ManagedConfiguration.Version
	validated, err := service.Execute(ctx, command.Command{Kind: validateKind, Principal: principal,
		Mutation: value.Mutation{IdempotencyKey: key + "-validate", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			RevisionRef: created.ManagedRevision.Ref}})
	if err != nil || validated.ManagedRevision == nil || validated.ManagedRevision.State != "VALID" {
		t.Fatalf("validate %s draft: result=%#v err=%v", key, validated, err)
	}
	version = validated.ManagedConfiguration.Version
	published, err := service.Execute(ctx, command.Command{Kind: publishKind, Principal: principal,
		Mutation: value.Mutation{IdempotencyKey: key + "-publish", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			RevisionRef: created.ManagedRevision.Ref}})
	if err != nil || published.ManagedRevision == nil || published.ManagedRevision.State != "PUBLISHED" {
		t.Fatalf("publish %s draft: result=%#v err=%v", key, published, err)
	}
	impact, err := service.GetManagedConfigurationImpact(ctx, principal, created.ManagedConfiguration.Ref, created.ManagedRevision.Ref, query.Filter{})
	if err != nil || impact.Digest == "" {
		t.Fatalf("read %s impact: impact=%#v err=%v", key, impact, err)
	}
	version = published.ManagedConfiguration.Version
	rebound, err := service.Execute(ctx, command.Command{Kind: rebindKind, Principal: principal,
		Mutation: value.Mutation{IdempotencyKey: key + "-rebind", ExpectedVersion: &version},
		Payload: command.ManagedConfigurationInput{ConfigurationRef: created.ManagedConfiguration.Ref,
			RevisionRef: created.ManagedRevision.Ref, ImpactDigest: impact.Digest,
			Consumers: []entity.ManagedConfigurationConsumer{consumer}}})
	if err != nil || rebound.ManagedConfiguration == nil || rebound.ManagedRevision == nil {
		t.Fatalf("rebind %s consumer: result=%#v err=%v", key, rebound, err)
	}
	return rebound
}

func testStaleRoleRuntimeContractRejectsLaunch(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
	pool *pgxpool.Pool,
) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Runtime contract owner", CallerWorkload: "control-api-gateway",
		Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct runtime contract service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-contract-project"}, Payload: command.ProjectInput{
			Name: "Runtime contract boundary", Purpose: "Verify stale runtime contract launch rejection", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create runtime contract project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "runtime-contract-agent", "Runtime contract agent")
	baseline, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-contract-current-launch"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Task: "Verify launch with the current runtime contract.",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref},
		}})
	if err != nil || baseline.Run == nil {
		t.Fatalf("launch with current runtime contract: run=%#v err=%v", baseline.Run, err)
	}
	baselineVersion := baseline.Run.Version
	cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-contract-current-cancel", ExpectedVersion: &baselineVersion},
		Payload:  command.RunCommandInput{RunRef: baseline.Run.Ref, Reason: "Component fixture cleanup"},
	})
	if err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
		t.Fatalf("cancel current runtime contract fixture: run=%#v err=%v", cancelled.Run, err)
	}
	var sessionsBefore, runsBefore int64
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM control_plane.sessions session WHERE session.project_id = project.id),
		       (SELECT count(*) FROM control_plane.runs run WHERE run.project_id = project.id)
		FROM control_plane.projects project
		WHERE project.ref = $1
	`, project.Project.Ref).Scan(&sessionsBefore, &runsBefore); err != nil {
		t.Fatalf("read durable launch state before contract upgrade: %v", err)
	}
	original := repository.roleImages
	upgraded := original
	upgraded.RoleRuntimeContractRevision++
	upgraded.RoleRuntimeContractSHA256 = strings.Repeat("e", 64)
	if err := repository.ConfigureRoleImages(upgraded); err != nil {
		t.Fatalf("configure upgraded runtime contract: %v", err)
	}
	defer func() {
		if restoreErr := repository.ConfigureRoleImages(original); restoreErr != nil {
			t.Errorf("restore runtime contract configuration: %v", restoreErr)
		}
	}()
	if _, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-contract-stale-launch"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Task: "This run must be rejected before durable state is created.",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref},
		}}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("stale runtime contract launch error = %v, want conflict", err)
	}
	var sessionsAfter, runsAfter int64
	if err := pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM control_plane.sessions session WHERE session.project_id = project.id),
		       (SELECT count(*) FROM control_plane.runs run WHERE run.project_id = project.id)
		FROM control_plane.projects project
		WHERE project.ref = $1
	`, project.Project.Ref).Scan(&sessionsAfter, &runsAfter); err != nil {
		t.Fatalf("read durable launch state after rejected launch: %v", err)
	}
	if sessionsAfter != sessionsBefore || runsAfter != runsBefore {
		t.Fatalf("rejected launch changed durable state: sessions=%d->%d runs=%d->%d",
			sessionsBefore, sessionsAfter, runsBefore, runsAfter)
	}
}

func testSystemAssistantWarmRuntimeProviderFailover(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
	pool *pgxpool.Pool,
) {
	t.Helper()
	originalProviderCredential := repository.providerCredential
	defer func() {
		if restoreErr := repository.ConfigureProviderCredential(originalProviderCredential); restoreErr != nil {
			t.Errorf("restore configured provider credential: %v", restoreErr)
		}
	}()
	if _, err := pool.Exec(ctx, bootstrapComponentInsertWarmFailoverProviderQuery); err != nil {
		t.Fatalf("insert warm failover provider account: %v", err)
	}
	seedObservedCatalogFixture(t, ctx, repository)
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Warm failover owner", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	reconcileWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.reconcile",
	}, "runtime-controller")
	reportWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.warm.report",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct warm failover service: %v", err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatalf("read system assistant before failover: %v", err)
	}
	configuration, err := service.GetAgentRuntimeConfiguration(ctx, owner, assistant.Ref)
	if err != nil {
		t.Fatalf("read system assistant runtime configuration: %v", err)
	}
	outsiderInput := platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000092", ExternalTenantID: "20000000-0000-4000-8000-000000000002", ExternalDisplayName: "Runtime configuration outsider", CallerWorkload: "control-api-gateway", Operation: "platform.query.agents.runtime-configuration.get"}
	if _, outsiderErr := repository.ResolveProofAuthority(ctx, outsiderInput); !errors.Is(outsiderErr, domainerrs.ErrForbidden) {
		t.Fatalf("outsider runtime authority: %v", outsiderErr)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{Query: outsiderInput.ExternalDisplayName, Page: query.Page{Size: 20}}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("outsider subject: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := repository.resolveAccessSubject(ctx, tx, owner.AuthorityTenant, subjects[0].Ref)
	_ = tx.Rollback(ctx)
	if err != nil {
		t.Fatal(err)
	}
	outsider := owner
	outsider.ActorID = resolved.id
	if _, err := service.GetAgentRuntimeConfiguration(ctx, outsider, assistant.Ref); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("outsider read system configuration: %v", err)
	}
	agentRole, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "warm-agent-manager-role"}, Payload: command.AccessRoleInput{
			Name: "Warm agent manager", PermissionKeys: []string{"agent.view", "agent.manage"}, AllowedScopes: []string{"RESOURCE_KIND"}, ChangeComment: "System runtime boundary regression",
		}})
	if err != nil || agentRole.AccessRole == nil {
		t.Fatalf("agent role: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "warm-agent-manager-binding"}, Payload: command.AccessBindingInput{
			SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: agentRole.AccessRole.CurrentVersion.Ref,
			Scope: entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "AGENT"},
		}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.GetAgentRuntimeConfiguration(ctx, outsider, assistant.Ref); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("agent wildcard granted system runtime read: %v", err)
	}
	if _, _, _, err := service.ListConfigOverlayRevisions(ctx, outsider, query.Filter{ResourceRef: assistant.Ref}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("agent wildcard granted system overlay history: %v", err)
	}
	if _, err := service.GetConfigOverlayRevision(ctx, outsider, assistant.Ref, configuration.PublishedOverlay.Ref); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("agent wildcard granted system overlay preview: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.PublishAgentRuntimeConfig, Principal: outsider,
		Mutation: value.Mutation{IdempotencyKey: "warm-agent-manager-publish", ExpectedVersion: &configuration.AgentVersion},
		Payload:  command.AgentRuntimeConfigurationInput{AgentRef: assistant.Ref},
	}); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("agent wildcard granted system runtime publication: %v", err)
	}
	initialConfigurationVersion := configuration.Configuration.Version
	var initialConfigurationCount int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::bigint
		FROM control_plane.agent_runtime_config_versions config
		JOIN control_plane.agents agent ON agent.id = config.agent_id
		WHERE agent.ref = $1
	`, assistant.Ref).Scan(&initialConfigurationCount); err != nil {
		t.Fatalf("count system assistant runtime configuration revisions: %v", err)
	}
	var currentSessionID, currentSessionRef, currentAccountID, currentAccountRef, fallbackAccountID string
	var configuredProviderCredential ProviderCredentialConfig
	if err := pool.QueryRow(ctx, `
		SELECT session.id::text, session.ref, account.id::text, account.ref,
		       fallback.id::text, credential.secret_name, credential.secret_uid::text,
		       credential.secret_resource_version, credential.content_sha256
		FROM control_plane.assistant_runtime runtime
		JOIN control_plane.sessions session ON session.ref = runtime.system_session_ref
		JOIN control_plane.provider_accounts account ON account.id = session.provider_account_id
		JOIN control_plane.provider_credential_revisions credential
		  ON credential.id = account.current_credential_revision_id
		JOIN control_plane.provider_accounts fallback
		  ON fallback.organization_id = runtime.organization_id
		 AND fallback.ref = 'pacc_component_warm_failover'
		WHERE runtime.organization_id = session.organization_id
		  AND account.stable_key = 'default-openai-codex'
	`).Scan(
		&currentSessionID,
		&currentSessionRef,
		&currentAccountID,
		&currentAccountRef,
		&fallbackAccountID,
		&configuredProviderCredential.SecretName,
		&configuredProviderCredential.SecretUID,
		&configuredProviderCredential.SecretResourceVersion,
		&configuredProviderCredential.ContentSHA256,
	); err != nil {
		t.Fatalf("read initial warm session binding: %v", err)
	}
	if err := repository.ConfigureProviderCredential(configuredProviderCredential); err != nil {
		t.Fatalf("configure current default provider credential: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.provider_accounts
		SET state = 'REAUTHORIZATION_REQUIRED', version = version + 1, updated_at = clock_timestamp()
		WHERE id = $1::uuid
	`, currentAccountID); err != nil {
		t.Fatalf("reject current warm provider account: %v", err)
	}
	defer func() {
		if _, restoreErr := pool.Exec(context.WithoutCancel(ctx), `
			UPDATE control_plane.provider_accounts
			SET state = 'AUTHORIZED', enabled = true, version = version + 1, updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, currentAccountID); restoreErr != nil {
			t.Errorf("restore current warm provider account: %v", restoreErr)
		}
	}()

	const workloadInstance = "runtime-warm-failover"
	failedOver, desired, required, err := service.ReconcileWarmRuntime(ctx, reconcileWorker, workloadInstance)
	if err != nil || !required {
		t.Fatalf("reconcile rejected warm provider: assistant=%#v required=%v err=%v", failedOver, required, err)
	}
	selectedFallback := stringMap(desired, "providerAccountRef")
	if failedOver.WarmSessionRef == currentSessionRef || failedOver.RuntimeState != "RECOVERING" ||
		failedOver.LastHeartbeatAt != nil || selectedFallback == "" || selectedFallback == currentAccountRef || stringMap(desired, "reasoningMode") != "SUPPORTED" || stringMap(desired, "effectiveReasoningEffort") == "" {
		t.Fatalf("warm provider failover readback mismatch: assistant=%#v desired=%#v", failedOver, desired)
	}
	if err := pool.QueryRow(ctx, queryCatalogFixtureAccountID, owner.AuthorityTenant, selectedFallback).Scan(&fallbackAccountID); err != nil {
		t.Fatal(err)
	}
	var reconciledConfigurationVersion, reconciledConfigurationCount int64
	var reconciledPolicyMode string
	var reconciledCandidates []entity.ProviderAccountCandidate
	var rawReconciledCandidates []byte
	if err := pool.QueryRow(ctx, `
		SELECT config.version_number,
		       count(all_config.id)::bigint,
		       policy.mode,
		       policy.account_candidates
		FROM control_plane.agents agent
		JOIN control_plane.agent_runtime_config_versions config ON config.id = agent.current_runtime_config_id
		JOIN control_plane.provider_account_policy_versions policy ON policy.id = config.provider_account_policy_id
		JOIN control_plane.agent_runtime_config_versions all_config ON all_config.agent_id = agent.id
		WHERE agent.ref = $1
		GROUP BY config.version_number, policy.mode, policy.account_candidates
	`, assistant.Ref).Scan(
		&reconciledConfigurationVersion,
		&reconciledConfigurationCount,
		&reconciledPolicyMode,
		&rawReconciledCandidates,
	); err != nil {
		t.Fatalf("read reconciled system assistant provider policy: %v", err)
	}
	if err := decodeStrict(rawReconciledCandidates, &reconciledCandidates); err != nil {
		t.Fatalf("decode reconciled system assistant provider policy: %v", err)
	}
	expectedCandidates := make([]entity.ProviderAccountCandidate, 0, len(reconciledCandidates))
	rows, err := pool.Query(ctx, `
		SELECT account.ref
		FROM control_plane.provider_accounts account
		WHERE account.definition_key = 'openai-codex'
		  AND account.organization_id = (
		      SELECT runtime.organization_id
		      FROM control_plane.assistant_runtime runtime
		      JOIN control_plane.agents agent ON agent.id = runtime.agent_id
		      WHERE agent.ref = $1
		  )
		  AND account.current_credential_revision_id IS NOT NULL
		  AND account.state = 'AUTHORIZED' AND account.enabled
		ORDER BY account.ref
	`, assistant.Ref)
	if err != nil {
		t.Fatalf("list expected system assistant provider accounts: %v", err)
	}
	for rows.Next() {
		var accountRef string
		if err := rows.Scan(&accountRef); err != nil {
			rows.Close()
			t.Fatalf("scan expected system assistant provider account: %v", err)
		}
		expectedCandidates = append(expectedCandidates, entity.ProviderAccountCandidate{AccountRef: accountRef, Weight: 1})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("read expected system assistant provider accounts: %v", err)
	}
	for index := range expectedCandidates {
		catalog, readErr := service.ListModelCatalog(ctx, owner, "openai-codex", expectedCandidates[index].AccountRef, query.Filter{})
		if readErr != nil {
			t.Fatal(readErr)
		}
		expectedCandidates[index].ProviderDefinitionKey = "openai-codex"
		expectedCandidates[index].CatalogRevision, expectedCandidates[index].CatalogDigest = catalog.Revision, catalog.Digest
		for _, model := range catalog.Models {
			if model.ID == configuration.Configuration.Model {
				expectedCandidates[index].DefaultReasoningEffort = model.DefaultReasoningEffort
			}
		}
	}
	expectedMode := "LEAST_USED"
	if len(expectedCandidates) == 1 {
		expectedMode = "FIXED"
	}
	if reconciledConfigurationVersion != initialConfigurationVersion+1 ||
		reconciledConfigurationCount != initialConfigurationCount+1 ||
		reconciledPolicyMode != expectedMode ||
		!providerCandidatesEqual(reconciledCandidates, expectedCandidates) {
		t.Fatalf("system assistant provider policy was not reconciled: version=%d count=%d mode=%s candidates=%#v",
			reconciledConfigurationVersion, reconciledConfigurationCount, reconciledPolicyMode, reconciledCandidates)
	}
	var oldState, oldProviderID, currentState, currentProviderID string
	var currentWarmInstance *string
	var currentHeartbeat *time.Time
	var activeSessions int
	if err := pool.QueryRow(ctx, `
		SELECT old_session.state, old_session.provider_account_id::text,
		       current_session.state, current_session.provider_account_id::text,
		       runtime.warm_instance_ref, runtime.last_heartbeat_at,
		       count(*) FILTER (WHERE all_sessions.state = 'ACTIVE')::int
		FROM control_plane.assistant_runtime runtime
		JOIN control_plane.sessions old_session ON old_session.id = $1::uuid
		JOIN control_plane.sessions current_session ON current_session.ref = runtime.system_session_ref
		JOIN control_plane.sessions all_sessions
		  ON all_sessions.organization_id = runtime.organization_id
		 AND all_sessions.ref IN (old_session.ref, current_session.ref)
		WHERE runtime.organization_id = current_session.organization_id
		GROUP BY old_session.state, old_session.provider_account_id,
		         current_session.state, current_session.provider_account_id,
		         runtime.warm_instance_ref, runtime.last_heartbeat_at
	`, currentSessionID).Scan(&oldState, &oldProviderID, &currentState, &currentProviderID,
		&currentWarmInstance, &currentHeartbeat, &activeSessions); err != nil {
		t.Fatalf("read warm provider failover state: %v", err)
	}
	if oldState != "CLOSED" || oldProviderID != currentAccountID || currentState != "ACTIVE" ||
		currentProviderID != fallbackAccountID || currentWarmInstance != nil || currentHeartbeat != nil || activeSessions != 1 {
		t.Fatalf("warm provider failover is not atomic: old=%s/%s current=%s/%s instance=%v heartbeat=%v active=%d",
			oldState, oldProviderID, currentState, currentProviderID, currentWarmInstance, currentHeartbeat, activeSessions)
	}
	var rejectedState, rejectedCredentialID string
	var rejectedVersion, rejectedRevisionCount int64
	if err := pool.QueryRow(ctx, `
		SELECT account.state, account.current_credential_revision_id::text, account.version,
		       count(revision.id)::bigint
		FROM control_plane.provider_accounts account
		JOIN control_plane.provider_credential_revisions revision
		  ON revision.provider_account_id = account.id
		WHERE account.id = $1::uuid
		GROUP BY account.state, account.current_credential_revision_id, account.version
	`, currentAccountID).Scan(&rejectedState, &rejectedCredentialID, &rejectedVersion, &rejectedRevisionCount); err != nil {
		t.Fatalf("read rejected default provider before bootstrap restart: %v", err)
	}
	if rejectedState != "REAUTHORIZATION_REQUIRED" {
		t.Fatalf("default provider state before bootstrap restart = %q", rejectedState)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		if err := repository.Bootstrap(ctx); err != nil {
			t.Fatalf("bootstrap after warm provider failover attempt %d: %v", attempt, err)
		}
	}
	var restartedState, restartedCredentialID string
	var restartedVersion, restartedRevisionCount int64
	if err := pool.QueryRow(ctx, `
		SELECT account.state, account.current_credential_revision_id::text, account.version,
		       count(revision.id)::bigint
		FROM control_plane.provider_accounts account
		JOIN control_plane.provider_credential_revisions revision
		  ON revision.provider_account_id = account.id
		WHERE account.id = $1::uuid
		GROUP BY account.state, account.current_credential_revision_id, account.version
	`, currentAccountID).Scan(&restartedState, &restartedCredentialID, &restartedVersion, &restartedRevisionCount); err != nil {
		t.Fatalf("read rejected default provider after bootstrap restart: %v", err)
	}
	if restartedState != rejectedState || restartedCredentialID != rejectedCredentialID ||
		restartedVersion != rejectedVersion || restartedRevisionCount != rejectedRevisionCount {
		t.Fatalf("bootstrap restart changed rejected default provider: state=%s->%s credential=%s->%s version=%d->%d revisions=%d->%d",
			rejectedState, restartedState, rejectedCredentialID, restartedCredentialID,
			rejectedVersion, restartedVersion, rejectedRevisionCount, restartedRevisionCount)
	}
	reported, err := service.ReportWarmRuntime(ctx, reportWorker, command.WarmRuntimeInput{
		WorkloadInstance: workloadInstance, RuntimeRevision: failedOver.DesiredRuntimeRevision, State: "READY",
	})
	if err != nil {
		t.Fatalf("report failed-over warm runtime ready: %v", err)
	}
	stable, stableDesired, stableRequired, err := service.ReconcileWarmRuntime(ctx, reconcileWorker, workloadInstance)
	if err != nil || stableRequired {
		t.Fatalf("reconcile stable warm runtime: assistant=%#v desired=%#v required=%v err=%v",
			stable, stableDesired, stableRequired, err)
	}
	if stable.WarmSessionRef != failedOver.WarmSessionRef || stable.Version != reported.Version {
		t.Fatalf("stable reconcile repeated session migration: failed_over=%#v reported=%#v stable=%#v",
			failedOver, reported, stable)
	}
	var stableConfigurationCount int64
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::bigint
		FROM control_plane.agent_runtime_config_versions config
		JOIN control_plane.agents agent ON agent.id = config.agent_id
		WHERE agent.ref = $1
	`, assistant.Ref).Scan(&stableConfigurationCount); err != nil {
		t.Fatalf("count stable system assistant runtime configuration revisions: %v", err)
	}
	if stableConfigurationCount != reconciledConfigurationCount {
		t.Fatalf("stable reconcile created a duplicate provider policy revision: before=%d after=%d",
			reconciledConfigurationCount, stableConfigurationCount)
	}
	var systemSessionCount, activeSystemSessionCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int, count(*) FILTER (WHERE state = 'ACTIVE')::int
		FROM control_plane.sessions
		WHERE ref IN ($1, $2)
	`, currentSessionRef, failedOver.WarmSessionRef).Scan(&systemSessionCount, &activeSystemSessionCount); err != nil {
		t.Fatalf("count system assistant sessions after stable reconcile: %v", err)
	}
	if systemSessionCount != 2 || activeSystemSessionCount != 1 {
		t.Fatalf("stable reconcile changed session cardinality: total=%d active=%d", systemSessionCount, activeSystemSessionCount)
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "warm-failover-conversation-create"},
		Payload:  command.AssistantConversationInput{}})
	if err != nil || created.Conversation == nil {
		t.Fatalf("create assistant conversation after provider failover: conversation=%#v err=%v", created.Conversation, err)
	}
	var conversationProviderID, conversationProviderRef string
	if err := pool.QueryRow(ctx, `
		SELECT session.provider_account_id::text, account.ref
		FROM control_plane.assistant_conversations conversation
		JOIN control_plane.sessions session ON session.id = conversation.session_id
		JOIN control_plane.provider_accounts account ON account.id = session.provider_account_id
		WHERE conversation.ref = $1
	`, created.Conversation.Ref).Scan(&conversationProviderID, &conversationProviderRef); err != nil {
		t.Fatalf("read assistant conversation provider binding: %v", err)
	}
	conversationProviderAllowed := false
	for _, candidate := range expectedCandidates {
		if candidate.AccountRef == conversationProviderRef {
			conversationProviderAllowed = true
			break
		}
	}
	if !conversationProviderAllowed || conversationProviderID == currentAccountID {
		t.Fatalf("assistant conversation bypassed provider policy: provider_account_id=%s provider_account_ref=%s",
			conversationProviderID, conversationProviderRef)
	}
}

func testSystemAssistantRuntimeEnvironmentReconciliation(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	config := repository.roleImages
	config.DefaultImageReference = "registry.invalid/kodex/roles/system@sha256:" + strings.Repeat("e", 64)
	if err := repository.ConfigureRoleImages(config); err != nil {
		t.Fatalf("configure next system runtime image: %v", err)
	}
	reconcile := func() {
		t.Helper()
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin system runtime reconciliation: %v", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := repository.reconcileSystemAssistantRuntimeEnvironment(ctx, tx); err != nil {
			t.Fatalf("reconcile next system runtime image: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit system runtime reconciliation: %v", err)
		}
	}
	reconcile()
	image := entity.RuntimeEnvironmentImage{Reference: config.DefaultImageReference, Digest: "sha256:" + strings.Repeat("e", 64)}
	policy := runtimecontract.DefaultRuntimeEnvironmentPolicy()
	expectedCoreDigest, expectedDigest, err := runtimeEnvironmentConfigurationDigests(nil, nil, image, nil, policy)
	if err != nil {
		t.Fatalf("compute expected system runtime digest: %v", err)
	}
	assertReadback := func(expectedVersions int) {
		t.Helper()
		var version, versionCount int
		var coreDigest, digest, state, desiredRevision string
		var parentBound, warmInstanceCleared, heartbeatCleared bool
		if err := pool.QueryRow(ctx, bootstrapComponentRuntimeEnvironmentReconcileReadbackQuery).Scan(
			&version, &coreDigest, &digest, &parentBound, &versionCount,
			&state, &desiredRevision, &warmInstanceCleared, &heartbeatCleared,
		); err != nil {
			t.Fatalf("read reconciled system runtime environment: %v", err)
		}
		if version != 2 || coreDigest != expectedCoreDigest || digest != expectedDigest || !parentBound ||
			versionCount != expectedVersions || state != "RECOVERING" ||
			desiredRevision != "system-assistant-runtime-"+expectedDigest || !warmInstanceCleared || !heartbeatCleared {
			t.Fatalf("unexpected reconciled system runtime: version=%d core=%s digest=%s parent=%t versions=%d state=%s desired=%s warmCleared=%t heartbeatCleared=%t",
				version, coreDigest, digest, parentBound, versionCount, state, desiredRevision, warmInstanceCleared, heartbeatCleared)
		}
	}
	assertReadback(2)
	reconcile()
	assertReadback(2)
}

func testProviderAccountApplicationAccess(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	ownerInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Provider account owner", CallerWorkload: "control-api-gateway",
		Operation: "platform.query.provider-accounts.list",
	}
	owner := resolvedTestPrincipal(t, ctx, repository, ownerInput, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct provider account access service: %v", err)
	}
	ownerItems, _, ownerActions, err := service.ListProviderAccounts(ctx, owner, query.Filter{Page: query.Page{Size: 20}})
	if err != nil || len(ownerItems) == 0 || !reflect.DeepEqual(ownerActions, []string{"CREATE_CONNECTION"}) ||
		!contains(ownerItems[0].NextActions, "REVOKE") {
		t.Fatalf("owner provider account actions: items=%#v actions=%v err=%v", ownerItems, ownerActions, err)
	}
	created, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProviderAccount, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-account-owner-create"},
		Payload: command.ProviderAccountInput{
			DefinitionKey: "openai-codex", Name: "Provider account owner API key",
		},
	})
	if err != nil || created.ProviderAccount == nil || created.ProviderAccount.State != "PENDING_AUTHORIZATION" {
		t.Fatalf("owner create provider account: account=%#v err=%v", created.ProviderAccount, err)
	}
	ownerItems, _, _, err = service.ListProviderAccounts(ctx, owner, query.Filter{Page: query.Page{Size: 20}})
	if err != nil {
		t.Fatalf("list provider accounts after owner create: %v", err)
	}
	providerAccess, err := service.QueryEffectiveAccess(ctx, owner, "", entity.AccessScope{
		Kind: "RESOURCE_INSTANCE", ResourceKind: "PROVIDER_ACCOUNT", ResourceRef: ownerItems[0].Ref,
	}, []string{"provider.account.view"}, time.Time{})
	if err != nil || len(providerAccess.Decisions) != 1 || !providerAccess.Decisions[0].Allowed {
		t.Fatalf("resolve provider account effective access: access=%#v err=%v", providerAccess, err)
	}

	candidateInput := platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000009993", ExternalTenantID: ownerInput.ExternalTenantID,
		ExternalDisplayName: "Provider account viewer", CallerWorkload: "control-api-gateway",
		Operation: "platform.query.provider-accounts.list",
	}
	if _, resolveErr := repository.ResolveProofAuthority(ctx, candidateInput); !errors.Is(resolveErr, domainerrs.ErrForbidden) {
		t.Fatalf("unbound provider account viewer received authority: %v", resolveErr)
	}
	subjects, _, err := service.ListAccessSubjects(ctx, owner, query.Filter{
		Query: candidateInput.ExternalDisplayName, Page: query.Page{Size: 20},
	}, "USER")
	if err != nil || len(subjects) != 1 {
		t.Fatalf("list provider account viewer subject: subjects=%#v err=%v", subjects, err)
	}
	roleResult, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessRole, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-account-viewer-role"}, Payload: command.AccessRoleInput{
			Name: "Provider account viewer", PermissionKeys: []string{"provider.account.view"},
			AllowedScopes: []string{"RESOURCE_KIND"}, ChangeComment: "component provider account RBAC scenario",
		}})
	if err != nil || roleResult.AccessRole == nil {
		t.Fatalf("create provider account viewer role: role=%#v err=%v", roleResult.AccessRole, err)
	}
	bindingResult, err := service.Execute(ctx, command.Command{Kind: command.CreateAccessBinding, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-account-viewer-binding"}, Payload: command.AccessBindingInput{
			SubjectKind: "USER", SubjectRef: subjects[0].Ref, RoleVersionRef: roleResult.AccessRole.CurrentVersion.Ref,
			Scope: entity.AccessScope{Kind: "RESOURCE_KIND", ResourceKind: "PROVIDER_ACCOUNT"},
		}})
	if err != nil || bindingResult.AccessBinding == nil {
		t.Fatalf("bind provider account viewer: binding=%#v err=%v", bindingResult.AccessBinding, err)
	}
	viewer := resolvedTestPrincipal(t, ctx, repository, candidateInput, "control-api-gateway")
	viewerItems, _, viewerActions, err := service.ListProviderAccounts(ctx, viewer, query.Filter{Page: query.Page{Size: 20}})
	if err != nil || len(viewerItems) != len(ownerItems) || len(viewerActions) != 0 {
		t.Fatalf("viewer provider account collection: items=%#v actions=%v err=%v", viewerItems, viewerActions, err)
	}
	for _, item := range viewerItems {
		if !reflect.DeepEqual(item.NextActions, []string{"OPEN"}) {
			t.Fatalf("viewer provider account %q actions=%v, want [OPEN]", item.Ref, item.NextActions)
		}
	}
	viewerItem, err := service.GetProviderAccount(ctx, viewer, viewerItems[0].Ref)
	if err != nil || !reflect.DeepEqual(viewerItem.NextActions, []string{"OPEN"}) {
		t.Fatalf("viewer provider account detail: item=%#v err=%v", viewerItem, err)
	}
}

func testRuntimeConfigurationPublish(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	if _, err := repository.pool.Exec(ctx, bootstrapComponentInsertSecondaryProviderQuery); err != nil {
		t.Fatalf("insert secondary provider account: %v", err)
	}
	seedObservedCatalogFixture(t, ctx, repository)
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
	roleImagePrincipal, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve role image principal: %v", err)
	}
	recipes, _, err := repository.List(ctx, roleImagePrincipal, roleimagerepo.Filter{
		ProjectRef: createdProject.Project.Ref,
		Page:       query.Page{Size: 20},
	})
	if err != nil || len(recipes) != 1 || recipes[0].ActiveImageArtifactRef == "" ||
		recipes[0].PromotedImageReference != repository.roleImages.DefaultImageReference {
		t.Fatalf("bootstrap role image is not active and promoted: recipes=%#v err=%v", recipes, err)
	}
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
	inputCandidates := append([]entity.ProviderAccountCandidate{}, current.Configuration.ProviderPolicy.AccountCandidates...)
	for index := range inputCandidates {
		inputCandidates[index].DefaultReasoningEffort = ""
	}
	for name, mutate := range map[string]func(*entity.ProviderAccountCandidate){
		"missing-pin": func(candidate *entity.ProviderAccountCandidate) {
			candidate.CatalogRevision = ""
			candidate.CatalogDigest = ""
		},
		"stale-pin": func(candidate *entity.ProviderAccountCandidate) {
			candidate.CatalogDigest = strings.Repeat("f", 64)
			candidate.CatalogRevision = "mcat_" + candidate.CatalogDigest
		},
		"self-issued-default": func(candidate *entity.ProviderAccountCandidate) { candidate.DefaultReasoningEffort = "high" },
		"other-provider":      func(candidate *entity.ProviderAccountCandidate) { candidate.ProviderDefinitionKey = "other-provider" },
	} {
		candidates := append([]entity.ProviderAccountCandidate{}, inputCandidates...)
		mutate(&candidates[0])
		_, failure := service.Execute(ctx, command.Command{Kind: command.PublishAgentRuntimeConfig, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "runtime-configuration-" + name, ExpectedVersion: &expectedVersion},
			Payload:  command.AgentRuntimeConfigurationInput{AgentRef: agent.Ref, RuntimeProfileRef: current.Configuration.RuntimeProfileRef, Model: current.Configuration.Model, ProviderPolicyMode: current.Configuration.ProviderPolicy.Mode, ProviderAccounts: candidates},
		})
		if name == "stale-pin" && !errors.Is(failure, domainerrs.ErrVersionMismatch) || name != "stale-pin" && !errors.Is(failure, domainerrs.ErrInvalid) {
			t.Fatalf("catalog mutation %s: %v", name, failure)
		}
	}
	result, err := service.Execute(ctx, command.Command{Kind: command.PublishAgentRuntimeConfig, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-configuration-publish", ExpectedVersion: &expectedVersion},
		Payload: command.AgentRuntimeConfigurationInput{
			AgentRef: agent.Ref, RuntimeProfileRef: current.Configuration.RuntimeProfileRef,
			Model: current.Configuration.Model, ProviderPolicyMode: current.Configuration.ProviderPolicy.Mode,
			ProviderAccounts: inputCandidates,
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
	expectedVersion = result.RuntimeConfiguration.AgentVersion
	draft, err := service.Execute(ctx, command.Command{Kind: command.CreateConfigOverlayDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-overlay-unsupported-effort", ExpectedVersion: &expectedVersion},
		Payload:  command.ConfigOverlayInput{AgentRef: agent.Ref, Content: "model_reasoning_effort = \"adaptive\"\n"},
	})
	if err != nil || draft.RuntimeConfiguration == nil || draft.RuntimeConfiguration.DraftOverlay == nil {
		t.Fatalf("create overlay: %v", err)
	}
	expectedVersion = draft.RuntimeConfiguration.AgentVersion
	validated, err := service.Execute(ctx, command.Command{Kind: command.ValidateConfigOverlayDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-overlay-validate-unsupported", ExpectedVersion: &expectedVersion},
		Payload:  command.ConfigOverlayInput{AgentRef: agent.Ref},
	})
	if err != nil || validated.RuntimeConfiguration == nil || validated.RuntimeConfiguration.DraftOverlay == nil {
		t.Fatalf("validate overlay: %v", err)
	}
	invalid := validated.RuntimeConfiguration.DraftOverlay
	if invalid.State != "INVALID" || len(invalid.Diagnostics) != 1 || invalid.Diagnostics[0].Code != "CONFIG_OVERLAY_EFFORT_UNSUPPORTED" || invalid.Diagnostics[0].Key != "model_reasoning_effort" || invalid.Diagnostics[0].Line != 1 || invalid.Diagnostics[0].Column < 1 || invalid.SchemaRevision == "" || invalid.SchemaDigest == "" {
		t.Fatalf("safe versioned diagnostic missing: %+v", invalid)
	}
	expectedVersion = validated.RuntimeConfiguration.AgentVersion
	if _, err := service.Execute(ctx, command.Command{Kind: command.PublishConfigOverlayDraft, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-overlay-publish-unsupported", ExpectedVersion: &expectedVersion},
		Payload:  command.ConfigOverlayInput{AgentRef: agent.Ref},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("unsupported overlay published: %v", err)
	}
}

func testSessionProviderAffinityAfterPolicyMutation(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
	pool *pgxpool.Pool,
) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Provider affinity owner", CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	owner.CredentialAuthenticatedAt = time.Now().UTC()
	owner.CredentialACR = "urn:kodex:acr:interactive"
	owner.CredentialAMR = []string{"pwd"}
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct provider affinity service: %v", err)
	}

	var primaryAccountID, primaryAccountRef, primaryCredentialID, secondaryAccountRef string
	if err := pool.QueryRow(ctx, `
		SELECT primary_account.id::text, primary_account.ref,
		       primary_account.current_credential_revision_id::text, secondary_account.ref
		FROM control_plane.provider_accounts primary_account
		JOIN control_plane.provider_accounts secondary_account
		  ON secondary_account.organization_id = primary_account.organization_id
		 AND secondary_account.stable_key = 'component-secondary'
		WHERE primary_account.stable_key = 'default-openai-codex'
	`).Scan(&primaryAccountID, &primaryAccountRef, &primaryCredentialID, &secondaryAccountRef); err != nil {
		t.Fatalf("read provider affinity accounts: %v", err)
	}

	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-project"}, Payload: command.ProjectInput{
			Name: "Provider affinity", Purpose: "Verify immutable Session account affinity", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create provider affinity project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref,
		"provider-affinity-agent", "Provider affinity specialist")

	publishFixedPolicy := func(key, accountRef string, selectedModel ...string) {
		t.Helper()
		current, readErr := service.GetAgentRuntimeConfiguration(ctx, owner, agent.Ref)
		if readErr != nil {
			t.Fatalf("read provider affinity runtime configuration: %v", readErr)
		}
		catalog, catalogErr := service.ListModelCatalog(ctx, owner, current.Configuration.Provider, accountRef, query.Filter{})
		if catalogErr != nil {
			t.Fatalf("read provider affinity catalog: %v", catalogErr)
		}
		expectedVersion := current.AgentVersion
		model := current.Configuration.Model
		if len(selectedModel) != 0 {
			model = selectedModel[0]
		}
		published, publishErr := service.Execute(ctx, command.Command{Kind: command.PublishAgentRuntimeConfig, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key, ExpectedVersion: &expectedVersion},
			Payload: command.AgentRuntimeConfigurationInput{
				AgentRef: agent.Ref, RuntimeProfileRef: current.Configuration.RuntimeProfileRef,
				Model: model, ProviderPolicyMode: "FIXED",
				ProviderAccounts: []entity.ProviderAccountCandidate{{AccountRef: accountRef, Weight: 1, ProviderDefinitionKey: current.Configuration.Provider, CatalogRevision: catalog.Revision, CatalogDigest: catalog.Digest}},
			}})
		if publishErr != nil || published.RuntimeConfiguration == nil {
			t.Fatalf("publish fixed provider policy for %s: configuration=%#v err=%v",
				accountRef, published.RuntimeConfiguration, publishErr)
		}
	}

	publishFixedPolicy("provider-affinity-policy-primary", primaryAccountRef)
	launched, err := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-launch"}, Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Provider affinity run",
			Task: "Verify immutable provider account affinity.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref},
		}})
	if err != nil || launched.Run == nil {
		t.Fatalf("launch provider affinity run: run=%#v err=%v", launched.Run, err)
	}
	publishFixedPolicy("provider-affinity-policy-secondary", secondaryAccountRef)

	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-claim"},
		Payload:  command.LeaseInput{WorkloadInstance: "runtime-provider-affinity", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 ||
		stringMap(claimed.RuntimeItems[0], "runRef") != launched.Run.Ref ||
		stringMap(claimed.RuntimeItems[0], "providerAccountRef") != primaryAccountRef {
		t.Fatalf("claim switched Session provider after policy mutation: claims=%#v err=%v", claimed.RuntimeItems, err)
	}
	lease := claimed.RuntimeItems[0]
	if stringMap(lease, "effectiveReasoningEffort") != "high" || stringMap(lease, "reasoningMode") != "SUPPORTED" {
		t.Fatal("materialization lost server-owned model effort")
	}
	workspacePolicy, ok := lease["workspacePolicy"].(entity.RuntimeWorkspacePolicy)
	if !ok || !reflect.DeepEqual(workspacePolicy, runtimeWorkspacePolicy()) {
		t.Fatalf("claim does not carry a bounded workspace policy: %#v", lease["workspacePolicy"])
	}
	promptSnapshot, ok := lease["promptSnapshot"].(entity.PromptMaterializationSnapshot)
	if !ok || promptSnapshot.Variables["agent.name"] != agent.Name || promptSnapshot.Variables["project.name"] != project.Project.Name {
		t.Fatalf("claim does not carry server-owned contextual names: %#v", lease["promptSnapshot"])
	}
	testTemplateVariableContext(t, ctx, repository, service, owner, agent.ProjectRef, agent.Ref, stringMap(lease, "runtimeRevisionRef"))
	preview, err := service.PreviewPromptTemplate(ctx, owner, "", "RUN", launched.Run.Ref, true)
	if err != nil || preview.Digest != stringMap(lease, "promptMaterializationDigest") ||
		preview.Prompt != stringMap(lease, "instructions") || strings.Contains(preview.SafePrompt, "immutable provider account affinity") {
		t.Fatalf("run prompt preview diverged from pinned snapshot: preview=%#v err=%v", preview, err)
	}
	sessionPreview, err := service.PreviewPromptTemplate(ctx, owner, "", "SESSION", launched.Run.SessionRef, false)
	if err != nil || sessionPreview.Digest != preview.Digest || sessionPreview.Prompt != preview.Prompt {
		t.Fatalf("session prompt preview diverged from pinned snapshot: preview=%#v err=%v", sessionPreview, err)
	}
	var sessionAccountID, revisionAccountID string
	if err := pool.QueryRow(ctx, `
		SELECT session.provider_account_id::text, revision.provider_account_id::text
		FROM control_plane.runtime_revisions revision
		JOIN control_plane.sessions session ON session.id = revision.session_id
		WHERE revision.ref = $1
	`, stringMap(lease, "runtimeRevisionRef")).Scan(&sessionAccountID, &revisionAccountID); err != nil {
		t.Fatalf("read provider affinity RuntimeRevision: %v", err)
	}
	if sessionAccountID != primaryAccountID || revisionAccountID != primaryAccountID {
		t.Fatalf("RuntimeRevision account differs from Session: session=%s revision=%s want=%s",
			sessionAccountID, revisionAccountID, primaryAccountID)
	}
	refresh := command.ProviderCredentialRefreshInput{
		LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
		PreviousCredentialRevisionRef: stringMap(lease, "providerCredentialRevisionRef"), PreviousContentSHA256: stringMap(lease, "providerCredentialSHA256"),
		SecretName: "runtime-provider-affinity-refreshed", SecretUID: "50000000-0000-4000-8000-000000000091", SecretResourceVersion: "affinity-refresh-1", ContentSHA256: strings.Repeat("9", 64),
	}
	refreshed, err := service.Execute(ctx, command.Command{Kind: command.CommitProviderCredentialRefresh, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-managed-refresh"}, Payload: refresh,
	})
	if err != nil || stringMap(refreshed.Runtime, "providerCredentialRevisionRef") == "" {
		t.Fatalf("managed refresh: %v", err)
	}
	if err := pool.QueryRow(ctx, queryCatalogFixtureCredentialID, stringMap(refreshed.Runtime, "providerCredentialRevisionRef")).Scan(&primaryCredentialID); err != nil {
		t.Fatal(err)
	}
	completed := completeClaimedExecution(t, ctx, service, worker, lease, "provider-affinity-first", false)
	if completed.Run == nil || completed.Run.State != "SUCCEEDED" {
		t.Fatalf("complete provider affinity run: run=%#v", completed.Run)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-refresh-without-observation"},
		Payload:  command.SessionTurnInput{SessionRef: launched.Run.SessionRef, RunRef: launched.Run.Ref, Task: "Continue after managed refresh."},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("managed refresh reused stale observation: %v", err)
	}
	seedObservedCatalogFixture(t, ctx, repository)

	continued, err := service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-continuation"}, Payload: command.SessionTurnInput{
			SessionRef: launched.Run.SessionRef, RunRef: launched.Run.Ref,
			Task: "Verify revoked Session account fails closed without provider fallback.",
		}})
	if err != nil || continued.Run == nil {
		t.Fatalf("continue provider affinity Session: run=%#v err=%v", continued.Run, err)
	}

	restored := false
	defer func() {
		if restored {
			return
		}
		if _, restoreErr := pool.Exec(context.WithoutCancel(ctx), `
			UPDATE control_plane.provider_accounts
			SET state = 'AUTHORIZED', enabled = true, current_credential_revision_id = $2::uuid,
			    version = version + 1, updated_at = clock_timestamp()
			WHERE id = $1::uuid
		`, primaryAccountID, primaryCredentialID); restoreErr != nil {
			t.Errorf("restore provider affinity account: %v", restoreErr)
		}
	}()
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.provider_accounts
		SET state = 'REVOKED', enabled = false, current_credential_revision_id = NULL,
		    version = version + 1, updated_at = clock_timestamp()
		WHERE id = $1::uuid
	`, primaryAccountID); err != nil {
		t.Fatalf("revoke provider affinity account: %v", err)
	}

	blocked, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-claim-revoked"},
		Payload:  command.LeaseInput{WorkloadInstance: "runtime-provider-affinity-revoked", Limit: 1}})
	if err != nil || len(blocked.RuntimeItems) != 0 {
		t.Fatalf("revoked Session account switched to current policy fallback: claims=%#v err=%v", blocked.RuntimeItems, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.provider_accounts
		SET state = 'AUTHORIZED', enabled = true, current_credential_revision_id = $2::uuid,
		    version = version + 1, updated_at = clock_timestamp()
		WHERE id = $1::uuid
	`, primaryAccountID, primaryCredentialID); err != nil {
		t.Fatalf("restore provider affinity account: %v", err)
	}
	restored = true
	if _, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-claim-before-observation"},
		Payload:  command.LeaseInput{WorkloadInstance: "runtime-provider-affinity-unobserved", Limit: 1},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("restored account reused old observation: %v", err)
	}
	seedObservedCatalogFixture(t, ctx, repository)

	recovered, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-claim-restored"},
		Payload:  command.LeaseInput{WorkloadInstance: "runtime-provider-affinity-restored", Limit: 1}})
	if err != nil || len(recovered.RuntimeItems) != 1 ||
		stringMap(recovered.RuntimeItems[0], "runRef") != continued.Run.Ref ||
		stringMap(recovered.RuntimeItems[0], "providerAccountRef") != primaryAccountRef {
		t.Fatalf("restored Session did not retain provider account: claims=%#v err=%v", recovered.RuntimeItems, err)
	}
	originalRunPreview, err := service.PreviewPromptTemplate(ctx, owner, "", "RUN", launched.Run.Ref, false)
	diff, diffErr := service.GetRuntimeRevisionDiff(ctx, owner, continued.Run.Ref, stringMap(recovered.RuntimeItems[0], "runtimeRevisionRef"))
	if diffErr != nil || diff.Previous == nil || diff.Previous.Ref != stringMap(lease, "runtimeRevisionRef") ||
		diff.Current.SessionRef != diff.Previous.SessionRef || diff.Current.RunRef != continued.Run.Ref {
		t.Fatalf("runtime revision diff lost exact session predecessor: diff=%+v err=%v", diff, diffErr)
	}
	if _, err := service.GetRuntimeRevisionDiff(ctx, owner, launched.Run.Ref, diff.Current.Ref); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("runtime revision diff accepted current revision of another run: %v", err)
	}
	firstDiff, firstDiffErr := service.GetRuntimeRevisionDiff(ctx, owner, launched.Run.Ref, "")
	if firstDiffErr != nil || firstDiff.Previous != nil || firstDiff.Current.Ref != stringMap(lease, "runtimeRevisionRef") {
		t.Fatalf("first runtime revision has unexpected predecessor: diff=%+v err=%v", firstDiff, firstDiffErr)
	}
	if err != nil || originalRunPreview.Digest != preview.Digest {
		t.Fatalf("exact original Run preview selected another root revision: preview=%#v err=%v", originalRunPreview, err)
	}
	continuedRunPreview, err := service.PreviewPromptTemplate(ctx, owner, "", "RUN", continued.Run.Ref, false)
	if err != nil || continuedRunPreview.Digest != stringMap(recovered.RuntimeItems[0], "promptMaterializationDigest") ||
		continuedRunPreview.Digest == originalRunPreview.Digest {
		t.Fatalf("exact continuation Run preview selected another root revision: preview=%#v err=%v", continuedRunPreview, err)
	}
	completeClaimedExecution(t, ctx, service, worker, recovered.RuntimeItems[0], "provider-affinity-restored", false)
	if _, err := pool.Exec(ctx, queryCatalogFixtureAdvanceAccount, owner.AuthorityTenant, primaryAccountRef); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, repository, func(observation *platformrepo.ProviderModelCatalogObservation) {
		observation.Models[0].DefaultReasoningEffort = "low"
	})
	if _, err := service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-changed-default-denied"},
		Payload:  command.SessionTurnInput{SessionRef: launched.Run.SessionRef, RunRef: continued.Run.Ref, Task: "Continue after changed default."},
	}); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("changed capabilities reused Session pin: %v", err)
	}
	publishFixedPolicy("provider-affinity-republish-default", primaryAccountRef)
	next, err := service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-new-default-turn"},
		Payload:  command.SessionTurnInput{SessionRef: launched.Run.SessionRef, RunRef: continued.Run.Ref, Task: "Use explicitly republished model default."},
	})
	if err != nil || next.Run == nil {
		t.Fatalf("republished default turn: %v", err)
	}
	newDefault, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-new-default-claim"},
		Payload:  command.LeaseInput{WorkloadInstance: "runtime-provider-affinity-default", Limit: 1},
	})
	if err != nil || len(newDefault.RuntimeItems) != 1 || stringMap(newDefault.RuntimeItems[0], "effectiveReasoningEffort") != "low" || stringMap(newDefault.RuntimeItems[0], "providerAccountRef") != primaryAccountRef {
		t.Fatalf("new server default not materialized: %+v %v", newDefault.RuntimeItems, err)
	}
	completeClaimedExecution(t, ctx, service, worker, newDefault.RuntimeItems[0], "provider-affinity-new-default", false)
	publishFixedPolicy("provider-affinity-non-reasoning-model", primaryAccountRef, "non-reasoning")
	next, err = service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-non-reasoning-turn"},
		Payload:  command.SessionTurnInput{SessionRef: launched.Run.SessionRef, RunRef: next.Run.Ref, Task: "Use a model without reasoning support."},
	})
	if err != nil || next.Run == nil {
		t.Fatalf("non-reasoning turn: %v", err)
	}
	unsupported, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-non-reasoning-claim"},
		Payload:  command.LeaseInput{WorkloadInstance: "runtime-provider-affinity-non-reasoning", Limit: 1},
	})
	if err != nil || len(unsupported.RuntimeItems) != 1 || stringMap(unsupported.RuntimeItems[0], "effectiveReasoningEffort") != "" || stringMap(unsupported.RuntimeItems[0], "reasoningMode") != "UNSUPPORTED" {
		t.Fatalf("non-reasoning model received invented effort: %+v %v", unsupported.RuntimeItems, err)
	}
	completeClaimedExecution(t, ctx, service, worker, unsupported.RuntimeItems[0], "provider-affinity-non-reasoning", false)
	if _, err := pool.Exec(ctx, queryCatalogFixtureAdvanceAccount, owner.AuthorityTenant, secondaryAccountRef); err != nil {
		t.Fatal(err)
	}
	seedObservedCatalogFixture(t, ctx, repository, func(observation *platformrepo.ProviderModelCatalogObservation) {
		observation.Models = append(observation.Models, platformrepo.ProviderModelCatalogRecord{ID: "secondary-only"})
	})
	publishFixedPolicy("provider-affinity-secondary-only-model", secondaryAccountRef, "secondary-only")
	if _, err := service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-affinity-unsupported-new-model"},
		Payload:  command.SessionTurnInput{SessionRef: launched.Run.SessionRef, RunRef: next.Run.Ref, Task: "Reject model absent from the selected Session account."},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("unsupported model switched Session account: %v", err)
	}
	publishFixedPolicy("provider-affinity-restore-model", primaryAccountRef, "gpt-5")
	for _, ref := range []string{primaryAccountRef, secondaryAccountRef} {
		if _, err := pool.Exec(ctx, queryCatalogFixtureAdvanceAccount, owner.AuthorityTenant, ref); err != nil {
			t.Fatal(err)
		}
	}
	seedObservedCatalogFixture(t, ctx, repository)
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

func testRuntimeEnvironmentLifecycle(
	t *testing.T,
	ctx context.Context,
	repository *Repository,
	pool *pgxpool.Pool,
) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		ExternalDisplayName: "Runtime environment lifecycle owner", CallerWorkload: "control-api-gateway",
		Operation: "platform.command.runtime-environments.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct runtime environment lifecycle service: %v", err)
	}
	var artifactRef, projectRef string
	if err := pool.QueryRow(ctx, `
SELECT artifact.ref, project.ref
FROM control_plane.image_artifacts artifact
JOIN control_plane.projects project ON project.id = artifact.project_id
JOIN control_plane.role_image_recipes recipe ON recipe.id = artifact.recipe_id
WHERE project.name = 'Role image promotion'
  AND recipe.name = 'Promotion image'
  AND recipe.active_image_artifact_id = artifact.id
  AND artifact.admission_state = 'ACCEPTED'
  AND artifact.promotion_state = 'PROMOTED'
  AND artifact.promoted_reference <> ''
ORDER BY artifact.promoted_at DESC, artifact.ref
LIMIT 1`).Scan(&artifactRef, &projectRef); err != nil {
		t.Fatalf("read promoted runtime image fixture: %v", err)
	}
	createEnvironment := func(key, name, mode string) entity.RuntimeEnvironmentSet {
		t.Helper()
		result, createErr := service.Execute(ctx, command.Command{
			Kind: command.CreateRuntimeEnvironment, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key},
			Payload: command.RuntimeEnvironmentInput{
				ProjectRef: projectRef, Name: name, Description: "Runtime lifecycle component environment",
				ImageArtifactRef: artifactRef,
				Values:           []entity.RuntimeEnvironmentValue{{Name: "LIFECYCLE_MODE", Value: mode}},
				Policy:           runtimecontract.DefaultRuntimeEnvironmentPolicy(),
			},
		})
		if createErr != nil || result.RuntimeEnvironment == nil || result.RuntimeEnvironment.CurrentVersion.Ref == "" ||
			result.RuntimeEnvironment.CurrentVersion.Digest == "" || !result.RuntimeEnvironment.Ready ||
			len(result.RuntimeEnvironment.ReadinessBlockers) != 0 {
			t.Fatalf("create %s: environment=%#v err=%v", key, result.RuntimeEnvironment, createErr)
		}
		return *result.RuntimeEnvironment
	}
	first := createEnvironment("runtime-environment-lifecycle-first", "Runtime lifecycle first", "first")
	second := createEnvironment("runtime-environment-lifecycle-second", "Runtime lifecycle second", "second")
	environments, _, err := service.ListRuntimeEnvironments(ctx, owner, query.Filter{ProjectRef: projectRef})
	if err != nil {
		t.Fatalf("list ready runtime environments: %v", err)
	}
	for _, environment := range environments {
		if (environment.Ref == first.Ref || environment.Ref == second.Ref) &&
			(!environment.Ready || len(environment.ReadinessBlockers) != 0) {
			t.Fatalf("promoted runtime environment is not ready in list: %#v", environment)
		}
	}
	agent := createLifecycleAgent(t, ctx, service, owner, projectRef,
		"runtime-environment-lifecycle-agent", "Runtime lifecycle specialist")
	agentVersion := agent.Version
	boundFirst, err := service.Execute(ctx, command.Command{
		Kind: command.BindAgentRuntimeEnvironment, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-bind-first", ExpectedVersion: &agentVersion},
		Payload:  command.RuntimeEnvironmentBindingInput{AgentRef: agent.Ref, EnvironmentRef: first.Ref},
	})
	if err != nil || boundFirst.RuntimeConfiguration == nil || boundFirst.RuntimeConfiguration.Environment.Ref != first.Ref ||
		!boundFirst.RuntimeConfiguration.Environment.Ready ||
		!reflect.DeepEqual(boundFirst.RuntimeConfiguration.Environment.CurrentVersion, first.CurrentVersion) {
		t.Fatalf("bind first runtime environment: configuration=%#v err=%v", boundFirst.RuntimeConfiguration, err)
	}
	firstVersion := first.Version
	disabled, err := service.Execute(ctx, command.Command{
		Kind: command.SetRuntimeEnvironmentEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-disable-first", ExpectedVersion: &firstVersion},
		Payload:  command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: first.Ref, Enabled: false},
	})
	if err != nil || disabled.RuntimeEnvironment == nil || disabled.RuntimeEnvironment.State != "DISABLED" ||
		disabled.RuntimeEnvironment.Version != firstVersion+1 ||
		!reflect.DeepEqual(disabled.RuntimeEnvironment.CurrentVersion, first.CurrentVersion) {
		t.Fatalf("disable runtime environment: environment=%#v err=%v", disabled.RuntimeEnvironment, err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.SetRuntimeEnvironmentEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-enable-stale", ExpectedVersion: &firstVersion},
		Payload:  command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: first.Ref, Enabled: true},
	}); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("stale runtime environment enable was accepted: %v", err)
	}
	disabledVersion := disabled.RuntimeEnvironment.Version
	enabled, err := service.Execute(ctx, command.Command{
		Kind: command.SetRuntimeEnvironmentEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-enable-first", ExpectedVersion: &disabledVersion},
		Payload:  command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: first.Ref, Enabled: true},
	})
	if err != nil || enabled.RuntimeEnvironment == nil || enabled.RuntimeEnvironment.State != "ACTIVE" ||
		!reflect.DeepEqual(enabled.RuntimeEnvironment.CurrentVersion, first.CurrentVersion) {
		t.Fatalf("enable runtime environment: environment=%#v err=%v", enabled.RuntimeEnvironment, err)
	}
	enabledVersion := enabled.RuntimeEnvironment.Version
	disabled, err = service.Execute(ctx, command.Command{
		Kind: command.SetRuntimeEnvironmentEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-disable-for-delete", ExpectedVersion: &enabledVersion},
		Payload:  command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: first.Ref, Enabled: false},
	})
	if err != nil || disabled.RuntimeEnvironment == nil || disabled.RuntimeEnvironment.State != "DISABLED" {
		t.Fatalf("disable runtime environment for delete: environment=%#v err=%v", disabled.RuntimeEnvironment, err)
	}
	deleteVersion := disabled.RuntimeEnvironment.Version
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.DeleteRuntimeEnvironment, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-delete-bound", ExpectedVersion: &deleteVersion},
		Payload:  command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: first.Ref},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("bound runtime environment was deleted: %v", err)
	}
	boundVersion := boundFirst.RuntimeConfiguration.AgentVersion
	boundSecond, err := service.Execute(ctx, command.Command{
		Kind: command.BindAgentRuntimeEnvironment, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-bind-second", ExpectedVersion: &boundVersion},
		Payload:  command.RuntimeEnvironmentBindingInput{AgentRef: agent.Ref, EnvironmentRef: second.Ref},
	})
	if err != nil || boundSecond.RuntimeConfiguration == nil || boundSecond.RuntimeConfiguration.Environment.Ref != second.Ref ||
		!boundSecond.RuntimeConfiguration.Environment.Ready ||
		!reflect.DeepEqual(boundSecond.RuntimeConfiguration.Environment.CurrentVersion, second.CurrentVersion) {
		t.Fatalf("rebind second runtime environment: configuration=%#v err=%v", boundSecond.RuntimeConfiguration, err)
	}
	deleteCommand := command.Command{
		Kind: command.DeleteRuntimeEnvironment, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "runtime-environment-delete", ExpectedVersion: &deleteVersion},
		Payload:  command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: first.Ref},
	}
	deleted, err := service.Execute(ctx, deleteCommand)
	if err != nil || deleted.RuntimeEnvironment == nil || deleted.RuntimeEnvironment.State != "DELETED" ||
		deleted.RuntimeEnvironment.Version != deleteVersion+1 || deleted.RuntimeEnvironment.Ref != first.Ref ||
		deleted.RuntimeEnvironment.ProjectRef != disabled.RuntimeEnvironment.ProjectRef ||
		deleted.RuntimeEnvironment.Name != disabled.RuntimeEnvironment.Name ||
		deleted.RuntimeEnvironment.Description != disabled.RuntimeEnvironment.Description ||
		!reflect.DeepEqual(deleted.RuntimeEnvironment.CurrentVersion, disabled.RuntimeEnvironment.CurrentVersion) ||
		deleted.RuntimeEnvironment.UpdatedAt.Before(disabled.RuntimeEnvironment.UpdatedAt) || len(deleted.RuntimeEnvironment.NextActions) != 0 {
		t.Fatalf("delete runtime environment terminal snapshot: environment=%#v err=%v", deleted.RuntimeEnvironment, err)
	}
	replayedDelete, err := service.Execute(ctx, deleteCommand)
	if err != nil || !reflect.DeepEqual(replayedDelete.RuntimeEnvironment, deleted.RuntimeEnvironment) {
		t.Fatalf("replay runtime environment delete: replay=%#v deleted=%#v err=%v",
			replayedDelete.RuntimeEnvironment, deleted.RuntimeEnvironment, err)
	}
	wrongDeleteVersion := deleted.RuntimeEnvironment.Version
	wrongDelete := deleteCommand
	wrongDelete.Mutation.ExpectedVersion = &wrongDeleteVersion
	if replay, err := service.Execute(ctx, wrongDelete); !errors.Is(err, domainerrs.ErrNotFound) || replay.RuntimeEnvironment != nil {
		t.Fatalf("inexact runtime environment delete replay bypassed masking: replay=%#v err=%v", replay.RuntimeEnvironment, err)
	}
	if readback, err := service.GetRuntimeEnvironment(ctx, owner, first.Ref); !errors.Is(err, domainerrs.ErrNotFound) || readback.Ref != "" {
		t.Fatalf("deleted runtime environment remained get-eligible: environment=%#v err=%v", readback, err)
	}
	environments, _, err = service.ListRuntimeEnvironments(ctx, owner, query.Filter{ProjectRef: projectRef})
	if err != nil {
		t.Fatalf("list runtime environments after delete: %v", err)
	}
	for _, environment := range environments {
		if environment.Ref == first.Ref {
			t.Fatalf("deleted runtime environment remained list-eligible: %#v", environment)
		}
	}
	var state string
	var storedVersion, bindingCount, deleteAuditCount, deleteEventCount int64
	if err := pool.QueryRow(ctx, `
SELECT environment.state, environment.version,
       (SELECT count(*) FROM control_plane.agent_runtime_environment_bindings binding
        WHERE binding.environment_set_id = environment.id),
       (SELECT count(*) FROM control_plane.audit_events audit
        WHERE audit.resource_ref = environment.ref AND audit.action = 'controlplane.delete_runtime_environment'),
       (SELECT count(*) FROM control_plane.outbox_events event
        WHERE convert_from(event.payload, 'UTF8')::jsonb ->> 'eventName' = 'RUNTIME_ENVIRONMENT_CHANGED'
          AND convert_from(event.payload, 'UTF8')::jsonb ->> 'aggregateRef' = environment.ref
          AND (convert_from(event.payload, 'UTF8')::jsonb ->> 'aggregateVersion')::bigint = environment.version
          AND convert_from(event.payload, 'UTF8')::jsonb #>> '{data,state}' = 'DELETED'
          AND convert_from(event.payload, 'UTF8')::jsonb #>> '{data,safeSummary}' = 'i18n:RUNTIME_ENVIRONMENT_DELETED')
FROM control_plane.runtime_environment_sets environment
WHERE environment.ref = $1`, first.Ref).Scan(
		&state, &storedVersion, &bindingCount, &deleteAuditCount, &deleteEventCount,
	); err != nil || state != "DELETED" || storedVersion != deleted.RuntimeEnvironment.Version ||
		bindingCount != 0 || deleteAuditCount != 1 || deleteEventCount != 1 {
		t.Fatalf("runtime environment delete readback: state=%q version=%d bindings=%d audits=%d events=%d err=%v",
			state, storedVersion, bindingCount, deleteAuditCount, deleteEventCount, err)
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
	blocker, err := repository.pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin unchanged OIDC group blocker: %v", err)
	}
	if _, err := blocker.Exec(ctx, `
		UPDATE control_plane.subjects SET updated_at = updated_at WHERE id = $1::uuid
	`, owner.ActorID); err != nil {
		_ = blocker.Rollback(ctx)
		t.Fatalf("lock unchanged OIDC subject: %v", err)
	}
	fastPathContext, cancelFastPath := context.WithTimeout(ctx, 750*time.Millisecond)
	_, fastPathErr := repository.ResolveProofAuthority(fastPathContext, groupedOwner)
	cancelFastPath()
	if rollbackErr := blocker.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("release unchanged OIDC group blocker: %v", rollbackErr)
	}
	if fastPathErr != nil {
		t.Fatalf("unchanged OIDC groups waited for subject lock: %v", fastPathErr)
	}
	if _, err := repository.pool.Exec(ctx, `
		UPDATE control_plane.oidc_groups
		SET last_seen_at = clock_timestamp() - interval '25 hours'
		WHERE organization_id = $1::uuid AND display_name = $2
	`, owner.AuthorityTenant, groupedOwner.ExternalGroups[0]); err != nil {
		t.Fatalf("age synchronized OIDC group: %v", err)
	}
	if _, err := repository.ResolveProofAuthority(ctx, groupedOwner); err != nil {
		t.Fatalf("refresh unchanged stale OIDC group: %v", err)
	}
	var refreshedGroups int
	if err := repository.pool.QueryRow(ctx, `
		SELECT count(*)
		FROM control_plane.oidc_groups oidc_group
		JOIN control_plane.oidc_group_memberships membership ON membership.group_id = oidc_group.id
		WHERE oidc_group.organization_id = $1::uuid
		  AND oidc_group.display_name = $2
		  AND oidc_group.state = 'ACTIVE'
		  AND oidc_group.last_seen_at >= clock_timestamp() - interval '1 minute'
		  AND membership.subject_id = $3::uuid
		  AND membership.subject_session_revision = $4
	`, owner.AuthorityTenant, groupedOwner.ExternalGroups[0], owner.ActorID, groupedOwner.ExternalSessionRevision).Scan(&refreshedGroups); err != nil || refreshedGroups != 1 {
		t.Fatalf("refreshed unchanged OIDC group readback: groups=%d err=%v", refreshedGroups, err)
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
	for _, projectRef := range []string{"", projectA.Ref, projectB.Ref} {
		items, next, err := service.ListAgents(ctx, candidate, query.Filter{ProjectRef: projectRef, Query: "Enterprise", Page: query.Page{Size: 1}})
		expected := 1
		if projectRef == projectB.Ref {
			expected = 0
		}
		if err != nil || len(items) != expected || next != "" || expected == 1 && items[0].Ref != agentA.Ref {
			t.Fatalf("exact-grant catalog eligibility: project=%q items=%d next=%q err=%v", projectRef, len(items), next, err)
		}
	}
	page := query.Filter{Query: "Enterprise", Page: query.Page{Size: 1}}
	seen := map[string]bool{}
	for {
		items, next, err := service.ListAgents(ctx, owner, page)
		if err != nil || len(items) != 1 || seen[items[0].Ref] {
			t.Fatalf("global keyset page: items=%d err=%v", len(items), err)
		}
		seen[items[0].Ref] = true
		if next == "" {
			break
		}
		changed := page
		changed.Page.Token, changed.ProjectRef = next, projectA.Ref
		if _, _, err := service.ListAgents(ctx, owner, changed); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("cross-filter cursor accepted: %v", err)
		}
		changed.ProjectRef = ""
		if _, _, err := service.ListAgents(ctx, candidate, changed); !errors.Is(err, domainerrs.ErrInvalid) {
			t.Fatalf("cross-actor cursor accepted: %v", err)
		}
		if next == page.Page.Token {
			t.Fatal("cursor did not advance")
		}
		page.Page.Token = next
	}
	if len(seen) != 3 {
		t.Fatalf("global catalog lost rows: %d", len(seen))
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
	allowedRunVersion := allowed.Run.Version
	cancelled, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "enterprise-launch-agent-a-cleanup", ExpectedVersion: &allowedRunVersion},
		Payload:  command.RunCommandInput{RunRef: allowed.Run.Ref, Reason: "Close enterprise access component fixture"},
	})
	if err != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
		t.Fatalf("cancel enterprise access fixture: run=%#v err=%v", cancelled.Run, err)
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

func testInteractionHealthRouting(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct optional interaction service: %v", err)
	}
	definitions, _, _, err := service.ListIntegrationDefinitions(ctx, owner, query.Filter{})
	if err != nil {
		t.Fatalf("list integration definitions: %v", err)
	}
	var mattermost *entity.IntegrationDefinition
	for index := range definitions {
		if definitions[index].Key == "mattermost" {
			mattermost = &definitions[index]
			break
		}
	}
	if mattermost == nil || mattermost.AdapterOwner != "interaction-gateway" ||
		mattermost.ExecutionRoute != "INTERACTION" || mattermost.AdapterReadiness != "READY" || len(mattermost.Capabilities) != 18 {
		t.Fatalf("Mattermost routing metadata is invalid: %#v", mattermost)
	}
	connection, err := service.Execute(ctx, command.Command{
		Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "interaction-connection-create"},
		Payload: command.ConnectionInput{DefinitionKey: "mattermost", Name: "Optional customer channel", PublicConfiguration: map[string]any{
			"base_url": "https://mattermost.example.test", "team_name": "customer-success", "channel_name": "ai-results",
		}},
	})
	if err != nil || connection.Connection == nil || connection.Connection.State != "NOT_CONNECTED" {
		t.Fatalf("create Mattermost connection: %v", err)
	}
	if _, err := pool.Exec(ctx, `WITH revision AS (
INSERT INTO control_plane.integration_credential_revisions
(ref,organization_id,connection_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256,created_by)
SELECT 'mattermost_test_credential',organization_id,id,1,'kodex-system/kodex-integration-credentials#test-token',gen_random_uuid(),'1',repeat('d',64),created_by
FROM control_plane.integration_connections WHERE ref=$1 RETURNING id,connection_id)
UPDATE control_plane.integration_connections connection SET credential_revision_id=revision.id,masked_credentials_state='CONFIGURED'
FROM revision WHERE connection.id=revision.connection_id`, connection.Connection.Ref); err != nil {
		t.Fatalf("seed Mattermost credential revision: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.TestConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "interaction-connection-test", ExpectedVersion: &connection.Connection.Version},
		Payload:  command.ConnectionInput{Ref: connection.Connection.Ref}}); err != nil {
		t.Fatalf("start Mattermost health check: %v", err)
	}
	worker := func(workload, operation string) value.Principal {
		return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", CallerWorkload: workload, Operation: operation}, workload)
	}
	generic := worker("integration-gateway", "platform.runtime.integration-tests.claim")
	claims, err := service.ClaimIntegrationConnectionTests(ctx, generic, "generic-mattermost-test", 32)
	if err != nil {
		t.Fatalf("generic health claim boundary: %v", err)
	}
	for _, claim := range claims {
		if stringMap(claim, "connectionRef") == connection.Connection.Ref {
			t.Fatal("generic worker claimed interaction health check")
		}
	}
	interaction := worker("interaction-gateway", "platform.interactions.connection-tests.claim")
	claims, err = service.ClaimIntegrationConnectionTests(ctx, interaction, "interaction-mattermost-test", 32)
	if err != nil || len(claims) != 1 || stringMap(claims[0], "connectionRef") != connection.Connection.Ref {
		t.Fatalf("interaction health claim: count=%d err=%v", len(claims), err)
	}
	claim := claims[0]
	completion := command.Command{Kind: command.CompleteConnectionTest, Principal: generic,
		Mutation: value.Mutation{IdempotencyKey: "interaction-health-completion"},
		Payload:  command.IntegrationConnectionTestInput{TestRef: stringMap(claim, "testRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"), Generation: claim["generation"].(int64), Success: true}}
	if _, err := service.Execute(ctx, completion); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("generic worker completed interaction health check: %v", err)
	}
	completion.Principal = interaction
	if result, err := service.Execute(ctx, completion); err != nil || result.Connection == nil || result.Connection.State != "CONNECTED" {
		t.Fatalf("interaction health completion: %v", err)
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
	definitions, _, actions, err := service.ListIntegrationDefinitions(ctx, owner, query.Filter{})
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
	updateVersion := revoked.Connection.Version
	updated, err := service.Execute(ctx, command.Command{
		Kind: command.UpdateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-update", ExpectedVersion: &updateVersion},
		Payload: command.ConnectionInput{Ref: created.Connection.Ref, Name: "Updated synthetic journal",
			PublicConfiguration: map[string]any{"journal": "component-updated"}},
	})
	if err != nil || updated.Connection == nil || updated.Connection.Version != updateVersion+1 ||
		updated.Connection.Name != "Updated synthetic journal" || updated.Connection.PublicConfiguration["journal"] != "component-updated" ||
		len(updated.Connection.Grants) != 1 || updated.Connection.Grants[0].Enabled {
		t.Fatalf("update integration connection: connection=%#v err=%v", updated.Connection, err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.UpdateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-update-stale", ExpectedVersion: &updateVersion},
		Payload: command.ConnectionInput{Ref: created.Connection.Ref, Name: "Stale synthetic journal",
			PublicConfiguration: map[string]any{"journal": "component-stale"}},
	}); !errors.Is(err, domainerrs.ErrVersionMismatch) {
		t.Fatalf("stale integration update was accepted: %v", err)
	}
	var reenableVersion int64
	if err := pool.QueryRow(ctx, bootstrapComponentConnectIntegrationQuery, updated.Connection.Ref).Scan(&reenableVersion); err != nil {
		t.Fatalf("materialize updated integration fixture: %v", err)
	}
	reenabledGrant, err := service.Execute(ctx, command.Command{
		Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-grant-reenable", ExpectedVersion: &reenableVersion},
		Payload: command.IntegrationGrantInput{ConnectionRef: created.Connection.Ref,
			CapabilityKey: "synthetic.journal.read", AgentRef: agent.Ref, Enabled: true},
	})
	if err != nil || reenabledGrant.Connection == nil || len(reenabledGrant.Connection.Grants) != 1 || !reenabledGrant.Connection.Grants[0].Enabled {
		t.Fatalf("reenable integration grant: connection=%#v err=%v", reenabledGrant.Connection, err)
	}
	testVersion := reenabledGrant.Connection.Version
	testing, err := service.Execute(ctx, command.Command{
		Kind: command.TestConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-test-before-disable", ExpectedVersion: &testVersion},
		Payload:  command.ConnectionInput{Ref: created.Connection.Ref},
	})
	if err != nil || testing.Connection == nil || testing.Connection.State != "TESTING" {
		t.Fatalf("queue integration test before disable: connection=%#v err=%v", testing.Connection, err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.UpdateConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-update-testing", ExpectedVersion: &testing.Connection.Version},
		Payload: command.ConnectionInput{Ref: created.Connection.Ref, Name: "Testing synthetic journal",
			PublicConfiguration: map[string]any{"journal": "component-testing"}},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("testing integration connection accepted update: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{
		Kind: command.DeleteConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-delete-active", ExpectedVersion: &testing.Connection.Version},
		Payload:  command.ConnectionInput{Ref: created.Connection.Ref},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("active integration connection was deleted: %v", err)
	}
	disableVersion := testing.Connection.Version
	disabled, err := service.Execute(ctx, command.Command{
		Kind: command.SetConnectionEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-disable-for-delete", ExpectedVersion: &disableVersion},
		Payload:  command.ConnectionInput{Ref: created.Connection.Ref, Enabled: false},
	})
	if err != nil || disabled.Connection == nil || disabled.Connection.State != "DISABLED" || disabled.Connection.Enabled ||
		len(disabled.Connection.Grants) != 1 || disabled.Connection.Grants[0].Enabled {
		t.Fatalf("disable integration connection atomically: connection=%#v err=%v", disabled.Connection, err)
	}
	var activeTests, cancelledTests, enabledGrants int64
	if err := pool.QueryRow(ctx, `
SELECT count(*) FILTER (WHERE test.state IN ('DUE', 'CLAIMED')),
       count(*) FILTER (WHERE test.state = 'CANCELLED'),
       (SELECT count(*) FROM control_plane.integration_grants grant_row
        WHERE grant_row.connection_id = connection.id AND grant_row.enabled)
FROM control_plane.integration_connections connection
LEFT JOIN control_plane.integration_connection_tests test ON test.connection_id = connection.id
WHERE connection.ref = $1
GROUP BY connection.id`, created.Connection.Ref).Scan(&activeTests, &cancelledTests, &enabledGrants); err != nil ||
		activeTests != 0 || cancelledTests != 1 || enabledGrants != 0 {
		t.Fatalf("disable dependency readback: active_tests=%d cancelled_tests=%d enabled_grants=%d err=%v",
			activeTests, cancelledTests, enabledGrants, err)
	}
	deleteVersion := disabled.Connection.Version
	deleteCommand := command.Command{
		Kind: command.DeleteConnection, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-delete", ExpectedVersion: &deleteVersion},
		Payload:  command.ConnectionInput{Ref: created.Connection.Ref},
	}
	deleted, err := service.Execute(ctx, deleteCommand)
	if err != nil || deleted.Connection == nil || deleted.Connection.State != "DELETED" ||
		deleted.Connection.LifecycleState != "DELETED" || deleted.Connection.Version != deleteVersion+1 ||
		deleted.Connection.Ref != disabled.Connection.Ref || deleted.Connection.Name != disabled.Connection.Name ||
		deleted.Connection.DefinitionKey != disabled.Connection.DefinitionKey ||
		deleted.Connection.DefinitionVersion != disabled.Connection.DefinitionVersion ||
		deleted.Connection.DefinitionDigest != disabled.Connection.DefinitionDigest ||
		!reflect.DeepEqual(deleted.Connection.PublicConfiguration, disabled.Connection.PublicConfiguration) ||
		!reflect.DeepEqual(deleted.Connection.Capabilities, disabled.Connection.Capabilities) ||
		!reflect.DeepEqual(deleted.Connection.Grants, disabled.Connection.Grants) ||
		deleted.Connection.CreatedAt != disabled.Connection.CreatedAt || deleted.Connection.UpdatedAt.Before(disabled.Connection.UpdatedAt) ||
		len(deleted.Connection.NextActions) != 0 {
		t.Fatalf("delete integration terminal snapshot: connection=%#v err=%v", deleted.Connection, err)
	}
	replayedDelete, err := service.Execute(ctx, deleteCommand)
	if err != nil || !reflect.DeepEqual(replayedDelete.Connection, deleted.Connection) {
		t.Fatalf("replay integration delete: replay=%#v deleted=%#v err=%v", replayedDelete.Connection, deleted.Connection, err)
	}
	wrongDeleteVersion := deleted.Connection.Version
	wrongMutation := deleteCommand
	wrongMutation.Mutation.ExpectedVersion = &wrongDeleteVersion
	if replay, err := service.Execute(ctx, wrongMutation); !errors.Is(err, domainerrs.ErrNotFound) || replay.Connection != nil {
		t.Fatalf("inexact integration delete replay bypassed masking: replay=%#v err=%v", replay.Connection, err)
	}
	otherActorCommand := deleteCommand
	if err := pool.QueryRow(ctx, `
INSERT INTO control_plane.subjects
    (organization_id, ref, issuer, external_subject_digest, display_name)
VALUES ($1::uuid, 'usr_delete_replay_actor', 'component.test', repeat('d', 64), 'Delete replay alternate actor')
RETURNING id::text`, owner.AuthorityTenant).Scan(&otherActorCommand.Principal.ActorID); err != nil {
		t.Fatalf("create alternate integration actor identity: %v", err)
	}
	if replay, err := service.Execute(ctx, otherActorCommand); !errors.Is(err, domainerrs.ErrNotFound) || replay.Connection != nil {
		t.Fatalf("other actor received integration delete receipt: replay=%#v err=%v", replay.Connection, err)
	}
	otherTenantCommand := deleteCommand
	otherTenantCommand.Principal.AuthorityTenant = "ffffffff-ffff-4fff-8fff-ffffffffffff"
	if replay, err := service.Execute(ctx, otherTenantCommand); err == nil || replay.Connection != nil {
		t.Fatalf("other tenant received integration delete receipt: replay=%#v err=%v", replay.Connection, err)
	}
	if readback, err := service.GetIntegrationConnection(ctx, owner, created.Connection.Ref); !errors.Is(err, domainerrs.ErrNotFound) || readback.Ref != "" {
		t.Fatalf("deleted integration remained get-eligible: connection=%#v err=%v", readback, err)
	}
	connections, _, err := service.ListIntegrationConnections(ctx, owner, query.Filter{DefinitionKey: "synthetic"})
	if err != nil {
		t.Fatalf("list integrations after delete: %v", err)
	}
	for _, connection := range connections {
		if connection.Ref == created.Connection.Ref {
			t.Fatalf("deleted integration remained list-eligible: %#v", connection)
		}
	}
	var lifecycleState, operationalState string
	var connectionEnabled bool
	var storedVersion, deleteAuditCount, deleteEventCount int64
	if err := pool.QueryRow(ctx, `
SELECT connection.lifecycle_state, connection.state, connection.enabled, connection.version,
       (SELECT count(*) FROM control_plane.audit_events audit
        WHERE audit.resource_ref = connection.ref AND audit.action = 'controlplane.delete_integration_connection'),
       (SELECT count(*) FROM control_plane.outbox_events event
        WHERE convert_from(event.payload, 'UTF8')::jsonb ->> 'eventName' = 'INTEGRATION_CONNECTION_CHANGED'
          AND convert_from(event.payload, 'UTF8')::jsonb ->> 'aggregateRef' = connection.ref
          AND (convert_from(event.payload, 'UTF8')::jsonb ->> 'aggregateVersion')::bigint = connection.version
          AND convert_from(event.payload, 'UTF8')::jsonb #>> '{data,state}' = 'DELETED'
          AND convert_from(event.payload, 'UTF8')::jsonb #>> '{data,safeSummary}' = 'i18n:INTEGRATION_CONNECTION_DELETED')
FROM control_plane.integration_connections connection
WHERE connection.ref = $1`, created.Connection.Ref).Scan(
		&lifecycleState, &operationalState, &connectionEnabled, &storedVersion, &deleteAuditCount, &deleteEventCount,
	); err != nil || lifecycleState != "DELETED" || operationalState != "DISABLED" || connectionEnabled ||
		storedVersion != deleted.Connection.Version || deleteAuditCount != 1 || deleteEventCount != 1 {
		t.Fatalf("integration delete readback: lifecycle=%q state=%q enabled=%t version=%d audits=%d events=%d err=%v",
			lifecycleState, operationalState, connectionEnabled, storedVersion, deleteAuditCount, deleteEventCount, err)
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
	if err != nil || len(rejectedExecutionResult.RuntimeItems) != 1 ||
		stringMap(rejectedExecutionResult.RuntimeItems[0], "runRef") != rejectedRun.Run.Ref {
		t.Fatalf("claim rejected effect runtime: claims=%d err=%v", len(rejectedExecutionResult.RuntimeItems), err)
	}
	rejectedExecution := rejectedExecutionResult.RuntimeItems[0]
	integrationGrants, ok := rejectedExecution["integrationGrants"].([]map[string]string)
	if !ok || len(integrationGrants) != 2 ||
		integrationGrants[0]["capabilityKey"] != "synthetic.journal.read" ||
		integrationGrants[1]["capabilityKey"] != "synthetic.journal.write" {
		t.Fatalf("claimed runtime lost integration grants: %#v", rejectedExecution["integrationGrants"])
	}
	readResolved, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{
		"run_ref": stringMap(rejectedExecution, "runRef"), "node_ref": stringMap(rejectedExecution, "nodeRef"),
		"connection_ref": created.Connection.Ref, "capability_key": "synthetic.journal.read",
		"idempotency_key": "integration-effect-read-invocation",
	}, map[string]any{})
	if err != nil || stringMap(readResolved, "state") != "READY" || stringMap(readResolved, "gateRef") != "" {
		t.Fatalf("resolve read invocation without gate: result=%#v err=%v", readResolved, err)
	}
	interactionWorker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "interaction-gateway", Operation: "platform.interactions.invocations.claim",
	}, "interaction-gateway")
	if claims, err := service.ClaimIntegrationInvocations(ctx, interactionWorker, "interaction-isolation", 1); err != nil || len(claims) != 0 {
		t.Fatalf("interaction workload claimed managed invocation: %d %v", len(claims), err)
	}
	readClaims, err := service.ClaimIntegrationInvocations(ctx, gateway, "integration-gateway-component", 1)
	if err != nil || len(readClaims) != 1 || stringMap(readClaims[0], "capabilityKey") != "synthetic.journal.read" {
		t.Fatalf("claim read invocation without gate: claims=%#v err=%v", readClaims, err)
	}
	readClaim := readClaims[0]
	if _, err := service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: interactionWorker,
		Mutation: value.Mutation{IdempotencyKey: "interaction-wrong-workload-complete"},
		Payload: command.IntegrationInvocationInput{InvocationRef: stringMap(readClaim, "invocationRef"), LeaseRef: stringMap(readClaim, "leaseRef"),
			Fence: stringMap(readClaim, "fence"), Generation: readClaim["generation"].(int64)},
	}); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("interaction workload completed managed claim with disclosed fence: %v", err)
	}
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
	if err != nil || rejection.Gate == nil || rejection.Gate.State != "REJECTED" || rejection.Run == nil || rejection.Run.State != "RUNNING" {
		t.Fatalf("reject integration effect: gate=%#v run=%#v err=%v", rejection.Gate, rejection.Run, err)
	}
	if rejection.Graph == nil || graphNodeState(rejection.Graph.Nodes, "ROOT_PROCESS") != "RUNNING" {
		t.Fatalf("rejected integration effect terminated the active root graph: %#v", rejection.Graph)
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
	currentRejectedRun, err := service.GetRun(ctx, owner, rejectedRun.Run.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.CancelRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-rejected-phase-cleanup", ExpectedVersion: &currentRejectedRun.Version},
		Payload:  command.RunCommandInput{RunRef: currentRejectedRun.Ref, Reason: "Rejected effect fixture phase completed"},
	}); err != nil {
		t.Fatalf("close rejected phase before provider reuse: %v", err)
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
	if err != nil || len(approvedExecutionResult.RuntimeItems) != 1 ||
		stringMap(approvedExecutionResult.RuntimeItems[0], "runRef") != launched.Run.Ref {
		t.Fatalf("claim approved effect runtime: claims=%d err=%v", len(approvedExecutionResult.RuntimeItems), err)
	}
	execution := approvedExecutionResult.RuntimeItems[0]
	resolved, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{
		"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"),
		"connection_ref": created.Connection.Ref, "capability_key": "synthetic.journal.write",
		"idempotency_key": "integration-effect-approved-invocation",
	}, map[string]any{"value": strings.Repeat("v", 3000)})
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
	resultSummary := `{"journal":"effect-main","effect_key":"` + stringMap(claim, "effectKey") + `","sequence":1,"value":"` + strings.Repeat("v", 3000) + `","count":1}`
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
	}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("mismatched effect receipt error=%v, want invalid", err)
	}
	var receiptCount int
	if err := pool.QueryRow(ctx, bootstrapComponentEffectReceiptCountQuery, stringMap(claim, "effectKey")).Scan(&receiptCount); err != nil || receiptCount != 1 {
		t.Fatalf("effect receipt count=%d err=%v", receiptCount, err)
	}
	afterCompletion, err := service.ClaimIntegrationInvocations(ctx, gateway, "integration-gateway-component", 1)
	if err != nil || len(afterCompletion) != 0 {
		t.Fatalf("claim completed effect retry: claims=%#v err=%v", afterCompletion, err)
	}
	largeReadback, err := service.GetIntegrationInvocation(ctx, runtimeWorker, stringMap(claim, "invocationRef"))
	if err != nil || stringMap(largeReadback, "resultSummary") != resultSummary {
		t.Fatalf("typed result was truncated: %v", err)
	}
	for _, expire := range []bool{false, true} {
		key := fmt.Sprintf("integration-unknown-%t", expire)
		unknown, err := service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{
			"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"),
			"connection_ref": created.Connection.Ref, "capability_key": "synthetic.journal.write", "idempotency_key": key,
		}, map[string]any{"value": key})
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Execute(ctx, command.Command{Kind: command.ResolveOwnerGate, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key + "-approve", ExpectedVersion: &gateVersion},
			Payload:  command.GateResolutionInput{GateRef: stringMap(unknown, "gateRef"), Decision: "APPROVE"}})
		if err != nil {
			t.Fatal(err)
		}
		claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "unknown-component", 1)
		if err != nil || len(claims) != 1 {
			t.Fatalf("unknown claim: count=%d err=%v", len(claims), err)
		}
		claim := claims[0]
		if expire {
			_, err = pool.Exec(ctx, `UPDATE control_plane.integration_invocations SET lease_expires_at=clock_timestamp()-interval '1 second' WHERE ref=$1`, stringMap(claim, "invocationRef"))
		} else {
			_, err = service.Execute(ctx, command.Command{Kind: command.CompleteIntegrationInvocation, Principal: gateway,
				Mutation: value.Mutation{IdempotencyKey: key + "-complete"}, Payload: command.IntegrationInvocationInput{
					InvocationRef: stringMap(claim, "invocationRef"), LeaseRef: stringMap(claim, "leaseRef"), Fence: stringMap(claim, "fence"),
					Generation: claim["generation"].(int64), UnknownOutcome: true, SafeErrorCode: "INTEGRATION_OUTCOME_UNKNOWN",
				}})
		}
		if err != nil {
			t.Fatal(err)
		}
		for attempt := 0; attempt < 2; attempt++ {
			claims, err = service.ClaimIntegrationInvocations(ctx, gateway, "replacement-worker", 1)
			if err != nil || len(claims) != 0 {
				t.Fatalf("unknown outcome was reclaimed: %d, %v", len(claims), err)
			}
		}
		readback, err := service.GetIntegrationInvocation(ctx, runtimeWorker, stringMap(unknown, "invocationRef"))
		if err != nil || stringMap(readback, "state") != "UNKNOWN_OUTCOME" || stringMap(readback, "effectReceiptRef") != "" {
			t.Fatalf("unknown durable readback: %#v %v", readback, err)
		}
	}
	_, err = service.ResolveIntegrationInvocation(ctx, runtimeWorker, map[string]string{
		"run_ref": stringMap(execution, "runRef"), "node_ref": stringMap(execution, "nodeRef"),
		"connection_ref": created.Connection.Ref, "capability_key": "synthetic.journal.read", "idempotency_key": "integration-revoked-queued",
	}, map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	connection, err := service.GetIntegrationConnection(ctx, owner, created.Connection.Ref)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(ctx, command.Command{Kind: command.ChangeIntegrationGrant, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "integration-revoke-before-claim", ExpectedVersion: &connection.Version},
		Payload:  command.IntegrationGrantInput{ConnectionRef: connection.Ref, CapabilityKey: "synthetic.journal.read", AgentRef: agent.Ref, Enabled: false}})
	if err != nil {
		t.Fatal(err)
	}
	if claims, err := service.ClaimIntegrationInvocations(ctx, gateway, "revoked-component", 1); err != nil || len(claims) != 0 {
		t.Fatalf("revoked grant claimed: %d %v", len(claims), err)
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
		CallerWorkload: "control-api-gateway", ProjectRef: candidateAuthority.ProjectID, CredentialRevision: 1,
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
	runs, runTotal, _, err := service.ListRuns(ctx, candidate, query.Filter{ProjectRef: projectRef, Page: query.Page{Size: 20}})
	if err != nil || runTotal != 1 || len(runs) != 1 || len(runs[0].NextActions) != 1 || runs[0].NextActions[0] != "OPEN" {
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
	firstAuditPage, nextAuditPage, err := service.ListAuditEvents(ctx, owner, query.Filter{
		Query: "Readback run", Page: query.Page{Size: 1},
	})
	if err != nil || len(firstAuditPage) != 1 || nextAuditPage == "" {
		t.Fatalf("first audit cursor page is unstable: events=%#v next=%q err=%v", firstAuditPage, nextAuditPage, err)
	}
	secondAuditPage, _, err := service.ListAuditEvents(ctx, owner, query.Filter{
		Query: "Readback run", Page: query.Page{Size: 1, Token: nextAuditPage},
	})
	if err != nil || len(secondAuditPage) != 1 || secondAuditPage[0].Ref == firstAuditPage[0].Ref {
		t.Fatalf("second audit cursor page is unstable: first=%#v second=%#v err=%v", firstAuditPage, secondAuditPage, err)
	}
	if _, _, err := service.ListAuditEvents(ctx, owner, query.Filter{Page: query.Page{Size: 1, Token: "invalid"}}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("invalid audit cursor was accepted: %v", err)
	}
	hiddenAuditEvents, _, err := service.ListAuditEvents(ctx, candidate, query.Filter{Query: "Readback run", Page: query.Page{Size: 20}})
	if err != nil || len(hiddenAuditEvents) != 0 {
		t.Fatalf("audit readback ignored VIEW_AUDIT eligibility: events=%#v err=%v", hiddenAuditEvents, err)
	}
	testRunCatalogTotals(t, ctx, service, owner, candidate, projectRef, actionAgent.Ref)
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
	visibleSearch, _, _, err := service.Search(ctx, candidate, query.Filter{Query: "Readback", Limit: 20})
	if err != nil || len(visibleSearch) != 0 {
		t.Fatalf("legacy project VIEW leaked resource metadata: results=%#v err=%v", visibleSearch, err)
	}
	legacyVFS, legacyVFSTotal, _, err := service.ListVFSNodes(ctx, candidate, query.Filter{
		ProjectRef: projectRef, ResourceRef: "/projects/" + projectRef + "/entities/agents", Page: query.Page{Size: 20},
	})
	if err != nil || legacyVFSTotal != 0 || len(legacyVFS) != 0 {
		t.Fatalf("legacy project VIEW leaked VFS agent metadata: nodes=%#v total=%d err=%v", legacyVFS, legacyVFSTotal, err)
	}
	foreignSearch, _, _, err := service.Search(ctx, candidate, query.Filter{Query: "Foreign access", Limit: 20})
	if err != nil || len(foreignSearch) != 0 {
		t.Fatalf("search exposed inaccessible project: results=%#v err=%v", foreignSearch, err)
	}
	ownerSearch, _, _, err := service.Search(ctx, owner, query.Filter{Query: "Foreign access", Limit: 20})
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
	if err != nil || len(claims) != 1 || stringMap(claims[0], "inputDigest") == "" ||
		stringMap(claims[0], "scheduleRevisionRef") != created.Schedule.CurrentRevision.Ref ||
		stringMap(claims[0], "scheduleRevisionDigest") != created.Schedule.CurrentRevision.Digest ||
		claims[0]["scheduleRevision"].(int64) != created.Schedule.CurrentRevision.Revision {
		t.Fatalf("claim due schedule: claims=%#v err=%v", claims, err)
	}
	staleClaim := claims[0]
	if _, err := repository.pool.Exec(ctx, bootstrapComponentExpireScheduleClaimQuery, stringMap(staleClaim, "occurrenceRef")); err != nil {
		t.Fatalf("expire schedule claim: %v", err)
	}
	claims, err = service.ClaimDueSchedules(ctx, schedulerClaim, "scheduler-recovery-component", 1)
	if err != nil || len(claims) != 1 || stringMap(claims[0], "occurrenceRef") != stringMap(staleClaim, "occurrenceRef") ||
		claims[0]["generation"].(int64) != staleClaim["generation"].(int64)+1 || stringMap(claims[0], "leaseRef") == stringMap(staleClaim, "leaseRef") ||
		stringMap(claims[0], "scheduleRevisionRef") != stringMap(staleClaim, "scheduleRevisionRef") ||
		stringMap(claims[0], "scheduleRevisionDigest") != stringMap(staleClaim, "scheduleRevisionDigest") {
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
	if err != nil || archived.Schedule == nil || archived.Schedule.State != "ARCHIVED" || archived.Schedule.Enabled || archived.Schedule.NextRunAt != nil || !reflect.DeepEqual(archived.Schedule.NextActions, []string{"OPEN", "DELETE"}) ||
		!reflect.DeepEqual(archived.Schedule.CurrentRevision, archiveCandidate.Schedule.CurrentRevision) {
		t.Fatalf("archive schedule: schedule=%#v err=%v", archived.Schedule, err)
	}
	replayedArchive, err := service.Execute(ctx, archiveCommand)
	if err != nil || replayedArchive.Schedule == nil || replayedArchive.Schedule.Version != archived.Schedule.Version {
		t.Fatalf("replay schedule archive: schedule=%#v err=%v", replayedArchive.Schedule, err)
	}
	archivedDetail, err := service.GetSchedule(ctx, owner, archiveCandidate.Schedule.Ref)
	if err != nil || archivedDetail.State != "ARCHIVED" || archivedDetail.Target.Ref != agent.Ref || archivedDetail.Input["task"] != "Prepare an archive lifecycle summary." ||
		!reflect.DeepEqual(archivedDetail.CurrentRevision, archived.Schedule.CurrentRevision) {
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
	deleteVersion := archived.Schedule.Version
	deleteCommand := command.Command{
		Kind: command.DeleteSchedule, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-delete", ExpectedVersion: &deleteVersion},
		Payload:  command.ScheduleInput{Ref: archiveCandidate.Schedule.Ref},
	}
	deleted, err := service.Execute(ctx, deleteCommand)
	if err != nil || deleted.Schedule == nil || deleted.Schedule.State != "DELETED" || deleted.Schedule.Enabled ||
		deleted.Schedule.Version != deleteVersion+1 || deleted.Schedule.Ref != archived.Schedule.Ref ||
		deleted.Schedule.ProjectRef != archived.Schedule.ProjectRef || deleted.Schedule.Name != archived.Schedule.Name ||
		deleted.Schedule.Target.Type != archived.Schedule.Target.Type || deleted.Schedule.Target.Ref != archived.Schedule.Target.Ref ||
		deleted.Schedule.Preset != archived.Schedule.Preset || deleted.Schedule.CronExpression != archived.Schedule.CronExpression ||
		deleted.Schedule.Timezone != archived.Schedule.Timezone ||
		!reflect.DeepEqual(deleted.Schedule.Input, archived.Schedule.Input) ||
		deleted.Schedule.SessionPolicy != archived.Schedule.SessionPolicy ||
		deleted.Schedule.NotificationPolicy != archived.Schedule.NotificationPolicy ||
		!reflect.DeepEqual(deleted.Schedule.CurrentRevision, archived.Schedule.CurrentRevision) ||
		deleted.Schedule.CreatedAt != archived.Schedule.CreatedAt || deleted.Schedule.UpdatedAt.Before(archived.Schedule.UpdatedAt) ||
		deleted.Schedule.NextRunAt != nil || len(deleted.Schedule.NextActions) != 0 {
		t.Fatalf("delete schedule terminal snapshot: schedule=%#v err=%v", deleted.Schedule, err)
	}
	replayedDelete, err := service.Execute(ctx, deleteCommand)
	if err != nil || !reflect.DeepEqual(replayedDelete.Schedule, deleted.Schedule) {
		t.Fatalf("replay schedule delete: replay=%#v deleted=%#v err=%v", replayedDelete.Schedule, deleted.Schedule, err)
	}
	wrongDeleteVersion := deleted.Schedule.Version
	wrongDelete := deleteCommand
	wrongDelete.Mutation.ExpectedVersion = &wrongDeleteVersion
	if replay, err := service.Execute(ctx, wrongDelete); !errors.Is(err, domainerrs.ErrNotFound) || replay.Schedule != nil {
		t.Fatalf("inexact schedule delete replay bypassed masking: replay=%#v err=%v", replay.Schedule, err)
	}
	if readback, err := service.GetSchedule(ctx, owner, archiveCandidate.Schedule.Ref); !errors.Is(err, domainerrs.ErrNotFound) || readback.Ref != "" {
		t.Fatalf("deleted schedule remained get-eligible: schedule=%#v err=%v", readback, err)
	}
	schedules, _, err := service.ListSchedules(ctx, owner, query.Filter{ProjectRef: project.Project.Ref})
	if err != nil {
		t.Fatalf("list schedules after delete: %v", err)
	}
	for _, schedule := range schedules {
		if schedule.Ref == archiveCandidate.Schedule.Ref {
			t.Fatalf("deleted schedule remained list-eligible: %#v", schedule)
		}
	}
	var deletedLifecycle string
	var deletedEnabled, deletedNextRunCleared bool
	var storedDeleteVersion, deleteAuditCount, deleteEventCount int64
	if err := repository.pool.QueryRow(ctx, `
SELECT schedule.lifecycle_state, schedule.enabled, schedule.next_run_at IS NULL, schedule.version,
       (SELECT count(*) FROM control_plane.audit_events audit
        WHERE audit.resource_ref = schedule.ref AND audit.action = 'controlplane.delete_schedule'),
       (SELECT count(*) FROM control_plane.outbox_events event
        WHERE convert_from(event.payload, 'UTF8')::jsonb ->> 'eventName' = 'SCHEDULE_CHANGED'
          AND convert_from(event.payload, 'UTF8')::jsonb ->> 'aggregateRef' = schedule.ref
          AND (convert_from(event.payload, 'UTF8')::jsonb ->> 'aggregateVersion')::bigint = schedule.version
          AND convert_from(event.payload, 'UTF8')::jsonb #>> '{data,state}' = 'DELETED'
          AND convert_from(event.payload, 'UTF8')::jsonb #>> '{data,safeSummary}' = 'i18n:SCHEDULE_DELETED')
FROM control_plane.schedules schedule
WHERE schedule.ref = $1`, archiveCandidate.Schedule.Ref).Scan(
		&deletedLifecycle, &deletedEnabled, &deletedNextRunCleared, &storedDeleteVersion, &deleteAuditCount, &deleteEventCount,
	); err != nil || deletedLifecycle != "DELETED" || deletedEnabled || !deletedNextRunCleared ||
		storedDeleteVersion != deleted.Schedule.Version || deleteAuditCount != 1 || deleteEventCount != 1 {
		t.Fatalf("schedule delete readback: lifecycle=%q enabled=%t next_run_cleared=%t version=%d audits=%d events=%d err=%v",
			deletedLifecycle, deletedEnabled, deletedNextRunCleared, storedDeleteVersion, deleteAuditCount, deleteEventCount, err)
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

func testScheduleContractReadback(t *testing.T, ctx context.Context, repository *Repository) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.schedules.create",
	}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct schedule contract service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{
		Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-contract-project-create"},
		Payload:  command.ProjectInput{Name: "Schedule contract project", Purpose: "Verify schedule read models", Language: "en"},
	})
	if err != nil || project.Project == nil {
		t.Fatalf("create schedule contract project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "schedule-contract-agent", "Schedule contract agent")
	created, err := service.Execute(ctx, command.Command{
		Kind: command.CreateSchedule, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-contract-create"},
		Payload: command.ScheduleInput{
			ProjectRef: project.Project.Ref, Name: "Schedule contract readback",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Preset: "DAILY", TimeOfDay: "08:15",
			Timezone: "UTC", Input: map[string]any{"task": "Verify the schedule read model.", "limit": float64(10)},
			SessionPolicy: "CONTINUE_ONE", NotificationPolicy: "CONTROL_CENTER_ONLY",
		},
	})
	if err != nil || created.Schedule == nil {
		t.Fatalf("create schedule contract fixture: schedule=%#v err=%v", created.Schedule, err)
	}
	if created.Schedule.CurrentRevision.Ref == "" || created.Schedule.CurrentRevision.Revision != 1 ||
		created.Schedule.CurrentRevision.Digest == "" || created.Schedule.CurrentRevision.Target.Ref != agent.Ref {
		t.Fatalf("create schedule omitted current revision: %#v", created.Schedule)
	}
	initialDetail, err := service.GetSchedule(ctx, owner, created.Schedule.Ref)
	if err != nil {
		t.Fatalf("get schedule before continuation binding: %v", err)
	}
	assertScheduleContractReadback(t, initialDetail, created.Schedule.CurrentRevision, "")
	updateVersion := initialDetail.Version
	updated, err := service.Execute(ctx, command.Command{
		Kind: command.UpdateSchedule, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-contract-update", ExpectedVersion: &updateVersion},
		Payload: command.ScheduleInput{
			Ref: created.Schedule.Ref, Name: "Updated schedule contract readback",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}, Preset: "WEEKDAYS", TimeOfDay: "09:45",
			Timezone: "Europe/Saratov", Input: map[string]any{"task": "Verify the updated immutable revision.", "limit": float64(20)},
			SessionPolicy: "CONTINUE_ONE", NotificationPolicy: "CONTROL_CENTER_ONLY",
		},
	})
	if err != nil || updated.Schedule == nil || updated.Schedule.CurrentRevision.Revision != 2 ||
		updated.Schedule.CurrentRevision.Ref == created.Schedule.CurrentRevision.Ref ||
		updated.Schedule.CurrentRevision.Input["task"] != "Verify the updated immutable revision." {
		t.Fatalf("update schedule current revision: schedule=%#v err=%v", updated.Schedule, err)
	}
	updatedDetail, err := service.GetSchedule(ctx, owner, created.Schedule.Ref)
	if err != nil {
		t.Fatalf("get updated schedule contract readback: %v", err)
	}
	assertScheduleContractReadback(t, updatedDetail, updated.Schedule.CurrentRevision, "")

	run, err := service.Execute(ctx, command.Command{
		Kind: command.LaunchRun, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "schedule-contract-session-create"},
		Payload: command.LaunchRunInput{
			ProjectRef: project.Project.Ref, Title: "Schedule continuation session", Task: "Create a reusable session.",
			Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref},
		},
	})
	if err != nil || run.Run == nil || run.Run.SessionRef == "" {
		t.Fatalf("create schedule continuation session: run=%#v err=%v", run.Run, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cleanupCancel()
		current, cleanupErr := service.GetRun(cleanupCtx, owner, run.Run.Ref)
		if cleanupErr != nil {
			t.Errorf("read schedule continuation run during fixture cleanup: %v", cleanupErr)
			return
		}
		if current.State == "SUCCEEDED" || current.State == "FAILED" || current.State == "CANCELLED" {
			return
		}
		version := current.Version
		cancelled, cleanupErr := service.Execute(cleanupCtx, command.Command{
			Kind: command.CancelRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "schedule-contract-session-cleanup", ExpectedVersion: &version},
			Payload:  command.RunCommandInput{RunRef: current.Ref, Reason: "Component fixture cleanup"},
		})
		if cleanupErr != nil || cancelled.Run == nil || cancelled.Run.State != "CANCELLED" {
			t.Errorf("cancel schedule continuation fixture: run=%#v err=%v", cancelled.Run, cleanupErr)
		}
	})
	tag, err := repository.pool.Exec(ctx, `
UPDATE control_plane.schedules schedule
SET continue_session_id = session.id,
    updated_at = clock_timestamp()
FROM control_plane.sessions session
WHERE schedule.ref = $1
  AND session.ref = $2
  AND session.organization_id = schedule.organization_id
  AND session.project_id = schedule.project_id
`, created.Schedule.Ref, run.Run.SessionRef)
	if err != nil || tag.RowsAffected() != 1 {
		t.Fatalf("bind schedule continuation session: rows=%d err=%v", tag.RowsAffected(), err)
	}

	detail, err := service.GetSchedule(ctx, owner, created.Schedule.Ref)
	if err != nil {
		t.Fatalf("get schedule contract readback: %v", err)
	}
	assertScheduleContractReadback(t, detail, updated.Schedule.CurrentRevision, run.Run.SessionRef)
	items, _, err := service.ListSchedules(ctx, owner, query.Filter{ProjectRef: project.Project.Ref, Page: query.Page{Size: 20}})
	if err != nil {
		t.Fatalf("list schedule contract readback: %v", err)
	}
	for _, item := range items {
		if item.Ref == created.Schedule.Ref {
			assertScheduleContractReadback(t, item, detail.CurrentRevision, run.Run.SessionRef)
			return
		}
	}
	t.Fatalf("created schedule %q is absent from list readback", created.Schedule.Ref)
}

func assertScheduleContractReadback(t *testing.T, item entity.Schedule, expectedRevision entity.ScheduleRevision, expectedSessionRef string) {
	t.Helper()
	if item.ContinueSessionRef != expectedSessionRef {
		t.Fatalf("schedule continuation session = %q, want %q", item.ContinueSessionRef, expectedSessionRef)
	}
	revision := item.CurrentRevision
	if revision.Ref != expectedRevision.Ref || revision.Revision != expectedRevision.Revision ||
		revision.Digest != expectedRevision.Digest || revision.Name != expectedRevision.Name ||
		revision.Target.Type != expectedRevision.Target.Type || revision.Target.Ref != expectedRevision.Target.Ref ||
		revision.Preset != expectedRevision.Preset || revision.CronExpression != expectedRevision.CronExpression ||
		revision.Timezone != expectedRevision.Timezone || !reflect.DeepEqual(revision.Input, expectedRevision.Input) ||
		revision.SessionPolicy != expectedRevision.SessionPolicy ||
		revision.NotificationPolicy != expectedRevision.NotificationPolicy || revision.CreatedAt.IsZero() {
		t.Fatalf("schedule current revision is incomplete: got=%#v want=%#v", revision, expectedRevision)
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
	waiting := claimAndCompleteRun(t, ctx, service, worker, launched.Run.Ref, "gate-review", false)
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
	changeWaiting := claimAndCompleteRun(t, ctx, service, worker, changeRun.Run.Ref, "gate-change-review", false)
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
	reworked := claimAndCompleteRun(t, ctx, service, worker, changes.Run.Ref, "gate-change-rework", false)
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
	testOwnerGateList(t, ctx, service, owner, project.Project.Ref)
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
	var regularChildLease, gatedChildLease map[string]any
	for _, lease := range claimedChildren.RuntimeItems {
		childSession := stringMap(lease, "sessionRef")
		if childSession == "" || childSession == stringMap(coordinatorLease, "sessionRef") {
			t.Fatalf("child execution reused the parent FIFO session: child=%q parent=%q", childSession, stringMap(coordinatorLease, "sessionRef"))
		}
		childSessions[childSession] = struct{}{}
		switch stringMap(lease, "agentRef") {
		case firstChild.Ref:
			regularChildLease = lease
		case secondChild.Ref:
			gatedChildLease = lease
		}
	}
	if len(childSessions) != 2 || regularChildLease == nil || gatedChildLease == nil {
		t.Fatalf("parallel children did not receive distinct attributable sessions: sessions=%#v claims=%#v", childSessions, claimedChildren.RuntimeItems)
	}
	regularPrompt, regularOK := regularChildLease["promptSnapshot"].(entity.PromptMaterializationSnapshot)
	gatedPrompt, gatedOK := gatedChildLease["promptSnapshot"].(entity.PromptMaterializationSnapshot)
	if !regularOK || !gatedOK || regularPrompt.HumanGateCapabilities != nil ||
		gatedPrompt.HumanGateCapabilities == nil || len(gatedPrompt.HumanGateCapabilities) != 0 {
		t.Fatalf("claim did not preserve exact Human Gate authority layers: regular=%#v gated=%#v", regularPrompt, gatedPrompt)
	}
	coordinatorCompleted := completeClaimedExecution(t, ctx, service, worker, coordinatorLease, "delegation-coordinator", false)
	if coordinatorCompleted.Run == nil || coordinatorCompleted.Run.State != "RUNNING" || coordinatorCompleted.Graph == nil {
		t.Fatalf("coordinator completion before callbacks changed the run incorrectly: run=%#v graph=%#v", coordinatorCompleted.Run, coordinatorCompleted.Graph)
	}
	gatedChild := completeClaimedExecution(t, ctx, service, worker, gatedChildLease, "delegation-child-gated", false)
	if gatedChild.Run == nil || gatedChild.Run.State != "SUCCEEDED" || len(gatedChild.Run.GateRefs) != 1 || gatedChild.Run.Usage != turnUsageFixture() {
		t.Fatalf("gated child completion did not open the owner gate: %#v", gatedChild.Run)
	}
	gatedRoot, err := service.GetRun(ctx, owner, launched.Run.Ref)
	if err != nil || gatedRoot.State != "WAITING_HUMAN" || len(gatedRoot.GateRefs) != 1 {
		t.Fatalf("gated child completion did not block the root run: run=%#v err=%v", gatedRoot, err)
	}
	regularChild := completeClaimedExecution(t, ctx, service, worker, regularChildLease, "delegation-child-regular", false)
	if regularChild.Run == nil || regularChild.Run.Usage != turnUsageFixture() {
		t.Fatalf("regular child completion usage = %#v", regularChild.Run)
	}
	waitingForOwner, err := service.GetRun(ctx, owner, launched.Run.Ref)
	if err != nil || waitingForOwner.State != "WAITING_HUMAN" || len(waitingForOwner.GateRefs) != 1 {
		t.Fatalf("human-gated delegated step did not open exactly one owner gate: run=%#v err=%v", waitingForOwner, err)
	}
	if regularChild.Graph == nil {
		t.Fatal("regular child completion did not return the authoritative graph")
	}
	preApprovalContinuationEdges := 0
	preApprovalContinuationState := ""
	for _, edge := range regularChild.Graph.Edges {
		if edge.Type != "CONTINUES" {
			continue
		}
		preApprovalContinuationEdges++
		for _, node := range regularChild.Graph.Nodes {
			if node.Ref == edge.TargetNodeRef {
				preApprovalContinuationState = node.State
			}
		}
	}
	if preApprovalContinuationEdges != 1 || preApprovalContinuationState != "QUEUED" {
		t.Fatalf("callback continuation was not queued behind the owner gate: edges=%#v nodes=%#v", regularChild.Graph.Edges, regularChild.Graph.Nodes)
	}
	blockedClaim, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "delegation-continuation-blocked-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(blockedClaim.RuntimeItems) != 0 {
		t.Fatalf("continuation became claimable before owner approval: claims=%#v err=%v", blockedClaim.RuntimeItems, err)
	}
	for index, item := range []struct {
		lease map[string]any
		key   string
	}{
		{lease: gatedChildLease, key: "delegation-child-gated"},
		{lease: regularChildLease, key: "delegation-child-regular"},
	} {
		replayed := completeClaimedExecution(t, ctx, service, worker, item.lease, item.key, false)
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

func testProviderCredentialRefreshAndCapacity(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct provider refresh service: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.provider_accounts SET max_concurrent_executions = 1`); err != nil {
		t.Fatalf("configure serialized provider account: %v", err)
	}
	defer func() {
		if _, restoreErr := pool.Exec(context.WithoutCancel(ctx), `UPDATE control_plane.provider_accounts SET max_concurrent_executions = 32`); restoreErr != nil {
			t.Errorf("restore provider account capacity: %v", restoreErr)
		}
	}()

	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-refresh-project"}, Payload: command.ProjectInput{
			Name: "Provider refresh", Purpose: "Verify serialized OAuth credential refresh", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create provider refresh project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "provider-refresh-agent", "Provider refresh specialist")
	for index := 1; index <= 2; index++ {
		launched, launchErr := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: "provider-refresh-launch-" + leftPad(index, 2)},
			Payload: command.LaunchRunInput{ProjectRef: project.Project.Ref, Title: "Provider refresh run",
				Task: "Verify provider refresh serialization.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref}},
		})
		if launchErr != nil || launched.Run == nil {
			t.Fatalf("launch provider refresh run %d: run=%#v err=%v", index, launched.Run, launchErr)
		}
	}
	type claimResult struct {
		result command.Result
		err    error
	}
	start := make(chan struct{})
	results := make(chan claimResult, 2)
	for _, key := range []string{"provider-refresh-claim-a", "provider-refresh-claim-b"} {
		key := key
		go func() {
			<-start
			result, claimErr := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
				Mutation: value.Mutation{IdempotencyKey: key}, Payload: command.LeaseInput{WorkloadInstance: key, Limit: 1}})
			results <- claimResult{result: result, err: claimErr}
		}()
	}
	close(start)
	var firstLease map[string]any
	claimedCount := 0
	for range 2 {
		claim := <-results
		if claim.err != nil {
			t.Fatalf("concurrent provider claim failed: %v", claim.err)
		}
		claimedCount += len(claim.result.RuntimeItems)
		if len(claim.result.RuntimeItems) == 1 {
			firstLease = claim.result.RuntimeItems[0]
		}
	}
	if claimedCount != 1 || firstLease == nil {
		t.Fatalf("serialized provider account produced %d concurrent claims", claimedCount)
	}
	revokeOwner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.revoke",
	}, "control-api-gateway")
	var claimedAccountVersion int64
	if err := pool.QueryRow(ctx, `
		SELECT version FROM control_plane.provider_accounts WHERE ref = $1
	`, stringMap(firstLease, "providerAccountRef")).Scan(&claimedAccountVersion); err != nil {
		t.Fatalf("read claimed provider account version: %v", err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeProviderAccount, Principal: revokeOwner,
		Mutation: value.Mutation{IdempotencyKey: "provider-refresh-active-lease-revoke", ExpectedVersion: &claimedAccountVersion},
		Payload:  command.ProviderAccountInput{AccountRef: stringMap(firstLease, "providerAccountRef")},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("active runtime lease did not block provider revoke: %v", err)
	}

	refresh := command.ProviderCredentialRefreshInput{
		LeaseRef: stringMap(firstLease, "leaseRef"), Fence: stringMap(firstLease, "fence"), Generation: firstLease["generation"].(int64),
		PreviousCredentialRevisionRef: stringMap(firstLease, "providerCredentialRevisionRef"),
		PreviousContentSHA256:         stringMap(firstLease, "providerCredentialSHA256"),
		SecretName:                    "runtime-provider-refresh-component-a", SecretUID: "50000000-0000-4000-8000-000000000001",
		SecretResourceVersion: "refresh-1", ContentSHA256: strings.Repeat("a", 64),
	}
	if refresh.PreviousContentSHA256 == refresh.ContentSHA256 {
		refresh.ContentSHA256 = strings.Repeat("b", 64)
	}
	committed, err := service.Execute(ctx, command.Command{Kind: command.CommitProviderCredentialRefresh, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-refresh-commit"}, Payload: refresh})
	if err != nil || stringMap(committed.Runtime, "providerCredentialRevisionRef") == "" ||
		stringMap(committed.Runtime, "providerCredentialSHA256") != refresh.ContentSHA256 {
		t.Fatalf("commit provider credential refresh: binding=%#v err=%v", committed.Runtime, err)
	}
	committedRef := stringMap(committed.Runtime, "providerCredentialRevisionRef")
	var retainedState, retainedSecretName, retainedSecretUID, retainedSecretVersion, retainedDigest string
	var retainedAt time.Time
	if err := pool.QueryRow(ctx, `
		SELECT task.state, task.eligible_at, task.secret_name, task.secret_uid::text,
		       task.secret_resource_version, task.content_sha256
		FROM control_plane.provider_credential_cleanup_tasks task
		JOIN control_plane.provider_credential_revisions revision
		  ON revision.id = task.provider_credential_revision_id
		WHERE revision.ref = $1
	`, refresh.PreviousCredentialRevisionRef).Scan(&retainedState, &retainedAt, &retainedSecretName,
		&retainedSecretUID, &retainedSecretVersion, &retainedDigest); err != nil {
		t.Fatalf("read superseded provider credential cleanup retention: %v", err)
	}
	if retainedState != "PENDING" || retainedAt.Before(time.Now().UTC().Add(23*time.Hour)) ||
		retainedAt.After(time.Now().UTC().Add(25*time.Hour)) ||
		retainedSecretName != stringMap(firstLease, "providerSecretName") ||
		retainedSecretUID != stringMap(firstLease, "providerSecretUID") ||
		retainedSecretVersion != stringMap(firstLease, "providerSecretResourceVersion") ||
		retainedDigest != refresh.PreviousContentSHA256 {
		t.Fatalf("superseded provider cleanup snapshot/retention mismatch: state=%s eligible=%s descriptor=%s/%s/%s/%s",
			retainedState, retainedAt, retainedSecretName, retainedSecretUID, retainedSecretVersion, retainedDigest)
	}
	repeated, err := service.Execute(ctx, command.Command{Kind: command.CommitProviderCredentialRefresh, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-refresh-repeat"}, Payload: refresh})
	if err != nil || stringMap(repeated.Runtime, "providerCredentialRevisionRef") != committedRef {
		t.Fatalf("repeat provider credential refresh: binding=%#v err=%v", repeated.Runtime, err)
	}
	var exactRevisionCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM control_plane.provider_credential_revisions
		WHERE ref = $1 AND secret_uid = $2::uuid AND secret_resource_version = $3 AND content_sha256 = $4
	`, committedRef, refresh.SecretUID, refresh.SecretResourceVersion, refresh.ContentSHA256).Scan(&exactRevisionCount); err != nil || exactRevisionCount != 1 {
		t.Fatalf("provider credential refresh was not immutable and idempotent: count=%d err=%v", exactRevisionCount, err)
	}
	late := refresh
	late.SecretUID = "50000000-0000-4000-8000-000000000002"
	late.SecretResourceVersion = "refresh-2"
	late.ContentSHA256 = strings.Repeat("c", 64)
	if _, err := service.Execute(ctx, command.Command{Kind: command.CommitProviderCredentialRefresh, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-refresh-late"}, Payload: late}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("late provider credential callback was not rejected: %v", err)
	}

	completeClaimedExecution(t, ctx, service, worker, firstLease, "provider-refresh-first", false)
	seedObservedCatalogFixture(t, ctx, repository)
	second, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "provider-refresh-claim-after-release"},
		Payload:  command.LeaseInput{WorkloadInstance: "provider-refresh-after-release", Limit: 1}})
	if err != nil || len(second.RuntimeItems) != 1 ||
		stringMap(second.RuntimeItems[0], "providerCredentialRevisionRef") != committedRef {
		t.Fatalf("claim after provider capacity release: claims=%#v err=%v", second.RuntimeItems, err)
	}
	completeClaimedExecution(t, ctx, service, worker, second.RuntimeItems[0], "provider-refresh-second", false)
	var providerAccountID, providerOrganizationID string
	if err := pool.QueryRow(ctx, `
		SELECT id::text, organization_id::text
		FROM control_plane.provider_accounts
		WHERE ref = $1
	`, stringMap(firstLease, "providerAccountRef")).Scan(&providerAccountID, &providerOrganizationID); err != nil {
		t.Fatalf("read provider cleanup guard scope: %v", err)
	}
	var activeRuntimeLease, activeWarmConsumer bool
	if err := pool.QueryRow(ctx, queryProviderAccountsCleanupGuard, pgx.StrictNamedArgs{
		"organization_id": providerOrganizationID, "account_id": providerAccountID,
	}).Scan(&activeRuntimeLease, &activeWarmConsumer); err != nil {
		t.Fatalf("read provider cleanup guard after terminal lease: %v", err)
	}
	if activeRuntimeLease || activeWarmConsumer {
		t.Fatalf("historical runtime revision blocked provider cleanup: lease=%v warm=%v",
			activeRuntimeLease, activeWarmConsumer)
	}
}

func testProviderCredentialCleanupLifecycle(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct provider cleanup service: %v", err)
	}
	revokeOwner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.revoke",
	}, "control-api-gateway")

	var defaultAccountID, defaultOrganizationID, defaultAccountRef string
	var defaultAccountVersion int64
	if err := pool.QueryRow(ctx, `
		SELECT id::text, organization_id::text, ref, version
		FROM control_plane.provider_accounts
		WHERE stable_key = 'default-openai-codex'
	`).Scan(&defaultAccountID, &defaultOrganizationID, &defaultAccountRef, &defaultAccountVersion); err != nil {
		t.Fatalf("read default provider account for cleanup guards: %v", err)
	}
	var originalWarmInstance *string
	var originalHeartbeat *time.Time
	var originalRuntimeState string
	if err := pool.QueryRow(ctx, `
		SELECT warm_instance_ref, last_heartbeat_at, runtime_state
		FROM control_plane.assistant_runtime
		WHERE organization_id = $1::uuid
	`, defaultOrganizationID).Scan(&originalWarmInstance, &originalHeartbeat, &originalRuntimeState); err != nil {
		t.Fatalf("read warm provider consumer: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.assistant_runtime
		SET warm_instance_ref = 'provider-cleanup-warm-consumer', runtime_state = 'READY',
		    last_heartbeat_at = clock_timestamp(), updated_at = clock_timestamp()
		WHERE organization_id = $1::uuid
	`, defaultOrganizationID); err != nil {
		t.Fatalf("activate warm provider consumer fixture: %v", err)
	}
	var warmAccountRef string
	var warmAccountVersion int64
	if err := pool.QueryRow(ctx, queryCatalogFixtureWarmAccount, defaultOrganizationID).Scan(&warmAccountRef, &warmAccountVersion); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.RevokeProviderAccount, Principal: revokeOwner,
		Mutation: value.Mutation{IdempotencyKey: "provider-cleanup-warm-block", ExpectedVersion: &warmAccountVersion},
		Payload:  command.ProviderAccountInput{AccountRef: warmAccountRef},
	}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("active warm consumer did not block provider revoke: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.assistant_runtime
		SET warm_instance_ref = $2, last_heartbeat_at = $3, runtime_state = $4,
		    updated_at = clock_timestamp()
		WHERE organization_id = $1::uuid
	`, defaultOrganizationID, originalWarmInstance, originalHeartbeat, originalRuntimeState); err != nil {
		t.Fatalf("restore warm provider consumer fixture: %v", err)
	}

	var reusableLeaseID string
	if err := pool.QueryRow(ctx, `
		SELECT lease.id::text
		FROM control_plane.runtime_leases lease
		JOIN control_plane.runtime_revisions revision ON revision.id = lease.runtime_revision_id
		WHERE revision.provider_account_id = $1::uuid AND lease.state = 'COMPLETED'
		ORDER BY lease.updated_at DESC
		LIMIT 1
	`, defaultAccountID).Scan(&reusableLeaseID); err != nil {
		t.Fatalf("read terminal provider lease for race fixture: %v", err)
	}
	claimTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin provider claim/revoke race: %v", err)
	}
	var maximumConcurrent int64
	if err := claimTx.QueryRow(ctx, queryRuntimeClaimexecutionLockProviderAccount,
		defaultAccountID, defaultOrganizationID).Scan(&maximumConcurrent); err != nil {
		_ = claimTx.Rollback(ctx)
		t.Fatalf("lock provider account as runtime claim winner: %v", err)
	}
	if _, err := claimTx.Exec(ctx, `
		UPDATE control_plane.runtime_leases
		SET state = 'CLAIMED', expires_at = clock_timestamp() + interval '1 minute',
		    updated_at = clock_timestamp()
		WHERE id = $1::uuid
	`, reusableLeaseID); err != nil {
		_ = claimTx.Rollback(ctx)
		t.Fatalf("materialize provider race lease: %v", err)
	}
	raced := make(chan error, 1)
	go func(version int64) {
		_, revokeErr := service.Execute(ctx, command.Command{Kind: command.RevokeProviderAccount, Principal: revokeOwner,
			Mutation: value.Mutation{IdempotencyKey: "provider-cleanup-claim-race", ExpectedVersion: &version},
			Payload:  command.ProviderAccountInput{AccountRef: defaultAccountRef},
		})
		raced <- revokeErr
	}(defaultAccountVersion)
	time.Sleep(25 * time.Millisecond)
	if err := claimTx.Commit(ctx); err != nil {
		t.Fatalf("commit provider claim/revoke race winner: %v", err)
	}
	if err := <-raced; !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("claim/revoke race did not fail closed: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.runtime_leases
		SET state = 'COMPLETED', updated_at = clock_timestamp()
		WHERE id = $1::uuid
	`, reusableLeaseID); err != nil {
		t.Fatalf("restore provider race lease: %v", err)
	}

	const accountRef = "pacc_cleanup_component"
	var accountID, organizationID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO control_plane.provider_accounts (
		    ref, organization_id, definition_key, stable_key, name,
		    state, enabled, created_by
		)
		SELECT $1, source.organization_id, source.definition_key,
		       'component-cleanup', 'Component cleanup account',
		       'REAUTHORIZATION_REQUIRED', false, source.created_by
		FROM control_plane.provider_accounts source
		WHERE source.stable_key = 'default-openai-codex'
		RETURNING id::text, organization_id::text
	`, accountRef).Scan(&accountID, &organizationID); err != nil {
		t.Fatalf("create provider cleanup account: %v", err)
	}
	const firstCredentialRef = "pcr_cleanup_component_1"
	firstDescriptor := entity.ProviderCredentialDescriptor{
		SecretName: "runtime-provider-cleanup-component-1",
		SecretUID:  "61000000-0000-4000-8000-000000000001", SecretResourceVersion: "cleanup-1",
		ContentSHA256: strings.Repeat("6", 64),
	}
	var firstCredentialID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO control_plane.provider_credential_revisions (
		    ref, organization_id, provider_account_id, revision_number,
		    secret_name, secret_uid, secret_resource_version, content_sha256, observed_at
		) VALUES ($1, $2::uuid, $3::uuid, 1, $4, $5::uuid, $6, $7, clock_timestamp())
		RETURNING id::text
	`, firstCredentialRef, organizationID, accountID, firstDescriptor.SecretName, firstDescriptor.SecretUID,
		firstDescriptor.SecretResourceVersion, firstDescriptor.ContentSHA256).Scan(&firstCredentialID); err != nil {
		t.Fatalf("create first provider cleanup credential: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.provider_accounts
		SET current_credential_revision_id = $2::uuid, version = version + 1,
		    updated_at = clock_timestamp()
		WHERE id = $1::uuid
	`, accountID, firstCredentialID); err != nil {
		t.Fatalf("activate first provider cleanup credential: %v", err)
	}
	var accountVersion int64
	if err := pool.QueryRow(ctx, `SELECT version FROM control_plane.provider_accounts WHERE id = $1::uuid`, accountID).Scan(&accountVersion); err != nil {
		t.Fatalf("read provider cleanup account version: %v", err)
	}
	authorizeOwner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.api-key-authorize",
	}, "control-api-gateway")
	secondDescriptor := entity.ProviderCredentialDescriptor{
		SecretName: "runtime-provider-cleanup-component-2",
		SecretUID:  "61000000-0000-4000-8000-000000000002", SecretResourceVersion: "cleanup-2",
		ContentSHA256: strings.Repeat("7", 64),
	}
	authorized, err := service.Execute(ctx, command.Command{Kind: command.AuthorizeProviderAPIKey, Principal: authorizeOwner,
		Mutation: value.Mutation{IdempotencyKey: "provider-cleanup-activate", ExpectedVersion: &accountVersion},
		Payload: command.ProviderAccountInput{AccountRef: accountRef, AuthorizationRef: "pauth_cleanup_component",
			AuthorizationMethod: "API_KEY", AuthorizationState: "AUTHORIZED",
			ExternalAccountMasked: "Cleanup account", Credential: &secondDescriptor},
	})
	if err != nil || authorized.ProviderAccount == nil {
		t.Fatalf("activate provider cleanup revision: account=%#v err=%v", authorized.ProviderAccount, err)
	}
	var retainedAt time.Time
	var retainedDescriptor entity.ProviderCredentialDescriptor
	if err := pool.QueryRow(ctx, `
		SELECT task.eligible_at, task.secret_name, task.secret_uid::text,
		       task.secret_resource_version, task.content_sha256
		FROM control_plane.provider_credential_cleanup_tasks task
		WHERE task.provider_credential_revision_id = $1::uuid
	`, firstCredentialID).Scan(&retainedAt, &retainedDescriptor.SecretName, &retainedDescriptor.SecretUID,
		&retainedDescriptor.SecretResourceVersion, &retainedDescriptor.ContentSHA256); err != nil {
		t.Fatalf("read successful activation cleanup retention: %v", err)
	}
	if retainedDescriptor != firstDescriptor || retainedAt.Before(time.Now().UTC().Add(23*time.Hour)) ||
		retainedAt.After(time.Now().UTC().Add(25*time.Hour)) {
		t.Fatalf("successful activation cleanup mismatch: eligible=%s descriptor=%#v", retainedAt, retainedDescriptor)
	}

	accountVersion = authorized.ProviderAccount.Version
	deleteOwner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.delete",
	}, "control-api-gateway")
	revoked, err := service.Execute(ctx, command.Command{Kind: command.DeleteProviderAccount, Principal: deleteOwner,
		Mutation: value.Mutation{IdempotencyKey: "provider-cleanup-delete-api-key", ExpectedVersion: &accountVersion},
		Payload:  command.ProviderAccountInput{AccountRef: accountRef},
	})
	if err != nil || revoked.ProviderAccount == nil || revoked.ProviderAccount.State != "REVOKED" {
		t.Fatalf("delete API-key provider cleanup account: account=%#v err=%v", revoked.ProviderAccount, err)
	}
	var scheduledCount, acceleratedCount int
	if err := pool.QueryRow(ctx, `
		SELECT count(*)::int,
		       count(*) FILTER (WHERE eligible_at <= clock_timestamp())::int
		FROM control_plane.provider_credential_cleanup_tasks
		WHERE provider_account_id = $1::uuid
	`, accountID).Scan(&scheduledCount, &acceleratedCount); err != nil {
		t.Fatalf("read deleted provider cleanup tasks: %v", err)
	}
	if scheduledCount != 2 || acceleratedCount != 2 {
		t.Fatalf("delete cleanup schedule = %d/%d, want 2/2", scheduledCount, acceleratedCount)
	}

	claimed, err := repository.ClaimProviderCredentialCleanupTasks(ctx, "provider-cleanup-component", 2)
	if err != nil || len(claimed) != 2 {
		t.Fatalf("claim provider cleanup tasks: tasks=%#v err=%v", claimed, err)
	}
	claimedByName := make(map[string]platformrepo.ProviderCredentialCleanupTask, len(claimed))
	for _, task := range claimed {
		claimedByName[task.Credential.SecretName] = task
	}
	firstTask := claimedByName[firstDescriptor.SecretName]
	secondTask := claimedByName[secondDescriptor.SecretName]
	if firstTask.Ref == "" || secondTask.Ref == "" || firstTask.Credential != firstDescriptor ||
		secondTask.Credential != secondDescriptor || firstTask.Generation != 1 || secondTask.Generation != 1 {
		t.Fatalf("claimed cleanup lost exact descriptors or generations: %#v", claimed)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.provider_credential_cleanup_tasks
		SET lease_expires_at = clock_timestamp() - interval '1 second'
		WHERE ref = $1
	`, secondTask.Ref); err != nil {
		t.Fatalf("expire provider cleanup claim: %v", err)
	}
	reclaimed, err := repository.ClaimProviderCredentialCleanupTasks(ctx, "provider-cleanup-component", 1)
	if err != nil || len(reclaimed) != 1 || reclaimed[0].Ref != secondTask.Ref ||
		reclaimed[0].Generation != 2 || reclaimed[0].Attempt != 2 || reclaimed[0].Credential != secondDescriptor {
		t.Fatalf("reclaim expired provider cleanup task: task=%#v err=%v", reclaimed, err)
	}
	if _, err := repository.CompleteProviderCredentialCleanupTask(ctx, secondTask.Ref,
		"provider-cleanup-component", secondTask.Generation, "cleanup-stale-receipt"); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("stale provider cleanup completion was not fenced: %v", err)
	}
	const cleanupReceipt = "provider-cleanup-component-receipt"
	completed, err := repository.CompleteProviderCredentialCleanupTask(ctx, secondTask.Ref,
		"provider-cleanup-component", reclaimed[0].Generation, cleanupReceipt)
	if err != nil || completed.State != "COMPLETED" || completed.TerminalReceipt != cleanupReceipt {
		t.Fatalf("complete provider cleanup task: result=%#v err=%v", completed, err)
	}
	repeatedComplete, err := repository.CompleteProviderCredentialCleanupTask(ctx, secondTask.Ref,
		"provider-cleanup-component", reclaimed[0].Generation, cleanupReceipt)
	if err != nil || repeatedComplete != completed {
		t.Fatalf("repeat provider cleanup completion: result=%#v err=%v", repeatedComplete, err)
	}

	failed, err := repository.FailProviderCredentialCleanupTask(ctx, firstTask.Ref,
		"provider-cleanup-component", firstTask.Generation, "PROVIDER_CREDENTIAL_CLEANUP_UNAVAILABLE")
	if err != nil || failed.State != "PENDING" || !failed.RetryScheduled {
		t.Fatalf("fail provider cleanup task: result=%#v err=%v", failed, err)
	}
	repeatedFail, err := repository.FailProviderCredentialCleanupTask(ctx, firstTask.Ref,
		"provider-cleanup-component", firstTask.Generation, "PROVIDER_CREDENTIAL_CLEANUP_UNAVAILABLE")
	if err != nil || repeatedFail != failed {
		t.Fatalf("repeat provider cleanup failure: result=%#v err=%v", repeatedFail, err)
	}
	lastGeneration := firstTask.Generation
	for attempt := int32(2); attempt <= providerCredentialCleanupMaxAttempts; attempt++ {
		if _, err := pool.Exec(ctx, `
			UPDATE control_plane.provider_credential_cleanup_tasks
			SET eligible_at = clock_timestamp() - interval '1 second'
			WHERE ref = $1
		`, firstTask.Ref); err != nil {
			t.Fatalf("make provider cleanup retry %d eligible: %v", attempt, err)
		}
		retry, claimErr := repository.ClaimProviderCredentialCleanupTasks(ctx, "provider-cleanup-component", 1)
		if claimErr != nil || len(retry) != 1 || retry[0].Ref != firstTask.Ref || retry[0].Attempt != attempt ||
			retry[0].Generation <= lastGeneration || retry[0].Credential != firstDescriptor {
			t.Fatalf("claim provider cleanup retry %d: task=%#v err=%v", attempt, retry, claimErr)
		}
		lastGeneration = retry[0].Generation
		failed, err = repository.FailProviderCredentialCleanupTask(ctx, firstTask.Ref,
			"provider-cleanup-component", retry[0].Generation, "PROVIDER_CREDENTIAL_CLEANUP_UNAVAILABLE")
		if err != nil {
			t.Fatalf("fail provider cleanup retry %d: %v", attempt, err)
		}
	}
	if failed.State != "DEAD_LETTER" || failed.RetryScheduled || failed.TerminalReceipt == "" {
		t.Fatalf("provider cleanup did not enter dead letter: %#v", failed)
	}
	repeatedDeadLetter, err := repository.FailProviderCredentialCleanupTask(ctx, firstTask.Ref,
		"provider-cleanup-component", lastGeneration, "PROVIDER_CREDENTIAL_CLEANUP_UNAVAILABLE")
	if err != nil || repeatedDeadLetter != failed {
		t.Fatalf("repeat provider cleanup dead letter: result=%#v err=%v", repeatedDeadLetter, err)
	}
}

func testProviderAuthRejectionLifecycle(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.runs.launch",
	}, "control-api-gateway")
	worker := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation",
		CallerWorkload: "runtime-controller", Operation: "platform.runtime.execution.claim",
	}, "runtime-controller")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatalf("construct provider rejection service: %v", err)
	}
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "provider-rejection-project"}, Payload: command.ProjectInput{
			Name: "Provider rejection", Purpose: "Verify exact provider credential failure isolation", Language: "en",
		}})
	if err != nil || project.Project == nil {
		t.Fatalf("create provider rejection project: project=%#v err=%v", project.Project, err)
	}
	agent := createLifecycleAgent(t, ctx, service, owner, project.Project.Ref, "provider-rejection-agent", "Provider rejection specialist")

	launchAndClaim := func(key string) map[string]any {
		t.Helper()
		launched, launchErr := service.Execute(ctx, command.Command{Kind: command.LaunchRun, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: key + "-launch"}, Payload: command.LaunchRunInput{
				ProjectRef: project.Project.Ref, Title: "Provider rejection " + key,
				Task: "Verify provider credential isolation.", Target: entity.RunTarget{Type: "AGENT", Ref: agent.Ref},
			}})
		if launchErr != nil || launched.Run == nil {
			t.Fatalf("launch %s provider rejection run: run=%#v err=%v", key, launched.Run, launchErr)
		}
		claimed, claimErr := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: key + "-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-provider-rejection", Limit: 1}})
		if claimErr != nil || len(claimed.RuntimeItems) != 1 || stringMap(claimed.RuntimeItems[0], "runRef") != launched.Run.Ref {
			t.Fatalf("claim %s provider rejection run: claims=%#v err=%v", key, claimed.RuntimeItems, claimErr)
		}
		return claimed.RuntimeItems[0]
	}
	completeRejected := func(key string, lease map[string]any) {
		t.Helper()
		completed, completeErr := service.Execute(ctx, command.Command{Kind: command.CompleteExecution, Principal: worker,
			Mutation: value.Mutation{IdempotencyKey: key + "-complete"}, Payload: command.CompleteExecutionInput{
				LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
				Success: false, SafeErrorCode: "PROVIDER_AUTH_REJECTED", ResultSummary: "Provider authentication was rejected.",
				Usage: turnUsageFixture(),
			}})
		if completeErr != nil || completed.Run == nil || completed.Run.State != "FAILED" {
			t.Fatalf("complete %s rejected provider run: run=%#v err=%v", key, completed.Run, completeErr)
		}
	}
	providerForRevision := func(runtimeRevisionRef string) (string, string) {
		t.Helper()
		var accountID, credentialID string
		if readErr := pool.QueryRow(ctx, bootstrapComponentRuntimeProviderReadbackQuery, runtimeRevisionRef).Scan(&accountID, &credentialID); readErr != nil {
			t.Fatalf("read runtime provider revision: %v", readErr)
		}
		return accountID, credentialID
	}
	accountState := func(accountID string) (string, string, int64) {
		t.Helper()
		var state string
		var credentialID *string
		var version int64
		if readErr := pool.QueryRow(ctx, bootstrapComponentProviderAccountReadbackQuery, accountID).Scan(&state, &credentialID, &version); readErr != nil {
			t.Fatalf("read provider account state: %v", readErr)
		}
		if credentialID == nil {
			return state, "", version
		}
		return state, *credentialID, version
	}
	rotate := func(accountID, secretUID, resourceVersion, digest string) string {
		t.Helper()
		var credentialID string
		if rotateErr := pool.QueryRow(ctx, bootstrapComponentRotateProviderCredentialQuery,
			accountID, secretUID, resourceVersion, digest).Scan(&credentialID); rotateErr != nil {
			t.Fatalf("rotate provider credential: %v", rotateErr)
		}
		return credentialID
	}

	staleLease := launchAndClaim("stale")
	accountID, staleCredentialID := providerForRevision(stringMap(staleLease, "runtimeRevisionRef"))
	_, currentCredentialID, versionBeforeRotation := accountState(accountID)
	if currentCredentialID != staleCredentialID {
		t.Fatalf("runtime revision did not pin the current credential: runtime=%s current=%s", staleCredentialID, currentCredentialID)
	}
	rotatedCredentialID := rotate(accountID, "40000000-0000-4000-8000-000000000001", "reauth-1", strings.Repeat("d", 64))
	completeRejected("stale", staleLease)
	state, currentCredentialID, versionAfterStale := accountState(accountID)
	if state != "AUTHORIZED" || currentCredentialID != rotatedCredentialID || versionAfterStale != versionBeforeRotation+1 {
		t.Fatalf("stale rejection disabled a rotated credential: state=%s credential=%s version=%d", state, currentCredentialID, versionAfterStale)
	}

	seedObservedCatalogFixture(t, ctx, repository)
	currentLease := launchAndClaim("current")
	currentAccountID, runtimeCredentialID := providerForRevision(stringMap(currentLease, "runtimeRevisionRef"))
	if currentAccountID != accountID || runtimeCredentialID != rotatedCredentialID {
		t.Fatalf("new runtime did not pin the rotated credential: account=%s credential=%s", currentAccountID, runtimeCredentialID)
	}
	completeRejected("current", currentLease)
	state, currentCredentialID, rejectedVersion := accountState(accountID)
	if state != "REAUTHORIZATION_REQUIRED" || currentCredentialID != rotatedCredentialID || rejectedVersion != versionAfterStale+1 {
		t.Fatalf("current rejection did not require reauthorization: state=%s credential=%s version=%d", state, currentCredentialID, rejectedVersion)
	}
	var rejectedAccountRef string
	if err := pool.QueryRow(ctx, `SELECT ref FROM control_plane.provider_accounts WHERE id = $1::uuid`, accountID).Scan(&rejectedAccountRef); err != nil {
		t.Fatalf("read rejected provider account ref: %v", err)
	}
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("resolve provider rejection owner: %v", err)
	}
	ownerScope, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatalf("resolve provider rejection owner scope: %v", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rejected provider selection: %v", err)
	}
	fallbackAccountID, selectErr := repository.selectProviderAccountForAgent(ctx, tx, ownerScope.organizationID, agent.Ref)
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback rejected provider selection: %v", rollbackErr)
	}
	if selectErr != nil || fallbackAccountID == "" || fallbackAccountID == accountID {
		t.Fatalf("rejected provider did not fail over to another authorized account: account=%s err=%v", fallbackAccountID, selectErr)
	}

	deviceReauthorizeOwner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.device-reauthorize",
	}, "control-api-gateway")
	deviceAuthorizationRef := "pauth_provider_rejection_reauthorize"
	materializerAttemptRef := "provider-rejection-reauthorize-attempt"
	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	pending, err := service.Execute(ctx, command.Command{Kind: command.StartProviderDeviceAuth, Principal: deviceReauthorizeOwner,
		Mutation: value.Mutation{IdempotencyKey: "provider-rejection-device-reauthorize", ExpectedVersion: &rejectedVersion},
		Payload: command.ProviderAccountInput{AccountRef: rejectedAccountRef, AuthorizationRef: deviceAuthorizationRef,
			AuthorizationMethod: "DEVICE_CODE", AuthorizationState: "PENDING", MaterializerAttemptRef: materializerAttemptRef,
			VerificationURI: "https://provider.invalid/device", UserCode: "TEST-CODE", AuthorizationExpiresAt: &expiresAt},
	})
	state, currentCredentialID, pendingVersion := accountState(accountID)
	if err != nil || pending.ProviderAccount == nil || state != "PENDING_AUTHORIZATION" || currentCredentialID != "" || pendingVersion != rejectedVersion+1 {
		t.Fatalf("device reauthorize retained credential: account=%#v state=%s credential=%s version=%d err=%v",
			pending.ProviderAccount, state, currentCredentialID, pendingVersion, err)
	}
	var cleanupEligible bool
	if err := pool.QueryRow(ctx, `
		SELECT task.eligible_at <= clock_timestamp()
		FROM control_plane.provider_credential_cleanup_tasks task
		WHERE task.provider_credential_revision_id = $1::uuid
	`, rotatedCredentialID).Scan(&cleanupEligible); err != nil || !cleanupEligible {
		t.Fatalf("device reauthorize did not schedule immediate credential cleanup: eligible=%t err=%v", cleanupEligible, err)
	}
	deviceVerifyOwner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.provider-accounts.device-verify",
	}, "control-api-gateway")
	finalDescriptor := entity.ProviderCredentialDescriptor{
		SecretName: "runtime-provider-rejection-reauthorized",
		SecretUID:  "40000000-0000-4000-8000-000000000002", SecretResourceVersion: "reauth-2",
		ContentSHA256: strings.Repeat("e", 64),
	}
	reauthorized, err := service.Execute(ctx, command.Command{Kind: command.RefreshProviderAuthorization, Principal: deviceVerifyOwner,
		Mutation: value.Mutation{IdempotencyKey: "provider-rejection-device-verify", ExpectedVersion: &pendingVersion},
		Payload: command.ProviderAccountInput{AccountRef: rejectedAccountRef, AuthorizationRef: deviceAuthorizationRef,
			AuthorizationMethod: "DEVICE_CODE", AuthorizationState: "AUTHORIZED", MaterializerAttemptRef: materializerAttemptRef,
			ExternalAccountMasked: "Reauthorized account", Credential: &finalDescriptor},
	})
	if err != nil || reauthorized.ProviderAccount == nil {
		t.Fatalf("verify device reauthorization: account=%#v err=%v", reauthorized.ProviderAccount, err)
	}
	if _, err := pool.Exec(ctx, `
		UPDATE control_plane.provider_credential_cleanup_tasks
		SET eligible_at = clock_timestamp() + interval '1 day', updated_at = clock_timestamp()
		WHERE provider_account_id = $1::uuid AND state = 'PENDING'
	`, accountID); err != nil {
		t.Fatalf("isolate device reauthorization cleanup fixture: %v", err)
	}
	state, currentCredentialID, finalVersion := accountState(accountID)
	if state != "AUTHORIZED" || currentCredentialID == "" || finalVersion != pendingVersion+1 {
		t.Fatalf("new credential did not restore provider account: state=%s credential=%s version=%d", state, currentCredentialID, finalVersion)
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.provider_accounts SET state = 'DISABLED', enabled = false WHERE id = $1::uuid`, fallbackAccountID); err != nil {
		t.Fatalf("disable fallback provider for exact restored selection: %v", err)
	}
	defer func() {
		if _, restoreErr := pool.Exec(context.WithoutCancel(ctx), `UPDATE control_plane.provider_accounts SET state = 'AUTHORIZED', enabled = true WHERE id = $1::uuid`, fallbackAccountID); restoreErr != nil {
			t.Errorf("restore fallback provider account: %v", restoreErr)
		}
	}()
	tx, err = pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin restored provider selection: %v", err)
	}
	selectedAccountID, selectErr := repository.selectProviderAccountForAgent(ctx, tx, ownerScope.organizationID, agent.Ref)
	if rollbackErr := tx.Rollback(ctx); rollbackErr != nil {
		t.Fatalf("rollback restored provider selection: %v", rollbackErr)
	}
	if selectErr != nil || selectedAccountID != accountID {
		t.Fatalf("reauthorized provider was not selected: account=%s err=%v", selectedAccountID, selectErr)
	}
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
	cleanArtifacts, cleanTotal, cleanNext, err := service.ListArtifacts(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, Query: "support-policy", ArtifactType: "TEXT", ScanState: "CLEAN",
		SourceKind: "CONTROL_CENTER", Page: query.Page{Size: 1},
	})
	if err != nil || cleanTotal != 1 || cleanNext != "" || len(cleanArtifacts) != 1 || cleanArtifacts[0].Ref != uploaded.Ref {
		t.Fatalf("server-side artifact filters were not applied before limit: artifacts=%#v next=%q err=%v", cleanArtifacts, cleanNext, err)
	}
	firstPage, firstTotal, nextPageToken, err := service.ListArtifacts(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, Page: query.Page{Size: 1},
	})
	firstRef, secondRef := quarantined.Ref, uploaded.Ref
	if firstRef > secondRef {
		firstRef, secondRef = secondRef, firstRef
	}
	if err != nil || firstTotal != 2 || len(firstPage) != 1 || firstPage[0].Ref != firstRef || nextPageToken == "" {
		t.Fatalf("first artifact cursor page is unstable: artifacts=%#v next=%q err=%v", firstPage, nextPageToken, err)
	}
	secondPage, secondTotal, finalPageToken, err := service.ListArtifacts(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, Page: query.Page{Size: 1, Token: nextPageToken},
	})
	if err != nil || secondTotal != 2 || len(secondPage) != 1 || secondPage[0].Ref != secondRef || finalPageToken != "" {
		t.Fatalf("second artifact cursor page is unstable: artifacts=%#v next=%q err=%v", secondPage, finalPageToken, err)
	}
	if _, _, _, err := service.ListArtifacts(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, ArtifactType: "EXECUTABLE", Page: query.Page{Size: 1},
	}); !errors.Is(err, domainerrs.ErrInvalid) {
		t.Fatalf("unknown artifact type was accepted: %v", err)
	}
	if _, err := service.DownloadArtifact(ctx, owner, quarantined.Ref, "DOWNLOAD"); !errors.Is(err, domainerrs.ErrForbidden) {
		t.Fatalf("download quarantined artifact must be forbidden: %v", err)
	}
	testArtifactCatalogTotals(t, ctx, service, owner, project.Project.Ref)
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
	metadata, err := service.Execute(ctx, command.Command{Kind: command.ProposeRunMetadata, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-run-metadata"}, Payload: command.ProposeRunMetadataInput{
			LeaseRef: stringMap(lease, "leaseRef"), Fence: stringMap(lease, "fence"), Generation: lease["generation"].(int64),
			Title: "Agent-proposed support run", ActivitySummary: "Preparing the support response",
		}})
	if err != nil || metadata.Run == nil || metadata.Run.Title != launch.Run.Title ||
		metadata.Run.TitleSource != "USER_EDITED" || metadata.Run.ActivitySummary != "Preparing the support response" {
		t.Fatalf("propose run metadata without overriding user title: run=%#v err=%v", metadata.Run, err)
	}
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
	outputAttachmentSetRef := finalizedAttachmentSetRef(t, ctx, service, owner, project.Project.Ref,
		"SESSION_TURN", "lifecycle-output-reuse-set", completed.CreatedRefs[0])
	continued, err := service.Execute(ctx, command.Command{Kind: command.AddSessionTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "lifecycle-continuation-1"}, Payload: command.SessionTurnInput{
			SessionRef: launch.Run.SessionRef, RunRef: launch.Run.Ref, Task: "Add a concise follow-up for the customer.",
			AttachmentSetRef: outputAttachmentSetRef,
		}})
	if err != nil || continued.Run == nil || continued.Graph == nil || continued.Run.State != "RUNNING" {
		t.Fatalf("continue session: run=%#v graph=%#v err=%v", continued.Run, continued.Graph, err)
	}
	resultPath := "/projects/" + project.Project.Ref + "/runs/" + launch.Run.Ref + "/workspace/results"
	inputPath := "/projects/" + project.Project.Ref + "/runs/" + continued.Run.Ref + "/workspace/inputs"
	results, resultTotal, _, err := service.ListVFSNodes(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, ResourceRef: resultPath, Page: query.Page{Size: 20},
	})
	if err != nil || resultTotal != 1 || len(results) != 1 || results[0].EntityRef != completed.CreatedRefs[0] || results[0].Kind != "RESULT" {
		t.Fatalf("VFS lost producer Run output classification: nodes=%#v total=%d err=%v", results, resultTotal, err)
	}
	inputs, inputTotal, _, err := service.ListVFSNodes(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, ResourceRef: inputPath, Page: query.Page{Size: 20},
	})
	if err != nil || inputTotal != 1 || len(inputs) != 1 || inputs[0].EntityRef != completed.CreatedRefs[0] || inputs[0].Kind != "INPUT" {
		t.Fatalf("VFS did not resolve exact continuation AttachmentSet input: nodes=%#v total=%d err=%v", inputs, inputTotal, err)
	}
	results, resultTotal, _, err = service.ListVFSNodes(ctx, owner, query.Filter{
		ProjectRef: project.Project.Ref, ResourceRef: resultPath, Page: query.Page{Size: 20},
	})
	if err != nil || resultTotal != 1 || len(results) != 1 || results[0].Kind != "RESULT" {
		t.Fatalf("VFS input reuse changed producer classification: nodes=%#v total=%d err=%v", results, resultTotal, err)
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
	completedRetry := claimAndCompleteRun(t, ctx, service, worker, retried.Run.Ref, "lifecycle-retry", false)
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

func claimAndCompleteRun(t *testing.T, ctx context.Context, service *platformservice.Service, worker value.Principal, expectedRunRef, key string, artifact bool) command.Result {
	t.Helper()
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: key + "-claim"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 || stringMap(claimed.RuntimeItems[0], "runRef") != expectedRunRef {
		t.Fatalf("claim %s execution: expected_run=%s claims=%#v err=%v", key, expectedRunRef, claimed.RuntimeItems, err)
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
	assistantReadback, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	firstHeartbeat, err := service.ReportWarmRuntime(ctx, warmWorker, command.WarmRuntimeInput{
		WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistantReadback.DesiredRuntimeRevision, State: "READY",
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
		WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistantReadback.DesiredRuntimeRevision, State: "READY",
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
	organizationArtifacts, _, _, err := service.ListArtifacts(ctx, owner, query.Filter{
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
	if _, err := service.Execute(ctx, command.Command{Kind: command.ArchiveAssistantConversation, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-busy-archive", ExpectedVersion: &turn.Conversation.Version},
		Payload:  command.AssistantConversationArchiveInput{ConversationRef: created.Conversation.Ref}}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("archived active assistant execution: %v", err)
	}
	queuedInputVersion := assistantInput.Version
	var queuedRunRef, queuedRunTitle, queuedRunState string
	if err := repository.pool.QueryRow(ctx, `
		SELECT ref, title, state
		FROM control_plane.runs
		WHERE organization_id = $1::uuid AND session_id = $2::uuid
		ORDER BY created_at DESC, ref DESC
		LIMIT 1
	`, ownerScope.organizationID, sessionID).Scan(&queuedRunRef, &queuedRunTitle, &queuedRunState); err != nil {
		t.Fatalf("read queued assistant run descriptor: %v", err)
	}
	queuedImpact, err := service.GetArtifactImpact(ctx, owner, assistantInput.Ref, "DELETE")
	if err != nil || !queuedImpact.Permitted || queuedImpact.ActiveRuntimeCount != 1 || queuedImpact.ActiveRunsTruncated ||
		len(queuedImpact.ActiveRuns) != 1 || queuedImpact.ActiveRuns[0].RunRef != queuedRunRef ||
		queuedImpact.ActiveRuns[0].Title != queuedRunTitle || queuedImpact.ActiveRuns[0].State != queuedRunState ||
		len(queuedImpact.Blockers) != 0 {
		t.Fatalf("queued assistant input impact: impact=%#v err=%v", queuedImpact, err)
	}
	deletedInput, err := service.Execute(ctx, command.Command{Kind: command.DeleteArtifact, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-artifact-delete-before-claim-1", ExpectedVersion: &queuedInputVersion},
		Payload:  command.ArtifactLifecycleInput{ArtifactRef: assistantInput.Ref, ImpactDigest: queuedImpact.Digest},
	})
	if err != nil || deletedInput.Artifact == nil || deletedInput.Artifact.LifecycleState != "DELETED" {
		t.Fatalf("soft-delete queued assistant input: artifact=%#v err=%v", deletedInput.Artifact, err)
	}
	if _, err := service.DownloadArtifact(ctx, owner, assistantInput.Ref, "DOWNLOAD"); !errors.Is(err, domainerrs.ErrNotFound) {
		t.Fatalf("ordinary download exposed soft-deleted assistant input: %v", err)
	}
	rejectedConversation, err := service.Execute(ctx, command.Command{Kind: command.CreateAssistantConversation, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-conversation-deleted-input-1"}, Payload: command.AssistantConversationInput{}})
	if err != nil || rejectedConversation.Conversation == nil {
		t.Fatalf("create assistant conversation for deleted input rejection: conversation=%#v err=%v", rejectedConversation.Conversation, err)
	}
	if _, err := service.Execute(ctx, command.Command{Kind: command.AddAssistantTurn, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "assistant-turn-deleted-input-1"}, Payload: command.AssistantTurnInput{
			ConversationRef: rejectedConversation.Conversation.Ref, Content: "Must not bind deleted input", AttachmentSetRef: assistantAttachmentSetRef,
		}}); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("new assistant turn accepted soft-deleted attachment snapshot: %v", err)
	}
	activePurgeImpact, err := service.GetArtifactImpact(ctx, owner, assistantInput.Ref, "PURGE")
	if err != nil || activePurgeImpact.Permitted || activePurgeImpact.AttachmentCount < 1 ||
		activePurgeImpact.ActiveRuntimeCount != 1 || activePurgeImpact.ActiveRunsTruncated ||
		len(activePurgeImpact.ActiveRuns) != 1 || activePurgeImpact.ActiveRuns[0].RunRef != queuedRunRef ||
		!contains(activePurgeImpact.Blockers, "ACTIVE_RUN_USES_ARTIFACT") ||
		contains(activePurgeImpact.Blockers, "ARTIFACT_HAS_IMMUTABLE_ATTACHMENTS") {
		t.Fatalf("active assistant input purge impact: impact=%#v err=%v", activePurgeImpact, err)
	}
	if _, err := service.PurgeArtifact(ctx, owner,
		value.Mutation{IdempotencyKey: "assistant-artifact-purge-active-1", ExpectedVersion: &deletedInput.Artifact.Version},
		assistantInput.Ref, activePurgeImpact.Digest); !errors.Is(err, domainerrs.ErrConflict) {
		t.Fatalf("purge active assistant input returned %v", err)
	}
	claimed, err := service.Execute(ctx, command.Command{Kind: command.ClaimExecution, Principal: worker,
		Mutation: value.Mutation{IdempotencyKey: "assistant-claim-after-soft-delete-1"}, Payload: command.LeaseInput{WorkloadInstance: "runtime-test", Limit: 1}})
	if err != nil || len(claimed.RuntimeItems) != 1 {
		t.Fatalf("claim assistant execution after input soft delete: claims=%d err=%v", len(claimed.RuntimeItems), err)
	}
	lease := claimed.RuntimeItems[0]
	if stringMap(lease, "projectRef") != projectRef {
		t.Fatalf("assistant runtime lost project binding: got=%q want=%q", stringMap(lease, "projectRef"), projectRef)
	}
	artifactCatalog, ok := lease["artifacts"].([]map[string]any)
	if !ok || len(artifactCatalog) != 1 || stringMap(artifactCatalog[0], "ref") != assistantInput.Ref {
		t.Fatalf("assistant runtime lost soft-deleted organization attachment snapshot: %#v", lease["artifacts"])
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
	purgeImpact, err := service.GetArtifactImpact(ctx, owner, assistantInput.Ref, "PURGE")
	if err != nil || !purgeImpact.Permitted || purgeImpact.AttachmentCount < 1 ||
		purgeImpact.ActiveRuntimeCount != int64(len(purgeImpact.ActiveRuns)) || purgeImpact.ActiveRunsTruncated ||
		len(purgeImpact.Blockers) != 0 {
		t.Fatalf("terminal assistant attachment purge impact: impact=%#v err=%v", purgeImpact, err)
	}
	purgedState, err := service.PurgeArtifact(ctx, owner,
		value.Mutation{IdempotencyKey: "assistant-artifact-purge-terminal-1", ExpectedVersion: &deletedInput.Artifact.Version},
		assistantInput.Ref, purgeImpact.Digest)
	if err != nil || purgedState != "PURGED" {
		t.Fatalf("purge terminal assistant attachment: state=%q err=%v", purgedState, err)
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
		corePromptCount != 1 || assistantRuntimeCount != 1 || capabilityCount != 9 ||
		integrationDefinitionCount != 7 || providerDefinitionCount != 1 || providerAccountCount != 1 ||
		providerCredentialRevisionCount != 1 || completedBootstrapCount != 1 {
		t.Fatalf("unexpected bootstrap state: organization=%d owner_contract=%d assistant=%d core_prompt=%d runtime=%d capabilities=%d integrations=%d provider_definitions=%d provider_accounts=%d provider_credentials=%d completed=%d",
			organizationCount, ownerContractCount, systemAssistantCount, corePromptCount,
			assistantRuntimeCount, capabilityCount, integrationDefinitionCount, providerDefinitionCount,
			providerAccountCount, providerCredentialRevisionCount, completedBootstrapCount)
	}
}
