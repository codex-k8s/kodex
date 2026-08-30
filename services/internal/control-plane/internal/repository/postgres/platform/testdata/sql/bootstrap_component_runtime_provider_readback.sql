-- name: bootstrap_component_runtime_provider_readback :one
SELECT revision.provider_account_id::text,
       revision.provider_credential_revision_id::text
FROM control_plane.runtime_revisions revision
WHERE revision.ref = $1;
