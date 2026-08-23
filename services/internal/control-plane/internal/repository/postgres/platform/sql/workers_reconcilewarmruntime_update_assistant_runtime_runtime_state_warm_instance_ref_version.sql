-- name: workers_reconcilewarmruntime_update_assistant_runtime_runtime_state_warm_instance_ref_version :exec
UPDATE control_plane.assistant_runtime SET runtime_state='RECOVERING',warm_instance_ref=$2,version=version+1,updated_at=clock_timestamp() WHERE organization_id=$1::uuid
