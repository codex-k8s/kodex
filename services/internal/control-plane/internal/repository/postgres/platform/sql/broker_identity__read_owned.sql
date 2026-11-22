-- name: broker_identity__read_owned :one
SELECT ref,target_ref,purpose,revision,created_at
FROM control_plane.integration_broker_identity_revisions
WHERE organization_id=@organization_id::uuid AND id=@identity_id::uuid
 AND connection_id=@connection_id::uuid AND subject_id=@subject_id::uuid
 AND binding_id IS NOT DISTINCT FROM NULLIF(@binding_id,'')::uuid
 AND credential_policy=@policy AND purpose=@purpose AND target_ref=@target_ref;
