-- name: ProjectHasLiveResources :one
SELECT EXISTS (
    SELECT 1
    FROM control_plane.resources
    WHERE organization_id = @organization_id::uuid
      AND project_id = @project_id::uuid
      AND id <> @project_id::uuid
      AND state <> 'DELETED'
      AND NOT (
          kind = 'WORKSPACE_MATTERMOST_MAPPING'
          AND state = 'ARCHIVED'
          AND spec ->> 'mappingState' = 'UNLINKED'
      )
);
