package platform

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/libs/go/runtimesecret"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sshComposeComponentFixture owns a disposable migrated database and metadata-only secret operations.
// sshComposeComponentFixture владеет disposable мигрированной БД и операциями только с metadata секрета.
type sshComposeComponentFixture struct {
	ctx                   context.Context
	pool                  *pgxpool.Pool
	repository            *Repository
	service               *platformservice.Service
	owner                 value.Principal
	current               scope
	projectRef, projectID string
	imageArtifactRef      string
}

// newSSHComposeComponentFixture admits only the existing loopback disposable template and starts no external worker.
// newSSHComposeComponentFixture допускает только штатную loopback disposable template без внешнего worker.
func newSSHComposeComponentFixture(t *testing.T) *sshComposeComponentFixture {
	t.Helper()
	dsn := isolatedAssistantComponentDSN(t)
	ctx, cancel := context.WithTimeout(t.Context(), 120*time.Second)
	t.Cleanup(cancel)
	configuration, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal("parse disposable SSH fixture")
	}
	configuration.MaxConns = 6
	pool, err := pgxpool.NewWithConfig(ctx, configuration)
	if err != nil {
		t.Fatal("open disposable SSH fixture")
	}
	t.Cleanup(pool.Close)
	repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	t.Log("SSH fixture stage: bootstrap")
	if err = repository.Bootstrap(ctx); err != nil {
		t.Fatalf("SSH fixture bootstrap: %v", err)
	}
	t.Log("SSH fixture stage: synthetic catalog observation")
	seedObservedCatalogFixture(t, ctx, repository)
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("SSH fixture stage: owner proof identity")
	owner := resolvedTestPrincipal(t, ctx, repository, port.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.projects.create"}, "control-api-gateway")
	owner.CredentialAuthenticatedAt = time.Now().UTC()
	owner.CredentialACR, owner.CredentialAMR = "urn:kodex:acr:interactive", []string{"pwd"}
	t.Log("SSH fixture stage: project creation")
	project, err := service.Execute(ctx, command.Command{Kind: command.CreateProject, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "ssh-component-project"}, Payload: command.ProjectInput{Name: "SSH synthetic component", Language: "en"}})
	if err != nil || project.Project == nil {
		t.Fatalf("create SSH fixture project: %v", err)
	}
	t.Log("SSH fixture stage: transport UUID to owner public identity")
	resolvedOwner, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatalf("SSH fixture principal normalization: %v", err)
	}
	current, err := repository.resolveScope(ctx, resolvedOwner)
	if err != nil {
		t.Fatalf("SSH fixture public owner scope: %v", err)
	}
	fixture := &sshComposeComponentFixture{ctx: ctx, pool: pool, repository: repository, service: service, owner: owner, current: current, projectRef: project.Project.Ref}
	if err = pool.QueryRow(ctx, `SELECT id::text FROM control_plane.projects WHERE ref=$1`, fixture.projectRef).Scan(&fixture.projectID); err != nil {
		t.Fatal(err)
	}
	helper := fixture.execute(t, owner, command.CreateProjectAssistant, "environment-helper", nil, command.ProjectAssistantInput{ProjectRef: fixture.projectRef, Name: "SSH custody helper", Purpose: "Native environment image fixture", Instructions: "Return only assigned synthetic metadata."}).ProjectAssistant
	if helper == nil {
		t.Fatal("SSH fixture project helper missing")
	}
	base, err := service.GetAgentRuntimeConfiguration(ctx, owner, helper.AgentRef)
	if err != nil || base.Environment.CurrentVersion.Image.ArtifactRef == "" {
		t.Fatal("SSH fixture native environment image missing")
	}
	fixture.imageArtifactRef = base.Environment.CurrentVersion.Image.ArtifactRef
	return fixture
}

// createSecret exercises real owner prepare/claim/complete with synthetic descriptors and no private bytes.
// createSecret проверяет реальные owner prepare/claim/complete с синтетическими дескрипторами без private bytes.
func (fixture *sshComposeComponentFixture) createSecret(t *testing.T, name, purpose string, hint bool) entity.RuntimeSecret {
	t.Helper()
	prepared, err := fixture.service.PrepareRuntimeSecretOperation(fixture.ctx, runtimeSecretOwnerPrincipal(fixture.owner, "secret.create"), port.RuntimeSecretPrepareInput{Purpose: purpose, Kind: "CREATE", ProjectRef: fixture.projectRef, Name: name, ValueType: "STRING", ExpectedContentSHA256: runtimeSecretHashA, Mutation: value.Mutation{IdempotencyKey: "ssh-create-" + name}})
	if err != nil {
		t.Fatalf("prepare synthetic SSH secret: %v", err)
	}
	consume := runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secrets.operations.consume")
	claim, err := fixture.service.ConsumeRuntimeSecretOperation(fixture.ctx, consume, port.RuntimeSecretConsumeInput{OperationGrant: prepared.OperationGrant, ClaimantID: "ssh-secret-fixture"})
	if err != nil {
		t.Fatal(err)
	}
	secretName, err := runtimesecret.VersionedKubernetesName(claim.SecretRef, claim.TargetRevision)
	if err != nil {
		t.Fatal(err)
	}
	descriptor := &entity.RuntimeSecretMaterialization{Namespace: "kodex-runtime", SecretName: secretName, SecretKey: "value", SecretUID: "10000000-0000-4000-8000-000000000183", SecretResourceVersion: "183", ContentSHA256: runtimeSecretHashA}
	if hint {
		descriptor.DisplayHint = &entity.RuntimeSecretDisplayHint{Prefix: "fx", Suffix: "tail"}
	}
	complete := runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secrets.operations.complete")
	result, err := fixture.service.CompleteRuntimeSecretOperation(fixture.ctx, complete, port.RuntimeSecretCompleteInput{OperationRef: prepared.OperationRef, ClaimantID: "ssh-secret-fixture", ClaimGeneration: claim.ClaimGeneration, Materialization: descriptor})
	if err != nil || result.Ref == "" {
		t.Fatalf("complete synthetic SSH secret: %v", err)
	}
	return result
}

// configuration selects one public synthetic destination without opening a network connection.
// configuration задаёт единственное синтетическое public назначение без сетевого подключения.
func (fixture *sshComposeComponentFixture) configuration(secret entity.RuntimeSecret) map[string]string {
	key := append([]byte{0, 0, 0, 11}, []byte("ssh-ed25519")...)
	key = append(key, 0, 0, 0, 32)
	key = append(key, bytes.Repeat([]byte{1}, 32)...)
	return map[string]string{"project_ref": fixture.projectRef, "owner": "example", "repository": "project", "address": "8.8.8.8", "user": "kdx_fixture", "host_key": "ssh-ed25519 " + base64.StdEncoding.EncodeToString(key), "published_port": "8080", "runtime_image": "docker.io/library/python@sha256:" + strings.Repeat("a", 64), "ssh_secret_ref": secret.Ref}
}

// reserve submits the real public owner command, including canonical admission, OCC, audit and event persistence.
// reserve выполняет штатную owner-команду с canonical admission, OCC, audit и domain event.
func (fixture *sshComposeComponentFixture) reserve(t *testing.T, secret entity.RuntimeSecret) error {
	t.Helper()
	_, err := fixture.reserveWith(fixture.ctx, fixture.service, secret)
	return err
}

// reserveWith supports a traced native connection command without constructing business rows in SQL.
// reserveWith позволяет трассировать штатную connection-команду без создания business rows через SQL.
func (fixture *sshComposeComponentFixture) reserveWith(ctx context.Context, service *platformservice.Service, secret entity.RuntimeSecret) (string, error) {
	configuration := map[string]any{}
	for key, value := range fixture.configuration(secret) {
		configuration[key] = value
	}
	result, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: fixture.owner,
		Mutation: value.Mutation{IdempotencyKey: "ssh-custody-reserve-" + secret.Ref},
		Payload:  command.ConnectionInput{DefinitionKey: "ssh-compose", Name: "SSH custody " + secret.Name, PublicConfiguration: configuration}})
	if err != nil {
		return "", err
	}
	if result.Connection == nil {
		return "", errs.ErrUnavailable
	}
	return result.Connection.Ref, nil
}

// retainEnvironment creates real immutable secret descriptors through the owner API and returns the environment locator.
// retainEnvironment создаёт настоящие immutable secret descriptors через owner API и возвращает ENV locator.
func (fixture *sshComposeComponentFixture) retainEnvironment(ctx context.Context, service *platformservice.Service, secret entity.RuntimeSecret) (string, error) {
	result, err := service.Execute(ctx, command.Command{Kind: command.CreateRuntimeEnvironment, Principal: fixture.owner,
		Mutation: value.Mutation{IdempotencyKey: "ssh-custody-environment-" + secret.Ref},
		Payload: command.RuntimeEnvironmentInput{ProjectRef: fixture.projectRef, Name: "SSH custody " + secret.Name,
			ImageArtifactRef: fixture.imageArtifactRef, Policy: runtimecontract.DefaultRuntimeEnvironmentPolicy(),
			SecretBindings: []entity.RuntimeSecretBinding{{Name: "APP_KEY", SecretRef: secret.Ref, Revision: secret.CurrentRevision}}}})
	if err != nil {
		return "", err
	}
	if result.RuntimeEnvironment == nil {
		return "", errs.ErrUnavailable
	}
	return result.RuntimeEnvironment.Ref, nil
}

// TestSSHComposeCustodyComponent verifies purpose, old ENV exclusion, replay and both binding/reservation race orders.
// TestSSHComposeCustodyComponent проверяет purpose, запрет старого ENV, replay и оба порядка гонки binding/reservation.
func TestSSHComposeCustodyComponent(t *testing.T) {
	fixture := newSSHComposeComponentFixture(t)
	t.Run("write-only draft publication preserves custody and replay", func(t *testing.T) {
		owner := runtimeSecretOwnerPrincipal(fixture.owner, "secret.create")
		claimDraft := func(receipt entity.RuntimeSecretDraftOperationReceipt) entity.RuntimeSecretDraftWork {
			t.Helper()
			work, err := fixture.service.ConsumeRuntimeSecretDraft(fixture.ctx, runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secret-drafts.operations.consume"), port.RuntimeSecretDraftWorkInput{OperationGrant: receipt.OperationGrant, ClaimantID: "ssh-draft-fixture"})
			if err != nil {
				t.Fatal(err)
			}
			return work
		}
		finish := func(work entity.RuntimeSecretDraftWork, encrypted *entity.RuntimeSecretDraftEncryptedDescriptor, materialized *entity.RuntimeSecretMaterialization) entity.RuntimeSecretDraftResult {
			t.Helper()
			result, err := fixture.service.FinishRuntimeSecretDraft(fixture.ctx, runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secret-drafts.operations.complete"), port.RuntimeSecretDraftWorkInput{Action: "COMPLETE", OperationRef: work.OperationRef, ClaimantID: work.ClaimantID, ClaimGeneration: work.ClaimGeneration, Encrypted: encrypted, Materialization: materialized})
			if err != nil {
				t.Fatal(err)
			}
			return result
		}
		receipt, err := fixture.service.PrepareRuntimeSecretDraft(fixture.ctx, owner, port.RuntimeSecretDraftPrepareInput{Purpose: "SSH_COMPOSE", Kind: "SAVE", ProjectRef: fixture.projectRef, Name: "ssh-write-only-draft", ValueType: "STRING", ExpectedContentSHA256: runtimeSecretHashA, Mutation: value.Mutation{IdempotencyKey: "ssh-draft-save"}})
		if err != nil {
			t.Fatal(err)
		}
		work := claimDraft(receipt)
		var purpose string
		if err = fixture.pool.QueryRow(fixture.ctx, `SELECT purpose FROM control_plane.runtime_secrets WHERE ref=$1`, work.Draft.SecretRef).Scan(&purpose); err != nil || purpose != "SSH_COMPOSE" {
			t.Fatal("draft did not reserve custody before value")
		}
		encrypted := &entity.RuntimeSecretDraftEncryptedDescriptor{Namespace: work.StagedNamespace, SecretName: work.StagedSecretName, SecretKey: work.StagedSecretKey, SecretUID: "ssh-ciphertext-fixture", SecretResourceVersion: "1", CiphertextSHA256: runtimeSecretHashB, EncryptionKeyID: "synthetic-draft-key", EncryptionKeyGeneration: 1}
		saved := finish(work, encrypted, nil)
		validate, err := fixture.service.PrepareRuntimeSecretDraft(fixture.ctx, owner, port.RuntimeSecretDraftPrepareInput{Kind: "VALIDATE", DraftRef: saved.Draft.Ref, ExpectedSecretVersion: saved.Draft.SecretVersion, Mutation: value.Mutation{IdempotencyKey: "ssh-draft-validate", ExpectedVersion: &saved.Draft.Version}})
		if err != nil {
			t.Fatal(err)
		}
		validated := finish(claimDraft(validate), encrypted, nil)
		plan, err := fixture.service.PrepareRuntimeSecretDraftImpact(fixture.ctx, owner, validated.Draft.Ref, value.Mutation{IdempotencyKey: "ssh-draft-impact", ExpectedVersion: &validated.Draft.Version})
		if err != nil || plan.Total != 0 {
			t.Fatalf("new SSH draft impact: %v", err)
		}
		publish, err := fixture.service.PrepareRuntimeSecretDraft(fixture.ctx, owner, port.RuntimeSecretDraftPrepareInput{Kind: "PUBLISH", DraftRef: validated.Draft.Ref, ExpectedSecretVersion: validated.Draft.SecretVersion, ImpactPlanRef: plan.Ref, Mutation: value.Mutation{IdempotencyKey: "ssh-draft-publish", ExpectedVersion: &validated.Draft.Version}})
		if err != nil {
			t.Fatal(err)
		}
		work = claimDraft(publish)
		name, err := runtimesecret.VersionedKubernetesName(work.Draft.SecretRef, work.TargetRevision)
		if err != nil {
			t.Fatal(err)
		}
		materialized := &entity.RuntimeSecretMaterialization{Namespace: work.Namespace, SecretName: name, SecretKey: "value", SecretUID: "ssh-published-fixture", SecretResourceVersion: "2", ContentSHA256: runtimeSecretHashA, DisplayHint: &entity.RuntimeSecretDisplayHint{Prefix: "fx", Suffix: "tail"}}
		for repeat := 0; repeat < 2; repeat++ {
			published := finish(work, encrypted, materialized)
			if published.Secret == nil || published.Secret.Purpose != "SSH_COMPOSE" || published.Secret.DisplayHint != nil || published.Draft.State != "PUBLISHED" {
				t.Fatal("draft publication/replay lost custody")
			}
			for _, action := range published.Secret.NextActions {
				if action == "REVEAL" {
					t.Fatal("published SSH draft offered reveal")
				}
			}
		}
	})
	t.Run("purpose before value blocks reveal and environment", func(t *testing.T) {
		secret := fixture.createSecret(t, "ssh-purpose", "SSH_COMPOSE", true)
		if secret.Purpose != "SSH_COMPOSE" || secret.DisplayHint != nil {
			t.Fatal("SSH completion exposed a hint or lost purpose")
		}
		for _, action := range secret.NextActions {
			if action == "REVEAL" {
				t.Fatal("SSH metadata offered reveal")
			}
		}
		if err := fixture.reserve(t, secret); err != nil {
			t.Fatalf("reserve SSH purpose: %v", err)
		}
		_, err := fixture.service.PrepareRuntimeSecretOperation(fixture.ctx, runtimeSecretOwnerPrincipal(fixture.owner, "secret.reveal"), port.RuntimeSecretPrepareInput{Kind: "REVEAL", SecretRef: secret.Ref, Mutation: value.Mutation{IdempotencyKey: "ssh-forbidden-reveal", ExpectedVersion: &secret.Version}})
		if !errors.Is(err, errs.ErrForbidden) {
			t.Fatalf("SSH reveal was not refused: %v", err)
		}
		tx, err := fixture.pool.Begin(fixture.ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, _, _, _, err = fixture.repository.resolveEnvironmentPayload(fixture.ctx, tx, fixture.current.organizationID, fixture.projectID, nil, []entity.RuntimeSecretBinding{{Name: "SSH_KEY", SecretRef: secret.Ref}})
		_ = tx.Rollback(fixture.ctx)
		if !errors.Is(err, errs.ErrNotFound) {
			t.Fatalf("SSH ENV binding was not refused: %v", err)
		}
		// Schema-negative не создаёт fixture state: даже ошибочно разрешённый UPDATE всегда откатывается.
		guard, err := fixture.pool.Begin(fixture.ctx)
		if err != nil {
			t.Fatal(err)
		}
		_, downgradeErr := guard.Exec(fixture.ctx, `UPDATE control_plane.runtime_secrets SET purpose='APPLICATION' WHERE ref=$1`, secret.Ref)
		_ = guard.Rollback(fixture.ctx)
		if downgradeErr == nil {
			t.Fatal("SSH purpose downgrade accepted")
		}
		descriptor := secret.CurrentRevisionDescriptor
		var ref string
		err = fixture.pool.QueryRow(fixture.ctx, queryCredentialProjectionResolveRuntimeSecret, pgx.StrictNamedArgs{"organization_id": fixture.current.organizationID, "scope_kind": "PROJECT", "project_id": fixture.projectID, "secret_name": descriptor.SecretName, "secret_key": descriptor.SecretKey, "secret_uid": descriptor.SecretUID, "secret_resource_version": descriptor.SecretResourceVersion, "content_sha256": descriptor.ContentSHA256}).Scan(&ref)
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("old runtime projection admitted SSH purpose: %v", err)
		}
		rotated := completeRuntimeSecretRotate(t, fixture.ctx, fixture.service, runtimeSecretOwnerPrincipal(fixture.owner, "secret.rotate"), runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secrets.operations.consume"), runtimeSecretSystemPrincipal(t, fixture.ctx, fixture.repository, "platform.runtime-secrets.operations.complete"), secret, runtimeSecretHashB, "ssh-purpose-rotate")
		if rotated.Purpose != "SSH_COMPOSE" || rotated.DisplayHint != nil {
			t.Fatal("rotation lost SSH custody")
		}
	})
	t.Run("exposed and deleted historical application credentials cannot be adopted", func(t *testing.T) {
		hinted := fixture.createSecret(t, "app-hint", "APPLICATION", true)
		if err := fixture.reserve(t, hinted); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("exposed application key adopted: %v", err)
		}
		secret := fixture.createSecret(t, "app-old-env", "APPLICATION", false)
		ref, err := fixture.retainEnvironment(fixture.ctx, fixture.service, secret)
		if err != nil {
			t.Fatal(err)
		}
		environment, err := fixture.service.GetRuntimeEnvironment(fixture.ctx, fixture.owner, ref)
		if err != nil {
			t.Fatal(err)
		}
		disabled := fixture.execute(t, fixture.owner, command.SetRuntimeEnvironmentEnabled, "historical-disable", &environment.Version, command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: ref, Enabled: false}).RuntimeEnvironment
		if disabled == nil {
			t.Fatal("disabled historical environment missing")
		}
		fixture.execute(t, fixture.owner, command.DeleteRuntimeEnvironment, "historical-delete", &disabled.Version, command.RuntimeEnvironmentLifecycleInput{EnvironmentRef: ref})
		if err = fixture.reserve(t, secret); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("historically exposed ENV key adopted: %v", err)
		}
	})
	t.Run("native binding holds custody against nonblocking reservation", func(t *testing.T) {
		secret := fixture.createSecret(t, "binding-first", "APPLICATION", false)
		publisher, release, published := startHistoricalSecretCall(t, fixture.ctx, fixture.repository, "runtime_secret_resolve_binding", false,
			func(ctx context.Context, service *platformservice.Service) (string, error) {
				return fixture.retainEnvironment(ctx, service, secret)
			})
		awaitHistoricalSecretPhase(t, fixture.ctx, publisher, published)
		if err := fixture.reserve(t, secret); !errors.Is(err, errs.ErrUnavailable) {
			t.Fatalf("contended custody did not roll back: %v", err)
		}
		release()
		if result := awaitHistoricalSecretResult(t, fixture.ctx, published); result.err != nil || result.action == "" {
			t.Fatalf("winning native ENV publication: %v", result.err)
		}
		if err := fixture.reserve(t, secret); !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("reservation missed committed ENV: %v", err)
		}
	})
	t.Run("native reservation commits before waiting binding", func(t *testing.T) {
		secret := fixture.createSecret(t, "reservation-first", "APPLICATION", false)
		reservation, release, reserved := startHistoricalSecretCall(t, fixture.ctx, fixture.repository, "ssh_compose__secret_reserve", false,
			func(ctx context.Context, service *platformservice.Service) (string, error) {
				return fixture.reserveWith(ctx, service, secret)
			})
		reservationPID := awaitHistoricalSecretPhase(t, fixture.ctx, reservation, reserved)
		publisher, _, published := startHistoricalSecretCall(t, fixture.ctx, fixture.repository, "runtime_secret_resolve_binding", true,
			func(ctx context.Context, service *platformservice.Service) (string, error) {
				return fixture.retainEnvironment(ctx, service, secret)
			})
		publisherPID := awaitHistoricalSecretPhase(t, fixture.ctx, publisher, published)
		awaitHistoricalSecretLock(t, fixture.ctx, fixture.pool, publisherPID, reservationPID, published)
		release()
		if result := awaitHistoricalSecretResult(t, fixture.ctx, reserved); result.err != nil || result.action == "" {
			t.Fatalf("winning native SSH reservation: %v", result.err)
		}
		if result := awaitHistoricalSecretResult(t, fixture.ctx, published); !errors.Is(result.err, errs.ErrNotFound) {
			t.Fatalf("ENV ignored committed SSH custody: %v", result.err)
		}
	})
}
