---
id: OPS-HTTP-ROLE-IMAGE-1045
title: Серверный каталог и происхождение рецептов образов
type: operations
status: approved
owner: developer
version: 1.1.0
updated: 2026-09-06
---

# Контракт #1045

CFG-01/02/03 и MVP-UI-05 требуют серверный каталог и точную связь с
конфигурацией. Producer — CP `b9402939a3ccdcef384d44cc2c04dfa5554f73b5`.

Verified session → GET `/projects/{projectRef}/role-image-recipes` →
ListRoleImageRecipes передаёт query/state/roleDefinitionRef/page без локального
поиска. CP разрешает project и проверяет тот же project.view в list/count;
repeatable-read возвращает items, total и actor/filter-bound cursor.
GET recipe и специализированные Manage commands возвращают managedLineage;
HTTP сохраняет конфигурацию, immutable revision и UI/GIT/SHIPPED provenance.
Пустое происхождение старого рецепта не назначает полномочий. SHIPPED baseline
может не иметь managed revision; partial revision tuple закрыто отклоняется.
Build сохраняет configurationRevisionRef, когда owner связал его с ревизией.

Create/Update/Archive/Restore/RequestBuild сохраняют существующие server-owned
authority, If-Match и idempotency. Receipt не заменяется произвольным latest
GET. Публичный read/count не создаёт event: authoritative GET/List является
путём повторного чтения. UI использует nextActions и managed lifecycle;
browser не выбирает promotion, source ownership либо поколение.

Неверные запросы дают 400 до RPC; повреждённые count/cursor/lineage и чужой
project в list дают 502. Hidden/permission/OCC/state/unavailable сохраняют
существующие 404/403/412/409/503 mappings без private details. Go/TS SDK
генерируется из OpenAPI. Context7: kin-openapi и protobuf-go, проверены при
работе с тем же gateway source validation/codegen.

Локально PASS: targeted RoleImage race1.104s, полный gateway race/vet/build,
strict OpenAPI kin-openapi0.135.0, strict generated SDK typecheck и Proto
lint/build/replay с policy64. Первый OpenAPI запуск имел FAIL(setup) из-за
неверного относительного пути; повтор с абсолютным source path прошёл.
Первый Proto replay завершился FAIL без диагностики; повтор с каноническим
локальным TMPDIR/GOTMPDIR прошёл, generated diff отсутствует.
Реальный owner/browser/provider, build/promotion/rebind и полный интеграционный
baseline — NOT RUN. Логи `http-role-image-*.log` находятся в приватном каталоге.
Секреты, private worker snapshot и credentials в lineage не добавляются.

## Отдельный допуск к исходнику (#1083)

Additive CP source `d547d49690bf8c524967c1fd478fc9bd670aa6ac` объявляет
`sourceAvailable` отдельно от metadata. Verified actor → existing List/Get
или Manage receipt → owner source.view → HTTP read DTO → editor. При false
Dockerfile и InstallationBlock отсутствуют; environment/package/tool metadata
сохраняется. При true текущий owner материализует непустой Dockerfile, в том
числе в QUEUED build snapshot. Допуск к сборке не заменяет source.view.

HTTP разделяет `RoleEnvironmentView` и прежний обязательный
`RoleEnvironmentSelection` для create/update. Противоречие false с исходным
текстом, как и true без materialized Dockerfile, закрывается 502 до выдачи
list/single/receipt; текст не включается в ошибку. OCC/idempotency и команды
не изменены, read event не добавляется. Consumer включает editor только по
authoritative sourceAvailable и отдельно проверяет действия записи.

Новые HTTP/SDK проверки дополнения локально PASS: targeted race 1.127 с,
полный HTTP race 6.188 с, strict generated SDK, canonical Go/TS/AsyncAPI и
replay. В дополнительном тесте контекста ассистента сначала обнаружен FAIL:
общий normalizer удалял TYPE_ из literal entityName. Теперь route/kind/ref/name
сохраняются, allowedOperations остаются enum; FILE/ENVIRONMENT/INTEGRATION
readback проходит ту же проверку. Ошибки компиляции новой fixture (optional
version pointer) и размещения guard исправлены до повторного PASS.
Исторические PASS выше относятся к прежнему каталогу.
Producer RBAC полного нового CP checkpoint здесь ещё NOT RUN.
Ручная проверка: actor с metadata/build без source.view видит карточку и
историю без исходника; actor с source.view получает текст; отзыв права
убирает текст при следующем защищённом чтении и replay. Owner RBAC и live
проверяются отдельно. Rollback только согласованным откатом всего owner/HTTP
контракта: возвращать прежнюю выдачу текста metadata-читателю нельзя.
