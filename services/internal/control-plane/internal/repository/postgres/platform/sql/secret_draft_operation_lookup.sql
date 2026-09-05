-- name: secret_draft_operation_lookup :one
SELECT o.ref,d.ref FROM control_plane.runtime_secret_draft_operations o
JOIN control_plane.runtime_secret_drafts d ON d.id=o.draft_id
WHERE o.organization_id=@organization_id::uuid AND
((@operation_ref<>'' AND o.ref=@operation_ref)
OR (@token_digest<>'' AND o.token_digest=@token_digest)
OR (@idempotency_key<>'' AND o.actor_id=@actor_id::uuid AND o.kind=@kind AND o.idempotency_key=@idempotency_key));
