-- name: integration_github_historical_fixture :exec
-- Только metadata в disposable БД: воспроизведение подключения до upgrade.
UPDATE control_plane.integration_connections
SET definition_version=$2,definition_digest=$3,version=version+1
WHERE ref=$1 AND definition_key='github';
