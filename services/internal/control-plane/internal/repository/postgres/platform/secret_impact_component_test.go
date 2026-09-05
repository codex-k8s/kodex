package platform

import (
	"context"
	"errors"
	"testing"

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

func testSecretImpact(t *testing.T, ctx context.Context, repository *Repository, pool *pgxpool.Pool) {
	t.Helper()
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001",
		ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.runtime-secrets.rebind"}, "control-api-gateway")
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	var projectRef, imageRef string
	if err := pool.QueryRow(ctx, `SELECT project.ref,image.ref FROM control_plane.image_artifacts image JOIN control_plane.projects project ON project.id=image.project_id
WHERE project.name='Role image promotion' AND image.promotion_state='PROMOTED' AND image.admission_state='ACCEPTED' ORDER BY image.ref LIMIT 1`).Scan(&projectRef, &imageRef); err != nil {
		t.Fatal(err)
	}
	prepared, err := service.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, "secret.create"), platformrepo.RuntimeSecretPrepareInput{
		Kind: "CREATE", ProjectRef: projectRef, Name: "selected-rebind-secret", ValueType: "STRING", ExpectedContentSHA256: runtimeSecretHashA, Mutation: value.Mutation{IdempotencyKey: "secret-impact-create"}})
	if err != nil {
		t.Fatal(err)
	}
	consume := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.consume")
	complete := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.complete")
	recoverer := runtimeSecretSystemPrincipal(t, ctx, repository, "platform.runtime-secrets.operations.recover")
	claimed, err := service.ConsumeRuntimeSecretOperation(ctx, consume, platformrepo.RuntimeSecretConsumeInput{OperationGrant: prepared.OperationGrant, ClaimantID: "secret-impact-worker"})
	if err != nil {
		t.Fatal(err)
	}
	name, _ := runtimesecret.VersionedKubernetesName(claimed.SecretRef, claimed.TargetRevision)
	materialization := entity.RuntimeSecretMaterialization{Namespace: "kodex-runtime", SecretName: name, SecretKey: "value", SecretUID: "10000000-0000-4000-8000-000000000201", SecretResourceVersion: "201", ContentSHA256: runtimeSecretHashA}
	secret, err := service.CompleteRuntimeSecretOperation(ctx, complete, platformrepo.RuntimeSecretCompleteInput{OperationRef: prepared.OperationRef, ClaimantID: "secret-impact-worker", ClaimGeneration: claimed.ClaimGeneration, Materialization: &materialization})
	if err != nil {
		t.Fatal(err)
	}
	environmentInput := command.RuntimeEnvironmentInput{ProjectRef: projectRef, Name: "Secret impact environment", ImageArtifactRef: imageRef, Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy(),
		SecretBindings: []entity.RuntimeSecretBinding{{Name: "SERVICE_TOKEN", SecretRef: secret.Ref, Revision: 1}}}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "secret-impact-environment"}, Payload: environmentInput})
	if err != nil || created.RuntimeEnvironment == nil {
		t.Fatalf("create secret environment: %v", err)
	}
	environment := *created.RuntimeEnvironment
	var consumers []entity.RuntimeEnvironmentConsumer
	for _, key := range []string{"secret-impact-agent-a", "secret-impact-agent-b"} {
		agent := createLifecycleAgent(t, ctx, service, owner, projectRef, key, key)
		bound, err := service.Execute(ctx, command.Command{Kind: command.BindAgentRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: key + "-bind", ExpectedVersion: &agent.Version},
			Payload: command.RuntimeEnvironmentBindingInput{AgentRef: agent.Ref, EnvironmentRef: environment.Ref, VersionRef: environment.CurrentVersion.Ref}})
		if err != nil || bound.RuntimeConfiguration == nil {
			t.Fatalf("bind secret agent: %v", err)
		}
		view := bound.RuntimeConfiguration
		b := view.EnvironmentBinding
		consumers = append(consumers, entity.RuntimeEnvironmentConsumer{AgentRef: agent.Ref, AgentVersion: view.AgentVersion, BindingRef: b.Ref, BindingVersion: b.Version, VersionRef: b.VersionRef, ProjectRef: projectRef})
	}
	rotated := completeRuntimeSecretRotate(t, ctx, service, runtimeSecretOwnerPrincipal(owner, "secret.rotate"), consume, complete, secret, runtimeSecretHashB, "secret-impact-rotate")
	recoverOld := func(want string) {
		t.Helper()
		result, err := service.RecoverRuntimeSecretMaterialization(ctx, recoverer, platformrepo.RuntimeSecretRecoveryInput{OperationRef: prepared.OperationRef, Materialization: materialization})
		if err != nil || result.Action != want {
			t.Fatalf("old revision recovery=%s want=%s: %v", result.Action, want, err)
		}
	}
	recoverOld("KEEP")
	impact, err := service.GetRuntimeSecretImpact(ctx, owner, secret.Ref, rotated.CurrentRevision, "", query.Page{Size: 1})
	if err != nil || impact.Total != 2 || len(impact.Consumers) != 1 || impact.NextPageToken == "" {
		t.Fatalf("secret impact page: total=%d count=%d %v", impact.Total, len(impact.Consumers), err)
	}
	next, err := service.GetRuntimeSecretImpact(ctx, owner, secret.Ref, rotated.CurrentRevision, "", query.Page{Size: 1, Token: impact.NextPageToken})
	if err != nil || len(next.Consumers) != 1 || next.Consumers[0].Consumer.AgentRef == impact.Consumers[0].Consumer.AgentRef {
		t.Fatalf("secret impact cursor: %v", err)
	}
	if _, err := service.GetRuntimeSecretImpact(ctx, owner, secret.Ref, 1, "", query.Page{Size: 1, Token: impact.NextPageToken}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("secret target cursor reused: %v", err)
	}
	if _, err := service.GetRuntimeSecretImpact(ctx, owner, secret.Ref, rotated.CurrentRevision, "changed", query.Page{Size: 1, Token: impact.NextPageToken}); !errors.Is(err, errs.ErrInvalid) {
		t.Fatalf("secret impact cursor crossed search: %v", err)
	}
	filtered, err := service.GetRuntimeSecretImpact(ctx, owner, secret.Ref, rotated.CurrentRevision, impact.Consumers[0].Consumer.AgentRef, query.Page{Size: 1})
	if err != nil || filtered.Total != 1 || len(filtered.Consumers) != 1 || filtered.NextPageToken != "" {
		t.Fatalf("secret impact SQL search: total=%d err=%v", filtered.Total, err)
	}
	selection := entity.RuntimeSecretRebindSelection{EnvironmentRef: environment.Ref, ExpectedEnvironmentVersion: environment.Version, SourceVersionRef: environment.CurrentVersion.Ref, Consumers: append([]entity.RuntimeEnvironmentConsumer(nil), consumers...)}
	selection.Consumers[1].BindingVersion++
	rebind := command.Command{Kind: command.RebindRuntimeSecret, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "secret-impact-stale-batch", ExpectedVersion: &rotated.Version},
		Payload: command.RuntimeSecretRebindInput{SecretRef: secret.Ref, Revision: rotated.CurrentRevision, Selections: []entity.RuntimeSecretRebindSelection{selection}}}
	if _, err := service.Execute(ctx, rebind); !errors.Is(err, errs.ErrVersionMismatch) {
		t.Fatalf("stale secret batch: %v", err)
	}
	unchanged, err := service.GetRuntimeEnvironment(ctx, owner, environment.Ref)
	if err != nil || unchanged.Version != environment.Version {
		t.Fatalf("stale batch leaked publication: %v", err)
	}
	selection.Consumers = consumers[:1]
	rebind.Mutation.IdempotencyKey = "secret-impact-selected"
	rebind.Payload = command.RuntimeSecretRebindInput{SecretRef: secret.Ref, Revision: rotated.CurrentRevision, Selections: []entity.RuntimeSecretRebindSelection{selection}}
	rebound, err := service.Execute(ctx, rebind)
	if err != nil || len(rebound.RuntimeEnvironments) != 1 || len(rebound.EnvironmentBindings) != 1 {
		t.Fatalf("selected secret rebind: %v", err)
	}
	if _, err := service.Execute(ctx, rebind); err != nil {
		t.Fatalf("selected secret replay: %v", err)
	}
	for index, c := range consumers {
		view, err := service.GetAgentRuntimeConfiguration(ctx, owner, c.AgentRef)
		if err != nil {
			t.Fatal(err)
		}
		want := int64(1)
		if index == 0 {
			want = rotated.CurrentRevision
		}
		if len(view.Environment.CurrentVersion.SecretDescriptors) != 1 || view.Environment.CurrentVersion.SecretDescriptors[0].Revision != want {
			t.Fatalf("consumer %d secret revision changed incorrectly", index)
		}
	}
	recoverOld("KEEP")
	c := consumers[1]
	if _, err := service.Execute(ctx, command.Command{Kind: command.BindAgentRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "secret-impact-last-consumer", ExpectedVersion: &c.AgentVersion},
		Payload: command.RuntimeEnvironmentBindingInput{AgentRef: c.AgentRef, EnvironmentRef: environment.Ref, VersionRef: rebound.RuntimeEnvironments[0].CurrentVersion.Ref}}); err != nil {
		t.Fatal(err)
	}
	recoverOld("DELETE")
	if _, err := pool.Exec(ctx, `UPDATE control_plane.runtime_secret_revisions SET state='ACTIVE'
WHERE secret_id=(SELECT id FROM control_plane.runtime_secrets WHERE ref=$1) AND revision=1`, secret.Ref); err == nil {
		t.Fatal("retired revision was reactivated")
	}
	recoverOld("DELETE")
	environmentInput.Name = "Retired secret must not return"
	if _, err := service.Execute(ctx, command.Command{Kind: command.CreateRuntimeEnvironment, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "secret-impact-retired"}, Payload: environmentInput}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("retired revision was bound: %v", err)
	}
}
