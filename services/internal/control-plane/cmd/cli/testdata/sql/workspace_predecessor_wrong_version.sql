-- Только отдельная disposable БД; изменение откатывается после отрицательной проверки.
INSERT INTO public.goose_db_version(version_id,is_applied) VALUES(20261026001175,true);
