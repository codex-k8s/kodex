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

Balance/units/spending/live paid continuation пока UNKNOWN/NOT RUN.
Причина удалённой attempt4 UNKNOWN; successful attempt5 не доказывает её
устранение. Active workspace readiness может дать503/READ_ONLY на закрытых
provider-owned каталогах; successful terminal требует прежней post-join
нормализации и полного quota/metadata контроля. Live chmod, пропуск квоты и
расширение broker IPC не выполнялись. Phase-aware readiness только предложена,
не реализована. Все эти ограничения остаются в финальном отчёте.
