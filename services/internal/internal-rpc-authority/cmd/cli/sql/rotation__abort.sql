-- name: rotation__abort :one
SELECT internal_rpc_authority.publisher_abort_rotation($1::uuid, $2, $3);
