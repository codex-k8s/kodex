-- +goose Up
SET ROLE control_plane_owner;
SET LOCAL search_path=pg_catalog;
SET LOCAL lock_timeout='8s';
SELECT pg_advisory_xact_lock(hashtextextended('kodex.workspace.backup-acl-maintenance.v1',0));

-- Строго тот же предикат Kubernetes FILE: новая версия восстанавливает exact SERVER DAG
-- без выдачи общих прав на файлы. Функция и grants сохраняют существующий OID.
-- +goose StatementBegin
DO $predecessor$
BEGIN
 IF (SELECT md5(prosrc) FROM pg_proc WHERE oid=
      'control_plane.runtime_file_source_visible_revision(uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid)'::regprocedure)
    IS DISTINCT FROM '1fa42c481cd3935099281855abf46c96' THEN
   RAISE EXCEPTION 'workflow file provenance predecessor changed';
 END IF;
END $predecessor$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.runtime_file_source_visible_revision(
    p_tenant uuid,p_actor uuid,p_project uuid,p_agent uuid,p_artifact uuid,p_purpose text,p_source_revision text,p_node uuid,p_owner_entry uuid
) RETURNS boolean LANGUAGE sql STABLE SECURITY INVOKER
SET search_path=pg_catalog,control_plane AS $$
    SELECT EXISTS (
        SELECT 1 FROM control_plane.catalog_access_targets file_target
        JOIN control_plane.artifacts current_file ON current_file.id=file_target.id AND current_file.organization_id=p_tenant
          AND current_file.lifecycle_state='ACTIVE' AND current_file.scan_state IN ('CLEAN','NOT_SCANNED')
        LEFT JOIN control_plane.runtime_file_catalog_entries pinned ON pinned.id=p_owner_entry
          AND pinned.artifact_id=file_target.id AND pinned.purpose=p_purpose AND pinned.source_revision_ref=p_source_revision
        LEFT JOIN control_plane.runtime_file_catalogs original_catalog ON original_catalog.id=pinned.catalog_id
          AND original_catalog.organization_id=p_tenant AND original_catalog.actor_id=p_actor
          AND original_catalog.project_id=p_project AND original_catalog.agent_id=p_agent AND original_catalog.node_id=p_node
          AND original_catalog.frozen AND p_purpose=ANY(original_catalog.purposes)
        JOIN control_plane.artifact_history file ON file.id=file_target.id AND file.organization_id=p_tenant
          AND ((p_owner_entry IS NULL AND (file.revision_id=current_file.revision_id OR
               (p_purpose='RUN_RESULT' AND control_plane.runtime_workflow_input_visible(p_tenant,p_node,file.revision_id))))
           OR(p_owner_entry IS NOT NULL AND original_catalog.id IS NOT NULL AND p_purpose IN('PROJECT','WORKSPACE_INPUT','RUN_RESULT')
            AND file.revision=pinned.artifact_revision AND file.digest=pinned.artifact_digest
            AND file.size_bytes=pinned.size_bytes AND file.media_type=pinned.media_type AND file.source=pinned.source
            AND pinned.entry_digest=control_plane.runtime_file_entry_digest(pinned)))
        JOIN control_plane.artifact_revision_content content ON content.revision_id=file.revision_id
          AND content.digest=file.digest AND content.size_bytes=file.size_bytes
        JOIN control_plane.catalog_access_targets project_target
          ON project_target.organization_id=p_tenant AND project_target.kind='PROJECT' AND project_target.id=p_project
        LEFT JOIN control_plane.catalog_access_targets agent_target
          ON agent_target.organization_id=p_tenant AND agent_target.kind='AGENT' AND agent_target.id=p_agent
          AND agent_target.project_id=p_project
        JOIN control_plane.agents current_agent ON current_agent.id=p_agent AND current_agent.organization_id=p_tenant
        WHERE file_target.organization_id=p_tenant AND file_target.kind='ARTIFACT' AND file.id=p_artifact
          AND control_plane.kubernetes_capture_runtime_visible(p_tenant,p_actor,p_artifact,p_node,p_purpose)
          AND (file.project_id=p_project OR (p_purpose='WORKSPACE_INPUT' AND file.project_id IS NULL
              AND file.created_by=p_actor AND current_agent.system_key='system-assistant'))
          AND file.lifecycle_state='ACTIVE' AND file.scan_state IN ('CLEAN','NOT_SCANNED')
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
              AND CASE WHEN EXISTS (
                SELECT 1 FROM control_plane.run_nodes execution
                JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=p_tenant
                JOIN control_plane.workflow_versions version ON version.id=root.workflow_version_id
                  AND version.organization_id=p_tenant
                WHERE execution.id=p_node AND execution.organization_id=p_tenant
                  AND version.spec->>'ExecutionMode'='SERVER_DAG_V1'
              ) THEN EXISTS (
                SELECT 1 FROM control_plane.run_nodes execution
                JOIN control_plane.runs root ON root.id=execution.root_run_id AND root.organization_id=p_tenant
                WHERE execution.id=p_node AND execution.organization_id=p_tenant AND execution.agent_id=p_agent
                  AND root.project_id=p_project AND root.initiated_by=p_actor
                  AND control_plane.runtime_workflow_input_visible(p_tenant,p_node,file.revision_id)
              ) ELSE (
                (control_plane.runtime_file_coordinator(p_tenant,p_actor,p_project,p_agent,p_node)
                 AND EXISTS (SELECT 1 FROM control_plane.runtime_received_result_artifacts(p_tenant,p_node) pin
                     WHERE pin->>'ref'=file.ref AND pin->>'digest'=file.digest
                       AND pin->'revision'=to_jsonb(file.revision) AND pin->'version'=to_jsonb(coalesce(pinned.artifact_version,file.version))
                       AND pin->'sizeBytes'=to_jsonb(file.size_bytes) AND pin->>'fileName'=file.file_name
                       AND pin->>'mediaType'=file.media_type AND pin->>'source'=file.source))
                OR (NOT control_plane.runtime_file_coordinator(p_tenant,p_actor,p_project,p_agent,p_node)
                    AND 'platform.artifact.manage'=ANY(current_agent.capabilities)
                    AND NOT EXISTS (SELECT 1 FROM control_plane.run_nodes execution
                        WHERE execution.id=p_node AND execution.workflow_step_key LIKE 'workflow.coordinator.%'))
              ) END
              AND EXISTS (SELECT 1 FROM control_plane.catalog_access_targets run_target
                WHERE run_target.organization_id=p_tenant AND run_target.kind='RUN' AND run_target.id=file.run_id
                  AND control_plane.catalog_resource_visible(p_tenant,p_actor,'run.view','RUN',run_target.id,
                    run_target.project_id,run_target.owner_id,run_target.related_ids,statement_timestamp(),false))
            WHEN 'SKILL' THEN file.scan_state='CLEAN' AND EXISTS (
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
SET LOCAL search_path=public;
RESET ROLE;

-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN
 RAISE EXCEPTION 'workflow file provenance migration is forward-only';
END $$;
-- +goose StatementEnd
