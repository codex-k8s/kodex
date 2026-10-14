-- name: prepared_record_receipt :one
SELECT control_plane.prepared_content_record_receipt(@id,@owner,@generation,@key,@version,@etag,@digest,@size);
