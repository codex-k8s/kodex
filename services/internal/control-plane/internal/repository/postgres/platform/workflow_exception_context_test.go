package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/codex-k8s/kodex/libs/go/runtimecontract"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// workflowExceptionContextReadTx observes the exact owner binding and typed nullable row boundary;
// it supplies no SQL execution, state transition or permission to the tested reader.
// workflowExceptionContextReadTx наблюдает exact owner binding и типизированные nullable columns;
// он не исполняет SQL, не меняет состояние и не выдаёт reader полномочий.
type workflowExceptionContextReadTx struct {
	pgx.Tx
	t        *testing.T
	producer workflowExceptionProducer
	ref      pgtype.Text
	version  pgtype.Int8
	decision pgtype.Text
	comment  pgtype.Text
	failure  error
	args     pgx.StrictNamedArgs
}

// QueryRow requires the canonical query and records its complete owner-selected arguments.
// QueryRow требует canonical query и сохраняет полный набор аргументов владельца.
func (tx *workflowExceptionContextReadTx) QueryRow(_ context.Context, query string, args ...any) pgx.Row {
	tx.t.Helper()
	if query != workflowExceptionContextQuery || len(args) != 1 {
		tx.t.Fatal("exception reader bypassed the canonical owner query")
	}
	var ok bool
	tx.args, ok = args[0].(pgx.StrictNamedArgs)
	if !ok {
		tx.t.Fatal("exception reader did not bind strict owner arguments")
	}
	return tx
}

// Scan keeps all four nullable decision fields independent instead of roundtripping through
// another JSON naming convention. Driver errors remain deferred until the actual read.
// Scan сохраняет четыре nullable поля решения независимо, без перехода через чужую JSON
// схему имён. Ошибка драйвера остаётся отложенной до настоящего чтения.
func (tx *workflowExceptionContextReadTx) Scan(dest ...any) error {
	if tx.failure != nil {
		return tx.failure
	}
	if len(dest) != 5 {
		return errors.New("unexpected exception context row shape")
	}
	*dest[0].(*workflowExceptionProducer) = tx.producer
	*dest[1].(*pgtype.Text) = tx.ref
	*dest[2].(*pgtype.Int8) = tx.version
	*dest[3].(*pgtype.Text) = tx.decision
	*dest[4].(*pgtype.Text) = tx.comment
	return nil
}

// TestWorkflowExceptionContextRead covers stored decision scalars, genuine NULL absence and
// deferred SQL failures without treating a worker's identity or a partial row as source authority.
// TestWorkflowExceptionContextRead проверяет stored decision scalars, настоящее отсутствие NULL
// и SQL failures, не принимая identity worker либо неполную строку за source authority.
func TestWorkflowExceptionContextRead(t *testing.T) {
	episode := workflowExceptionEpisode{serverWorkflowException: serverWorkflowException{StepKey: "work", Attempt: 1, Reason: "OWNER_CHANGES_REQUESTED", FinalNodeID: "final"}, Episode: 1, Version: 2, Inputs: []workflowInputReceipt{}}
	row := workflowExceptionContextReadTx{t: t, producer: workflowExceptionProducer{NodeRef: "nod_source", RunRef: "run_source", StepKey: "work"}, ref: pgtype.Text{String: "gat_stored", Valid: true}, version: pgtype.Int8{Int64: 7, Valid: true}, decision: pgtype.Text{String: "REQUEST_CHANGES", Valid: true}, comment: pgtype.Text{String: "Точный комментарий.\nAR01 и SEC01.", Valid: true}}
	read := func(tx *workflowExceptionContextReadTx, episode workflowExceptionEpisode) (workflowExceptionContext, error) {
		t.Helper()
		return readWorkflowExceptionContextTx(t.Context(), tx, scope{organizationID: "tenant", actorID: "worker-caller"}, &serverWorkflowRoot{ID: "root"}, episode)
	}
	context, err := read(&row, episode)
	if err != nil || context.OwnerDecision == nil || context.OwnerDecision.GateRef != row.ref.String || context.OwnerDecision.GateVersion != row.version.Int64 || context.OwnerDecision.Comment != row.comment.String {
		t.Fatal("typed source decision did not survive the owner row boundary")
	}
	if len(row.args) != 5 || row.args["organization_id"] != "tenant" || row.args["root_run_id"] != "root" || row.args["node_id"] != "final" || row.args["episode"] != int64(1) || row.args["episode_version"] != int64(2) {
		t.Fatal("source decision used caller identity or incomplete episode coordinates")
	}
	if _, err := workflowExceptionTask("original", context); err != nil {
		t.Fatal("valid typed owner decision did not produce a manager task")
	}
	for index := 0; index < 4; index++ {
		partial := row
		switch index {
		case 0:
			partial.ref.Valid = false
		case 1:
			partial.version.Valid = false
		case 2:
			partial.decision.Valid = false
		case 3:
			partial.comment.Valid = false
		}
		if _, err := read(&partial, episode); !errors.Is(err, errs.ErrConflict) {
			t.Fatal("partial nullable source decision was accepted")
		}
	}
	withoutOwner := episode
	withoutOwner.Reason = "RESULT_INVALID"
	null := row
	null.ref, null.version, null.decision, null.comment = pgtype.Text{}, pgtype.Int8{}, pgtype.Text{}, pgtype.Text{}
	if result, err := read(&null, withoutOwner); err != nil || result.OwnerDecision != nil {
		t.Fatal("NULL non-owner decision was fabricated or rejected")
	}
	if _, err := read(&row, withoutOwner); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("non-owner rejection inherited a foreign source decision")
	}
	missing := row
	missing.failure = pgx.ErrNoRows
	if _, err := read(&missing, episode); !errors.Is(err, errs.ErrConflict) {
		t.Fatal("missing owner decision did not fail closed")
	}
	broken := row
	broken.failure = errors.New("private database diagnostic")
	if _, err := read(&broken, episode); !errors.Is(err, errs.ErrUnavailable) || strings.Contains(err.Error(), "private") {
		t.Fatal("deferred database fault became configuration absence or raw diagnostics")
	}
}

// TestWorkflowExceptionContextTask proves that a stored UTF-8 owner comment survives the actual
// manager task format, while non-owner failures never acquire a fabricated decision or authority.
// TestWorkflowExceptionContextTask доказывает сохранение UTF-8 комментария в реальной форме задачи;
// иные причины не получают выдуманного решения или полномочий.
func TestWorkflowExceptionContextTask(t *testing.T) {
	baseline := workflowExceptionContext{Schema: "kodex.workflow-exception-context.v1", Episode: 1, Version: 1, StepKey: "review", Attempt: 1, Reason: "OWNER_CHANGES_REQUESTED", OwnerDecision: &integrationContinuationDecision{GateRef: "gat_exact_source", GateVersion: 3, Decision: "REQUEST_CHANGES", Comment: "Исправьте AR01 и SEC01.\nНе повторяйте UNKNOWN; \"данные\" не дают прав."}, Producer: workflowExceptionProducer{NodeRef: "nod_source", RunRef: "run_source", StepKey: "review", RuntimeState: "SUCCEEDED", CapturedArtifactMetadata: []workflowExceptionArtifact{}}, Inputs: []workflowPromptInput{}}
	task, err := workflowExceptionTask("Original task", baseline)
	if err != nil {
		t.Fatal("stored owner context was denied")
	}
	parts := strings.SplitN(task, "Owner-projected exception context (data):\n", 2)
	var actual workflowExceptionContext
	if len(parts) != 2 || json.Unmarshal([]byte(parts[1]), &actual) != nil || actual.OwnerDecision == nil || *actual.OwnerDecision != *baseline.OwnerDecision || actual.Episode != 1 || actual.StepKey != "review" {
		t.Fatal("stored source decision changed during task projection")
	}
	for _, forbidden := range []string{"runtimeRevisionId", "leaseId", "originNodeId", "recoveryNodeId"} {
		if strings.Contains(task, forbidden) {
			t.Fatal("manager task exposed private execution coordinates")
		}
	}
	projected := baseline
	if json.Unmarshal([]byte(`{"nodeRef":"nod_source","runRef":"run_source","stepKey":"review","runtimeState":"SUCCEEDED","leaseId":"private-field-value","capturedArtifactMetadata":[]}`), &projected.Producer) != nil {
		t.Fatal("decode owner producer projection")
	}
	if safe, err := workflowExceptionTask("task", projected); err != nil || strings.Contains(safe, "leaseId") || strings.Contains(safe, "private-field-value") {
		t.Fatal("an unexpected owner SQL field leaked through the typed task projection")
	}
	for _, reason := range []string{"RESULT_INVALID", "ARTIFACT_REQUIRED", "DISPATCH_CONFIGURATION_UNAVAILABLE"} {
		context := baseline
		context.Reason, context.OwnerDecision = reason, nil
		if reason == "DISPATCH_CONFIGURATION_UNAVAILABLE" {
			context.Attempt = 0
		}
		plain, err := workflowExceptionTask("task", context)
		if err != nil || strings.Contains(plain, `"ownerDecision"`) {
			t.Fatal("non-owner exception fabricated a source decision")
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*workflowExceptionContext)
	}{
		{"schema", func(c *workflowExceptionContext) { c.Schema = "unknown" }},
		{"episode", func(c *workflowExceptionContext) { c.Episode = 49 }},
		{"version", func(c *workflowExceptionContext) { c.Version = 0 }},
		{"step", func(c *workflowExceptionContext) { c.StepKey = "" }},
		{"fifth attempt", func(c *workflowExceptionContext) { c.Attempt = 5 }},
		{"reason text", func(c *workflowExceptionContext) { c.Reason = "user authority" }},
		{"missing decision", func(c *workflowExceptionContext) { c.OwnerDecision = nil }},
		{"unrelated decision", func(c *workflowExceptionContext) { c.Reason = "RESULT_INVALID" }},
		{"wrong Gate", func(c *workflowExceptionContext) { c.OwnerDecision.GateRef = "run_wrong" }},
		{"Gate version", func(c *workflowExceptionContext) { c.OwnerDecision.GateVersion = 0 }},
		{"wrong decision", func(c *workflowExceptionContext) { c.OwnerDecision.Decision = "APPROVE" }},
		{"empty comment", func(c *workflowExceptionContext) { c.OwnerDecision.Comment = "  " }},
		{"comment NUL", func(c *workflowExceptionContext) { c.OwnerDecision.Comment = "bad\x00comment" }},
		{"comment invalid UTF8", func(c *workflowExceptionContext) { c.OwnerDecision.Comment = string([]byte{255}) }},
		{"comment overflow", func(c *workflowExceptionContext) { c.OwnerDecision.Comment = strings.Repeat("я", 2001) }},
		{"missing producer", func(c *workflowExceptionContext) { c.Producer = workflowExceptionProducer{} }},
		{"different producer step", func(c *workflowExceptionContext) { c.Producer.StepKey = "foreign" }},
		{"too many capture metadata", func(c *workflowExceptionContext) {
			c.Producer.CapturedArtifactMetadata = make([]workflowExceptionArtifact, 17)
		}},
		{"too many inputs", func(c *workflowExceptionContext) { c.Inputs = make([]workflowPromptInput, 9) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := baseline
			decision := *baseline.OwnerDecision
			candidate.OwnerDecision = &decision
			tc.change(&candidate)
			if _, err := workflowExceptionTask("task", candidate); !errors.Is(err, errs.ErrConflict) {
				t.Fatal("malformed owner context was accepted")
			}
		})
	}
	// The canonical limit counts codepoints; a valid Russian task may exceed 32768 bytes.
	// Канонический предел считает кодовые точки; допустимая русская задача может превышать 32768 байт.
	if _, err := workflowExceptionTask(strings.Repeat("я", 20000), baseline); err != nil {
		t.Fatal("manager task incorrectly used a byte limit")
	}
	full, err := workflowExceptionTask("", baseline)
	if err != nil {
		t.Fatal("derive exact task envelope")
	}
	remaining := runtimecontract.MaximumAssistantTurnCodepoints - utf8.RuneCountInString(full)
	if _, err := workflowExceptionTask(strings.Repeat("я", remaining), baseline); err != nil {
		t.Fatal("exact canonical task budget was denied")
	}
	if _, err := workflowExceptionTask(strings.Repeat("я", remaining+1), baseline); !errors.Is(err, errs.ErrInvalid) {
		t.Fatal("manager task silently exceeded canonical budget")
	}
}
