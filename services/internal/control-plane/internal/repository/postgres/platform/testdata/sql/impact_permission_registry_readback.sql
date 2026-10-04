-- Имя permission ограничивается закрытым списком fixture; значений Secret нет.
SELECT permission_key,resource_kinds FROM control_plane.permission_registry
WHERE permission_key IN ('secret.view','secret.create','secret.rotate','secret.revoke','secret.reveal')
ORDER BY permission_key;
