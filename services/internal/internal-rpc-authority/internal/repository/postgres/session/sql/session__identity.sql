-- name: session__identity :one
SELECT session_user, current_user;
