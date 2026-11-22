package platform

import (
	"context"
	_ "embed"
	"errors"
	"time"

	"github.com/codex-k8s/kodex/libs/go/objectstorage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// queryKubernetesFileOrigin resolves only the frozen original catalog entry and retained exact revision.
// Current source policy is evaluated without selecting a later RuntimeRevision or artifact version.
// queryKubernetesFileOrigin разрешает лишь frozen original catalog entry и retained exact revision.
// Current source policy проверяется без выбора позднего RuntimeRevision либо artifact version.
//
//go:embed sql/kubernetes_file__origin.sql
var queryKubernetesFileOrigin string

// kubernetesFileOrigin separates private immutable storage pins from safe source metadata.
// The actor is the persisted root initiator, never the technical broker or a request field.
// kubernetesFileOrigin отделяет private immutable storage pins от safe source metadata.
// Actor является persisted root initiator, не technical broker либо request field.
type kubernetesFileOrigin struct {
	actor                                                                                    scope
	projectID, projectRef, artifactRef, revisionRef, purpose, agentRef, sourceRunRef, runRef string
	object                                                                                   objectstorage.Receipt
	entryDigest                                                                              []byte
	systemAssistant                                                                          bool
	accessUntil                                                                              time.Time
}

// readKubernetesFileOriginTx reuses canonical version-aware runtime-file and artifact read admission.
// It retains the earliest finite permission bound through blocking reads and rejects all partial results.
// readKubernetesFileOriginTx использует canonical version-aware runtime-file и artifact read admission.
// Сохраняет earliest finite permission bound через blocking reads, отбрасывает partial results.
func (repository *Repository) readKubernetesFileOriginTx(ctx context.Context, tx pgx.Tx, owner kubernetesOperationOwner, first time.Time) (kubernetesFileOrigin, time.Time, error) {
	var origin kubernetesFileOrigin
	var actorID string
	err := tx.QueryRow(ctx, queryKubernetesFileOrigin, pgx.StrictNamedArgs{"organization_id": owner.current.organizationID, "invocation_ref": owner.candidate.ref}).Scan(
		&actorID, &origin.projectID, &origin.projectRef, &origin.artifactRef, &origin.revisionRef, &origin.object.Digest, &origin.object.SizeBytes,
		&origin.object.Key, &origin.object.VersionID, &origin.object.ETag, &origin.purpose, &origin.agentRef, &origin.sourceRunRef, &origin.entryDigest, &origin.systemAssistant, &origin.runRef)
	if errors.Is(err, pgx.ErrNoRows) {
		return origin, time.Time{}, errs.ErrForbidden
	}
	if err != nil {
		return origin, time.Time{}, errs.ErrUnavailable
	}
	err = tx.QueryRow(ctx, queryRepositoryResolvescopeSelectMembershipsOrganizationIdSubjectIdActive, owner.candidate.initiatorRef, owner.current.organizationRef).Scan(
		&origin.actor.organizationID, &origin.actor.organizationRef, &origin.actor.actorID, &origin.actor.actorRef, &origin.actor.actorName, &origin.actor.role)
	if errors.Is(err, pgx.ErrNoRows) {
		return kubernetesFileOrigin{}, time.Time{}, errs.ErrForbidden
	}
	if err != nil {
		return kubernetesFileOrigin{}, time.Time{}, errs.ErrUnavailable
	}
	if actorID != origin.actor.actorID || origin.actor.organizationID != owner.current.organizationID || len(origin.entryDigest) != 32 || origin.object.SizeBytes < 0 || origin.object.SizeBytes > 16<<20 || !objectstorage.ValidDigest(origin.object.Digest) {
		return kubernetesFileOrigin{}, time.Time{}, errs.ErrForbidden
	}
	until := earlierAccessDeadline(first, owner.lease.ExpiresAt)
	origin.accessUntil = owner.accessUntil
	checks := []struct{ permission, kind, ref string }{
		{"project.view", "PROJECT", origin.projectRef},
		{"artifact.view", "ARTIFACT", origin.artifactRef},
		{"artifact.download", "ARTIFACT", origin.artifactRef},
	}
	if !origin.systemAssistant {
		checks = append(checks, struct{ permission, kind, ref string }{"agent.view", "AGENT", origin.agentRef})
	}
	if origin.purpose == "RUN_RESULT" {
		if origin.sourceRunRef == "" {
			return kubernetesFileOrigin{}, time.Time{}, errs.ErrForbidden
		}
		checks = append(checks, struct{ permission, kind, ref string }{"run.view", "RUN", origin.sourceRunRef})
	}
	for _, check := range checks {
		fresh, err := repository.requireAccessUntil(ctx, tx, origin.actor, check.permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: check.kind, ResourceRef: check.ref, ProjectRef: origin.projectRef})
		if err != nil {
			return kubernetesFileOrigin{}, time.Time{}, err
		}
		until = earlierAccessDeadline(until, fresh)
		origin.accessUntil = earlierAccessDeadline(origin.accessUntil, fresh)
		if err := owner.creditError(ctx, until); err != nil {
			return kubernetesFileOrigin{}, time.Time{}, err
		}
	}
	if _, err := repository.artifactRevisionReadHead(ctx, tx, origin.actor, origin.artifactRef); err != nil {
		return kubernetesFileOrigin{}, time.Time{}, err
	}
	if _, err := readKubernetesCaptureOrigin(ctx, tx, origin.actor, origin.artifactRef, origin.revisionRef); err != nil {
		return kubernetesFileOrigin{}, time.Time{}, err
	}
	if err := owner.creditError(ctx, until); err != nil {
		return kubernetesFileOrigin{}, time.Time{}, err
	}
	return origin, until, nil
}
