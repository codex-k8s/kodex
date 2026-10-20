-- name: project_purge_readback :many
-- Read-only диагностика #1816. Только состояния и счётчики; без contents/credentials.
BEGIN TRANSACTION READ ONLY;
SET LOCAL statement_timeout = '10s';
SELECT jsonb_build_object('projectRef', receipt.project_ref, 'receiptState', receipt.state,
    'requestedAt', receipt.requested_at, 'purgedAt', receipt.purged_at,
    'deletedRows', receipt.deleted_rows, 'externalDigestPresent', receipt.objects_digest IS NOT NULL,
    'projectPresent', EXISTS (SELECT 1 FROM control_plane.projects project
        WHERE project.id=receipt.project_id AND project.organization_id=receipt.organization_id))
FROM control_plane.project_purge_receipts receipt
ORDER BY receipt.requested_at DESC,receipt.project_id LIMIT 16;
SELECT jsonb_build_object(
    'projectRef', project.ref, 'state', project.lifecycle,
    'version', project.version, 'receiptState', receipt.state,
    'requestedAt', receipt.requested_at,
    'archiveTasks', (SELECT COALESCE(jsonb_object_agg(state, total), '{}'::jsonb)
        FROM (SELECT task.state, count(*) total FROM control_plane.session_archive_tasks task
            WHERE task.organization_id=project.organization_id AND task.project_id=project.id GROUP BY task.state) grouped),
    'activeRuns', (SELECT count(*) FROM control_plane.runs run
        WHERE run.organization_id=project.organization_id AND run.project_id=project.id
            AND run.state IN ('QUEUED','RUNNING','CANCELLING')),
    'inventory', (SELECT COALESCE(jsonb_agg(jsonb_build_object('kind', kind, 'count', total)), '[]'::jsonb)
        FROM (SELECT item.kind, count(*) total
            FROM control_plane.project_purge_external_inventory(project.organization_id,project.id) item GROUP BY item.kind) grouped)
)
FROM control_plane.projects project
LEFT JOIN control_plane.project_purge_receipts receipt ON receipt.organization_id=project.organization_id AND receipt.project_id=project.id
WHERE project.lifecycle='PURGE_PENDING'
ORDER BY receipt.requested_at,project.id
LIMIT 16;
SELECT jsonb_build_object('projectRef', project.ref, 'inventoryClass', grouped.class, 'count', grouped.total)
FROM control_plane.projects project
JOIN control_plane.organizations organization ON organization.id=project.organization_id
CROSS JOIN LATERAL (
    SELECT class, count(*) total FROM (
        SELECT CASE WHEN item.kind='PVC' THEN 'PVC'
            WHEN item.target LIKE 'organizations/' || organization.ref || '/projects/' || project.ref || '/artifacts/%' THEN 'ARTIFACT'
            WHEN item.target LIKE 'session-archive/v1/' || organization.ref || '/' || project.ref || '/%' THEN 'ARCHIVE'
            ELSE 'UNEXPECTED_PREFIX' END class
        FROM control_plane.project_purge_external_inventory(project.organization_id,project.id) item
    ) classified GROUP BY class
) grouped
WHERE project.lifecycle='PURGE_PENDING'
ORDER BY project.id,grouped.class;
SELECT jsonb_build_object('projectRef', project.ref,
    'targetSHA256', encode(sha256(convert_to(item.target,'UTF8')),'hex'),
    'archivePrefix', starts_with(item.target,'session-archive/v1/'),
    'organizationRoot', starts_with(item.target,'organizations/'),
    'organizationMatches', split_part(item.target,'/',3)=organization.ref,
    'globalArchive', starts_with(item.target,'session-archive/v1/' || organization.ref || '/global/'),
    'projectRefPresent', position('/' || project.ref || '/' IN item.target)>0,
    'versionPresent', item.version<>'')
FROM control_plane.projects project
JOIN control_plane.organizations organization ON organization.id=project.organization_id
CROSS JOIN LATERAL control_plane.project_purge_external_inventory(project.organization_id,project.id) item
WHERE project.lifecycle='PURGE_PENDING' AND item.kind='OBJECT'
    AND NOT starts_with(item.target,'organizations/' || organization.ref || '/projects/' || project.ref || '/artifacts/')
    AND NOT starts_with(item.target,'session-archive/v1/' || organization.ref || '/' || project.ref || '/')
ORDER BY project.id,item.target,item.version
LIMIT 16;
SELECT jsonb_build_object('projectRef',project.ref,'preparedState',content.state,
    'cleanupAttempts',content.cleanup_attempts,'writerExpired',content.writer_deadline<=clock_timestamp(),
    'cleanupExpired',content.cleanup_deadline IS NULL OR content.cleanup_deadline<=clock_timestamp(),
    'availableExpired',content.available_until<=clock_timestamp(),
    'versionPresent',content.object_version<>'','etagPresent',content.object_etag<>'',
    'lastUpdatedAt',max(content.updated_at),'count',count(*))
FROM control_plane.prepared_content content
JOIN control_plane.projects project ON project.id=content.project_id AND project.organization_id=content.organization_id
WHERE project.lifecycle='PURGE_PENDING'
GROUP BY project.ref,content.state,content.cleanup_attempts,
    content.writer_deadline<=clock_timestamp(),content.cleanup_deadline IS NULL OR content.cleanup_deadline<=clock_timestamp(),
    content.available_until<=clock_timestamp(),content.object_version<>'',content.object_etag<>''
ORDER BY project.ref,content.state,content.cleanup_attempts LIMIT 32;
COMMIT;
