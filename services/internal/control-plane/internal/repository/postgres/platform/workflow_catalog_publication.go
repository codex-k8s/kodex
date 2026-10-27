package platform

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"

	"github.com/codex-k8s/kodex/libs/go/controlplaneapi"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/query"
	"github.com/jackc/pgx/v5"
)

// queryWorkflowCatalogPublication embeds one exact published workflow with immutable
// revision/digest in an active same-tenant project. Both workflow.view and workflow.launch are
// checked by SQL before returning the canonical workflow projection; caller pins and the live
// execution origin remain separate checks.
// queryWorkflowCatalogPublication встраивает один точный published workflow с immutable
// revision/digest активного project того же tenant. SQL проверяет workflow.view и workflow.launch
// до возврата канонической workflow projection; caller pins и live execution origin остаются
// отдельными проверками.
//
//go:embed sql/workflow_catalog__publication.sql
var queryWorkflowCatalogPublication string

// queryWorkflowCatalogActiveRuns embeds up to 11 visible active canonical workflow-root runs in
// ref order under exact tenant/project/workflow. It projects each run historical published
// revision/digest rather than rebinding to the current publication, and applies run.view before
// cursor/limit. It supplies advisory rows, not cancellation authority.
// queryWorkflowCatalogActiveRuns встраивает до 11 видимых active canonical workflow-root runs в
// порядке ref внутри точных tenant/project/workflow. Проецирует исторические published
// revision/digest каждого run без перепривязки к текущей публикации и применяет run.view до
// cursor/limit. Предоставляет пояснительные строки, а не полномочия отмены.
//
//go:embed sql/workflow_catalog__active_runs.sql
var queryWorkflowCatalogActiveRuns string

// readExecutionWorkflowCatalogTx selects publication pins before active-run pins, reads the
// current eligible workflow and requires exact workflow/project/published-ref/spec-digest/OCC
// matches. Publication mode encodes the full owner projection; active mode binds its ten-row
// cursor to runtime revision plus current publication pins while retaining each active run
// historical revision. Malformed stored rows are Unavailable and stale pins are Conflict. The
// caller owns live-origin authorization, retries and commit; this helper neither launches nor
// changes runs.
// readExecutionWorkflowCatalogTx выбирает publication pins раньше active-run pins, читает текущий
// eligible workflow и требует точные workflow/project/published-ref/spec-digest/OCC matches.
// Publication mode кодирует полную owner projection; active mode связывает cursor до десяти строк
// с runtime revision и текущими publication pins, сохраняя историческую revision каждого active
// run. Повреждённые stored rows дают Unavailable, stale pins — Conflict. Caller владеет
// live-origin authorization, retry и commit; helper не запускает и не меняет runs.
func (repository *Repository) readExecutionWorkflowCatalogTx(ctx context.Context, tx pgx.Tx, actor scope, projectID, projectRef, revisionID, revisionDigest string, input query.ExecutionWorkflowCatalog) (entity.ExecutionWorkflowCatalog, error) {
	empty := entity.ExecutionWorkflowCatalog{}
	var pins query.ExecutionWorkflowReadPins
	if input.Publication != nil {
		pins = input.Publication.Pins
	} else if input.ActiveRuns != nil {
		pins = input.ActiveRuns.Pins
	} else {
		return empty, errs.ErrInvalid
	}
	var digest string
	item, err := scanWorkflow(workflowCatalogRow{row: tx.QueryRow(ctx, queryWorkflowCatalogPublication, pgx.StrictNamedArgs{"organization_id": actor.organizationID, "project_id": projectID, "actor_id": actor.actorID, "workflow_ref": pins.WorkflowRef}), digest: &digest}, true)
	if err != nil {
		return empty, err
	}
	if item.Published == nil || !exactSHA256(digest) {
		return empty, errs.ErrUnavailable
	}
	if item.Ref != pins.WorkflowRef || item.ProjectRef != projectRef || item.Published.Ref != pins.PublishedRef || digest != pins.SpecDigest || item.Version != pins.WorkflowVersion {
		return empty, errs.ErrConflict
	}
	if input.Publication != nil {
		raw, sha, err := executionWorkflowPublication(item, digest)
		if err != nil {
			return empty, err
		}
		return entity.ExecutionWorkflowCatalog{Publication: &entity.ExecutionWorkflowPublication{ConfigurationJSON: raw, ConfigurationSHA256: sha}}, nil
	}
	commitment := sha256.Sum256(asJSON(struct {
		RevisionID, RevisionDigest string
		Pins                       query.ExecutionWorkflowReadPins
	}{revisionID, revisionDigest, pins}))
	filter := query.Filter{ProjectRef: projectRef, ResourceRef: revisionID, ExpectedCatalogDigest: hex.EncodeToString(commitment[:]), State: "ACTIVE", Page: query.Page{Size: 10, Token: input.PageToken}}
	cursor, err := decodeCatalogCursor(actor, "EXECUTION_WORKFLOW_ACTIVE_RUNS", filter)
	if err != nil {
		return empty, err
	}
	rows, err := tx.Query(ctx, queryWorkflowCatalogActiveRuns, pgx.StrictNamedArgs{"organization_id": actor.organizationID, "project_id": projectID, "actor_id": actor.actorID, "workflow_ref": pins.WorkflowRef, "cursor": cursor})
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	page := &entity.ExecutionWorkflowActiveRuns{WorkflowRef: pins.WorkflowRef, PublishedRef: pins.PublishedRef, SpecDigest: pins.SpecDigest, WorkflowVersion: pins.WorkflowVersion, Items: []entity.ExecutionWorkflowActiveRun{}}
	for rows.Next() {
		var row entity.ExecutionWorkflowActiveRun
		if rows.Scan(&row.RunRef, &row.WorkflowRef, &row.PublishedRef, &row.SpecDigest, &row.PublishedVersion, &row.RunVersion, &row.Title, &row.State, &row.CreatedAt) != nil || !workflowLaunchRefPattern.MatchString(row.RunRef) || row.WorkflowRef != pins.WorkflowRef || !workflowLaunchRefPattern.MatchString(row.PublishedRef) || !exactSHA256(row.SpecDigest) || row.PublishedVersion < 1 || row.RunVersion < 1 || !contains([]string{"QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"}, row.State) {
			rows.Close()
			return empty, errs.ErrUnavailable
		}
		page.Items = append(page.Items, row)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return empty, errs.ErrUnavailable
	}
	if len(page.Items) > 10 {
		page.Items = page.Items[:10]
		page.NextPageToken = encodeCatalogCursor(actor, "EXECUTION_WORKFLOW_ACTIVE_RUNS", filter, page.Items[9].RunRef)
	}
	return entity.ExecutionWorkflowCatalog{ActiveRuns: page}, nil
}

// executionWorkflowPublication requires a valid published workflow, decodes its result schema
// through the wire codec with nil mapped to an empty object, then projects every input and step
// including defaults, dependencies and capability/gate choices. It delegates canonical JSON/SHA
// encoding to controlplaneapi and maps failures to Unavailable. Supplied digest/identity must
// already be pinned by the caller; encoding alone does not validate ownership or recompute the
// owner spec digest.
// executionWorkflowPublication требует допустимый published workflow, декодирует result schema
// через wire codec с nil как пустым объектом, затем проецирует каждый input и step вместе с
// defaults, dependencies и capability/gate choices. Делегирует каноническое JSON/SHA-кодирование
// controlplaneapi и отображает ошибки в Unavailable. Supplied digest/identity caller обязан
// закрепить заранее; одно кодирование не проверяет владение и не пересчитывает owner spec digest.
func executionWorkflowPublication(item entity.Workflow, digest string) ([]byte, string, error) {
	source := item.Published
	if source == nil || !validWorkflowVersion(*source) {
		return nil, "", errs.ErrUnavailable
	}
	schemaRaw, err := json.Marshal(source.ResultSchema)
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	if source.ResultSchema == nil {
		schemaRaw = []byte("{}")
	}
	schema, err := controlplaneapi.DecodeWorkflowPublicationSchema(schemaRaw)
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	value := controlplaneapi.WorkflowPublication{Version: 1, WorkflowRef: item.Ref, ProjectRef: item.ProjectRef, PublishedRef: source.Ref, SpecDigest: digest, WorkflowVersion: item.Version, PublishedVersion: source.VersionNumber, Name: source.Name, Purpose: source.Purpose, CoordinatorAgentRef: source.CoordinatorAgentRef, Instructions: source.Instructions, CompletionCriteria: source.CompletionCriteria, Concurrency: source.Concurrency, TimeoutSeconds: source.TimeoutSeconds, GateDecisions: source.GateDecisions, ResultSchema: schema, InputFields: []controlplaneapi.WorkflowPublicationInput{}, Steps: []controlplaneapi.WorkflowPublicationStep{}}
	for _, f := range source.Inputs {
		value.InputFields = append(value.InputFields, controlplaneapi.WorkflowPublicationInput{Key: f.Key, Label: f.Label, ValueType: f.Type, Description: f.Help, DefaultValue: f.DefaultValue, Required: f.Required, Options: f.Options})
	}
	for _, s := range source.Steps {
		value.Steps = append(value.Steps, controlplaneapi.WorkflowPublicationStep{Key: s.Key, Name: s.Name, AgentRef: s.AgentRef, Instructions: s.Instructions, ExpectedResult: s.ExpectedResult, Position: s.Position, ParallelGroup: s.ParallelGroup, TimeoutSeconds: s.TimeoutSeconds, Parallel: s.Parallel, HumanGateAfter: s.HumanGateAfter, DependsOn: s.DependsOn, GateDecisions: s.GateDecisions, RequiredCapabilityKeys: s.RequiredCapabilityKeys})
	}
	raw, sha, err := controlplaneapi.EncodeWorkflowPublication(value)
	if err != nil {
		return nil, "", errs.ErrUnavailable
	}
	return raw, sha, nil
}
