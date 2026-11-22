-- name: broker_health__shared_finish :exec
UPDATE control_plane.integration_connection_tests SET state=@state,result_summary=@summary,safe_error_code=@error_code,lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL,version=version+1,updated_at=clock_timestamp()
WHERE organization_id=@organization_id::uuid AND id=@id::uuid AND state='CLAIMED';
