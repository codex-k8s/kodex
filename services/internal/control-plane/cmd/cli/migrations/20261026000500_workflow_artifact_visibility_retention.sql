-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL lock_timeout = '8s';
SET LOCAL statement_timeout = '30s';

-- Назначенный physical step, а не manager exception или похожий workflow key.
-- Helper подтверждает только owner lineage; доступ остаётся в canonical reader.
-- +goose StatementBegin
CREATE FUNCTION control_plane.runtime_workflow_step(p_tenant uuid,p_node uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT EXISTS (
   SELECT 1 FROM control_plane.run_nodes node
   JOIN control_plane.runs physical ON physical.id=node.run_id AND physical.organization_id=node.organization_id
     AND physical.root_run_id=node.root_run_id
   JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
     AND root.project_id=physical.project_id
   JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
   JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.organization_id=node.organization_id
     AND agent.project_id=root.project_id
   CROSS JOIN LATERAL jsonb_array_elements(version.spec->'Steps') step
   WHERE node.organization_id=p_tenant AND node.id=p_node AND node.type='AGENT_EXECUTION'
     AND node.materialization_state='MATERIALIZED' AND node.workflow_business_attempt BETWEEN 1 AND 4
     AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
     AND step->>'Key'=node.workflow_step_key AND step->>'AgentRef'=agent.ref
 );
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.runtime_workflow_step(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.runtime_workflow_step(uuid,uuid) TO control_plane_runtime;

-- Capture pins могут предшествовать final business result. Принимается только
-- замкнутая обратная physical цепочка текущей business attempt, не весь root.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_captured_artifact_pins(p_tenant uuid,p_node uuid)
RETURNS TABLE(artifact_id uuid,revision_id uuid)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 WITH RECURSIVE final AS MATERIALIZED (
   SELECT node.*,run.session_id,run.project_id,
     COALESCE(node.workflow_step_origin_node_id,node.id) AS origin_id
   FROM control_plane.run_nodes node
   JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=node.organization_id
     AND run.root_run_id=node.root_run_id
   WHERE node.organization_id=p_tenant AND node.id=p_node
     AND control_plane.runtime_workflow_step(p_tenant,p_node)
 ), chain AS (
   SELECT final.id,final.turn_id,final.attempt,final.workflow_captured_artifacts,
     0 AS depth,ARRAY[final.id] AS path,false AS cycle FROM final
   UNION ALL
   SELECT previous.id,previous.turn_id,previous.attempt,previous.workflow_captured_artifacts,
     chain.depth+1,chain.path||previous.id,previous.id=ANY(chain.path)
   FROM chain CROSS JOIN final
   JOIN control_plane.run_edges edge ON edge.organization_id=p_tenant AND edge.root_run_id=final.root_run_id
     AND edge.type='CONTINUES' AND edge.target_node_id=chain.id
   JOIN control_plane.run_nodes previous ON previous.id=edge.source_node_id AND previous.organization_id=p_tenant
     AND previous.root_run_id=final.root_run_id AND previous.run_id=final.run_id AND previous.agent_id=final.agent_id
     AND previous.state='SUCCEEDED' AND previous.attempt+1=chain.attempt
     AND COALESCE(previous.workflow_step_origin_node_id,previous.id)=final.origin_id
     AND previous.workflow_business_attempt=final.workflow_business_attempt
   JOIN control_plane.session_turns turn ON turn.id=previous.turn_id AND turn.run_id=previous.run_id
     AND turn.session_id=final.session_id
   JOIN control_plane.runtime_revisions proof ON proof.id::text=previous.workflow_captured_artifacts->>'runtimeRevisionId'
     AND proof.organization_id=p_tenant AND proof.node_id=previous.id AND proof.run_id=previous.run_id
     AND proof.root_run_id=previous.root_run_id AND proof.agent_id=previous.agent_id
     AND proof.turn_id=previous.turn_id AND proof.attempt=previous.attempt
   JOIN control_plane.runtime_leases lease ON lease.id::text=previous.workflow_captured_artifacts->>'leaseId'
     AND lease.organization_id=p_tenant AND lease.runtime_revision_id=proof.id AND lease.node_id=previous.id
     AND lease.run_id=previous.run_id AND lease.generation=proof.generation AND lease.state='COMPLETED'
     AND previous.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)
   WHERE NOT chain.cycle AND chain.depth<65
     AND 1=(SELECT count(*) FROM control_plane.run_edges incoming
       WHERE incoming.organization_id=p_tenant AND incoming.root_run_id=final.root_run_id
         AND incoming.type='CONTINUES' AND incoming.target_node_id=chain.id)
 ), closed AS (
   SELECT final.* FROM final
   WHERE NOT EXISTS (SELECT 1 FROM chain WHERE cycle OR depth>64)
     AND NOT EXISTS (
       SELECT 1 FROM chain current
       WHERE (SELECT count(*) FROM control_plane.run_edges incoming
         WHERE incoming.organization_id=p_tenant AND incoming.root_run_id=final.root_run_id
           AND incoming.type='CONTINUES' AND incoming.target_node_id=current.id)>1
       OR EXISTS (SELECT 1 FROM control_plane.run_edges incoming
         WHERE incoming.organization_id=p_tenant AND incoming.root_run_id=final.root_run_id
           AND incoming.type='CONTINUES' AND incoming.target_node_id=current.id
           AND NOT EXISTS (SELECT 1 FROM chain previous
             WHERE previous.id=incoming.source_node_id AND previous.depth=current.depth+1)))
 )
 SELECT DISTINCT artifact.id,artifact.revision_id FROM chain CROSS JOIN closed
 JOIN control_plane.runtime_revisions proof ON proof.id::text=chain.workflow_captured_artifacts->>'runtimeRevisionId'
   AND proof.organization_id=p_tenant AND proof.node_id=chain.id AND proof.run_id=closed.run_id
   AND proof.root_run_id=closed.root_run_id AND proof.agent_id=closed.agent_id
   AND proof.turn_id=chain.turn_id AND proof.attempt=chain.attempt
 JOIN control_plane.runtime_leases lease ON lease.id::text=chain.workflow_captured_artifacts->>'leaseId'
   AND lease.organization_id=p_tenant AND lease.runtime_revision_id=proof.id
   AND lease.node_id=chain.id AND lease.run_id=closed.run_id AND lease.generation=proof.generation
   AND chain.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation) AND lease.state='COMPLETED'
 CROSS JOIN LATERAL jsonb_array_elements(chain.workflow_captured_artifacts->'artifacts') pin
 JOIN control_plane.artifact_history artifact ON artifact.organization_id=p_tenant AND artifact.project_id=closed.project_id
   AND artifact.node_id=chain.id AND artifact.run_id=closed.run_id AND artifact.source='AGENT_RESULT'
   AND artifact.ref=pin->>'ref' AND artifact.revision_ref=pin->>'revisionRef'
   AND to_jsonb(artifact.revision)=pin->'revision' AND artifact.digest=pin->>'digest'
   AND to_jsonb(artifact.size_bytes)=pin->'sizeBytes' AND artifact.file_name=pin->>'fileName'
   AND artifact.media_type=pin->>'mediaType'
 JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
   AND content.digest=artifact.digest AND content.size_bytes=artifact.size_bytes;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_captured_artifact_pins(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_captured_artifact_pins(uuid,uuid) TO control_plane_runtime;

-- Вход потребителя — exact immutable accepted receipt объявленного dependency.
-- Его final physical node выбирает свою capture ancestry, включая EARLY producer.
-- +goose StatementBegin
CREATE FUNCTION control_plane.workflow_runtime_artifact_pins(p_tenant uuid,p_node uuid)
RETURNS TABLE(artifact_id uuid,revision_id uuid)
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 WITH consumer AS MATERIALIZED (
   SELECT node.*,root.project_id,version.spec FROM control_plane.run_nodes node
   JOIN control_plane.runs root ON root.id=node.root_run_id AND root.organization_id=node.organization_id
   JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_tenant
   WHERE node.organization_id=p_tenant AND node.id=p_node AND control_plane.runtime_workflow_step(p_tenant,p_node)
 ), accepted AS (
   SELECT consumer.root_run_id,consumer.project_id,input->'receipt' AS receipt
   FROM consumer CROSS JOIN LATERAL jsonb_array_elements(consumer.workflow_inputs) input
   JOIN control_plane.run_nodes source ON source.id::text=input->>'sourceNodeId' AND source.organization_id=p_tenant
     AND source.root_run_id=consumer.root_run_id AND source.workflow_step_origin_node_id IS NULL
     AND source.workflow_business_attempt=1 AND source.workflow_step_key=input->>'stepKey'
   CROSS JOIN LATERAL jsonb_array_elements(source.workflow_attempts) journal
   WHERE journal=input->'receipt' AND journal->>'accepted'='true'
     AND EXISTS (SELECT 1 FROM jsonb_array_elements(consumer.spec->'Steps') step
       WHERE step->>'Key'=consumer.workflow_step_key AND step->'DependsOn' ? source.workflow_step_key)
 ), dependency AS (
   SELECT pins.artifact_id,pins.revision_id FROM accepted
   JOIN control_plane.run_nodes physical ON physical.id::text=accepted.receipt #>> '{provenance,nodeId}'
     AND physical.organization_id=p_tenant AND physical.root_run_id=accepted.root_run_id AND physical.state='SUCCEEDED'
     AND to_jsonb(physical.workflow_business_attempt)=accepted.receipt->'attempt'
   CROSS JOIN LATERAL control_plane.workflow_captured_artifact_pins(p_tenant,physical.id) pins
   JOIN control_plane.artifact_history artifact ON artifact.id=pins.artifact_id AND artifact.revision_id=pins.revision_id
     AND artifact.organization_id=p_tenant AND artifact.project_id=accepted.project_id
   CROSS JOIN LATERAL jsonb_array_elements(accepted.receipt->'results') result
   WHERE result->>'kind'='ARTIFACT' AND result->>'artifactRef'=artifact.ref
     AND result->>'artifactRevision'=artifact.revision_ref AND 'sha256:'||(result->>'digest')=artifact.digest
 )
 SELECT * FROM control_plane.workflow_captured_artifact_pins(p_tenant,p_node)
 UNION SELECT * FROM dependency;
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.workflow_runtime_artifact_pins(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.workflow_runtime_artifact_pins(uuid,uuid) TO control_plane_runtime;

-- Возвращаем только provenance membership, не отдельный grant на body/download.
-- +goose StatementBegin
CREATE FUNCTION control_plane.runtime_workflow_input_visible(p_tenant uuid,p_node uuid,p_revision uuid)
RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog AS $$
 SELECT EXISTS (SELECT 1 FROM control_plane.workflow_runtime_artifact_pins(p_tenant,p_node) pin
   WHERE pin.revision_id=p_revision);
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.runtime_workflow_input_visible(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.runtime_workflow_input_visible(uuid,uuid,uuid) TO control_plane_runtime;

-- Сохраняем OID и весь актуальный immutable-history reader. Меняется только
-- RUN_RESULT: server root не получает legacy project-wide artifact.manage fallback.
-- +goose StatementBegin
DO $migration$
DECLARE definition text; prefix text; suffix text;
BEGIN
 IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'::regprocedure)
      IS DISTINCT FROM 'e616be43adcd8e00f3f076514c3c56d5' THEN
   RAISE EXCEPTION 'workflow runtime file source profile changed'; END IF;
 SELECT pg_get_functiondef('control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid)'::regprocedure) INTO definition;
 prefix:=E'WHEN ''RUN_RESULT'' THEN file.source IN (''AGENT_RESULT'',''INTEGRATION_RESULT'')\n              AND (';
 suffix:=E'              )\n              AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets run_target';
 IF (length(definition)-length(replace(definition,prefix,'')))/length(prefix)<>1
    OR (length(definition)-length(replace(definition,suffix,'')))/length(suffix)<>1 THEN
   RAISE EXCEPTION 'workflow runtime file branch changed'; END IF;
 definition:=replace(definition,prefix,$patch$WHEN 'RUN_RESULT' THEN file.source IN ('AGENT_RESULT','INTEGRATION_RESULT')
              AND CASE WHEN EXISTS (
                SELECT 1 FROM control_plane.run_nodes execution
                JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=execution.organization_id
                JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=p_tenant
                WHERE execution.id=p_node AND execution.organization_id=p_tenant AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
              ) THEN EXISTS (
                SELECT 1 FROM control_plane.run_nodes execution
                JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=p_tenant
                WHERE execution.id=p_node AND execution.organization_id=p_tenant AND execution.agent_id=p_agent
                  AND root.project_id=p_project AND root.initiated_by=p_actor
                  AND control_plane.runtime_workflow_input_visible(p_tenant,p_node,file.revision_id)
              ) ELSE ($patch$);
 definition:=replace(definition,suffix,E'              ) END\n              AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets run_target');
 EXECUTE definition;
END $migration$;
-- +goose StatementEnd

-- Capture держит exact revision на всём active root, в том числе до acceptance
-- и до frozen RR следующего хода. Это consumer retention, не право чтения.
-- Прежние bundle UNKNOWN, RR, callback, skill, attachment и binding ветви остаются.
-- +goose StatementBegin
DO $migration$
DECLARE definition text; marker text;
BEGIN
 IF (SELECT md5(prosrc) FROM pg_proc WHERE oid='control_plane.artifact_has_retained_revisions(uuid)'::regprocedure)
      IS DISTINCT FROM 'a47d0665a9c7bf837a82da01169a10f1' THEN
   RAISE EXCEPTION 'workflow artifact retention profile changed'; END IF;
 SELECT pg_get_functiondef('control_plane.artifact_has_retained_revisions(uuid)'::regprocedure) INTO definition;
 marker:=E'    SELECT EXISTS (\n        SELECT 1 FROM control_plane.repository_bundle_publications publication';
 IF (length(definition)-length(replace(definition,marker,'')))/length(marker)<>1 THEN
   RAISE EXCEPTION 'workflow artifact retention branch changed'; END IF;
 definition:=replace(definition,marker,$patch$    SELECT EXISTS (
        SELECT 1 FROM control_plane.artifact_heads retained
        JOIN control_plane.runs root ON root.organization_id=retained.organization_id AND root.project_id=retained.project_id
          AND root.id=root.root_run_id AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')
        JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id AND version.organization_id=root.organization_id
          AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
        JOIN control_plane.run_nodes producer ON producer.organization_id=root.organization_id AND producer.root_run_id=root.id
          AND producer.workflow_business_attempt BETWEEN 1 AND 4 AND producer.id=retained.node_id AND producer.run_id=retained.run_id
        JOIN control_plane.runtime_revisions proof ON proof.id::text=producer.workflow_captured_artifacts->>'runtimeRevisionId'
          AND proof.organization_id=root.organization_id AND proof.root_run_id=root.id AND proof.node_id=producer.id
          AND proof.run_id=producer.run_id AND proof.agent_id=producer.agent_id AND proof.turn_id=producer.turn_id AND proof.attempt=producer.attempt
        JOIN control_plane.runtime_leases lease ON lease.id::text=producer.workflow_captured_artifacts->>'leaseId'
          AND lease.organization_id=root.organization_id AND lease.runtime_revision_id=proof.id AND lease.node_id=producer.id
          AND lease.run_id=producer.run_id AND lease.generation=proof.generation AND lease.state='COMPLETED'
          AND producer.workflow_captured_artifacts->'generation'=to_jsonb(lease.generation)
        CROSS JOIN LATERAL jsonb_array_elements(producer.workflow_captured_artifacts->'artifacts') pin
        JOIN control_plane.artifact_revisions revision ON revision.artifact_id=retained.id
          AND revision.ref=pin->>'revisionRef' AND revision.digest=pin->>'digest'
          AND to_jsonb(revision.revision)=pin->'revision' AND to_jsonb(revision.size_bytes)=pin->'sizeBytes'
          AND revision.file_name=pin->>'fileName' AND revision.media_type=pin->>'mediaType'
        WHERE retained.id=p_artifact_id AND retained.ref=pin->>'ref'
    ) OR EXISTS (
        SELECT 1 FROM control_plane.repository_bundle_publications publication$patch$);
 EXECUTE definition;
END $migration$;
-- +goose StatementEnd

RESET ROLE;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 RAISE EXCEPTION 'workflow artifact visibility migration is forward-only';
END $$;
-- +goose StatementEnd
