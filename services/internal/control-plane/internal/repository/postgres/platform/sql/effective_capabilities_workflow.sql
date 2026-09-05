-- name: effective_capabilities_workflow :one
SELECT p.ref, version.ref, version.spec
FROM control_plane.workflows workflow
JOIN control_plane.projects p ON p.id=workflow.project_id
JOIN control_plane.workflow_versions version ON version.workflow_id=workflow.id
 AND version.version_number=workflow.published_version
WHERE workflow.organization_id=$1::uuid AND workflow.ref=$2
 AND workflow.state<>'ARCHIVED' AND p.lifecycle='ACTIVE';
