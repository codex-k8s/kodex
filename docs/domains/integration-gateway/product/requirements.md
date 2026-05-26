---
doc_id: PRD-CK8S-INTEGRATION-GATEWAY-0001
type: prd
title: kodex — требования integration-gateway
status: active
owner_role: PM
created_at: 2026-05-25
updated_at: 2026-05-26
related_issues: [781, 792, 807, 770]
related_prs: []
related_docsets:
  - docs/platform/architecture/service_boundaries.md
  - docs/platform/architecture/provider_integration_model.md
  - docs/domains/provider-native-work-items/architecture/api_contract.md
  - docs/domains/interaction-hub/architecture/api_contract.md
approvals:
  required: ["Owner"]
  status: approved
  request_id: "owner-2026-05-25-integration-gateway-igw-0"
  approved_by: "ai-da-stas"
  approved_at: 2026-05-25
---

# Требования integration-gateway

## Кратко

`integration-gateway` нужен как отдельный внешний HTTP-вход для webhook и callback событий. Он принимает запросы от GitHub/GitLab, внешних каналов, пакетов и будущих интеграций, проверяет их на границе и передаёт безопасный envelope сервису-владельцу.

Первый активный MVP-сценарий: GitHub provider webhook -> `provider-hub.IngestWebhookEvent`.

## Пользователи и системы

| Участник | Потребность |
|---|---|
| GitHub | Доставить webhook в публичный HTTPS endpoint платформы. |
| GitLab и другие providers | Подключаются отдельными расширениями route registry после GitHub-среза. |
| Внешний канал | Вернуть callback по доставленному запросу обратной связи или approval. |
| Пакет или интеграция | Передать callback в платформу по согласованному route. |
| `provider-hub` | Получить уже проверенный webhook через внутренний gRPC без публичной HTTP-логики. |
| `interaction-hub` | Получить будущий безопасный callback envelope без владения публичной проверкой подписи. |
| Оператор платформы | Видеть безопасный статус входного контура, отказы подписи, rate limit и backpressure без секретов и больших payload. |

## Функциональные требования

| ID | Требование | Приоритет |
|---|---|---|
| IGW-FR-1 | Сервис должен принимать внешние provider webhook через публичный HTTP endpoint. | Обязательно |
| IGW-FR-2 | Сервис должен проверять источник, подпись или token, content type, размер payload и route policy до вызова внутреннего владельца. | Обязательно |
| IGW-FR-3 | Сервис должен передавать provider webhook в `provider-hub.IngestWebhookEvent` с `provider_slug`, `delivery_id`, `event_name`, `payload_json`, `received_at` и безопасным `meta` после успешной edge-проверки. | Обязательно |
| IGW-FR-4 | Сервис не должен нормализовать provider business events, строить provider projections или хранить webhook inbox. | Обязательно |
| IGW-FR-5 | Сервис должен поддерживать backpressure и rate limits по source, route и downstream owner service. | Обязательно |
| IGW-FR-6 | Сервис должен возвращать безопасные HTTP-ошибки без секретов, подписей, токенов и полного payload. | Обязательно |
| IGW-FR-7 | Сервис должен иметь OpenAPI-контракт внешней HTTP-поверхности. | Обязательно |
| IGW-FR-8 | Сервис должен иметь задел для callback routes внешних каналов и пакетов, но активировать их только после готовности owner-service контракта. | Обязательно |

## Нефункциональные требования

| ID | Категория | Требование |
|---|---|---|
| IGW-NFR-1 | Безопасность | Значения секретов используются только в памяти процесса на время проверки подписи. |
| IGW-NFR-2 | Надёжность | Повтор provider webhook должен быть безопасен через delivery id и дедупликацию у сервиса-владельца. |
| IGW-NFR-3 | Производительность | Payload size guard и backpressure должны срабатывать до gRPC-вызова владельца. |
| IGW-NFR-4 | Наблюдаемость | Метрики и логи должны содержать route, source, статус, latency, payload size bucket и безопасную причину отказа. |
| IGW-NFR-5 | Расширяемость | Новый внешний source добавляется через route registry и owner-service contract, а не через доменную логику в gateway. |

## Критерии приёмки MVP

| ID | Критерий |
|---|---|
| IGW-AC-1 | Provider webhook с валидной подписью и delivery id приводит к внутреннему вызову `provider-hub.IngestWebhookEvent`. |
| IGW-AC-2 | Невалидная подпись, неизвестный source, превышение размера или rate limit отклоняются до вызова владельца. |
| IGW-AC-3 | Gateway не пишет полный payload, секреты, подписи или токены в логи, ошибки, метрики и события. |
| IGW-AC-4 | `provider-hub` остаётся единственным владельцем webhook inbox, дедупликации и нормализации provider events. |
| IGW-AC-5 | Callback route описан как контрактный задел и не активируется без готового `interaction-hub` или другого owner-service контракта. |

## Не входит

- Полная активация provider webhook route без проверки подписи и source binding.
- Хранение provider projections, cursors, operations или provider business state.
- UI/staff/user endpoints.
- Codex hooks и MCP tools.
- Конкретные GitLab/GitHub адаптеры нормализации payload.

## Апрув

- request_id: `owner-2026-05-25-integration-gateway-igw-0`
- Решение: approved
- Комментарий: требования `integration-gateway` согласованы как целевое состояние IGW-0.
