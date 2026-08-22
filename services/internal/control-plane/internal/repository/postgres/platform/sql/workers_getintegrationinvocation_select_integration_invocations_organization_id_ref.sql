-- name: platform__workers_getintegrationinvocation_select_integration_invocations_organization_id_ref :one
SELECT state,result_summary,safe_error_code FROM control_plane.integration_invocations WHERE organization_id=$1::uuid AND ref=$2
