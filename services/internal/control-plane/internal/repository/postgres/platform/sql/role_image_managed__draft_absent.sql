-- name: role_image_managed__draft_absent :one
SELECT NOT EXISTS (
    SELECT 1 FROM control_plane.managed_configuration_revisions
    WHERE configuration_set_id = $1::uuid AND state IN ('DRAFT', 'VALID', 'INVALID')
);
