---
id: OPS-DOC-1330
title: Публичные access scopes и создание исходника RoleImage
type: operation-proof
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-08
---

# Причины и границы

Refs [#1330](https://github.com/codex-k8s/kodex/issues/1330),
[#1332](https://github.com/codex-k8s/kodex/issues/1332), CFG-01/CFG-03,
итоговая приёмка [#1031](https://github.com/codex-k8s/kodex/issues/1031).
Публичный ORGANIZATION/PROJECT scope не содержал внутренний resource locator,
который требовал CP resolver. Корректный SDK JSON давал HTTP400 INVALID_REQUEST.
После устранения этого разрыва общий PWA predicate всё равно требовал
image.build для создания managed source; это право неприменимо к ресурсу
ORGANIZATION и отдельно проверяется только build/recipe path.

# Сквозная карта

| Инициатор и authority | Endpoint → BFF → RPC | Владелец состояния и ответ | Переход / event |
| --- | --- | --- | --- |
| Cookie/CSRF session; actor/org из проверенного signed context | POST effective-access/query → strict generated EffectiveAccessQuery → AccessService.QueryEffectiveAccess | CP principal → read-only PG repeatable-read → public scope resolver → visibility → subject/bindings → Evaluate; decisions сохраняют входной валидированный public scope | Нет mutation/idempotency/event |
| Тот же actor; другой subject только через действующее access.manage | POST effective-access/explain → strict ExplainAccessInput → AccessService.ExplainAccess → тот же Query с одной permission | Один authoritative decision; скрытый target/subject не раскрывается | Нет mutation/event |
| Проверенный actor с access.manage; subject не назначает authority | POST effective-access/simulate → strict SimulateAccessInput → AccessService.SimulateAccess | Тот же resolver; существующие bindings плюс ephemeral simulated binding, current/simulated решения; PG commit только read transaction | Binding/role/grant не сохраняются; нет event |
| Пользователь нового managed RoleImage | ConfigurationEditor → loadRoleImageSourceCreateAccess → query двух source прав | image.source.view/manage по organization/project; build predicate отдельно сохраняет image.build | До Save только read; все серверные write checks неизменны |

Полный префикс трёх endpoint: `/api/v1/administration/access/`.
`effective-access/simulate` является read-only вычислением. Public RESOURCE_KIND
с projectRef разрешает действующий проект и его visibility до расчёта;
RESOURCE_INSTANCE продолжает authoritative resource lookup. Внутренние уже
материализованные locators не переписаны. Никакие grants, registry, схема,
version floor, credential или audit/outbox history не меняются.

# Локальные доказательства и ограничения

- BFF `TestPublicAccessQueryScopesAcrossEndpoints`: три настоящих generated
  HTTP routes × четыре scope shape; exact JSON → generated RPC и публичный
  response target; actorRef как неизвестное поле отвергается до RPC.
- CP `TestPublicAccessScopeRPCPreservesQueryShape`: generated RPC → domain
  scope и обратный caster для тех же четырёх вариантов.
- Disposable PG `TestBootstrapComponent/public_access_query_scopes`:
  owner/viewer, четыре public JSON shape, ALLOWED/DENIED без выдачи новых прав,
  single-permission query, Simulate с обязательным transport-assigned
  RoleVersionRef=simulation, повторное чтение без сохранённой authority;
  malformed/contradictory scope, чужой tenant/project/subject и неизвестное
  permission закрыты. Это составная проверка трёх границ, а не заявление
  об одном процессе HTTP→gRPC→PG или живом provider effect.
- Штатный PG harness отдельно сохраняет enterprise exact-agent/project и
  RoleImage canonical authority. Их обязательные prerequisites:
  `catalog_owner_probe|model_catalog_is_version_bound`; запуск без них может
  упасть на later launch INVALID_INPUT и не доказывает дефект этого query.
- PWA unit разделяет source и recipe/build rights, сохраняет foreign target
  guard и deny любого source права. `source-create.synthetic.spec.ts`
  монтирует реальный ConfigurationEditor в Chromium/Firefox/WebKit,
  organization/project × allow/deny, без Save/build/provider.
- Read-only QA guard допускает только точный двухключевой source profile
  либо прежний трёхключевой recipe profile; другие admin writes запрещены.

Все фактические local outcomes и exact SHA закрепляются в PR. Live после
изменения остаётся NOT RUN до ограниченного rollout CP/PWA и нового GO.
Предыдущие live FAIL не переклассифицируются. Успех открытия редактора не
доказывает build/scan/SBOM/provenance/admission/promotion/node-pull и полный CFG.

# Выкладка и ручная проверка

Контракты и зависимости не изменены; приложение требует обновления CP и PWA.
BFF изменён только тестом, его runtime rollout не требуется из-за этого PR.
После точного serving readback открыть создание managed RoleImage на ru/en,
1440/390: поле имени и source доступны при двух source правах. Без любого
source права UI закрыт, recipe/build без image.build закрыт. Отдельно проверить
project scope. Ничего не создавать повторно при неопределённом исходе.
Откат приложения не меняет данные, но возвращает известные UI/query дефекты.
Секреты, cookies, source содержимое пользователей и персональные данные в
общие evidence не сохраняются.
