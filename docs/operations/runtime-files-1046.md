---
id: OPS-RUNTIME-FILES-1046
title: Закреплённый файловый каталог execution и read-only MCP
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Карта MVP-UI-37/61

Вклад в CP #1046 и последующий consumer runtime-controller #1025, Epic #1018.
CP owner реализует четыре RPC с authority profile66. Это вклад в общий unit,
не приёмка готового MCP пути: controller consumer и live runtime — NOT RUN.

## Источник полномочий и immutable snapshot

Перед выдачей execution lease control-plane выбирает разрешённые источники
в owner transaction: current actor/root lineage, Agent, Project, purpose,
artifact.view/download, текущие lifecycle/scan и exact revision/content digest.
Метаданные каталога не содержат storage locators или содержимого файлов.
Идентификатор, count и digest каталога входят в RuntimeRevision и её digest;
полный набор exact descriptors остаётся в приватных immutable owner tables
`runtime_file_catalogs`/`runtime_file_catalog_entries`. Deferred constraint
связывает frozen catalog с точной RuntimeRevision в одной транзакции.
Digest строится по потоку фиксированных entry commitments с бюджетом 5s;
полный каталог проекта не копируется в ограниченный JSON RuntimeRevision.

Закрытые purposes разделяют PROJECT, WORKSPACE_INPUT, RUN_RESULT и SKILL.
Полный effective ArtifactCapability разрешает поиск среди допустимых общих
файлов проекта и результатов доступных запусков. Сам по себе pinned input/Skill
manifest даёт только чтение перечисленных в нём exact источников: он не
расширяет Project или results catalog. Новый runtime получает новый snapshot;
старый attempt не получает файлы, появившиеся после его materialization.

## Сквозные операции

| Операция | Цепочка и границы | Ответ, аудит и lifecycle |
| --- | --- | --- |
| SearchExecutionFiles | MCP search → controller authenticated callback → generated RuntimeWork client → exact workload/bearer/signed context → CP lease/fence/generation и runtime catalog | Query ≤200 символов, page ≤100, cursor связан с execution/catalog/purpose/query. Current eligibility до SQL count/page; только safe descriptors |
| GetExecutionFileMetadata | Тот же execution grant; exact purpose/ref/revision/digest присутствуют в catalog | Только exact metadata, никаких locator; неизвестный, удалённый, quarantined или чужой источник скрывается |
| PreviewExecutionFile | Exact metadata eligibility → object-store port с owner-resolved key/version/digest | Только разрешённый text preview ≤16KiB; stream ограничен и закрывается, полный content отдельным разрешённым runtime artifact read path |
| GetExecutionFileManifest | Exact execution/catalog/purpose и cursor без расширяющих path/projectRef | Страница immutable descriptors с catalog digest/count и текущей eligibility; полный body не выдаётся |

Caller не назначает actor, Agent, Project, catalog watermark, object key или
список разрешённых revisions. Controller берёт execution identity из
проверенного callback input. Произвольные filesystem paths запрещены.

Все чтения имеют bounded deadline и owner audit без query/content. MCP tool
activity использует exact server catalog grant и безопасные purpose/count;
ошибка обязательного audit/readback не превращается в успешный ответ.
Отзыв actor/resource permission, terminal/cancel/expiry или замена generation
закрывает последующее чтение; прежний cursor не является источником authority.
Read-only операции не создают task/retry или внешний write effect. Повтор
чтения не публикует файл и не изменяет immutable RuntimeRevision.

## Обязательная проверка

- Positive: отдельно разрешённые project/input/result/Skill; server query,
  count/cursor, exact metadata/preview/manifest и audit без payload.
- Negative: caller-controlled project/path/purpose, foreign actor/tenant,
  wrong lease/fence/generation/runtime digest, stale revision, deleted/purging,
  quarantine, revoked permission, missing/tampered manifest, oversize/binary
  preview, object version/digest mismatch и canceled context.
- Controller: advertised tool schema, actual generated RPC, exact reply
  validation, bounded MCP envelope, safe tool activity, readiness/profile
  keys и оба итоговых render. Old revision без catalog не объявляет эти tools.
- Реальный agent workspace write/read/atomic replace/delete/result и
  immutable/symlink/traversal controls выполняются после общего gate на стенде.

Исходные требования: локальный backlog10, DOM-MC-004, GUIDE-DOC-003/006.
Context7 `/jackc/pgx` прочитан для транзакций, cursor и bounded context;
новый способ filesystem/object-store authority не вводится.

## Локальные проверки owner

На source tree вклада642 до фиксации checkpoint **PASS**:

- полный canonical `make test-control-plane-postgres`: disposable
  `TestBootstrapComponent` 22.309s и Avatar lifecycle 0.370s;
- реальные search/page/query-bound cursor, metadata, bounded text/prefix
  preview, manifest; exact permission, lease/fence/generation/catalog/file
  substitution и oversize preview закрыто отклоняются;
- Skill unbind и последующий rebind закрывают прежний manifest; immutable
  catalog mutation отклоняется trigger; existing runtime сценарии сохранены;
- полное чтение файла PROJECT через existing execution artifact API проверяет
  exact catalog grant и записывает owner audit. Новый файл после capture и
  удалённый файл не видны ни в поиске, ни в полном чтении;
- MCP activity принимает только exact catalog grant и закрытый purpose без
  write capability; дополнительные query/content параметры отклоняются;
- Proto lint/build/generation, policy66 codegen, authority ABI render и SQL
  boundary; Go owner/service/transport race 1.808/1.247/1.099s, shared
  runtimecontract race 1.105s. Необязательный file_catalog закреплён также
  в runtime v7 source schema; старые inputs без поля сохраняют прежний digest.

Первый общий прогон до вызовов новых RPC: PASS22.222s. Прямые RPC tests затем
выявили пустую выдачу: FAIL23.276s из-за совпадения SQL parameter `purpose` с
колонкой Agent. Повтор FAIL23.126s обнаружил ошибочно изменённые permission
literals при исправлении параметров. Явные `p_` arguments и канонические
permission keys восстановлены; промежуточный полный повтор PASS24.434s,
после full-body/activity расширения итоговый PASS22.309s. Общий
инвариант закреплён в GO-DOC-002. Логи находятся только в owner-private
локальном evidence-каталоге; query, contents и credentials не печатаются.

Controller MCP tools, advertisement/response validation/activity consumer,
итоговый integrated baseline,
staging/browser/provider/workspace acceptance остаются отдельной обязательной
незавершённой работой. Этот документ не объявляет MVP-UI-37/61 завершёнными.

При консолидации с вкладом641 сохраняются его свежие actor
artifact.view/download guards существующего input read и разрешение
readonly input manifest без managed write capability. Вклад642 добавляет
catalog UNION к этому owner path; отдельный checkpoint не заменяет общую
проверку объединённого control-plane. Policy revision общего дерева должна
сохранять более новые operations и монотонный номер.

Объединение owner641 (`f55aadb93`, `8a1bd2d08`, `d81b054e7`) и owner642
(`ccefadda86f25370924a5a4fd19f57d7ace7ae85`) выполнено поверх Environment644
`ac67c10e5691bd8039f087a38bc6ef472d90b8ad`. Actor read/download guard,
readonly input eligibility и catalog UNION сохранены одновременно;
capture выполняется до RuntimeRevision digest. Policy69 сохраняет все
operations66–69. Полный объединённый Bootstrap PASS27.109s; три package
race PASS1.104/1.729/1.087s, SQL/policy/ABI/web-only release PASS.

Выявленный transport tail остаётся обязательным: текущий unary body read
ограничен32MiB, тогда как InputArtifact допускает512MiB. Большие input files
не объявляются доступными через этот transport; следующий bounded stream
должен сохранить размер продукта, exact lease/provenance и проверку digest
до выдачи bytes. Увеличение unary buffer не считается завершением этого пути.
