-- Immutable subject-scoped result до штатного переноса общей сессии.
WITH selected AS (
    SELECT run.id,run.organization_id,run.project_id,run.initiated_by,organization.ref organization_ref,actor.ref actor_ref
    FROM control_plane.runs run
    JOIN control_plane.sessions session ON session.id=run.session_id AND session.organization_id=run.organization_id
    JOIN control_plane.organizations organization ON organization.id=run.organization_id
    JOIN control_plane.subjects actor ON actor.id=run.initiated_by AND actor.organization_id=run.organization_id
    WHERE session.ref=$1
    ORDER BY run.created_at DESC LIMIT 1
), artifact AS (
    INSERT INTO control_plane.artifacts
        (ref,organization_id,project_id,run_id,file_name,media_type,size_bytes,digest,source,scan_state,object_receipt_ref,preview_state,revision,created_by)
    SELECT 'art_purge_moved_fixture',organization_id,project_id,id,'result.txt','text/plain',1,$3,
        'AGENT_RESULT','QUARANTINED','fixture-purge-moved','BLOCKED',1,initiated_by FROM selected
    RETURNING id,digest,size_bytes
), content AS (
    INSERT INTO control_plane.artifact_content(artifact_id,object_key,object_version,object_etag,digest,size_bytes,antivirus_report)
    SELECT artifact.id,'organizations/' || selected.organization_ref || '/subjects/' || selected.actor_ref || '/artifacts/art_purge_moved_fixture/' || split_part($3,':',2),
        'fixture-artifact-version','fixture-etag',artifact.digest,artifact.size_bytes,$2
    FROM artifact CROSS JOIN selected
    RETURNING object_key
)
SELECT object_key FROM content;
