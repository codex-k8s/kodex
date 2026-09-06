-- name: runtime_files_capture_entries :exec
WITH candidates AS (
    SELECT artifact.id,artifact.ref,artifact.revision,artifact.version,artifact.digest,artifact.file_name,
        artifact.media_type,artifact.size_bytes,'PROJECT'::text AS purpose,artifact.source,
        artifact.ref AS source_ref,''::text AS source_revision_ref
    FROM control_plane.artifacts artifact
    JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=@catalog_id::uuid
      AND artifact.organization_id=catalog.organization_id AND artifact.project_id=catalog.project_id
    WHERE artifact.run_id IS NULL AND 'PROJECT'=ANY(catalog.purposes)
    UNION ALL
    SELECT artifact.id,artifact.ref,artifact.revision,artifact.version,artifact.digest,artifact.file_name,
        artifact.media_type,artifact.size_bytes,'RUN_RESULT',artifact.source,artifact.ref,''
    FROM control_plane.artifacts artifact
    JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=@catalog_id::uuid
      AND artifact.organization_id=catalog.organization_id AND artifact.project_id=catalog.project_id
    WHERE artifact.source IN ('AGENT_RESULT','INTEGRATION_RESULT') AND 'RUN_RESULT'=ANY(catalog.purposes)
    UNION ALL
    SELECT artifact.id,artifact.ref,artifact.revision,(item->>'version')::bigint,artifact.digest,artifact.file_name,
        artifact.media_type,artifact.size_bytes,'WORKSPACE_INPUT',artifact.source,artifact.ref,''
    FROM jsonb_array_elements(@inputs::jsonb) item
    JOIN control_plane.artifacts artifact ON artifact.ref=item->>'ref' AND artifact.digest=item->>'digest'
      AND to_jsonb(artifact.revision)=item->'revision' AND to_jsonb(artifact.size_bytes)=item->'sizeBytes'
      AND artifact.file_name=item->>'fileName' AND artifact.media_type=item->>'mediaType' AND artifact.source=item->>'source'
    UNION ALL
    SELECT artifact.id,artifact.ref,artifact.revision,artifact.version,artifact.digest,file->>'path',
        artifact.media_type,artifact.size_bytes,'SKILL',artifact.source,skill->>'bundle_ref',skill->>'revision_ref'
    FROM jsonb_array_elements(@skills::jsonb) skill
    CROSS JOIN LATERAL jsonb_array_elements(skill->'files') file
    JOIN control_plane.artifacts artifact ON artifact.ref=file->>'artifact_ref' AND artifact.digest=file->>'digest'
      AND to_jsonb(artifact.revision)=file->'artifact_revision' AND to_jsonb(artifact.size_bytes)=file->'size_bytes'
), eligible AS (
    SELECT DISTINCT candidates.id,candidates.ref,candidates.revision,candidates.version,candidates.digest,
        candidates.file_name,candidates.media_type,candidates.size_bytes,candidates.purpose,candidates.source,
        candidates.source_ref,candidates.source_revision_ref,
        catalog.id AS catalog_id,project.ref AS project_ref,COALESCE(run.ref,'') AS run_ref
    FROM candidates JOIN control_plane.runtime_file_catalogs catalog ON catalog.id=@catalog_id::uuid
    JOIN control_plane.artifacts artifact ON artifact.id=candidates.id
    JOIN control_plane.projects project ON project.id=catalog.project_id
    LEFT JOIN control_plane.runs run ON run.id=artifact.run_id
    WHERE candidates.purpose=ANY(catalog.purposes)
      AND control_plane.runtime_file_source_visible(catalog.organization_id,catalog.actor_id,catalog.project_id,
          catalog.agent_id,candidates.id,candidates.purpose,candidates.source_revision_ref)
)
INSERT INTO control_plane.runtime_file_catalog_entries(ref,catalog_id,artifact_id,artifact_ref,artifact_revision,
    artifact_version,artifact_digest,file_name,media_type,size_bytes,purpose,project_ref,run_ref,source,source_ref,source_revision_ref,entry_digest)
SELECT 'vfe_'||replace(gen_random_uuid()::text,'-',''),catalog_id,id,ref,revision,version,digest,file_name,media_type,size_bytes,
    purpose,project_ref,run_ref,source,source_ref,source_revision_ref,
    public.digest(convert_to((to_jsonb(eligible)-ARRAY['id','catalog_id'])::text,'UTF8'),'sha256')
FROM eligible;
