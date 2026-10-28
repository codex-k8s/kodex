-- +goose Up
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';

-- Профиль012 следует строго за согласованным011. Старые функции и provenance
-- businessAttempt0 не переписываются под manager: новый reader выводит scope
-- из текущего назначения episode, а не из task, payload или похожего step key.
-- Goose ledger принадлежит migrator: чтение выполняется до SET ROLE owner.
-- +goose StatementBegin
DO $migration$
BEGIN
 IF (SELECT max(version_id) FROM public.goose_db_version WHERE is_applied)
      IS DISTINCT FROM 20261026001100::bigint THEN
   RAISE EXCEPTION 'workflow exception context requires exact profile011';
 END IF;
END $migration$;
-- +goose StatementEnd

SET ROLE control_plane_owner;

-- +goose StatementBegin
DO $migration$
BEGIN
 IF (SELECT md5(prosrc) FROM pg_proc
     WHERE oid='control_plane.workflow_runtime_artifact_pins(uuid,uuid)'::regprocedure)
      IS DISTINCT FROM '901a48c03a0f96b7e6ff3f529b406a9c' THEN
   RAISE EXCEPTION 'workflow exception input reader profile changed';
 END IF;
END $migration$;
-- +goose StatementEnd

-- Только один текущий server-owned manager. Функция не выдаёт grant: каталог
-- дополнительно связывает lease/RR/generation, а все source ACL остаются свежими.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_exception_context_node(p_tenant uuid,p_node uuid)
RETURNS TABLE(root_id uuid,project_id uuid,actor_id uuid,source_node_id uuid,inputs jsonb,dependency_keys jsonb)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 WITH assigned AS MATERIALIZED (
   SELECT root.id AS root_id,root.project_id,root.initiated_by AS actor_id,
     root.workflow_exception AS episode,version.spec,manager.agent_id
   FROM control_plane.run_nodes manager
   JOIN control_plane.runs physical ON physical.id=manager.run_id AND physical.organization_id=manager.organization_id
     AND physical.root_run_id=manager.root_run_id AND physical.source='AGENT_DELEGATION'
     AND physical.target_type='AGENT' AND physical.state IN ('QUEUED','RUNNING','WAITING_HUMAN')
   JOIN control_plane.runs root ON root.id=manager.root_run_id AND root.organization_id=manager.organization_id
     AND root.id=root.root_run_id AND root.target_type='WORKFLOW' AND physical.parent_run_id=root.id
     AND physical.project_id=root.project_id AND physical.initiated_by=root.initiated_by
     AND physical.workflow_version_id=root.workflow_version_id
   JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
   JOIN control_plane.workflows workflow ON workflow.id=version.workflow_id AND workflow.organization_id=root.organization_id
     AND workflow.project_id=root.project_id AND workflow.ref=root.target_ref
   JOIN control_plane.projects project ON project.id=root.project_id AND project.organization_id=root.organization_id
     AND project.lifecycle='ACTIVE'
   JOIN control_plane.subjects actor ON actor.id=root.initiated_by AND actor.organization_id=root.organization_id AND actor.active
   JOIN control_plane.agents agent ON agent.id=manager.agent_id AND agent.organization_id=root.organization_id
     AND agent.project_id=root.project_id AND agent.ref=version.spec->>'CoordinatorAgentRef'
     AND agent.ref=physical.target_ref AND agent.enabled AND agent.state IN ('READY','RUNNING')
   JOIN control_plane.session_turns turn ON turn.id=manager.turn_id AND turn.run_id=physical.id
     AND turn.session_id=physical.session_id AND turn.organization_id=root.organization_id
   JOIN control_plane.run_nodes parent ON parent.id=manager.parent_node_id AND parent.organization_id=root.organization_id
     AND parent.root_run_id=root.id AND parent.run_id=root.id AND parent.type='ROOT_PROCESS' AND parent.parent_node_id IS NULL
   JOIN control_plane.catalog_access_targets target ON target.organization_id=root.organization_id
     AND target.kind='WORKFLOW' AND target.id=workflow.id
   WHERE manager.organization_id=p_tenant AND manager.id=p_node AND manager.type='AGENT_EXECUTION'
     AND manager.materialization_state='MATERIALIZED' AND manager.state IN ('QUEUED','RUNNING')
     AND manager.workflow_business_attempt=0 AND manager.workflow_step_origin_node_id IS NULL
     AND manager.workflow_inputs='[]'::jsonb
     AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN') AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
     AND root.workflow_exception=root.workflow_exception_history->-1
     AND root.workflow_exception->>'state'='MANAGER_QUEUED'
     AND root.workflow_exception->>'managerNodeId'=manager.id::text
     AND root.workflow_exception->>'managerNodeRef'=manager.ref
     AND manager.workflow_step_key='workflow.coordinator.exception.'||(root.workflow_exception->>'episode')
     AND control_plane.runtime_execution_before_deadline(p_tenant,root.id)
     AND control_plane.catalog_resource_visible(p_tenant,actor.id,'workflow.launch','WORKFLOW',target.id,
       target.project_id,target.owner_id,target.related_ids,statement_timestamp(),false)
 ), source AS MATERIALIZED (
   SELECT assigned.*,producer.id AS source_node_id,producer.workflow_step_key,producer.workflow_inputs,
     producer.workflow_business_attempt,producer.workflow_step_origin_node_id,step->'DependsOn' AS dependency_keys
   FROM assigned
   JOIN control_plane.run_nodes producer ON producer.organization_id=p_tenant AND producer.root_run_id=assigned.root_id
     AND producer.id::text=assigned.episode->>'finalNodeId'
     AND producer.workflow_step_key=assigned.episode->>'stepKey'
   JOIN control_plane.agents producer_agent ON producer_agent.id=producer.agent_id
     AND producer_agent.organization_id=p_tenant AND producer_agent.project_id=assigned.project_id
   CROSS JOIN LATERAL jsonb_array_elements(assigned.spec->'Steps') step
   WHERE step->>'Key'=producer.workflow_step_key AND step->>'AgentRef'=producer_agent.ref
     AND ((assigned.episode->'attempt'='0'::jsonb AND producer.workflow_business_attempt=0
       AND producer.materialization_state='PLANNED' AND producer.state='PLANNED'
       AND producer.id::text=assigned.episode->>'originNodeId')
       OR (to_jsonb(producer.workflow_business_attempt)=assigned.episode->'attempt'
         AND producer.workflow_business_attempt BETWEEN 1 AND 4
         AND COALESCE(producer.workflow_step_origin_node_id,producer.id)::text=assigned.episode->>'originNodeId'
         AND producer.workflow_inputs=assigned.episode->'inputs'))
 )
 SELECT source.root_id,source.project_id,source.actor_id,source.source_node_id,source.episode->'inputs',source.dependency_keys
 FROM source;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_exception_context_node(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_exception_context_node(uuid,uuid) TO control_plane_runtime;

-- Собственные закрытые producer captures нужны и при RESULT_INVALID: наличие
-- файла не означает бизнес-приёмку. Входы — только прежние exact accepted receipts.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_exception_artifact_pins(p_tenant uuid,p_node uuid)
RETURNS TABLE(artifact_id uuid,revision_id uuid)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 WITH context AS MATERIALIZED (
   SELECT * FROM control_plane.workflow_exception_context_node(p_tenant,p_node)
 ), accepted AS MATERIALIZED (
   SELECT context.root_id,context.project_id,input->'receipt' AS receipt
   FROM context CROSS JOIN LATERAL jsonb_array_elements(context.inputs) input
   JOIN control_plane.run_nodes source ON source.organization_id=p_tenant AND source.root_run_id=context.root_id
     AND source.id::text=input->>'sourceNodeId' AND source.workflow_step_key=input->>'stepKey'
     AND source.workflow_step_origin_node_id IS NULL AND source.workflow_business_attempt=1
   CROSS JOIN LATERAL jsonb_array_elements(source.workflow_attempts) journal
   WHERE journal=input->'receipt' AND journal->>'accepted'='true'
     AND context.dependency_keys ? source.workflow_step_key
 ), dependency AS MATERIALIZED (
   SELECT artifact.id AS artifact_id,artifact.revision_id
   FROM accepted
   JOIN control_plane.run_nodes producer ON producer.organization_id=p_tenant AND producer.root_run_id=accepted.root_id
     AND producer.id::text=accepted.receipt #>> '{provenance,nodeId}' AND producer.state='SUCCEEDED'
     AND to_jsonb(producer.workflow_business_attempt)=accepted.receipt->'attempt'
   CROSS JOIN LATERAL control_plane.workflow_captured_artifact_pins(p_tenant,producer.id) pin
   JOIN control_plane.artifact_history artifact ON artifact.id=pin.artifact_id AND artifact.revision_id=pin.revision_id
     AND artifact.organization_id=p_tenant AND artifact.project_id=accepted.project_id
   CROSS JOIN LATERAL jsonb_array_elements(accepted.receipt->'results') result
   WHERE result->>'kind'='ARTIFACT' AND result->>'artifactRef'=artifact.ref
     AND result->>'artifactRevision'=artifact.revision_ref AND 'sha256:'||(result->>'digest')=artifact.digest
 ), candidates AS MATERIALIZED (
   SELECT pin.artifact_id,pin.revision_id
   FROM context CROSS JOIN LATERAL control_plane.workflow_captured_artifact_pins(p_tenant,context.source_node_id) pin
   UNION SELECT dependency.artifact_id,dependency.revision_id FROM dependency
 ), pinned AS MATERIALIZED (
   SELECT candidates.artifact_id,candidates.revision_id
   FROM candidates
   JOIN control_plane.artifact_history artifact ON artifact.id=candidates.artifact_id AND artifact.revision_id=candidates.revision_id
     AND artifact.organization_id=p_tenant AND artifact.lifecycle_state='ACTIVE'
   JOIN context ON context.project_id=artifact.project_id
   JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
     AND content.digest=artifact.digest AND content.size_bytes=artifact.size_bytes
 )
 SELECT pinned.artifact_id,pinned.revision_id FROM pinned
 WHERE control_plane.artifact_effective_scan(pinned.revision_id)='CLEAN';
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_exception_artifact_pins(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_exception_artifact_pins(uuid,uuid) TO control_plane_runtime;

-- Сохраняем OID/ACL, старую ветку assigned steps и весь canonical body reader.
-- Manager union выдаёт только membership exact ревизий; все пять fresh ACL,
-- immutable content и frozen entry digest по-прежнему проверяют общие readers.
-- +goose StatementBegin
DO $migration$
DECLARE definition text; marker text;
BEGIN
 SELECT pg_get_functiondef('control_plane.workflow_runtime_artifact_pins(uuid,uuid)'::regprocedure) INTO definition;
 marker:=E' UNION SELECT * FROM dependency;';
 IF (length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN
   RAISE EXCEPTION 'workflow exception artifact union marker changed';
 END IF;
 definition:=replace(definition,marker,E' UNION SELECT * FROM dependency\n UNION SELECT * FROM control_plane.workflow_exception_artifact_pins(p_tenant,p_node);');
 EXECUTE definition;
END $migration$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 RAISE EXCEPTION 'workflow exception context migration is forward-only';
END $$;
-- +goose StatementEnd
