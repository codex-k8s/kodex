-- name: stage__create :exec
INSERT INTO agent_manager_stages (
    id, flow_version_id, slug, stage_type, display_name, icon_object_uri,
    required_artifacts, acceptance_policy, position
) VALUES (
    @id, @flow_version_id, @slug, @stage_type, @display_name, @icon_object_uri,
    @required_artifacts, @acceptance_policy, @position
);
