-- name: email_credential_insert :exec
INSERT INTO control_plane.email_credential_descriptors(name,generation,organization_id,connection_id,kind,
    content_sha256,secret_ref,secret_uid,secret_resource_version,created_by,mailbox_owner_generation)
VALUES(@name,@generation,@organization_id::uuid,@connection_id::uuid,@kind,@content_sha256,
    @secret_ref,@secret_uid,@secret_resource_version,@actor_id::uuid,@owner_generation);
