---
id: EXT-MC-003
title: Integration gateway
type: service
status: approved
owner: backend
version: 2.0.0
updated: 2026-08-28
---

# integration-gateway

`integration-gateway` — stateless worker типизированных внешних capabilities.
Метаданные подключений, grants, leases, результаты и audit принадлежат
`control-plane`. Пустой набор подключений является штатным состоянием и не
влияет на readiness.

Юнит не предоставляет универсальный proxy. Один schema-versioned YAML package
определяет adapter, configuration fields, capabilities, operation, risk,
approval policy, input и resource scope. Gateway принимает только совпадающие
`definition_version`, `definition_digest`, grant scope и immutable input
digest.

Поставляются adapters:

- synthetic HTTP journal: read и идемпотентный write по exact effect key;
- GitHub: repository metadata read и create/update issue только в exact
  `owner/repository` Connection scope через `https://api.github.com`;
- Mattermost остаётся за отдельным необязательным `interaction-gateway`.

Credential claim содержит только revision ref, Kubernetes Secret
`namespace/name#key`, Secret UID, `resourceVersion` и content SHA-256. Token
читается из server-mounted Secret непосредственно перед GitHub вызовом,
проверяется по digest и не возвращается в API, логи, audit или result.

READ invocation может быть claim-нут сразу. WRITE, SENSITIVE и DESTRUCTIVE
сначала атомарно создают отдельный Human Gate и остаются недоступны worker до
`APPROVED`. Успешный внешний effect завершается immutable receipt с exact
effect key, input digest, provider effect ref и response digest; повторное
завершение допустимо только как exact readback той же receipt.

`/healthz` отражает жизнь процесса, `/readyz` читает локальный снимок sidecar
authority. Доступность control-plane и внешних систем наблюдается отдельным
рабочим/diagnostic контуром и не меняет Kubernetes readiness pod.
