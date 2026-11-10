-- +goose Up
SET ROLE control_plane_owner;

-- Явный режим обычных файлов не является антивирусной аттестацией.
-- Applied history не меняется; новые NOT_SCANNED строки требуют отдельный receipt.
ALTER TABLE control_plane.artifact_revisions DROP CONSTRAINT artifact_revisions_scan_state_check;
ALTER TABLE control_plane.artifact_revisions ADD CONSTRAINT artifact_revisions_scan_state_check
 CHECK (scan_state IN ('PENDING','SCANNING','CLEAN','NOT_SCANNED','QUARANTINED','FAILED'));
ALTER TABLE control_plane.prepared_content DROP CONSTRAINT prepared_content_scan_state_check;
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_content_scan_state_check
 CHECK (scan_state IN ('CLEAN','NOT_SCANNED'));

-- Перевалидация CHECK после изменения используемой immutable function обязательна.
ALTER TABLE control_plane.artifact_revision_content DROP CONSTRAINT artifact_antivirus_receipt_shape;
ALTER TABLE control_plane.prepared_content DROP CONSTRAINT prepared_antivirus_receipt_shape;
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.artifact_scan_receipt_valid(report jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE PARALLEL SAFE SET search_path=pg_catalog AS $$
 SELECT COALESCE(jsonb_typeof(report)='object' AND (
   (report ?& ARRAY['engine','policy','scannedAt','verdict']
    AND report-ARRAY['engine','policy','scannedAt','verdict']='{}'::jsonb
    AND report->>'engine' ~ '^ClamAV [^\r\n]{1,249}$'
    AND report->>'policy'='clamav-instream-v1'
    AND report->>'scannedAt' ~ '^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$'
    AND report->>'verdict' IN ('CLEAN','QUARANTINED'))
   OR
   (report ?& ARRAY['engine','policy','checkedAt','verdict']
    AND report-ARRAY['engine','policy','checkedAt','verdict']='{}'::jsonb
    AND report->>'engine'='format-policy-v1'
    AND report->>'policy'='artifact-format-only-v1'
    AND report->>'checkedAt' ~ '^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d{1,9})?Z$'
    AND report->>'verdict'='NOT_SCANNED')
 ),false);
$$;
-- +goose StatementEnd
ALTER TABLE control_plane.artifact_revision_content ADD CONSTRAINT artifact_antivirus_receipt_shape
 CHECK (antivirus_report IS NULL OR control_plane.artifact_scan_receipt_valid(antivirus_report));
ALTER TABLE control_plane.prepared_content ADD CONSTRAINT prepared_antivirus_receipt_shape
 CHECK (antivirus_report IS NULL OR control_plane.artifact_scan_receipt_valid(antivirus_report));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.require_artifact_antivirus_receipt() RETURNS trigger
LANGUAGE plpgsql SET search_path=pg_catalog AS $$
DECLARE selected_scan text;
BEGIN
 IF NOT control_plane.artifact_scan_receipt_valid(NEW.antivirus_report) THEN
   RAISE EXCEPTION 'artifact admission receipt is required';
 END IF;
 IF TG_TABLE_NAME='artifact_revision_content' THEN
   SELECT scan_state INTO STRICT selected_scan FROM control_plane.artifact_revisions
   WHERE id=NEW.revision_id;
 ELSE
   selected_scan:=NEW.scan_state;
 END IF;
 IF (selected_scan='CLEAN' AND NEW.antivirus_report->>'verdict'<>'CLEAN')
 OR (selected_scan='NOT_SCANNED' AND NEW.antivirus_report->>'verdict'<>'NOT_SCANNED') THEN
   RAISE EXCEPTION 'artifact admission verdict binding is invalid';
 END IF;
 RETURN NEW;
END;
$$;
-- +goose StatementEnd

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.artifact_effective_scan(p_revision_id uuid) RETURNS text
LANGUAGE sql STABLE PARALLEL SAFE SET search_path=pg_catalog AS $$
 SELECT CASE WHEN revision.scan_state IN ('CLEAN','NOT_SCANNED') THEN
   CASE WHEN control_plane.artifact_scan_receipt_valid(content.antivirus_report)
     AND content.antivirus_report->>'verdict'=revision.scan_state
     AND content.digest=revision.digest AND content.size_bytes=revision.size_bytes
     THEN revision.scan_state ELSE 'QUARANTINED' END
   ELSE revision.scan_state END
 FROM control_plane.artifact_revisions revision
 LEFT JOIN control_plane.artifact_revision_content content ON content.revision_id=revision.id
 WHERE revision.id=p_revision_id;
$$;
-- +goose StatementEnd

CREATE OR REPLACE VIEW control_plane.artifact_history WITH (security_invoker=true) AS
SELECT head.id,head.ref,head.organization_id,head.project_id,head.run_id,head.node_id,
       revision.file_name,revision.media_type,revision.size_bytes,revision.digest,
       revision.source,control_plane.artifact_effective_scan(revision.id) AS scan_state,
       revision.object_receipt_ref,
       CASE WHEN control_plane.artifact_effective_scan(revision.id) IN ('CLEAN','NOT_SCANNED') THEN revision.preview_state ELSE 'BLOCKED' END AS preview_state,
       revision.revision,head.version,head.created_by,revision.created_at,
       head.lifecycle_state,head.deleted_at,head.purge_after,head.purged_at,
       head.retention_claim_owner,head.retention_claim_generation,head.retention_claim_expires_at,
       revision.id AS revision_id,revision.ref AS revision_ref
FROM control_plane.artifact_heads head
JOIN control_plane.artifact_revisions revision ON revision.artifact_id=head.id;

CREATE OR REPLACE VIEW control_plane.artifacts WITH (security_invoker=true) AS
SELECT head.id,head.ref,head.organization_id,head.project_id,head.run_id,head.node_id,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'purged-' || head.ref ELSE revision.file_name END AS file_name,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'application/octet-stream' ELSE revision.media_type END AS media_type,
       CASE WHEN head.lifecycle_state='PURGED' THEN 0::bigint ELSE revision.size_bytes END AS size_bytes,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'sha256:' || repeat('0',64) ELSE revision.digest END AS digest,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'CONTROL_CENTER' ELSE revision.source END AS source,
       CASE WHEN head.lifecycle_state='PURGED' THEN 'FAILED' ELSE control_plane.artifact_effective_scan(revision.id) END AS scan_state,
       CASE WHEN head.lifecycle_state='PURGED' THEN '' ELSE revision.object_receipt_ref END AS object_receipt_ref,
       CASE WHEN head.lifecycle_state='PURGED' OR COALESCE(control_plane.artifact_effective_scan(revision.id),'') NOT IN ('CLEAN','NOT_SCANNED')
         THEN 'BLOCKED' ELSE revision.preview_state END AS preview_state,
       CASE WHEN head.lifecycle_state='PURGED' THEN 1::bigint ELSE revision.revision END AS revision,
       head.version,head.created_by,head.created_at,
       head.lifecycle_state,head.deleted_at,head.purge_after,head.purged_at,
       head.retention_claim_owner,head.retention_claim_generation,head.retention_claim_expires_at,
       revision.id AS revision_id,COALESCE(revision.ref,'') AS current_revision_ref
FROM control_plane.artifact_heads head
LEFT JOIN control_plane.artifact_revisions revision ON revision.id=head.current_revision_id AND revision.artifact_id=head.id;

-- Обычные runtime файлы и knowledge сохраняют прежний exact owner/revision proof.
-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.runtime_file_source_visible(
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

CREATE OR REPLACE VIEW control_plane.runtime_file_visible_entries WITH (security_invoker=true) AS
SELECT entry.id,entry.ref,entry.catalog_id,entry.artifact_id,entry.artifact_ref,entry.artifact_revision,
    entry.artifact_version,entry.artifact_digest,entry.file_name,entry.media_type,entry.size_bytes,
    entry.purpose,entry.project_ref,entry.run_ref,entry.source,entry.source_ref,entry.source_revision_ref,entry.entry_digest
FROM control_plane.runtime_file_catalog_entries entry
JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=entry.catalog_id AND catalog.frozen
JOIN control_plane.runtime_revisions revision ON revision.ref=catalog.runtime_revision_ref
JOIN control_plane.artifact_history artifact ON artifact.id=entry.artifact_id AND artifact.ref=entry.artifact_ref
  AND artifact.revision=entry.artifact_revision AND artifact.digest=entry.artifact_digest
  AND artifact.size_bytes=entry.size_bytes AND artifact.media_type=entry.media_type AND artifact.source=entry.source
  AND (entry.purpose='SKILL' OR artifact.file_name=entry.file_name)
  AND artifact.scan_state IN ('CLEAN','NOT_SCANNED')
  AND (entry.purpose<>'SKILL' OR artifact.scan_state='CLEAN')
JOIN control_plane.artifact_revision_content content ON content.revision_id=artifact.revision_id
  AND content.digest=entry.artifact_digest AND content.size_bytes=entry.size_bytes
WHERE control_plane.runtime_file_source_visible(catalog.organization_id,catalog.actor_id,catalog.project_id,
    catalog.agent_id,entry.artifact_id,entry.purpose,entry.source_revision_ref,catalog.node_id)
  AND entry.entry_digest=control_plane.runtime_file_entry_digest(entry)
  AND (entry.purpose<>'SKILL' OR EXISTS (
    SELECT 1 FROM jsonb_array_elements(revision.safe_snapshot #> '{contextSnapshot,skills}') skill
    JOIN control_plane.agent_context_bindings binding
      ON binding.organization_id=catalog.organization_id AND binding.agent_id=catalog.agent_id
      AND binding.ref=skill->>'binding_ref' AND to_jsonb(binding.version)=skill->'binding_version' AND binding.enabled
    WHERE skill->>'bundle_ref'=entry.source_ref AND skill->>'revision_ref'=entry.source_revision_ref));

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION control_plane.assistant_context_projection_v3(
 tenant uuid,actor uuid,signed_project uuid,requested_context_kind text,requested_context_ref text,
 evaluated_at timestamptz,conversation_project uuid DEFAULT NULL,helper_scope text DEFAULT 'SYSTEM'
) RETURNS TABLE(project_id uuid,project_ref text,entity_name text,entity_version bigint,allowed_operations text[])
LANGUAGE sql STABLE SECURITY INVOKER SET search_path=pg_catalog,control_plane AS $$
 SELECT projection.project_id,projection.project_ref,projection.entity_name,projection.entity_version,
  projection.allowed_operations || CASE WHEN conversation_project IS NOT NULL
   AND (helper_scope='PROJECT' OR (helper_scope='SYSTEM' AND requested_context_kind IN ('PROJECT','FILE')))
   AND (projection.project_id=conversation_project OR helper_scope='PROJECT' AND projection.project_id IS NULL)
   AND EXISTS(SELECT 1 FROM control_plane.artifacts artifact
    WHERE artifact.organization_id=tenant AND artifact.project_id=conversation_project AND artifact.lifecycle_state='ACTIVE' AND artifact.scan_state IN ('CLEAN','NOT_SCANNED')
     AND control_plane.catalog_resource_visible(tenant,actor,'artifact.view','ARTIFACT',artifact.id,artifact.project_id,artifact.created_by,'{}'::jsonb,evaluated_at)
     AND control_plane.catalog_resource_visible(tenant,actor,'artifact.download','ARTIFACT',artifact.id,artifact.project_id,artifact.created_by,'{}'::jsonb,evaluated_at)
     AND control_plane.catalog_resource_visible(tenant,actor,'artifact.revision.create','ARTIFACT',artifact.id,artifact.project_id,artifact.created_by,'{}'::jsonb,evaluated_at))
   THEN ARRAY['CREATE_PROJECT_FILE_REVISION'] ELSE '{}'::text[] END
 FROM control_plane.assistant_context_projection_v2(tenant,actor,signed_project,requested_context_kind,requested_context_ref,evaluated_at,conversation_project) projection;
$$;
-- +goose StatementEnd

RESET ROLE;

-- +goose Down
-- Forward-only: старый binary не должен обслуживать новые NOT_SCANNED receipts.
SELECT 1;
