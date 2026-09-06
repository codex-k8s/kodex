-- name: prompt_workflow_template :one
SELECT revision.ref, revision.digest, revision.content
FROM control_plane.managed_configuration_bindings binding
JOIN control_plane.workflows workflow ON workflow.ref=binding.consumer_ref
  AND workflow.organization_id=binding.organization_id AND workflow.project_id=binding.project_id
JOIN control_plane.managed_configuration_sets configuration ON configuration.id=binding.configuration_set_id
  AND configuration.organization_id=binding.organization_id AND configuration.project_id=binding.project_id
  AND configuration.kind='PROMPT_TEMPLATE'
JOIN control_plane.managed_configuration_revisions revision ON revision.id=binding.configuration_revision_id
  AND revision.configuration_set_id=configuration.id AND revision.state IN ('PUBLISHED','SUPERSEDED')
WHERE binding.organization_id=@organization_id::uuid AND binding.consumer_kind='WORKFLOW'
  AND binding.configuration_kind='PROMPT_TEMPLATE' AND binding.consumer_ref=@workflow_ref;
