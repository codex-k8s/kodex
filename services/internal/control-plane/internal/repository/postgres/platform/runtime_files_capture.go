package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"time"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
)

var (
	// queryRuntimeCallbackReceiptExists embeds exact tenant/child-run/callback-edge receipt existence
	// lookup. It locates an already persisted callback fact without assigning lineage or permission
	// from request identifiers.
	// queryRuntimeCallbackReceiptExists встраивает проверку существования квитанции точных
	// организации, дочернего запуска и callback-ребра. Находит уже сохранённый факт callback без
	// назначения происхождения и прав из идентификаторов запроса.
	//go:embed sql/runtime_callback_receipt_exists.sql
	queryRuntimeCallbackReceiptExists string
	// queryRuntimeFilesCoordinator embeds server-owned coordinator eligibility for the exact run/node
	// using the root initiator, project and agent. It does not trust a task flag, capability list or
	// caller-supplied graph relation.
	// queryRuntimeFilesCoordinator встраивает серверную проверку допустимости координатора точных
	// run/node по root-инициатору, проекту и агенту. Не доверяет флагу task, списку capabilities и
	// переданной клиентом связи графа.
	//go:embed sql/runtime_files_coordinator.sql
	queryRuntimeFilesCoordinator string
	// queryRuntimeFilesCaptureCatalog embeds catalog identity creation from exact owner
	// run/node/session/turn/agent rows with a supplied fresh revision ref/generation and closed
	// purposes. The deferred revision binding requires the caller to finish the same RuntimeRevision
	// transaction.
	// queryRuntimeFilesCaptureCatalog встраивает создание идентичности каталога из точных строк
	// владельца run/node/session/turn/agent с переданными свежими revision ref/generation и закрытыми
	// purposes. Отложенная привязка ревизии требует завершить ту же транзакцию RuntimeRevision.
	//go:embed sql/runtime_files_capture_catalog.sql
	queryRuntimeFilesCaptureCatalog string
	// queryRuntimeFilesCaptureEntries embeds exact purpose-specific project/result/input/skill
	// candidates, immutable content pins and authoritative runtime file visibility before persisting
	// entry digests. Coordinator results come from received graph results; input/skill revisions use
	// frozen pins rather than a new head. It does not read object bytes or create standalone
	// authority.
	// queryRuntimeFilesCaptureEntries встраивает точные purpose-specific project/result/input/skill
	// кандидаты, неизменяемые content pins и авторитетную видимость runtime-файлов перед сохранением
	// дайджестов элементов. Результаты координатора берутся из полученных результатов графа;
	// input/skill ревизии используют frozen pins, а не новый head. Не читает байты объектов и не
	// создаёт отдельные полномочия.
	//go:embed sql/runtime_files_capture_entries.sql
	queryRuntimeFilesCaptureEntries string
	// queryRuntimeFilesCaptureDigests embeds ordered entry ref/digest streaming for one catalog ID.
	// The caller requires fixed SHA-256 digest length and includes each ref in the catalog commitment.
	// queryRuntimeFilesCaptureDigests встраивает упорядоченное потоковое чтение ref/digest элементов
	// одного каталога. Вызывающий код требует фиксированную длину SHA-256 и включает каждый ref в
	// обязательство каталога.
	//go:embed sql/runtime_files_capture_digests.sql
	queryRuntimeFilesCaptureDigests string
	// queryRuntimeFilesFreezeCatalog embeds one-way freeze of an unfrozen catalog with its computed
	// digest and total. Exactly one affected row is required before publishing catalog metadata into
	// the owner snapshot.
	// queryRuntimeFilesFreezeCatalog встраивает однонаправленную фиксацию незамороженного каталога с
	// вычисленными дайджестом и числом элементов. До помещения данных каталога в owner snapshot
	// требуется ровно одна изменённая строка.
	//go:embed sql/runtime_files_freeze_catalog.sql
	queryRuntimeFilesFreezeCatalog string
)

// captureRuntimeFileCatalog within a five-second caller-owned transaction segment, derives closed
// purposes from runtime capabilities, server coordinator eligibility, inputs and skills. It
// inserts eligible entries and streams a domain-separated ref/digest commitment, freezes exactly
// one catalog and attaches metadata before RuntimeRevision insertion. No project or purposes means
// no catalog; it never materializes the whole file catalog in Go memory or commits independently.
// captureRuntimeFileCatalog в пятисекундном участке транзакции вызывающего кода выводит закрытые
// purposes из runtime-capabilities, серверной допустимости координатора, inputs и skills.
// Записывает допустимые элементы, потоково вычисляет разделённое доменом ref/digest обязательство,
// замораживает ровно один каталог и добавляет метаданные до вставки RuntimeRevision. Без проекта
// либо purposes каталог не создаётся; весь каталог не материализуется в памяти Go и отдельного
// Commit нет.
func captureRuntimeFileCatalog(ctx context.Context, tx pgx.Tx, current scope, snapshot map[string]any, skills runtimecontract.RuntimeContextSnapshot) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if stringMap(snapshot, "projectRef") == "" {
		return nil
	}
	purposes := make([]string, 0, 4)
	if capabilityEnabled(runtimeRevisionStringSlice(snapshot["capabilities"]), runtimecontract.ArtifactCapability) {
		purposes = append(purposes, runtimecontract.FilePurposeProject, runtimecontract.FilePurposeRunResult)
	} else {
		// Право чтения результата координатора принадлежит server-owned графу,
		// а не capabilities либо refs из его task/input.
		var coordinator bool
		if err := tx.QueryRow(ctx, queryRuntimeFilesCoordinator, pgx.StrictNamedArgs{
			"organization_id": current.organizationID, "run_ref": stringMap(snapshot, "runRef"), "node_ref": stringMap(snapshot, "nodeRef"),
		}).Scan(&coordinator); err != nil {
			return errors.Join(errs.ErrUnavailable, err)
		}
		if coordinator {
			purposes = append(purposes, runtimecontract.FilePurposeRunResult)
		}
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
		if errors.Is(err, pgx.ErrNoRows) {
			return errs.ErrConflict
		}
		return errors.Join(errs.ErrUnavailable, err)
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
		return errors.Join(errs.ErrUnavailable, err)
	}
	rows, err := tx.Query(ctx, queryRuntimeFilesCaptureDigests, pgx.StrictNamedArgs{"catalog_id": catalogID})
	if err != nil {
		return errors.Join(errs.ErrUnavailable, err)
	}
	// Streaming commitment не материализует все файлы проекта в памяти Go
	// или bounded RuntimeRevision JSON. Каждый entry digest имеет фиксированную длину.
	hash := sha256.New()
	_, _ = hash.Write([]byte("kodex.runtime-file-catalog.v1\x00" + ref + "\x00"))
	var total int64
	for rows.Next() {
		var entryRef string
		var entryDigest []byte
		if err := rows.Scan(&entryRef, &entryDigest); err != nil {
			rows.Close()
			return errors.Join(errs.ErrUnavailable, err)
		}
		if len(entryDigest) != sha256.Size {
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
		return errors.Join(errs.ErrUnavailable, err)
	}
	catalog := runtimecontract.RuntimeFileCatalog{Ref: ref, Digest: hex.EncodeToString(hash.Sum(nil)), Total: total, Purposes: purposes}
	if catalog.Validate() != nil {
		return errs.ErrConflict
	}
	tag, err := tx.Exec(ctx, queryRuntimeFilesFreezeCatalog, pgx.StrictNamedArgs{"catalog_id": catalogID, "digest": catalog.Digest, "total": total})
	if err != nil {
		return errors.Join(errs.ErrUnavailable, err)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	snapshot["fileCatalog"] = catalog
	return nil
}
