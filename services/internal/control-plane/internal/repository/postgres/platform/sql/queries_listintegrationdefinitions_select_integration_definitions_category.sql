-- name: platform__queries_listintegrationdefinitions_select_integration_definitions_category :many
SELECT stable_key,name,description,category,optional,enabled,capabilities,configuration_schema FROM control_plane.integration_definitions WHERE ($1='' OR category=$1) ORDER BY category,name
