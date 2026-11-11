---
id: OPS-PROVIDER-MODEL-CATALOG-1068
title: Наблюдение каталога моделей через Secret Broker
type: operations
status: approved
owner: developer
version: 2.0.0
updated: 2026-11-11
---

# Каталог моделей OpenCode

В #2108 единственным источником наблюдения является `REMOTE_API` настроенного
OpenCode provider route. App-server, device flow и его cache reader из каталога
удалены; offline Codex harness и копирование бинарника в broker не являются
частью активного профиля. История прежнего механизма #1068/#1100 доступна в Git.

CP владеет account, credential revision, catalog task/claim, freshness и
публикацией. Брокер читает credential только после проверки transport authority,
точных task/request/credential pins, API_KEY и совпадения definition с локальной
проверенной конфигурацией. Отсутствующая либо чужая конфигурация не получает
default provider. Значение ключа не возвращается браузеру и очищается после
наблюдения. Карта сквозного сценария и всех переходов находится в
[описании #2108](opencode-native-sessions-2108.md#каталог-моделей-после-удаления-codex-runtime).

Наблюдение выполняет HTTPS GET `/models` выбранного endpoint через прежнюю
exact TLS/egress boundary. Redirect, произвольный endpoint в request, скрытый
retry и fallback запрещены. Полный ответ ограничен 4 MiB и 4096 remote IDs;
выдача содержит не более 128 моделей и 16 effort identifiers на модель,
128 KiB на gRPC response. Deadline не превышает 15 секунд либо более ранний
caller/task срок. Отмена прекращает HTTP запрос и не публикует частичный список.

Remote IDs пересекаются с allowlist deployment configuration. Невыбранные
aliases не становятся моделями Kodex; повтор выбранного ID и частичная
страница отклоняются. `/models` не доказывает совместимость turn/tool protocol
и не назначает reasoning defaults. Текущий configured catalog не выдаёт
reasoning capabilities без отдельного квалифицированного источника.

`NONE` допускается только вместе с `REMOTE_API`. Подтверждённое пустое
пересечение остаётся пустым, прежние модели не возвращаются как свежие.
`UNAVAILABLE`, `UNVERIFIED_SOURCE`, `AUTHORIZATION_REJECTED` не содержат моделей
и source. CP сохраняет результат только по действующей exact claim; поздний
ответ после revoke/expiry/config removal отклоняется. Новый completion
атомарно создаёт прежний `PROVIDER_ACCOUNT_CHANGED`; exact replay не повторяет
событие. Потерянный ответ проверяется через owner receipt.

Новых listener, workload, permissions или secret keys нет. Используются
прежние secret-broker ServiceAccount, credential read RBAC, protected RPC,
startup/readiness и metric bucket `provider_catalog_observe`. Account availability
доказывает свежий owner observation, а не локальная readiness брокера.

Публичные точки проверки: `make test-secret-broker-drafts`,
`make test-control-plane-postgres` и `make test-opencode-native` для настоящего
OpenCode с синтетической моделью. Успех этих команд сам по себе не является
доказательством установленного cluster path или доступности платного провайдера.
Результаты, пропуски и точные SHA фиксируются в #2108 отдельно.
