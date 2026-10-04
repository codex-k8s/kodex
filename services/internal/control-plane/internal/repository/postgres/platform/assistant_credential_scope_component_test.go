package platform

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/internalrpcauth/transportprofile"
	"github.com/codex-k8s/kodex/libs/go/objectstorage/objectstoragetest"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed testdata/sql/assistant_credential_scope_readback.sql
var queryAssistantCredentialScopeReadback string

//go:embed testdata/sql/assistant_credential_scope_disable_actor.sql
var queryAssistantCredentialScopeDisableActor string

// Только disposable PostgreSQL: реальные provider и runtime агенты не запускаются.
func TestAssistantCredentialScopesComponent(t *testing.T) {
	dsn := isolatedAssistantComponentDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	newRepository := func() *Repository {
		t.Helper()
		repository, err := New(pool, "openai-codex", "gpt-5", objectstoragetest.New())
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
		return repository
	}
	repository := newRepository()
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	prepareObservedWarmFixture(t, ctx, repository)
	principal := func(workload, operation, actor, tenant string) value.Principal {
		t.Helper()
		return resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
			ExternalActorID: actor, ExternalTenantID: tenant, CallerWorkload: workload, Operation: operation,
		}, workload)
	}
	owner := principal("control-api-gateway", "platform.assistant.turns.add", "20000000-0000-4000-8000-000000000001", "20000000-0000-4000-8000-000000000002")
	worker := principal("runtime-controller", "platform.runtime.execution.claim", "kodex-system-subject", "kodex-installation")
	warmWorker := principal("runtime-controller", "platform.runtime.warm.report", "kodex-system-subject", "kodex-installation")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	assistant, err := service.GetSystemAssistant(ctx, owner)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.ReportWarmRuntime(ctx, warmWorker, command.WarmRuntimeInput{
		WorkloadInstance: "catalog-observed-warm-fixture", RuntimeRevision: assistant.DesiredRuntimeRevision, State: "READY",
	}); err != nil {
		t.Fatal(err)
	}
	execute := func(kind command.Kind, actor value.Principal, key string, version *int64, payload any) command.Result {
		t.Helper()
		result, err := service.Execute(ctx, command.Command{Kind: kind, Principal: actor,
			Mutation: value.Mutation{IdempotencyKey: "credential-scope-" + key, ExpectedVersion: version}, Payload: payload})
		if err != nil {
			t.Fatalf("execute %s (%s): %v", kind, key, err)
		}
		return result
	}

	project := execute(command.CreateProject, owner, "project", nil, command.ProjectInput{Name: "Credential scope project", Language: "en"}).Project
	profile := execute(command.CreateProjectAssistant, owner, "profile", nil, command.ProjectAssistantInput{ProjectRef: project.Ref, Name: "Project assistant", Purpose: "Project scoped credential proof", Instructions: "Use only project resources."}).ProjectAssistant
	conversations := make(map[string]entity.AssistantConversation)
	for _, scope := range []string{"SYSTEM", "PROJECT"} {
		conversations[scope] = *execute(command.CreateAssistantConversation, owner, scope+"-create", nil, command.AssistantConversationInput{AssistantScope: scope, ProjectRef: project.Ref}).Conversation
		execute(command.AddAssistantTurn, owner, scope+"-turn", nil, command.AssistantTurnInput{ConversationRef: conversations[scope].Ref, Content: "Synthetic credential boundary " + scope, DeliveryMode: "QUEUE"})
	}
	claims := execute(command.ClaimExecution, worker, "claim", nil, command.LeaseInput{WorkloadInstance: "credential-scope-controller", Limit: 2}).RuntimeItems
	if len(claims) != 2 {
		t.Fatal("both scoped assistant executions were not claimed")
	}
	broker := principal("secret-broker", "platform.credential-projections.runtime.resolve", "kodex-system-subject", "kodex-installation")
	broker, err = repository.ResolvePrincipal(ctx, broker)
	if err != nil {
		t.Fatal(err)
	}
	trusted := *repository
	if err := trusted.ConfigureRPCProfile(transportprofile.TrustedCluster); err != nil {
		t.Fatal(err)
	}
	for _, claim := range claims {
		scope := stringMap(claim, "assistantScope")
		t.Run(scope, func(t *testing.T) {
			if scope != "SYSTEM" && scope != "PROJECT" {
				t.Fatal("unknown scoped execution")
			}
			if scope == "PROJECT" && stringMap(claim, "assistantProfileRef") != profile.Ref {
				t.Fatal("claim lost project profile")
			}
			var expectedActor, expectedOrg, expectedResourceProject, contextProject, pinScope string
			if err := pool.QueryRow(ctx, queryAssistantCredentialScopeReadback, stringMap(claim, "leaseRef")).Scan(&expectedActor, &expectedOrg, &expectedResourceProject, &contextProject, &pinScope); err != nil {
				t.Fatal(err)
			}
			if contextProject == "" || pinScope != scope || scope == "SYSTEM" && expectedResourceProject != "" || scope == "PROJECT" && expectedResourceProject == "" {
				t.Fatal("fixture did not distinguish screen context from resource authority")
			}
			if scope == "SYSTEM" {
				t.Run("SQL secret consumer only", func(t *testing.T) {
					testAssistantCredentialSecretConsumer(t, ctx, pool, expectedActor, expectedOrg, contextProject)
				})
			}
			raw, _ := json.Marshal(claim)
			var materialization runtimeMaterializationInput
			if json.Unmarshal(raw, &materialization) != nil {
				t.Fatal("invalid claim fixture")
			}
			materialization.WorkloadInstance = "credential-scope-controller"
			materialization.RuntimeRevisionDigest = stringMap(claim, "revisionDigest")
			materialization.SystemAssistant = scope == "SYSTEM"
			operation, digest, err := runtimeMaterializationDigest(materialization)
			if err != nil {
				t.Fatal(err)
			}
			proofProject := project.Ref
			method := runtimeProjectionMethod
			if scope == "SYSTEM" {
				proofProject = ""
				method = assistantProjectionMethod
			}
			proofInput := platformrepo.ProofPrincipalInput{CallerWorkload: "runtime-controller", Operation: operation, ExternalActorID: "kodex-system-subject", ExternalTenantID: "kodex-installation", ProjectRef: proofProject, RequestDigestSHA256: digest}
			proof, err := repository.ResolveProofAuthority(ctx, proofInput)
			if err != nil || proof.RuntimeExecution == nil || proof.ActorID != expectedActor || proof.OrganizationID != expectedOrg || proof.ProjectID != expectedResourceProject || proof.RuntimeExecution.RevisionDigest != materialization.RuntimeRevisionDigest {
				t.Fatalf("owner materialization proof diverged from canonical resource scope: %v", err)
			}
			input := platformrepo.RuntimeCredentialProjectionInput{Authority: platformrepo.CredentialProjectionAuthority{RPCProfile: transportprofile.TrustedCluster, CallerWorkloadID: "runtime-controller", CallerFullMethod: method},
				WorkloadInstance: materialization.WorkloadInstance, LeaseRef: materialization.LeaseRef, Fence: materialization.Fence, Generation: materialization.Generation, RuntimeRevisionRef: materialization.RuntimeRevisionRef, RuntimeRevisionDigest: materialization.RuntimeRevisionDigest, SessionRef: materialization.SessionRef, TurnRef: materialization.TurnRef, Attempt: int32(materialization.Attempt), InputDigest: materialization.InputDigest}
			projection, err := trusted.ResolveRuntimeCredentialProjection(ctx, broker, input)
			if err != nil || projection.Authority.ActorID != expectedActor || projection.Authority.TenantID != expectedOrg || projection.Authority.ProjectID != expectedResourceProject || projection.ProviderCredential.AccountRef == "" {
				t.Fatalf("exact scoped credential materialization failed: %v", err)
			}
			for name, mutate := range map[string]func(*platformrepo.RuntimeCredentialProjectionInput){
				"cross scope method": func(value *platformrepo.RuntimeCredentialProjectionInput) {
					if scope == "SYSTEM" {
						value.Authority.CallerFullMethod = runtimeProjectionMethod
					} else {
						value.Authority.CallerFullMethod = assistantProjectionMethod
					}
				},
				"caller actor":     func(value *platformrepo.RuntimeCredentialProjectionInput) { value.Authority.ActorID = expectedActor },
				"caller project":   func(value *platformrepo.RuntimeCredentialProjectionInput) { value.Authority.ProjectID = contextProject },
				"foreign turn":     func(value *platformrepo.RuntimeCredentialProjectionInput) { value.TurnRef = "turn_foreign123" },
				"foreign session":  func(value *platformrepo.RuntimeCredentialProjectionInput) { value.SessionRef = "session_foreign123" },
				"stale attempt":    func(value *platformrepo.RuntimeCredentialProjectionInput) { value.Attempt++ },
				"wrong fence":      func(value *platformrepo.RuntimeCredentialProjectionInput) { value.Fence += "wrong" },
				"wrong generation": func(value *platformrepo.RuntimeCredentialProjectionInput) { value.Generation++ },
				"foreign workload": func(value *platformrepo.RuntimeCredentialProjectionInput) { value.WorkloadInstance += "-foreign" },
				"foreign revision": func(value *platformrepo.RuntimeCredentialProjectionInput) {
					value.RuntimeRevisionRef = "rrev_foreign123"
				},
				"wrong revision digest": func(value *platformrepo.RuntimeCredentialProjectionInput) {
					value.RuntimeRevisionDigest = strings.Repeat("f", 64)
				},
				"wrong input digest": func(value *platformrepo.RuntimeCredentialProjectionInput) {
					value.InputDigest = strings.Repeat("f", 64)
				},
			} {
				t.Run(name, func(t *testing.T) {
					changed := input
					mutate(&changed)
					if _, err := trusted.ResolveRuntimeCredentialProjection(ctx, broker, changed); !errors.Is(err, errs.ErrForbidden) && !errors.Is(err, errs.ErrNotFound) {
						t.Fatalf("detached authority was not rejected: %v", err)
					}
				})
			}
			changedProof := proofInput
			if scope == "SYSTEM" {
				changedProof.ProjectRef = project.Ref
			} else {
				changedProof.ProjectRef = ""
			}
			if _, err := repository.ResolveProofAuthority(ctx, changedProof); !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("proof admitted wrong resource project: %v", err)
			}
			validate := broker
			validate.Permission = "platform.credential-projections.runtime.validate"
			recovery := input
			recovery.Fence = ""
			recovery.Authority = projection.Authority
			recovery.ProviderCredential = projection.ProviderCredential
			recovery.RuntimeSecrets = projection.RuntimeSecrets
			if current, err := trusted.ValidateRuntimeCredentialProjection(ctx, validate, recovery); err != nil || !current {
				t.Fatalf("exact recovery unavailable: %v", err)
			}
			t.Run("disabled root actor", func(t *testing.T) {
				tx, err := pool.Begin(ctx)
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback(ctx) }()
				args := pgx.StrictNamedArgs{
					"organization_id": expectedOrg, "actor_id": expectedActor, "project_id": nullUUID(expectedResourceProject),
					"system_assistant": scope == "SYSTEM", "lease_ref": input.LeaseRef, "workload_instance": input.WorkloadInstance,
					"generation": input.Generation, "fence": input.Fence, "runtime_revision_ref": input.RuntimeRevisionRef,
					"runtime_revision_digest": input.RuntimeRevisionDigest, "attempt": input.Attempt, "input_digest": input.InputDigest,
					"session_ref": input.SessionRef, "turn_ref": input.TurnRef,
				}
				assertEligibility := func(want bool) {
					t.Helper()
					rows, err := tx.Query(ctx, queryCredentialProjectionResolveRuntime, args)
					if err != nil {
						t.Fatal(err)
					}
					found := rows.Next()
					rows.Close()
					if found != want || rows.Err() != nil {
						t.Fatalf("runtime credential eligibility want=%v got=%v err=%v", want, found, rows.Err())
					}
				}
				assertEligibility(true)
				if tag, err := tx.Exec(ctx, queryAssistantCredentialScopeDisableActor, input.LeaseRef); err != nil || tag.RowsAffected() != 1 {
					t.Fatalf("disable exact fixture root actor: %v", err)
				}
				proofRows, err := tx.Query(ctx, runtimeMaterializationResolveProofSQL, pgx.StrictNamedArgs{
					"operation": operation, "request_digest": digest, "project_ref": proofProject, "system_assistant": scope == "SYSTEM",
				})
				if err != nil {
					t.Fatal(err)
				}
				proofFound := proofRows.Next()
				proofRows.Close()
				if proofFound || proofRows.Err() != nil {
					t.Fatalf("disabled actor retained materialization proof: %v", proofRows.Err())
				}
				authorityRows, err := tx.Query(ctx, queryCredentialProjectionTrustedAuthority, pgx.StrictNamedArgs{
					"organization_id": expectedOrg, "lease_ref": input.LeaseRef, "workload_instance": input.WorkloadInstance,
					"generation": input.Generation, "runtime_revision_ref": input.RuntimeRevisionRef, "runtime_revision_digest": input.RuntimeRevisionDigest,
				})
				if err != nil {
					t.Fatal(err)
				}
				authorityFound := authorityRows.Next()
				authorityRows.Close()
				if authorityFound || authorityRows.Err() != nil {
					t.Fatalf("disabled actor retained trusted materialization authority: %v", authorityRows.Err())
				}
				assertEligibility(false)
			})
			for name, mutate := range map[string]func(*platformrepo.RuntimeCredentialProjectionInput){
				"foreign actor": func(value *platformrepo.RuntimeCredentialProjectionInput) {
					value.Authority.ActorID = "30000000-0000-4000-8000-000000000001"
				},
				"foreign tenant": func(value *platformrepo.RuntimeCredentialProjectionInput) {
					value.Authority.TenantID = "30000000-0000-4000-8000-000000000002"
				},
				"changed owner revision":      func(value *platformrepo.RuntimeCredentialProjectionInput) { value.Authority.SourceRevision++ },
				"changed workload credential": func(value *platformrepo.RuntimeCredentialProjectionInput) { value.Authority.CallerCredentialRevision++ },
				"extended expiry": func(value *platformrepo.RuntimeCredentialProjectionInput) {
					value.Authority.ExpiresAt = value.Authority.ExpiresAt.Add(time.Second)
				},
				"changed provider digest": func(value *platformrepo.RuntimeCredentialProjectionInput) {
					value.ProviderCredential.ContentSHA256 = strings.Repeat("f", 64)
				},
			} {
				t.Run("recovery "+name, func(t *testing.T) {
					changed := recovery
					mutate(&changed)
					current, err := trusted.ValidateRuntimeCredentialProjection(ctx, validate, changed)
					if current || err != nil && !errors.Is(err, errs.ErrForbidden) {
						t.Fatalf("detached recovery was not closed: current=%v err=%v", current, err)
					}
				})
			}
			completeClaimedExecutionWithSummary(t, ctx, service, worker, claim, "credential-scope-"+scope, false, "Synthetic scoped credential completion")
			if current, err := trusted.ValidateRuntimeCredentialProjection(ctx, validate, recovery); err != nil || current {
				t.Fatalf("terminal lease kept scoped credentials current: %v", err)
			}
			if _, err := repository.ResolveProofAuthority(ctx, proofInput); !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("terminal lease kept materialization proof: %v", err)
			}
		})
	}
}
