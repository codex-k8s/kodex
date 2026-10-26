package platform

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"

	"github.com/codex-k8s/kodex/libs/go/repositorybundle"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/errs"
	workflowservice "github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/service/workflow"
	"github.com/codex-k8s/kodex/services/internal/control-plane/internal/domain/types/entity"
	"github.com/jackc/pgx/v5"
)

// workflowEffectLimit caps immutable logical slots and per-business-attempt rejoins, independently of technical turns.
// workflowEffectLimit ограничивает logical slots и rejoin одной business attempt независимо от technical turns.
const workflowEffectLimit = 16

// queryWorkflowEffectRoot locks the existing SERVER root before native node/connection/grant locks; legacy roots have no row.
// queryWorkflowEffectRoot блокирует SERVER root до native node/connection/grant; legacy roots не выбираются.
//
//go:embed sql/workflow_effect__root.sql
var queryWorkflowEffectRoot string

// queryWorkflowEffectOrigin proves the published slot, live immutable execution and logical anchor without trusting caller coordinates.
// queryWorkflowEffectOrigin доказывает published slot, live execution и anchor без доверия caller coordinates.
//
//go:embed sql/workflow_effect__origin.sql
var queryWorkflowEffectOrigin string

// queryWorkflowEffectAppend appends one owner-proven record in the same native transaction.
// queryWorkflowEffectAppend добавляет одну owner-proven запись в той же native transaction.
//
//go:embed sql/workflow_effect__append.sql
var queryWorkflowEffectAppend string

// queryWorkflowEffectReceipt resolves only the original native ledger and immutable execution pins, never a latest receipt.
// queryWorkflowEffectReceipt разрешает только исходный native ledger и immutable pins, а не latest receipt.
//
//go:embed sql/workflow_effect__receipt.sql
var queryWorkflowEffectReceipt string

// workflowEffectIntent indexes an existing native invocation; it neither replaces its receipt nor authorizes another write.
// workflowEffectIntent индексирует existing invocation, не заменяя receipt и не разрешая новый WRITE.
type workflowEffectIntent struct {
	Schema            string `json:"schema"`
	ResultKey         string `json:"resultKey"`
	Operation         string `json:"operation"`
	SemanticDigest    string `json:"semanticDigest"`
	ConnectionRef     string `json:"connectionRef"`
	ConnectionVersion int64  `json:"connectionVersion"`
	GrantRef          string `json:"grantRef"`
	GrantVersion      int64  `json:"grantVersion"`
	DefinitionDigest  string `json:"definitionDigest"`
	ScopeDigest       string `json:"scopeDigest"`
	ApprovalPolicy    string `json:"approvalPolicy"`
	OriginNodeID      string `json:"originNodeId"`
	OriginRunID       string `json:"originRunId"`
	OriginRevisionID  string `json:"originRevisionId"`
	OriginLeaseID     string `json:"originLeaseId"`
	OriginGeneration  int64  `json:"originGeneration"`
	OriginAttempt     int64  `json:"originAttempt"`
	OriginBusiness    int64  `json:"originBusinessAttempt"`
	InvocationRef     string `json:"invocationRef"`
	EffectKey         string `json:"effectKey"`
	InputDigest       string `json:"inputDigest"`
}

// workflowEffectRejoin binds a read-only previous success to one fresh immutable execution; no worker grant is issued.
// workflowEffectRejoin связывает read-only прежний успех со свежим execution, не выдавая worker grant.
type workflowEffectRejoin struct {
	Schema            string `json:"schema"`
	ResultKey         string `json:"resultKey"`
	Operation         string `json:"operation"`
	SemanticDigest    string `json:"semanticDigest"`
	InvocationRef     string `json:"invocationRef"`
	ReceiptRef        string `json:"receiptRef"`
	ReceiptDigest     string `json:"receiptDigest"`
	RuntimeRevisionID string `json:"runtimeRevisionId"`
	LeaseID           string `json:"leaseId"`
	Generation        int64  `json:"generation"`
	RuntimeAttempt    int64  `json:"runtimeAttempt"`
	BusinessAttempt   int64  `json:"businessAttempt"`
}

// workflowEffectAdmission carries only owner-resolved coordinates while a root lock serializes admission and retry.
// workflowEffectAdmission несёт owner-resolved coordinates под root lock, сериализующим admission и retry.
type workflowEffectAdmission struct {
	RootID, AnchorID, NodeID string
	Intent                   workflowEffectIntent
}

// workflowEffectSemanticDigest normalizes validated JSON object ordering without discarding scalar values or numeric precision.
// workflowEffectSemanticDigest нормализует порядок ключей проверенного JSON, сохраняя scalar values и точность чисел.
func workflowEffectSemanticDigest(key, operation, scopeDigest string, raw []byte) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var canonical any
	if !json.Valid(raw) || decoder.Decode(&canonical) != nil {
		return "", errs.ErrConflict
	}
	return workflowResultSHA(asJSON([]any{"kodex.workflow-effect-semantic.v1", key, operation, scopeDigest, canonical})), nil
}

// lockWorkflowEffectRootTx preserves legacy behavior and acquires the SERVER owner lock before native locks.
// lockWorkflowEffectRootTx сохраняет legacy behavior и берёт SERVER owner lock до native locks.
func lockWorkflowEffectRootTx(ctx context.Context, tx pgx.Tx, current scope, input map[string]string) (string, error) {
	var rootID string
	err := tx.QueryRow(ctx, queryWorkflowEffectRoot, pgx.StrictNamedArgs{"organization_id": current.organizationID, "run_ref": input["run_ref"], "node_ref": input["node_ref"], "lease_ref": input["lease_ref"]}).Scan(&rootID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", serializableTransactionError(err, errs.ErrUnavailable)
	}
	return rootID, nil
}

// workflowEffectSlot derives one published result key and target; unsupported or ambiguous writes never allocate an effect.
// workflowEffectSlot выводит один published result key и target; unsupported/ambiguous WRITE не создаёт effect.
func workflowEffectSlot(step entity.WorkflowStep, dependencies []workflowResultDependency, operation string, resourceScope, canonicalInput []byte) (string, error) {
	var resource map[string]string
	if json.Unmarshal(resourceScope, &resource) != nil || !workflowservice.ValidRequirements(step.ResultRequirements, step.DependsOn) {
		return "", errs.ErrConflict
	}
	repository := resource["owner"] + "/" + resource["repository"]
	key := ""
	for _, requirement := range step.ResultRequirements {
		if requirement.Repository != repository {
			continue
		}
		allowed := requirement.Kind == "PULL_REQUEST" && contains([]string{repositoryBundleOperation, "github.pull_request.create"}, operation)
		if requirement.Kind == "INTEGRATION_RECEIPT" && operation == "github.pull_request.merge" && requirement.Operation == operation {
			subject, found := workflowResultSubject(dependencies, requirement.SubjectStepKey)
			var target struct {
				Number int64  `json:"pull_request_number"`
				SHA    string `json:"sha"`
			}
			allowed = found && subject.Repository == requirement.Repository && json.Unmarshal(canonicalInput, &target) == nil && target.Number == subject.Number && target.SHA == subject.HeadSHA
		}
		if allowed {
			if key != "" {
				return "", errs.ErrConflict
			}
			key = requirement.Key
		}
	}
	if key == "" {
		return "", errs.ErrForbidden
	}
	return key, nil
}

// admitWorkflowEffectTx admits only published SERVER writes and rejoins a previous known success without creating any native work.
// admitWorkflowEffectTx допускает только published SERVER WRITE и повторно читает прежний успех без новой native работы.
func admitWorkflowEffectTx(ctx context.Context, tx pgx.Tx, current scope, rootID string, input map[string]string, operation, grantRef string, grantVersion int64, definitionDigest, scopeDigest, approvalPolicy string, resourceScope, canonicalInput, semanticInput []byte) (*workflowEffectAdmission, map[string]any, error) {
	if rootID == "" {
		return nil, nil, nil
	}
	admission := &workflowEffectAdmission{RootID: rootID}
	intent := workflowEffectIntent{Schema: "kodex.workflow-effect-intent.v1", Operation: operation, ConnectionRef: input["connection_ref"], GrantRef: grantRef, GrantVersion: grantVersion, DefinitionDigest: definitionDigest, ScopeDigest: scopeDigest, ApprovalPolicy: approvalPolicy}
	var rawStep, rawInputs, rawIntents, rawRejoins []byte
	var rejoinCount int
	err := tx.QueryRow(ctx, queryWorkflowEffectOrigin, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": rootID, "run_ref": input["run_ref"], "node_ref": input["node_ref"], "connection_ref": intent.ConnectionRef, "grant_ref": grantRef, "grant_version": grantVersion, "operation": operation, "definition_digest": definitionDigest, "approval_policy": approvalPolicy}).Scan(
		&admission.AnchorID, &admission.NodeID, &intent.OriginRunID, &intent.OriginRevisionID, &intent.OriginLeaseID, &intent.OriginGeneration, &intent.OriginAttempt, &intent.OriginBusiness, &rawStep, &rawInputs, &rawIntents, &rawRejoins, &intent.ConnectionVersion, &rejoinCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, errs.ErrForbidden
	}
	if err != nil {
		return nil, nil, serializableTransactionError(err, errs.ErrUnavailable)
	}
	intent.OriginNodeID = admission.NodeID
	var step entity.WorkflowStep
	var dependencies []workflowResultDependency
	var intents []workflowEffectIntent
	var rejoins []workflowEffectRejoin
	if decodeStrict(rawStep, &step) != nil || decodeStrict(rawInputs, &dependencies) != nil || decodeStrict(rawIntents, &intents) != nil || decodeStrict(rawRejoins, &rejoins) != nil || len(intents) > workflowEffectLimit || rejoinCount > workflowEffectLimit {
		return nil, nil, errs.ErrConflict
	}
	intent.ResultKey, err = workflowEffectSlot(step, dependencies, operation, resourceScope, canonicalInput)
	if err != nil {
		return nil, nil, err
	}
	if semanticInput == nil {
		semanticInput = canonicalInput
	}
	intent.SemanticDigest, err = workflowEffectSemanticDigest(intent.ResultKey, operation, scopeDigest, semanticInput)
	if err != nil {
		return nil, nil, err
	}
	admission.Intent = intent
	for _, old := range intents {
		if old.ResultKey != intent.ResultKey || old.Operation != operation {
			continue
		}
		if old.Schema != intent.Schema || old.SemanticDigest != intent.SemanticDigest || old.ConnectionRef != intent.ConnectionRef || old.ConnectionVersion != intent.ConnectionVersion || old.GrantRef != grantRef || old.GrantVersion != grantVersion || old.DefinitionDigest != definitionDigest || old.ScopeDigest != scopeDigest || old.ApprovalPolicy != approvalPolicy {
			return nil, nil, errs.ErrConflict
		}
		var invocationRef, state, effectKey, inputDigest, receiptRef, receiptDigest, publicationRef, originalKey, gateRef string
		var oldInput, oldManifest []byte
		var sourcePolicy string
		err = tx.QueryRow(ctx, queryWorkflowEffectReceipt, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": rootID, "anchor_id": admission.AnchorID, "origin_node_id": old.OriginNodeID, "origin_revision_id": old.OriginRevisionID, "origin_lease_id": old.OriginLeaseID, "invocation_ref": old.InvocationRef, "operation": operation, "connection_ref": old.ConnectionRef, "grant_ref": old.GrantRef, "grant_version": old.GrantVersion, "definition_digest": old.DefinitionDigest, "scope_digest": old.ScopeDigest, "approval_policy": old.ApprovalPolicy}).Scan(&invocationRef, &state, &effectKey, &inputDigest, &receiptRef, &receiptDigest, &publicationRef, &originalKey, &gateRef, &oldInput, &oldManifest, &sourcePolicy)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil, errs.ErrConflict
		}
		if err != nil {
			return nil, nil, serializableTransactionError(err, errs.ErrUnavailable)
		}
		if effectKey != old.EffectKey || inputDigest != old.InputDigest {
			return nil, nil, errs.ErrConflict
		}
		oldSemantic := oldInput
		if operation == repositoryBundleOperation {
			var original struct {
				BaseCommitSHA string `json:"base_commit_sha"`
			}
			if json.Unmarshal(oldInput, &original) != nil || sourcePolicy != repositoryBundleContentPolicy || bytes.Equal(oldManifest, []byte("null")) {
				return nil, nil, errs.ErrConflict
			}
			oldSemantic = asJSON([]any{"kodex.workflow-bundle-content.v1", sourcePolicy, original.BaseCommitSHA, json.RawMessage(oldManifest)})
		}
		provenDigest, digestErr := workflowEffectSemanticDigest(intent.ResultKey, operation, scopeDigest, oldSemantic)
		if digestErr != nil || provenDigest != old.SemanticDigest {
			return nil, nil, errs.ErrConflict
		}
		// Only an exact RPC replay may poll its original pending invocation; a fresh physical call cannot resume it.
		// Только exact RPC replay может читать свой pending invocation; fresh physical call его не возобновляет.
		exactReplay := old.OriginNodeID == intent.OriginNodeID && old.OriginRevisionID == intent.OriginRevisionID && originalKey == input["idempotency_key"]
		if state != "SUCCEEDED" && !exactReplay || state == "SUCCEEDED" && (receiptRef == "" || !repositorybundle.ValidDigest(receiptDigest) || operation == repositoryBundleOperation && publicationRef == "") {
			return nil, nil, errs.ErrConflict
		}
		if state == "SUCCEEDED" && old.OriginNodeID != intent.OriginNodeID {
			rejoin := workflowEffectRejoin{Schema: "kodex.workflow-effect-rejoin.v1", ResultKey: intent.ResultKey, Operation: operation, SemanticDigest: intent.SemanticDigest, InvocationRef: invocationRef, ReceiptRef: receiptRef, ReceiptDigest: receiptDigest, RuntimeRevisionID: intent.OriginRevisionID, LeaseID: intent.OriginLeaseID, Generation: intent.OriginGeneration, RuntimeAttempt: intent.OriginAttempt, BusinessAttempt: intent.OriginBusiness}
			found := false
			for _, existing := range rejoins {
				if existing.ResultKey == rejoin.ResultKey && existing.Operation == rejoin.Operation {
					if existing != rejoin {
						return nil, nil, errs.ErrConflict
					}
					found = true
				}
			}
			if !found {
				if rejoinCount >= workflowEffectLimit {
					return nil, nil, errs.ErrConflict
				}
				if err := appendWorkflowEffectTx(ctx, tx, current, rootID, admission.NodeID, "rejoin", rejoin); err != nil {
					return nil, nil, err
				}
			}
		}
		return nil, map[string]any{"invocationRef": invocationRef, "grantRef": grantRef, "operation": operation, "state": state, "gateRef": gateRef, "workflowRejoined": state == "SUCCEEDED", "publicationRef": publicationRef, "receiptRef": receiptRef}, nil
	}
	if len(intents) >= workflowEffectLimit {
		return nil, nil, errs.ErrConflict
	}
	return admission, nil, nil
}

// appendWorkflowEffectTx requires one actual owner update; an unexpected miss rolls back native admission.
// appendWorkflowEffectTx требует одно owner update; неожиданный miss откатывает native admission.
func appendWorkflowEffectTx(ctx context.Context, tx pgx.Tx, current scope, rootID, nodeID, kind string, record any) error {
	tag, err := tx.Exec(ctx, queryWorkflowEffectAppend, pgx.StrictNamedArgs{"organization_id": current.organizationID, "root_run_id": rootID, "node_id": nodeID, "kind": kind, "record": asJSON(record)})
	if err != nil {
		return serializableTransactionError(err, errs.ErrUnavailable)
	}
	if tag.RowsAffected() != 1 {
		return errs.ErrConflict
	}
	return nil
}
