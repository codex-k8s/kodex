-- name: platform__queries_connectionauthority_select_memberships_organization_id_subject_id :one
SELECT EXISTS(SELECT 1 FROM control_plane.memberships WHERE organization_id=$1::uuid AND subject_id=$2::uuid AND project_id IS NOT NULL AND active AND 'MANAGE_INTEGRATIONS'=ANY(permissions))
