---
id: OPS-HTTP-ROLE-IMAGE-IMPACT-1045
title: HTTP план применения RoleImage
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

Источник: #1045/#1046, Epic #1018, MVP-UI-42/47.
Owner643 и policy68 входят в CP455. Это отдельный SDK checkpoint
существующего полного gateway PR #1066, без нового PR или deploy.

| Сценарий | HTTP → RPC, authority | Owner и consumer |
| --- | --- | --- |
| Подготовка после promotion | POST role-image-configurations/{configurationRef}/revisions/{revisionRef}/impact-plans → PrepareRoleImageImpactPlan; signed actor, current source manage, If-Match set и idempotency | CP сохраняет immutable recipe/build/admission/image и source consumer pins; SDK отдаёт 201 plan |
| Чтение/страница/поиск | GET role-image-impact-plans/{planRef} → GetRoleImageImpactPlan; current owner source/consumer eligibility | Server query/count/cursor, без локальной фильтрации; PREPARED/APPLIED/EXPIRED |
| Выборочное применение | Existing POST consumer-bindings → RebindRoleImageConsumers; planRef/digest/selectedItemRefs, тот же current authority/OCC | CP атомарно создаёт Environment version, меняет выбранные bindings, receipts и обязательные events; response содержит plan и новые configuration/revision |
| Потеря ответа | Exact command replay либо GET плана | Новых эффектов от чтения нет, PWA читает outcomes; query не является grant |

Лимиты whole plan и выбранных refs — 1000. Пустой явный массив допустим;
отсутствующий/null массив и legacy metadata-only consumers закрыто отвергаются.
HTTP не пересчитывает digest. Artifact digest сохраняет OCI sha256 prefix,
остальные digests — lowercase64. Environment-only item имеет projectRef и
не содержит фиктивного Agent consumer. APPLIED требует новую Environment
revision, а Agent item также прежний binding ref с увеличенной версией.
Другие outcomes не могут содержать результаты записи.

Canonical unary issuer связывает полный protobuf digest; actor берётся из
проверенного application context. Payload refs не назначают authority.
HTTP не добавляет project/resource hints вне канонического client path.

Проверки: prepare/apply/read request mappings, exact source/plan pins,
закрытые enums/digests/version bounds, environment/agent outcomes,
запрет legacy/duplicate selection и response contradictions. Targeted race,
gateway vet/build, strict OpenAPI и SDK typecheck, Go/TS replay отмечаются
в exact checkpoint ledger. Полный HTTP completeness пока FAIL по семи
writeback65 RPC; следующие consumers выполняются в том же unit.

На CP455 поиск RoleImage plan ограничен refs; owner name-search correction
включается следующим stable CP644 checkpoint. HTTP query уже передаётся
неизменённым. Browser, actual protected producer route и integrated review
этого пакета — NOT RUN. Секреты не раскрыты, owner gate/deploy не выполнялись.
