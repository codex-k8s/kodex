-- name: secret_draft_impact_insert :one
INSERT INTO control_plane.runtime_secret_draft_impact_plans
(ref,organization_id,actor_id,draft_id,draft_version,secret_version,source_revision,credential_revision,digest,idempotency_key,intent_digest)
VALUES(@ref,@organization_id::uuid,@actor_id::uuid,@draft_id::uuid,@draft_version,@secret_version,@source_revision,@credential_revision,@digest,@idempotency_key,@intent_digest)
RETURNING id::text,expires_at;
