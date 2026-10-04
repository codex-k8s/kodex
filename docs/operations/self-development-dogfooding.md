---
id: OPS-DOC-SELFDEV-001
title: Самонастройка и разработка Kodex средствами платформы
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-04
---

# Цель и источники

Полностью выполнить согласованное владельцем задание
`/home/s/projects/kodex/.agents/full-qa-task.md` (65 разделов), а не заменять
его обходом экранов. После самонастройки системного и проектного помощников
внутренняя ИИ-команда разрабатывает сам Kodex по реальной GitHub Issue.
Результат — отдельный PR, `READY_FOR_HUMAN_REVIEW`, без merge.

Исходный `main`: `d43bd605ec7b41335ec038a84a896b1ab5b0d189`, PR #1790 уже слит.
Связанное Issue: https://github.com/codex-k8s/kodex/issues/1797.
Ветка: `kodex-agent/issue-1797-self-development-bootstrap`.
Bootstrap PR: https://github.com/codex-k8s/kodex/pull/1798 (Draft).
Все новые платформенные изменения — в одном сквозном bootstrap PR как явно
разрешённое владельцем исключение из правила одного deployable unit.
Данный документ фиксирует дополнения владельца; полный исходный сценарий
сохраняется в `.agents/full-qa-task.md` и выполняется целиком.

## Решения владельца и режим

- Bootstrap PR разрешено сливать автономно после фактических проверок без
  нового owner gate; запрещено обходить GitHub protection/checks.
- Доработки внешнего host-агента не отправлять на отдельный review: быстрые
  адресные unit-тесты, необходимые сборка/codegen, затем живой сквозной QA с
  исправлением найденного. Незапущенные suites — NOT RUN, не PASS.
- Внутренние Documentation/Security/Lexical reviews — обязательная часть
  проверяемого Workflow и не отменены этим исключением.
- До живого QA разрешены основной агент и до пяти субагентов
  `gpt-6.1-sol`, reasoning `high`, в пределах фактического лимита инструментов.
  Разделять владение файлами; общий контракт интегрирует основной агент.
  В финальном Workflow host не подменяет сотрудников платформы.
- Рабочий клон `/home/s/projects/kodex` примонтирован в разрешённый локальный
  кластер; изменения проверять на hot reload. KUBECONFIG
  `/home/s/.kube/config`, context `k3d-kodex`; staging/production не затрагивать.
- Каждые 10–15 вызовов инструментов или 5–10 минут получать список вкладок
  Chrome MCP; дополнительно обновлять рабочую вкладку раз в 5 минут,
  предварительно сохраняя ввод. Чужие вкладки не закрывать.
- При согласованном bootstrap/dogfooding разрешены реальные ИИ-запуски и
  предусмотренные сценарием GitHub effects. STT/device-code не тестировать.
- Никакого legacy, двойных источников состояния и ручных обходов платформы.
  Applied migrations не менять, новые изменения forward-only.
- Секреты не показывать в prompts, аргументах, URL, логах, screenshots,
  документации и Git. Единый runtime источник host-секретов —
  `/home/s/.codex/agent-secrets.env`, regular file текущего владельца, 0600.
  Владелец явно разрешил разово получить из `/home/s/projects/kodex/.env`
  только `CODEX_GITHUB_AGENT_INTEGRATION_TOKEN` и
  `CODEX_GITHUB_AGENT_GIT_TOKEN`; не исполнять `.env` и не загружать остальные
  project variables. GIT_BOT_PAT — разработка/PR; GIT_OWNER_PAT — Issue и явно
  разрешённые owner операции; owner token агентам платформы не выдаётся.

## План с доказательствами

- [x] 1. Создать связанное Issue, ветку от свежего main и один Draft bootstrap
  PR; фиксировать результаты PASS/FAIL/NOT RUN/BLOCKED на точном SHA.
- [ ] 2. Полные управляемые MCP/tool profiles системного помощника,
  проектного помощника и каждого сотрудника; host Context7 reference,
  immutable RuntimeRevision, scoped Secret binding, exact network/readiness.
  Ключ Context7 доступен только доверенному MCP adapter/server, не shell агента.
- [ ] 3. Настраиваемая ApprovalPolicy grant: package default/allowed policies,
  durable/versioned/audited selected policy, CP/gateway/adapter/runtime pins.
  Collaborative GitHub writes допускают NONE только в разрешённом реестре;
  destructive операции не становятся автономными.
- [ ] 4. Сессия для её владельца отображается как переписка: пользовательские
  сообщения, публикуемые промежуточные сообщения и итоговые ответы агента.
  В общей хронологии показываются вызовы инструментов, название действия,
  статус и раскрываемые безопасные детали/результат, как в интерфейсе Codex.
  Работает для помощников, сотрудников, процессов и дочерних сессий; автора,
  session/turn/attempt нельзя перепутать. Realtime/rejoin/reload сохраняют
  порядок, сообщения и дедупликацию; длинный вывод сворачивается, прокрутка
  не прыгает. Секреты, сырые bearer headers и скрытые рассуждения не выводятся.
- [ ] 5. Безопасный observability/read path фактически materialized prompt:
  instructions, template variables, integrations, identity, tools/MCP,
  files, user/task input с harmless marker, model/reasoning и exact pins.
- [ ] 6. Общий admitted/promoted образ kodex-selfdev со всем требуемым
  инструментарием; отдельные execution workspaces, без общего mutable PVC.
- [ ] 7. System Assistant сам настраивает себя typed plan; подтверждение,
  публикация, Context7/web/GitHub read и prompt proof реальных ходов.
- [ ] 8. System Assistant создаёт Kodex | Dev и отдельного Project Assistant;
  authoritative ownership/version/audit readback; project isolation,
  Context7/repository/network/runtime/prompt proof.
- [ ] 9. Project Assistant создаёт шесть сотрудников (Manager, Architect,
  Developer, Documentation Reviewer, Security Reviewer, Lexical Guardian),
  selfdev-write/selfdev-review, Project Files/Secrets, GitHub connection и
  least-privilege grants. Raw git push token только Developer.
- [ ] 10. Проверить реальные тестовые ходы каждой роли, template validate/
  preview/publish/materialization, scoped grants, NONE writes, оба Human Gate
  режима, delegation и handoff через файлы/артефакты.
- [ ] 11. SOFTWARE_CHANGE: Manager → Architect → Developer → параллельные
  Documentation/Security/Lexical reviews → fixes/re-review → final Manager.
  Проверить небольшой disposable delegated run до настоящей Issue.
- [ ] 12. При bootstrap acceptance зафиксировать и автономно слить bootstrap
  PR, обновить стенд на свежий main и повторно сверить созданные ресурсы,
  migrations/source/Pod/image/runtime/realtime и prompt pins.
- [ ] 13. Manager выбирает #1796, если актуальна и имеет поддерживаемый
  upstream API; иначе следующую подходящую реальную Issue. Не scraping,
  не private undocumented endpoint и не выдуманные usage/credits.
- [ ] 14. Выполнить полный реальный Workflow силами команды Kodex; host
  проверяет каждый значимый transition и исправляет дефекты платформы,
  но не пишет финальную задачу вместо Developer и не подменяет reviewers.
- [ ] 15. Internal reviews/fixes/responses на exact SHA, final-readiness.md,
  финальный PR READY_FOR_HUMAN_REVIEW и отчёт по разделу 64 исходного задания.
  Этот PR не merge, не auto-merge, не approve от имени владельца.

## Карта новых пользовательских сценариев

| Сценарий | Authority и владелец состояния | Consumer / проверка |
| --- | --- | --- |
| MCP profile publish → turn | Проверенный actor/scope, CP immutable revision и secret metadata; trusted adapter получает только exact binding | Runner startup и штатный MCP call, Console/Network/runtime proof |
| Grant policy select → GitHub effect | Package allowed set и exact selected grant snapshot; CP owner transaction | Gateway/adapter membership check, grant pin, NONE/Human Gate negative cases |
| Runtime message/tool → transcript | Callback workload/session/turn/attempt, CP persisted event sequence; session eligibility из серверного read path | Scoped WebSocket и history/rejoin, owner transcript без secret leakage |
| Typed plan self-config → следующий ход | Owner confirmation, OCC/idempotency, immutable опубликованные pins | Runtime readback, actual prompt/tool/network proof; stale plan закрыто отклоняется |

Lifecycle cancel/delete/retry/terminal, deduplication и возможные частичные
переходы детализируются перед изменением соответствующих контрактов. Нельзя
считать зелёный Pod, скриншот или unit-тест доказательством живого Workflow.

### Жизненный цикл переписки и инструментов

| Переход | Проверка и атомарный результат владельца | История и потребитель |
| --- | --- | --- |
| Native/MCP tool started | Exact lease/fence/generation + session/turn/attempt/input/revision; stable call ref и revision 1, RUNNING, audit/event/receipt | Одна раскрываемая запись действия; сырые аргументы не выдаются |
| Published message completed | Только COMMENTARY/FINAL completed item, UTF-8 до 64 KiB, стабильный item ref; owner назначает execution/actor, immutable event | Полный текст в отдельном message body, не в сокращённом summary; reasoning исключён |
| Tool completed | Тот же call/execution, монотонная revision, неизменный тип и authority; bounded безопасный результат | Обновление той же записи SUCCEEDED/FAILED, исходные события неизменяемы |
| Exact replay / lost ACK | Тот же item/revision/content возвращает прежний receipt; иной content закрыто отклоняется | Дедупликация по immutable event и item/execution/revision |
| Cancel/delete/terminal/expiry | Прежняя owner-транзакция отзывает execution и закрывает незавершённые activity; stale callback не создаёт новых фактов | Сохранённая история остаётся доступна только по прежнему eligibility; отмена не превращается в успех |
| Retry/continuation | Новые turn/attempt и свежая RuntimeRevision, прежние items не переписываются | Exact tuple разделяет попытки и дочерние сессии |
| Rejoin/reload/gap | Прежний защищённый run event read и непрерывный cursor, без нового cache/authority | Порядок внутри Run по sequence; между assistant turns по owner turnNumber |
| UI consumer acquire/release | Независимый lease подписки в одном realtime store; logout очищает прежних владельцев | Закрытие модалки не отключает соседний экран; старый release не влияет на новую сессию |

## Журнал

04.10.2026: задания прочитаны, уточнения владельца внесены; код ещё не изменён,
новый живой QA не запускался. Два явно разрешённых ключа в проектном `.env`
присутствуют и перенесены в единый private source с readback 0600; значения
не выводились. Рабочая вкладка Chrome MCP доступна.

04.10.2026, bootstrap checkpoint `2c103867cf9b14bbcfd8ead0555f1f1dfcde691a`:
PASS — Issue #1797, ветка от подтверждённого main, Draft PR #1798 и совпадающий
GitHub head SHA. Реализация и новый живой QA пока NOT RUN.

Подтверждён разрыв переписки: runner принимает `commentary`, но не передаёт
его владельцу состояния; native tool calls сохраняются пакетом после хода,
а MCP — после завершения вызова. Исправление должно сохранять исходную
хронологию, точные session/turn/attempt и состояния вызова. Служебный summary
ограничен 2000 символами и не заменяет полный bounded текст сообщения.

04.10.2026, bootstrap в работе, base SHA
`4633e75c774758aa97f1364390b15ce000b31328`, изменённое дерево (ещё не immutable
release): typed MCP/Context7 profiles и новая upstream health receipt,
explicit grant policy, специализированный SYSTEM grant plan и организационные
решения; streaming COMMENTARY/FINAL и tool RUNNING → terminal вместо
пакетной публикации после хода; общий transcript и независимые Run read leases.

PASS — адресные disposable PostgreSQL health/activity проверки (4.955 s,
`/tmp/kodex-activity-health-pg-target3.log`), SYSTEM grants/typed plan (6.611 s,
`/tmp/kodex-system-assistant-grants-pg-target4.log`); runtimecontract,
runner app/callback/Codex/readiness, controller callback/workload, полный
HTTP gateway (10.189 s), адресные race/vet; frontend 229 адресных tests,
полный typecheck/build, scoped lint/format, authority и AsyncAPI codegen.
Предупреждения Vite о размере chunk не являются ошибкой сборки.

FAIL → исправление стенда — старый render повторно использовал завершённый
migration Job; его Complete не доказывает применение новых migrations.
Новый SDK сначала отсутствовал в read-only Pod module cache; штатный
repo-owned cache prime выполнен с Go 1.26.6. Нужны свежий render,
миграции и новый runner с exact source/image/admission readback.
Chrome рабочая вкладка обновляется; реальные ИИ-сценарии нового этапа,
визуальная приёмка transcript, STT/device-code/staging/production — NOT RUN.
Checkbox 2–15 пока не отмечены: synthetic PASS не заменяет живой dogfooding.

04.10.2026, то же изменённое дерево на base `4633e75c`:
PASS — повторный disposable PostgreSQL health/activity прогон (4.708 s,
`/tmp/kodex-activity-health-pg-target4.log`), включая отказ terminal tool с
revision 1; адресные CP/gRPC unit (0.054/0.043 s), полный integrationpackage
(3.086 s), runtimecontract (0.068 s), AsyncAPI model boundary.
FAIL — compound SYSTEM grant regression обнаружил, что `CancelRun` оставляет
READY integration invocation открытым. Исправление должно закрывать его в той
же owner-транзакции по семантике terminal graph, без обхода fixture или
ослабления проверки активной работы. Проверка исправления ещё NOT RUN.
Живой Chrome показывает unavailable до применения новых миграций;
backend log подтверждает bootstrap failure на новом adapter constraint.
Это не визуальный PASS; сохранение SSO-сессии пока UNKNOWN.

PASS — 13 герметичных fixture-проверок нового `render-current-local.sh`,
shellcheck и syntax checks. Скрипт связывает чистый HEAD с фактическими
trusted source mounts, читает точные image pins и свежий API endpoint,
создаёт новые приватные render/log; не выполняет apply/bootstrap и не
записывает authority state. Штатная подготовка caches существующим renderer
явно допускается. Реальное выполнение renderer ещё NOT RUN.

04.10.2026 07:38:42 UTC: PASS — read-only host→Pod SHA256 равен до/внутри/после
чтения для `runtime_activity.go`, `system_assistant_integration_endpoints.go`,
`RunTranscript.vue`, `callback/managed_mcp.go`. Проверены owner chains
Pod→ReplicaSet→Deployment, без выбора совпадающих по labels завершённых Jobs.
CP/GW/controller используют read-only root `/workspace`, frontend — свой
read-only subtree. CP NOT READY (bootstrap constraint), остальные три READY.
Доказательство относится к изменяемому дереву на этот момент, не к immutable
SHA, image acceptance или живому пользовательскому сценарию.

PASS — исправленный CancelRun и compound grant plan: actual disposable
PostgreSQL target8 (16.364 s, `/tmp/kodex-system-assistant-grants-pg-target8.log`).
SYSTEM/PROJECT READY invocation закрывается, начатый WRITE получает
UNKNOWN_OUTCOME, lease/fence снимаются, scoped approvals отзываются;
replay не дублирует receipt/audit/outbox, поздний completion отклоняется.
Пять разных capabilities одного connection применяются атомарно;
внешний конфликт откатывает все пять. Исходный target7 FAIL сохранён.
PASS — читаемые RU/EN подписи инструментов: ещё 20 frontend tests,
полный typecheck и scoped lint/format. Визуальная проверка пока NOT RUN.

PASS — финальный disposable PostgreSQL target10 (12.594 s,
`/tmp/kodex-system-assistant-grants-pg-target10.log`): Type30 объявляется в
fresh SYSTEM create/read/rejoin и immutable runtime context только при
точной текущей authority. PROJECT и отозванный owner не получают действие.
Повтор exact grant в одной revision не проходит VALID; пять разных grants
по-прежнему применяются атомарно. Добавлена forward-only миграция 007.
Исходный target9 fixture FAIL сохранён; текущие compile/diff checks PASS.
Владелец самостоятельно восстановил SSO, повторную авторизацию не выполняли.

04.10.2026 08:06–08:14 UTC, checkpoint
`b1dca181a546b5600226c1e0f27882f9c8c297d4`:
PASS — новый runner собран repo-owned скриптом, exact image digest
`sha256:f0edb963a68c42dbdb8fc90b8795de5b7a66c991fc659f6404e6ca99d58b118e`;
свежий private render, миграции фактически до `20261004000700`, supply-chain
readback и новая runtime contract revision 2. Все 29 публичных admission
policy fields совпали с render; exact source/host/Pod hashes для CP/GW/FE и
controller. CP/GW/FE/IG/controller READY на новых Pod. Первый rollout GW
FAIL по timeout подключения NATS, после штатного startup restart READY;
первоначальный FAIL не заменён задним числом.

PASS — адресные runner tests повторены на exact checkpoint (Codex 4.505 s,
app 14.637 s, callback 0.149 s, readiness 0.053 s). Browser SSO callback,
bootstrap и session ticket HTTP 200, Console без ошибок. Вход завершён
существующей SSO-сессией, без ввода пароля и device-code.

FAIL — первый реальный SYSTEM self-configuration turn
`run_jb9Xo0PXGGRigLOrqCgAf_Fi` остановился до inference на ACCOUNT_READ.
Offline stable schema установленного Codex 0.160.0 содержит `promax`,
которого не было в строгом decoder. Исправление enum и positive/negative
unit PASS; связь именно с этим live failure ещё UNKNOWN до повторного хода.
FAIL — hard reload после первого SYSTEM run зацикливает realtime reducer:
организация bootstrap ещё не загружена при обработке RUN snapshot.
Нельзя ослаблять owner pin check; исправляется порядок authoritative bootstrap.
На скриншоте также технические progress codes и дублирующий terminal summary;
требуется адресное исправление представления и повторный визуальный readback.
Чекбоксы 2–15 остаются неотмеченными: живой self-configuration ещё не выполнен.

04.10.2026 08:19–08:26 UTC, исправляемое дерево после checkpoint `b1dca181`:
PASS — повторный Chrome hard reload восстанавливает SYSTEM transcript,
WebSocket показывает «Подключено», Console без ошибок; bootstrap/session,
run graph/history HTTP 200. Скриншот
`/tmp/kodex-system-history-fixed-hot.png`: progress/failure локализованы,
terminal ошибка не дублируется неподписанным summary. 95 адресных frontend
tests, полный typecheck/scoped lint/format соответствующей области PASS.
Новые bootstrap ordering/one-shot transfer tests ещё проверяются; начальный
build на редактируемых fixtures FAIL и требует повторного запуска после freeze.
PASS — полный runner Codex (4.501 s), app (14.738 s), callback (0.154 s),
readiness (0.050 s). Для диагностики добавлен закрытый класс отказа без
сырого ответа, account metadata или provider error text в логах.
Новый runner ещё не активирован; успешный реальный повторный ход NOT RUN.

04.10.2026 08:30 UTC, финальное дерево перед фиксацией:
PASS — bootstrap ordering и однократная передача снимка: 110 адресных tests
в 5 файлах, полный typecheck, scoped ESLint и Prettier. Возврат из публичного
раздела сохраняет свежий realtime snapshot; foreign organization и отменённый
probe закрыто отклоняются. Полная frontend build повторена после freeze:
PASS (7.29 s), предупреждение о размере bundle сохранено.
Живой повтор SYSTEM turn после нового image digest ещё NOT RUN.

04.10.2026 08:38–08:42 UTC, checkpoint
`7fb8340b4e2427e733f6808e73c95db2b349da1a`:
PASS — runner exact digest
`sha256:d30f832f76fb2527ba4f147f6a0a3e98127861d911a719db35f94481183cff2c`,
fresh render и supply-chain/core repo-owned activation. CP/GW/FE/controller
READY с аннотацией этого SHA; source mounts и hashes проверены через точную
Pod→RS→Deployment UID chain, warm spec.image=imageID нового digest.
FAIL — повтор SYSTEM self-configuration
`run_WR3j6LlwtTRzOWVGHacgUtEG`: ACCOUNT_READ, закрытый класс PROVIDER.
Это не ACCOUNT_RESPONSE_SCHEMA; исходная причина не доказана. Ни план, ни
успешный inference не созданы. Добавляется безопасный closed detail/RPC code,
без сырых account/upstream diagnostics. Live повтор после диагностики NOT RUN.

По просьбе владельца в hot-reload дереве изменена общая чатовая вёрстка:
USER справа, сообщения агента/инструменты/статусы слева; adaptive width,
long text wrapping. PASS — 79 unit tests, typecheck/scoped lint/format,
Chrome screenshot `/tmp/kodex-chat-bubbles-hot.png`, Console без ошибок.
Source hashes host=Pod подтверждены; это dirty hot-reload, не immutable
image/commit acceptance. Старый SYSTEM failure сохранён в истории.

04.10.2026 08:50–08:52 UTC, hot-reload дерево после `7fb8340b`:
PASS — точный provider discovery GET
`chatgpt.com/backend-api/wham/accounts/check` подтверждён первичным
[кодом Codex rust-v0.160.0](https://github.com/openai/codex/blob/rust-v0.160.0/codex-rs/backend-client/src/client.rs#L398-L405).
Provider grant не расширяет пользовательский WebAccess; соседние paths,
методы/hosts/WebSocket закрыты. Runtimecontract unit/full/race/vet PASS.
PASS — полный egress gateway tests (0.228 s) и observability (0.007 s);
новые закрытые metric labels сохраняют unknown для произвольных значений.
Первый новый gateway target до добавления discovery route был FAIL, финальный
target PASS. Safe provider call diagnostics unit PASS (4.591 s), frontend
build после чатовой вёрстки PASS (7.29 s).
FAIL — третий живой self-configuration
`run_dL83A_39p_6phhSj1-gKsiuY`: ACCOUNT_READ/PROVIDER сохранился; добавление
discovery route само по себе не устранило отказ. Модель/plan ещё NOT RUN.
Прокси hot-reload обновлён; точный safe RPC detail требует нового runner image.
Метка `QA_SELF_CONFIG_20261004_0852` отправлена штатным UI, materialized input
ещё не доказан. Необязательный config/bundle без actual trace не разрешён.

04.10.2026 09:01–09:06 UTC, checkpoint
`a2a2e55290c1fb0ff943831fe77835761528e4be`:
PASS — новый runner exact digest
`sha256:eb2275008e8168c19baafb68fff780e2e103049d1ca11b27cc08bc7ed3ac8dca`,
repo-owned build/render/supply-chain activation. CP/controller/egress READY;
warm и фактический turn Pod обслуживали этот digest.
FAIL — четвёртый SYSTEM self-configuration
`run_isAFDbBo30PgUJGL5xhTfion`: ACCOUNT_READ, PROVIDER, RPC_ERROR,
JSON-RPC code `-32603`, notification NONE. Это ответ app-server, не ошибка
нашего account schema decoder. План и inference ещё NOT RUN.
Попытка прочитать только безопасные materialized pins через exec не успела
до завершения контейнеров: NOT RUN, не доказательство prompt delivery.
PASS — Chrome reload, bootstrap/session/ticket/graph/history HTTP 200,
Console без ошибок. Скриншот `/tmp/kodex-system-account-read-diagnostic.png`
подтверждает USER справа, агент/прогресс/terminal слева и отсутствие дубля
terminal ошибки. Чекбоксы 2–15 остаются открытыми.
Следующий адресный шаг — точное сопоставление статических workspace-routing
ошибок закреплённого Codex 0.160.0 с закрытым enum, без raw message/data,
account metadata или credentials в логах. Сам код `-32603` ещё не доказывает
конкретную сетевую либо конфигурационную причину.

04.10.2026 09:08–09:11 UTC, исправляемое дерево после `a2a2e552`:
PASS — runner account/read reason classifier: 18 точных статических причин
официального `rust-v0.160.0`, строгие method/code/schema/size boundaries,
неизвестное UNKNOWN, raw message/data не сохраняются. Полный Codex 4.500 s,
адресный race 1.101 s и vet PASS. Egress diagnostics теперь различают
точный provider discovery, policy deny, ответ upstream и DNS/DIAL/TLS/
timeout; labels/логи закрытые, неизвестные paths не записываются. Полный
gateway 0.242 s, synthetic redaction/HTTP403/503/DNS/TLS cases PASS.
PASS — подготовлен repo-owned `tools/dev/protected-secret-input.mjs` для
credential уже созданного Context7/GitHub connection. Приватный canonical
reader, отдельный in-memory SSO browser, exact origins/owner/catalog/version,
один PUT с OCC/idempotency, UNKNOWN outcome без повторной отправки. 40 Node
tests, syntax/format PASS. Реальные secrets/SSO/effects helper ещё NOT RUN;
runtime-secret operation этой оснасткой закрыто отклоняется. Это не подмена
самонастройки: подключение и план должен создать настоящий помощник.
Новый runner build/activation и повторный live turn с закрытой причиной
ещё NOT RUN на момент фиксации этого checkpoint.

04.10.2026 09:12–09:26 UTC, checkpoint
`d0f283cc2c1990d47f7fcfb376b09cae2bccd430`:
PASS — repo-owned runner build: exact digest
`sha256:2e31bf59610ee5c436f60e5c3ef10ce0cfdf90192af4a2993c3c185610a3dccf`,
binary SHA256 `20ad03b676ae2179548a0d3c965413729f6bcb5e24b934cebfbdadeac9994537`.
Первый fresh render FAIL: GO_TOOLCHAIN_MISMATCH; повтор с явным Go 1.26.6
PASS. Supply-chain, CP и egress activation PASS; warm и turn imageID
соответствуют новому digest. Подмена версии runner не использована.
FAIL — пятый–седьмой SYSTEM turn: account/read по-прежнему отклоняется;
новая причина DISCOVERY_FAILED, RPC_ERROR/-32603, notification NONE.
Для пятого и шестого turn через фактический provider Pod доказаны SYSTEM
scope, session/turn/runtime/image pins, совпадение инструкций и наличие
`QA_SELF_CONFIG_20261004_0852` в materialized prompt/AGENTS.md. Auth не читался;
MCP/integration grants пока 0, потому что самонастройка не выполнена.
PASS — найден и воспроизведён отдельный proxy defect: HTTP/2 upstream
сериализовался строкой HTTP/2.0 в HTTP/1.1 downstream. Regression сначала
FAIL, после исправления PASS; весь egress module PASS. Hot host/Pod source
hash совпал. Седьмой turn всё ещё FAIL, но три provider discovery запроса
получили 2XX и UPSTREAM_BODY COMPLETED. Этот defect не объявляется единственной
причиной отказа; успешные headers/body не доказывают SDK decode или inference.

04.10.2026 09:31–09:34 UTC, hot-reload дерево после `d0f283cc`:
PASS — ограниченная диагностика ответа accounts/check: только закрытые
encoding/content-type/schema enums, буфер не более 1 MiB + 1 байт,
обнуление до итоговых логов; фактический поток и headers не изменяются.
Никакие account values, keys, decoder errors или хеши ответа не выдаются.
Адресные proxy tests PASS (0.140 s), полный egress module PASS; shape helper
targeted/full/race/vet PASS. Helper проверяет типы/defaults закреплённого
SDK, а не authorization/routing или полную эквивалентность Rust decoder.
FAIL — восьмой живой turn `run_T-5N0L5X6RkZaRRYA-gNVO1E`:
ACCOUNT_READ/DISCOVERY_FAILED сохранился. Все три upstream ответа:
2XX, body COMPLETED, encoding IDENTITY, type JSON, SCHEMA_OK_LIST.
Actual warm binary отдельно подтвердил codex-cli 0.160.0.
Следующая гипотеза — downstream framing/закрытие TLS для unknown-length
upstream body; raw account response и credentials не извлекаются.
Полная самонастройка, inference и дальнейший dogfooding ещё NOT RUN;
чекбоксы 2–15 остаются открытыми.

04.10.2026 09:38–09:41 UTC, hot-reload дерево после `d0f283cc`:
FAIL→PASS — streaming HTTP/2 fixture без Content-Length воспроизвёл
close-delimited downstream и потерю trailers при формально COMPLETED body.
Исправление задаёт явный HTTP/1.1 chunked для unknown-length body; исключены
HEAD, no-body statuses и upgrade. Полный egress module PASS (gateway 0.384 s),
повторный regression PASS (0.038 s), адресный race PASS (1.976 s), vet PASS.
Hash proxy host=Pod:
`bb63ed8c25cb1a527a1ee90719cc71028735769a710be1c262c5c18a5ccf1089`.
PASS — девятый turn `run_KeumAt2ApaWMlTDH9OFY3cJt` больше не получил
DISCOVERY_FAILED/RPC_ERROR: app-server успешно вернул account/read после
одного accounts/check 2XX, завершённого body, IDENTITY/JSON/SCHEMA_OK_LIST.
FAIL — следующий отдельный boundary: ACCOUNT_RESPONSE_SCHEMA в нашем
адаптере. Проверяется строгая схема закреплённого SDK, включая добавленный
workspaceRouting. Это не успешный inference/self-configuration.
Chrome Console без ошибок, bootstrap/session/ticket/graph/history HTTP 200.

04.10.2026 09:42–09:46 UTC, финальное дерево перед следующим checkpoint:
PASS — установленный warm binary выполнил только generate-json-schema:
обычный и experimental GetAccountResponse подтверждают точный тип
workspaceRouting; actual inference/auth операции при codegen не выполнялись.
FAIL→PASS — synthetic account/read с workspaceRouting object/null отвергался
прежним адаптером; теперь принимается строгое optional поле с тремя
обязательными bounded strings и закрытым routing enum. Unknown/duplicate/
missing/wrong-type/over-limit закрыто отклоняются, значения не сохраняются
и не назначают account authority или сетевые grants. Отсутствие аккаунта
по-прежнему приводит к authentication failure. Experimental capability
не менялась; account/updated по первичному SDK содержит только прежние поля.
Полный Codex PASS (4.474 s), targeted PASS (0.015 s), race PASS (1.086 s),
vet/diffcheck PASS. Новый runner activation и живой повтор пока NOT RUN.
