-- name: personal_credential_test__finish :exec
UPDATE control_plane.integration_personal_credential_tests
SET state=@state,outcome=@outcome,version=version+1,finished_at=clock_timestamp(),
 lease_ref=NULL,fence_digest=NULL,workload_instance=NULL,lease_expires_at=NULL
WHERE organization_id=@organization_id::uuid AND id=@id::uuid AND version=@version AND state IN ('DUE','CLAIMED','PROBING')
