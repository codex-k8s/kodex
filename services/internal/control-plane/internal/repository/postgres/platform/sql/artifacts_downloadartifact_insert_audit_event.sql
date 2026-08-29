-- name: artifacts_downloadartifact_insert_audit_event :exec
INSERT INTO control_plane.audit_events(
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
    @subject_id::uuid,
    @action,
    'ARTIFACT',
    @artifact_ref,
    'SUCCEEDED',
    @safe_summary,
    @correlation_ref
);
