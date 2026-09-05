---
id: OPS-CATALOG-RUNTIME-DIFF-1046
title: Контрактная карта D2 и D4
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# Граница вклада

## Реализованный D4 producer

`GetRuntimeRevisionDiff(run_ref, current_revision_ref?)` выбирает latest внутри
Run либо exact pin. Predecessor выбирается по `(created_at, ref)` строго раньше
current и в той же organization/project/Session. Обе Run проходят `run.view`
в одной read-only REPEATABLE READ transaction; скрытая ревизия и несовпадающий
Run pin дают `NotFound`. Отсутствующий predecessor не подменяется current.

SQL читает закрытый список столбцов, не `safe_snapshot`. Public identity:
ref/version/run/session/turn/attempt/revision digest/time. Change value содержит
только ref/version/digest/revision: PROVIDER и MODEL используют ref, RUNTIME_PROFILE
использует ref и текстовую revision; остальные компоненты используют exact refs,
числовые версии и digests. IMAGE выдаёт только manifest digest, без registry
locator. Credentials, значения, инструкции, файлы и private worker snapshot
в публичную модель не входят. Первый snapshot имеет только current changes.

Authority operation `platform.query.runtime-revisions.diff`: OIDC producer,
resource=run_ref required, unary Proto digest, без mutation/version/attempt
metadata. Policy revision 56. HTTP/PWA реализуют отдельные unit; новый
deployable/порт/ключ не добавляется.

Локальные проверки D4: domain/transport race, CP vet/build, Proto lint/build/
codegen и authority-policy codegen PASS. Disposable PG — PASS для prerequisite
`runtime_configuration_publish` и `session_provider_affinity` с exact predecessor,
foreign Run pin и first revision. Первый PG запуск без prerequisite — FAIL
(отсутствовал secondary provider fixture); повтор с обоими сценариями PASS.
Журнал `/tmp/kodex-1046-d4-pg.log`. Полный baseline, HTTP/PWA/live NOT RUN.

Вклад в единый #1046, база 67aa98d77. Только CP producer D2/D4;
HTTP/PWA, EMAIL configuration и staged Secret lifecycle не изменяются.
Источники: #1046/#1018, MVP-UI-17/18/36, GUIDE-DOC-003/004/006.

| Сценарий | Инициатор и authority | Контракт и владелец | Version/результат/событие |
| --- | --- | --- | --- |
| D2 каталог | Browser session → HTTP model-capabilities → authenticated CP ListModelCapabilities | CP organization.view, tenant из principal; provider/account SQL, закрытый model capability registry | Content-addressed catalog_revision и bare lower64 catalog_digest всего eligible snapshot, не страницы; чтение без события |
| D2 продолжение страницы | Тот же actor/tenant/filter + cursor | CP повторяет eligibility и вычисляет текущий snapshot | Cursor связан с actor/tenant/filter/revision/digest; mismatch InvalidArgument, никакой выдачи stale page |
| D2 exact pin | Expected revision/digest являются условием, не authority | CP сравнивает обе части с текущим snapshot | Изменение account/catalog закрывает старый pin; пустой результат также имеет revision/digest |
| D4 diff | Browser session → будущий HTTP consumer → authenticated GetRuntimeRevisionDiff | CP разрешает current revision внутри run.view; previous выбирается сервером в той же Session | Только safe typed changes и exact refs/versions; worker snapshot, credentials, locators, prompt/file contents не выдаются |

## Lifecycle

Обе операции read-only, не создают grants, claims, turn, retry, provider effect
или event. Eligibility повторяется на каждом read, включая terminal/archived
run; скрытый/удалённый owner возвращает NotFound. Cancel/deadline прерывает
SQL, ошибки не возвращают частичную страницу/diff. Publish/rebind/revoke
остаётся у существующих command owners. D2 snapshot является версией
наблюдаемого каталога CP, а не доказательством live provider health.

D4 не создаёт continuation и не разрешает запуск. Материализация continuation
template и exact-once message остаётся отдельным runtime consumer существующего
unit; этот вклад закрывает безопасный публичный read контракт между сохранёнными
RuntimeRevision. Исходная immutable revision не переписывается.

## Материализация

Producer CP → Proto/generated Go → controlplaneclient operation profile →
authority policy → HTTP/PWA consumer в их ownership. Используются существующие
CP mTLS/application/proof, bounded gRPC и PostgreSQL runtime. Нового listener,
ключа, NetworkPolicy или deployable нет. Readiness CP не заменяет full path.

Context7: /jackc/pgx, transaction options и bounded commit/rollback;
фактическое имя поля TxOptions сверяется с закреплённой версией pgx/v5.

## Передача HTTP

D2: ListModelCapabilitiesRequest добавляет expected_catalog_revision=5 и
expected_catalog_digest=6. Response добавляет catalog_revision=4 и
catalog_digest=5. Revision имеет форму mcat_<lower64>; digest bare lower64.
Это content-addressed identity, не монотонный счётчик и не live provider probe.
Digest включает версии SQL definition/account/credential source и safe models;
query/page не меняют commitment. Cursor v2 связан с tenant/actor/authority project,
filters и commitment; старый v1 либо изменённый source возвращает InvalidArgument.
Новой authority operation нет: platform.query.models.list сохраняется.

Локально D2 PASS: domain/cursor race, vet затронутых CP packages, CP build,
disposable PostgreSQL model_catalog_is_version_bound (0.07s scenario).
Первый PG запуск: сценарий PASS, последующий bootstrap assertion FAIL из-за
порядка fixture. Сценарий перенесён после bootstrap readback, повтор PASS.
Provider live/HTTP/PWA NOT RUN. SQL migrations для D2 не требуются.

Обнаружен D3 mismatch в cfb18a17e: CP TemplateVariable.Type имеет значения
string/reference/integer/collection, source включает AUTOMATION/GATE/INPUT/RUN.
HTTP mapper uppercase enum не принимает этот producer. Исправление принадлежит
HTTP #1066, здесь HTTP не изменяется.
