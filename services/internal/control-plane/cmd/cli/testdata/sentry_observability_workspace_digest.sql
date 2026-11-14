-- Только disposable fixture: hash всех retained rows без вывода содержимого.
SELECT encode(sha256(convert_to(jsonb_agg(to_jsonb(workspace) ORDER BY id)::text,'UTF8')),'hex')
FROM control_plane.session_workspace_storage workspace;
