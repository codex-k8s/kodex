---
id: OPS-DOC-1239
title: Окружения, секреты и автоматизации MVP
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-08
---

# Окружения, секреты и автоматизации MVP

Документ связывает #1239 и строки MVP-UI-43–50 общей приёмки
[общей приёмки](mvp-1031-acceptance.md) с существующими исполняемыми путями.
Владелец разрешил тематический PR нескольких компонентов и локальные проверки.
Точные SHA, команды и фактические результаты приводятся в PR задачи #1239; этот
документ не объявляет локальные fixtures приёмкой развёрнутой среды.

## Матрица требований и доказательств

Пути `PWA`, `CP`, `GW` ниже означают соответственно
`services/staff/control-center/src`, `services/internal/control-plane/internal`,
`services/external/control-api-gateway/internal`.

| Требование | Текущий рабочий путь                                                                                                                                                                                                                                                                                                                                                                | Локальное доказательство                                                                                                                                                                                                                                                                                                                                                                          | Остаток после разрешённого deploy                                                                                                                                                                           |
| ---------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| MVP-UI-43  | PWA `features/automations/AutomationEditorDialog.vue` хранит preset/custom, task и timezone; schedule preview и CP `domain/service/schedule/next.go` используют один parser пяти полей. CP `repository/postgres/platform/prompt_schedule_preview.go`, `prompt_schedule_runtime.go` и scheduler job материализуют immutable occurrence и prompt.                                     | `schedule/next_test.go`, `minimum_interval_test.go`: пять occurrence, DST gap/fold, минимум одна минута, отказ seconds/descriptors; scheduler `internal/app/app_test.go`: exact attempt key, lease и retry. PG `schedule_readback`, `durable_schedule`, `schedule_race`: AGENT/WORKFLOW prompt, preview, CONTINUE_ONE, race/retry и expiry.                                                       | NOT RUN: реальное расписание и materialized prompt через deployed gateway/runner; другой manager, revoke автора, first/bound Session, DST на обслуживаемой версии.                                          |
| MVP-UI-44  | PWA `pages/RuntimeEnvironmentsPage.vue`: пустой выбор, click/repeat/outside/Escape, double click и ссылка редактора. Запросы readiness/agents принадлежат выбранному ref и AbortSignal.                                                                                                                                                                                             | `RuntimeEnvironmentsPage.test.ts`: отсутствие ранних запросов, Escape вне registry, cleanup listener, уважение обработанного Escape; `features/runtime/store.test.ts`: отмена и stale state.                                                                                                                                                                                                      | NOT RUN: browser click/double click, action isolation, смена проекта/search/route и фактическая отмена запросов.                                                                                            |
| MVP-UI-45  | PWA `pages/RuntimeEnvironmentEditorPage.vue`: вкладки GENERAL/IMAGE_TOOLS/VALUES/SECRETS/POLICY/READINESS и отдельные lifecycle actions. Выбор образа очищает прежние tools, запрос имеет generation, project/environment/artifact pins и отменяется при смене выбора/unmount.                                                                                                      | `RuntimeEnvironmentEditorPage.test.ts`: старые success/error не заменяют новый образ, старый route load не перезаписывает новую форму, unmount отменяет запрос; environment-form/capabilities unit и production build.                                                                                                                                                                            | NOT RUN: desktop/mobile tabs, клавиатура и фактические disabled reasons каждого ограничения/permission/re-auth.                                                                                             |
| MVP-UI-46  | PWA `features/runtime/environment-drafts.ts` и редактор разделяют локальный ввод, server draft, validation и publication; GW environment draft endpoints направляют специализированные CP команды. Secret PWA draft flow идёт через GW и secret-broker к CP owner; ciphertext staging отделён от runtime active descriptor.                                                         | PG `runtime_environment_draft_publication`, `runtime_environment_privileged`, `runtime_secret_lifecycle`, `runtime_secret_drafts`: OCC, fresh authority, exact validation, draft save/discard, staged cleanup fences. Broker `internal/domain/service/secretdraft` и stagingcrypto/storage tests; PWA draft/reauth tests.                                                                         | NOT RUN: dirty navigation с save/discard/stay и re-auth в браузере; реальные Kubernetes encrypted staged/active resources, rotation, retention и cleanup после crash.                                       |
| MVP-UI-47  | CP `environment_draft_impact.go` и `runtime_secret_draft_impact*.go`: immutable actor-bound plan с exact source/target и binding versions. Prepare не публикует. Publish валидирует selected item refs и fresh permission, применяет допустимые bindings с отдельными receipts. PWA `PublicationImpactSelection.vue` и `RuntimeSecretDraftImpact.vue` показывают выбор и результат. | PG `environment_draft_impact_component_test.go`: два consumer, APPLIED/NOT_SELECTED, pagination/search, expired plan, foreign checkbox, exact replay, CONFLICT/FORBIDDEN. `runtime_secret_draft_impact_component_test.go`: два consumer с APPLIED/CONFLICT и exact result binding/revision; `secret_impact_component_test.go`: pinned running revision и retention. PWA publication/impact tests. | NOT RUN: browser impact modal с непустыми consumers, группировкой и server search/cursor; cancel/без замены/выборочная замена; running attempt на старой revision и следующий turn с новой revision/notice. |
| MVP-UI-48  | Существующие generated GW handlers и CP RPC обслуживают `/api/v1/runtime-environments/{environmentRef}/readiness` и `/agents`. PWA store очищает operational state при authoritative NOT_FOUND, catalog снимает выбор и делает bounded refresh.                                                                                                                                     | GW `identity_environment_endpoints_test.go`, CP transport tests, PWA runtime store tests: typed readiness, страницы, hidden/not-found, dependency errors и отмена. PG environment lifecycle подтверждает доступность owner-ресурса.                                                                                                                                                               | NOT RUN: exact deployed route/render, ready/not-ready, пустая и следующая страницы, скрытый/удалённый ref; dependency failure не должен стать ложным 404.                                                   |
| MVP-UI-49  | PWA `RuntimeSecretValueDialog.vue` использует textarea STRING/BINARY, общий sensitive CodeEditor для раскрытого JSON, type-change confirmation, локальный временный reveal и очистку plaintext. `features/runtime-secrets/model.ts` и shared Go `runtimesecret` валидируют тип/размер/encoding.                                                                                     | PWA model, draft-dialog и reveal-flow tests; broker staged encryption/validation tests. Введённый plaintext остаётся только в локальном pending mutation до exact receipt либо явного закрытия, без сериализации в store/storage/telemetry.                                                                                                                                                       | NOT RUN: browser Base64 padding/whitespace/decoded size, JSON diagnostics/format/Tab/Shift+Tab, смена типа и plaintext cleanup при success/close/unmount.                                                   |
| MVP-UI-50  | PWA runtime-secrets model/store нормализуют page до reactive state, сохраняют прежние строки при read failure и проверяют committed metadata receipt; bounded read-after-write использует exact secret/project/ref/version. Draft retry хранит один исходный idempotency key и очищает payload после ACK.                                                                           | PWA `model.test.ts`, `store.test.ts`, `RuntimeSecretDraftDialog.test.ts`, `draft-api.test.ts`: null/undefined/malformed page, lag, error, retry, re-auth и mutation serialization. Broker/CP crash-consistency fixtures проверяют один authoritative результат.                                                                                                                                   | NOT RUN: create/rotate/reload без Vue console errors в настоящем браузере, потеря ответа HTTP и projection lag на развёрнутом контуре.                                                                      |

## Жизненный цикл и authority

| Сценарий             | Инициатор и полномочия                                                                                                      | Владелец состояния и переход                                                                                                                                      | Идемпотентность и результат                                                                                                                                  |
| -------------------- | --------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Environment draft    | Аутентифицированный actor через GW, owner/project permission; privileged policy дополнительно требует fresh authentication. | CP transaction: create/save -> DRAFT, validate -> VALID/INVALID, discard -> DISCARDED, publish -> PUBLISHED и immutable environment revision.                     | Expected draft/environment version, validation digest, authoritative metadata read; save/prepare не меняют active binding.                                   |
| Impact prepare/apply | Текущий actor и разрешения каждого точного consumer; item checkbox не является authority.                                   | CP immutable plan PREPARED -> APPLIED либо EXPIRED; выбранные bindings меняются в owner transaction, локальный конфликт фиксируется receipt.                      | Plan digest/expiry, source/target revision и binding OCC; APPLIED/NOT_SELECTED/CONFLICT/FORBIDDEN отражаются поэлементно, exact replay не повторяет effects. |
| Secret draft         | GW принимает локальный ввод; secret-broker получает exact operation grant, CP хранит authoritative lifecycle.               | SAVE/VALIDATE/PUBLISH/DISCARD через encrypted staged descriptor; публикация меняет active revision, cleanup удаляет exact retired/staged materialization с fence. | Exact operation/claim generation и descriptor pins; metadata/mask readback без plaintext; retry и recovery не создают вторую active revision.                |
| Schedule occurrence  | automation-scheduler как зарегистрированный workload; запуск сохраняет authority автора расписания.                         | CP claim/renew/materialize/fail; immutable ScheduleRevision, scheduledFor, attempt и input digest; CONTINUE_ONE добавляет один turn.                              | Lease/fence/generation и stable attempt key; retry не повторяет turn, running RuntimeRevision сохраняет pins.                                                |

Durable receipt, audit и предусмотренные domain events создаются существующей
owner-транзакцией. UI cancellation отменяет локальный read, но не объявляет
серверную mutation отменённой. Для неизвестного результата публикации
используется сохранённый metadata intent и authoritative read/replay.
Текущий registry environment binding имеет прямого consumer AGENT; Workflow и
Automation получают окружение через назначенного agent и runtime snapshot,
а не через придуманный универсальный binding endpoint.

Связанный #1237 исправляет downstream delivery `SessionContext` в agent-runner:
история восстанавливается для cold thread, а provider resume получает только
новую exact continuation notice. CP automation fixtures в #1239 подтверждают
материализацию контекста владельцем; доставка этого контекста реальному provider
требует интеграции #1237 и отдельной проверки после deploy.

## Воспроизводимая локальная проверка

Disposable PG harness сам создаёт контейнер с динамическим loopback-портом,
выполняет migrations/bootstrap и удаляет контейнер. Внешний DSN ему не нужен.
Подготовка `role_image_promotion` обязательна: environment/secret impact
fixtures используют созданный ею admitted artifact.

```bash
GOMAXPROCS=2 timeout 300s bash scripts/tests/control-plane-postgres-test.sh \
  '^TestBootstrapComponent$/(role_image_promotion|schedule_readback|durable_schedule|schedule_race|runtime_environment_lifecycle|runtime_environment_draft|runtime_environment_privileged|runtime_secret|secret_revision_impact)'

cd services/staff/control-center
npm run test:unit -- src/pages/RuntimeEnvironmentEditorPage.test.ts \
  src/pages/RuntimeEnvironmentsPage.test.ts src/features/runtime \
  src/features/runtime-secrets src/features/automations
npm run build
```

Go unit/build выполняются с `GOMAXPROCS=4`, `go test -p 2` и
`go build -p 2 -o /dev/null ./...` в профильных модулях CP, GW,
secret-broker и automation-scheduler. Незапущенные browser/deploy/provider
проверки остаются NOT RUN. Локальные fixtures не проверяют реальный кластер,
provider prompt delivery или графический интерфейс браузера.

## Ручная проверка и откат

После отдельного разрешения deploy создать синтетические Environment и Secret
с двумя agent consumers. Сохранить draft, validate, проверить отмену impact,
опубликовать с одним выбранным consumer и сверить оба binding readback.
Повторить с конкурентным изменением одного consumer и отозванным permission;
проверить отдельные receipts и неизменность запущенной attempt.

В редакторе быстро выбрать два образа, задержав первый HTTP-ответ: tools и
loading должны принадлежать второму. Перейти к другому окружению до завершения
первого запроса и проверить сохранность нового ввода. В каталоге выбрать
окружение, перевести фокус вне таблицы и нажать Escape; inspector закрывается.

Для automation проверить пять preview timestamps, реальный следующий запуск,
task в AGENT/WORKFLOW и ровно один новый turn у CONTINUE_ONE после retry.
Для secrets пройти типы STRING/JSON/BINARY, validate/publish/rotation/reload,
неизвестный ACK и cleanup, не сохраняя plaintext в evidence.

Изменения #1239 не требуют миграции данных. Откат UI выполняется возвратом
предыдущего образа PWA после owner approval; immutable опубликованные revisions
не перемещаются назад. Повторная публикация требует новой revision и обычных
OCC/authority checks. Секретные значения в документ, PR и evidence не включаются.

Через Context7 проверена официальная документация Vue 3 о
[watch cleanup и stale async requests](https://vuejs.org/guide/essentials/watchers).
