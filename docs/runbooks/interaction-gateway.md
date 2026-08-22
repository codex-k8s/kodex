---
id: RUN-MC-022
title: Диагностика optional interaction-gateway
type: runbook
status: approved
owner: sre
version: 1.0.0
updated: 2026-08-23
---

# Диагностика optional interaction-gateway

Компонент существует только в профиле `web-with-mattermost`. В `web-only` нет
его Deployment, authority trust, credential mount и NetworkPolicy. Его
отсутствие никогда не блокирует core startup, Run, Human Gate или artifact.

## Capabilities

- `mattermost.inbound`;
- `mattermost.notifications`;
- `mattermost.result_mirror`;
- `mattermost.gate_decisions`.

Каждая capability включается отдельным server-owned IntegrationGrant. Единого
`mattermostMode` нет. External Team/Channel/Post/User IDs являются только
локаторами adapter-а и не доказывают actor/project authority.

## Конфигурация

Server URL должен совпасть с exact allowed-host catalog deployment-а. Credential
читается по безопасной file reference; symlink, broad mode, oversized/nonregular
file и путь вне root закрыто отклоняются. В manifest и log нет token value.

## Probes

`/healthz` проверяет процесс. `/readyz` читает локальный config/authority
snapshot; Mattermost и control-plane не вызываются на каждую probe. Недоступный
external WebSocket/REST переводит конкретный source/delivery в degraded либо
retryable state, но Pod и core Run не становятся failed автоматически.

## Решение Human Gate

Adapter переводит разрешённую локализованную команду в typed control-plane
resolution с verified source/grant. Web и Mattermost конкурируют за одну
server-owned version: первый valid winner продолжает Run, второй получает
stale/conflict readback. Adapter не создаёт continuation самостоятельно.

## Диагностика delivery

Проверить connection/grant state, exact source locator, attempt count,
next-attempt time и safe error key через Administration. После terminal delivery
failure создаётся optional incident `CoreAffected=false`. Не публиковать
результат вручную и не менять успешный core Run.

Пользовательские ответы выбираются из `internal/i18n/messages.ru.yaml` или
`messages.en.yaml` по проверенной locale. Runtime diagnostics остаются на
английском и не содержат message text, post body или credentials.
