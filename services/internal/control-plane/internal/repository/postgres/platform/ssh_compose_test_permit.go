package platform

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"reflect"
	"time"

	"github.com/codex-k8s/kodex/libs/go/sshcompose"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// issueSSHComposeTestPermit pins an active project secret to one already claimed read-only health test.
// issueSSHComposeTestPermit закрепляет active project secret за захваченным read-only health test.
func (repository *Repository) issueSSHComposeTestPermit(ctx context.Context, tx pgx.Tx, current scope, binding platformrepo.SSHComposeLeaseBinding) (map[string]any, error) {
	var id, connectionRef, projectID, projectRef, initiatorRef string
	var rawConfiguration, rawStored []byte
	var expires time.Time
	var attempt int
	if err := tx.QueryRow(ctx, querySSHComposeTest, pgx.StrictNamedArgs{"organization_id": current.organizationID, "test_ref": binding.ConnectionTestRef,
		"lease_ref": binding.LeaseRef, "generation": binding.Generation, "fence_digest": sshcompose.Digest([]byte(binding.Fence)), "workload_instance": binding.WorkloadInstance}).Scan(
		&id, &connectionRef, &rawConfiguration, &projectID, &projectRef, &initiatorRef, &expires, &rawStored, &attempt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, errs.ErrForbidden
		}
		return nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	var configuration map[string]string
	if json.Unmarshal(rawConfiguration, &configuration) != nil {
		return nil, errs.ErrUnavailable
	}
	target, err := sshcompose.TargetFromConfiguration(configuration)
	if err != nil || target.ProjectRef != projectRef {
		return nil, errs.ErrForbidden
	}
	secret, err := resolveSSHComposeSecret(ctx, tx, current.organizationID, projectID, configuration["ssh_secret_ref"])
	if err != nil {
		return nil, err
	}
	initiator := current
	initiator.actorRef = initiatorRef
	if err := repository.requireAccess(ctx, tx, initiator, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: connectionRef}); err != nil {
		return nil, err
	}
	if err := repository.requireAccess(ctx, tx, initiator, "secret.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "SECRET", ResourceRef: secret.Ref}); err != nil {
		return nil, err
	}
	snapshot := sshcompose.Snapshot{Version: sshcompose.ProtocolVersion, Target: target, SSHSecret: secret, RootRunRef: binding.ConnectionTestRef, SessionRef: binding.ConnectionTestRef, TurnRef: binding.ConnectionTestRef, Attempt: attempt}
	if len(rawStored) > 0 {
		var stored sshcompose.Snapshot
		if sshcompose.Decode(rawStored, &stored, 32768) != nil || !reflect.DeepEqual(stored, snapshot) {
			return nil, errs.ErrForbidden
		}
	} else {
		encoded, _ := json.Marshal(snapshot)
		if _, err := tx.Exec(ctx, querySSHComposeStoreTestSnapshot, pgx.StrictNamedArgs{"test_id": id, "snapshot": encoded}); err != nil {
			return nil, errs.ErrUnavailable
		}
	}
	now := time.Now().UTC()
	permit := sshcompose.Permit{Version: sshcompose.ProtocolVersion, Purpose: "INSPECT", Target: target, InvocationRef: binding.ConnectionTestRef, RootRunRef: binding.ConnectionTestRef,
		SessionRef: binding.ConnectionTestRef, TurnRef: binding.ConnectionTestRef, Attempt: attempt, LeaseRef: binding.LeaseRef, Generation: binding.Generation,
		EffectKey: "test_" + sshcompose.Digest([]byte(binding.ConnectionTestRef))[:32], InputDigest: sshcompose.Digest([]byte("{}")), IssuedAt: now.Unix(), ExpiresAt: expires.Unix(), Deadline: expires.Unix()}
	signed, err := sshcompose.Sign(permit, ed25519.PrivateKey(repository.sshComposeSigningKey), now)
	if err != nil {
		return nil, errs.ErrForbidden
	}
	encoded, _ := json.Marshal(signed)
	return map[string]any{"permit": encoded, "targetRef": target.Ref, "secretRef": secret.Ref, "expiresAt": expires,
		"descriptor": entity.RuntimeSecretRevisionDescriptor{Revision: secret.Revision, Namespace: secret.Namespace, SecretName: secret.Name, SecretKey: secret.Key, SecretUID: secret.UID, SecretResourceVersion: secret.ResourceVersion, ContentSHA256: secret.ContentSHA256}}, nil
}
