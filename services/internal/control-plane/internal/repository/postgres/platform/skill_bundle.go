package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/skillpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

type skillBundleRow struct {
	entity.SkillBundle
	id, projectID, currentRevisionID, draftRevisionID string
}

func (repository *Repository) skillBundleAccess(ctx context.Context, tx pgx.Tx, current scope, bundle skillBundleRow) error {
	if current.authorityProjectID != "" && current.authorityProjectID != bundle.projectID {
		return errs.ErrForbidden
	}
	return repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: bundle.ProjectRef})
}

func (repository *Repository) refreshSkillReceipt(ctx context.Context, tx pgx.Tx, current scope, result *command.Result) error {
	if result.SkillBundle == nil {
		return nil
	}
	bundle, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, result.SkillBundle.Ref))
	if err != nil {
		return err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != bundle.projectID {
		return errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: bundle.ProjectRef}); err != nil {
		return err
	}
	for _, revision := range []*entity.SkillBundleRevision{result.SkillBundle.CurrentRevision, result.SkillBundle.DraftRevision} {
		if revision == nil {
			continue
		}
		if bundle.State == "PURGED" {
			revision.Files = nil
			continue
		}
		for _, file := range revision.Files {
			if err := repository.requireAccess(ctx, tx, current, "artifact.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: file.ArtifactRef}); err != nil {
				return err
			}
		}
	}
	if bundle.State == "PURGED" {
		result.SkillBundle.State = "PURGED"
	}
	return nil
}

func scanSkillBundle(row rowScanner) (skillBundleRow, error) {
	var result skillBundleRow
	var raw []byte
	if err := row.Scan(&result.id, &result.projectID, &result.currentRevisionID, &result.draftRevisionID, &raw); errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	} else if err != nil {
		return result, errs.ErrUnavailable
	}
	if json.Unmarshal(raw, &result.SkillBundle) != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) GetSkillBundle(ctx context.Context, principal value.Principal, ref string) (entity.SkillBundle, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.SkillBundle{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.SkillBundle{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	bundle, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, ref))
	if err != nil {
		return entity.SkillBundle{}, err
	}
	if current.authorityProjectID != "" && current.authorityProjectID != bundle.projectID {
		return entity.SkillBundle{}, errs.ErrForbidden
	}
	if err := repository.requireAccess(ctx, tx, current, "project.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "PROJECT", ResourceRef: bundle.ProjectRef}); err != nil {
		return entity.SkillBundle{}, err
	}
	for _, revision := range []*entity.SkillBundleRevision{bundle.CurrentRevision, bundle.DraftRevision} {
		if revision == nil {
			continue
		}
		for _, file := range revision.Files {
			if err := repository.requireAccess(ctx, tx, current, "artifact.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: file.ArtifactRef}); err != nil {
				return entity.SkillBundle{}, err
			}
		}
	}
	if tx.Commit(ctx) != nil {
		return entity.SkillBundle{}, errs.ErrUnavailable
	}
	return bundle.SkillBundle, nil
}

func (repository *Repository) resolveSkillFiles(ctx context.Context, tx pgx.Tx, current scope, projectID string, specification entity.SkillBundleSpecification) (entity.SkillBundleSpecification, string, error) {
	if strings.TrimSpace(specification.Name) == "" || len([]rune(specification.Name)) > 160 || len([]rune(specification.Description)) > 2000 || !utf8.ValidString(specification.Name+specification.Description) || strings.ContainsRune(specification.Name+specification.Description, 0) {
		return specification, "", errs.ErrInvalid
	}
	paths := make([]string, 0, len(specification.Files))
	for _, file := range specification.Files {
		paths = append(paths, file.Path)
	}
	if skillpolicy.ValidatePaths(paths) != nil {
		return specification, "", errs.ErrInvalid
	}
	specification.Files = append([]entity.SkillBundleFile(nil), specification.Files...)
	sort.Slice(specification.Files, func(i, j int) bool { return specification.Files[i].Path < specification.Files[j].Path })
	var total int64
	for i := range specification.Files {
		file := &specification.Files[i]
		if file.ArtifactRevision < 1 {
			return specification, "", errs.ErrInvalid
		}
		if err := repository.requireAccess(ctx, tx, current, "artifact.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: file.ArtifactRef}); err != nil {
			return specification, "", err
		}
		var scan, state string
		if err := tx.QueryRow(ctx, querySkillArtifactGet, current.organizationID, projectID, file.ArtifactRef, file.ArtifactRevision).Scan(&file.Digest, &file.SizeBytes, &scan, &state); errors.Is(err, pgx.ErrNoRows) {
			return specification, "", errs.ErrNotFound
		} else if err != nil {
			return specification, "", errs.ErrUnavailable
		}
		if state != "ACTIVE" || file.SizeBytes < 0 || file.SizeBytes > 32<<20 {
			return specification, "", errs.ErrConflict
		}
		total += file.SizeBytes
		if total > 64<<20 {
			return specification, "", errs.ErrInvalid
		}
	}
	raw, _ := json.Marshal(specification)
	digest := sha256.Sum256(raw)
	return specification, hex.EncodeToString(digest[:]), nil
}

func (repository *Repository) saveSkillDraft(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.SkillBundleInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	create := input.Kind == command.CreateSkillBundleDraft && payload.BundleRef == ""
	var id, ref, state string
	var version int64
	if create {
		ref, _ = newRef("sklb")
		if err := tx.QueryRow(ctx, querySkillBundleInsert, current.organizationID, ref, payload.ProjectRef, current.actorID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		ref = payload.BundleRef
		if err := tx.QueryRow(ctx, querySkillBundleLock, current.organizationID, ref).Scan(&id, &version, &state); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != version {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if state != "ACTIVE" {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	bundle, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, ref))
	if err != nil {
		return commandOutcome{}, err
	}
	if payload.ProjectRef != "" && payload.ProjectRef != bundle.ProjectRef {
		return commandOutcome{}, errs.ErrForbidden
	}
	specification, digest, err := repository.resolveSkillFiles(ctx, tx, current, bundle.projectID, payload.Specification)
	if err != nil {
		return commandOutcome{}, err
	}
	files, _ := json.Marshal(specification.Files)
	args := pgx.StrictNamedArgs{"organization_id": current.organizationID, "bundle_id": id, "name": specification.Name, "description": specification.Description, "files": files, "digest": digest}
	revisionID := bundle.draftRevisionID
	if input.Kind == command.CreateSkillBundleDraft {
		if revisionID != "" {
			return commandOutcome{}, errs.ErrConflict
		}
		revisionRef, _ := newRef("sklv")
		args["revision_ref"], args["actor_id"] = revisionRef, current.actorID
		if err := tx.QueryRow(ctx, querySkillRevisionInsert, args).Scan(&revisionID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else if input.Kind == command.SaveSkillBundleDraft {
		if bundle.DraftRevision == nil || payload.RevisionRef != bundle.DraftRevision.Ref {
			return commandOutcome{}, errs.ErrConflict
		}
		args["revision_ref"] = payload.RevisionRef
		tag, err := tx.Exec(ctx, querySkillRevisionSave, args)
		if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if tag.RowsAffected() != 1 {
			return commandOutcome{}, errs.ErrConflict
		}
	} else {
		return commandOutcome{}, errs.ErrInvalid
	}
	increment := int64(1)
	if create {
		increment = 0
	}
	if _, err := tx.Exec(ctx, querySkillBundleSetDraft, current.organizationID, id, revisionID, increment); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	bundle, err = scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, ref))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{SkillBundle: &bundle.SkillBundle}, resourceKind: "SKILL_BUNDLE", resourceRef: ref, projectID: bundle.projectID, projectRef: bundle.ProjectRef, summary: "i18n:SKILL_BUNDLE_CHANGED"}, nil
}
