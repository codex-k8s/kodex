-- name: platform__configuration_recoverassistant_1 :exec
UPDATE control_plane.assistant_runtime SET runtime_state='RECOVERING',warm_instance_ref=NULL,last_heartbeat_at=NULL,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid AND version=$2
