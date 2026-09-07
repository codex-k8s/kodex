---
id: OPS-DOC-1150
title: Канонический bootstrap provider credential
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-07
---

# Канонический bootstrap provider credential

Refs #1150, #1148, #1031. Общий publisher —
`tools/install/provider-bootstrap.py`; команды `dev.sh provider-import` и
`provider-authorize` сохраняются. Новый API или deployable не вводится.

Первый installer seed позволяет CP создать installation owner и provider
account, но не доказывает готовность provider. После Ready control-plane и до
warm wait оба deploy entrypoint вызывают `recover`:

1. Оператор через existing Kubernetes context и PostgreSQL CP разрешает
   единственный account через installation owner и читает current descriptor.
2. Legacy exact UID/RV/content допускается только как вход переноса в новый
   immutable Secret. Старый объект и in-flight revisions не меняются и не удаляются.
3. Publisher `kodex-provider-bootstrap` устанавливает свой `bootstrap=v1`,
   actual accountRef и content digest; `auth.sha256` содержит ровно 64 ASCII байта.
4. SQL блокирует account, проверяет owner/state, account version и current ref.
   Один конкурирующий switch и audit фиксируются вместе; exact replay не увеличивает version.
   Ошибка CAS оставляет прежнюю привязку и созданный объект для разбора.
5. Независимый readback CP и Secret проверяет полный descriptor; default metadata
   обновляется с resourceVersion, без force. Равный readback не переписывает ConfigMap.

`recover` не принимает auth-file и сохраняет current credential, включая runtime
rotation. Явный `import` читает приватный обычный файл текущего пользователя без
symlink. Восстановление дополнительных accounts при `dev.sh up` использует
`--preserve-current`: файл нужен только при отсутствии credential.

Credential bytes не попадают в argv, SQL или diagnostics. Operator SQL хранит
только descriptor; bootstrap transition не публикует отдельное domain event:
результат читается через protected account/current revision, а существующий
catalog worker наблюдает новые account/credential versions. Это не публичная
команда авторизации runtime.

POST/commit с неизвестным результатом внутри запуска не повторяется. Следующий
запуск сначала читает deterministic имя и actual owner state. Foreign account,
неизвестный publisher, canonical LF, mismatch UID/RV/hash и disabled/terminal
account закрыто отклоняются. Legacy LF — только миграционный вход, не runtime
fallback. Cleanup новой bootstrap credential проходит exact owner path с UID/RV
preconditions; generic discovery не получает право удалять bootstrap объекты.

Restart CP со старым exact историческим bootstrap pin требует companion #1151.
Сначала сливается #1151, затем #1150. Неизвестные конфигурационные descriptors
по-прежнему не разрешают rotation.

## Проверки и ручная приёмка

- `make test-local-provider-account-persistence-contract`: publisher, replay,
  stale CAS, lost create ACK, сохранение current вместо старого файла.
- `make test-provider-bootstrap-postgres`: disposable PostgreSQL с полной схемой,
  actual SQL first bind/replay/concurrency/foreign/rollback/retained/disabled.
- Broker tests получают manifest непосредственно из Python publisher и читают
  настоящим `ReadProviderCredentialExact`, включая отрицательные pins.
- `make test-secret-broker-drafts`, install/web-only-release и local render обоих
  profiles проверяют применимые consumers и deploy entrypoint.

Root выполняет штатный `up` из слитого exact source: проверяет повтор без новой
revision, успешный свежий catalog, pinned warm session и Ready нового warm Pod.
До отдельного live readback эти результаты NOT RUN; workspace/STT/browser этим
не заменяются. Rollback forward-only: не переписывать immutable material и не
возвращать старую credential автоматически. Секреты и приватные данные не раскрыты.

Context7: проверены `/kubernetes/website`, immutable Secrets и resourceVersion
optimistic concurrency; ограничения сохранены в publisher.
