---
id: UNIT-INTERACTION-GATEWAY-001
title: Необязательный interaction gateway Kodex
type: unit-readme
status: approved
owner: platform
version: 1.1.0
updated: 2026-09-05
---

# interaction-gateway

`interaction-gateway` — необязательный interaction adapter. Он обслуживает
18 независимо выдаваемых Mattermost capabilities: 16 типизированных MCP
операций чтения, поиска, файлов, reactions и сообщений, а также две системные
подписки `mattermost.inbound` и `mattermost.gate_decisions`. Системные подписки
не становятся вызываемыми агентом tools.

Метаданные подключений, grants, delivery attempts, inbound receipts, Runs и
Human Gates принадлежат `control-plane`. Идентификаторы Mattermost используются
только как внешние локаторы и audit metadata, но не как источник полномочий.
Gateway не читает PostgreSQL и не меняет core lifecycle самостоятельно.

Подтверждение входящего сообщения создаётся владельцем атомарно с receipt и
доставляется через fenced очередь в exact team/channel/thread. Listener не
отправляет отдельный ACK. Его поколение связано с версией подключения и полной
неизменяемой credential projection; замена ждёт завершения старого listener.

Исходящие доставки claim-ятся с fenced lease и завершаются отдельно от Run:
ошибка Mattermost не меняет успешный core Run на `FAILED`. Входящее сообщение
маршрутизируется только через единственный активный server-owned grant.
Решение Human Gate использует тот же one-winner/OCC contract, что и Control
Center, поэтому повтор с другой поверхности получает stale readback.

Неопределённая отправка фиксируется как `UNKNOWN_OUTCOME` без автоматического
повтора. Входящий gate reply подтверждается чтением post/root и точной связкой
gate/run/version; внешнего user identifier недостаточно без server-owned
привязки к субъекту Kodex. Детали жизненного цикла и оставшаяся область полного
unit описаны в [контракте #1030](../../../docs/operations/interaction-gateway-1030.md).

Пользовательский текст локализуется по locale подключения из embedded YAML.
Credential material читается только из точного server-mounted файла и не
попадает в API, логи или audit. Весь внешний трафик идёт через egress gateway к
hostname из deployment allowlist с TLS 1.3.

Проверка подключения использует тот же scoped HTTPS client, read-only Secret
key/content digest и реальный team/channel lookup, что рабочие операции.
MCP claims и connection tests обрабатываются только по отдельному
`InteractionGatewayOperations` профилю; generic integration worker их не
получает.

Connection test и MCP invocation проверяют private package bytes и точные
version/digest каждой claim. Опубликованная UI/Git revision может сужать
поставленный контракт; она не заменяет глобальный registry. Configuration,
input и deadline проверяются по выбранной revision. Автоматический health
check не выполняет операцию, которой требуется отдельный Human Gate.

`/healthz` отражает жизнь собственного процесса. `/readyz` читает локальный
снимок authority sidecar и не вызывает `control-plane` или Mattermost. Сбои
рабочего межсервисного и внешнего пути наблюдаются как отдельные переходы
degraded/recovered без влияния на Kubernetes readiness.
