-- name: insert_audit_event :exec
INSERT INTO control_plane.audit_events (
    ref,
    organization_id,
    project_id,
    actor_id,
    action,
    resource_kind,
    resource_ref,
    outcome,
    safe_summary,
    correlation_ref
)
VALUES (
    @audit_ref,
    @organization_id::uuid,
    NULLIF(@project_id, '')::uuid,
    @actor_id::uuid,
    CASE WHEN @manual::boolean THEN 'artifact.manual_purge_reconcile' ELSE 'artifact.retention_purge' END,
    'ARTIFACT',
    @artifact_ref,
    'SUCCEEDED',
    CASE WHEN @manual::boolean THEN 'i18n:ARTIFACT_PURGED' ELSE 'i18n:ARTIFACT_RETENTION_PURGED' END,
    @correlation_ref
);
