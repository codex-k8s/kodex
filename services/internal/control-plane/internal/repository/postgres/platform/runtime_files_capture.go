package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

var (
	//go:embed sql/runtime_files_capture_catalog.sql
	queryRuntimeFilesCaptureCatalog string
	//go:embed sql/runtime_files_capture_entries.sql
	queryRuntimeFilesCaptureEntries string
	//go:embed sql/runtime_files_capture_digests.sql
	queryRuntimeFilesCaptureDigests string
	//go:embed sql/runtime_files_freeze_catalog.sql
	queryRuntimeFilesFreezeCatalog string
)

// captureRuntimeFileCatalog вызывается внутри owner transaction до вставки
// RuntimeRevision. Deferred binding не позволяет зафиксировать отдельный grant.
func captureRuntimeFileCatalog(ctx context.Context, tx pgx.Tx, current scope, snapshot map[string]any, skills runtimecontract.RuntimeContextSnapshot) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if stringMap(snapshot, "projectRef") == "" {
		return nil
	}
	purposes := make([]string, 0, 4)
	if capabilityEnabled(runtimeRevisionStringSlice(snapshot["capabilities"]), runtimecontract.ArtifactCapability) {
		purposes = append(purposes, runtimecontract.FilePurposeProject, runtimecontract.FilePurposeRunResult)
	}
	if len(runtimeRevisionArtifacts(snapshot["artifacts"])) > 0 {
		purposes = append(purposes, runtimecontract.FilePurposeWorkspaceInput)
	}
	if len(skills.Skills) > 0 {
		purposes = append(purposes, runtimecontract.FilePurposeSkill)
	}
	if len(purposes) == 0 {
		return nil
	}
	sort.Strings(purposes)
	ref, err := newRef("vfc")
	if err != nil {
		return err
	}
	var catalogID string
	if err := tx.QueryRow(ctx, queryRuntimeFilesCaptureCatalog, pgx.StrictNamedArgs{
		"catalog_ref": ref, "organization_id": current.organizationID,
		"run_ref": stringMap(snapshot, "runRef"), "node_ref": stringMap(snapshot, "nodeRef"),
		"revision_ref": stringMap(snapshot, "runtimeRevisionRef"), "generation": runtimeRevisionMapInt64(snapshot, "runtimeRevisionVersion"), "purposes": purposes,
	}).Scan(&catalogID); err != nil {
		return errs.ErrConflict
	}
	inputs, err := json.Marshal(snapshot["artifacts"])
	if err != nil {
		return errs.ErrConflict
	}
	if string(inputs) == "null" {
		inputs = []byte("[]")
	}
	skillFiles, err := json.Marshal(skills.Skills)
	if err != nil {
		return errs.ErrConflict
	}
	if string(skillFiles) == "null" {
		skillFiles = []byte("[]")
	}
	if _, err := tx.Exec(ctx, queryRuntimeFilesCaptureEntries, pgx.StrictNamedArgs{
		"catalog_id": catalogID, "inputs": inputs, "skills": skillFiles,
	}); err != nil {
		return errs.ErrConflict
	}
	rows, err := tx.Query(ctx, queryRuntimeFilesCaptureDigests, pgx.StrictNamedArgs{"catalog_id": catalogID})
	if err != nil {
		return errs.ErrUnavailable
	}
	// Streaming commitment не материализует все файлы проекта в памяти Go
	// или bounded RuntimeRevision JSON. Каждый entry digest имеет фиксированную длину.
	hash := sha256.New()
	_, _ = hash.Write([]byte("kodex.runtime-file-catalog.v1\x00" + ref + "\x00"))
	var total int64
	for rows.Next() {
		var entryRef string
		var entryDigest []byte
		if rows.Scan(&entryRef, &entryDigest) != nil || len(entryDigest) != sha256.Size {
			rows.Close()
			return errs.ErrConflict
		}
		_, _ = hash.Write([]byte(entryRef + "\x00"))
		_, _ = hash.Write(entryDigest)
		total++
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return errs.ErrUnavailable
	}
	catalog := runtimecontract.RuntimeFileCatalog{Ref: ref, Digest: hex.EncodeToString(hash.Sum(nil)), Total: total, Purposes: purposes}
	if catalog.Validate() != nil {
		return errs.ErrConflict
	}
	tag, err := tx.Exec(ctx, queryRuntimeFilesFreezeCatalog, pgx.StrictNamedArgs{"catalog_id": catalogID, "digest": catalog.Digest, "total": total})
	if err != nil || tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	snapshot["fileCatalog"] = catalog
	return nil
}
