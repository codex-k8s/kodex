---
doc_id: RB-CK8S-PACKAGE-HUB-OPS
type: runbook
title: "package-hub — Runbook: эксплуатационный контур"
status: active
owner_role: SRE
created_at: 2026-05-11
updated_at: 2026-05-11
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-11-package-hub-ops"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-11
---

# Runbook: package-hub — эксплуатационный контур

## TL;DR

- Симптом: `package-hub` не готов, миграции не завершились, gRPC-запросы не отвечают или события `package.*` не уходят в общий журнал событий.
- Быстрая диагностика: проверить `Deployment`, `Job/package-hub-migrations`, `/health/readyz`, `/metrics`, подключение к БД `package-hub`, доступ к `access-manager` и БД `platform-event-log`.
- Быстрое восстановление: исправить env/secret/image, повторно применить миграции, перезапустить `Deployment/package-hub`, проверить общей обвязкой первого кольца или Go checks.

## Когда использовать

- `Deployment/package-hub` не выходит в `Available`.
- `Job/package-hub-migrations` завершился ошибкой или завис в ожидании БД.
- `/health/readyz` возвращает ошибку.
- gRPC boundary не отвечает ожидаемым прикладным статусом.
- Outbox не публикует `package.*` события в `platform-event-log`.

Серьёзность зависит от поверхности, которая использует пакетную платформу. До подключения UI, `agent-manager` и runtime-срезов отказ `package-hub` блокирует только операции каталога и установок пакетов.

## Предпосылки и доступы

- Доступ к Kubernetes namespace платформы.
- Доступ к логам `package-hub`, `package-hub-migrations`, `access-manager` и `postgres`.
- Доступ к локальному bootstrap env, подготовленному `bootstrap/host/bootstrap_cluster.sh`.
- Значения секретов не выводить в логи, Issue, PR и сообщения.

## Диагностика

1. Проверить, что namespace и базовые манифесты применены:

   ```bash
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get deployment/package-hub service/package-hub job/package-hub-migrations
   ```

2. Проверить миграции:

   ```bash
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs job/package-hub-migrations
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe job/package-hub-migrations
   ```

3. Проверить rollout:

   ```bash
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" rollout status deployment/package-hub
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" describe deployment/package-hub
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs deploy/package-hub
   ```

4. Проверить health endpoint:

   ```bash
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" port-forward svc/package-hub 18083:8080
   curl -fsS http://127.0.0.1:18083/health/readyz
   curl -fsS http://127.0.0.1:18083/metrics
   ```

5. Проверить первый серверный контур через общую обвязку проверки:

   ```bash
   KODEX_SMOKE_ENV_FILE=/path/to/bootstrap.env bash bootstrap/host/smoke_backend_contour.sh
   ```

6. Если `readyz` не проходит, проверить зависимости:

   ```bash
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" rollout status deployment/access-manager
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" get job platform-event-log-migrations
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" logs statefulset/postgres
   ```

## Митигирование

1. Если образ не найден, пересобрать и загрузить образы:

   ```bash
   KODEX_BUILD_ENV_FILE=/path/to/bootstrap.env scripts/build-package-hub-images.sh
   ```

2. Если миграции упали из-за временной недоступности БД, удалить failed job и применить migration manifest повторно:

   ```bash
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" delete job package-hub-migrations --ignore-not-found
   kubectl apply -f /path/to/rendered/package-hub/migrations.yaml
   ```

3. Если изменились env/secret, повторно применить postgres secrets и `package-hub` manifest, затем перезапустить deployment:

   ```bash
   kubectl apply -f /path/to/rendered/postgres/secrets.yaml
   kubectl apply -f /path/to/rendered/package-hub/package-hub.yaml
   kubectl -n "$KODEX_PRODUCTION_NAMESPACE" rollout restart deployment/package-hub
   ```

4. Если outbox не публикует события, проверить `KODEX_PACKAGE_HUB_OUTBOX_*`, `KODEX_PACKAGE_HUB_EVENT_LOG_DATABASE_DSN` и доступность БД общего журнала событий.

## Эскалация

Эскалировать владельцу домена пакетной платформы, если:

- миграции требуют ручной правки данных;
- ошибка связана с несовместимой схемой БД;
- `package-hub` не может пройти access-check через `access-manager`;
- события `package.*` не попадают в общий журнал событий после повторного запуска.

К эскалации приложить:

- номер версии образа `package-hub`;
- статус `Deployment` и `Job`;
- фрагменты логов без секретов;
- результат проверки готовности;
- список последних изменений в манифестах и env.

## План отката

- Откатить образ `package-hub` на предыдущую проверенную версию через env `KODEX_PACKAGE_HUB_IMAGE`.
- Не откатывать миграции вручную без отдельного плана восстановления данных.
- Если новый сервис блокирует rollout платформы, временно не применять `package-hub` manifests, но оставить БД и общий event log в согласованном состоянии.

## Проверка результата

- `Job/package-hub-migrations` завершён успешно.
- `Deployment/package-hub` доступен.
- `/health/readyz` возвращает успешный ответ.
- `/metrics` доступен.
- Общая обвязка первого кольца проходит без повторной сборки образов; gRPC
  boundary проверяется Go tests или будущим Go integration runner.

## Пост-действия

- Если была авария, создать Issue с причиной и корректирующими действиями.
- Если обнаружен пробел в манифестах, env или проверке готовности, обновить этот runbook в том же PR, где исправляется поведение.

## Апрув

- request_id: `owner-2026-05-11-package-hub-ops`
- Решение: approved
- Комментарий: runbook закрепляет эксплуатационный контур `package-hub` для PKG-7.
