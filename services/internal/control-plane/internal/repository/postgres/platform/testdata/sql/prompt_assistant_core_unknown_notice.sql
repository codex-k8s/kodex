-- name: prompt_assistant_core_unknown_notice :exec
INSERT INTO control_plane.session_continuation_notices
    (organization_id,session_id,turn_id,node_id,attempt,previous_runtime_revision_id,
     current_runtime_revision_id,template_ref,template_digest,service_template_revision,
     service_template_digest,variable_snapshot_digest,diff_digest,materialization_digest,
     content,safe_snapshot)
SELECT organization_id,session_id,turn_id,node_id,attempt,previous_runtime_revision_id,
       current_runtime_revision_id,template_ref,template_digest,'prompt-service-v4',
       service_template_digest,variable_snapshot_digest,diff_digest,materialization_digest,
       content,safe_snapshot
FROM control_plane.session_continuation_notices
WHERE organization_id=$1::uuid
ORDER BY created_at,id LIMIT 1;
