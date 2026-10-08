-- +goose Up
SET ROLE control_plane_owner;
-- Исторические callbacks не дополняются предположениями: NULL не выдаёт intrinsic READ.
ALTER TABLE control_plane.callback_receipts ADD COLUMN result_snapshot jsonb;
ALTER TABLE control_plane.callback_receipts ADD CONSTRAINT callback_result_snapshot_shape CHECK (
    result_snapshot IS NULL OR (
        jsonb_typeof(result_snapshot)='object' AND result_snapshot->'format'='1'::jsonb
        AND jsonb_typeof(result_snapshot->'artifacts')='array'
        AND octet_length(result_snapshot::text)<=1048576
    )
);

-- +goose StatementBegin
CREATE FUNCTION control_plane.protect_callback_result_snapshot() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog,control_plane AS $$
DECLARE
    child control_plane.runs%ROWTYPE;
    edge control_plane.run_edges%ROWTYPE;
    pin jsonb;
BEGIN
    IF TG_OP<>'INSERT' THEN
        RAISE EXCEPTION 'callback result snapshot is immutable';
    END IF;
    IF NEW.result_snapshot IS NULL THEN
        RAISE EXCEPTION 'callback result snapshot is required';
    END IF;
    IF jsonb_typeof(NEW.result_snapshot) IS DISTINCT FROM 'object'
       OR NEW.result_snapshot->'format' IS DISTINCT FROM '1'::jsonb
       OR jsonb_typeof(NEW.result_snapshot->'artifacts') IS DISTINCT FROM 'array'
       OR NEW.result_snapshot - ARRAY['format','childRunRef','childVersion','nodeRef','state','resultSummary','artifacts'] <> '{}'::jsonb THEN
        RAISE EXCEPTION 'callback result snapshot format is invalid';
    END IF;
    SELECT * INTO STRICT child FROM control_plane.runs WHERE id=NEW.child_run_id;
    SELECT * INTO STRICT edge FROM control_plane.run_edges WHERE id=NEW.callback_edge_id;
    IF edge.type<>'CALLBACK_TO' OR edge.organization_id<>child.organization_id
       OR NEW.result_snapshot->>'childRunRef' IS DISTINCT FROM child.ref
       OR NEW.result_snapshot->>'state' IS DISTINCT FROM child.state
       OR NEW.result_snapshot->'childVersion' IS DISTINCT FROM to_jsonb(child.version)
       OR NEW.result_snapshot->>'resultSummary' IS DISTINCT FROM child.result_summary
       OR child.state NOT IN ('SUCCEEDED','FAILED','CANCELLED')
       OR NOT EXISTS (
           SELECT 1 FROM control_plane.run_nodes source
           JOIN control_plane.run_nodes target ON target.id=edge.target_node_id
           JOIN control_plane.runs parent ON parent.id=target.run_id
           WHERE source.id=edge.source_node_id AND source.organization_id=child.organization_id
             AND target.organization_id=child.organization_id AND parent.organization_id=child.organization_id
             AND parent.project_id IS NOT DISTINCT FROM child.project_id
             AND parent.initiated_by=child.initiated_by AND target.root_run_id=edge.root_run_id
             AND NEW.result_snapshot->>'nodeRef'=source.ref
             AND ((source.run_id=child.id AND EXISTS (
                 SELECT 1 FROM control_plane.run_edges delegation
                 WHERE delegation.organization_id=child.organization_id AND delegation.root_run_id=edge.root_run_id
                   AND delegation.type='DELEGATED_TO' AND delegation.source_node_id=target.id
                   AND delegation.target_node_id=source.id)) OR EXISTS (
                 SELECT 1 FROM control_plane.required_workflow_launches launch
                 WHERE launch.organization_id=child.organization_id AND launch.child_root_run_id=child.id
                   AND launch.proxy_node_id=source.id AND launch.callback_edge_id=edge.id
                   AND launch.origin_node_id=target.id AND launch.origin_root_run_id=edge.root_run_id
                   AND launch.state IN ('SUCCEEDED','FAILED','CANCELLED')))
       ) THEN
        RAISE EXCEPTION 'callback result origin is invalid';
    END IF;
    FOR pin IN SELECT value FROM jsonb_array_elements(NEW.result_snapshot->'artifacts') LOOP
        IF NOT EXISTS (
            SELECT 1 FROM control_plane.artifacts artifact
            JOIN control_plane.runtime_revisions revision ON revision.ref=pin->>'runtimeRevisionRef'
              AND revision.organization_id=child.organization_id AND revision.node_id=artifact.node_id
              AND revision.run_id=artifact.run_id AND revision.project_id=child.project_id
            WHERE artifact.organization_id=child.organization_id AND artifact.ref=pin->>'ref'
              AND (artifact.run_id=child.id OR revision.root_run_id=child.id)
              AND artifact.source='AGENT_RESULT'
              AND pin=jsonb_build_object('ref',artifact.ref,'revision',artifact.revision,'version',artifact.version,
                  'digest',artifact.digest,'sizeBytes',artifact.size_bytes,'fileName',artifact.file_name,
                  'mediaType',artifact.media_type,'source',artifact.source,'runtimeRevisionRef',revision.ref,
                  'attempt',revision.attempt,'generation',revision.generation)
        ) AND NOT EXISTS (
            SELECT 1 FROM control_plane.runtime_received_result_artifacts(child.organization_id,edge.source_node_id) received
            WHERE received=pin
        ) AND NOT EXISTS (
            SELECT 1 FROM control_plane.run_edges delivered
            JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=delivered.id
              AND receipt.result_snapshot IS NOT NULL
            CROSS JOIN LATERAL jsonb_array_elements(receipt.result_snapshot->'artifacts') received(value)
            WHERE delivered.organization_id=child.organization_id AND delivered.root_run_id=child.id
              AND delivered.type='CALLBACK_TO' AND received.value=pin
        ) THEN
            RAISE EXCEPTION 'callback result artifact binding is invalid';
        END IF;
    END LOOP;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER protect_callback_result_snapshot BEFORE INSERT OR UPDATE OR DELETE ON control_plane.callback_receipts
FOR EACH ROW WHEN (NOT control_plane.project_purge_authorized())
EXECUTE FUNCTION control_plane.protect_callback_result_snapshot();
REVOKE ALL ON FUNCTION control_plane.protect_callback_result_snapshot() FROM PUBLIC;

-- +goose StatementBegin
CREATE FUNCTION control_plane.runtime_file_coordinator(
    p_tenant uuid,p_actor uuid,p_project uuid,p_agent uuid,p_node uuid
) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.run_nodes node
        JOIN control_plane.runs run ON run.id=node.run_id AND run.organization_id=p_tenant
        JOIN control_plane.runs root ON root.id=node.root_run_id AND root.id=run.root_run_id
          AND root.organization_id=p_tenant AND root.project_id=p_project AND root.initiated_by=p_actor
        JOIN control_plane.workflow_versions workflow ON workflow.id=root.workflow_version_id
        JOIN control_plane.agents agent ON agent.id=node.agent_id AND agent.id=p_agent
          AND agent.organization_id=p_tenant AND agent.project_id=p_project
        WHERE node.id=p_node AND node.organization_id=p_tenant AND run.project_id=p_project
          AND run.initiated_by=p_actor AND node.type='AGENT_EXECUTION'
          AND node.workflow_step_key LIKE 'workflow.coordinator.%'
          AND workflow.spec->>'CoordinatorAgentRef'=agent.ref
          AND root.target_type='WORKFLOW' AND agent.enabled AND agent.state IN ('READY','RUNNING')
          AND 'platform.run.delegate'=ANY(agent.capabilities)
          AND node.state IN ('QUEUED','RUNNING')
          AND run.state IN ('QUEUED','RUNNING','WAITING_HUMAN')
          AND root.state IN ('QUEUED','RUNNING','WAITING_HUMAN')
    );
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE FUNCTION control_plane.runtime_received_result_artifacts(p_tenant uuid,p_node uuid)
RETURNS SETOF jsonb LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
    WITH RECURSIVE ancestors AS (
        SELECT node.id,node.root_run_id,node.run_id,node.agent_id
        FROM control_plane.run_nodes node WHERE node.id=p_node AND node.organization_id=p_tenant
        UNION
        SELECT previous.id,previous.root_run_id,previous.run_id,previous.agent_id
        FROM ancestors current_node
        JOIN control_plane.run_edges continuation ON continuation.organization_id=p_tenant
          AND continuation.root_run_id=current_node.root_run_id
          AND continuation.target_node_id=current_node.id AND continuation.type='CONTINUES'
        JOIN control_plane.run_nodes previous ON previous.id=continuation.source_node_id
          AND previous.organization_id=p_tenant AND previous.root_run_id=current_node.root_run_id
          AND previous.run_id=current_node.run_id AND previous.agent_id=current_node.agent_id
    )
    SELECT DISTINCT pin.value
    FROM ancestors ancestor
    JOIN control_plane.run_edges callback ON callback.organization_id=p_tenant
      AND callback.root_run_id=ancestor.root_run_id AND callback.target_node_id=ancestor.id
      AND callback.type='CALLBACK_TO'
    JOIN control_plane.callback_receipts receipt ON receipt.callback_edge_id=callback.id
      AND receipt.result_snapshot IS NOT NULL
    JOIN control_plane.runs child ON child.id=receipt.child_run_id AND child.organization_id=p_tenant
    JOIN control_plane.run_nodes source ON source.id=callback.source_node_id AND source.organization_id=p_tenant
    CROSS JOIN LATERAL jsonb_array_elements(receipt.result_snapshot->'artifacts') pin(value)
    WHERE receipt.result_snapshot->>'childRunRef'=child.ref
      AND receipt.result_snapshot->>'nodeRef'=source.ref
      AND receipt.result_snapshot->>'state' IN ('SUCCEEDED','FAILED','CANCELLED')
      AND ((source.run_id=child.id AND EXISTS (
          SELECT 1 FROM control_plane.run_edges delegation
          WHERE delegation.organization_id=p_tenant AND delegation.root_run_id=ancestor.root_run_id
            AND delegation.type='DELEGATED_TO' AND delegation.source_node_id=ancestor.id
            AND delegation.target_node_id=source.id)) OR EXISTS (
          SELECT 1 FROM control_plane.required_workflow_launches launch
          WHERE launch.organization_id=p_tenant AND launch.origin_root_run_id=ancestor.root_run_id
            AND launch.origin_node_id=ancestor.id AND launch.child_root_run_id=child.id
            AND launch.proxy_node_id=source.id AND launch.callback_edge_id=callback.id
            AND launch.state IN ('SUCCEEDED','FAILED','CANCELLED')));
$$;
-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.runtime_file_coordinator(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
REVOKE ALL ON FUNCTION control_plane.runtime_received_result_artifacts(uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.runtime_file_coordinator(uuid,uuid,uuid,uuid,uuid),
    control_plane.runtime_received_result_artifacts(uuid,uuid) TO control_plane_runtime;

-- +goose StatementBegin
CREATE FUNCTION control_plane.runtime_file_source_visible(
    p_tenant uuid,p_actor uuid,p_project uuid,p_agent uuid,p_artifact uuid,p_purpose text,p_source_revision text,p_node uuid
) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.catalog_access_targets file_target
        JOIN control_plane.artifacts file ON file.id=file_target.id AND file.organization_id=p_tenant
        JOIN control_plane.artifact_content content ON content.artifact_id=file.id
          AND content.digest=file.digest AND content.size_bytes=file.size_bytes
        JOIN control_plane.catalog_access_targets project_target
          ON project_target.organization_id=p_tenant AND project_target.kind='PROJECT' AND project_target.id=p_project
        LEFT JOIN control_plane.catalog_access_targets agent_target
          ON agent_target.organization_id=p_tenant AND agent_target.kind='AGENT' AND agent_target.id=p_agent
          AND agent_target.project_id=p_project
        JOIN control_plane.agents current_agent ON current_agent.id=p_agent AND current_agent.organization_id=p_tenant
        WHERE file_target.organization_id=p_tenant AND file_target.kind='ARTIFACT' AND file.id=p_artifact
          AND (file.project_id=p_project OR (p_purpose='WORKSPACE_INPUT' AND file.project_id IS NULL
              AND file.created_by=p_actor AND current_agent.system_key='system-assistant'))
          AND file.lifecycle_state='ACTIVE' AND file.scan_state='CLEAN'
          AND control_plane.catalog_resource_visible(p_tenant,p_actor,'project.view','PROJECT',project_target.id,
            project_target.project_id,project_target.owner_id,project_target.related_ids,statement_timestamp(),false)
          AND (control_plane.catalog_resource_visible(p_tenant,p_actor,'agent.view','AGENT',agent_target.id,
            agent_target.project_id,agent_target.owner_id,agent_target.related_ids,statement_timestamp(),false)
            OR (current_agent.system_key='system-assistant' AND current_agent.project_id IS NULL AND current_agent.state<>'ARCHIVED'))
          AND control_plane.catalog_resource_visible(p_tenant,p_actor,'artifact.view','ARTIFACT',file_target.id,
            file_target.project_id,file_target.owner_id,file_target.related_ids,statement_timestamp())
          AND control_plane.catalog_resource_visible(p_tenant,p_actor,'artifact.download','ARTIFACT',file_target.id,
            file_target.project_id,file_target.owner_id,file_target.related_ids,statement_timestamp())
          AND CASE p_purpose
            WHEN 'PROJECT' THEN file.run_id IS NULL AND 'platform.artifact.manage'=ANY(current_agent.capabilities)
            WHEN 'WORKSPACE_INPUT' THEN 'platform.artifact.manage'=ANY(current_agent.capabilities)
            WHEN 'RUN_RESULT' THEN file.source IN ('AGENT_RESULT','INTEGRATION_RESULT')
              AND (
                (control_plane.runtime_file_coordinator(p_tenant,p_actor,p_project,p_agent,p_node)
                 AND EXISTS (SELECT 1 FROM control_plane.runtime_received_result_artifacts(p_tenant,p_node) pin
                     WHERE pin->>'ref'=file.ref AND pin->>'digest'=file.digest
                       AND pin->'revision'=to_jsonb(file.revision) AND pin->'version'=to_jsonb(file.version)
                       AND pin->'sizeBytes'=to_jsonb(file.size_bytes) AND pin->>'fileName'=file.file_name
                       AND pin->>'mediaType'=file.media_type AND pin->>'source'=file.source))
                OR (NOT control_plane.runtime_file_coordinator(p_tenant,p_actor,p_project,p_agent,p_node)
                    AND 'platform.artifact.manage'=ANY(current_agent.capabilities)
                    AND NOT EXISTS (SELECT 1 FROM control_plane.run_nodes execution
                        WHERE execution.id=p_node AND execution.workflow_step_key LIKE 'workflow.coordinator.%'))
              )
              AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets run_target
                WHERE run_target.organization_id=p_tenant AND run_target.kind='RUN' AND run_target.id=file.run_id
                  AND control_plane.catalog_resource_visible(p_tenant,p_actor,'run.view','RUN',run_target.id,
                    run_target.project_id,run_target.owner_id,run_target.related_ids,statement_timestamp(),false))
            WHEN 'SKILL' THEN EXISTS (
              SELECT 1 FROM control_plane.agent_context_bindings binding
              JOIN control_plane.skill_bundle_revisions revision ON revision.id=binding.skill_revision_id
              JOIN control_plane.skill_bundles bundle ON bundle.id=revision.bundle_id AND bundle.id=binding.skill_bundle_id
              WHERE binding.organization_id=p_tenant AND binding.project_id=p_project AND binding.agent_id=p_agent AND binding.enabled
                AND revision.ref=p_source_revision AND bundle.project_id=p_project AND bundle.state='ACTIVE'
                AND control_plane.skill_revision_visible(p_tenant,p_actor,revision.id,statement_timestamp()))
            ELSE false END
    );
$$;

-- +goose StatementEnd
REVOKE ALL ON FUNCTION control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text,uuid) TO control_plane_runtime;
CREATE OR REPLACE VIEW control_plane.runtime_file_visible_entries WITH (security_invoker=true) AS
SELECT entry.id,entry.ref,entry.catalog_id,entry.artifact_id,entry.artifact_ref,entry.artifact_revision,
    entry.artifact_version,entry.artifact_digest,entry.file_name,entry.media_type,entry.size_bytes,
    entry.purpose,entry.project_ref,entry.run_ref,entry.source,entry.source_ref,entry.source_revision_ref,entry.entry_digest
FROM control_plane.runtime_file_catalog_entries entry
JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=entry.catalog_id AND catalog.frozen
JOIN control_plane.runtime_revisions revision ON revision.ref=catalog.runtime_revision_ref
JOIN control_plane.artifacts artifact ON artifact.id=entry.artifact_id AND artifact.ref=entry.artifact_ref
  AND artifact.revision=entry.artifact_revision AND artifact.version=entry.artifact_version AND artifact.digest=entry.artifact_digest
  AND artifact.size_bytes=entry.size_bytes AND artifact.media_type=entry.media_type AND artifact.source=entry.source
  AND (entry.purpose='SKILL' OR artifact.file_name=entry.file_name)
WHERE control_plane.runtime_file_source_visible(catalog.organization_id,catalog.actor_id,catalog.project_id,
    catalog.agent_id,entry.artifact_id,entry.purpose,entry.source_revision_ref,catalog.node_id)
  AND (entry.purpose<>'SKILL' OR EXISTS (
    SELECT 1 FROM jsonb_array_elements(revision.safe_snapshot #> '{contextSnapshot,skills}') skill
    JOIN control_plane.agent_context_bindings binding
      ON binding.organization_id=catalog.organization_id AND binding.agent_id=catalog.agent_id
      AND binding.ref=skill->>'binding_ref' AND to_jsonb(binding.version)=skill->'binding_version' AND binding.enabled
    WHERE skill->>'bundle_ref'=entry.source_ref AND skill->>'revision_ref'=entry.source_revision_ref));

DROP FUNCTION control_plane.runtime_file_source_visible(uuid,uuid,uuid,uuid,uuid,text,text);
RESET ROLE;

-- +goose Down
-- Forward-only: исторические receipts и RuntimeRevision не переписываются.
SELECT 1;
