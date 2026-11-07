-- name: personal_credential_test__cancel_binding :exec
UPDATE control_plane.integration_personal_credential_tests
SET state='CANCELLED',outcome='CANCELLED',version=version+1,finished_at=clock_timestamp(),
 lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL
WHERE organization_id=@organization_id::uuid AND binding_id=@binding_id::uuid AND state IN ('DUE','CLAIMED','PROBING')
