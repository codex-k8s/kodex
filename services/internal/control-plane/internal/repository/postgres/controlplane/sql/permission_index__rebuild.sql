WITH removed AS (
    DELETE FROM control_plane.project_actor_permissions
    WHERE organization_id = @organization_id::uuid
      AND project_id = @project_id::uuid
),
indexed AS (
    SELECT
        project.organization_id,
        project.id AS project_id,
        project.owner_actor_id AS actor_id,
        '*'::text AS permission,
        project.version AS source_version
    FROM control_plane.resources AS project
    WHERE project.organization_id = @organization_id::uuid
      AND project.id = @project_id::uuid
      AND project.kind = 'PROJECT'
      AND project.state <> 'DELETED'
    UNION
    SELECT
        team.organization_id,
        team.project_id,
        member.value::uuid AS actor_id,
        capability.value AS permission,
        greatest(team.version, role.version) AS source_version
    FROM control_plane.resources AS team
    CROSS JOIN LATERAL jsonb_array_elements_text(
        team.spec -> 'memberActorIds'
    ) AS member(value)
    CROSS JOIN LATERAL jsonb_array_elements_text(team.spec -> 'roleIds') AS role_id(value)
    JOIN control_plane.resources AS role
      ON role.id = role_id.value::uuid
     AND role.organization_id = team.organization_id
     AND role.project_id = team.project_id
     AND role.kind = 'ROLE'
     AND role.state = 'ACTIVE'
    CROSS JOIN LATERAL jsonb_array_elements_text(
        role.spec -> 'capabilities'
    ) AS capability(value)
    WHERE team.organization_id = @organization_id::uuid
      AND team.project_id = @project_id::uuid
      AND team.kind = 'TEAM'
      AND team.state = 'ACTIVE'
)
INSERT INTO control_plane.project_actor_permissions (
    organization_id,
    project_id,
    actor_id,
    permission,
    source_version,
    updated_at
)
SELECT
    organization_id,
    project_id,
    actor_id,
    permission,
    max(source_version),
    clock_timestamp()
FROM indexed
GROUP BY organization_id, project_id, actor_id, permission
