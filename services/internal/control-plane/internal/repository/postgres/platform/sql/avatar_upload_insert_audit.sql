-- name: avatar_upload_insert_audit :exec
INSERT INTO control_plane.audit_events (
    ref, organization_id, project_id, actor_id, action, resource_kind,
    resource_ref, outcome, safe_summary, correlation_ref
)
VALUES (
    @audit_ref, @organization_id::uuid, @project_id::uuid, @actor_id::uuid,
    'agent.avatar.upload', 'AGENT', @agent_ref, 'SUCCEEDED',
    'i18n:AGENT_AVATAR_UPDATED', @correlation_ref
);
