-- name: PermissionIndexActorRoles
SELECT DISTINCT role.id::text
FROM control_plane.resources AS team
CROSS JOIN LATERAL jsonb_array_elements_text(team.spec -> 'memberActorIds') AS member(actor_id)
CROSS JOIN LATERAL jsonb_array_elements_text(team.spec -> 'roleIds') AS assigned(role_id)
JOIN control_plane.resources AS role
  ON role.id = assigned.role_id::uuid
 AND role.organization_id = team.organization_id
 AND role.project_id = team.project_id
 AND role.kind = 'ROLE'
 AND role.state = 'ACTIVE'
WHERE team.organization_id = @organization_id::uuid
  AND team.project_id = @project_id::uuid
  AND team.kind = 'TEAM'
  AND team.state = 'ACTIVE'
  AND member.actor_id = @actor_id
ORDER BY role.id::text
