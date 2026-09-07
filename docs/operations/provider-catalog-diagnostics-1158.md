---
id: OPS-DOC-1158
title: Безопасная диагностика наблюдения каталога моделей
type: runbook
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-07
---

# Отказ наблюдения каталога

Refs #1158, #1148, #1031. При отсутствии проверенного каталога Control Plane
не создаёт warm runtime. `UNVERIFIED_SOURCE` не означает отозванную
авторизацию: этот результат также возникает при отказе проверки протокола
или происхождения свежего каталога. Причина текущего live-инцидента пока
не установлена; диагностический checkpoint не объявляет её исправленной.

Secret Broker пишет один `provider model catalog observation rejected` для
завершённого отклонённого наблюдения. Поля `stage` и `failure` принадлежат
закрытым множествам. Account/task refs, credential, provider response, stderr,
модели, содержимое кэша и исходная ошибка в эту запись не включаются.
Публичный ответ и сохранённый Failure не меняются; повторных provider вызовов
диагностика не добавляет. Ошибки transport/deadline по-прежнему обслуживаются
существующей границей gRPC.

| `stage` | Проверяемая граница |
| --- | --- |
| `credential_read` | Exact owner descriptor и чтение credential |
| `authentication` | Закрытая форма credential для выбранного метода |
| `runtime_check` | Исполняемый файл, private root и exact версия CLI |
| `process_start`, `initialize` | Создание изолированного App Server и handshake |
| `login_response` | RPC входа и тип `chatgptAuthTokens` |
| `model_list_call` | Завершение RPC списка |
| `model_list_schema`, `model_list_identity` | Форма страницы и соответствие id/model |
| `model_list_capabilities`, `model_list_cursor` | Reasoning capabilities и bounded pagination |
| `cache_open`, `cache_metadata`, `cache_read` | Новый private cache, тип файла, владелец, links и размер |
| `cache_schema`, `cache_version`, `cache_freshness` | Структура, exact версия и время в текущем наблюдении |
| `cache_identity`, `cache_capabilities` | Уникальность моделей и допустимые reasoning значения |
| `capabilities_match` | Совпадение default и упорядоченных efforts между cache и model/list |
| `api_catalog` | Account-specific API catalog и его проверенный источник capabilities |
| `cleanup`, `result_validation` | Удаление private state и итоговая граница результата |
| `unknown` | Отказ без известного внутреннего этапа; payload не используется как код |

Оператор после штатного rollout читает только эту закрытую запись и связывает
её по времени с новым наблюдением CP. Следующий шаг выбирается по конкретному
этапу. Нельзя исправлять отказ допуском bundled/stale cache, печатью provider
payload, отключением provenance, изменением SQL состояния или ручным повтором
платного запуска. Success требует нового проверенного наблюдения, Ready warm
Pod и штатного `up/status/smoke`; локальные fixtures этого не доказывают.

Проверены Context7 `/openai/codex` (model/list, cache, external tokens) и
официальный закреплённый исходник `rust-v0.153.4`:
[версия cache](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/models-manager/src/lib.rs),
[producer cache](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/models-manager/src/manager.rs),
[ModelInfo → ModelPreset](https://github.com/openai/codex/blob/rust-v0.153.4/codex-rs/protocol/src/openai_models.rs).
`client_version_to_whole` сохраняет patch `0.153.4`; отсутствующий default
преобразуется в `none`, порядок efforts сохраняется. Эти проверки не ослаблены.

Регрессии выполняются существующим `go test -race ./...` в модуле broker:
изолированный test process проверяет login/list/cache failure и cleanup;
parser проверяет свежесть/версию/capabilities и null default; log fixture
проверяет закрытые поля, отсутствие payload и единственную запись.
Настоящий provider, live-диагностика и бизнесовая приёмка до rollout — NOT RUN.
