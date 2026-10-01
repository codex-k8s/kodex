-- name: assistant_purge_delete :one
SELECT control_plane.purge_assistant_conversation(
    $1::uuid, $2::uuid, $3, $4, $5::uuid, 'OWNER_REQUEST'
);
