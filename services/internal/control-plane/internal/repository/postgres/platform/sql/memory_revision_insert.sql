-- name: memory_revision_insert :one
INSERT INTO control_plane.memory_record_revisions
(ref,organization_id,record_id,revision,title,summary,digest,parent_revision_id,source_run_id,source_run_version,source_kind,retention_until,created_by)
SELECT @revision_ref,@organization_id::uuid,record.id,
       COALESCE((SELECT max(revision) FROM control_plane.memory_record_revisions WHERE record_id=record.id),0)+1,
       @title,@summary,@digest,record.current_revision_id,run.id,run.version,'USER_SUMMARY',@retention_until,@actor_id::uuid
FROM control_plane.memory_records record
LEFT JOIN control_plane.runs run ON run.organization_id=record.organization_id AND run.project_id=record.project_id AND run.ref=@source_run_ref
WHERE record.organization_id=@organization_id::uuid AND record.id=@record_id::uuid
  AND (@source_run_ref='' OR run.id IS NOT NULL)
RETURNING id::text;
