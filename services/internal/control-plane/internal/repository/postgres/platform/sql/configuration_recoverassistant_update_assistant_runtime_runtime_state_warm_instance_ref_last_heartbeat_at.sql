-- name: configuration_recoverassistant_update_assistant_runtime_runtime_state_warm_instance_ref_last_heartbeat_at :exec
UPDATE control_plane.assistant_runtime SET runtime_state='RECOVERING',warm_instance_ref=NULL,last_heartbeat_at=NULL,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND version=$2
