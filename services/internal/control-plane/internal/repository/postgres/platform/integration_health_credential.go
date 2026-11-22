package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// querySharedHealthCredentialSource reads metadata only under the caller's existing exact Connection lock; private material is resolved solely by purpose-specific owner admission later.
// querySharedHealthCredentialSource читает лишь metadata под existing exact Connection lock caller; private material позднее разрешает только purpose-specific owner admission.
//
//go:embed sql/integration_health__credential_source.sql
var querySharedHealthCredentialSource string

// sharedHealthCredentialSource retains owner-derived representation and immutable metadata for the common queue, without creating a parallel credential selector or generic Secret shape for broker identities.
// sharedHealthCredentialSource сохраняет owner-derived representation и immutable metadata для common queue без parallel credential selector либо generic Secret shape для broker identities.
type sharedHealthCredentialSource struct {
	delivery   integrationpackage.CredentialDelivery
	identityID string
	identity   *entity.IntegrationBrokerIdentityRevision
}

// sharedHealthCredentialSnapshot keeps the exact stored Connection source distinct from the package's declared delivery; missing or mixed data cannot be inferred as TOKEN.
// sharedHealthCredentialSnapshot отделяет exact stored Connection source от package delivery; missing либо mixed data не выводятся как TOKEN.
type sharedHealthCredentialSnapshot struct {
	configuration []byte
	tokenPresent  bool
	identityID    string
	identity      entity.IntegrationBrokerIdentityRevision
	createdAt     *time.Time
	serviceRef    string
	serviceActive bool
}

// readSharedHealthCredentialSourceTx derives delivery from the exact owner-pinned package before queue insertion. Only exact EMAIL/NONE may admit a personal Connection after the caller's mailbox owner gate; broker metadata retains PROJECT_SHARED and the authoritative selector with an active SERVICE.
// readSharedHealthCredentialSourceTx выводит delivery из exact owner-pinned package до queue insertion. Только exact EMAIL/NONE допускает personal Connection после mailbox owner gate caller; broker metadata сохраняет PROJECT_SHARED и authoritative selector с active SERVICE.
func (repository *Repository) readSharedHealthCredentialSourceTx(ctx context.Context, tx pgx.Tx, organizationID, connectionID, connectionRef string, definition integrationpackage.Package) (sharedHealthCredentialSource, error) {
	delivery, known := definition.ConnectionCredentialDelivery()
	allowPersonalEmail := known && definition.Metadata.Key == "email" && definition.Spec.Adapter == string(integrationpackage.AdapterEmailHTTPS) && delivery.Kind == integrationpackage.CredentialDeliveryNone
	var stored sharedHealthCredentialSnapshot
	err := tx.QueryRow(ctx, querySharedHealthCredentialSource, pgx.StrictNamedArgs{"organization_id": organizationID, "connection_id": connectionID, "allow_personal_email": allowPersonalEmail}).Scan(
		&stored.configuration, &stored.tokenPresent, &stored.identityID, &stored.identity.Ref, &stored.identity.Revision, &stored.identity.Purpose, &stored.identity.TargetRef, &stored.createdAt, &stored.serviceRef, &stored.serviceActive)
	if err != nil {
		return sharedHealthCredentialSource{}, errs.ErrUnavailable
	}
	return sharedHealthCredentialFromSnapshot(organizationID, connectionRef, definition, stored, time.Now().UTC())
}

// sharedHealthCredentialFromSnapshot validates representation before any owner transition while preserving NONE/TOKEN delivery and retaining no private transport locator in the claim.
// sharedHealthCredentialFromSnapshot проверяет representation до owner transition, сохраняя NONE/TOKEN delivery и не передавая private transport locator в claim.
func sharedHealthCredentialFromSnapshot(organizationID, connectionRef string, definition integrationpackage.Package, stored sharedHealthCredentialSnapshot, now time.Time) (sharedHealthCredentialSource, error) {
	delivery, known := definition.ConnectionCredentialDelivery()
	if !known {
		return sharedHealthCredentialSource{}, errs.ErrUnavailable
	}
	if definition.Metadata.Key == "email" && (definition.Spec.Adapter != string(integrationpackage.AdapterEmailHTTPS) || delivery.Kind != integrationpackage.CredentialDeliveryNone || stored.tokenPresent) {
		return sharedHealthCredentialSource{}, errs.ErrUnavailable
	}
	result := sharedHealthCredentialSource{delivery: delivery}
	switch delivery.Kind {
	case integrationpackage.CredentialDeliveryNone, integrationpackage.CredentialDeliveryToken:
		if stored.identityID != "" || stored.identity.Ref != "" || stored.identity.Revision != 0 || stored.identity.Purpose != "" || stored.identity.TargetRef != "" || stored.createdAt != nil || stored.serviceRef != "" || stored.serviceActive {
			return sharedHealthCredentialSource{}, errs.ErrUnavailable
		}
		return result, nil
	case integrationpackage.CredentialDeliveryBrokerIdentity:
		if delivery.BrokerPurpose != string(entity.IntegrationBrokerIdentityKubernetes) && delivery.BrokerPurpose != string(entity.IntegrationBrokerIdentitySSH) || stored.tokenPresent {
			return sharedHealthCredentialSource{}, errs.ErrUnavailable
		}
	default:
		return sharedHealthCredentialSource{}, errs.ErrUnavailable
	}
	if stored.identityID == "" {
		return sharedHealthCredentialSource{}, errs.ErrConflict
	}
	if stored.createdAt == nil {
		return sharedHealthCredentialSource{}, errs.ErrUnavailable
	}
	stored.identity.CreatedAt = stored.createdAt.UTC()
	var configuration map[string]any
	if json.Unmarshal(stored.configuration, &configuration) != nil {
		return sharedHealthCredentialSource{}, errs.ErrUnavailable
	}
	strings, valid := integrationStringConfiguration(configuration)
	if !valid || definition.ValidateConfiguration(strings) != nil {
		return sharedHealthCredentialSource{}, errs.ErrUnavailable
	}
	if strings["service_subject_ref"] == "" || !stored.serviceActive || stored.serviceRef != strings["service_subject_ref"] {
		return sharedHealthCredentialSource{}, errs.ErrConflict
	}
	raw, err := json.Marshal(configuration)
	if err != nil {
		return sharedHealthCredentialSource{}, errs.ErrUnavailable
	}
	sum := sha256.Sum256(raw)
	health, exists := definition.CapabilityByOperation(definition.Spec.HealthCheck.Operation)
	if !exists || health.Risk != "READ" || health.ApprovalPolicy != "NONE" {
		return sharedHealthCredentialSource{}, errs.ErrUnavailable
	}
	selected, err := platformservice.ResolveIntegrationCredential(entity.IntegrationCredentialRequirement{
		ConnectionRef: connectionRef, CapabilityKey: health.Key, DefinitionKey: definition.Metadata.Key, DefinitionDigest: definition.Digest, ConfigurationDigest: hex.EncodeToString(sum[:]),
		Policy: entity.IntegrationCredentialProjectShared, CredentialRequired: true, Kind: entity.IntegrationCredentialBrokerIdentity,
		BrokerPurpose: entity.IntegrationBrokerIdentityPurpose(delivery.BrokerPurpose), BrokerTargetRef: strings["target_ref"],
	}, entity.IntegrationCredentialInitiator{OrganizationID: organizationID}, &entity.IntegrationCredentialSource{Kind: entity.IntegrationCredentialBrokerIdentity, BrokerIdentity: &stored.identity}, nil, now)
	if err != nil {
		return sharedHealthCredentialSource{}, err
	}
	if selected.Problem != "" || selected.Credential != nil || selected.BrokerIdentity == nil {
		return sharedHealthCredentialSource{}, errs.ErrConflict
	}
	result.identityID, result.identity = stored.identityID, selected.BrokerIdentity
	return result, nil
}

// validSharedHealthInputDigest checks canonical SHA256 spelling only; the owner query separately binds it to exact stored snapshot bytes and absence never becomes a representation or permission.
// validSharedHealthInputDigest проверяет лишь canonical SHA256 spelling; owner query отдельно связывает hash с exact stored snapshot bytes, отсутствие не становится representation либо permission.
func validSharedHealthInputDigest(digest string) bool {
	if len(digest) != sha256.Size*2 || strings.ToLower(digest) != digest {
		return false
	}
	_, err := hex.DecodeString(digest)
	return err == nil
}
