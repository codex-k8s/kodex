package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/codex-k8s/kodex/libs/go/integrationpackage"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// gateCompletionRow supplies synthetic owner metadata through the real projection scanner.
// gateCompletionRow передаёт синтетические owner metadata настоящему scanner проекции.
type gateCompletionRow func(...any) error

// Scan delegates without querying a database or interpreting caller-controlled identity.
// Scan делегирует без запроса к БД либо интерпретации identity из пользовательских данных.
func (row gateCompletionRow) Scan(dest ...any) error { return row(dest...) }

// gateCompletionReader models exact owner reads and records attempts to leave the tenant/gate boundary.
// gateCompletionReader моделирует точные owner reads и проверяет сохранение tenant/gate boundary.
type gateCompletionReader struct {
	t              *testing.T
	mode           string
	version        entity.WorkflowVersion
	anchors        []serverWorkflowAnchor
	metadataReads  int
	pending        bool
	otherOpen      bool
	producerBlock  bool
	producer       string
	exception      string
	legacyComplete bool
	metadataError  error
	pendingError   error
	anchorsError   error
}

// QueryRow returns only known projection rows; the proposed read-only metadata query stays owner-scoped.
// QueryRow возвращает только известные строки проекции; новый read-only metadata query сохраняет owner scope.
func (reader *gateCompletionReader) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	reader.t.Helper()
	return gateCompletionRow(func(dest ...any) error {
		switch {
		case query == queryOwnerGateScope:
			*dest[0].(*string), *dest[1].(*string), *dest[2].(*string) = "PROJECT", "org_fixture", "prj_fixture"
		case query == queryOwnerGateIntent:
			if len(dest) != 20 {
				reader.t.Fatal("existing intent projection shape changed")
			}
			for _, value := range dest {
				switch item := value.(type) {
				case *string:
					*item = ""
				case *bool:
					*item = false
				case *[]byte:
					*item = nil
				case *[]string:
					*item = nil
				}
			}
			*dest[2].(*bool) = reader.mode == "delivery"
			*dest[3].(*bool) = reader.legacyComplete
			if reader.mode == "integration" {
				input := []byte(`{"action":"CREATE","value":"fixture"}`)
				sum := sha256.Sum256(input)
				*dest[1].(*bool) = true
				*dest[4].(*string), *dest[5].(*string), *dest[6].(*string) = "int_fixture", "Fixture", "synthetic"
				*dest[7].(*string), *dest[8].(*string), *dest[10].(*string) = "synthetic.journal.write", "synthetic.journal.write", "SYNTHETIC_JOURNAL"
				*dest[11].(*[]byte), *dest[13].(*[]byte), *dest[14].(*string) = []byte(`{"journal":"fixture"}`), input, hex.EncodeToString(sum[:])
				*dest[15].(*string), *dest[16].(*string) = "WRITE", "HUMAN_EACH_EFFECT"
			}
		case query == queryOwnerGateCompletion:
			if len(args) != 1 || !reflect.DeepEqual(args[0], pgx.StrictNamedArgs{"organization_id": "tenant", "gate_ref": "gat_fixture"}) {
				reader.t.Fatal("completion projection lost exact owner pins")
			}
			reader.metadataReads++
			if reader.metadataError != nil {
				return reader.metadataError
			}
			*dest[0].(*string), *dest[1].(*string) = "root", "WAITING_HUMAN"
			*dest[2].(*[]byte) = asJSON(reader.version)
			*dest[3].(*[]byte) = []byte(reader.exception)
			*dest[4].(*string), *dest[5].(*bool), *dest[6].(*bool) = reader.producer, reader.otherOpen, reader.producerBlock
		case query == queryWorkflowDispatchPending:
			if reader.pendingError != nil {
				return reader.pendingError
			}
			*dest[0].(*bool) = reader.pending
		default:
			reader.t.Fatal("unexpected owner projection query")
		}
		return nil
	})
}

// Query serves the canonical logical-anchor reader, rather than inventing a second acceptance formula.
// Query обслуживает канонический reader логических anchors вместо второй формулы приёмки.
func (reader *gateCompletionReader) Query(_ context.Context, query string, _ ...any) (pgx.Rows, error) {
	if query != queryWorkflowDispatchAnchors {
		reader.t.Fatal("unexpected anchor query")
	}
	if reader.anchorsError != nil {
		return nil, reader.anchorsError
	}
	return &gateCompletionRows{anchors: reader.anchors}, nil
}

// gateCompletionRows implements only the bounded row operations used by the production anchor reader.
// gateCompletionRows реализует только bounded операции строк, используемые production reader anchors.
type gateCompletionRows struct {
	pgx.Rows
	anchors []serverWorkflowAnchor
	index   int
}

// Next advances through the fixed synthetic logical-anchor set.
// Next проходит фиксированный синтетический набор логических anchors.
func (rows *gateCompletionRows) Next() bool { rows.index++; return rows.index <= len(rows.anchors) }

// Scan preserves every canonical anchor field, including latest-attempt and gate provenance.
// Scan сохраняет все canonical поля anchor, включая latest attempt и gate provenance.
func (rows *gateCompletionRows) Scan(dest ...any) error {
	a := rows.anchors[rows.index-1]
	*dest[0].(*string), *dest[1].(*string), *dest[2].(*string), *dest[3].(*string) = a.ID, a.Ref, a.Key, a.State
	*dest[4].(*int32), *dest[5].(*[]entity.WorkflowAttemptResult), *dest[6].(*[]workflowInputReceipt) = a.AssignedAttempt, a.Receipts, a.Inputs
	*dest[7].(*int32), *dest[8].(*bool), *dest[9].(*bool) = a.LatestAttempt, a.GateApproved, a.GateBlocked
	return nil
}

// Close has no external resources because this fixture owns only in-memory rows.
// Close не закрывает внешние ресурсы: оснастка владеет только строками в памяти.
func (rows *gateCompletionRows) Close() {}

// Err reports successful exhaustion of the synthetic row source.
// Err сообщает успешное исчерпание синтетического источника строк.
func (rows *gateCompletionRows) Err() error { return nil }

// TestOwnerGateCompletionProjectionUsesSemanticState exercises the actual public gate projection,
// including recovery, unmaterialized successors, pending effects, final acceptance and immutable history.
// TestOwnerGateCompletionProjectionUsesSemanticState проверяет настоящую публичную Gate-проекцию:
// recovery, ещё не назначенные шаги, pending effects, финальную приёмку и неизменную историю.
func TestOwnerGateCompletionProjectionUsesSemanticState(t *testing.T) {
	definitions, err := integrationpackage.LoadShipped()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"recovery", "nonfinal", "pending", "other-open", "producer-block", "wrong-producer", "stale-attempt", "duplicate-producer", "final", "legacy-final", "legacy-active", "integration", "delivery", "closed-history"} {
		t.Run(mode, func(t *testing.T) {
			created, err := assistantWorkflow(assistantServerWorkflowFixture(t))
			if err != nil {
				t.Fatal(err)
			}
			version := *created.Draft
			last := len(version.Steps) - 1
			version.Steps[last].HumanGateAfter, version.Steps[last].GateDecisions = true, []string{"APPROVE", "REJECT", "CANCEL"}
			reader := &gateCompletionReader{t: t, mode: mode, version: version, legacyComplete: true, exception: "null"}
			for i, step := range version.Steps {
				id, producer := "anchor-"+step.Key, "producer-"+step.Key
				reader.anchors = append(reader.anchors, serverWorkflowAnchor{ID: id, Key: step.Key, AssignedAttempt: 1, LatestAttempt: 1,
					GateBlocked: i == last, Receipts: []entity.WorkflowAttemptResult{{Attempt: 1, Accepted: true, Provenance: &entity.WorkflowCompletionProvenance{OriginNodeID: id, NodeID: producer}}}})
				if i == last {
					reader.producer = producer
				}
			}
			wantTerminal := mode == "final" || mode == "legacy-final" || mode == "closed-history"
			switch mode {
			case "recovery":
				reader.exception = `{"reason":"EXECUTION_FAILED"}`
			case "nonfinal":
				reader.anchors[last].Receipts = nil
				reader.anchors[last].LatestAttempt = 0
			case "pending":
				reader.pending = true
			case "other-open":
				reader.otherOpen = true
			case "producer-block":
				reader.producerBlock = true
			case "wrong-producer":
				reader.producer = "unrelated-node"
			case "duplicate-producer":
				reader.anchors[0].Receipts[0].Provenance.NodeID = reader.producer
			case "stale-attempt":
				reader.anchors[last].LatestAttempt = 2
			case "legacy-final":
				reader.version.ExecutionMode = ""
			case "legacy-active":
				reader.version.ExecutionMode, reader.legacyComplete = "MANAGER_DRIVEN", false
			}
			gate := entity.OwnerGate{Ref: "gat_fixture", ProjectRef: "prj_fixture", State: "OPEN", AllowedDecisions: []string{"APPROVE", "REJECT", "CANCEL"}}
			if mode == "closed-history" {
				gate.State, reader.pending = "APPROVED", true
			}
			if err := (&Repository{integrationDefinitions: definitions}).projectGateIntent(t.Context(), reader, scope{organizationID: "tenant"}, &gate, false); err != nil {
				t.Fatal(err)
			}
			if gate.DecisionConsequences[0].TerminalForRun != wantTerminal {
				t.Fatal("APPROVE consequence disagrees with the owner lifecycle")
			}
			if (mode == "closed-history" || mode == "delivery" || mode == "integration") && reader.metadataReads != 0 {
				t.Fatal("historical or external effect projection read hypothetical workflow state")
			}
			if _, err := json.Marshal(gate.DecisionConsequences); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestOwnerGateCompletionReadFailuresStayClosed ensures missing, cancelled and failed reads
// cannot publish a positive consequence; only stable domain errors leave the repository boundary.
// TestOwnerGateCompletionReadFailuresStayClosed проверяет, что missing, cancel и ошибки чтения
// не публикуют положительный прогноз; границу repository покидают только доменные ошибки.
func TestOwnerGateCompletionReadFailuresStayClosed(t *testing.T) {
	for _, mode := range []string{"missing", "cancelled", "metadata", "anchors", "pending"} {
		t.Run(mode, func(t *testing.T) {
			created, err := assistantWorkflow(assistantServerWorkflowFixture(t))
			if err != nil {
				t.Fatal(err)
			}
			reader := &gateCompletionReader{t: t, version: *created.Draft, exception: "null", producer: "producer"}
			for index, step := range reader.version.Steps {
				id, producer := "anchor-"+step.Key, "earlier-producer"
				if index == len(reader.version.Steps)-1 {
					producer = reader.producer
				}
				reader.anchors = append(reader.anchors, serverWorkflowAnchor{ID: id, Key: step.Key, LatestAttempt: 1,
					Receipts: []entity.WorkflowAttemptResult{{Attempt: 1, Accepted: true, Provenance: &entity.WorkflowCompletionProvenance{OriginNodeID: id, NodeID: producer}}}})
			}
			cause := errors.New("private driver detail must not cross the repository boundary")
			switch mode {
			case "missing":
				reader.metadataError = pgx.ErrNoRows
			case "cancelled":
				reader.metadataError = context.Canceled
			case "metadata":
				reader.metadataError = cause
			case "anchors":
				reader.anchorsError = cause
			case "pending":
				reader.pendingError = cause
			}
			terminal, err := openOwnerGateCompletesRun(t.Context(), reader, scope{organizationID: "tenant"}, "gat_fixture", false, false, true)
			if terminal || err != errs.ErrUnavailable {
				t.Fatal("failed owner read did not close the consequence projection")
			}
		})
	}
}
