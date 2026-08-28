-- name: configuration_changeconnection_insert_credential_revision :one
INSERT INTO control_plane.integration_credential_revisions(
	ref,organization_id,connection_id,revision,secret_ref,secret_uid,secret_resource_version,content_sha256,created_by
)
VALUES($1,$2::uuid,$3::uuid,1,$4,$5::uuid,$6,$7,$8::uuid)
RETURNING id::text,ref,revision,secret_ref,secret_uid::text,secret_resource_version,content_sha256,created_at
