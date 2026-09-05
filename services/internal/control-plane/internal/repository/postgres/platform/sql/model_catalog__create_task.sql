-- name: model_catalog__create_task :one
INSERT INTO control_plane.provider_model_catalog_tasks
    (ref, organization_id, provider_account_id, account_version, provider_credential_revision_id, authorization_method)
VALUES ($1, $2::uuid, $3::uuid, $4, $5::uuid, $6)
RETURNING id::text, clock_timestamp() + interval '15 seconds';
