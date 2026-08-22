package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/errs"
	"github.com/codex-k8s/matter-codex/services/internal/control-plane/internal/domain/types/command"
	"github.com/jackc/pgx/v5"
)

func (repository *Repository) changeExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	switch input.Kind {
	case command.ClaimExecution:
		return repository.claimExecution(ctx, tx, scope, input)
	case command.RenewExecution:
		return repository.renewExecution(ctx, tx, scope, input)
	case command.ReportExecutionProgress:
		return repository.reportProgress(ctx, tx, scope, input)
	case command.CompleteExecution:
		return repository.completeExecution(ctx, tx, scope, input)
	case command.DelegateExecution:
		return repository.delegateExecution(ctx, tx, scope, input)
	case command.DeliverCallback:
		return repository.deliverCallback(ctx, tx, scope, input)
	default:
		return commandOutcome{}, errs.ErrInvalid
	}
}

func (repository *Repository) claimExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LeaseInput)
	if !ok || payload.WorkloadInstance == "" || payload.Limit < 1 || payload.Limit > 32 {
		return commandOutcome{}, errs.ErrInvalid
	}
	rows, err := tx.Query(ctx, `SELECT n.id::text,n.ref,n.run_id::text,r.ref,r.root_run_id::text,COALESCE(r.project_id::text,''),COALESCE(p.ref,''),r.session_id::text,s.ref,r.task,a.ref,a.runtime_key,rp.runtime_revision,iv.ref,iv.digest,iv.content,a.capabilities,a.knowledge_artifact_refs,r.input
		FROM control_plane.run_nodes n JOIN control_plane.runs r ON r.id=n.run_id LEFT JOIN control_plane.projects p ON p.id=r.project_id JOIN control_plane.sessions s ON s.id=r.session_id JOIN control_plane.agents a ON a.id=n.agent_id JOIN control_plane.runtime_profiles rp ON rp.stable_key=a.runtime_key JOIN LATERAL(SELECT i.ref,i.digest,i.content FROM control_plane.instruction_versions i WHERE i.agent_id=a.id AND i.state='PUBLISHED' ORDER BY i.version_number DESC LIMIT 1)iv ON true
		WHERE n.organization_id=$1::uuid AND n.type='AGENT_EXECUTION' AND n.state='QUEUED' AND r.state IN('RUNNING','QUEUED')
		AND NOT EXISTS(SELECT 1 FROM control_plane.run_edges e JOIN control_plane.run_nodes dependency ON dependency.id=e.source_node_id WHERE e.target_node_id=n.id AND e.type='WAITING_FOR' AND dependency.state<>'SUCCEEDED')
		ORDER BY CASE WHEN a.system_key='system-assistant' THEN 0 ELSE 1 END,n.created_at FOR UPDATE OF n SKIP LOCKED LIMIT $2`, scope.organizationID, payload.Limit)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	defer rows.Close()
	var items []map[string]any
	var firstProjectID, firstProjectRef, firstRunRef string
	for rows.Next() {
		var nodeID, nodeRef, runID, runRef, rootRunID, projectID, projectRef, sessionID, sessionRef, task, agentRef, runtimeKey, runtimeRevision, instructionRef, instructionDigest, instructions string
		var capabilities, knowledge []string
		var rawInput []byte
		if err := rows.Scan(&nodeID, &nodeRef, &runID, &runRef, &rootRunID, &projectID, &projectRef, &sessionID, &sessionRef, &task, &agentRef, &runtimeKey, &runtimeRevision, &instructionRef, &instructionDigest, &instructions, &capabilities, &knowledge, &rawInput); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		fence, err := newRef("fnc")
		if err != nil {
			return commandOutcome{}, err
		}
		fenceDigest := sha256.Sum256([]byte(fence))
		leaseRef, _ := newRef("lea")
		inputDigest := sha256.Sum256(rawInput)
		var generation int64
		if err := tx.QueryRow(ctx, `SELECT COALESCE(max(generation),0)+1 FROM control_plane.runtime_leases WHERE node_id=$1::uuid`, nodeID).Scan(&generation); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		expiresAt := time.Now().UTC().Add(30 * time.Second)
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.runtime_leases(ref,organization_id,run_id,node_id,workload_instance,fence_digest,generation,state,input_digest,expires_at) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5,$6,$7,'CLAIMED',$8,$9)`, leaseRef, scope.organizationID, runID, nodeID, payload.WorkloadInstance, hex.EncodeToString(fenceDigest[:]), generation, hex.EncodeToString(inputDigest[:]), expiresAt); err != nil {
			return commandOutcome{}, errs.ErrConflict
		}
		if _, err := tx.Exec(ctx, `UPDATE control_plane.run_nodes SET state='RUNNING',started_at=COALESCE(started_at,clock_timestamp()),version=version+1 WHERE id=$1::uuid`, nodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		event, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, nodeRef, "TURN_STARTED", nodeRef, "", "", "", "Агент начал работу", "RUNNING", "RUNNING")
		if err != nil {
			return commandOutcome{}, err
		}
		var inputMap map[string]any
		_ = jsonUnmarshal(rawInput, &inputMap)
		items = append(items, map[string]any{"runRef": runRef, "nodeRef": nodeRef, "sessionRef": sessionRef, "task": task, "leaseRef": leaseRef, "fence": fence, "generation": generation, "expiresAt": expiresAt, "agentRef": agentRef, "runtimeKey": runtimeKey, "runtimeRevision": runtimeRevision, "instructionRef": instructionRef, "instructionDigest": instructionDigest, "instructions": instructions, "capabilities": capabilities, "knowledgeArtifactRefs": knowledge, "input": inputMap, "eventRef": event.Ref})
		if firstRunRef == "" {
			firstProjectID, firstProjectRef, firstRunRef = projectID, projectRef, runRef
		}
	}
	if err := rows.Err(); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	resourceRef := firstRunRef
	if resourceRef == "" {
		resourceRef = payload.WorkloadInstance
	}
	return commandOutcome{result: command.Result{RuntimeItems: items}, projectID: firstProjectID, projectRef: firstProjectRef, resourceKind: "RUNTIME_CLAIM", resourceRef: resourceRef, summary: "Work claims materialized"}, nil
}

func jsonUnmarshal(raw []byte, target any) error { return json.Unmarshal(raw, target) }

func (repository *Repository) lease(ctx context.Context, tx pgx.Tx, scope scope, payload command.LeaseInput, lock bool) (map[string]any, error) {
	suffix := ""
	if lock {
		suffix = " FOR UPDATE"
	}
	var leaseID, runID, nodeID, rootRunID, projectID, projectRef, runRef, nodeRef, storedDigest, state string
	var generation int64
	var expiresAt time.Time
	err := tx.QueryRow(ctx, `SELECT l.id::text,l.run_id::text,l.node_id::text,r.root_run_id::text,COALESCE(r.project_id::text,''),COALESCE(p.ref,''),r.ref,n.ref,l.fence_digest,l.generation,l.state,l.expires_at FROM control_plane.runtime_leases l JOIN control_plane.runs r ON r.id=l.run_id LEFT JOIN control_plane.projects p ON p.id=r.project_id JOIN control_plane.run_nodes n ON n.id=l.node_id WHERE l.organization_id=$1::uuid AND l.ref=$2`+suffix, scope.organizationID, payload.LeaseRef).Scan(&leaseID, &runID, &nodeID, &rootRunID, &projectID, &projectRef, &runRef, &nodeRef, &storedDigest, &generation, &state, &expiresAt)
	if err != nil {
		return nil, errs.ErrNotFound
	}
	digest := sha256.Sum256([]byte(payload.Fence))
	if storedDigest != hex.EncodeToString(digest[:]) || generation != payload.Generation || state != "CLAIMED" || time.Now().After(expiresAt) {
		return nil, errs.ErrForbidden
	}
	return map[string]any{"leaseID": leaseID, "runID": runID, "nodeID": nodeID, "rootRunID": rootRunID, "projectID": projectID, "projectRef": projectRef, "runRef": runRef, "nodeRef": nodeRef, "generation": generation}, nil
}

func (repository *Repository) renewExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LeaseInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, payload, true)
	if err != nil {
		return commandOutcome{}, err
	}
	expires := time.Now().UTC().Add(30 * time.Second)
	if _, err := tx.Exec(ctx, `UPDATE control_plane.runtime_leases SET expires_at=$2,updated_at=clock_timestamp() WHERE id=$1::uuid`, lease["leaseID"], expires); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	return commandOutcome{result: command.Result{Runtime: map[string]any{"leaseRef": payload.LeaseRef, "fence": payload.Fence, "generation": payload.Generation, "expiresAt": expires}}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUNTIME_LEASE", resourceRef: payload.LeaseRef, summary: "Runtime lease renewed"}, nil
}

func (repository *Repository) reportProgress(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.LeaseInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, payload, true)
	if err != nil {
		return commandOutcome{}, err
	}
	progress := truncate(payload.Progress, 2000)
	if _, err := tx.Exec(ctx, `UPDATE control_plane.run_nodes SET progress_summary=$2,version=version+1 WHERE id=$1::uuid`, lease["nodeID"], progress); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), stringMap(lease, "nodeRef"), "TURN_PROGRESS", stringMap(lease, "nodeRef"), "", "", "", progress, "RUNNING", "RUNNING")
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, stringMap(lease, "runRef"))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN_NODE", resourceRef: stringMap(lease, "nodeRef"), summary: "Runtime progress recorded"}, nil
}

func stringMap(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func (repository *Repository) completeExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.CompleteExecutionInput)
	if !ok {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}, true)
	if err != nil {
		return commandOutcome{}, err
	}
	nodeState, runState := "SUCCEEDED", "RUNNING"
	if !payload.Success {
		nodeState, runState = "FAILED", "FAILED"
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.runtime_leases SET state='COMPLETED',updated_at=clock_timestamp() WHERE id=$1::uuid`, lease["leaseID"]); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var humanGateAfter bool
	var turnID, sessionID, targetType string
	if err := tx.QueryRow(ctx, `UPDATE control_plane.run_nodes SET state=$2,progress_summary=$3,safe_error_code=$4,safe_error_message=$5,finished_at=clock_timestamp(),next_actions=CASE WHEN $2='FAILED' THEN ARRAY['OPEN','RETRY'] ELSE ARRAY['OPEN'] END,version=version+1 WHERE id=$1::uuid RETURNING human_gate_after,COALESCE(turn_id::text,'')`, lease["nodeID"], nodeState, truncate(payload.ResultSummary, 2000), truncate(payload.SafeErrorCode, 100), truncate(payload.SafeErrorMessage, 2000)).Scan(&humanGateAfter, &turnID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if turnID != "" {
		_, _ = tx.Exec(ctx, `UPDATE control_plane.session_turns SET state=$2,completed_at=clock_timestamp() WHERE id=$1::uuid`, turnID, map[bool]string{true: "COMPLETED", false: "FAILED"}[payload.Success])
	}
	if err := tx.QueryRow(ctx, `SELECT session_id::text,target_type FROM control_plane.runs WHERE id=$1::uuid`, lease["runID"]).Scan(&sessionID, &targetType); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	artifactRefs := []string{}
	for _, artifact := range payload.Artifacts {
		ref, _ := newRef("art")
		digest := sha256.Sum256([]byte(artifact.ObjectReceiptRef))
		var artifactID string
		if err := tx.QueryRow(ctx, `INSERT INTO control_plane.artifacts(ref,organization_id,project_id,run_id,node_id,file_name,media_type,size_bytes,digest,scan_state,object_receipt_ref,preview_state,created_by) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6,$7,$8,$9,'PENDING',$10,'UNAVAILABLE',$11::uuid) RETURNING id::text`, ref, scope.organizationID, nullUUID(stringMap(lease, "projectID")), lease["runID"], lease["nodeID"], safeFileName(artifact.FileName), artifact.MediaType, artifact.SizeBytes, "sha256:"+hex.EncodeToString(digest[:]), artifact.ObjectReceiptRef, scope.actorID).Scan(&artifactID); err != nil {
			return commandOutcome{}, mapWriteError(err)
		}
		artifactRefs = append(artifactRefs, ref)
		_ = artifactID
	}
	if _, err := tx.Exec(ctx, `UPDATE control_plane.runs SET state=$2,result_summary=$3,safe_error_code=$4,safe_error_message=$5,finished_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid AND id<>root_run_id`, lease["runID"], map[bool]string{true: "SUCCEEDED", false: "FAILED"}[payload.Success], truncate(payload.ResultSummary, 4000), truncate(payload.SafeErrorCode, 100), truncate(payload.SafeErrorMessage, 2000)); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if targetType == "SYSTEM_ASSISTANT" {
		turnRef, _ := newRef("trn")
		var next int64
		if err := tx.QueryRow(ctx, `SELECT next_turn_number FROM control_plane.sessions WHERE id=$1::uuid FOR UPDATE`, sessionID).Scan(&next); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.session_turns(ref,organization_id,session_id,run_id,turn_number,actor_kind,actor_ref,content,artifact_refs,state,completed_at) SELECT $1,$2::uuid,$3::uuid,$4::uuid,$5,'SYSTEM_ASSISTANT',a.ref,$6,$7,$8,clock_timestamp() FROM control_plane.agents a WHERE a.organization_id=$2::uuid AND a.system_key='system-assistant'`, turnRef, scope.organizationID, sessionID, lease["runID"], next, nonEmptyResult(payload), artifactRefs, map[bool]string{true: "COMPLETED", false: "FAILED"}[payload.Success]); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		_, _ = tx.Exec(ctx, `UPDATE control_plane.sessions SET next_turn_number=next_turn_number+1,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, sessionID)
		_, _ = tx.Exec(ctx, `UPDATE control_plane.assistant_conversations SET version=version+1,updated_at=clock_timestamp() WHERE session_id=$1::uuid`, sessionID)
	}
	if payload.Success && humanGateAfter {
		gateNodeRef, _ := newRef("nod")
		var gateNodeID string
		if err := tx.QueryRow(ctx, `INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,parent_node_id,type,state,display_name,role,next_actions) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'HUMAN_GATE','WAITING','Требуется решение','Проверка результата',ARRAY['OPEN','RESOLVE_GATE']) RETURNING id::text`, gateNodeRef, scope.organizationID, lease["rootRunID"], lease["runID"], lease["nodeID"]).Scan(&gateNodeID); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		edgeRef, _ := newRef("edg")
		_, _ = tx.Exec(ctx, `INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'WAITING_FOR','Ожидает решения')`, edgeRef, scope.organizationID, lease["rootRunID"], lease["nodeID"], gateNodeID)
		gateRef, _ := newRef("gat")
		if _, err := tx.Exec(ctx, `INSERT INTO control_plane.owner_gates(ref,organization_id,project_id,root_run_id,node_id,title,prompt,context_summary,allowed_decisions,state) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'Проверить результат','Подтвердите продолжение процесса',$6,ARRAY['APPROVE','REJECT','REQUEST_CHANGES','CANCEL'],'OPEN')`, gateRef, scope.organizationID, lease["projectID"], lease["rootRunID"], gateNodeID, truncate(payload.ResultSummary, 1000)); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := tx.Exec(ctx, `UPDATE control_plane.runs SET state='WAITING_HUMAN',version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, lease["rootRunID"]); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		runState = "WAITING_HUMAN"
		if _, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), gateRef, "OWNER_GATE_OPENED", gateNodeRef, edgeRef, gateRef, "", "Требуется решение человека", runState, "WAITING"); err != nil {
			return commandOutcome{}, err
		}
	}
	if !payload.Success {
		if _, err := tx.Exec(ctx, `UPDATE control_plane.runs SET state='FAILED',result_summary=$2,safe_error_code=$3,safe_error_message=$4,finished_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, lease["rootRunID"], truncate(payload.ResultSummary, 4000), truncate(payload.SafeErrorCode, 100), truncate(payload.SafeErrorMessage, 2000)); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
	} else if !humanGateAfter {
		var active int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM control_plane.run_nodes WHERE root_run_id=$1::uuid AND type='AGENT_EXECUTION' AND state IN('QUEUED','RUNNING','WAITING')`, lease["rootRunID"]).Scan(&active); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if active == 0 {
			runState = "SUCCEEDED"
			if _, err := tx.Exec(ctx, `UPDATE control_plane.runs SET state='SUCCEEDED',result_summary=$2,finished_at=clock_timestamp(),version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, lease["rootRunID"], truncate(payload.ResultSummary, 4000)); err != nil {
				return commandOutcome{}, errs.ErrUnavailable
			}
			_, _ = tx.Exec(ctx, `UPDATE control_plane.run_nodes SET state='SUCCEEDED',finished_at=clock_timestamp(),version=version+1 WHERE root_run_id=$1::uuid AND type='ROOT_PROCESS'`, lease["rootRunID"])
		}
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), stringMap(lease, "nodeRef"), "TURN_COMPLETED", stringMap(lease, "nodeRef"), "", "", "", nonEmptyResult(payload), runState, nodeState)
	if err != nil {
		return commandOutcome{}, err
	}
	run, graph, err := repository.readRunGraphTx(ctx, tx, scope, stringMap(lease, "runRef"))
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &run, Graph: &graph, Event: &event, CreatedRefs: artifactRefs}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN_NODE", resourceRef: stringMap(lease, "nodeRef"), summary: "Runtime execution completed"}, nil
}

func nonEmptyResult(payload command.CompleteExecutionInput) string {
	if text := strings.TrimSpace(payload.ResultSummary); text != "" {
		return truncate(text, 2000)
	}
	if payload.Success {
		return "Задача выполнена"
	}
	return "Задача завершилась ошибкой"
}

func (repository *Repository) delegateExecution(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.DelegateInput)
	if !ok || payload.TargetAgentRef == "" || strings.TrimSpace(payload.Task) == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	lease, err := repository.lease(ctx, tx, scope, command.LeaseInput{LeaseRef: payload.LeaseRef, Fence: payload.Fence, Generation: payload.Generation}, true)
	if err != nil {
		return commandOutcome{}, err
	}
	var allowed bool
	if err := tx.QueryRow(ctx, `SELECT 'platform.run.delegate'=ANY(a.capabilities) FROM control_plane.run_nodes n JOIN control_plane.agents a ON a.id=n.agent_id WHERE n.id=$1::uuid`, lease["nodeID"]).Scan(&allowed); err != nil || !allowed {
		return commandOutcome{}, errs.ErrForbidden
	}
	var agentID, agentName, role string
	if err := tx.QueryRow(ctx, `SELECT a.id::text,a.name,a.role_description FROM control_plane.agents a WHERE a.organization_id=$1::uuid AND a.project_id=$2::uuid AND a.ref=$3 AND a.enabled AND a.state='READY'`, scope.organizationID, lease["projectID"], payload.TargetAgentRef).Scan(&agentID, &agentName, &role); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	childRef, _ := newRef("run")
	var sessionID, initiatorID, parentRunID string
	if err := tx.QueryRow(ctx, `SELECT session_id::text,initiated_by::text,id::text FROM control_plane.runs WHERE id=$1::uuid`, lease["runID"]).Scan(&sessionID, &initiatorID, &parentRunID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	var childID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.runs(ref,organization_id,project_id,session_id,root_run_id,parent_run_id,target_type,target_ref,source,title,task,input,state,initiated_by,started_at) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,$6::uuid,'AGENT',$7,'AGENT_DELEGATION',$8,$9,$10,'RUNNING',$11::uuid,clock_timestamp()) RETURNING id::text`, childRef, scope.organizationID, lease["projectID"], sessionID, lease["rootRunID"], parentRunID, payload.TargetAgentRef, agentName+": "+truncate(payload.Task, 100), payload.Task, asJSON(payload.Input), initiatorID).Scan(&childID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	turnRef, _ := newRef("trn")
	var turnID string
	var turnNumber int64
	if err := tx.QueryRow(ctx, `SELECT next_turn_number FROM control_plane.sessions WHERE id=$1::uuid FOR UPDATE`, sessionID).Scan(&turnNumber); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.session_turns(ref,organization_id,session_id,run_id,turn_number,actor_kind,actor_ref,content,state) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5,'AGENT',$6,$7,'QUEUED') RETURNING id::text`, turnRef, scope.organizationID, sessionID, childID, turnNumber, stringMap(lease, "nodeRef"), payload.Task).Scan(&turnID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	_, _ = tx.Exec(ctx, `UPDATE control_plane.sessions SET next_turn_number=next_turn_number+1,version=version+1,updated_at=clock_timestamp() WHERE id=$1::uuid`, sessionID)
	nodeRef, _ := newRef("nod")
	var nodeID string
	if err := tx.QueryRow(ctx, `INSERT INTO control_plane.run_nodes(ref,organization_id,root_run_id,run_id,parent_node_id,type,state,display_name,role,agent_id,turn_id,input_summary,next_actions) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'AGENT_EXECUTION','QUEUED',$6,$7,$8::uuid,$9::uuid,$10,ARRAY['OPEN','CANCEL']) RETURNING id::text`, nodeRef, scope.organizationID, lease["rootRunID"], childID, lease["nodeID"], agentName, role, agentID, turnID, truncate(payload.Task, 1000)).Scan(&nodeID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	delegateEdgeRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'DELEGATED_TO','Делегировано дочернему агенту')`, delegateEdgeRef, scope.organizationID, lease["rootRunID"], lease["nodeID"], nodeID); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	callbackEdgeRef, _ := newRef("edg")
	if _, err := tx.Exec(ctx, `INSERT INTO control_plane.run_edges(ref,organization_id,root_run_id,source_node_id,target_node_id,type,label) VALUES($1,$2::uuid,$3::uuid,$4::uuid,$5::uuid,'CALLBACK_TO','Вернуть результат родителю')`, callbackEdgeRef, scope.organizationID, lease["rootRunID"], nodeID, lease["nodeID"]); err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	event, err := repository.emitRunEvent(ctx, tx, scope, stringMap(lease, "projectID"), stringMap(lease, "rootRunID"), childRef, "DELEGATION_CREATED", nodeRef, delegateEdgeRef, "", "", "Дочерний агент запущен", "RUNNING", "QUEUED")
	if err != nil {
		return commandOutcome{}, err
	}
	child, graph, err := repository.readRunGraphTx(ctx, tx, scope, childRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &child, Graph: &graph, Event: &event, Runtime: map[string]any{"callbackEdgeRef": callbackEdgeRef}}, projectID: stringMap(lease, "projectID"), projectRef: stringMap(lease, "projectRef"), resourceKind: "RUN", resourceRef: childRef, summary: "Child run delegated"}, nil
}

func (repository *Repository) deliverCallback(ctx context.Context, tx pgx.Tx, scope scope, input command.Command) (commandOutcome, error) {
	payload, ok := input.Payload.(command.CallbackInput)
	if !ok || payload.ChildRunRef == "" || payload.CallbackEdgeRef == "" {
		return commandOutcome{}, errs.ErrInvalid
	}
	var childID, rootRunID, projectID, projectRef, parentRunID, resultSummary, edgeID, parentNodeID string
	var childState string
	if err := tx.QueryRow(ctx, `SELECT child.id::text,child.root_run_id::text,child.project_id::text,p.ref,child.parent_run_id::text,child.result_summary,child.state,e.id::text,e.target_node_id::text FROM control_plane.runs child JOIN control_plane.projects p ON p.id=child.project_id JOIN control_plane.run_edges e ON e.root_run_id=child.root_run_id AND e.ref=$3 AND e.type='CALLBACK_TO' WHERE child.organization_id=$1::uuid AND child.ref=$2 FOR UPDATE OF child,e`, scope.organizationID, payload.ChildRunRef, payload.CallbackEdgeRef).Scan(&childID, &rootRunID, &projectID, &projectRef, &parentRunID, &resultSummary, &childState, &edgeID, &parentNodeID); err != nil {
		return commandOutcome{}, errs.ErrNotFound
	}
	if childState != "SUCCEEDED" {
		return commandOutcome{}, errs.ErrConflict
	}
	tag, err := tx.Exec(ctx, `INSERT INTO control_plane.callback_receipts(child_run_id,callback_edge_id) VALUES($1::uuid,$2::uuid) ON CONFLICT DO NOTHING`, childID, edgeID)
	if err != nil {
		return commandOutcome{}, errs.ErrUnavailable
	}
	duplicate := tag.RowsAffected() == 0
	if !duplicate {
		if _, err := tx.Exec(ctx, `UPDATE control_plane.run_nodes SET callback_summary=$2,version=version+1 WHERE id=$1::uuid`, parentNodeID, truncate(resultSummary, 2000)); err != nil {
			return commandOutcome{}, errs.ErrUnavailable
		}
		if _, err := repository.emitRunEvent(ctx, tx, scope, projectID, rootRunID, payload.ChildRunRef, "CALLBACK_DELIVERED", "", payload.CallbackEdgeRef, "", "", "Результат дочернего агента доставлен", "RUNNING", ""); err != nil {
			return commandOutcome{}, err
		}
	}
	parentRef := mustRunRef(ctx, tx, parentRunID)
	parent, graph, err := repository.readRunGraphTx(ctx, tx, scope, parentRef)
	if err != nil {
		return commandOutcome{}, err
	}
	return commandOutcome{result: command.Result{Run: &parent, Graph: &graph, Duplicate: duplicate}, projectID: projectID, projectRef: projectRef, resourceKind: "CALLBACK", resourceRef: payload.CallbackEdgeRef, summary: "Child callback processed"}, nil
}
