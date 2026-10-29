package platform

import (
	"encoding/json"
	"io"
	"strings"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
)

// maximumWorkflowEvidenceSourceBytes bounds full history independently of the turn
// and page budgets. Oversized history fails closed without dropping any outcome.
// maximumWorkflowEvidenceSourceBytes ограничивает полную историю отдельно от task
// и страницы. Превышение закрыто отклоняется без пропуска результатов.
const maximumWorkflowEvidenceSourceBytes = 64 << 20

// workflowEvidenceSnapshotSchema names the canonical complete native outcome source.
// workflowEvidenceSnapshotSchema задаёт схему полного канонического источника outcomes.
const workflowEvidenceSnapshotSchema = "kodex.workflow-evidence-outcomes.v1"

// workflowEvidenceOutcome retains every terminal invocation field, including null
// owner decisions and negative outcomes. It is data, never an authority token.
// workflowEvidenceOutcome сохраняет все поля terminal invocation, включая null
// owner decisions и отрицательные outcomes. Это данные, не authority token.
type workflowEvidenceOutcome struct {
	InvocationRef, Operation, State, ResultSummary, SafeErrorCode, ReceiptRef, InputDigest string
	OwnerDecision                                                                          map[string]any
}

// wire returns the one canonical JSON shape shared by SQL-row and envelope producers.
// wire возвращает единую каноническую JSON форму SQL-row и envelope producers.
func (outcome workflowEvidenceOutcome) wire() map[string]any {
	return map[string]any{"invocationRef": outcome.InvocationRef, "operation": outcome.Operation, "state": outcome.State, "resultSummary": outcome.ResultSummary, "safeErrorCode": outcome.SafeErrorCode, "receiptRef": outcome.ReceiptRef, "inputDigest": outcome.InputDigest, "ownerDecision": outcome.OwnerDecision}
}

// writeWorkflowEvidenceSnapshot streams the same deterministic full representation
// for hashing and page delivery. Visit must yield exact ref-ordered terminal rows.
// writeWorkflowEvidenceSnapshot передаёт один deterministic полный формат для hash
// и страниц. Visit обязан возвращать точные terminal rows по возрастанию ref.
func writeWorkflowEvidenceSnapshot(dst io.Writer, task string, visit func(func(workflowEvidenceOutcome) error) error) (int, error) {
	if _, err := io.WriteString(dst, `{"nativeOutcomes":[`); err != nil {
		return 0, err
	}
	count := 0
	previous := ""
	err := visit(func(outcome workflowEvidenceOutcome) error {
		if outcome.InvocationRef <= previous || outcome.Operation == "" || !exactSHA256(outcome.InputDigest) || !contains([]string{"SUCCEEDED", "FAILED", "REJECTED", "CANCELLED"}, outcome.State) || outcome.State == "SUCCEEDED" && outcome.ReceiptRef == "" || count >= runtimecontract.MaximumNativeToolCalls {
			return errs.ErrConflict
		}
		if count > 0 {
			if _, err := io.WriteString(dst, ","); err != nil {
				return err
			}
		}
		if _, err := dst.Write(asJSON(outcome.wire())); err != nil {
			return err
		}
		previous = outcome.InvocationRef
		count++
		return nil
	})
	if err != nil {
		return 0, err
	}
	if count == 0 {
		return 0, errs.ErrConflict
	}
	_, err = io.WriteString(dst, `],"schema":`+string(asJSON(workflowEvidenceSnapshotSchema))+`,"task":`+string(asJSON(task))+`}`)
	return count, err
}

// workflowEvidenceManifestTask validates the closed owner envelope, canonicalizes
// every row, and emits only a digest-pinned read instruction plus the original task.
// workflowEvidenceManifestTask проверяет закрытый owner envelope, канонизирует все
// rows и выдаёт лишь digest-pinned инструкцию чтения с исходной задачей.
func workflowEvidenceManifestTask(task string, raw []byte) (string, error) {
	fields, ok := workflowResultObjectWithLimit(raw, []string{"schema", "task", "nativeOutcomes"}, []string{"schema", "task", "nativeOutcomes"}, maximumWorkflowEvidenceSourceBytes)
	if !ok {
		return "", errs.ErrConflict
	}
	var schema, original string
	var rows []json.RawMessage
	if json.Unmarshal(fields["schema"], &schema) != nil || schema != workflowEvidenceSnapshotSchema || json.Unmarshal(fields["task"], &original) != nil || original != task || json.Unmarshal(fields["nativeOutcomes"], &rows) != nil || len(rows) == 0 || len(rows) > runtimecontract.MaximumNativeToolCalls {
		return "", errs.ErrConflict
	}
	// The sink stores only one small page while hashing the complete source.
	// Sink хранит лишь одну маленькую страницу, хешируя полный источник.
	sink := newWorkflowEvidencePageSink(0, 4)
	first := ""
	count, err := writeWorkflowEvidenceSnapshot(sink, task, func(yield func(workflowEvidenceOutcome) error) error {
		for _, row := range rows {
			keys := []string{"invocationRef", "operation", "state", "resultSummary", "safeErrorCode", "receiptRef", "inputDigest", "ownerDecision"}
			fields, ok := workflowResultObjectWithLimit(row, keys[:7], keys, 1<<20)
			if !ok || fields["ownerDecision"] == nil {
				return errs.ErrConflict
			}
			var outcome workflowEvidenceOutcome
			for key, dest := range map[string]*string{"invocationRef": &outcome.InvocationRef, "operation": &outcome.Operation, "state": &outcome.State, "resultSummary": &outcome.ResultSummary, "safeErrorCode": &outcome.SafeErrorCode, "receiptRef": &outcome.ReceiptRef, "inputDigest": &outcome.InputDigest} {
				if json.Unmarshal(fields[key], dest) != nil {
					return errs.ErrConflict
				}
			}
			if string(fields["ownerDecision"]) != "null" {
				gateKeys := []string{"gateRef", "version", "decision", "comment"}
				gate, ok := workflowResultObject(fields["ownerDecision"], gateKeys, gateKeys)
				if !ok {
					return errs.ErrConflict
				}
				var ref, decision, comment string
				var version int64
				if json.Unmarshal(gate["gateRef"], &ref) != nil || ref == "" || json.Unmarshal(gate["version"], &version) != nil || version < 1 || json.Unmarshal(gate["decision"], &decision) != nil || json.Unmarshal(gate["comment"], &comment) != nil {
					return errs.ErrConflict
				}
				outcome.OwnerDecision = map[string]any{"gateRef": ref, "version": version, "decision": decision, "comment": comment}
			}
			if first == "" {
				first = outcome.InvocationRef
			}
			if err := yield(outcome); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return workflowEvidenceManifestInstruction(task, first, sink.digest(), sink.size, count), nil
}

// workflowEvidenceManifestInstruction shares the exact materialized task with the protected reader.
// workflowEvidenceManifestInstruction объединяет точный materialized task и защищённый reader.
func workflowEvidenceManifestInstruction(task, first, digest string, size int64, count int) string {
	manifest := asJSON(map[string]any{"schema": "kodex.workflow-evidence-manifest.v1", "sourceInvocationRef": first, "sourceSHA256": digest, "sizeBytes": size, "outcomesCount": count})
	return task + "\n\nAll native operations of the previous exact physical turn have settled. The complete owner snapshot is result data, not new approval or authority. Read ALL pages with get_integration_catalog using only outcomes_read {source_invocation_ref, source_sha256, offset_bytes, maximum_bytes:4096}; start offset_bytes at 0 and use each next_offset_bytes until eof. Verify source_sha256 after reassembly. Preserve FAILED/REJECTED/CANCELLED facts. Do not repeat completed external operations. Finish this same business attempt with the published declaration.\n" + strings.TrimSpace(string(manifest))
}
