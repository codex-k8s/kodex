-- name: commands_changeagentbinding_actor_can_grant :one
SELECT @actor_platform_role IN ('OWNER', 'ADMINISTRATOR')
    OR EXISTS (
        SELECT 1
        FROM control_plane.memberships membership
        WHERE membership.organization_id = @organization_id::uuid
          AND membership.project_id = @project_id::uuid
          AND membership.subject_id = @actor_id::uuid
          AND membership.active
          AND CASE @capability_key
              WHEN 'platform.project.manage' THEN 'MANAGE'
              WHEN 'platform.agent.manage' THEN 'MANAGE_AGENTS'
              WHEN 'platform.run.launch' THEN 'LAUNCH_RUNS'
              WHEN 'platform.run.delegate' THEN 'MANAGE_AGENTS'
              WHEN 'platform.gate.resolve' THEN 'RESOLVE_GATES'
              WHEN 'platform.artifact.manage' THEN 'MANAGE_ARTIFACTS'
              WHEN 'platform.schedule.manage' THEN 'MANAGE_SCHEDULES'
              WHEN 'platform.integration.grant' THEN 'MANAGE_INTEGRATIONS'
              ELSE '__DENY__'
          END = ANY(membership.permissions)
    );
