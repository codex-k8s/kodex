-- name: provider_cleanup_produced_descriptor_conflict :one
SELECT EXISTS (
 SELECT 1 FROM control_plane.provider_credential_revisions revision
 WHERE revision.secret_uid=@secret_uid::uuid
   AND (revision.organization_id<>@organization_id::uuid
     OR revision.provider_account_id<>@account_id::uuid
     OR revision.secret_name<>@secret_name
     OR (revision.secret_resource_version=@secret_resource_version AND revision.content_sha256<>@content_sha256))
);
