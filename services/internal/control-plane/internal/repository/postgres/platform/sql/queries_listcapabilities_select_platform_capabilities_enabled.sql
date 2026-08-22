-- name: platform__queries_listcapabilities_select_platform_capabilities_enabled :many
SELECT stable_key,name,description,risk FROM control_plane.platform_capabilities WHERE enabled ORDER BY name
