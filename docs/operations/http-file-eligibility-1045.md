---
id: OPS-HTTP-FILE-ELIGIBILITY-1045
title: HTTP проекция VFS и полномочий целей файлов
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

Источник: #1045, #1046, #1022, Epic #1018, MVP-UI-31/37/40.
Producer: `455a19121d1e97406499dcfb318e0548201d70dc`, policy68
содержит query67. Вклад входит в существующий полный gateway PR #1066.

| Сценарий и authority | HTTP → RPC | Owner и результат | Consumer |
| --- | --- | --- | --- |
| Files, подписанный actor и текущие artifact.bind/agent.view | GET artifacts/{artifactRef}/binding-targets → ListArtifactBindingTargets | CP RR snapshot, версии Artifact/Agent, точный total/cursor; archived tombstone может разрешать только UNBIND | PWA получает canBind/canUnbind и закрытые причины, не выводит grant из состояния |
| NewRun, actor с текущим launch, для continuation дополнительно run.view | GET projects/{projectRef}/run-attachment-eligibility → GetRunAttachmentEligibility | CP проверяет exact target/Session и aggregate Workflow; ответ не разрешает обход команды | PWA перечитывает после смены цели, не выполняет browser fanout |
| VFS, тот же owner eligibility одиночного и массового чтения | GET vfs/nodes или vfs/search → ListVFSNodes/SearchVFS | CP применяет path/query/lifecycleState/kinds до count/page; отдаёт версии, immutable revision, scan, lifecycle и действия | PWA сохраняет точные дескрипторы и использует server selectionReason |

Это read-only query: нет idempotency, state transition или domain event.
Версии и digest принадлежат владельцу; последующая команда независимо
повторяет текущую authority, OCC и idempotency. Ответ query не является grant.
Payload и projectRef не назначают actor/tenant. Оба query67 используют
зарегистрированный exact unary profile без дополнительного project metadata.

HTTP закрыто отклоняет чужой ответ, неизвестный enum, несовместимые boolean
и reason, unsafe JSON version, повторяющиеся refs/actions, invalid timestamp
и digest. Server filtering не заменяется фильтрацией страницы gateway.
VFS directory и immutable context не становятся selectable по наличию ref.

Локальные проверки: targeted race VFS/67, gateway vet/build, strict SDK
typecheck. Проверяются archived UNBIND, точный target Workflow, запрет
ложной readiness, opaque owner cursor и отрицательные upstream mappings.
Строгая OpenAPI validation и canonical Go/TS replay фиксируются для exact SHA.
Полный HTTP race пока FAIL по девяти новым RPC writeback65/RoleImage643:
их consumers выполняются следующим пакетом этого же unit. Проверка полноты
не отключена. Новая цепочка HTTP/SDK → browser, live и deployment — NOT RUN.

Документация библиотек: ранее проверенные Context7 kin-openapi validation и
Go generated adapters; новые внешние библиотеки не добавлены.
Owner gate, merge и deploy не выполнялись. Секреты не раскрыты.
