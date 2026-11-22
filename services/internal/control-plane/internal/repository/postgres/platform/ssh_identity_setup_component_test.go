package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/libs/go/managedssh"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/enum"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// sshSetupNativeFixture uses the actual shipped SSH package and public owner-created Connection; no synthetic package IDs or readiness override enter admission.
// sshSetupNativeFixture использует actual shipped SSH package и public owner-created Connection; synthetic package IDs либо readiness override не участвуют в admission.
type sshSetupNativeFixture struct {
	ctx                      context.Context
	pool                     *pgxpool.Pool
	repository               *Repository
	service                  *platformservice.Service
	owner, broker, transport value.Principal
	connection               entity.IntegrationConnection
	shared                   bool
}

// querySSHSetupTestRevokedAttempt observes stored grant closure and current attachment without treating a derived view or lease expiry as an atomic cancellation.
// querySSHSetupTestRevokedAttempt наблюдает stored grant closure и current attachment, не подменяя atomic cancellation вычисленной view либо lease expiry.
//
//go:embed testdata/sql/ssh_setup_test__revoked_attempt.sql
var querySSHSetupTestRevokedAttempt string

// TestSSHIdentityRevocationOwnerComponent uses real self-revoke, disable, reconfiguration, policy and delete commands during the first pending WRITE, requiring same-transaction grant closure and preserving late Known receipts without attachment.
// TestSSHIdentityRevocationOwnerComponent выполняет real self-revoke, disable, reconfiguration, policy и delete при первом pending WRITE, требуя same-transaction grant closure и сохранения late Known receipts без attachment.
func TestSSHIdentityRevocationOwnerComponent(t *testing.T) {
	for _, change := range []string{"self-revoke", "disable", "configure", "policy", "delete"} {
		t.Run(change, func(t *testing.T) {
			f := newSSHSetupNativeFixture(t)
			owner := f.transport
			if change == "policy" || change == "delete" {
				owner.Permission = "platform.command.integrations.enable"
				disabled, err := f.service.Execute(f.ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner,
					Mutation: value.Mutation{IdempotencyKey: "ssh-native-initial-disable", ExpectedVersion: &f.connection.Version}, Payload: command.ConnectionInput{Ref: f.connection.Ref, Enabled: false}})
				if err != nil || disabled.Connection == nil {
					t.Fatal("disable before prepared intent", err)
				}
				f.connection = *disabled.Connection
			}
			setup := f.begin(t, "ssh-native-revoke-pending", 0)
			admission, _ := f.claim(t, setup)
			if time.Until(admission.ExpiresAt) < 5*time.Second {
				t.Fatal("revoke regression requires a fresh live WRITE lease")
			}
			commandInput := command.Command{Principal: owner, Mutation: value.Mutation{IdempotencyKey: "ssh-native-change-during-upload", ExpectedVersion: &f.connection.Version}}
			switch change {
			case "self-revoke":
				owner.Permission = "platform.personal-integrations.revoke"
				version := setup.View.BindingVersion
				commandInput.Kind = command.RevokePersonalIntegrationCredential
				commandInput.Mutation.ExpectedVersion = &version
				commandInput.Payload = command.PersonalIntegrationCredentialInput{ConnectionRef: f.connection.Ref}
			case "disable":
				owner.Permission = "platform.command.integrations.enable"
				commandInput.Kind = command.SetConnectionEnabled
				commandInput.Payload = command.ConnectionInput{Ref: f.connection.Ref, Enabled: false}
			case "configure":
				owner.Permission = "platform.command.integrations.update"
				commandInput.Kind = command.UpdateConnection
				commandInput.Payload = command.ConnectionInput{Ref: f.connection.Ref, Name: "Changed source-only SSH", PublicConfiguration: map[string]any{"target_ref": "target_native_successor"}}
			case "policy":
				owner.Permission = "platform.integrations.credential-policy.set"
				commandInput.Kind = command.SetIntegrationCredentialPolicy
				commandInput.Payload = command.IntegrationCredentialPolicyInput{ConnectionRef: f.connection.Ref, Policy: entity.IntegrationCredentialProjectShared}
			case "delete":
				owner.Permission = "platform.command.integrations.delete"
				commandInput.Kind = command.DeleteConnection
				commandInput.Payload = command.ConnectionInput{Ref: f.connection.Ref}
			}
			commandInput.Principal = owner
			result, err := f.service.Execute(f.ctx, commandInput)
			if err != nil {
				t.Fatal("owner lifecycle command during pending upload failed", err)
			}
			if change == "self-revoke" && (result.PersonalIntegrationCredential == nil || result.PersonalIntegrationCredential.State != "REVOKED") {
				t.Fatal("self-revoke did not retain revoked binding")
			}
			var setupState, attemptState, bindingState string
			var unbound bool
			if err := f.pool.QueryRow(f.ctx, querySSHSetupTestRevokedAttempt, setup.View.Ref, admission.AttemptRef).Scan(&setupState, &attemptState, &bindingState, &unbound); err != nil {
				t.Fatal("read closed SSH ledger", err)
			}
			if setupState != "CANCELLED" || attemptState != "CLOSED" || !unbound || change == "self-revoke" && bindingState != "REVOKED" {
				t.Errorf("owner change retained active SSH upload grant: setup=%s attempt=%s binding=%s unbound=%t", setupState, attemptState, bindingState, unbound)
			}
			locator := f.locator(setup)
			late, err := f.complete(admission, command.SSHIdentityUploadComplete{Identity: &locator})
			if err != nil || late.State != enum.SSHIdentitySetupCancelled {
				t.Fatal("late material receipt changed cancellation", err)
			}
			if err := f.pool.QueryRow(f.ctx, querySSHSetupTestRevokedAttempt, setup.View.Ref, admission.AttemptRef).Scan(&setupState, &attemptState, &bindingState, &unbound); err != nil || !unbound || attemptState != "CLOSED" || change == "self-revoke" && bindingState != "REVOKED" {
				t.Fatal("late material receipt restored SSH attachment or grant", err)
			}
		})
	}
}

// newSSHSetupNativeFixture bootstraps only a dedicated migrated loopback template, then resolves the authenticated synthetic actor through the ordinary owner boundary.
// newSSHSetupNativeFixture готовит лишь dedicated migrated loopback template, затем разрешает authenticated synthetic actor через ordinary owner boundary.
func newSSHSetupNativeFixture(t *testing.T) *sshSetupNativeFixture {
	return newSSHSetupNativeFixtureWithPolicy(t, false)
}

// newSSHSetupNativeFixtureWithPolicy selects personal custody or an explicit existing active SERVICE through actual public configuration, never an implicit administrative identity.
// newSSHSetupNativeFixtureWithPolicy выбирает personal custody либо explicit existing active SERVICE через actual public configuration, не implicit administrative identity.
func newSSHSetupNativeFixtureWithPolicy(t *testing.T, shared bool) *sshSetupNativeFixture {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	t.Cleanup(cancel)
	config, err := pgxpool.ParseConfig(isolatedAssistantComponentDSN(t))
	if err != nil {
		t.Fatal("parse SSH native fixture", err)
	}
	config.ConnConfig.Tracer = componentQueryRejectionTracer{t: t}
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal("connect SSH owner fixture", err)
	}
	t.Cleanup(pool.Close)
	repository := newConfiguredAssistantComponentRepository(t, pool, "gpt-5")
	if err := repository.ConfigureSSHIdentitySetup(value.SSHIdentityUploadEndpoint{Origin: "https://ssh-upload.example.invalid", TLSServerName: "ssh-upload.example.invalid"}); err != nil {
		t.Fatal("configure fixed setup endpoint", err)
	}
	if err := repository.Bootstrap(ctx); err != nil {
		t.Fatal("bootstrap actual SSH definitions", err)
	}
	service, err := platformservice.New(repository)
	if err != nil {
		t.Fatal(err)
	}
	owner := resolvedTestPrincipal(t, ctx, repository, platformrepo.ProofPrincipalInput{ExternalActorID: "20000000-0000-4000-8000-000000000001", ExternalTenantID: "20000000-0000-4000-8000-000000000002", CallerWorkload: "control-api-gateway", Operation: "platform.command.integrations.create"}, "control-api-gateway")
	configuration := map[string]any{"target_ref": "target_native_ssh"}
	if shared {
		configuration["service_subject_ref"] = "sys_platform"
	}
	created, err := service.Execute(ctx, command.Command{Kind: command.CreateConnection, Principal: owner, Mutation: value.Mutation{IdempotencyKey: "ssh-native-create-connection"}, Payload: command.ConnectionInput{DefinitionKey: integrationpackage.SSHManagedDefinitionKey, Name: "Disposable SSH identity", PublicConfiguration: configuration}})
	if err != nil || created.Connection == nil {
		t.Fatal("create actual SSH package Connection", err)
	}
	connection := *created.Connection
	owner.Permission = "platform.command.integrations.enable"
	disabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "ssh-native-disable", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref, Enabled: false}})
	if err != nil || disabled.Connection == nil {
		t.Fatal("disable actual SSH Connection", err)
	}
	connection = *disabled.Connection
	{
		policy, policyKey := entity.IntegrationCredentialInitiatorRequired, "ssh-native-personal-policy"
		if shared {
			policy, policyKey = entity.IntegrationCredentialProjectShared, "ssh-native-shared-policy"
		}
		owner.Permission = "platform.integrations.credential-policy.set"
		personal, err := service.Execute(ctx, command.Command{Kind: command.SetIntegrationCredentialPolicy, Principal: owner,
			Mutation: value.Mutation{IdempotencyKey: policyKey, ExpectedVersion: &connection.Version}, Payload: command.IntegrationCredentialPolicyInput{ConnectionRef: connection.Ref, Policy: policy}})
		if err != nil || personal.Connection == nil {
			t.Fatal("select personal SSH policy", err)
		}
		connection = *personal.Connection
	}
	owner.Permission = "platform.command.integrations.enable"
	enabled, err := service.Execute(ctx, command.Command{Kind: command.SetConnectionEnabled, Principal: owner,
		Mutation: value.Mutation{IdempotencyKey: "ssh-native-enable", ExpectedVersion: &connection.Version}, Payload: command.ConnectionInput{Ref: connection.Ref, Enabled: true}})
	if err != nil || enabled.Connection == nil {
		t.Fatal("enable actual personal SSH Connection", err)
	}
	connection = *enabled.Connection
	resolved, err := repository.ResolvePrincipal(ctx, owner)
	if err != nil {
		t.Fatal("resolve owner public coordinates", err)
	}
	broker := resolved
	broker.CallerWorkload = "secret-broker"
	return &sshSetupNativeFixture{ctx: ctx, pool: pool, repository: repository, service: service, owner: resolved, broker: broker, transport: owner, connection: connection, shared: shared}
}

// begin allocates the first empty binding or an explicitly current successor, retaining the one-off permit only in this private fixture.
// begin выделяет first empty binding либо explicitly current successor, сохраняя one-off permit лишь в private fixture.
func (f *sshSetupNativeFixture) begin(t *testing.T, key string, version int64) entity.SSHIdentitySetupAdmission {
	t.Helper()
	owner := f.owner
	owner.Permission = sshSetupBeginOperation
	result, err := f.repository.BeginSSHIdentitySetup(f.ctx, owner, value.Mutation{IdempotencyKey: key, ExpectedVersion: &version}, command.SSHIdentitySetupBegin{ConnectionRef: f.connection.Ref, ExpectedConnectionVersion: f.connection.Version, ContentSHA256: strings.Repeat("c", 64), ContentBytes: 128})
	if err != nil || len(result.Permit) != 32 || result.View.State != enum.SSHIdentitySetupPrepared {
		if f.shared {
			f.sharedSetupDiagnostic(t, key, version)
		}
		t.Fatalf("first actual SSH BI setup: %v", err)
	}
	return result
}

// sharedSetupDiagnostic observes only bounded validity flags in a rolled-back shared owner read, never printing UUIDs, selectors, material or database messages; the original failed public operation remains failed.
// sharedSetupDiagnostic наблюдает лишь bounded validity flags в rolled-back shared owner read без UUIDs, selectors, material либо database messages; исходная public operation остаётся неуспешной.
func (f *sshSetupNativeFixture) sharedSetupDiagnostic(t *testing.T, key string, version int64) {
	t.Helper()
	t.Logf("shared setup input: connection_version=%d expected_version=%d key_valid=%t reference_valid=%t", f.connection.Version, version, validSSHSetupKey(key), managedssh.OwnerReference(f.connection.Ref))
	current, err := f.repository.resolveScope(f.ctx, f.owner)
	if err != nil {
		t.Log("shared setup diagnostic: scope unavailable")
		return
	}
	tx, err := f.pool.Begin(f.ctx)
	if err != nil {
		t.Log("shared setup diagnostic: observer unavailable")
		return
	}
	defer tx.Rollback(f.ctx)
	connection, err := f.repository.lockIntegrationConnection(f.ctx, tx, current.organizationID, f.connection.Ref)
	if err != nil {
		t.Log("shared setup diagnostic: source unavailable")
		return
	}
	source, err := f.repository.sshSetupOwnerTx(f.ctx, tx, current, connection, f.connection.Ref, 0)
	t.Logf("shared setup owner: stored_version=%d policy_shared=%t source_accepted=%t owner_valid=%t subject_present=%t binding_empty=%t",
		connection.version, connection.credentialPolicy == entity.IntegrationCredentialProjectShared, err == nil,
		source.owner.valid(), source.owner.subjectID != "", source.owner.bindingID == "" && source.owner.bindingRef == "")
}

// claim exercises current Resolve and Claim with broker-derived integrity metadata and returns the exact original completion fence.
// claim выполняет current Resolve и Claim с broker-derived integrity metadata и возвращает exact original completion fence.
func (f *sshSetupNativeFixture) claim(t *testing.T, setup entity.SSHIdentitySetupAdmission) (entity.SSHIdentityUploadAdmission, command.SSHIdentityUploadClaim) {
	t.Helper()
	digest := sha256.Sum256(setup.Permit)
	permit := hex.EncodeToString(digest[:])
	broker := f.broker
	broker.Permission = "platform.runtime.ssh.setup.resolve"
	snapshot, err := f.repository.ResolveSSHIdentityUpload(f.ctx, broker, command.SSHIdentityUploadResolve{SetupRef: setup.View.Ref, PermitSHA256: permit})
	if err != nil {
		t.Fatal("resolve fresh SSH setup", err)
	}
	input := command.SSHIdentityUploadClaim{SetupRef: setup.View.Ref, PermitSHA256: permit, SnapshotDigest: snapshot.Digest, WorkloadInstance: "synthetic-broker", Key: managedssh.IdentityKeyMetadata{ContentSHA256: strings.Repeat("c", 64), AuthenticationSHA256: strings.Repeat("a", 64)}}
	broker.Permission = "platform.runtime.ssh.setup.claim"
	admission, err := f.repository.ClaimSSHIdentityUpload(f.ctx, broker, input)
	if err != nil {
		t.Fatal("claim exact SSH upload", err)
	}
	return admission, input
}

// complete sends one explicit receipt under the immutable original attempt; it never manufactures a new write or renews a lease.
// complete передаёт explicit receipt под immutable original attempt; новый write не создаётся и lease не продлевается.
func (f *sshSetupNativeFixture) complete(admission entity.SSHIdentityUploadAdmission, input command.SSHIdentityUploadComplete) (entity.SSHIdentitySetupView, error) {
	input.SetupRef = admission.SetupRef
	input.AttemptRef = admission.AttemptRef
	input.Generation = admission.Generation
	input.Fence = admission.Fence
	input.WorkloadInstance = "synthetic-broker"
	broker := f.broker
	broker.Permission = "platform.runtime.ssh.setup.complete"
	return f.repository.CompleteSSHIdentityUpload(f.ctx, broker, input)
}

// locator supplies synthetic exact material metadata only; actual TLS custody and SSH host authentication are separate native transport consumers.
// locator предоставляет лишь synthetic exact material metadata; actual TLS custody и SSH host authentication проверяются отдельными native transport consumers.
func (f *sshSetupNativeFixture) locator(setup entity.SSHIdentitySetupAdmission) managedssh.IdentityLocator {
	return managedssh.IdentityLocator{Purpose: managedssh.IdentityPurpose, IdentityRevisionRef: setup.View.IdentityRef, TargetRef: setup.View.TargetRef, Revision: setup.View.IdentityRevision, Namespace: sshIdentityMaterialNamespace, SecretName: managedssh.IdentitySecretName(setup.View.IdentityRef, setup.View.IdentityRevision), SecretUID: uuid.NewString(), SecretResourceVersion: "1", ContentSHA256: strings.Repeat("c", 64), AuthenticationSHA256: strings.Repeat("a", 64)}
}

// TestSSHIdentitySetupOwnerComponent proves real first-binding allocation, exact Known attachment and explicit UNKNOWN→OBSERVE→ACK_REJECTED closure through the current owner repository.
// TestSSHIdentitySetupOwnerComponent доказывает real first-binding allocation, exact Known attachment и explicit UNKNOWN→OBSERVE→ACK_REJECTED closure через current owner repository.
func TestSSHIdentitySetupOwnerComponent(t *testing.T) {
	t.Run("finite-access", func(t *testing.T) {
		f := newSSHSetupNativeFixture(t)
		current, err := f.repository.resolveScope(f.ctx, f.owner)
		if err != nil {
			t.Fatal("resolve setup actor", err)
		}
		viewUntil, restore := isolateIdentitySetupPermission(t, f.ctx, f.pool, f.service, f.transport, current, []string{"integration.view"}, "integration.view", 20*time.Second)
		setup := f.begin(t, "ssh-setup-finite-access", 0)
		defer restore()
		if !setup.View.ExpiresAt.Equal(viewUntil) {
			t.Fatal("setup permit outlived original view permission")
		}
		admission, input := f.claim(t, setup)
		if admission.ExpiresAt.After(viewUntil) || !admission.ExpiresAt.After(time.Now()) {
			t.Fatal("SSH admission did not honor fresh finite permission")
		}
		var durableExpiry time.Time
		if err := f.pool.QueryRow(f.ctx, "SELECT expires_at FROM control_plane.ssh_identity_upload_attempts WHERE ref=$1", admission.AttemptRef).Scan(&durableExpiry); err != nil || !durableExpiry.Equal(viewUntil) {
			t.Fatal("SSH durable attempt did not retain stricter permission expiry", err)
		}
		broker := f.broker
		broker.Permission = "platform.runtime.ssh.setup.resolve"
		if _, err := f.repository.ResolveSSHIdentityUpload(f.ctx, broker, command.SSHIdentityUploadResolve{SetupRef: input.SetupRef, PermitSHA256: input.PermitSHA256}); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("active limited claim minted another resolve", err)
		}
	})

	for _, scenario := range []string{"known", "observe-rejected"} {
		t.Run(scenario, func(t *testing.T) {
			f := newSSHSetupNativeFixture(t)
			setup := f.begin(t, "ssh-native-first-setup", 0)
			admission, _ := f.claim(t, setup)
			if admission.Mode != enum.SSHIdentityUploadWrite {
				t.Fatal("first setup did not allocate the sole WRITE")
			}
			receipt := command.SSHIdentityUploadComplete{Unknown: true}
			if scenario == "known" {
				locator := f.locator(setup)
				receipt = command.SSHIdentityUploadComplete{Identity: &locator}
			}
			completed, err := f.complete(admission, receipt)
			if err != nil {
				t.Fatal("complete initial upload", err)
			}
			if scenario == "known" {
				if completed.State != enum.SSHIdentitySetupCompleted {
					t.Fatalf("known material was not attached: %s/%s", completed.State, completed.ReasonCode)
				}
				owner := f.transport
				owner.Permission = "platform.personal-integrations.get"
				binding, err := f.service.GetPersonalIntegrationCredential(f.ctx, owner, f.connection.Ref)
				if err != nil || binding.Kind != entity.IntegrationCredentialBrokerIdentity || binding.Credential != nil || binding.BrokerIdentity == nil || binding.BrokerIdentity.Ref != setup.View.IdentityRef || binding.State != "CHECK_REQUIRED" {
					t.Fatal("actual first SSH binding lost broker metadata", err)
				}
				replay, err := f.complete(admission, receipt)
				if err != nil || replay != completed {
					t.Fatal("historical exact receipt changed state", err)
				}
			} else {
				if completed.State != enum.SSHIdentitySetupUnknown {
					t.Fatal("explicit ambiguity lost state")
				}
				observed, _ := f.claim(t, setup)
				if observed.Mode != enum.SSHIdentityUploadObserve || observed.Generation != 2 {
					t.Fatal("UNKNOWN did not admit sole next observation")
				}
				rejected, err := f.complete(observed, command.SSHIdentityUploadComplete{Rejected: &command.SSHIdentityRejectedAcknowledgement{ResponseSHA256: strings.Repeat("e", 64), ObservedUID: uuid.NewString()}})
				if err != nil || rejected.State != enum.SSHIdentitySetupAcknowledgementRejected {
					t.Fatal("damaged successful GET was not terminal", err)
				}
				broker := f.broker
				broker.Permission = "platform.runtime.ssh.setup.resolve"
				digest := sha256.Sum256(setup.Permit)
				if _, err := f.repository.ResolveSSHIdentityUpload(f.ctx, broker, command.SSHIdentityUploadResolve{SetupRef: setup.View.Ref, PermitSHA256: hex.EncodeToString(digest[:])}); !errors.Is(err, errs.ErrConflict) {
					t.Fatal("rejected ACK acquired another observation", err)
				}
			}
		})
	}
}

// TestSSHIdentityUnreportedObserveOwnerComponent waits for a real observation lease to expire, then proves historical WRITE replay and late OBSERVE completion cannot mint another observation.
// TestSSHIdentityUnreportedObserveOwnerComponent ожидает real expiry observation lease, затем доказывает, что historical WRITE replay и late OBSERVE completion не создают новый observation.
func TestSSHIdentityUnreportedObserveOwnerComponent(t *testing.T) {
	f := newSSHSetupNativeFixture(t)
	setup := f.begin(t, "ssh-native-observe-gap", 0)
	original, _ := f.claim(t, setup)
	if _, err := f.complete(original, command.SSHIdentityUploadComplete{Unknown: true}); err != nil {
		t.Fatal("retain original transport ambiguity", err)
	}
	observe, _ := f.claim(t, setup)
	if observe.Mode != enum.SSHIdentityUploadObserve {
		t.Fatal("missing OBSERVE grant")
	}
	wait := time.Until(observe.ExpiresAt) + 100*time.Millisecond
	if wait <= 0 || wait > 31*time.Second {
		t.Fatal("unexpected actual observation lease")
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-f.ctx.Done():
		t.Fatal("bounded actual lease expiry canceled")
	}
	before, err := f.complete(original, command.SSHIdentityUploadComplete{Unknown: true})
	if err != nil || before.State != enum.SSHIdentitySetupAttempting {
		t.Fatal("historical WRITE replay changed unreported OBSERVE", err)
	}
	broker := f.broker
	broker.Permission = "platform.runtime.ssh.setup.resolve"
	digest := sha256.Sum256(setup.Permit)
	input := command.SSHIdentityUploadResolve{SetupRef: setup.View.Ref, PermitSHA256: hex.EncodeToString(digest[:])}
	if _, err := f.repository.ResolveSSHIdentityUpload(f.ctx, broker, input); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("unreported latest OBSERVE acquired recovery credit", err)
	}
	late, err := f.complete(observe, command.SSHIdentityUploadComplete{Unknown: true})
	if err != nil || late.State != enum.SSHIdentitySetupExpired {
		t.Fatal("late OBSERVE receipt renewed authority", err)
	}
	replay, err := f.complete(original, command.SSHIdentityUploadComplete{Unknown: true})
	if err != nil || replay != late {
		t.Fatal("older receipt revived terminal authority", err)
	}
	if _, err := f.repository.ResolveSSHIdentityUpload(f.ctx, broker, input); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("late receipt acquired another recovery grant", err)
	}
}
