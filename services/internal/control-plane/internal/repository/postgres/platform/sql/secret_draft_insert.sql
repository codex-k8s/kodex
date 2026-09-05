-- name: secret_draft_insert :exec
INSERT INTO control_plane.runtime_secret_drafts(ref,organization_id,secret_id,owner_actor_id,state,expected_content_sha256,staged_namespace)
VALUES(@ref,@organization_id::uuid,@secret_id::uuid,@actor_id::uuid,'PREPARING',@content_sha256,@staged_namespace);
