-- name: repository_bundle__work_guard :one
SELECT root.state,invocation.state,invocation.generation
FROM control_plane.repository_bundle_publications publication
JOIN control_plane.runs root ON root.id=publication.root_run_id AND root.organization_id=publication.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=publication.integration_invocation_id AND invocation.organization_id=publication.organization_id
WHERE publication.organization_id=@organization_id::uuid AND publication.ref=@proposal_ref
FOR UPDATE OF root,invocation NOWAIT;
