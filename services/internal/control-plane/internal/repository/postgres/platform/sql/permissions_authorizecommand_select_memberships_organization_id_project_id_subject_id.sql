-- name: permissions_authorizecommand_select_memberships_organization_id_project_id_subject_id :one
SELECT EXISTS(SELECT 1 FROM control_plane.memberships WHERE organization_id=$1::uuid AND project_id=$2::uuid AND subject_id=$3::uuid AND active AND $4=ANY(permissions))
