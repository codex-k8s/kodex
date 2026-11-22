-- name: personal_probe_deadline__job :one
SELECT test.state,test.lease_expires_at,test.probe_admitted_at IS NOT NULL,
 (SELECT count(*) FROM control_plane.audit_events audit
  WHERE audit.organization_id=test.organization_id AND audit.resource_ref=test.ref
   AND audit.action='platform.personal-integration-tests.begin')
FROM control_plane.integration_personal_credential_tests test
WHERE test.organization_id=$1::uuid AND test.ref=$2
