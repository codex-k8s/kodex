package platform

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// repositoryBundleOrigin contains only owner-resolved relational identity from an exact live execution and its pinned grant.
// repositoryBundleOrigin содержит лишь owner-resolved relational identity exact live execution и его pinned grant.
type repositoryBundleOrigin struct {
	runID, runRef, rootRunID, projectID, projectRef string
	actorID, actorRef, nodeID, nodeRef              string
	revisionID, revisionRef, connectionID           string
	connectionRef, grantID, resourceDigest          string
	grantVersion                                    int64
	resourceScope                                   []byte
}

// validRepositoryBundlePin checks the complete bounded captured artifact metadata; syntactic validity never replaces runtime eligibility.
// validRepositoryBundlePin проверяет полные bounded captured artifact metadata; синтаксическая корректность не заменяет runtime eligibility.
func validRepositoryBundlePin(pin entity.RepositoryBundleArtifactPin, maximum int64) bool {
	return strings.HasPrefix(pin.Ref, "art_") && len(pin.Ref) <= 96 && pin.Revision > 0 && pin.Version > 0 && pin.Size > 0 && pin.Size <= maximum && repositorybundle.ValidDigest(pin.SHA256)
}

// readRepositoryBundleArtifact reuses the native exact execution artifact reader and independently verifies size/digest after bounded streaming; no public or unscoped object key is accepted.
// readRepositoryBundleArtifact переиспользует native exact execution artifact reader и независимо сверяет size/digest после bounded streaming; public либо unscoped object key не принимается.
func (repository *Repository) readRepositoryBundleArtifact(ctx context.Context, principal value.Principal, input port.RepositoryBundlePrepare, pin entity.RepositoryBundleArtifactPin) ([]byte, error) {
	download, err := repository.ReadExecutionArtifact(ctx, principal, input.LeaseRef, input.Fence, input.Generation, pin.Ref)
	if err != nil {
		return nil, err
	}
	defer func() { _ = download.Reader.Close() }()
	artifact := download.Artifact
	if artifact.Ref != pin.Ref || artifact.Revision != pin.Revision || artifact.Version != pin.Version || artifact.SizeBytes != pin.Size || artifact.Digest != "sha256:"+pin.SHA256 || artifact.ScanState != "CLEAN" {
		return nil, errs.ErrConflict
	}
	raw, err := io.ReadAll(io.LimitReader(download.Reader, pin.Size+1))
	if err != nil {
		clear(raw)
		return nil, errs.ErrUnavailable
	}
	if int64(len(raw)) != pin.Size || repositorybundle.Digest(raw) != pin.SHA256 {
		clear(raw)
		return nil, errs.ErrConflict
	}
	return raw, nil
}

// PrepareRepositoryBundle pins verified immutable source revisions and decoded-file receipts with a native integration invocation/Gate; all authority is rechecked after object reads and before owner commit. Its retries affect only PostgreSQL, never Git.
// PrepareRepositoryBundle закрепляет verified immutable source revisions и decoded-file receipts с native integration invocation/Gate; authority повторно проверяется после object reads до owner commit. Retry затрагивает лишь PostgreSQL, никогда Git.
func (repository *Repository) PrepareRepositoryBundle(ctx context.Context, principal value.Principal, input port.RepositoryBundlePrepare) (entity.RepositoryBundlePublication, error) {
	if principal.CallerWorkload != "runtime-controller" || principal.Permission != "platform.repository-bundles.prepare" || input.LeaseRef == "" || input.Fence == "" || input.Generation < 1 || input.GrantRef == "" ||
		len(input.BaseCommitSHA) != 40 || !validSourceCommit(input.BaseCommitSHA) || len(input.IdempotencyKey) < 8 || len(input.IdempotencyKey) > 128 || strings.TrimSpace(input.IdempotencyKey) != input.IdempotencyKey || strings.ContainsAny(input.IdempotencyKey, "\x00\r\n") ||
		!validRepositoryBundlePin(input.Archive, repositorybundle.MaximumArchiveBytes) || !validRepositoryBundlePin(input.Manifest, repositorybundle.MaximumManifestBytes) || input.Archive.Ref == input.Manifest.Ref {
		return entity.RepositoryBundlePublication{}, errs.ErrInvalid
	}
	archive, err := repository.readRepositoryBundleArtifact(ctx, principal, input, input.Archive)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	defer clear(archive)
	manifestBytes, err := repository.readRepositoryBundleArtifact(ctx, principal, input, input.Manifest)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	defer clear(manifestBytes)
	manifest, files, err := repositorybundle.Open(archive, manifestBytes)
	if err != nil {
		return entity.RepositoryBundlePublication{}, errs.ErrInvalid
	}
	for _, file := range files {
		defer clear(file.Bytes)
	}
	receipts, err := repository.inspectRepositoryBundleFiles(ctx, files)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	return retrySerializableTransaction(ctx, func() (entity.RepositoryBundlePublication, error) {
		return repository.prepareRepositoryBundleTx(ctx, principal, input, manifest, receipts)
	})
}

// prepareRepositoryBundleTx binds one full manifest to exact origin/grant/connection/package pins and creates the native gate and durable proposal together; stale or foreign locators cannot select another owner.
// prepareRepositoryBundleTx связывает полный manifest с exact origin/grant/connection/package pins, создавая native Gate и durable proposal совместно; stale/foreign locators не выбирают другого owner.
func (repository *Repository) prepareRepositoryBundleTx(ctx context.Context, principal value.Principal, input port.RepositoryBundlePrepare, manifest repositorybundle.Manifest, receipts []repositoryBundleFileReceipt) (entity.RepositoryBundlePublication, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
	}
	defer rollbackRepositoryBundle(ctx, tx)
	var origin repositoryBundleOrigin
	err = tx.QueryRow(ctx, queryRepositoryBundleOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_ref": input.LeaseRef,
		"fence_digest": repositorybundle.Digest([]byte(input.Fence)), "generation": input.Generation, "grant_ref": input.GrantRef}).Scan(
		&origin.runID, &origin.runRef, &origin.rootRunID, &origin.projectID, &origin.projectRef, &origin.actorID, &origin.actorRef,
		&origin.nodeID, &origin.nodeRef, &origin.revisionID, &origin.revisionRef, &origin.connectionID, &origin.connectionRef, &origin.grantID,
		&origin.grantVersion, &origin.resourceDigest, &origin.resourceScope,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return entity.RepositoryBundlePublication{}, errs.ErrForbidden
	}
	if err != nil {
		return entity.RepositoryBundlePublication{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	inputDigest := repositorybundle.Digest(asJSON(input))
	var existing, oldDigest string
	err = tx.QueryRow(ctx, queryRepositoryBundleExisting, pgx.StrictNamedArgs{"organization_id": current.organizationID, "runtime_revision_id": origin.revisionID, "node_id": origin.nodeID, "idempotency_key": input.IdempotencyKey}).Scan(&existing, &oldDigest)
	if err == nil {
		if oldDigest != inputDigest {
			return entity.RepositoryBundlePublication{}, errs.ErrIdempotencyReuse
		}
		row, err := lockRepositoryBundle(ctx, tx, current, existing)
		if err != nil {
			return entity.RepositoryBundlePublication{}, err
		}
		if tx.Commit(ctx) != nil {
			return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
		}
		return row.proposal, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return entity.RepositoryBundlePublication{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	initiator := current
	initiator.actorID, initiator.actorRef = origin.actorID, origin.actorRef
	if repository.requireAccess(ctx, tx, initiator, "integration.manage", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "INTEGRATION", ResourceRef: origin.connectionRef}) != nil {
		return entity.RepositoryBundlePublication{}, errs.ErrForbidden
	}
	for _, pin := range []entity.RepositoryBundleArtifactPin{input.Archive, input.Manifest} {
		var ref string
		if err := tx.QueryRow(ctx, queryRepositoryBundleArtifactEligibility, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": origin.projectID, "actor_id": origin.actorID,
			"artifact_ref": pin.Ref, "revision": pin.Revision, "digest": pin.SHA256, "size_bytes": pin.Size}).Scan(&ref); errors.Is(err, pgx.ErrNoRows) {
			return entity.RepositoryBundlePublication{}, errs.ErrNotFound
		} else if err != nil {
			return entity.RepositoryBundlePublication{}, serializableTransactionError(err, errs.ErrUnavailable)
		}
	}
	connection, err := readConnection(ctx, tx, initiator, origin.connectionRef)
	if err != nil || connection.CredentialRevision == nil || !connection.Enabled || connection.State != "CONNECTED" {
		return entity.RepositoryBundlePublication{}, errs.ErrForbidden
	}
	definition, err := repository.integrationPackage(ctx, tx, current.organizationID, connection.Ref, connection.DefinitionKey, connection.DefinitionVersion, connection.DefinitionDigest)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	configuration, valid := integrationStringConfiguration(connection.PublicConfiguration)
	if !valid || definition.ValidateConfiguration(configuration) != nil || connection.DefinitionKey != "github" {
		return entity.RepositoryBundlePublication{}, errs.ErrForbidden
	}
	var connectionID, credentialID string
	if err := tx.QueryRow(ctx, queryConfigurationSourceCredential, current.organizationID, connection.Ref, connection.CredentialRevision.Ref).Scan(&connectionID, &credentialID); err != nil || connectionID != origin.connectionID {
		return entity.RepositoryBundlePublication{}, errs.ErrForbidden
	}
	bounded := repositorybundle.InvocationInput(input.Archive, input.Manifest, input.BaseCommitSHA)
	resolved, err := repository.resolveIntegrationInvocationTx(ctx, tx, current, map[string]string{"run_ref": origin.runRef, "node_ref": origin.nodeRef, "connection_ref": origin.connectionRef, "capability_key": repositoryBundleOperation, "idempotency_key": input.IdempotencyKey}, bounded, true)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	ref, err := newRef("rbp")
	if err != nil {
		return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
	}
	var invocationID, gateID, nativeDigest, effectKey string
	if err := tx.QueryRow(ctx, queryRepositoryBundleIdentity, pgx.StrictNamedArgs{"organization_id": current.organizationID, "invocation_ref": stringMap(resolved, "invocationRef"), "node_id": origin.nodeID, "grant_id": origin.grantID, "root_run_id": origin.rootRunID, "project_id": origin.projectID}).Scan(&invocationID, &gateID, &nativeDigest, &effectKey); err != nil {
		return entity.RepositoryBundlePublication{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	var now time.Time
	if tx.QueryRow(ctx, queryCatalogSnapshotTime).Scan(&now) != nil {
		return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
	}
	proposal := entity.RepositoryBundlePublication{Ref: ref, Version: 1, ProjectRef: origin.projectRef, OriginRunRef: origin.runRef, OriginRuntimeRevisionRef: origin.revisionRef,
		InvocationRef: stringMap(resolved, "invocationRef"), GateRef: stringMap(resolved, "gateRef"), ConnectionRef: connection.Ref, ConnectionVersion: connection.Version, GrantRef: input.GrantRef, GrantVersion: origin.grantVersion,
		RepositoryRef: strings.ToLower(configuration["owner"] + "/" + configuration["repository"]), BaseCommitSHA: input.BaseCommitSHA, ProposalBranch: "kodex/bundle/" + ref,
		Archive: input.Archive, Manifest: input.Manifest, FileCount: int32(len(manifest.Files)), TotalBytes: manifest.TotalBytes, State: "WAITING_APPROVAL", CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}
	proposal.ApprovalDigest = repositorybundle.Digest(asJSON(proposal))
	capability, ok := definition.Capability(repositoryBundleOperation)
	canonical, canonicalErr := capability.ValidateInput(asJSON(bounded))
	if !ok || canonicalErr != nil || repositorybundle.Digest(canonical) != nativeDigest || effectKey == "" {
		return entity.RepositoryBundlePublication{}, errs.ErrInvalid
	}
	var resourceScope map[string]string
	if json.Unmarshal(origin.resourceScope, &resourceScope) != nil || resourceScope == nil || repositorybundle.Digest(asJSON(resourceScope)) != origin.resourceDigest {
		return entity.RepositoryBundlePublication{}, errs.ErrUnavailable
	}
	archiveSource, err := resolveRepositoryBundleSource(ctx, tx, current, origin.projectID, input.Archive)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	manifestSource, err := resolveRepositoryBundleSource(ctx, tx, current, origin.projectID, input.Manifest)
	if err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	snapshot := repositoryBundleSnapshot{SourcePolicy: repositoryBundleContentPolicy, FileReceipts: receipts, ArchiveSource: archiveSource, ManifestSource: manifestSource, ResourceScope: resourceScope, ResourceScopeDigest: origin.resourceDigest, Proposal: proposal, DefinitionKey: definition.Metadata.Key, DefinitionVersion: definition.Metadata.Version, DefinitionDigest: definition.Digest,
		DefinitionPackage: asJSON(definition), PublicConfiguration: connection.PublicConfiguration, Credential: *connection.CredentialRevision, InputDigest: nativeDigest, EffectKey: effectKey}
	if _, err := tx.Exec(ctx, queryRepositoryBundleInsert, pgx.StrictNamedArgs{"ref": ref, "organization_id": current.organizationID, "project_id": origin.projectID, "actor_id": origin.actorID, "root_run_id": origin.rootRunID, "run_id": origin.runID, "node_id": origin.nodeID,
		"runtime_revision_id": origin.revisionID, "lease_ref": input.LeaseRef, "generation": input.Generation, "connection_id": origin.connectionID, "grant_id": origin.grantID, "credential_id": credentialID, "gate_id": gateID, "invocation_id": invocationID,
		"idempotency_key": input.IdempotencyKey, "input_sha256": inputDigest, "snapshot_sha256": repositorybundle.Digest(asJSON(snapshot)), "approval_digest": proposal.ApprovalDigest, "snapshot": asJSON(snapshot), "manifest_metadata": asJSON(manifest), "manifest_sha256": input.Manifest.SHA256,
		"archive_revision_ref": archiveSource.RevisionRef, "manifest_revision_ref": manifestSource.RevisionRef, "archive_sha256": input.Archive.SHA256, "base_commit_sha": input.BaseCommitSHA, "repository_ref": proposal.RepositoryRef, "proposal_branch": proposal.ProposalBranch, "created_at": proposal.CreatedAt, "expires_at": proposal.ExpiresAt}); err != nil {
		return entity.RepositoryBundlePublication{}, serializableTransactionError(err, mapWriteError(err))
	}
	if err := repositoryBundleAudit(ctx, tx, current, origin.projectID, origin.actorID, proposal.Ref, proposal.State); err != nil {
		return entity.RepositoryBundlePublication{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return entity.RepositoryBundlePublication{}, serializableTransactionError(err, errs.ErrUnavailable)
	}
	return proposal, nil
}
