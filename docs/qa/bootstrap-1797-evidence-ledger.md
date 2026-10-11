---
id: QA-DOC-SELFDEV-002
title: Реестр доказательств bootstrap и оставшихся шлюзов приёмки
type: qa-report
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-11
---

# Область и границы результата

Issue #1797, Draft PR #1807; бизнес-задача #1796. Упоминание bootstrap #1798
в исходном [QA-задании](full-qa-task.md) относится к текущему bootstrap PR,
а не разрешает создавать ещё один PR. Единственный цикл комплексного ревью
бизнес-задачи и окончательный owner gate сохраняются.

Этот реестр сверяет разделы45/64 QA-задания. Он не утверждает завершение QA65,
готовность к merge или успешную бизнес-приёмку. Полные исторические команды,
ошибки и receipts сохранены в
[журнале](../operations/self-development-dogfooding.md) и
[точке продолжения](../operations/self-development-handoff.md).

## Проверка неизменности на 68d1b9b3

ROOT11.10.2026 18:02 UTC выполнил `git diff --quiet` соответствующих областей
и сверил SHA256 двух capture-файлов. Это PASS сравнения байтов, не новый запуск
перечисленных suites. С момента6cd6103d изменены только два журнала.

| Область | Доказательство и допустимое применение |
| --- | --- |
| Runner, runtime-controller, libs/go | Байты совпадают с44a0c7bb. Применимы сохранённые адресные unit/race/vet/build/kernel receipts только для покрытого ими scope; прежние SKIP не становятся PASS. |
| Contracts | Байты совпадают сbfb1460b. Сохранённые contract/Proto/OpenAPI/codegen receipts относятся к исходным SHA, не к новому full-suite запуску. |
| Frontend | Байты совпадают с2bdee3dd. Сохранены unit/lint/typecheck/build и actual negative/browser evidence графа; прошлые startup errors отдельного Playwright не удалены. |
| Capture tooling | collector611bb18cbef20e6d46f5d710cd6c605700b5612d42028070f9ef6f01f7675c66 и test402815c2576fc7b57bd53ab594789ff6a0b9c820c7e4f36111c25c6bacb7e4ac MATCH. 91+19 тестов PASS на3780c1b6, не свежий запуск на68d1b9b3. |
| SQL/PostgreSQL | Сохранены SQL boundary8896636a и адресные disposable component receipts. Полноту применимости к final changed scope ещё нужно сверить перед merge. |
| Core-v50 | Сохранены адресные unit/race/vet/build, actual CP source/serving marker и native предложение → draft → отдельная публикация Developer revision2. Общие API/grants не менялись. |
| Images/ENV/integrations/prompt | Сохранены canonical build/admission/promotion/pull,38 tools и non-image preservation; native Context7/web/GitHub/prompt/gate receipts только для своего проверенного scope. |

Повторные broad suites ради docs-only не требуются. Если изменится исполняемый
scope, контракт, policy, fixture либо deployment boundary, применимость
пересматривается; эта таблица не освобождает новую область от её проверок.

## Свежая цепочка выполнения

11.10 19:53 UTC: canonical runner b90c0cc2/compiled8f0fd117 доставлен
repo-owned publish/policy/binding/defaults/verify, исходные77promoted pins
сохранены. Два project recipes ACCEPTED/PROMOTED; три ENV и7bindings
опубликованы typed owner commands. Fresh full EOF/APPLIED proof
44ec0927717201b0b5fb78b2d160f9429f0e136a9a7b254cf12ea86232f35f48
подтверждает preservation3/3 и38tools. Новый native recovery ещё NOT RUN.
Semantic build-intent fix исключает только CorrelationRef; worker RED/GREEN
disposablePG и unit/race/vet PASS; ROOT integrated race1.190s/vet и canonical
disposable PostgreSQL3.253s с readbacks PASS. Live delivery пока NOT RUN.
Исторический actual Developer guard и первая build RPC причина UNKNOWN.

Новый runner scope поверх8e2138b0: FAILED post-join normalization,
закрытые filesystem stage/reason, фактические workspace budgets в guide,
post-execution artifact error mapping и collector этой закрытой диагностики.
ROOT whole runner unit/vet, scoped race1.131s, offline binary build,
collector93 tests6.356s и diff-check PASS на integration tree.
Kernel двух UID worker fixture PASS отдельно; ordinary unit его не запускал.
Canonical OCI delivery и native recovery пока NOT RUN; actual rejecting
guard прежнего Developer UNKNOWN. Canary/quota/authority не обходились.

Новый scope11.10 18:36 UTC поверх68c66645: egress `proxy_stream.go`, его тест
и единственный call-site в `http_proxy.go`. Три exact SHA256 и команды — в
checkpoint18:36. RED chunked/Content-Length подтверждён; scoped unit/race/vet
и ROOT integrated unit/build PASS, общий invariant GO-DOC-001 обновлён.
После завершения Workflow HOT ff-only до8e2138b0: actual mounted source
0614b1b5 и serving ELF4488f6a5 MATCH проверенной сборке, Pod Ready19:06 UTC.
Whole gateway package unit на b41a6e10 PASS2.549s; native download и полная
suite NOT RUN. Это не исправление текущего host deny или workspace refusal.

- [x] INTAKE SUCCEEDED17:03:27 UTC; пять actual результатов прочитаны owner
  до bounded EOF, size/SHA/revision/version rejoin MATCH.
- [x] Architect SUCCEEDED17:50:05 UTC; шесть новых actual результатов имеют
  такую же проверку. Semantic PASS_CODE_ONLY; live gate UNKNOWN/CLOSED.
- [x] Собственный published snapshot Architect: CODEX OBSERVED0.160.0,
  INITIALIZE_USER_AGENT, exact run/session/turn/revision pins; не host probe.
- [x] Coordinator прочитал шесть результатов и сам делегировал Developer.
- [x] Developer17:52:14 UTC: actual template03041170/revision2 доставлен;
  full instructions91830B/58f6bfb6, owner task3241B/c32b553f,
  same UID/inbox/file/image ELF rejoin MATCH. Это delivery, не implementation.
- [x] Actual substantive business commit c784ee85ec2e8c9aaf481014d46401d61110ca5e
  и Draft PR #1808 опубликованы Developer; remote head/base readback18:47
  MATCH. Это не успешная финализация, проверка business scope или review.
- [ ] Подтвердить substantive branch/commit/push/PR Developer, tests нового
  бизнес-scope и собственные full-read receipts опубликованного handoff.
- [ ] ONE Reviewer: exact diff/SHA, направления, prompt/handoff и findings;
  Manager: продуктовый результат и `final-readiness.md`, финальный owner gate.
  Не запускать автоматические fix/re-review циклы.
- [ ] Завершить applicable tests/acceptance ledger и проверить фактические
  GitHub checks/protections. Пустые checks не равны CI PASS.
- [ ] Bootstrap merge/readback, fresh main delivery и runtime/resources/
  realtime/prompt readback; требуемый business outcome на свежем main.
  Конечный бизнес-PR не merge/approve/auto-merge агентами.
- [ ] Итоговый раздел64: final/merge/main/deployed SHA, effective profiles
  помощников/ролей, business branch/PR/reviewed SHA, проверки и ограничения.

## Неустранённые ограничения

Fresh Developer FAILED18:52:14 UTC; root FAILED/graph1213, Reviewer и Manager
CANCELLED, циклов ревью0. Capture56547 сохранил actual same-UID
TERMINAL_FAILURE/RUNTIME_ARTIFACT_INVALID/rejoin VERIFIED; публичный summary
RUNTIME_WORKSPACE_INVALID. Конкретный filesystem guard UNKNOWN. Доказанный
общий порядок outbox denial → пропущенная normalization → canary denial
исправляется отдельно, не выдаётся за установленную actual причину.
Retained rollout содержит metadata пяти подготовленных файлов менее1MiB;
Developer artifacts не опубликованы. Work/outbox emptyDir удалены вместе
с Pod, retained session PVC сохраняет только codex-home. PR1808 не сливать.

Balance/units/spending/live paid continuation пока UNKNOWN/NOT RUN.
Причина удалённой attempt4 UNKNOWN; successful attempt5 не доказывает её
устранение. Active workspace readiness может дать503/READ_ONLY на закрытых
provider-owned каталогах; successful terminal требует прежней post-join
нормализации и полного quota/metadata контроля. Live chmod, пропуск квоты и
расширение broker IPC не выполнялись. Phase-aware readiness только предложена,
не реализована. Все эти ограничения остаются в финальном отчёте.
