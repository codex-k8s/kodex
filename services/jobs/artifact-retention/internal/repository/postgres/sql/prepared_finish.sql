-- name: prepared_finish :one
SELECT control_plane.prepared_content_finish(@id,@owner,@generation,@success,@version,@etag);
