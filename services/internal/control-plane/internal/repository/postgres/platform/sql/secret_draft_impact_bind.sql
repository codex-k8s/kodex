-- name: secret_draft_impact_bind :one
UPDATE control_plane.runtime_secret_draft_impact_plans p
SET operation_id=o.id FROM control_plane.runtime_secret_draft_operations o
WHERE p.ref=@plan_ref AND p.organization_id=o.organization_id AND p.actor_id=o.actor_id
AND o.ref=@operation_ref AND p.draft_id=o.draft_id AND p.state='PREPARED' AND p.operation_id IS NULL
AND p.draft_version=@draft_version AND p.secret_version=@secret_version AND p.expires_at>clock_timestamp()
RETURNING p.id::text;
