-- name: configuration_changeschedule_select_workflow_target :one
SELECT workflow.id::text,
       workflow.published_version::bigint,
       revision.digest
FROM control_plane.workflows workflow
JOIN control_plane.workflow_versions revision
  ON revision.workflow_id = workflow.id
 AND revision.version_number = workflow.published_version
WHERE workflow.organization_id = $1::uuid
  AND workflow.project_id = $2::uuid
  AND workflow.ref = $3
  AND workflow.state = 'PUBLISHED'
