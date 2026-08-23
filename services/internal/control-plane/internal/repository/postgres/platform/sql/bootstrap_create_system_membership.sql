-- name: bootstrap_create_system_membership :exec
INSERT INTO control_plane.memberships
    (ref, organization_id, subject_id, role, permissions)
VALUES ('mem_system_platform', $1::uuid, $2::uuid, 'OPERATOR', $3)
