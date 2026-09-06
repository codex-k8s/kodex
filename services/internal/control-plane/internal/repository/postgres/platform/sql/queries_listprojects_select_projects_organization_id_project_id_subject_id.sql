-- name: queries_listprojects_select_projects_organization_id_project_id_subject_id :many
SELECT p.id,
       p.ref,
       p.name,
       p.purpose,
       p.language,
       p.lifecycle,
       p.version,
       p.created_at,
       p.updated_at,
       COALESCE((
           SELECT array_agg(DISTINCT permission ORDER BY permission)
           FROM control_plane.memberships membership
           CROSS JOIN LATERAL unnest(membership.permissions) permission
           WHERE membership.organization_id=p.organization_id
             AND membership.project_id=p.id
             AND membership.subject_id=$2::uuid
             AND membership.active
       ), '{}'::text[]),
       (SELECT count(*)::integer FROM control_plane.agents agent WHERE agent.project_id=p.id AND agent.state<>'ARCHIVED'),
       (SELECT count(*)::integer FROM control_plane.workflows workflow WHERE workflow.project_id=p.id AND workflow.state<>'ARCHIVED'),
       (SELECT count(*)::integer FROM control_plane.runs execution WHERE execution.project_id=p.id AND execution.state IN ('QUEUED','RUNNING','WAITING_HUMAN','CANCELLING')),
       (SELECT count(*)::integer FROM control_plane.owner_gates gate WHERE gate.project_id=p.id AND gate.state='OPEN')
FROM control_plane.projects p
WHERE p.organization_id=$1::uuid
  AND p.lifecycle<>'ARCHIVED'
  AND ($5='' OR p.id=NULLIF($5,'')::uuid)
  AND EXISTS(SELECT 1 FROM control_plane.assistant_context_projection(
      p.organization_id,$2::uuid,NULLIF($5,'')::uuid,'PROJECT',p.ref,statement_timestamp()))
  AND ($3='' OR p.name ILIKE '%'||$3||'%' OR p.purpose ILIKE '%'||$3||'%')
ORDER BY p.updated_at DESC
LIMIT $4
