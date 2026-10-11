---
id: QA-DOC-SELFDEV-002
title: Реестр доказательств bootstrap и оставшихся шлюзов приёмки
type: qa-report
status: approved
owner: manager
version: 1.0.1
updated: 2026-10-12
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

## QA§45: применимость к опубликованному 60bf624d

11.10.2026 20:09 UTC ROOT повторил сравнение byte scopes и full diff-check на
clean SOURCE60bf624daa44a535c5f8d5eb23efbc4676d865f5. Remote Draft1807
readback MATCH; первый post-push readback UNKNOWN, повторное чтение подтвердило
тот же SHA без повторного push. HOT ff-only до того же SHA выполнен.

| Обязательный контур | Фактическое доказательство и область применения |
| --- | --- |
| Формат/static/build | Full `git diff --check ab4992e0 HEAD` PASS/exit0 на60bf. Committed95aa имел FAIL четырёх EOF blank lines; удалены только эти строки. Node continuation-readback5/5 PASS122.585ms, production SQL/binary inputs неизменны. Scoped gofmt/vet/lint/build остальных областей сохраняют исходные SHA. |
| Go runner/runtime/libraries | Runner whole unit/vet, scoped race/build и collector93 tests PASS на8f0fd117/его frozen integration scope. Two-UID kernel fixture отдельный PASS. Runner/collector bytes неизменны после8f; runtime-controller/libs byte-identical44a0c7bb. Default fixture SKIP не превращается в kernel PASS. |
| Go egress | Whole package unit2.549s наb41a6e10, scoped unit/race/vet/build, mounted source/serving ELF PASS. Egress bytes послеb41 неизменны; это не новая full-suite или external paid-download проверка. |
| Control Plane/PostgreSQL | После91a02d6d изменились только core-v50 и четыре build-intent файла. Оба scope имеют адресные unit/race/vet/build и disposable PG receipts: inheritance4cases8.450s, correlation3.253s/readbacks PASS. Ранние full/bootstrap/credential/profile receipts сохраняют свой scope; нового combined whole-module PASS не заявляется. |
| SQL/migrations | `make check-sql-boundary` PASS на8896636a; все SQL внутри Control Plane/CLI migration bytes по current comparison неизменны. Continuation tooling SQL менялся только удалением EOF blank line, его закрытая структура подтверждена Node5/5. |
| Contracts/Proto/OpenAPI/AsyncAPI/codegen | Byte-identicalbfb1460b: actual Proto/codegen, поздние AsyncAPI70Go/70TS и OpenAPI Go/TS regeneration/generated diff0 PASS. Старый regeneration NOT RUN superseded, первоначальные tooling FAIL сохранены исторически. Policyc153b494 registry7/actual policygen/authorityproof race PASS. |
| Render/security/RoleImage | Deploy/scripts byte-identicalc153b494. Оба render профиля, ABI/negative assertions и адресные PG negatives сохраняют свои receipts. Canonical b90/compiled8f, provenance/import/policy/defaults, два promoted recipes и ENV3/bindings7/tools38/preservation PASS. Fresh protected95 install/upgrade NOT RUN, не acceptance текущего trusted-dev runtime. |
| Integration/MCP/prompt | Unchangedbfb integration adapters/definitions/catalog: package/authority-policy и MCP16 wire fixture receipts PASS. Materialization/inheritance/core-v50 и same-UID native delivery PASS только для своих pins. Финальные handoff новой attempt6 ещё OPEN. |
| Frontend | Весь frontend byte-identical2bdee3dd. Исторический full399suites/3888tests на1d555df7 не является current whole PASS: последующие packets61/80/27/35tests и соответствующие lint/types/build receipts покрывают изменённые области. |
| Relevant Playwright/Chrome | Synthetic desktop/mobile/split scopes имеют отдельные PASS; прежние timeout/startup FAIL не удалены, единого green split5/5 run не заявляется. Current graph negative/resize/rejoin/screenshot/Console0/API200 PASS. Native attempt6 role/business semantics ещё OPEN, не QA65 PASS. |

Это inventory и ROOT read-only сравнение, не дополнительный review и не новый
запуск всех suites. Exact historical receipts находятся в журнале и handoff;
их SHA не заменяется на60bf. Нового обязательного запуска неизменных Go/PG/
contract/frontend областей не выявлено. Native chain, финальный ledger,
bootstrap merge/fresh-main/resources и отчёт QA§64 остаются открытыми.

Свежие GitHub gates95aa: mergeable=true/clean, checks0/status entries0,
reviews0, действующие rules deletion/non_fast_forward/creation. Это не CI PASS;
перед merge нужен readback окончательного опубликованного SHA.

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

11.10 21:02 UTC: compiled8a76/OCI3824059a canonical delivery завершена.
Оба новых project artifacts ACCEPTED/PROMOTED (staff66a00394/helper88447fff).
Full owner GET13rows200, планы EOF/APPLIED и exact offline proof
fb0f0f1c1aab8783f630da48e30b76d68ffcdd306d879a121e5fa5d56dc43166
подтверждают ENV3/bindings7/tools38/non-image preservation. Native Retry7
run_TK4meronkuMEdI-07g9tNBnv: root agent SUCCEEDED, INTAKE RUNNING.
Same-UID INTAKE ACK/rejoin CAPTURED, новый exact manifest/ELF EQUAL,
instructions/inbox EQUAL,38tools/22grants/25capabilities. Expected task
comparison NOT RUN; public summary предыдущей попытки byte-identical,
но это не исходный USER digest. Dual-source capture продолжает наблюдение.
Runner8a76 regression unit/race/vet относится к своему scope; новый native
terminal и весь QA65 ещё не PASS. Исторический attempt6 FAILED сохранён.

11.10 20:00 UTC: semantic CP fix pushed/serving3eeb8eb0, mounted source
MATCH и actual ELF995122df равен независимой сборке. Native Retry attempt6
с original inputb8ce9004 MATCH, fresh stages и root provider ACK/rejoin;
INTAKE ранний ACK/image5cb2711f/file ELF2991a81b подтверждены.
Новые role final handoffs, ONE Review/Manager/owner ещё OPEN.
Chrome own11 connected/screenshot/Console0/relevant200; это не QA65 PASS.

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
