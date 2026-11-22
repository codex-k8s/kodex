-- name: kubernetes_file__stream_source :one
SELECT source.ref FROM control_plane.kubernetes_file_input_sources source
JOIN control_plane.kubernetes_execution_streams stream ON stream.id=source.stream_id AND stream.organization_id=source.organization_id
JOIN control_plane.kubernetes_execution_plans plan ON plan.id=source.plan_id AND plan.organization_id=source.organization_id
JOIN control_plane.integration_invocations invocation ON invocation.id=plan.invocation_id AND invocation.organization_id=plan.organization_id
WHERE source.organization_id=@organization_id::uuid AND stream.ref=@stream_ref AND invocation.ref=@invocation_ref;
