-- name: secret_draft_audit :exec
INSERT INTO control_plane.audit_events(ref,organization_id,project_id,actor_id,action,resource_kind,resource_ref,outcome,safe_summary,correlation_ref)
VALUES(@ref,@organization_id::uuid,@project_id::uuid,@actor_id::uuid,@action,'SECRET',@secret_ref,@outcome,
'i18n:RUNTIME_SECRET_DRAFT_CHANGED',@correlation_ref);
