---
id: OPS-DOC-WORKFLOW-READ-001
title: Чтение опубликованной схемы процесса по имени
type: operations
status: approved
owner: manager
version: 1.0.1
updated: 2026-10-14
---

# Сценарий

Issue1797/PR1807. Natural PROJECT вопрос о SOFTWARE_CHANGE14.10: окружение
и материалы найдены, но exact published graph без открытия карточки не
подтверждён. Исторический manager-plan противоречит текущей publication;
старые файлы не заменяют авторитетное состояние процесса.

Добавляется отдельный закрытый `WORKFLOW_METADATA`, не расширяющий полный
`WORKFLOW_CONFIGURATION` или право изменений. Ответ содержит только текущие
resource/version/publication pins, имя/state, coordinator, concurrency,
timeout и опубликованные этапы/зависимости/ручные решения. Draft, инструкции,
результаты, input defaults, grants, env и secrets не включаются.

| Путь | Авторитет и исход |
| --- | --- |
| USER → helper → find_platform_resources | Root actor из SSO и exact lease; имя разрешается eligible поиском |
| get_configuration_catalog WORKFLOW_METADATA | Ref — locator, не authority; отдельный enum14, закрытый selector |
| RuntimeController → SearchAssistantResources → CP owner | Прежний RPC/operation profile, exact lease/fence/generation и свежий actor |
| CP repeatable read | Прежний GetWorkflow eligibility resolver и query, workflow.view, точная PROJECT boundary |
| Projection → caster → bounded pages → ответ | Только published snapshot с version/ref/revision и canonical SHA; SYSTEM не выходит из tenant/user eligibility |

READ не пишет business state, audit или events. Expiry/revocation/terminal,
неверный source/fence/generation закрыто отклоняются прежним lease resolver.
Скрытый/чужой target — NOT_FOUND; mismatched pins/private fields — закрытый
отказ caster. Full configuration и UPDATE остаются context-bound.

- [x] Proto/codegen, domain validation, producer и consumer/schema/guidance.
- [x] Whitelist/private-field/pin/schema unit и disposable eligibility/project/stale/terminal checks.
- [x] Safe idle delivery, source/serving proof.
- [x] Native повтор натурального вопроса; точные актуальные этапы и один финальный owner gate.

Code-tree14.10: Proto lint/build/exact codegen PASS; CP domain/repository
packages PASS, callback package PASS6.618s, disposable full project/helper
component PASS51.430s (SYSTEM/PROJECT, AGENT/WORKFLOW экран, source/fence/
generation, foreign project, terminal; full source gate не расширен).
Один общий producer/caster golden; private fields, JSON null, duplicate step,
foreign project, version mismatch, mixed payload и ordinary agent отклонены.
Первый CP fixture FAIL из-за отсутствующего input type и callback fixture
panic из-за предположения helper о наличии screen исправлены. Первые seven
старых tools/list hashes FAIL; новые schema hashes проверяют enum14.
Первоначальный producer/caster был CODE_ONLY; далее доставлен штатным
hot reload после authoritative idle. Exact binaries:
CP3854981434ed080e6f78e62b1b48ebfa9f65d049723713e9b148cac3d48d6ed3;
RCe861d02ef9b35f8453bc3350a5af2cd12420ef03026683d1e27ad4ea76526a77.

14.10.2026 18:17 UTC: guard PASS на SOURCE/HOT5d085497, active roots/leases/
builds/admissions/promotions0, published85/pinsf82202f6 unchanged. Actual
CP PID3391 и RC PID752 /proc/exe SHA совпали с указанными binaries;
host/Pod source WORKFLOW_METADATA99f02816 и callback849b5b5d MATCH.

Native PROJECT run_Ot30Ax_MyXmR5zNTe4hfZle7 SUCCEEDED/version2/sequence24.
Вопрос без ID, не в карточке процесса; get_configuration_catalog с
WORKFLOW_METADATA SUCCEEDED. Ответ прочитал published revision16/
wfv_Bctunk4SNG_NahT4TNKlkIcb и перечислил пять этапов, зависимости1→2→3→4→5,
единственный SINGLE_COMPREHENSIVE_REVIEW и FINAL_MANAGER_REVIEW с human_gate.
Public workflow GET49/PUBLISHED подтвердил назначенных исполнителей и gate
только пятого этапа. Старые файлы не использованы, mutation plan отсутствует.
Desktop/mobile response screenshots просмотрены через обычный SSO fallback,
page overflowfalse/Console0/relevant API200/Network0; screenshot начального
realtime RESTORING не подменяет отдельный MCP connected/rejoin proof.
Это закрывает natural published-metadata сценарий, не dogfooding acceptance.
