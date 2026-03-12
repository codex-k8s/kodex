-- name: runtimedeploytask__count_recent :one
SELECT COUNT(*)
FROM runtime_deploy_tasks
WHERE ($1::text = '' OR status = $1::text)
  AND ($2::text = '' OR target_env = $2::text);
