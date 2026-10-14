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

//go:embed sql/workflow_catalog__publication.sql
var queryWorkflowCatalogPublication string

//go:embed sql/workflow_catalog__active_runs.sql
var queryWorkflowCatalogActiveRuns string

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
	if item.Published == nil || !workflowSpecDigestPattern.MatchString(digest) {
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
		if rows.Scan(&row.RunRef, &row.WorkflowRef, &row.PublishedRef, &row.SpecDigest, &row.PublishedVersion, &row.RunVersion, &row.Title, &row.State, &row.CreatedAt) != nil || !workflowLaunchRefPattern.MatchString(row.RunRef) || row.WorkflowRef != pins.WorkflowRef || !workflowLaunchRefPattern.MatchString(row.PublishedRef) || !workflowSpecDigestPattern.MatchString(row.SpecDigest) || row.PublishedVersion < 1 || row.RunVersion < 1 || !contains([]string{"QUEUED", "RUNNING", "WAITING_HUMAN", "CANCELLING"}, row.State) {
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
