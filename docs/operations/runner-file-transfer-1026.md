---
id: OPS-RUNNER-FILE-TRANSFER-1026
title: Ограниченное чтение файлов через runner
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# Сквозной путь

Refs #1026, PR #1058, Epic #1018; MVP-UI-37/61. Prerequisite controller
297ea7a7e3be7233f521e5b4ded3c85a482c6a48 содержит CP policy71 и проверенный
stream/spool. Runner получает только immutable RuntimeRevision и relative
download descriptor из metadata. Ключи provider не участвуют в чтении файла.

| Переход | Authority и bound input | Результат и cleanup |
| --- | --- | --- |
| Init Input/Skill | exact RuntimeRevision artifact/Skill pin → отдельный file HTTP client → TLS1.3/mTLS и execution ticket | Проверены status/media/size/SHA256; прежняя материализация публикует файл только после успешного чтения |
| Catalog download | Provider local bearer → loopback GET bridge → SO_PEERCRED UDS → trusted runner callback client | Только относительный путь exact lease/artifact и четыре selectors purpose/entry_ref/revision/digest; catalog purpose принадлежит RuntimeRevision |
| Owner read | Controller проверяет execution headers/ticket, metadata через CP catalog и StreamExecutionArtifact | Private controller spool проверен до HTTP200; runtime input/Skill/catalog current eligibility принадлежит CP |
| Delivery | Bounded body до512MiB, exact Content-Length/digest, фиксированные safe response headers | Постоянный буфер; provider не получает ticket, Cookie, Set-Cookie или object-store locator |
| Invalid/redirect | Unknown selectors, другой lease/catalog purpose, дубликат/неcanonical revision, чужой local bearer | Отказ до upstream; redirect не выполняется, скрытого retry нет |
| Cancel/deadline/disconnect | Context запроса связан с жизненным циклом execution | HTTP body закрывается; partial delivery обрывается, новый artifact/receipt не создаётся |
| Shutdown | Cancel/join прежнего runner lifecycle | Bridge server закрывается, UDS удаляется; оба HTTP transport освобождают idle connections |

Чтение не меняет domain state и не публикует событие. Повтор требует нового
авторизованного чтения CP; completed/current eligibility не кэшируется в runner.
Форма descriptor не является authority: exact entry/revision/digest и текущий
доступ повторно разрешает controller/CP.

# Ограничения

Общий command client сохраняет35s, его transport header30s. File client
использует отдельный clone transport: header timeout равен максимальному
controller spool2min +15s (metadata допускает10s), полный запрос — двум
transfer budgets +30s (metadata, spool и delivery). Более короткий
execution/request context сохраняет приоритет. Обе proxy ступени устанавливают
per-response socket write deadline; медленный downstream не обходит timeout,
блокируя запись после получения upstream body.
Skill остаётся ограничен32MiB, input/catalog —512MiB; большие JSON/MCP envelopes
не вводятся. Header bound16KiB, automatic redirects запрещены.

Provider получает URL относительно того же authenticated MCP origin;
произвольный URL/path, project, token в query, иной HTTP method и request body
запрещены. Пересылаемый trusted запрос создаётся заново из server-owned base
URL и execution headers. Общие MCP/command deadlines не увеличены.

Context7 `/golang/go`: проверены http.Client.Timeout (включает body),
Transport.ResponseHeaderTimeout, CheckRedirect/ErrUseLastResponse и context
cancellation. Источник: https://github.com/golang/go/blob/master/src/net/http/client.go
и https://github.com/golang/go/blob/master/src/net/http/transport.go.

# Проверки

Новый non-root protocol fixture проходит фактические loopback→UDS→TLS1.3/mTLS
ступени под UID10002/GID29000, передаёт33MiB+7 с checksum и проверяет отсутствие
upstream для чужих bearer/lease/selectors. Отдельные tests покрывают descriptors,
redirect/oversize/digest, отмену активного body и независимый file budget.
PASS: полный runner race (app9.912s, callback1.170s, codex6.317s,
contextfiles7.463s и остальные packages), полный vet/build; публичный
make test-agent-runner, web-only/optional Mattermost render, policy71/ABI.
После уточнения metadata budget повтор четырёх затронутых package race
PASS1.015/1.166/6.323s. В protocol fixture действительно используются
UID10002/GID29000, SO_PEERCRED, local bearer и проверка peer certificate.

Исторические FAIL сохранены: длинный Go temporary path превысил Linux Unix
socket limit в старом credentialrelay test; повтор с коротким private
TMPDIR/GOTMPDIR прошёл. Новый cancellation fixture сначала отменял context
до первого downstream byte; синхронизация исправлена на фактическую запись,
после чего abort и закрытие upstream подтверждены. Production guards
ради этих fixtures не ослаблялись.

Live/provider, staging/production, новый полный Docker image и общий
интеграционный gate — NOT RUN; синтетическая оснастка их не заменяет.
