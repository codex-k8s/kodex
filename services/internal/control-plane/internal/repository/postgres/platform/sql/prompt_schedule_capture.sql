-- name: prompt_schedule_capture :one
SELECT jsonb_build_object('revision',1,'values',@values::jsonb,'template',(
    SELECT jsonb_build_object('ref',revision.ref,'digest',revision.digest,'content',revision.content,
        'source',configuration.source,'sourceRevision',configuration.source_revision,
        'scope',CASE WHEN scope.revision_id IS NOT NULL THEN jsonb_build_object(
            'targetKind',scope.target_kind,'targetRef',scope.target_ref,'templateKind',scope.template_kind,'contextPin',scope.context_pin) END)
    FROM control_plane.managed_configuration_bindings binding
    JOIN control_plane.schedules schedule ON schedule.ref=binding.consumer_ref
      AND schedule.organization_id=binding.organization_id AND schedule.project_id=binding.project_id
    JOIN control_plane.managed_configuration_sets configuration ON configuration.id=binding.configuration_set_id
      AND configuration.organization_id=binding.organization_id AND configuration.project_id=binding.project_id
      AND configuration.kind='PROMPT_TEMPLATE'
    JOIN control_plane.managed_configuration_revisions revision ON revision.id=binding.configuration_revision_id
      AND revision.configuration_set_id=configuration.id AND revision.state IN ('PUBLISHED','SUPERSEDED')
    LEFT JOIN control_plane.prompt_template_scopes scope ON scope.revision_id=revision.id AND scope.organization_id=binding.organization_id
    WHERE binding.organization_id=@organization_id::uuid AND binding.consumer_kind='SCHEDULE'
      AND binding.configuration_kind='PROMPT_TEMPLATE' AND binding.consumer_ref=@schedule_ref
))::text;
