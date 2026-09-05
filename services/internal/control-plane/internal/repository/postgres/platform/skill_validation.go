package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/skillpolicy"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) ConfigureSkillScanner(scanner skillpolicy.Scanner) error {
	if scanner == nil {
		return errs.ErrInvalid
	}
	repository.skillScanner = scanner
	return nil
}

func skillScanDigest(digest, engine string) string {
	value := sha256.Sum256([]byte(digest + "\n" + engine))
	return hex.EncodeToString(value[:])
}

func (repository *Repository) readSkillFile(ctx context.Context, tx pgx.Tx, current scope, projectID string, file entity.SkillBundleFile) ([]byte, error) {
	if err := repository.requireAccess(ctx, tx, current, "artifact.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "ARTIFACT", ResourceRef: file.ArtifactRef}); err != nil {
		return nil, err
	}
	var key, version, etag, digest string
	var size int64
	if err := tx.QueryRow(ctx, querySkillArtifactContent, current.organizationID, projectID, file.ArtifactRef, file.ArtifactRevision).Scan(&key, &version, &etag, &digest, &size); err != nil {
		return nil, errs.ErrConflict
	}
	if size < 0 || size > 32<<20 || size != file.SizeBytes || digest != file.Digest {
		return nil, errs.ErrConflict
	}
	object, err := repository.objects.Get(ctx, key, version)
	if err != nil {
		return nil, errs.ErrUnavailable
	}
	defer func() { _ = object.Body.Close() }()
	if object.Digest != digest || object.SizeBytes != size || object.VersionID != version || object.ETag != etag {
		return nil, errs.ErrConflict
	}
	body, err := io.ReadAll(io.LimitReader(object.Body, size+1))
	if err != nil || int64(len(body)) != size {
		return nil, errs.ErrUnavailable
	}
	actual := sha256.Sum256(body)
	if hex.EncodeToString(actual[:]) != strings.TrimPrefix(digest, "sha256:") {
		return nil, errs.ErrConflict
	}
	return body, nil
}

func (repository *Repository) inspectSkill(ctx context.Context, tx pgx.Tx, current scope, bundle skillBundleRow) (string, string, []string) {
	revision := bundle.DraftRevision
	if repository.skillScanner == nil {
		return "ERROR", "", []string{"SKILL_MALWARE_SCANNER_UNAVAILABLE"}
	}
	paths := make([]string, 0, len(revision.Files))
	for _, file := range revision.Files {
		paths = append(paths, file.Path)
	}
	if skillpolicy.ValidatePaths(paths) != nil {
		return "ERROR", "", []string{"SKILL_STRUCTURE_INVALID"}
	}
	engine := ""
	for _, file := range revision.Files {
		body, err := repository.readSkillFile(ctx, tx, current, bundle.projectID, file)
		if err != nil {
			return "ERROR", "", []string{"SKILL_FILE_UNAVAILABLE"}
		}
		if file.Path == "SKILL.md" {
			manifest, err := skillpolicy.ParseManifest(body)
			if err != nil || manifest.Name != revision.Name || manifest.Description != revision.Description {
				return "ERROR", "", []string{"SKILL_MANIFEST_INVALID"}
			}
		}
		verdict, err := repository.skillScanner.Scan(ctx, body)
		if err != nil || verdict.Engine == "" || (engine != "" && verdict.Engine != engine) {
			return "ERROR", "", []string{"SKILL_MALWARE_SCANNER_UNAVAILABLE"}
		}
		engine = verdict.Engine
		if verdict.Infected {
			return "INFECTED", engine, []string{"SKILL_MALWARE_DETECTED"}
		}
	}
	return "CLEAN", engine, []string{}
}

func (repository *Repository) validateSkillDraft(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.SkillBundleInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	var id, state string
	var version int64
	if err := tx.QueryRow(ctx, querySkillBundleLock, current.organizationID, payload.BundleRef).Scan(&id, &version, &state); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if input.Mutation.ExpectedVersion == nil || *input.Mutation.ExpectedVersion != version {
		return commandOutcome{}, errs.ErrVersionMismatch
	}
	if state != "ACTIVE" {
		return commandOutcome{}, errs.ErrConflict
	}
	bundle, err := scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, payload.BundleRef))
	if err != nil {
		return commandOutcome{}, err
	}
	if bundle.DraftRevision == nil || payload.RevisionRef != bundle.DraftRevision.Ref || payload.ExpectedDigest != bundle.DraftRevision.Digest {
		return commandOutcome{}, errs.ErrConflict
	}
	if !contains([]string{"DRAFT", "INVALID", "VALIDATED", "REJECTED"}, bundle.DraftRevision.State) {
		return commandOutcome{}, errs.ErrConflict
	}
	scan, engine, diagnostics := repository.inspectSkill(ctx, tx, current, bundle)
	state = "INVALID"
	scanDigest := ""
	if scan == "CLEAN" {
		state = "VALIDATED"
	}
	if engine != "" {
		scanDigest = skillScanDigest(bundle.DraftRevision.Digest, engine)
	}
	raw, _ := json.Marshal(diagnostics)
	tag, err := tx.Exec(ctx, querySkillRevisionValidate, pgx.StrictNamedArgs{"organization_id": current.organizationID, "bundle_id": id, "revision_ref": payload.RevisionRef, "digest": payload.ExpectedDigest, "state": state, "scan_state": scan, "scan_engine": engine, "scan_digest": scanDigest, "diagnostics": raw})
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if tag.RowsAffected() != 1 {
		return commandOutcome{}, errs.ErrConflict
	}
	if _, err := tx.Exec(ctx, querySkillBundleSetDraft, current.organizationID, id, bundle.draftRevisionID, int64(1)); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	bundle, err = scanSkillBundle(tx.QueryRow(ctx, querySkillBundleGet, current.organizationID, payload.BundleRef))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{SkillBundle: &bundle.SkillBundle}, resourceKind: "SKILL_BUNDLE", resourceRef: bundle.Ref, projectID: bundle.projectID, projectRef: bundle.ProjectRef, summary: "i18n:SKILL_BUNDLE_CHANGED"}, nil
}
