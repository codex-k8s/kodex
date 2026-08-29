-- name: runtime_secret_audit :exec
WITH inserted_audit AS (
    INSERT INTO control_plane.audit_events
      (ref, organization_id, project_id, actor_id, action, resource_kind, resource_ref, outcome, safe_summary, correlation_ref)
    VALUES
      (@ref, @organization_id::uuid, @project_id::uuid, @actor_id::uuid, @action, 'SECRET', @secret_ref, @outcome, @summary, @correlation_ref)
    RETURNING id
)
INSERT INTO control_plane.runtime_secret_operation_audits (operation_id, audit_event_id)
SELECT @operation_id::uuid, id FROM inserted_audit;
