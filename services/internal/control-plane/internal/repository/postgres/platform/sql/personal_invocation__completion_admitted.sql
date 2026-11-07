-- name: personal_invocation__completion_admitted :one
SELECT EXISTS (
 SELECT 1 FROM control_plane.integration_personal_invocation_admissions admission
 JOIN control_plane.integration_invocations invocation ON invocation.id=admission.invocation_id
  AND invocation.organization_id=admission.organization_id
 WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
  AND admission.generation=@generation::bigint AND admission.lease_ref=@lease_ref
  AND admission.fence_digest=@fence_digest
);
