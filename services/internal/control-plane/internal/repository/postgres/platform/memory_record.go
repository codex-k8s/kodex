package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/command"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

type memoryRecordRow struct {
	entity.KodexMemoryRecord
	id, projectID, agentID, currentRevisionID string
}

// Receipt хранит только ссылку на содержимое: retention и purge проверяются заново при replay.
func memoryReceiptMetadata(result command.Result) command.Result {
	if result.MemoryRecord != nil && result.MemoryRecord.CurrentRevision != nil {
		record := *result.MemoryRecord
		revision := *record.CurrentRevision
		revision.Summary = ""
		record.CurrentRevision = &revision
		result.MemoryRecord = &record
	}
	return result
}

func (repository *Repository) refreshMemoryReceipt(ctx context.Context, tx pgx.Tx, current scope, result *command.Result) error {
	if result.MemoryRecord == nil {
		return nil
	}
	if result.MemoryRecord.CurrentRevision == nil {
		return errs.ErrUnavailable
	}
	record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, result.MemoryRecord.Ref))
	if err != nil {
		return err
	}
	if err := repository.memoryRecordAccess(ctx, tx, current, record, false); err != nil {
		return err
	}
	var raw []byte
	if err := tx.QueryRow(ctx, queryMemoryRevisionGet, current.organizationID, record.Ref, result.MemoryRecord.CurrentRevision.Ref).Scan(&raw); err != nil {
		return errs.ErrUnavailable
	}
	var revision entity.MemoryRecordRevision
	if json.Unmarshal(raw, &revision) != nil {
		return errs.ErrUnavailable
	}
	if revision.Provenance.SourceRef != "" {
		if err := repository.requireAccess(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: revision.Provenance.SourceRef}); err != nil {
			return err
		}
	}
	result.MemoryRecord.CurrentRevision = &revision
	if record.State == "PURGED" {
		result.MemoryRecord.State = "PURGED"
	}
	return nil
}

func scanMemoryRecord(row rowScanner) (memoryRecordRow, error) {
	var result memoryRecordRow
	var raw []byte
	if err := row.Scan(&result.id, &result.projectID, &result.agentID, &result.currentRevisionID, &raw); errors.Is(err, pgx.ErrNoRows) {
		return result, errs.ErrNotFound
	} else if err != nil {
		return result, errs.ErrUnavailable
	}
	if json.Unmarshal(raw, &result.KodexMemoryRecord) != nil {
		return result, errs.ErrUnavailable
	}
	return result, nil
}

func (repository *Repository) memoryRecordAccess(ctx context.Context, tx pgx.Tx, current scope, record memoryRecordRow, write bool) error {
	if current.authorityProjectID != "" && current.authorityProjectID != record.projectID {
		return errs.ErrForbidden
	}
	permission, kind, ref := "project.view", "PROJECT", record.ProjectRef
	if record.AgentRef != "" {
		permission, kind, ref = "agent.view", "AGENT", record.AgentRef
	}
	if write {
		permission = strings.TrimSuffix(permission, "view") + "manage"
	}
	if err := repository.requireAccess(ctx, tx, current, permission, entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: kind, ResourceRef: ref}); err != nil {
		return err
	}
	return nil
}

func (repository *Repository) GetMemoryRecord(ctx context.Context, principal value.Principal, ref string) (entity.KodexMemoryRecord, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	current, err := repository.resolveScope(ctx, principal)
	if err != nil {
		return entity.KodexMemoryRecord{}, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return entity.KodexMemoryRecord{}, errs.ErrUnavailable
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, ref))
	if err != nil {
		return record.KodexMemoryRecord, err
	}
	if err := repository.memoryRecordAccess(ctx, tx, current, record, false); err != nil {
		return entity.KodexMemoryRecord{}, err
	}
	if record.CurrentRevision != nil && record.CurrentRevision.Provenance.SourceRef != "" {
		if err := repository.requireAccess(ctx, tx, current, "run.view", entity.AccessScope{Kind: "RESOURCE_INSTANCE", ResourceKind: "RUN", ResourceRef: record.CurrentRevision.Provenance.SourceRef}); err != nil {
			return entity.KodexMemoryRecord{}, err
		}
	}
	if tx.Commit(ctx) != nil {
		return entity.KodexMemoryRecord{}, errs.ErrUnavailable
	}
	return record.KodexMemoryRecord, nil
}

func (repository *Repository) changeMemoryRecord(ctx context.Context, tx pgx.Tx, current scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.MemoryRecordInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	create := input.Kind == command.CreateMemoryRecord
	var id, ref, state string
	var version int64
	if create {
		ref, _ = newRef("memr")
		if err := tx.QueryRow(ctx, queryMemoryRecordInsert, current.organizationID, ref, payload.ProjectRef, payload.AgentRef, current.actorID).Scan(&id); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
	} else {
		ref = payload.RecordRef
		if err := tx.QueryRow(ctx, queryMemoryRecordLock, current.organizationID, ref).Scan(&id, &version, &state); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if input.Mutation.ExpectedVersion == nil || version != *input.Mutation.ExpectedVersion {
			return commandOutcome{}, errs.ErrVersionMismatch
		}
		if state == "PURGED" {
			return commandOutcome{}, errs.ErrConflict
		}
	}
	if create || input.Kind == command.ReviseMemoryRecord {
		specification := payload.Specification
		if strings.TrimSpace(specification.Title) == "" || len([]rune(specification.Title)) > 160 || !utf8.ValidString(specification.Title) ||
			strings.TrimSpace(specification.Summary) == "" || len(specification.Summary) > 65536 || !utf8.ValidString(specification.Summary) ||
			strings.ContainsRune(specification.Title+specification.Summary, 0) || !specification.RetentionUntil.After(time.Now()) || specification.RetentionUntil.After(time.Now().Add(366*24*time.Hour)) {
			return commandOutcome{}, errs.ErrInvalid
		}
		raw, _ := json.Marshal(specification)
		digest := sha256.Sum256(raw)
		revisionRef, _ := newRef("memv")
		var revisionID string
		if err := tx.QueryRow(ctx, queryMemoryRevisionInsert, pgx.StrictNamedArgs{"organization_id": current.organizationID, "record_id": id, "revision_ref": revisionRef,
			"title": specification.Title, "summary": specification.Summary, "digest": hex.EncodeToString(digest[:]), "source_run_ref": specification.SourceRunRef,
			"retention_until": specification.RetentionUntil, "actor_id": current.actorID}).Scan(&revisionID); errors.Is(err, pgx.ErrNoRows) {
			return commandOutcome{}, errs.ErrNotFound
		} else if err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		increment := int64(1)
		if create {
			increment = 0
		}
		if _, err := tx.Exec(ctx, queryMemoryRecordSetRevision, current.organizationID, id, revisionID, increment); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	} else {
		target := ""
		switch input.Kind {
		case command.ArchiveMemoryRecord:
			if state != "ACTIVE" {
				return commandOutcome{}, errs.ErrConflict
			}
			target = "ARCHIVED"
		case command.RestoreMemoryRecord:
			if state != "ARCHIVED" {
				return commandOutcome{}, errs.ErrConflict
			}
			record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, ref))
			if err != nil {
				return commandOutcome{}, err
			}
			if record.CurrentRevision == nil || record.CurrentRevision.Redacted {
				return commandOutcome{}, errs.ErrConflict
			}
			target = "ACTIVE"
		case command.PurgeMemoryRecord:
			if state != "ARCHIVED" {
				return commandOutcome{}, errs.ErrConflict
			}
			target = "PURGED"
		default:
			return commandOutcome{}, errs.ErrInvalid
		}
		if _, err := tx.Exec(ctx, queryMemoryRecordSetState, current.organizationID, id, target); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if target != "ACTIVE" {
			if _, err := tx.Exec(ctx, queryMemoryRecordDisableBindings, current.organizationID, id); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
		if target == "PURGED" {
			if _, err := tx.Exec(ctx, queryMemoryRecordPurge, current.organizationID, id); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
		}
	}
	record, err := scanMemoryRecord(tx.QueryRow(ctx, queryMemoryRecordGet, current.organizationID, ref))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{MemoryRecord: &record.KodexMemoryRecord}, resourceKind: "MEMORY_RECORD", resourceRef: ref,
		projectID: record.projectID, projectRef: record.ProjectRef, summary: "i18n:MEMORY_RECORD_CHANGED"}, nil
}
