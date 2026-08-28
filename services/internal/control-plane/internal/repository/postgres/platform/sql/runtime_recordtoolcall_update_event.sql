-- name: runtime_recordtoolcall_update_event :exec
UPDATE control_plane.run_events
SET actor_kind=$2,actor_ref=$3,actor_name=$4,message_kind='TOOL_CALL',tool_call=$5
WHERE organization_id=$1::uuid AND ref=$6 AND type='TOOL_CALL_RECORDED'
