-- name: platform__commands_changemembership_update_memberships_active_version_updated_at :one
UPDATE control_plane.memberships SET active=false,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND project_id=$3::uuid AND version=$4 AND subject_id<>$5::uuid RETURNING ref,role,permissions,active,version
