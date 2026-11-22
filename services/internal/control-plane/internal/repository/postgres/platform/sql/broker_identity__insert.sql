-- name: broker_identity__insert :one
INSERT INTO control_plane.integration_broker_identity_revisions(
 ref,organization_id,connection_id,binding_id,subject_id,credential_policy,purpose,target_ref,revision)
SELECT @ref,@organization_id::uuid,@connection_id::uuid,NULLIF(@binding_id,'')::uuid,@subject_id::uuid,@policy,@purpose,@target_ref,
 coalesce(max(revision),0)+1
FROM control_plane.integration_broker_identity_revisions
WHERE organization_id=@organization_id::uuid AND connection_id=@connection_id::uuid
 AND subject_id=@subject_id::uuid AND purpose=@purpose
HAVING coalesce(max(revision),0)<9007199254740991
RETURNING id::text,ref,target_ref,purpose,revision,created_at;
