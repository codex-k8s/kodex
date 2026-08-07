---
id: RUNBOOK-DOC-INTERACTION-GATEWAY
title: Interaction gateway
type: runbook
status: approved
owner: manager
version: 1.6.0
updated: 2026-08-07
---

# Interaction gateway

## Сигналы

- `InteractionGatewayUnavailable` — нет доступной реплики;
- `InteractionGatewayNotReady` — exact dependency path не готов;
- `InteractionGatewayDeliveryFailures` — delivery worker не получает durable
  provider receipt либо domain acknowledgement;
- `InteractionGatewayInboundFailures` — inbound не проходит mapping, artifact
  scan или control-plane command;
- `InteractionGatewayTeamRecoveryFailures` — неоднозначный Mattermost Team
  effect не подтверждён exact provider readback и требует диагностики;
- `InteractionGatewayMappingRecoveryFailures` — специализированная mapping-команда
  не подтверждена авторитетным control-plane readback;
- `InteractionGatewayPostgresqlUnavailable` — недостаточно готовых CNPG Pod.

## Диагностика

1. Проверить `/readyz` и метрики gateway, authority issuer и authority verifier
   без чтения значений Secret.
2. Проверить готовность CNPG, control-plane, Mattermost REST/WebSocket и S3
   bucket по тем же TLS identities, которые использует рабочий путь.
3. Для delivery прочитать защищённый
   `/internal/v1/deliveries/{deliveryId}`: сверить state, payload digest,
   attempts и provider receipt. Помимо mTLS обязателен producer-specific bearer
   credential с exact organization/project/delivery; gateway должен получить
   положительный online readback durable issue/digest/revocation у control-plane.
   Claim token в readback не возвращается.
4. При ambiguous Mattermost POST проверять exact `PendingPostId`, client-owned
   `matter_codex_*` props, bot/channel/root и payload digest. Если exact
   readback отсутствует, outcome `AMBIGUOUS_PROVIDER_EFFECT_REQUIRES_RECONCILIATION`
   terminal: автоматический или ручной повтор effect запрещён.
5. При owner decision сверить Gate version, claim fence/expiry, recipient,
   Mattermost post/channel и process/session/turn/attempt/input lineage.
6. `DEAD_LETTER` не объявлять успешным и не изменять SQL вручную; recovery
   оформляется отдельным owner-approved repair Issue.
7. При зависшем inbound проверить DB-time lease/fence/token: expired
   `PROCESSING` reclaim-ится worker, а `WAITING_SCAN` poll не уменьшает terminal
   retry budget. Запись stale worker должна иметь нулевую cardinality.
8. При `DELETION_PENDING` проверить исходный delete event, Chat/Session version,
   24-часовой cleanup и отсутствие новых Turn/callback. Restore обязан отменить
   тот же cleanup receipt; SQL-переход вручную запрещён.
9. Для result больше Mattermost upload limit сверить `CLEAN`, exact private S3
   metadata/project prefix и одноразовый download audit. Direct S3 URL быть не
   должно; gateway повторно проверяет Mattermost User/channel membership и
   current `BOUND` mapping у control-plane до чтения object.
10. При Team create сверить immutable normalized intent, request SHA-256,
    project-scoped single-winner fence, operation lease, случайную correlation,
    operation-bound provider slug и момент `EFFECT_PENDING`. После
    неоднозначного ответа `CreateTeam` запрещено повторять provider mutation:
    recovery читает exact operation slug через `GetTeamByName` и до изменения
    membership проверяет correlation marker, display/type и causality digest.
    Чужая/raced Team не принимается и не изменяется. Transient readback
    сохраняет `AMBIGUOUS`; только PostgreSQL clock после durable deadline
    назначает `RECOVERY_TIMEOUT`/`REPAIR_REQUIRED`.
11. Raw Mattermost Team ID допустим только в RLS-scoped selector/operation
    checkpoint, подписанном internal provider receipt и авторитетной mapping
    spec control-plane. gRPC DTO, логи и метрики его не раскрывают;
    selector/cursor повторно разрешаются в actor/organization/project scope.
    Состояние `PROVIDER_ACCEPTED` означает только завершённый provider effect;
    ответ create считается успешным лишь после exact `BOUND` readback control-plane.
12. При bind/relink/unlink сверить semantic idempotency key, immutable request
    digest, local `effect_generation`/receipt JTI и авторитетные mapping
    version/generation. До каждого нового JTI и owner retry обязателен fresh
    exact Team+owner membership readback. Receipt обязан содержать exact `aud`,
    совпадающий с authority policy. После ambiguous RPC worker сначала
    выполняет signed `ListWorkspaceMattermostMappings` и
    `GetWorkspaceMattermostMapping`; повтор `ManageWorkspaceMattermostMapping`
    допустим только для доказанного прежнего owner-state. Durable outcome
    читается через `GetMattermostTeamMappingOperation`; `REPAIR_REQUIRED` и
    открытый Chat/Session/Turn/delivery graph не обходятся ручным SQL.
13. Для отказов inbound/delivery проверить PostgreSQL joined route: exact
    current `BOUND` mapping ID/version/generation/digest, fresh Mattermost
    Team/channel и отдельный monotonic high-watermark. `UNLINKED`, stale Team,
    недоступный provider либо неоднозначный owner list закрывают путь до любого
    provider effect. Проверка обязательна после reclaim queued inbound, для
    direct/owner delivery, catch-up, прямо перед publish и artifact download,
    а также в readiness; environment manifest не содержит current Team
    authority и старый tenant/project snapshot её не заменяет.

## Ротация ключей и PostgreSQL identity

- Mattermost JWK публикуется как `NEXT`, затем новый generation становится
  `CURRENT`, старый ограниченно обслуживается как `PREVIOUS`, после overlap —
  только `RETIRED`. Control-plane admission readback должен показать тот же
  high-watermark; откат и повторный ввод retired generation запрещены.
- Первый запуск не трактует пустую таблицу как доверие: migration job с
  `KEYSET_GENESIS_ENABLED=true` выполняет single-winner genesis, затем сверяет
  exact revision/digest и каждую пару generation/kid/RFC7638 thumbprint. Такой
  же lifecycle применяется к delivery-readback signer и verifier; отсутствие
  genesis audit закрывает startup.
- PostgreSQL migration job сверяет served high-watermark и immutable
  principal→organization/projects mapping.
  Следующее поколение вводится отдельным code-first изменением с overlap;
  `POSTGRES_EXPECTED_SESSION_USER` при этом обязан быть ровно
  `interaction_gateway_runtime_g<N>` для выбранного generation. До следующей
  ротации единственный `PREVIOUS` principal должен быть retired;
  retire выполняется `interaction_gateway_retire_runtime_identity` минимальной
  controller identity, делает `NOLOGIN`, revoke membership, завершает backend
  и не имеет rollback.

## Rollback

1. Остановить rollout и вернуть предыдущие immutable image digests и
   environment overlay.
2. Не выполнять `goose down`: миграция forward-only и совместима с отсутствием
   новой версии Pod.
3. Не удалять gateway PostgreSQL, S3 objects, provider receipts, cursors и
   delivery rows: они нужны для dedup/recovery.
4. Если изменился mapping, вернуть одновременно предыдущие Git-pinned Vault
   revision path, KV version, expected revision и digest. Mutable alias не
   использовать; опубликованный CAS=0 revision не перезаписывать.
5. Восстановить readiness и только затем возобновить входящие callbacks.

Production-действия требуют отдельного подтверждения владельца.
