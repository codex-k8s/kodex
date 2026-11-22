-- name: kubernetes_pf__connection_list :many
SELECT connection.ref FROM control_plane.kubernetes_port_forward_connections connection
WHERE connection.stream_id=@stream_id::uuid AND connection.organization_id=@organization_id::uuid AND connection.actor_id=@actor_id::uuid
 AND(@after_ref='' OR connection.generation>(SELECT cursor.generation FROM control_plane.kubernetes_port_forward_connections cursor
  WHERE cursor.ref=@after_ref AND cursor.stream_id=connection.stream_id AND cursor.organization_id=connection.organization_id AND cursor.actor_id=connection.actor_id))
ORDER BY connection.generation LIMIT @limit;
