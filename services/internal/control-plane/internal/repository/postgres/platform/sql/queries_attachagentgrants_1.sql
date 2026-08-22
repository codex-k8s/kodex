-- name: platform__queries_attachagentgrants_1 :many
SELECT g.ref FROM control_plane.integration_grants g WHERE g.organization_id=$1::uuid AND g.target_kind='AGENT' AND g.target_ref=$2 AND g.enabled ORDER BY g.ref
