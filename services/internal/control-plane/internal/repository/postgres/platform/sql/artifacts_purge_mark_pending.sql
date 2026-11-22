-- name: artifacts_purge_mark_pending :exec
SELECT artifact_id FROM control_plane.artifact_manual_purge_issue(
 @artifact_id::uuid,@expected_version,@claim_owner,@claim_generation,@claim_until::timestamptz,
 @actor_id::uuid,@idempotency_key);
