package platform

import (
	"context"
	_ "embed"
	"errors"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5/pgxpool"
)

// historicalSecretAdmissionSnapshot observes exact ENV/binding history and owner-effect cardinalities without reading secret values or changing state.
// historicalSecretAdmissionSnapshot наблюдает точную ENV/binding history и owner-effect cardinalities без чтения значений секретов и изменения состояния.
//
//go:embed testdata/sql/historical_secret_admission_snapshot.sql
var historicalSecretAdmissionSnapshot string

// TestHistoricalEnvironmentSecretAdmissionComponent keeps an eligible historical pin usable while retired or revoked material must fail before bind/rebind/rollback effects. Native owner commands and synthetic broker callbacks create every business state; consumer-rebind uses the actual impact snapshot exposed by the public operation profile.
// TestHistoricalEnvironmentSecretAdmissionComponent сохраняет пригодный historical pin доступным, а retired/revoked material обязан отклоняться до bind/rebind/rollback effects. Все business states создаются native owner commands и синтетическими broker callbacks; consumer-rebind использует настоящий impact snapshot публичного operation profile.
func TestHistoricalEnvironmentSecretAdmissionComponent(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("construct disposable historical admission pool")
	}
	defer pool.Close()
	repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal("bootstrap historical admission repository")
	}
	seedObservedCatalogFixture(t, ctx, repository)
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{
		ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002",
		CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create",
	}, "control-api-gateway")
	consume := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.consume")
	complete := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.complete")
	recoverer := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.recover")
	for _, mode := range []string{"eligible-historical", "retired-historical", "revoked-historical"} {
		t.Run(mode, func(t *testing.T) {
			invoke := func(kind command.Kind, key string, version *int64, payload any) (command.Result, error) {
				return service.Execute(ctx, command.Command{Kind: kind, Principal: owner,
					Mutation: value.Mutation{IdempotencyKey: "historical-admission-" + mode + "-" + key, ExpectedVersion: version}, Payload: payload})
			}
			execute := func(kind command.Kind, key string, version *int64, payload any) command.Result {
				t.Helper()
				result, err := invoke(kind, key, version, payload)
				if err != nil {
					t.Fatalf("fixture %s: %v", key, err)
				}
				return result
			}
			project := execute(command.CreateProject, "project", nil, command.ProjectInput{Name: "Historical secret " + mode, Language: "en"}).Project
			helper := execute(command.CreateProjectAssistant, "helper", nil, command.ProjectAssistantInput{ProjectRef: project.Ref, Name: "Historical admission helper",
				Purpose: "Bounded historical secret fixture", Instructions: "Use only explicitly assigned resources."}).ProjectAssistant
			base, err := service.GetAgentRuntimeConfiguration(ctx, owner, helper.AgentRef)
			if err != nil || base.Environment.CurrentVersion.Image.ArtifactRef == "" {
				t.Fatal("native helper has no exact project image")
			}
			prepared, err := service.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, "secret.create"), platformrepo.RuntimeSecretPrepareInput{
				Kind: "CREATE", ProjectRef: project.Ref, Name: "Historical marker", ValueType: "STRING", ExpectedContentSHA256: runtimeSecretHashA,
				Mutation: value.Mutation{IdempotencyKey: "historical-admission-" + mode + "-secret"}})
			if err != nil {
				t.Fatalf("prepare marker: %v", err)
			}
			claimed, err := service.ConsumeRuntimeSecretOperation(ctx, consume, platformrepo.RuntimeSecretConsumeInput{OperationGrant: prepared.OperationGrant, ClaimantID: "historical-admission-broker"})
			if err != nil {
				t.Fatalf("claim marker: %v", err)
			}
			name, err := runtimesecret.VersionedKubernetesName(claimed.SecretRef, claimed.TargetRevision)
			if err != nil {
				t.Fatal(err)
			}
			material := entity.RuntimeSecretMaterialization{Namespace: "kodex-runtime", SecretName: name, SecretKey: "value",
				SecretUID: "10000000-0000-4000-8000-000000001876", SecretResourceVersion: "1876", ContentSHA256: runtimeSecretHashA}
			secret, err := service.CompleteRuntimeSecretOperation(ctx, complete, platformrepo.RuntimeSecretCompleteInput{
				OperationRef: prepared.OperationRef, ClaimantID: "historical-admission-broker", ClaimGeneration: claimed.ClaimGeneration, Materialization: &material})
			if err != nil {
				t.Fatalf("complete marker: %v", err)
			}
			contents := command.RuntimeEnvironmentInput{ProjectRef: project.Ref, Name: "Historical marker environment", ImageArtifactRef: base.Environment.CurrentVersion.Image.ArtifactRef,
				Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy(), SecretBindings: []entity.RuntimeSecretBinding{{Name: "HISTORICAL_MARKER", SecretRef: secret.Ref, Revision: secret.CurrentRevision}}}
			environment := execute(command.CreateRuntimeEnvironment, "environment-v1", nil, contents).RuntimeEnvironment
			historical := environment.CurrentVersion
			secret = completeRuntimeSecretRotate(t, ctx, service, runtimeSecretOwnerPrincipal(owner, "secret.rotate"), consume, complete, secret, runtimeSecretHashB, "historical-admission-"+mode+"-rotate")
			contents.Ref, contents.SecretBindings = environment.Ref, nil
			environment = execute(command.PublishRuntimeEnvironment, "environment-v2-empty", &environment.Version, contents).RuntimeEnvironment
			if mode == "retired-historical" {
				retired, err := service.RecoverRuntimeSecretMaterialization(ctx, recoverer, platformrepo.RuntimeSecretRecoveryInput{OperationRef: prepared.OperationRef, Materialization: material})
				if err != nil || retired.Action != "DELETE" {
					t.Fatalf("native R1 retirement did not complete: %v", err)
				}
			} else if mode == "revoked-historical" {
				revoke, err := service.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, "secret.revoke"), platformrepo.RuntimeSecretPrepareInput{Kind: "REVOKE", SecretRef: secret.Ref,
					Mutation: value.Mutation{IdempotencyKey: "historical-admission-" + mode + "-revoke", ExpectedVersion: &secret.Version}})
				if err != nil {
					t.Fatalf("prepare unreferenced marker revoke: %v", err)
				}
				revokeClaim, err := service.ConsumeRuntimeSecretOperation(ctx, consume, platformrepo.RuntimeSecretConsumeInput{OperationGrant: revoke.OperationGrant, ClaimantID: "historical-admission-revoke"})
				if err != nil {
					t.Fatalf("claim marker revoke: %v", err)
				}
				revoked, err := service.CompleteRuntimeSecretOperation(ctx, complete, platformrepo.RuntimeSecretCompleteInput{OperationRef: revoke.OperationRef,
					ClaimantID: "historical-admission-revoke", ClaimGeneration: revokeClaim.ClaimGeneration})
				if err != nil || revoked.State != "REVOKED" {
					t.Fatalf("native marker revocation did not complete: %v", err)
				}
			}
			snapshot := func() string {
				t.Helper()
				var result string
				if err := pool.QueryRow(ctx, historicalSecretAdmissionSnapshot, environment.Ref, helper.AgentRef).Scan(&result); err != nil {
					t.Fatal("read historical admission owner snapshot")
				}
				return result
			}
			for _, operation := range []string{"publish", "bind", "rebind", "rollback"} {
				if mode == "eligible-historical" && operation == "publish" {
					continue
				}
				t.Run(operation, func(t *testing.T) {
					kind := command.BindAgentRuntimeEnvironment
					version := base.AgentVersion
					var payload any = command.RuntimeEnvironmentBindingInput{AgentRef: helper.AgentRef, EnvironmentRef: environment.Ref, VersionRef: historical.Ref}
					if operation == "rollback" {
						kind, version = command.RollbackRuntimeEnvironment, environment.Version
						payload = command.RuntimeEnvironmentInput{Ref: environment.Ref, PublishedVersionRef: historical.Ref}
					} else if operation == "publish" {
						kind, version = command.PublishRuntimeEnvironment, environment.Version
						candidate := contents
						candidate.SecretBindings = []entity.RuntimeSecretBinding{{Name: "HISTORICAL_MARKER", SecretRef: secret.Ref, Revision: 1}}
						payload = candidate
					} else if operation == "rebind" {
						fresh, err := service.GetAgentRuntimeConfiguration(ctx, owner, helper.AgentRef)
						if err != nil {
							t.Fatal("read native consumer before public rebind fixture")
						}
						execute(command.BindAgentRuntimeEnvironment, "bind-empty-v2-for-rebind", &fresh.AgentVersion,
							command.RuntimeEnvironmentBindingInput{AgentRef: helper.AgentRef, EnvironmentRef: environment.Ref})
						impact, err := service.GetRuntimeEnvironmentImpact(ctx, owner, environment.Ref, historical.Ref, "", query.Page{Size: 10})
						if err != nil || len(impact.Consumers) != 1 || impact.Consumers[0].AgentRef != helper.AgentRef || impact.Consumers[0].VersionRef != environment.CurrentVersion.Ref {
							t.Fatal("public historical rebind has no exact current consumer snapshot")
						}
						kind, version = command.RebindRuntimeEnvironment, environment.Version
						payload = command.RuntimeEnvironmentRebindInput{EnvironmentRef: environment.Ref, VersionRef: historical.Ref, Consumers: impact.Consumers}
					}
					before := snapshot()
					result, err := invoke(kind, operation, &version, payload)
					if mode != "eligible-historical" {
						if !errors.Is(err, errs.ErrNotFound) {
							t.Errorf("%s with %s must reject the exact inactive revision before effects: %v", operation, mode, err)
						}
						if snapshot() != before {
							t.Error("rejected historical secret admission changed ENV, binding, history, receipt, audit or outbox")
						}
						return
					}
					if err != nil {
						t.Fatalf("eligible historical %s was rejected: %v", operation, err)
					}
					if operation == "bind" && (result.RuntimeConfiguration == nil || result.RuntimeConfiguration.Environment.CurrentVersion.Ref != historical.Ref ||
						len(result.RuntimeConfiguration.Environment.CurrentVersion.SecretDescriptors) != 1 || result.RuntimeConfiguration.Environment.CurrentVersion.SecretDescriptors[0].Revision != 1) {
						t.Fatal("historical bind silently selected a newer secret or ENV version")
					}
					if operation == "rebind" {
						fresh, err := service.GetAgentRuntimeConfiguration(ctx, owner, helper.AgentRef)
						if err != nil || len(result.EnvironmentBindings) != 1 || fresh.Environment.CurrentVersion.Ref != historical.Ref ||
							len(fresh.Environment.CurrentVersion.SecretDescriptors) != 1 || fresh.Environment.CurrentVersion.SecretDescriptors[0].Revision != 1 {
							t.Fatal("public historical rebind silently selected a newer secret or ENV version")
						}
					}
					if operation == "rollback" && (result.RuntimeEnvironment == nil || result.RuntimeEnvironment.CurrentVersion.Digest != historical.Digest ||
						len(result.RuntimeEnvironment.CurrentVersion.SecretDescriptors) != 1 || result.RuntimeEnvironment.CurrentVersion.SecretDescriptors[0].Revision != 1) {
						t.Fatal("historical rollback silently selected newer secret contents")
					}
					committed := snapshot()
					if _, err := invoke(kind, operation, &version, payload); err != nil || snapshot() != committed {
						t.Fatalf("eligible historical %s receipt did not replay without additional effects: %v", operation, err)
					}
				})
			}
		})
	}
}
