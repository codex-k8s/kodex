-- name: configuration_changeintegrationgrant_select_workflow_organization_id_ref :one
SELECT p.id::text,p.ref,w.name FROM control_plane.workflows w JOIN control_plane.projects p ON p.id=w.project_id WHERE w.organization_id=$1::uuid AND w.ref=$2 AND w.state IN ('DRAFT','VALID','PUBLISHED') AND p.lifecycle='ACTIVE'
