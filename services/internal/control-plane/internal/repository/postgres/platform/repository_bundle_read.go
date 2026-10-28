package platform

import (
	"context"
	_ "embed"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	port "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/repository/platform"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryRepositoryBundleResolve locates exactly one proposal/native gate pair in the resolved organization; authorization still precedes any manifest output.
// queryRepositoryBundleResolve находит exact proposal/native Gate pair в разрешённой организации; authorization предшествует выдаче manifest.
//
//go:embed sql/repository_bundle__resolve.sql
var queryRepositoryBundleResolve string

// GetRepositoryBundle reads a full immutable-manifest page only after the same native gate eligibility and current artifact permissions; cursors cannot mix proposals or falsely claim completeness.
// GetRepositoryBundle читает immutable-manifest page лишь после native Gate eligibility и текущих artifact permissions; cursors не смешивают proposals и не создают ложную полноту.
func (repository *Repository) GetRepositoryBundle(ctx context.Context, principal value.Principal, input port.RepositoryBundleRead) (entity.RepositoryBundlePublicationView, error) {
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.RepositoryBundlePublicationView{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return entity.RepositoryBundlePublicationView{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var ref, gateRef string
	if err := tx.QueryRow(ctx, queryRepositoryBundleResolve, pgx.StrictNamedArgs{"organization_id": current.organizationID, "proposal_ref": input.ProposalRef, "gate_ref": input.GateRef}).Scan(&ref, &gateRef); errors.Is(err, pgx.ErrNoRows) {
		return entity.RepositoryBundlePublicationView{}, errs.ErrNotFound
	} else if err != nil {
		return entity.RepositoryBundlePublicationView{}, errs.ErrUnavailable
	}
	gate, err := scanGate(tx.QueryRow(ctx, queryQueriesGetownergateSelectOwnerGatesOrganizationIdRefProjectId, current.organizationID, gateRef, current.role, current.actorID), true)
	if err != nil {
		return entity.RepositoryBundlePublicationView{}, err
	}
	row, err := lockRepositoryBundle(ctx, tx, current, ref)
	if err != nil {
		return entity.RepositoryBundlePublicationView{}, err
	}
	if row.proposal.GateRef != gate.Ref || row.proposal.ProjectRef != gate.ProjectRef || row.proposal.ProjectRef == "" || current.authorityProjectID != "" && current.authorityProjectID != row.projectID {
		return entity.RepositoryBundlePublicationView{}, errs.ErrNotFound
	}
	if err := requireRepositoryBundleArtifactEligibility(ctx, tx, current, row.projectID, current.actorID, row.proposal); err != nil {
		return entity.RepositoryBundlePublicationView{}, err
	}
	if input.ExpectedVersion > 0 && input.ExpectedVersion != row.proposal.Version || input.ExpectedManifestSHA256 != "" && input.ExpectedManifestSHA256 != row.proposal.Manifest.SHA256 {
		return entity.RepositoryBundlePublicationView{}, errs.ErrVersionMismatch
	}
	manifest, err := repositorybundle.ParseManifest(row.manifest)
	if err != nil || input.Offset < 0 || int(input.Offset) > len(manifest.Files) || input.Limit < 1 || input.Limit > 32 {
		return entity.RepositoryBundlePublicationView{}, errs.ErrInvalid
	}
	end := min(len(manifest.Files), int(input.Offset+input.Limit))
	view := entity.RepositoryBundlePublicationView{Proposal: row.proposal, Offset: input.Offset}
	for end > int(input.Offset) {
		view.Files = append([]repositorybundle.Entry(nil), manifest.Files[input.Offset:end]...)
		view.NextOffset, view.Complete = int32(end), end == len(manifest.Files)
		// Leave room for the transport projection inside its separate exact 24KiB bound.
		// Оставляем место transport projection в её отдельном exact24KiB bound.
		if len(asJSON(view)) <= 20<<10 {
			break
		}
		end--
	}
	if end == int(input.Offset) {
		if end != len(manifest.Files) {
			return entity.RepositoryBundlePublicationView{}, errs.ErrUnavailable
		}
		view.Files, view.NextOffset, view.Complete = []repositorybundle.Entry{}, input.Offset, true
	}
	if tx.Commit(ctx) != nil {
		return entity.RepositoryBundlePublicationView{}, errs.ErrUnavailable
	}
	return view, nil
}

// requireRepositoryBundleArtifactEligibility retains both exact artifact metadata rows and current actor download/view eligibility; historical pins are never replaced with latest content.
// requireRepositoryBundleArtifactEligibility удерживает обе exact artifact metadata rows и текущую actor download/view eligibility; historical pins не заменяются latest content.
func requireRepositoryBundleArtifactEligibility(ctx context.Context, tx pgx.Tx, current scope, projectID, actorID string, proposal entity.RepositoryBundlePublication) error {
	for _, pin := range []entity.RepositoryBundleArtifactPin{proposal.Archive, proposal.Manifest} {
		var ref string
		if err := tx.QueryRow(ctx, queryRepositoryBundleArtifactEligibility, pgx.StrictNamedArgs{"organization_id": current.organizationID, "project_id": projectID, "actor_id": actorID,
			"artifact_ref": pin.Ref, "revision": pin.Revision, "digest": pin.SHA256, "size_bytes": pin.Size}).Scan(&ref); errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrNotFound
		} else if err != nil {
			return errs.ErrUnavailable
		}
	}
	return nil
}
