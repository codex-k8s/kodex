-- name: commands_launchrun_select_workflows_organization_id_project_id_ref :one
SELECT workflow.name,
       version.id::text,
       version.ref,
       version.spec,
       version.digest,
       coordinator.ref,
       coordinator.name
FROM control_plane.workflows workflow
JOIN control_plane.workflow_versions version
  ON version.workflow_id = workflow.id
 AND version.version_number = workflow.published_version
JOIN control_plane.agents coordinator
  ON coordinator.id = workflow.coordinator_agent_id
 AND coordinator.organization_id = workflow.organization_id
 AND coordinator.project_id = workflow.project_id
 AND coordinator.enabled
 AND coordinator.state = 'READY'
WHERE workflow.organization_id = @organization_id::uuid
  AND workflow.project_id = @project_id::uuid
  AND workflow.ref = @workflow_ref
  AND workflow.state = 'PUBLISHED'
