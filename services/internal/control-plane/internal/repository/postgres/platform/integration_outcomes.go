package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"hash"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/value"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowEvidenceReadSource resolves live lease and immediate server lineage.
// queryWorkflowEvidenceReadSource разрешает live lease и непосредственную server lineage.
//
//go:embed sql/workflow_evidence__read_source.sql
var queryWorkflowEvidenceReadSource string

// queryWorkflowEvidenceReadOutcomes streams all settled owner fields in ref order.
// queryWorkflowEvidenceReadOutcomes читает все завершённые owner fields в порядке ref.
//
//go:embed sql/workflow_evidence__read_outcomes.sql
var queryWorkflowEvidenceReadOutcomes string

// workflowEvidencePageSink hashes the full bounded stream while retaining at most
// one requested page. It never buffers the complete history or logs its contents.
// workflowEvidencePageSink хеширует полный ограниченный поток, сохраняя лишь одну
// запрошенную страницу. Полная история не буферизуется и не попадает в журналы.
type workflowEvidencePageSink struct {
	hash         hash.Hash
	size, offset int64
	maximum      int
	page         []byte
}

// newWorkflowEvidencePageSink allocates only the requested byte window.
// newWorkflowEvidencePageSink выделяет лишь запрошенное окно байтов.
func newWorkflowEvidencePageSink(offset int64, maximum int) *workflowEvidencePageSink {
	return &workflowEvidencePageSink{hash: sha256.New(), offset: offset, maximum: maximum, page: make([]byte, 0, maximum)}
}

// Write commits every byte to the digest and copies only its intersection with
// the requested window. Overflow rejects the entire read rather than truncating it.
// Write включает каждый байт в digest и копирует лишь пересечение с запрошенным
// окном. Превышение отклоняет всё чтение вместо усечения истории.
func (sink *workflowEvidencePageSink) Write(raw []byte) (int, error) {
	end := sink.size + int64(len(raw))
	if end > maximumWorkflowEvidenceSourceBytes {
		return 0, errWorkflowEvidenceDeliveryLimit
	}
	start, finish := max(sink.size, sink.offset), min(end, sink.offset+int64(sink.maximum))
	if start < finish {
		sink.page = append(sink.page, raw[start-sink.size:finish-sink.size]...)
	}
	_, _ = sink.hash.Write(raw)
	sink.size = end
	return len(raw), nil
}

// digest returns the exact complete source commitment, independently of the page.
// digest возвращает точный digest полного источника независимо от страницы.
func (sink *workflowEvidencePageSink) digest() string { return hex.EncodeToString(sink.hash.Sum(nil)) }

// result validates a UTF-8 boundary and trims only the page end; source bytes are
// untouched, and next offset guarantees progress unless the exact EOF is requested.
// result проверяет UTF-8 boundary и подрезает лишь конец страницы; source bytes не
// меняются, next offset гарантирует прогресс кроме запроса точного EOF.
func (sink *workflowEvidencePageSink) result() (string, int64, bool, error) {
	if sink.offset > sink.size || len(sink.page) > 0 && !utf8.RuneStart(sink.page[0]) {
		return "", 0, false, errs.ErrInvalid
	}
	page := sink.page
	for trim := 0; !utf8.Valid(page) && len(page) > 0 && trim < 3; trim++ {
		page = page[:len(page)-1]
	}
	if !utf8.Valid(page) || len(page) == 0 && sink.offset < sink.size {
		return "", 0, false, errs.ErrInvalid
	}
	next := sink.offset + int64(len(page))
	return string(page), next, next == sink.size, nil
}

// GetExecutionIntegrationOutcomes bounds a read and retries only a serialization
// conflict using a fresh owner transaction, without performing external actions.
// GetExecutionIntegrationOutcomes ограничивает чтение и повторяет лишь serialization
// conflict в свежей owner transaction без внешних действий.
func (repository *Repository) GetExecutionIntegrationOutcomes(ctx context.Context, p value.Principal, input query.ExecutionIntegrationOutcomes) (entity.ExecutionIntegrationOutcomes, error) {
	ctx, cancel := context.WithTimeout(ctx, catalogQueryTimeout)
	defer cancel()
	return retryAssistantLockedRead(ctx, func(ctx context.Context) (entity.ExecutionIntegrationOutcomes, error) {
		return repository.executionIntegrationOutcomesOnce(ctx, p, input)
	})
}

// executionIntegrationOutcomesOnce locks the live continuation/actor/root boundary,
// streams its exact predecessor and verifies both digest and materialized task before
// commit. Cancellation, foreign lineage, source drift and malformed pages return no data.
// executionIntegrationOutcomesOnce блокирует live continuation/actor/root boundary,
// читает точного предшественника и сверяет digest с materialized task до commit.
// Cancel, чужая lineage, изменение источника и malformed pages не возвращают данные.
func (repository *Repository) executionIntegrationOutcomesOnce(ctx context.Context, p value.Principal, input query.ExecutionIntegrationOutcomes) (_ entity.ExecutionIntegrationOutcomes, resultError error) {
	empty := entity.ExecutionIntegrationOutcomes{}
	if input.OffsetBytes < 0 || input.OffsetBytes > maximumWorkflowEvidenceSourceBytes || input.MaximumBytes < 4 || input.MaximumBytes > 4096 {
		return empty, errs.ErrInvalid
	}
	current, err := repository.resolveScope(ctx, p)
	if err != nil {
		return empty, err
	}
	tx, err := repository.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	defer rollbackAssistantLockedRead(ctx, tx, &resultError)
	fence := sha256.Sum256([]byte(input.Fence))
	var sourceID, task, currentTask string
	err = readWorkflowEvidenceWithGenericPlan(ctx, tx, func() error {
		return tx.QueryRow(ctx, queryWorkflowEvidenceReadSource, pgx.StrictNamedArgs{"organization_id": current.organizationID, "lease_ref": input.LeaseRef, "fence_digest": hex.EncodeToString(fence[:]), "generation": input.Generation, "source_invocation_ref": input.SourceInvocationRef}).Scan(&sourceID, &task, &currentTask)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, errs.ErrNotFound
	}
	if err != nil {
		return empty, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	rows, err := tx.Query(ctx, queryWorkflowEvidenceReadOutcomes, pgx.StrictNamedArgs{"organization_id": current.organizationID, "source_node_id": sourceID})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	sink := newWorkflowEvidencePageSink(input.OffsetBytes, int(input.MaximumBytes))
	count, readErr := writeWorkflowEvidenceSnapshot(sink, task, func(yield func(workflowEvidenceOutcome) error) error {
		for rows.Next() {
			var outcome workflowEvidenceOutcome
			var gateRef, decision, comment string
			var version int64
			if err := rows.Scan(&outcome.InvocationRef, &outcome.Operation, &outcome.State, &outcome.ResultSummary, &outcome.SafeErrorCode, &outcome.ReceiptRef, &outcome.InputDigest, &gateRef, &version, &decision, &comment); err != nil {
				return errs.ErrUnavailable
			}
			if gateRef != "" {
				outcome.OwnerDecision = map[string]any{"gateRef": gateRef, "version": version, "decision": decision, "comment": comment}
			}
			if err := yield(outcome); err != nil {
				return err
			}
		}
		return rows.Err()
	})
	rows.Close()
	if readErr != nil {
		return empty, errs.ErrUnavailable
	}
	if sink.digest() != input.SourceSHA256 || currentTask != workflowEvidenceManifestInstruction(task, input.SourceInvocationRef, sink.digest(), sink.size, count) {
		return empty, errs.ErrConflict
	}
	text, next, eof, err := sink.result()
	if err != nil {
		return empty, err
	}
	if err := tx.Commit(ctx); err != nil {
		return empty, assistantLockedReadError(err, errs.ErrUnavailable)
	}
	return entity.ExecutionIntegrationOutcomes{SourceInvocationRef: input.SourceInvocationRef, SourceSHA256: sink.digest(), SizeBytes: sink.size, OutcomesCount: int32(count), OffsetBytes: input.OffsetBytes, NextOffsetBytes: next, EOF: eof, PageSHA256: workflowResultSHA([]byte(text)), Text: text}, nil
}
