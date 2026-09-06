---
id: QA-INTEGRATION-GATE-1075
title: Авторитетное намерение и последствия Human Gate
type: verification-plan
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-06
---

# Intent и последствия #1075

Источники: MVP-UI-41/42, #1046, bug #1075, GUIDE-DOC-006.
Существующие OwnerGate/IntegrationIntent не требуют нового API или policy.

| Путь | Owner boundary и результат |
| --- | --- |
| ResolveIntegrationInvocation | Проверенная runtime authority, exact grant и package; immutable input/digest/scope/effect key сохраняются до Gate. |
| Get/ListOwnerGate | Exact organization, Gate, project membership и подписанный project scope, включая OWNER/ADMINISTRATOR; одинаковая граница применяется до count/page. Один transaction snapshot содержит Gate и его intent. |
| ResolveOwnerGate | Свежая команда owner с OCC/idempotency; APPROVE разрешает effect, REJECT/CANCEL отменяют только invocation, не весь Run. |
| Обычный Gate | Intent отсутствует; REJECT завершает Run ошибкой, CANCEL отменяет Run, REQUEST_CHANGES создаёт продолжение предыдущего шага. |
| Interaction delivery Gate | APPROVE разрешает доставку; REJECT/CANCEL не меняют terminal Run. Это отдельный owner lifecycle, не integration invocation. |
| Event и receipt replay | Сохраняют безопасный intent; выдача заново проверяет tenant/actor/project. Терминальное состояние не удаляет историческое намерение. |
| SourceAttachmentSet | Exact predecessor node → turn → attachment set; actor read требует project.view. Не используется последний turn Session. |

Все действующие события OWNER_GATE_OPENED/OWNER_GATE_RESOLVED сохраняются;
нового события нет. Дополнительные сведения возвращают существующие защищённые
Get/List/Resolve и Run event read paths.

Resolve проверяет подписанный project scope до OCC и receipt replay. Для
терминального Gate признак завершения Run определяется сохранённым событием
OWNER_GATE_RESOLVED, а не текущим набором активных узлов. Сохранённые последствия
в event/receipt не пересчитываются при последующем продолжении Run. Пользовательские
summary передаются семью ключами GATE_CONSEQUENCE_* для согласованных ru/en каталогов.

Preview извлекается из сохранённого input только после проверки его SHA-256.
Известная shipped operation и её InputFields ограничивают набор полей;
пакет UI/GIT может сужать, но не расширять этот набор. Адресаты, target/action,
OCC и текст показываются как plain text/scalars, не исполняемый HTML.
Лимиты: 4 KiB UTF-8 на поле, 16 KiB текста суммарно, максимум 24 поля.
Каждое усечённое поле имеет truncated=true; contentComplete=false запрещает
выдавать bounded preview за полный документ. Exact inputDigest остаётся общим.

attachments, content_base64, workflow_inputs, nested JSON, credentials,
password/token/header fields не раскрываются. Opaque descriptor содержит только
имя, тип и размер; ссылка на source artifact не выдумывается. Неизвестная
operation/поле не получает произвольную JSON-проекцию. Connection configuration
и credential bindings не читаются этим preview. Исторический intent не требует
current enabled connection/package version и не превращает approval в success.

Локальные fixtures синтетические. Реальная почта, провайдер и staging не
используются; их приёмка остаётся отдельной строкой общего плана #1031.

Локальная проверка `make test-control-plane-postgres`: Bootstrap PASS 35.159s,
Avatar PASS 0.311s. Проверены open/terminal/list/event/replay, чужой tenant и
подписанный project scope OWNER, точный predecessor attachment set и устойчивость
исторических последствий к появлению активных узлов. Два предыдущих прогона
завершились FAIL в новых fixtures: пропущенный ResolvePrincipal и попытка
перевести PLANNED-узел в RUNNING вопреки materialization constraint. Fixtures
исправлены; production constraints не ослаблялись. Через Context7 проверены
документы pgx о read-only RepeatableRead, завершении transaction и ErrNoRows.

На том же дереве production-кода полный CP `go test -race ./...`, `go vet ./...`,
`go build ./...`, `make check-sql-boundary` и `git diff --check` завершены PASS.
Proto, generated code и policy не менялись. Общая HTTP/PWA приёмка и review
фиксируются отдельно на согласованном интеграционном SHA.
