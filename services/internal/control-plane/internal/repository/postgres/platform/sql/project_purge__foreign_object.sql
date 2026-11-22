-- name: project_purge__foreign_object :one
SELECT EXISTS (
    SELECT 1 FROM control_plane.artifact_revision_content content
    JOIN control_plane.artifact_history artifact ON artifact.revision_id=content.revision_id
    WHERE content.object_key=@object_key
      AND (artifact.organization_id<>@organization_id::uuid OR artifact.project_id IS DISTINCT FROM @project_id::uuid)
    UNION ALL
    SELECT 1 FROM control_plane.agent_avatar_upload_reservations reservation
    WHERE reservation.object_key=@object_key
      AND (reservation.organization_id<>@organization_id::uuid OR reservation.project_id<>@project_id::uuid)
    UNION ALL
    SELECT 1 FROM control_plane.session_archives archive
    WHERE archive.object_key=@object_key
      AND (archive.organization_id<>@organization_id::uuid OR archive.project_id IS DISTINCT FROM @project_id::uuid)
    UNION ALL
    SELECT 1 FROM control_plane.session_archive_tasks task
    WHERE task.object_key=@object_key
      AND (task.organization_id<>@organization_id::uuid OR task.project_id IS DISTINCT FROM @project_id::uuid)
    UNION ALL
    SELECT 1 FROM control_plane.prepared_content prepared
    WHERE prepared.object_key=@object_key AND prepared.source_profile IN('KUBERNETES_OUTPUT','KUBERNETES_FILE_SOURCE')
      AND(prepared.organization_id<>@organization_id::uuid OR prepared.project_id IS DISTINCT FROM @project_id::uuid)
    UNION ALL
    SELECT 1 FROM control_plane.kubernetes_input_stages stage
    JOIN control_plane.kubernetes_execution_streams stream ON stream.id=stage.stream_id AND stream.organization_id=stage.organization_id
    JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
    JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
    JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
    JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=plan.invocation_id AND pin.organization_id=plan.organization_id
    JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
    WHERE stage.object_key=@object_key AND(stage.organization_id<>@organization_id::uuid OR root.project_id IS DISTINCT FROM @project_id::uuid)
    UNION ALL
    SELECT 1 FROM control_plane.kubernetes_output_stages stage
    JOIN control_plane.kubernetes_execution_streams stream ON stream.id=stage.stream_id AND stream.organization_id=stage.organization_id
    JOIN control_plane.kubernetes_execution_effects effect ON effect.id=stream.effect_id AND effect.organization_id=stream.organization_id
    JOIN control_plane.kubernetes_execution_children child ON child.id=effect.child_id AND child.organization_id=effect.organization_id
    JOIN control_plane.kubernetes_execution_plans plan ON plan.id=child.plan_id AND plan.organization_id=child.organization_id
    JOIN control_plane.kubernetes_invocation_identity_bindings pin ON pin.invocation_id=plan.invocation_id AND pin.organization_id=plan.organization_id
    JOIN control_plane.runs root ON root.id=pin.root_run_id AND root.organization_id=pin.organization_id
    WHERE stage.object_key=@object_key AND(stage.organization_id<>@organization_id::uuid OR root.project_id IS DISTINCT FROM @project_id::uuid)

)
