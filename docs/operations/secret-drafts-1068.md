---
id: OPS-SECRET-DRAFTS-1068
title: Защищённые черновики Runtime Secret
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Граница поставки

Issue #1068 завершает Secret Broker часть D6 #1046 для MVP-UI-46/47/49/50.
Control-plane владеет metadata, tenant/owner, draft generation, OCC,
идемпотентностью, lease/claim и конечным состоянием. Secret Broker принимает
plaintext только в ограниченном RPC save и публикует его только в разрешённую
immutable runtime materialization. Сохранённый черновик содержит ciphertext;
ключевой материал доставляется отдельно только broker.

## Карта сценариев и жизненного цикла

| Действие | Полномочия и переход владельца | Эффект broker | Итог и восстановление |
| --- | --- | --- | --- |
| Save | HTTP session/CSRF → CP PrepareSave с project ownership, expected version и idempotency → отдельный одноразовый SAVE grant | Bounded plaintext/type/digest validation, AES-256-GCM с random nonce и exact immutable AAD, encrypted Kubernetes Secret | Owner Complete фиксирует descriptor; active Secret не меняется. Lost reply возвращает прежний exact intent. |
| Validate | CP разрешает текущий draft и выдаёт отдельный VALIDATE grant | Exact UID/resourceVersion/ciphertext/key identity readback, decrypt и type/content validation | Owner фиксирует VALIDATED только для той же generation. Plaintext не возвращается. |
| Publish | Fresh owner/tenant authority, validated generation, expected active version, отдельный PUBLISH claim | Exact encrypted read, decrypt, fenced immutable runtime materialization | CP атомарно фиксирует active revision/audit/receipt; effect-before-complete восстанавливается по exact descriptor. |
| Discard | CP закрывает draft, grants и claims в owner-транзакции | Bounded удаление только exact UID/resourceVersion | Повтор после lost response не публикует и не удаляет чужой объект. |
| Expiry | Серверное время и точный незавершённый draft, закрытие owner-графа | Точная очистка закрытого encrypted descriptor | Новая replica продолжает по authoritative owner read; локальный таймер не назначает terminal. |
| Claim expiry/retry | Предыдущий lease закрыт до новой claim generation | Readback прежнего эффекта перед новым действием | Неизвестный исход не подменяется новым idempotency key. |
| Key rotation | Отдельная repo-owned операция доставки, монотонная generation | Read-only keyring, durable rollback guard, overlap для сохранённых черновиков | Missing/corrupt/retired key закрыто отклоняет decrypt/readiness; прежние drafts не удаляются. |

Публичные endpoints и подготовка операций принадлежат #1045/#1046; browser не
получает operation grant, encrypted descriptor или keyring. Authority RPC,
клиент, policy registration, readiness и итоговый render входят в полную
поставку; наличие типов само по себе не означает завершения.

Для каждой terminal операции отдельного доменного события не требуется:
авторитетный путь — CP draft/tombstone/operation receipt, а broker recovery
читает exact owner work. Published Runtime Secret использует существующий
owner revision/impact/rebind путь. Потребители не читают staging напрямую.

## Проверенная библиотечная документация

Context7 resolve-library-id для Go завершился `fetch failed`; использованы
официальные [Go cipher](https://pkg.go.dev/crypto/cipher#NewGCMWithRandomNonce)
и [Kubernetes Secret](https://kubernetes.io/docs/concepts/configuration/secret/).
Random nonce добавляет 28 bytes overhead; число шифрований одним ключом
ограничивается устойчивым счётчиком ниже границы 2^32. Immutable Secret и
UID/resourceVersion preconditions защищают сохранённый encrypted descriptor.

## Проверки и допуск

Реализация выполняется в одном #1068 PR; unit ещё не завершён. Применимые
проверки фиксируются только после фактического запуска на exact SHA.
Live/staging/production — NOT RUN. Общий gate и приёмка принадлежат #1031.
