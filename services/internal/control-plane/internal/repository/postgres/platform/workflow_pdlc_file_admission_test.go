package platform

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// workflowPDLCFileAdmissionTx provides native ledger rows to the real owner admission function.
// Embedded writes remain unavailable except the exact existing rejoin append. It does not prove SQL authority.
// workflowPDLCFileAdmissionTx передаёт native ledger rows настоящей owner admission функции.
// Запись недоступна, кроме exact существующего rejoin append. Оснастка не доказывает SQL authority.
type workflowPDLCFileAdmissionTx struct {
	pgx.Tx
	proofs                 map[string]workflowEffectNativeProof
	readError, appendError error
	appended               []workflowEffectRejoin
}

// QueryRow permits only the canonical receipt query and exact owner/receipt locators.
// QueryRow допускает только canonical receipt запрос и exact owner/receipt locators.
func (tx *workflowPDLCFileAdmissionTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	if query != queryWorkflowEffectReceipt || tx.readError != nil {
		return workflowEvaluatorRow{err: errs.ErrUnavailable}
	}
	pins := args[0].(pgx.StrictNamedArgs)
	p, found := tx.proofs[pins["invocation_ref"].(string)]
	if !found || pins["organization_id"] != "owner" || pins["root_run_id"] != "root" || pins["anchor_id"] != "anchor" {
		return workflowEvaluatorRow{err: pgx.ErrNoRows}
	}
	return workflowEvaluatorRow{values: []any{p.InvocationRef, p.State, p.EffectKey, p.InputDigest, p.ReceiptRef, p.ReceiptDigest, p.PublicationRef, p.OriginalKey, p.GateRef, p.Input, p.Manifest, p.SourcePolicy}}
}

// Exec records only owner-generated rejoin data and rejects invocation insertion by construction.
// Exec сохраняет только owner-generated rejoin data и конструктивно запрещает вставку invocation.
func (tx *workflowPDLCFileAdmissionTx) Exec(_ context.Context, query string, args ...any) (pgconn.CommandTag, error) {
	if query != queryWorkflowEffectAppend || tx.appendError != nil {
		return pgconn.CommandTag{}, errs.ErrUnavailable
	}
	pins := args[0].(pgx.StrictNamedArgs)
	var record workflowEffectRejoin
	if pins["kind"] != "rejoin" || decodeStrict(pins["record"].([]byte), &record) != nil {
		return pgconn.CommandTag{}, errs.ErrUnavailable
	}
	tx.appended = append(tx.appended, record)
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

// workflowPDLCAdmissionInput supplies canonical text-file input without invoking external GitHub.
// workflowPDLCAdmissionInput задаёт canonical text-file input без обращения к внешнему GitHub.
func workflowPDLCAdmissionInput(content, message, blob string) []byte {
	return asJSON(map[string]string{"path": "docs/requirements.md", "branch": "feature/journal", "message": message,
		"content_base64": base64.StdEncoding.EncodeToString([]byte(content)), "sha": blob})
}

// workflowPDLCAdmissionRecord binds one fixture intent to its immutable native-shaped proof.
// Its trusted-gateway-shaped bytes are local test data, not external GitHub delivery evidence.
// workflowPDLCAdmissionRecord связывает fixture intent с immutable native-shaped proof.
// Bytes формы trusted-gateway — локальные данные теста, не доказательство внешней доставки GitHub.
func workflowPDLCAdmissionRecord(t *testing.T, content, call, ref, predecessor string) (workflowEffectIntent, workflowEffectNativeProof) {
	t.Helper()
	input := workflowPDLCAdmissionInput(content, "Уточнить документ", strings.Repeat("a", 40))
	key := "file_" + workflowResultSHA([]byte("document\x00docs/requirements.md"))
	desired, err := workflowEffectSemanticDigestForProfile(workflowservice.GitHubPDLCV1, key, "github.repository.content.update", "scope-digest", input)
	if err != nil {
		t.Fatal("create desired file fixture")
	}
	intent := workflowEffectIntent{Schema: "kodex.workflow-effect-intent.v1", ResultKey: key, Operation: "github.repository.content.update", SemanticDigest: workflowPDLCFileEpochDigest(desired, predecessor),
		ConnectionRef: "int_00000000", ConnectionVersion: 7, GrantRef: "grt_00000000", GrantVersion: 2, DefinitionDigest: "definition-digest", ScopeDigest: "scope-digest", ApprovalPolicy: "NONE",
		OriginNodeID: "old-node", OriginRunID: "old-run", OriginRevisionID: "old-revision", OriginLeaseID: "old-lease", OriginGeneration: 1, OriginAttempt: 1, OriginBusiness: 1,
		InvocationRef: ref, EffectKey: "eff_" + ref, InputDigest: workflowResultSHA(input), FileCallKey: call, FilePredecessorRef: predecessor}
	proof := workflowEffectNativeProof{InvocationRef: ref, State: "SUCCEEDED", EffectKey: intent.EffectKey, InputDigest: intent.InputDigest, ReceiptRef: "erc_" + ref, ReceiptDigest: strings.Repeat("c", 64), OriginalKey: call, Input: input, Manifest: []byte(`null`)}
	return intent, proof
}

// TestWorkflowPDLCNativeFileAdmission verifies the composed file path around the canonical
// native reader: known success rejoins, changed bytes create a linked new intent, and uncertainty,
// corrupted proofs, reused keys and failed owner append never become a new external invocation.
// TestWorkflowPDLCNativeFileAdmission проверяет составленный file path с canonical native reader:
// known success повторно читается, иные bytes создают linked intent, а uncertainty, повреждение,
// повтор ключа и отказ owner append не превращаются в новую внешнюю invocation.
func TestWorkflowPDLCNativeFileAdmission(t *testing.T) {
	for _, mode := range []string{"first", "known-rejoin", "metadata-only", "changed-bytes", "revert", "original-call", "key-reuse-changed-input", "pending-exact", "pending-fresh", "missing-receipt", "bad-receipt", "corrupt-input", "changed-grant", "read-outage", "append-outage", "rejoin-limit"} {
		t.Run(mode, func(t *testing.T) {
			old, proof := workflowPDLCAdmissionRecord(t, "Первая версия\n", "call-0001", "inv_first000", "")
			input, call := proof.Input, "call-0002"
			current := old
			current.OriginNodeID, current.OriginRevisionID, current.OriginLeaseID, current.OriginAttempt = "new-node", "new-revision", "new-lease", 2
			current.FileCallKey, current.FilePredecessorRef, current.InvocationRef, current.EffectKey, current.InputDigest = "", "", "", "", ""
			intents := []workflowEffectIntent{old}
			tx := &workflowPDLCFileAdmissionTx{proofs: map[string]workflowEffectNativeProof{old.InvocationRef: proof}}
			count := 0
			switch mode {
			case "first":
				intents = nil
			case "metadata-only":
				input = workflowPDLCAdmissionInput("Первая версия\n", "Новое описание прежнего изменения", strings.Repeat("b", 40))
			case "changed-bytes":
				input = workflowPDLCAdmissionInput("Вторая версия\n", "Уточнить документ", strings.Repeat("b", 40))
			case "revert", "original-call":
				second, secondProof := workflowPDLCAdmissionRecord(t, "Вторая версия\n", "call-0003", "inv_second00", old.InvocationRef)
				intents = append(intents, second)
				tx.proofs[second.InvocationRef] = secondProof
				if mode == "original-call" {
					current, call = old, old.FileCallKey
				}
			case "key-reuse-changed-input":
				current, call = old, old.FileCallKey
				input = workflowPDLCAdmissionInput("Первая версия\n", "Другое сообщение под прежним ключом", strings.Repeat("a", 40))
			case "pending-exact", "pending-fresh":
				proof.State, proof.ReceiptRef, proof.ReceiptDigest = "RUNNING", "", ""
				tx.proofs[old.InvocationRef] = proof
				if mode == "pending-exact" {
					current, call = old, old.FileCallKey
				}
			case "missing-receipt":
				proof.ReceiptRef = ""
				tx.proofs[old.InvocationRef] = proof
			case "bad-receipt":
				proof.ReceiptDigest = "invalid"
				tx.proofs[old.InvocationRef] = proof
			case "corrupt-input":
				proof.Input = workflowPDLCAdmissionInput("Чужие bytes\n", "Уточнить документ", strings.Repeat("a", 40))
				tx.proofs[old.InvocationRef] = proof
			case "changed-grant":
				current.GrantVersion++
			case "read-outage":
				tx.readError = errs.ErrUnavailable
			case "append-outage":
				tx.appendError = errs.ErrUnavailable
			case "rejoin-limit":
				count = workflowPDLCEffectLimit
			}
			desired, err := workflowEffectSemanticDigestForProfile(workflowservice.GitHubPDLCV1, current.ResultKey, current.Operation, current.ScopeDigest, input)
			if err != nil {
				t.Fatal("normalize current native input")
			}
			current.SemanticDigest = desired
			admission := &workflowEffectAdmission{RootID: "root", AnchorID: "anchor", NodeID: current.OriginNodeID}
			actual, reply, err := (&Repository{}).admitWorkflowPDLCFileTx(t.Context(), tx, scope{organizationID: "owner"}, admission, current, intents, nil, count, call, input, "session", "turn")
			switch mode {
			case "first", "changed-bytes", "revert":
				if err != nil || actual == nil || reply != nil || len(tx.appended) != 0 || actual.Intent.FileCallKey != call {
					t.Fatal("new deliberate file intent not admitted", mode, err)
				}
				predecessor := ""
				if mode == "changed-bytes" {
					predecessor = old.InvocationRef
				} else if mode == "revert" {
					predecessor = "inv_second00"
				}
				if actual.Intent.FilePredecessorRef != predecessor || actual.Intent.SemanticDigest != workflowPDLCFileEpochDigest(desired, predecessor) {
					t.Fatal("file revision lost server-assigned predecessor")
				}
			case "known-rejoin", "metadata-only", "original-call", "pending-exact":
				if err != nil || actual != nil || stringMap(reply, "invocationRef") != old.InvocationRef {
					t.Fatal("known native call allocated another invocation", mode, err)
				}
				if mode == "pending-exact" {
					if reply["workflowRejoined"] != false || len(tx.appended) != 0 || stringMap(reply, "state") != "RUNNING" {
						t.Fatal("pending RPC poll became known external success")
					}
				} else if reply["workflowRejoined"] != true || stringMap(reply, "receiptRef") != proof.ReceiptRef || mode != "original-call" && len(tx.appended) != 1 {
					t.Fatal("known success lost exact receipt or fresh rejoin")
				}
			case "read-outage", "append-outage":
				if !errors.Is(err, errs.ErrUnavailable) || actual != nil || reply != nil {
					t.Fatal("owner outage became accepted file action")
				}
			default:
				if !errors.Is(err, errs.ErrConflict) || actual != nil || reply != nil || len(tx.appended) != 0 {
					t.Fatal("unproved file effect admitted", mode, err)
				}
			}
		})
	}
}
