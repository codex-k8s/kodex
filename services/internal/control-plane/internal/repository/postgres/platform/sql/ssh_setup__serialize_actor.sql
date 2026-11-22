-- name: ssh_setup__serialize_actor :exec
SELECT pg_advisory_xact_lock(hashtextextended(@organization_id||':'||@actor_id||':ssh-setup',0));
