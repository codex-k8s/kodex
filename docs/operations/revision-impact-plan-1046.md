---
id: OPS-REVISION-IMPACT-1046
title: Публикация черновика с выбранными потребителями
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Матрица Environment

Источник: #1046, Epic #1018 и MVP-UI-47. Migration644 и policy69 зарезервированы
для владельца control-plane. Environment owner исполняется по этой матрице;
HTTP/PWA и последующие Prompt/Instructions переходы пока NOT RUN.

| Переход | Authority и точность | Результат и чтение |
| --- | --- | --- |
| PrepareEnvironmentDraftImpact | Verified actor/tenant, exact draft/project.manage до OCC; VALID draft version и повторная проверка dependency digest | Immutable actor-bound plan, source Environment/base revision, target draft version/digest, доступные текущие binding pins; новый Environment ничего не публикует |
| GetRevisionImpactPlan | Exact plan actor/tenant, текущая source authority; закрытый kind registry | Без создания плана; безопасные refs/digests, поиск, cursor и total по текущей видимости; значения Environment/Secrets не выдаются |
| Save/Discard после Prepare | Существующая draft authority/OCC | Старый plan остаётся доступным как квитанция подготовки, но не может опубликовать изменённый или discarded draft |
| PublishRuntimeEnvironmentDraft | Exact plan/draft/version/digest и server item refs; повторная admission/Secret/policy eligibility | Публикация и item outcomes в одной owner TX; пустой selection публикует без замены bindings |
| Выбранный Agent | Текущая agent.manage, exact Agent/binding/source version | Существующий фактический BindAgentRuntimeEnvironment на новую immutable версию; APPLIED receipt хранит новый binding version |
| Конфликт/отзыв | Fresh authority перед каждым item и сохранённой квитанцией | CONFLICT/FORBIDDEN сохраняют старый binding; невыбранные items NOT_SELECTED |
| Истечение | Server TTL15 минут, неизменяемая дата | EXPIRED readback; новое применение закрыто, нужен новый Prepare |
| Неизвестный ответ/replay | Durable idempotency и текущая source authority до receipt | Тот же plan/outcomes без повторной публикации или новых binding versions |

Plan ограничен1000 items. Payload не создаёт новых потребителей. Environment
использует существующие Agent bindings; Workflow/Automation получают этот
Environment через конкретного Agent, отдельные вымышленные связи не создаются.
Старые RuntimeRevision остаются неизменными. Get не возвращает полный prompt,
Environment values, Secret descriptors или скрытые foreign resources.

Публикация target Environment является явно выбранным действием и сохраняется
даже при конфликтах всех отмеченных bindings; это не промежуточная версия,
созданная только ради binding. Отчёт различает успешную публикацию и каждый
результат замены. Архивирование потребителя после Prepare сохраняет FORBIDDEN
в durable item receipt; текущий public read скрывает уже недоступную строку
и пересчитывает видимый total, не изменяя immutable plan.total.

Prepare/expiry/final plan не создают нового domain event: authoritative Get,
command receipt и audit обеспечивают чтение результата. Реальная публикация
и изменение Agent используют существующие атомарные owner events; частичный
effect без durable outcome не допускается.

PromptTemplate и Instructions используют тот же закрытый read registry через
специализированные PrepareInstructionsImpact и PreparePromptTemplateImpact.
Для Instructions SourceRef/SourceVersion — Agent/OCC, SourceRevisionRef —
выбранный fallback, DraftRef/DraftVersion — VALID instruction revision.
Для Prompt SourceRef/SourceVersion — managed set/OCC, SourceRevisionRef —
прежняя current publication (пусто до первой), DraftRef/DraftVersion — VALID
managed revision. Prepare не изменяет эти версии и не публикует content.

PublishInstructionDraft и PublishPromptTemplateDraft требуют planRef и
selectedItemRefs. Publication остаётся явным эффектом даже при пустом выборе;
невыбранные потребители сохраняют прежние реальные bindings. Instructions
активирует server-owned Agent binding; managed PromptTemplate сохраняет
приоритет и не снимается неявно. Prompt применяет существующий owner Rebind
к каждому выбранному consumer с fresh authority, consumer/binding locks,
exact versions/revision и отдельным savepoint. Результат проверяется по
фактически сохранённой связи. Один CONFLICT/FORBIDDEN не подменяется APPLIED
и не отменяет независимый успешный элемент.

Prompt set.version увеличивается при публикации и каждом успешном owner
Rebind. ResultConsumerVersion может остаться прежней: изменяется binding,
а не Agent/Workflow/Schedule entity. ResultBindingRef сохраняется,
ResultBindingVersion растёт, ResultRevisionRef указывает новую публикацию.
Instructions Agent version увеличивается один раз при publication; его
binding меняется только для выбранного эффективного consumer. Get фильтрует
элементы текущими правами до count/page, plan.total остаётся immutable.

События новых Prepare отсутствуют: receipt/audit и GetRevisionImpactPlan.
Prompt publication использует существующий MANAGED_CONFIGURATION_CHANGED;
Instructions publication/rollback — INSTRUCTIONS_PUBLISHED. Draft save и
Validate Instructions не публикуют ложное событие публикации. HTTP/PWA
получают safe plan и затем читают актуальный owner snapshot, без общего
выдуманного realtime event.

# Контракт и проверка

PrepareEnvironmentDraftImpact принимает draftRef, его If-Match/version и
idempotency. GetRevisionImpactPlan принимает planRef/query/page, без version
или idempotency. Policy69: exact unary protobuf digest, resource required;
Prepare version/idempotency required, Get version/attempt/idempotency forbidden.
Plan ref/version/actor/expiry назначаются владельцем. SourceVersion — pin
Environment set, DraftVersion — pin подготовленного VALID draft. Plan digest
сохраняет исходные source/draft/binding pins независимо от terminal outcomes.

PublishRuntimeEnvironmentDraft требует planRef и явный selectedItemRefs
(0..1000); response содержит тот же план в APPLIED/version2 и exact
publishedRevisionRef. Caster сверяет draft/version/target digest с фактической
публикацией. Get возвращает текущий filtered total, а plan.total неизменен;
cursor связан с actor/tenant/plan/version/state/query.

Старый wire без planRef больше не публикует и не повторяет старую command
receipt. Неизвестный старый outcome восстанавливается через actor-authorized
GetRuntimeEnvironmentDraft: PUBLISHED и exact publishedEnvironmentRef.
Повторное publish/new effect для такого recovery не выполняется. Новый
unknown attempt сохраняет безопасные draftRef/planRef и использует оба
authoritative Get; nullable legacy plan не вводится.

Локально PASS: первый полный Bootstrap27.809s; расширенный полный повтор
23.374s проверяет expiry, immutable UPDATE/late INSERT, произвольный checkbox,
exact replay, частичный CONFLICT рядом с APPLIED, archived consumer FORBIDDEN
и текущую видимость readback. Три package race, полный vet/build, SQL/Proto,
policy69/ABI/web-only release PASS. Targeted caster proof также PASS.
Последующий targeted PostgreSQL1.844s повторил Environment и RoleImage
lifecycle с новым literal поиском по текущим именам Agent/Project/Environment
и refs. Фильтрация по текущим правам выполняется до public count/выдачи.
Исторический FAIL27.563s был вызван новым тестовым вызовом resolveScope до
штатного ResolvePrincipal; fixture исправлена, production guards сохранены.
Первый policygen получил отсутствующие файлы общего Go cache; повтор PASS,
чужие caches не изменялись. Live/browser acceptance нового impact — NOT RUN.

Owner70 Prompt/Instructions поверх641/642/644 прошёл полный Bootstrap27.449s.
Новый Prompt scenario0.40s проверяет фактический Rebind рядом с CONFLICT и
FORBIDDEN, сохранение прежней связи неуспешного consumer и exact replay.
Последующий PostgreSQL0.484s подтвердил Instructions NOT_SELECTED без смены
активного binding и повтор terminal receipt. Три package race1.211/1.699/1.082s,
полный vet/build, SQL/Proto replay/policy70/ABI/web-only release PASS.
Buf remote rate limit обработан штатным fallback к точным локальным plugins;
generated replay не изменил дерево. HTTP/PWA/live нового owner70 — NOT RUN.
