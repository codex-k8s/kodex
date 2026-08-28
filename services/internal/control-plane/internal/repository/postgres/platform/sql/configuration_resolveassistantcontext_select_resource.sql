-- name: configuration_resolveassistantcontext_select_resource :one
SELECT COALESCE(resource.project_id::text, ''), resource.name, resource.version
FROM (
    SELECT 'PROJECT'::text AS kind, p.organization_id, p.ref, p.id AS project_id, p.name, p.version
    FROM control_plane.projects p
    UNION ALL
    SELECT 'AGENT', a.organization_id, a.ref, a.project_id, a.name, a.version
    FROM control_plane.agents a
    UNION ALL
    SELECT 'WORKFLOW', w.organization_id, w.ref, w.project_id, w.name, w.version
    FROM control_plane.workflows w
    UNION ALL
    SELECT 'RUN', r.organization_id, r.ref, r.project_id, r.title, r.version
    FROM control_plane.runs r
) resource
WHERE resource.organization_id = $1::uuid
  AND resource.ref = $2
  AND resource.kind = $3
LIMIT 1
