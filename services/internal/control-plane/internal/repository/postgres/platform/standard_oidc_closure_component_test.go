package platform

import (
	"errors"
	"strconv"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	platformrepo "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
)

// TestStandardOIDCClosureComponent proves self-only tombstone reconciliation, single atomic revoke audit and no first-owner creation through closure routes.
// A revoked or disabled identity can close its exact existing session but never regain ordinary application authority.
// TestStandardOIDCClosureComponent проверяет self-only tombstone reconciliation, единый atomic revoke audit и отсутствие first-owner creation через closure routes.
// Revoked и disabled identity может лишь закрыть свою existing session, не восстанавливая ordinary application authority.
func TestStandardOIDCClosureComponent(t *testing.T) {
	repository, pool, identity := standardOIDCSessionFixture(t)
	ctx := t.Context()
	closure := identity
	closure.Operation = platformrepo.StandardOIDCClosureOperation
	if _, err := repository.ResolveStandardOIDCIdentity(ctx, closure); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("closure route bootstrapped a missing session")
	}
	binding, err := repository.ResolveStandardOIDCIdentity(ctx, identity)
	if err != nil {
		t.Fatalf("resolve explicit owner: %v", err)
	}
	proof := platformrepo.ProofPrincipalInput{CallerWorkload: "control-api-gateway", Operation: platformrepo.StandardOIDCClosureOperation,
		ExternalActorID: identity.Subject, ExternalTenantID: binding.OrganizationID, ExternalIssuer: identity.Issuer, ExternalSessionID: identity.SessionID,
		ExternalSessionRevision: binding.Revision, ExternalAuthenticatedAt: identity.AuthenticatedAt, ExternalIssuedAt: identity.IssuedAt, PlatformManagedOIDC: true}
	for range 2 {
		if err := repository.RevokeStandardOIDCSession(ctx, binding.OrganizationID, binding.ActorID, binding.Revision); err != nil {
			t.Fatalf("revoke/reconcile session: %v", err)
		}
	}
	var auditCount int
	if pool.QueryRow(ctx, `SELECT count(*) FROM control_plane.audit_events WHERE action='identity.session.revoke' AND resource_ref=$1`, strconv.FormatUint(binding.Revision, 10)).Scan(&auditCount) != nil || auditCount != 1 {
		t.Fatal("repeated revoke lost or duplicated the atomic audit")
	}
	if _, err := pool.Exec(ctx, `UPDATE control_plane.subjects SET active=false WHERE id=$1::uuid`, binding.ActorID); err != nil {
		t.Fatal("disable isolated fixture subject")
	}
	for _, operation := range []string{platformrepo.StandardOIDCClosureOperation, platformrepo.StandardOIDCRevokeOperation} {
		closure.Operation = operation
		tombstone, err := repository.ResolveStandardOIDCIdentity(ctx, closure)
		if err != nil || tombstone.Revision != binding.Revision || tombstone.State != "REVOKED" {
			t.Fatalf("closed identity reconciliation failed: %v", err)
		}
		proof.Operation = operation
		authority, err := repository.ResolveProofAuthority(ctx, proof)
		if err != nil || authority.ActorID != binding.ActorID || authority.OrganizationID != binding.OrganizationID || authority.ProjectID != "" {
			t.Fatalf("self-closure authority was not exact: %v", err)
		}
	}
	if _, err := repository.ResolveStandardOIDCIdentity(ctx, identity); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("closure eligibility leaked into ordinary identity resolution")
	}
	if _, err := repository.ReadStandardOIDCSession(ctx, binding.OrganizationID, binding.ActorID, binding.Revision, false); !errors.Is(err, errs.ErrForbidden) {
		t.Fatal("tombstone became ordinary session access")
	}
	for _, change := range []func(*platformrepo.ProofPrincipalInput){
		func(v *platformrepo.ProofPrincipalInput) { v.Operation = "platform.query.bootstrap.get" },
		func(v *platformrepo.ProofPrincipalInput) { v.CallerWorkload = "secret-broker" },
		func(v *platformrepo.ProofPrincipalInput) { v.ProjectRef = "prj_synthetic0001" },
		func(v *platformrepo.ProofPrincipalInput) { v.ExternalActorID = "20000000-0000-4000-8000-000000000099" },
		func(v *platformrepo.ProofPrincipalInput) {
			v.ExternalSessionID = "30000000-0000-4000-8000-000000000099"
		},
		func(v *platformrepo.ProofPrincipalInput) { v.ExternalTenantID = "50000000-0000-4000-8000-000000000099" },
		func(v *platformrepo.ProofPrincipalInput) { v.PlatformManagedOIDC = false },
	} {
		invalid := proof
		change(&invalid)
		if _, err := repository.ResolveProofAuthority(ctx, invalid); !errors.Is(err, errs.ErrForbidden) {
			t.Fatal("self-closure authority accepted a foreign tuple or application path")
		}
	}
}
