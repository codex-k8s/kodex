-- Только isolated disposable-БД и транзакция, которая всегда откатывается.
ALTER TABLE control_plane.runtime_revisions DISABLE TRIGGER protect_runtime_revision;
