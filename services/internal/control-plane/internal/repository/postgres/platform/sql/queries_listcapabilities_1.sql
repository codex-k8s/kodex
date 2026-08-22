-- name: platform__queries_listcapabilities_1 :many
SELECT stable_key,name,description,risk FROM control_plane.platform_capabilities WHERE enabled ORDER BY name
