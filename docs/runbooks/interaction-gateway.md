---
id: RUNBOOK-DOC-INTERACTION-GATEWAY
title: Interaction gateway
type: runbook
status: approved
owner: manager
version: 1.0.0
updated: 2026-08-04
---

# Interaction gateway

## Сигналы

- `InteractionGatewayUnavailable` — нет доступной реплики;
- `InteractionGatewayNotReady` — exact dependency path не готов;
- `InteractionGatewayDeliveryFailures` — delivery worker не получает durable
  provider receipt либо domain acknowledgement;
- `InteractionGatewayInboundFailures` — inbound не проходит mapping, artifact
  scan или control-plane command;
- `InteractionGatewayPostgresqlUnavailable` — недостаточно готовых CNPG Pod.

## Диагностика

1. Проверить `/readyz` и метрики gateway/authority issuer без чтения значений
   Secret.
2. Проверить готовность CNPG, control-plane, Mattermost REST/WebSocket и S3
   bucket по тем же TLS identities, которые использует рабочий путь.
3. Для delivery прочитать защищённый
   `/internal/v1/deliveries/{deliveryId}`: сверить state, payload digest,
   attempts и provider receipt. Claim token в readback не возвращается.
4. При ambiguous Mattermost POST проверять exact `PendingPostId`, client-owned
   `matter_codex_*` props, bot/channel/root, текст и digest каждого файла;
   ручной повтор публикации запрещён.
5. При owner decision сверить Gate version, claim fence/expiry, recipient,
   Mattermost post/channel и process/session/turn/attempt/input lineage.
6. `DEAD_LETTER` не объявлять успешным и не изменять SQL вручную; recovery
   оформляется отдельным owner-approved repair Issue.

## Rollback

1. Остановить rollout и вернуть предыдущие immutable image digests и
   environment overlay.
2. Не выполнять `goose down`: миграция forward-only и совместима с отсутствием
   новой версии Pod.
3. Не удалять gateway PostgreSQL, S3 objects, provider receipts, cursors и
   delivery rows: они нужны для dedup/recovery.
4. Если изменился mapping, вернуть его предыдущую Git revision вместе с
   соответствующими bot identities; не создавать dual write в legacy bot.
5. Восстановить readiness и только затем возобновить входящие callbacks.

Production-действия требуют отдельного подтверждения владельца.
