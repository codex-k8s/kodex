---
id: QA-INTEGRATION-GATE-1075
title: Авторитетное намерение и последствия Human Gate
type: verification-plan
status: approved
owner: developer
version: 1.1.0
updated: 2026-10-25
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

## Замечание владельца в продолжении #1863

Для позднего решения после завершения исходного execution CP читает из
авторитетного Gate точные `ref`, разрешённую `version`, `decision` и сохранённый
`decision_comment` в той же транзакции, которая создаёт `session_turns` и ребро
`CONTINUES`. JSON задачи содержит отдельный блок `owner_decision`: замечание
владельца не смешивается с недоверенным `result_summary` провайдера и повторно
не усекается. Ограничение записи самого Gate остаётся прежним — 2000 символов.

Цепочка потребления: `ResolveOwnerGate` либо terminal completion integration
worker → `scheduleIntegrationContinuation` → новая AGENT turn той же Session →
`ClaimExecution` → `RuntimeRevision.safe_snapshot.task` и digest → существующий
`RunnerInput.Task` → prompt сотрудника. Перезапуск CP не требует повторного чтения
изменяемого Gate для уже созданного runtime input. Изменения внешнего Proto и
строгого формата runner не нужны; доставка затрагивает только CP, без миграции.
Уже созданные задачи и RuntimeRevision не переписываются; replay прежнего
receipt не создаёт новую задачу и не является способом повторить WRITE.

| Состояние | Продолжение и полномочия |
| --- | --- |
| Gate OPEN, invocation WAITING_APPROVAL | Продолжения нет; worker не получает effect. |
| APPROVE, invocation READY/CLAIMED | Продолжение ожидает terminal результат effect. |
| APPROVE, invocation SUCCEEDED/FAILED после завершения исходного execution | Одна новая turn с точными pins решения, комментарием и terminal результатом. Ошибка провайдера не объявляется успехом. |
| REJECT/CANCEL после завершения исходного execution | Одна новая turn с явным отказом/отменой, замечанием и запретом повторять отклонённое действие. |
| Пустое замечание | Сохраняется пустая строка; причина не придумывается. |
| Replay того же решения | Прежний receipt, без второго CONTINUES или нового effect. |
| Устаревшая версия либо чужой project/tenant | Прежние OCC и owner boundary отклоняют команду до изменения состояния. |
| Terminal root, отмена проекта, неподходящий predecessor | Root не открывается; продолжение не создаётся. |

Замечание может задавать следующую разрешённую работу (например, сохранить
паспорт и canonical input), но не выдаёт grant и не разрешает повторный WRITE.
Публичные Run graph/events/session messages показывают прежний типизированный
`CALLBACK_CONTINUATION_PUBLIC`, а не внутреннюю задачу.

Системные аналоги: `REQUEST_CHANGES` обычного Gate уже записывает замечание в
отдельную USER turn; его lifecycle не меняется. Обычные REJECT/CANCEL и Gate
доставки Interaction сохраняют собственные terminal-правила. Прямой ответ
`GetIntegrationInvocation`/MCP содержит outcome и gate ref, но не новый блок
замечания; #1863 передаёт его через immutable continuation path, а не объявляет
прямой ответ полным источником owner feedback. Решение до завершения исходного
execution не создаёт это позднее продолжение; этот контракт не меняется.

Адресная локальная проверка (только disposable PostgreSQL):

```sh
GOTOOLCHAIN=go1.26.6 GOMAXPROCS=2 KODEX_TEST_POSTGRES_NETWORK=host \
  timeout --signal=TERM --kill-after=20s 240s \
  scripts/tests/control-plane-postgres-test.sh '^TestIntegrationGateFeedbackComponent$'
```

Штатный runner использует `-p 2`; setup/migrations имеют общий внешний бюджет,
сама component fixture ограничена 90 секундами. EXIT trap останавливает только
собственный disposable контейнер. Проверка не вызывает модель или GitHub.
Ручная приёмка после отдельной доставки CP: отклонить integration WRITE с
замечанием о конкретном разрешённом следующем действии; проверить сохранённый
комментарий Gate, новую Session turn, точные pins runtime и выполнение
запрошенной работы без повторного WRITE. Локальные проверки не заменяют эту
приёмку и не доказывают послушность модели.
