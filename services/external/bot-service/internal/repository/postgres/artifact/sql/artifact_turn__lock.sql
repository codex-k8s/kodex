-- name: artifact_turn__lock :exec
select pg_advisory_xact_lock(hashtextextended(format(
	'artifact-turn:%s:%s:%s:%s', $1::bigint, $2::bigint, $3::bigint, $4::text
), 0));
