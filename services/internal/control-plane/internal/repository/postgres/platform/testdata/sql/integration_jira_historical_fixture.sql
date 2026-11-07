-- name: integration_jira_historical_fixture :exec
-- Только metadata одноразовой fixture: прежняя pinned connection до upgrade.
UPDATE control_plane.integration_connections
SET definition_version=$2,definition_digest=$3,version=version+1
WHERE ref=$1 AND definition_key='jira-server-dc';
