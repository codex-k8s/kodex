-- name: workflow_launch__published_pins :one
SELECT version.ref,version.digest,workflow.version
FROM control_plane.workflows workflow
JOIN control_plane.workflow_versions version ON version.workflow_id=workflow.id
 AND version.organization_id=workflow.organization_id AND version.version_number=workflow.published_version
WHERE workflow.organization_id=@organization_id::uuid AND workflow.project_id=@project_id::uuid
 AND workflow.ref=@workflow_ref AND workflow.state='PUBLISHED'
FOR UPDATE OF workflow;
