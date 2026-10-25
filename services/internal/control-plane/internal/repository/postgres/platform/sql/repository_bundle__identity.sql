-- name: repository_bundle__identity :one
SELECT invocation.id::text,gate.id::text,invocation.input_digest,invocation.effect_key
FROM control_plane.integration_invocations invocation
JOIN control_plane.owner_gates gate ON gate.integration_invocation_id=invocation.id AND gate.organization_id=invocation.organization_id
WHERE invocation.organization_id=@organization_id::uuid AND invocation.ref=@invocation_ref
  AND invocation.node_id=@node_id::uuid AND invocation.grant_id=@grant_id::uuid
  AND gate.root_run_id=@root_run_id::uuid AND gate.project_id=@project_id::uuid
  AND invocation.operation='github.repository.bundle.publish'
  AND invocation.approval_policy='HUMAN_EACH_EFFECT' AND invocation.risk='WRITE';
