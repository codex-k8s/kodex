package platform

import (
	"context"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// historicalRecoveryFixture owns only synthetic descriptor metadata and native commands for one producer/project; it never materializes Kubernetes objects or exposes secret values.
// historicalRecoveryFixture владеет лишь синтетическими descriptor metadata и native commands одного producer/project; Kubernetes objects не создаются, значения секретов не раскрываются.
type historicalRecoveryFixture struct {
	repository                                                       *Repository
	service                                                          *platformservice.Service
	owner                                                            value.Principal
	producer, key, projectRef, agentRef, operationRef, historicalRef string
	legacyGeneration                                                 int64
	work                                                             entity.RuntimeSecretDraftWork
	encrypted                                                        *entity.RuntimeSecretDraftEncryptedDescriptor
	material                                                         entity.RuntimeSecretMaterialization
	secret                                                           entity.RuntimeSecret
	base                                                             entity.AgentRuntimeConfigurationView
	environment                                                      entity.RuntimeEnvironmentSet
	contents                                                         command.RuntimeEnvironmentInput
}

// execute applies one native owner command with a unique stable fixture key and the caller's explicit optimistic version.
// execute выполняет одну native owner command с уникальным устойчивым fixture key и явно переданной optimistic version.
func (f *historicalRecoveryFixture) execute(t *testing.T, ctx context.Context, kind command.Kind, key string, version *int64, payload any) command.Result {
	t.Helper()
	result, err := f.service.Execute(ctx, command.Command{Kind: kind, Principal: f.owner,
		Mutation: value.Mutation{IdempotencyKey: "historical-recovery-" + f.key + "-" + key, ExpectedVersion: version}, Payload: payload})
	if err != nil {
		t.Fatalf("native recovery fixture %s: %v", key, err)
	}
	return result
}

// broker resolves the existing exact internal operation permission for the synthetic secret-broker principal.
// broker разрешает существующее точное внутреннее operation permission синтетического principal secret-broker.
func (f *historicalRecoveryFixture) broker(t *testing.T, ctx context.Context, operation string) value.Principal {
	t.Helper()
	return runtimeSecretSystemPrincipal(t, ctx, f.repository, operation)
}

// newHistoricalRecoveryFixture creates CREATE-CLAIMED or SAVE/VALIDATE/PUBLISH-CLAIMED work through native ports; optional completion uses the same owner callback exercised by live brokers.
// newHistoricalRecoveryFixture создаёт CREATE-CLAIMED либо SAVE/VALIDATE/PUBLISH-CLAIMED work штатными ports; необязательное завершение использует тот же owner callback, что live brokers.
func newHistoricalRecoveryFixture(t *testing.T, ctx context.Context, r *Repository, s *platformservice.Service, owner value.Principal, producer, key string, completed bool) *historicalRecoveryFixture {
	t.Helper()
	f := &historicalRecoveryFixture{repository: r, service: s, owner: owner, producer: producer, key: key}
	f.projectRef = f.execute(t, ctx, command.CreateProject, "project", nil, command.ProjectInput{Name: "Recovery " + key, Language: "en"}).Project.Ref
	f.agentRef = f.execute(t, ctx, command.CreateProjectAssistant, "helper", nil, command.ProjectAssistantInput{ProjectRef: f.projectRef, Name: "Recovery helper", Purpose: "Bounded recovery fixture", Instructions: "Use only explicitly assigned resources."}).ProjectAssistant.AgentRef
	var err error
	f.base, err = s.GetAgentRuntimeConfiguration(ctx, owner, f.agentRef)
	if err != nil || f.base.Environment.CurrentVersion.Image.ArtifactRef == "" {
		t.Fatal("native recovery fixture has no project image")
	}
	if producer == "legacy" {
		prepared, err := s.PrepareRuntimeSecretOperation(ctx, runtimeSecretOwnerPrincipal(owner, "secret.create"), port.RuntimeSecretPrepareInput{Kind: "CREATE", ProjectRef: f.projectRef, Name: "Recovery marker", ValueType: "STRING", ExpectedContentSHA256: runtimeSecretHashA, Mutation: value.Mutation{IdempotencyKey: "historical-recovery-" + key + "-create"}})
		if err != nil {
			t.Fatalf("prepare legacy recovery marker: %v", err)
		}
		work, err := s.ConsumeRuntimeSecretOperation(ctx, f.broker(t, ctx, "platform.runtime-secrets.operations.consume"), port.RuntimeSecretConsumeInput{OperationGrant: prepared.OperationGrant, ClaimantID: "historical-recovery-broker"})
		if err != nil {
			t.Fatalf("claim legacy recovery marker: %v", err)
		}
		f.operationRef, f.legacyGeneration = prepared.OperationRef, work.ClaimGeneration
		f.secret.Ref = work.SecretRef
	} else {
		prepared, err := s.PrepareRuntimeSecretDraft(ctx, owner, port.RuntimeSecretDraftPrepareInput{Kind: "SAVE", ProjectRef: f.projectRef, Name: "Recovery draft", ValueType: "STRING", ExpectedContentSHA256: runtimeSecretHashA, Mutation: value.Mutation{IdempotencyKey: "historical-recovery-" + key + "-save"}})
		if err != nil {
			t.Fatalf("prepare draft recovery marker: %v", err)
		}
		savedWork, err := s.ConsumeRuntimeSecretDraft(ctx, f.broker(t, ctx, "platform.runtime-secret-drafts.operations.consume"), port.RuntimeSecretDraftWorkInput{OperationGrant: prepared.OperationGrant, ClaimantID: "historical-recovery-draft-broker"})
		if err != nil {
			t.Fatalf("claim saved draft: %v", err)
		}
		f.encrypted = &entity.RuntimeSecretDraftEncryptedDescriptor{Namespace: savedWork.StagedNamespace, SecretName: savedWork.StagedSecretName, SecretKey: savedWork.StagedSecretKey, SecretUID: "historical-ciphertext-uid", SecretResourceVersion: "10", CiphertextSHA256: runtimeSecretHashB, EncryptionKeyID: "fixture-key", EncryptionKeyGeneration: 1}
		saved, err := s.FinishRuntimeSecretDraft(ctx, f.broker(t, ctx, "platform.runtime-secret-drafts.operations.complete"), port.RuntimeSecretDraftWorkInput{Action: "COMPLETE", OperationRef: savedWork.OperationRef, ClaimantID: savedWork.ClaimantID, ClaimGeneration: savedWork.ClaimGeneration, Encrypted: f.encrypted})
		if err != nil || saved.Draft.State != "DRAFT" {
			t.Fatalf("complete saved draft: %v", err)
		}
		draft := saved.Draft
		for _, kind := range []string{"VALIDATE", "PUBLISH"} {
			input := port.RuntimeSecretDraftPrepareInput{Kind: kind, DraftRef: draft.Ref, ExpectedSecretVersion: draft.SecretVersion, Mutation: value.Mutation{IdempotencyKey: "historical-recovery-" + key + "-" + kind, ExpectedVersion: &draft.Version}}
			if kind == "PUBLISH" {
				impact, err := s.PrepareRuntimeSecretDraftImpact(ctx, owner, draft.Ref, value.Mutation{IdempotencyKey: "historical-recovery-" + key + "-impact", ExpectedVersion: &draft.Version})
				if err != nil || impact.Total != 0 {
					t.Fatalf("prepare empty draft impact: %v", err)
				}
				input.ImpactPlanRef = impact.Ref
			}
			receipt, err := s.PrepareRuntimeSecretDraft(ctx, owner, input)
			if err != nil {
				t.Fatalf("prepare draft %s: %v", kind, err)
			}
			work, err := s.ConsumeRuntimeSecretDraft(ctx, f.broker(t, ctx, "platform.runtime-secret-drafts.operations.consume"), port.RuntimeSecretDraftWorkInput{OperationGrant: receipt.OperationGrant, ClaimantID: "historical-recovery-draft-broker"})
			if err != nil {
				t.Fatalf("claim draft %s: %v", kind, err)
			}
			if kind == "PUBLISH" {
				f.work = work
				f.operationRef = work.OperationRef
				f.secret.Ref = work.Draft.SecretRef
				break
			}
			validated, err := s.FinishRuntimeSecretDraft(ctx, f.broker(t, ctx, "platform.runtime-secret-drafts.operations.complete"), port.RuntimeSecretDraftWorkInput{Action: "COMPLETE", OperationRef: work.OperationRef, ClaimantID: work.ClaimantID, ClaimGeneration: work.ClaimGeneration, Encrypted: f.encrypted})
			if err != nil || validated.Draft.State != "VALID" {
				t.Fatalf("complete draft validation: %v", err)
			}
			draft = validated.Draft
		}
	}
	name, err := runtimesecret.VersionedKubernetesName(f.secret.Ref, 1)
	if err != nil {
		t.Fatal(err)
	}
	f.material = entity.RuntimeSecretMaterialization{Namespace: "kodex-runtime", SecretName: name, SecretKey: "value", SecretUID: "historical-runtime-uid", SecretResourceVersion: "20", ContentSHA256: runtimeSecretHashA}
	if completed {
		f.complete(t, ctx)
		f.ackCiphertext(t, ctx)
	}
	return f
}

// complete performs the original CLAIMED completion using its real grant-derived claimant/generation; no SQL state or clock write is used.
// complete выполняет исходное CLAIMED completion с настоящими grant-derived claimant/generation; SQL state и clocks не подменяются.
func (f *historicalRecoveryFixture) complete(t *testing.T, ctx context.Context) {
	t.Helper()
	if f.producer == "legacy" {
		secret, err := f.service.CompleteRuntimeSecretOperation(ctx, f.broker(t, ctx, "platform.runtime-secrets.operations.complete"), port.RuntimeSecretCompleteInput{OperationRef: f.operationRef, ClaimantID: "historical-recovery-broker", ClaimGeneration: f.legacyGeneration, Materialization: &f.material})
		if err != nil {
			t.Fatalf("complete original legacy claim: %v", err)
		}
		f.secret = secret
		return
	}
	result, err := f.service.FinishRuntimeSecretDraft(ctx, f.broker(t, ctx, "platform.runtime-secret-drafts.operations.complete"), port.RuntimeSecretDraftWorkInput{Action: "COMPLETE", OperationRef: f.operationRef, ClaimantID: f.work.ClaimantID, ClaimGeneration: f.work.ClaimGeneration, Encrypted: f.encrypted, Materialization: &f.material})
	if err != nil || result.Secret == nil || result.Draft.State != "PUBLISHED" {
		t.Fatalf("complete original draft claim: %v", err)
	}
	f.secret = *result.Secret
}

// recover calls the unchanged public internal owner operation with an explicit observed materialization; the legacy RPC intentionally has no caller claimant/generation fields.
// recover вызывает прежнюю публичную внутреннюю owner operation с явно наблюдаемой materialization; legacy RPC намеренно не имеет caller claimant/generation fields.
func (f *historicalRecoveryFixture) recover(ctx context.Context, s *platformservice.Service, principal value.Principal, material entity.RuntimeSecretMaterialization) (string, error) {
	if f.producer == "legacy" {
		result, err := s.RecoverRuntimeSecretMaterialization(ctx, principal, port.RuntimeSecretRecoveryInput{OperationRef: f.operationRef, Materialization: material})
		return result.Action, err
	}
	result, err := s.FinishRuntimeSecretDraft(ctx, principal, port.RuntimeSecretDraftWorkInput{Action: "RECOVER", OperationRef: f.operationRef, ClaimantID: f.work.ClaimantID, ClaimGeneration: f.work.ClaimGeneration, Materialization: &material})
	return result.MaterializationAction, err
}

// recoveryPrincipal selects only the existing producer-specific method permission without granting a model any credential or capability.
// recoveryPrincipal выбирает лишь существующее producer-specific method permission, не выдавая модели credential либо capability.
func (f *historicalRecoveryFixture) recoveryPrincipal(t *testing.T, ctx context.Context) value.Principal {
	t.Helper()
	operation := "platform.runtime-secrets.operations.recover"
	if f.producer == "draft" {
		operation = "platform.runtime-secret-drafts.materialization.recover"
	}
	return f.broker(t, ctx, operation)
}

// ackCiphertext settles only the published draft's encrypted staging cleanup; it never treats published materialization KEEP as permission to delete that material.
// ackCiphertext завершает лишь encrypted staging cleanup опубликованного draft; KEEP опубликованной materialization не трактуется как разрешение удалить её.
func (f *historicalRecoveryFixture) ackCiphertext(t *testing.T, ctx context.Context) {
	t.Helper()
	if f.producer != "draft" {
		return
	}
	input := port.RuntimeSecretDraftWorkInput{Action: "RECOVER", OperationRef: f.operationRef, ClaimantID: f.work.ClaimantID, ClaimGeneration: f.work.ClaimGeneration, Encrypted: f.encrypted, Materialization: &f.material}
	result, err := f.service.FinishRuntimeSecretDraft(ctx, f.recoveryPrincipal(t, ctx), input)
	if err != nil || result.MaterializationAction != "KEEP" || result.EncryptedAction != "DELETE" {
		t.Fatalf("prepare independent ciphertext cleanup: %v", err)
	}
	input.Action, input.Materialization = "CLEANUP", nil
	if _, err := f.service.FinishRuntimeSecretDraft(ctx, f.broker(t, ctx, "platform.runtime-secret-drafts.cleanup.complete"), input); err != nil {
		t.Fatalf("ACK ciphertext cleanup: %v", err)
	}
}

// prepareHistorical makes R1 noncurrent and unreferenced through native create/rotate/publish commands while keeping its immutable V1 history available.
// prepareHistorical делает R1 нетекущей и неиспользуемой штатными create/rotate/publish commands, сохраняя immutable V1 history доступной.
func (f *historicalRecoveryFixture) prepareHistorical(t *testing.T, ctx context.Context) {
	t.Helper()
	f.contents = command.RuntimeEnvironmentInput{ProjectRef: f.projectRef, Name: "Historical recovery environment", ImageArtifactRef: f.base.Environment.CurrentVersion.Image.ArtifactRef, Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy(), SecretBindings: []entity.RuntimeSecretBinding{{Name: "RECOVERY_MARKER", SecretRef: f.secret.Ref, Revision: 1}}}
	f.environment = *f.execute(t, ctx, command.CreateRuntimeEnvironment, "environment-v1", nil, f.contents).RuntimeEnvironment
	f.historicalRef = f.environment.CurrentVersion.Ref
	f.secret = completeRuntimeSecretRotate(t, ctx, f.service, runtimeSecretOwnerPrincipal(f.owner, "secret.rotate"), f.broker(t, ctx, "platform.runtime-secrets.operations.consume"), f.broker(t, ctx, "platform.runtime-secrets.operations.complete"), f.secret, runtimeSecretHashB, "historical-recovery-"+f.key+"-rotate")
	empty := f.contents
	empty.Ref, empty.SecretBindings = f.environment.Ref, nil
	f.environment = *f.execute(t, ctx, command.PublishRuntimeEnvironment, "environment-v2-empty", &f.environment.Version, empty).RuntimeEnvironment
	f.contents.Ref = f.environment.Ref
}

// publishR1 returns a native owner call suitable for a traced service; its already-held canonical resolver SHARE locks provide the admission-wins barrier.
// publishR1 возвращает native owner call для traced service; уже удерживаемые canonical resolver SHARE locks образуют admission-wins barrier.
func (f *historicalRecoveryFixture) publishR1(ctx context.Context, s *platformservice.Service) (string, error) {
	result, err := s.Execute(ctx, command.Command{Kind: command.PublishRuntimeEnvironment, Principal: f.owner, Mutation: value.Mutation{IdempotencyKey: "historical-recovery-" + f.key + "-publish-r1", ExpectedVersion: &f.environment.Version}, Payload: f.contents})
	if err != nil {
		return "", err
	}
	if result.RuntimeEnvironment == nil {
		return "", errs.ErrUnavailable
	}
	return "PUBLISHED", nil
}

// bindEmpty places the one consumer on V2 before testing the public historical rebind operation.
// bindEmpty размещает единственного consumer на V2 перед проверкой публичной historical rebind operation.
func (f *historicalRecoveryFixture) bindEmpty(t *testing.T, ctx context.Context) {
	t.Helper()
	fresh, err := f.service.GetAgentRuntimeConfiguration(ctx, f.owner, f.agentRef)
	if err != nil {
		t.Fatal("read consumer before empty bind")
	}
	f.execute(t, ctx, command.BindAgentRuntimeEnvironment, "bind-empty", &fresh.AgentVersion, command.RuntimeEnvironmentBindingInput{AgentRef: f.agentRef, EnvironmentRef: f.environment.Ref})
}

// rebindR1 obtains the real public impact snapshot and submits the historical target version through the specialized consumer-binding command.
// rebindR1 получает настоящий public impact snapshot и передаёт historical target version специализированной consumer-binding command.
func (f *historicalRecoveryFixture) rebindR1(t *testing.T, ctx context.Context) error {
	t.Helper()
	impact, err := f.service.GetRuntimeEnvironmentImpact(ctx, f.owner, f.environment.Ref, f.historicalRef, "", query.Page{Size: 10})
	if err != nil {
		return err
	}
	if len(impact.Consumers) != 1 || impact.Consumers[0].AgentRef != f.agentRef || impact.Consumers[0].VersionRef != f.environment.CurrentVersion.Ref {
		t.Fatal("public recovery rebind lost its exact current consumer snapshot")
	}
	_, err = f.service.Execute(ctx, command.Command{Kind: command.RebindRuntimeEnvironment, Principal: f.owner, Mutation: value.Mutation{IdempotencyKey: "historical-recovery-" + f.key + "-rebind-r1", ExpectedVersion: &f.environment.Version}, Payload: command.RuntimeEnvironmentRebindInput{EnvironmentRef: f.environment.Ref, VersionRef: f.historicalRef, Consumers: impact.Consumers}})
	return err
}
