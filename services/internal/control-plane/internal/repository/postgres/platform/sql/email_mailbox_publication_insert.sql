-- name: email_mailbox_publication_insert :one
INSERT INTO control_plane.email_mailbox_publications(ref,revision,digest,document,organization_id,connection_id,connection_version,
    configuration_set_id,configuration_revision_id,created_by,kind)
VALUES(@ref,@revision,@digest,@document::jsonb,@organization_id::uuid,@connection_id::uuid,@connection_version,
    NULLIF(@configuration_set_id,'')::uuid,NULLIF(@configuration_revision_id,'')::uuid,@actor_id::uuid,@kind)
RETURNING created_at;
