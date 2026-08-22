-- name: platform__configuration_changeconnection_3 :one
UPDATE control_plane.integration_connections SET enabled=$4,state=$5,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND ref=$2 AND version=$3 RETURNING ref,definition_key,name,state,masked_credentials_state,last_test_summary,enabled,version,last_tested_at,created_at,updated_at
