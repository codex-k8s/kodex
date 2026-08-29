-- name: configuration_hydrateassistantoperation_select_project :one
SELECT name,purpose,language,version
FROM control_plane.projects
WHERE organization_id=$1::uuid AND ref=$2 AND lifecycle='ACTIVE'
