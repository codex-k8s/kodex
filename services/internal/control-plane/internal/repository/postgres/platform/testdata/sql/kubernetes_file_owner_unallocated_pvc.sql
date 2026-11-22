-- name: kubernetes_file_owner_unallocated_pvc :one
SELECT EXISTS(SELECT 1 FROM control_plane.sessions session
 WHERE session.organization_id=$1::uuid AND session.project_id=$2::uuid AND session.ref=$3
  AND 'runtime-session-'||encode(substring(public.digest(convert_to(session.ref,'UTF8'),'sha256') FROM 1 FOR 8),'hex')=$4
  AND NOT EXISTS(SELECT 1 FROM control_plane.session_workspace_storage storage
   WHERE storage.session_id=session.id AND storage.organization_id=session.organization_id AND storage.state<>'PURGED'));
