package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
)

// emailCredentialAdmissionRepository provides typed owner observations and records the internal
// command only; PostgreSQL authority and provider access are deliberately outside this unit test.
// emailCredentialAdmissionRepository задаёт typed owner observations и записывает внутреннюю
// команду; PostgreSQL authority и доступ к провайдеру намеренно не проверяются этим unit test.
type emailCredentialAdmissionRepository struct {
	platformrepo.Repository
	owner   entity.EmailMailboxOwnershipView
	command command.Command
}

// ResolvePrincipal substitutes the server-resolved fixture actor without deriving an owner from
// the command body or from the user who might approve a later mailbox effect.
// ResolvePrincipal подставляет серверного fixture actor, не выводя владельца из тела команды
// либо пользователя, который может позднее подтвердить почтовое действие.
func (*emailCredentialAdmissionRepository) ResolvePrincipal(_ context.Context, principal value.Principal) (value.Principal, error) {
	principal.ActorID = "subject_root_owner"
	return principal, nil
}

// GetIntegrationConnection returns an enabled personal mailbox with no generic token nextAction.
// GetIntegrationConnection возвращает включённый личный mailbox без generic token nextAction.
func (*emailCredentialAdmissionRepository) GetIntegrationConnection(_ context.Context, _ value.Principal, ref string) (entity.IntegrationConnection, error) {
	return entity.IntegrationConnection{Ref: ref, Version: 3, DefinitionKey: "email", Enabled: true, State: "NOT_CONNECTED", CredentialPolicy: entity.IntegrationCredentialInitiatorRequired}, nil
}

// GetEmailMailboxOwnership returns the selected current or counterfactual owner observation.
// GetEmailMailboxOwnership возвращает выбранное текущее либо контрфактическое owner observation.
func (repository *emailCredentialAdmissionRepository) GetEmailMailboxOwnership(context.Context, value.Principal, string) (entity.EmailMailboxOwnershipView, error) {
	return repository.owner, nil
}

// Execute records the owner-generation pin and returns a synthetic metadata-only commit result.
// Execute записывает pin поколения владельца и возвращает синтетический commit result без секрета.
func (repository *emailCredentialAdmissionRepository) Execute(_ context.Context, input command.Command) (command.Result, error) {
	repository.command = input
	credential := input.Payload.(command.EmailCredentialInput).Credential
	credential.ConnectionVersion = credential.Generation
	return command.Result{EmailCredential: &credential}, nil
}

// emailCredentialAdmissionMaterializer counts local materialization admissions without network or
// Kubernetes writes, and returns descriptor metadata with the real requested byte digest.
// emailCredentialAdmissionMaterializer считает локальные допуски материализации без network и
// Kubernetes writes, возвращая descriptor metadata с реальным digest переданных байт.
type emailCredentialAdmissionMaterializer struct{ calls int }

// Materialize records a call and returns a matching synthetic projection descriptor.
// Materialize записывает вызов и возвращает совпадающий синтетический projection descriptor.
func (materializer *emailCredentialAdmissionMaterializer) Materialize(_ context.Context, key string, raw []byte) (MaterializedCredential, error) {
	materializer.calls++
	digest := sha256.Sum256(raw)
	return MaterializedCredential{SecretRef: "kodex-system/email-bridge-mailbox-projection#" + key, SecretUID: "synthetic-uid", SecretResourceVersion: "17", ContentSHA256: hex.EncodeToString(digest[:])}, nil
}

// TestEmailCredentialOwnerAdmissionPrecedesMaterialization rejects disabled, foreign and stale
// owner observations before any secret effect; a valid owner supplies the internal epoch pin.
// TestEmailCredentialOwnerAdmissionPrecedesMaterialization отклоняет disabled, foreign и stale
// owner observations до любого secret effect; допустимый владелец задаёт внутренний epoch pin.
func TestEmailCredentialOwnerAdmissionPrecedesMaterialization(t *testing.T) {
	for _, name := range []string{"valid", "inactive", "revoked", "unconfigured", "foreign", "wrong delivery", "wrong policy", "stale version", "zero generation"} {
		t.Run(name, func(t *testing.T) {
			owner := entity.EmailMailboxOwnershipView{ConnectionRef: "int_mailbox_owner", ConnectionVersion: 3, DeliveryKind: "MAILBOX_CONFIGURATION", Policy: entity.IntegrationCredentialInitiatorRequired, State: "ACTIVE", Generation: 7, OwnerEligible: true}
			switch name {
			case "inactive":
				owner.OwnerEligible = false
			case "revoked":
				owner.State = "REVOKED"
			case "unconfigured":
				owner.State = "UNCONFIGURED"
			case "foreign":
				owner.ConnectionRef = "int_foreign"
			case "wrong delivery":
				owner.DeliveryKind = "TOKEN"
			case "wrong policy":
				owner.Policy = entity.IntegrationCredentialProjectShared
			case "stale version":
				owner.ConnectionVersion = 2
			case "zero generation":
				owner.Generation = 0
			}
			repository := &emailCredentialAdmissionRepository{owner: owner}
			materializer := &emailCredentialAdmissionMaterializer{}
			service, err := New(repository, WithEmailCredentialMaterializer(materializer))
			if err != nil {
				t.Fatal(err)
			}
			expected := int64(3)
			principal := value.Principal{ActorID: "external_actor", AuthorityTenant: "org_fixture", CallerWorkload: "control-api-gateway", Permission: "platform.command.email-mailbox.configure-credential", CorrelationRef: "correlation_fixture", CredentialRevision: 1}
			_, err = service.ConfigureEmailMailboxCredential(context.Background(), principal, value.Mutation{ExpectedVersion: &expected, IdempotencyKey: "email-owner-admission"}, "int_mailbox_owner", "AUTH_SECRET", []byte("synthetic credential"))
			if name != "valid" {
				if !errors.Is(err, errs.ErrConflict) || materializer.calls != 0 || repository.command.Kind != "" {
					t.Fatal("invalid owner reached materialization or commit")
				}
				return
			}
			if err != nil || materializer.calls != 1 || repository.command.Payload.(command.EmailCredentialInput).OwnerGeneration != 7 || repository.command.Principal.ActorID != "subject_root_owner" {
				t.Fatalf("valid typed owner admission failed: %v", err)
			}
		})
	}
}
