SELECT organization_id::text, project_id::text, owner_actor_id::text,
    resource_id::text, kind, version, attempt, generation, outcome,
    terminal_reason_code
FROM control_plane.next_workspace_recovery_candidate();
