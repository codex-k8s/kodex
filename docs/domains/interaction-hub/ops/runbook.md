---
doc_id: RB-CK8S-INTERACTION-HUB-0001
type: runbook
title: "interaction-hub — Runbook: deploy и диагностика"
status: active
owner_role: SRE
created_at: 2026-05-27
updated_at: 2026-05-27
related_issues: [894]
related_alerts: []
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-27-interaction-hub-ops-contour"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-27
---

# Runbook: interaction-hub — deploy и диагностика

## TL;DR

- Миграции: `deploy/base/interaction-hub/migrations.yaml.tpl`.
- Сервис: `deploy/base/interaction-hub/interaction-hub.yaml.tpl`.
- Smoke: `KODEX_SMOKE_ENV_FILE=<env> bash scripts/smoke-interaction-hub.sh`.
- Readiness: `GET /health/readyz` на HTTP-порту сервиса.

## Когда использовать

Runbook используется при первом backend deploy, после изменения миграций `interaction-hub` и при диагностике недоступности request/response lifecycle.

## Предпосылки и доступы

- Реальный env хранится вне Git в `bootstrap/host/config.env` или отдельном защищённом env-файле.
- В Kubernetes namespace уже доступны PostgreSQL, `platform-event-log` migrations и secret `kodex-platform-runtime`.
- Внутренний registry содержит образы `interaction-hub` и `interaction-hub-migrations`.
- Токены, DSN, адреса production и callback secrets не публикуются в Issue, PR, логах и документации.

## Диагностика

1. Проверить, что БД создана bootstrap job:
   `kubectl -n <namespace> logs job/kodex-postgres-bootstrap-databases`.
2. Проверить миграции:
   `kubectl -n <namespace> get job interaction-hub-migrations`.
3. Проверить rollout:
   `kubectl -n <namespace> rollout status deployment/interaction-hub`.
4. Проверить readiness:
   `kubectl -n <namespace> port-forward svc/interaction-hub 18087:8080` и `curl -fsS http://127.0.0.1:18087/health/readyz`.
5. Проверить метрики:
   `curl -fsS http://127.0.0.1:18087/metrics`.
6. Если readiness не готов, смотреть логи pod `interaction-hub` и init container `wait-database`.

## Типовые причины отказа

| Симптом | Вероятная причина | Действие |
|---|---|---|
| Migration job не завершается | Нет доступа к `KODEX_INTERACTION_HUB_DATABASE_DSN` или БД не создана | Проверить `kodex-platform-runtime`, bootstrap database job и PostgreSQL readiness. |
| Pod зависает на init container | Недоступна service DB или event-log DB | Проверить DSN keys `KODEX_INTERACTION_HUB_DATABASE_DSN` и `KODEX_INTERACTION_HUB_EVENT_LOG_DATABASE_DSN`. |
| `/health/readyz` возвращает ошибку | Сервис не может ping service DB или event-log DB | Проверить DSN, outbox publisher kind и connectivity к PostgreSQL. |
| gRPC возвращает `Unauthenticated` | Неверный service token | Проверить caller env и secret key `KODEX_INTERACTION_HUB_GRPC_AUTH_TOKEN`, не печатая значение. |
| Outbox events не появляются в event log | Dispatcher выключен или event-log DSN не задан | Проверить `KODEX_INTERACTION_HUB_OUTBOX_DISPATCH_ENABLED`, `KODEX_INTERACTION_HUB_OUTBOX_PUBLISHER_KIND` и event-log DB readiness. |

## Митигирование

- Повторить миграции безопасно: удалить завершённый или упавший job `interaction-hub-migrations` и применить `deploy/base/interaction-hub/migrations.yaml`.
- Перезапустить deployment после исправления env/secret: `kubectl -n <namespace> rollout restart deployment/interaction-hub`.
- Если event-log временно недоступен, сервис должен оставаться неготовым при включённом `postgres-event-log` publisher; отключение dispatcher допустимо только как явный операционный обход для диагностики.

## Проверка результата

- `interaction-hub-migrations` завершён успешно.
- `deployment/interaction-hub` rolled out.
- `/health/livez`, `/health/readyz` и `/metrics` доступны через service port-forward.
- В логах нет raw callback payload, transcript, prompt, tokens, DSN или secret values.

## Границы

Runbook не разворачивает UI, `staff-gateway`, concrete channel packages, Telegram/WhatsApp/Slack adapters, runtime workers и cross-domain operations inbox. Эти контуры подключаются отдельными срезами через владельцев своих доменов.

## Апрув

- request_id: `owner-2026-05-27-interaction-hub-ops-contour`
- Решение: approved
- Комментарий: эксплуатационный контур `interaction-hub` согласован для первого backend deploy без добавления нового бизнес-функционала.
