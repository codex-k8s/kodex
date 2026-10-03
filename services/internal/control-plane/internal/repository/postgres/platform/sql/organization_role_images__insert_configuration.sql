-- name: organization_role_images__insert_configuration :one
INSERT INTO control_plane.managed_configuration_sets
    (ref, organization_id, project_id, kind, name, managed_by, source, source_revision, created_by)
VALUES ($1, $2::uuid, NULL, 'ROLE_IMAGE', $3, 'UI', 'control-center', '', $4::uuid)
RETURNING id::text, ref, '', '', kind, name, managed_by, source, source_revision,
          version, updated_at, '', archived, copy_provenance;
