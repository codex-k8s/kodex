-- name: kubernetes_capture__prepared_reconcile :one
SELECT object_key,digest,size_bytes FROM control_plane.prepared_content
WHERE id=@id::uuid AND source_profile=@source_profile AND @source_profile IN('KUBERNETES_OUTPUT','KUBERNETES_FILE_SOURCE') AND request_digest=@request_digest
 AND generation=1 AND(state='UNKNOWN' OR state='IN_FLIGHT' AND writer_deadline<=clock_timestamp())
FOR UPDATE;
