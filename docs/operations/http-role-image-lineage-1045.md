---
id: OPS-HTTP-ROLE-IMAGE-1045
title: Серверный каталог и происхождение рецептов образов
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
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
