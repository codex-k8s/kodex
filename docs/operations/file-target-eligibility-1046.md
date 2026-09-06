---
id: OPS-DOC-067
title: Полномочия целей связи файла и вложений запуска
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Полномочия целей связи файла и вложений запуска

Источник: #1046, #1022, Epic #1018, MVP-UI-31/40. Этот вклад входит в один
полный control-plane PR #1071. Самостоятельный PR не создаётся.

## Карта сценариев

| Инициатор и authority | Внешний путь и RPC | Owner и версии | Результат, событие и потребитель |
| --- | --- | --- | --- |
| Files, проверенный actor/organization; artifact.bind и видимость Artifact/Agent | GET artifact binding targets → ListArtifactBindingTargets | CP, RR snapshot, Artifact version, Agent versions, query/actor-bound cursor | Только видимые цели и authoritative total, bound/canBind/canUnbind с закрытыми причинами; без события, PWA перечитывает query |
| Files, те же текущие полномочия до OCC и receipt | Существующий POST Artifact bindings → ChangeArtifactBinding | CP транзакция, If-Match Artifact, exact idempotency intent | BIND требует capability и допустимый lifecycle; UNBIND существующей связи допускает отозванную capability и архивный tombstone; прежний ARTIFACT_CHANGED, ответ и query |
| NewRun, actor с agent.launch/workflow.launch; continuation дополнительно run.view | GET target attachment eligibility → GetRunAttachmentEligibility | CP, текущие опубликованные target/runtime/catalog/provider/environment зависимости; continuation target только из Run/Session | Одна aggregate projection для Agent/Workflow, без browser fanout; без события, перечитывание после выбора/изменения target |
| Launch/AddSessionTurn, текущая owner authority | Существующие команды запуска/продолжения | Независимая повторная проверка actual launch predicate и immutable RuntimeRevision | Query не является grant или разрешением обойти команду; существующий Run lifecycle/event |

HTTP paths согласованы: `/api/v1/artifacts/{artifactRef}/binding-targets`
(`listArtifactBindingTargets`) и
`/api/v1/projects/{projectRef}/run-attachment-eligibility`
(`getRunAttachmentEligibility`, query targetType/targetRef/runRef).
Новые HTTP paths и generated SDK публикует полный gateway unit #1066 после
стабильного producer. Внутренние методы проходят transport → caster → service →
repository; profile 67 регистрируется после исполняемых owner checks. Runtime
claim SQL принадлежит параллельному prompt contribution и здесь не меняется.

## Матрица отказов и восстановления

- Hidden/foreign Artifact или Agent не раскрывается; hidden Agent исключается
  до total и пагинации. Отдельный agent.manage не добавляется: authority связи
  принадлежит artifact.bind, safe target требует agent.view.
- Снятие связи не требует runtimeReady. Архивная цель может быть показана
  только как видимый существующий tombstone, без возможности новой связи.
- Смена версии, видимости или capability между страницами инвалидирует cursor;
  ответ не содержит частичного total. Timeout/overflow закрыто отклоняются.
- Mutation повторно проверяет текущую authority до idempotency readback;
  receipt не сохраняет отозванное право. OCC не заменяет owner resolution.
- Для вложений Workflow проверяются координатор и все участники опубликованной
  версии. Частичная readiness не превращается в aggregate success.
- В continuation payload project/target должны точно совпадать с owner Run;
  произвольная подмена target не исправляется молча. Закрытая Session или
  активный turn не выдаются за допустимое продолжение.
- Digest основан на конкретных версиях и зависимостях owner, а не updatedAt.
- Artifact binding refs в single/list/event/receipt повторно ограничиваются
  текущим agent.view, включая только допустимые существующие tombstones.
  Исторический ответ не раскрывает скрытую или уже снятую связь.
- Assigned Agent files capability остаётся явным opt-in для вложений.
  Actor не обязан иметь upload/bind/delete для уже выбранных immutable inputs:
  exact view/download и owner manifest проверяются отдельно. Continuation
  использует immutable Session catalog, а не подменяет его текущим каталогом.

## Проверки

Локально PASS: race для repository/service/transport, полный Go vet/build,
Proto lint и чистый повтор генерации, policy 67 и SQL boundary.
Первый полный Bootstrap завершился FAIL в новой тестовой оснастке: прямой
projection helper получил principal до штатного ResolvePrincipal. Оснастка
исправлена. Полный повтор Bootstrap — PASS (25.038 s), новый совмещённый
сценарий binding/aggregate/readonly/tombstone — PASS (0.53 s).
Обязательные component сценарии: configured Agent без runtime для knowledge
binding, readonly/foreign/hidden targets, archived unbind и exact replay,
stale cursor, authority revoke перед receipt, Workflow с одним неготовым
участником, continuation с immutable Session catalog.
HTTP/SDK и browser chain новых query — NOT RUN.
Context7: pgx v5 RR/ReadOnly transactions, StrictNamedArgs и закрытие Rows.
Live providers, staging и production не запускались.

В основной #1046 включён exact source вклад
`e06c28726fc8e2576d01ce6919cba8f2359d3e52` вместе с hierarchy
`bfe7dd679f91a820e8c9c0fcd3a5500132f4f1d5` и RoleImage643. Объединённый
полный Bootstrap — PASS24.459s; три package race, полный vet/build,
SQL/Proto и policy68/authority ABI/web-only release — PASS. Ревизия policy
не понижена: оба новых query67 зарегистрированы вместе с RoleImage68.
