---
id: OPS-DOC-DEMO-001
title: Подготовка рабочего прототипа Kodex к демонстрации
type: operations
status: approved
owner: manager
version: 1.0.4
updated: 2026-10-15
---

# Срок и режим

Владелец заменил прежнюю полную QA-цель ускоренной подготовкой демонстрации.
Для запаса используется ближайший срок: **15.10.2026 13:00 Europe/Saratov**
(`09:00 UTC`). Если подразумевался следующий календарный день, этот более
ранний ориентир не откладывает готовность. До12:00 — основные исправления;
12:00–12:30 — финальный адресный smoke и отчёт, резерв до13:00.

Только код платформы Kodex и необходимые для него доказательства. Не менять
промпты и системные инструкции, не улучшать поведение ролей ради dogfooding,
не расширять scope косметикой и не повторять уже подтверждённые suites.
Полный исторический QA65 сохраняется, но не является новой обязательной
работой целиком до демонстрации; остатки передаются отдельно без ложного PASS.
Разрешены основной агент и до трёх субагентов на непересекающихся задачах.
Это реализация и диагностика, не дополнительный цикл review.

Issue [#1797](https://github.com/codex-k8s/kodex/issues/1797), текущий Draft
bootstrap [#1807](https://github.com/codex-k8s/kodex/pull/1807), ветка
`kodex-agent/issue-1797-full21-v7`. Все интегрированные исправления — здесь.
Бизнес Draft #1809 не merge/approve/auto-merge агентами; существующий native
Workflow не перезапускать и не подменять корректирующими USER сообщениями.
Один комплексный native Review и Manager остаются для его результата;
host не ждёт отладки промптов, чтобы исправлять код и проверять интерфейс.

# Порядок и критерии

- [ ] **1. Устранить блокирующий отказ финализации runtime.** Сопровождать
  текущий запуск; снять exact Pod/run/session/turn/attempt stage/reason до
  удаления Pod. Воспроизвести и исправить доказанную кодовую причину,
  проверить адресным тестом и нормальной публикацией результата. Не обходить
  workspace quotas, canary, admission, owner/tenant или provider ограничения.
- [x] **2. Обеспечить основной пользовательский путь.** Провайдер/модель,
  системный и проектный помощник, диалог/сообщения/инструменты, Stop/очередь,
  проект/сотрудник/процесс, graph/rejoin, файлы и результаты. Проверить только
  изменённые и ранее не доказанные части; фиксы по Console, Network и backend.
  Не тратить время на качество промптов и анализ бизнес-ответов ролей.
- [x] **3. Проверить код доступности аккаунта и лимитов.** Авторизованный
  аккаунт не блокируется ошибочной конфигурацией; weekly exhaustion не
  трактуется как запрет при доступных credits. Не выдумывать внешний баланс,
  units/charging/API или live PASS. Нет данных — честное UNKNOWN, не отказ
  доступа по предположению; security/provider rejection не обходить.
  Account/catalog/renewal адресный кодовый путь PASS; ошибочного weekly-zero
  gate нет, реальный обычный запуск SUCCEEDED. Financial balance/units/charging
  остаются UNKNOWN и не объявляются проверенными или реализованным widget1796.
- [x] **4. Закрыть критические frontend/realtime регрессии.** Direct file
  links, session isolation/rejoin и очевидные мешающие демонстрации layout
  дефекты. Каждый новый экран: desktop/mobile screenshot, Console/relevant
  Network; только быстрые применимые tests/lint/types/build. Уже доставленный
  узкий FilesWorkspace overlay проверить и штатно включить в clean checkout.
  Direct-link LIVE PASS; late Send/Stop ACK RED→GREEN195 tests и отдельный
  browser UI fixture PASS, synthetic/serverMutations0. SOURCE/host/mount двух
  overlays MATCH; clean checkout включение относится к этапу5. Это не QA65 PASS.
- [ ] **5. Зафиксировать и доставить финальное дерево.** Commit/push/readback
  того же PR, краткий список экранов/сценариев и оставшихся ограничений.
  Bootstrap merge только при фактических разрешённых gates, без bypass;
  fresh main/deployment/source/mount/serving/image/config proof. Если защита
  требует нового owner действия, сообщить точный blocker, не обходить её.
- [ ] **6. Финальный smoke и передача владельцу до срока.** Успешный обычный
  запуск с опубликованным результатом; рабочие owner UI/SSO/WebSocket,
  чаты/Stop/graph/files, неаварийные основные сервисы. Отчёт: что работает,
  exact serving/tree SHA, URL/PR, подтверждённые проверки и остатки полного QA.
  Незавершённое не объявлять выполненным ради срока.

# Параллельные зоны

| Исполнитель | Зона | Ограничение |
| --- | --- | --- |
| Основной | Chrome, текущий native run, интеграция, HOT delivery, GitHub и отчёт | Единственный владелец live изменений и общих контрактов |
| Runtime исполнитель | agent-runner completion/workspace диагностика и адресный код | Изолированная ветка; не менять policy/права/промпты/активные Pod |
| Frontend исполнитель | Прикладные UI/realtime регрессии и быстрые тесты | Не пользоваться общей Chrome вкладкой; не трогать FilesWorkspace overlay |
| Account исполнитель | Account/catalog/weekly/credits кодовый путь и адресные тесты | Без live billing/OAuth/секретов и без изменения native бизнес-ветки |

Каждый исполнитель читает применимые проектные правила, использует отдельную
ветку/worktree от current SOURCE и приносит точный diff/тесты. Один shared
contract или library меняет только согласованный основной владелец. Никаких
новых Issue/PR, долгих review или независимого deploy без согласования.

# Сохранение среды

Trusted development cluster, не staging/production. Без STT/device-code,
ручного SQL, experimental/internal-only API, stale credentials и security
bypass. Секреты только из разрешённого owner источника, без раскрытия.
Chrome MCP: list_pages каждые10–15 calls/5–10мин; reload своей вкладки раз
в5мин после проверки ввода; чужие вкладки не менять. Source/base и узкие HOT
overlays фиксировать честно; серверы не перезапускать из-за frontend правки.
Applied migrations forward-only, данные и активные workloads сохранять.

# Адресный checkpoint 15.10.2026 00:36 Саратов

- SOURCE9e1d75eb: late Send/Stop selection fix7ee8f9ac; основной повтор195 tests
  PASS3.67s, исполнитель lint/types/build PASS. Browser fixture Send/Stop A→B
  →delayed ACK остаётсяB, Console0/Network0, без server writes/ИИ. Первый
  helper FAIL на async readiness исправлен только в оснастке.
- Runtime9e1d75eb: missing completionArtifacts observation воспроизведён
  RED→GREEN; существующие PUBLISH_RESULT/closed reason, без смены guard/квот.
  ROOT app whole unit PASS17.207s/vet exit0; исполнитель race/build PASS.
  Это диагностика, actual historical rejecting guard остаётся UNKNOWN.
- Account7c91943d: адресные maintenance/catalog/client/usage tests PASS,
  доказанного дополнительного blocker нет. CP не имеет weekly-zero/credits
  gate; свежий exact credential/catalog eligibility используется штатно.
  Live paid balance/units/charging не проверены и не выдумываются.
- Runner full image14e8162e собран штатным build-local-runner из source9e,
  exact manifest/binary provenance и local node import PASS. Policy/admission/
  defaults/project ENV не изменены, текущий image5cc не заменён.
- Native Architect SUCCEEDED: пять outputs owner EOF/hash/rejoin MATCH;
  CODE_ONLY допуск Developer, не live finance. Developer u35bu RUNNING,
  exact UID/input/runtime/image ACK; long closed observer и read-only outbox
  metadata sensor подключены до terminal. Нет новых USER инструкций.

# Адресный checkpoint 15.10.2026 01:13 Саратов

- HOT/SOURCE/remote dc97665e clean, штатный FF после owner idle0; оба своих
  frontend overlays включены без потери других правок. Promoted85/pinsf822
  сохранены. CP/RC ELF38549814/e861d02e совпадают прежним проверенным binaries.
- Отдельный normal AGENT smoke nuBy SUCCEEDED/version3: Go1.26.6,2+2→4,
  exit0, actual runtime-smoke.md опубликован. Все3 results owner exact
  revision EOF/size/SHA/metadata rejoin MATCH3/3; desktop/mobile UI просмотрен,
  overflowfalse/Console0/Network0/API200. Ни GitHub, ни настройки не менялись.
- Native business hC5h FAILED на semantic BLOCKED прежней ветки; Developer
  u35bu технически SUCCEEDED и4 results полностью опубликованы/прочитаны.
  Не новая реализация/tests/Review1796 и не устранение historical guard.
  На продолжение этой бизнес-задачи и prompt debugging время не тратится.
- Runner14e из exact9e прошёл publish→preflight→policy→binding→defaults→verify
  штатными repo-owned скриптами. Actual CP/RC default reference14e, exact
  policy07691e4b,5 основных Deployment Ready1/1. Existing project role images
  не переназначались; политика/guard/прочие promoted pins сохранены.
- Во время штатной maintenance gateway был0 и API503; после open/полного
  verify/reload API200 и Console0. Эти transient errors не скрываются.
  Остались final source фиксация/merge/main/readback/handoff; stage1 требует
  честного ограничения по невоспроизведённой historical primary причине.

# Адресный checkpoint 15.10.2026 01:25 Саратов

- HOT/SOURCE/remote9a94bae2 clean MATCH после approved owner idle FF21:16;
  новая интеграция3db94948 добавляет только два kernel fixture-теста, не
  production-код и не новый OCI. ROOT повторил fixture на exact3db94948:
  PASS2.56s/vet/diff-check, реальные процессы UID10001/10002 без provider/сети.
- Local Git worktree и7000 sparse cache files:7625 записей/114761864B,
  normalization205ms при прежнем2s, canary131ms. Семь flat outputs дают
  девять собранных artifacts/1201882B; canonical completion Validate PASS.
  Приватные cache bodies mode0600 остаются EACCES для runner UID10001.
- Negative guards PASS:10626 записей → ENTRY_COUNT_EXCEEDED,
  1075454089B → BYTE_QUOTA_EXCEEDED, symlink → FILE_TYPE_INVALID.
  Квоты10k/1GiB, canary, права и production-поведение не ослаблены.
- Historical primary NOT_REPRODUCED/UNKNOWN, сравнение старого OCI NOT RUN.
  Stage1 остаётся открытым: успешный synthetic fixture и actual normal smoke
  не выдают утраченную первичную диагностику за установленную причину.
- MCP owner UI connected/без overflow, actual session endpoint200 и Console0.
  Ошибочная ручная probe /api/v1/auth/session дала404; правильный endpoint
  /api/v1/session200. Это ошибка диагностической команды, не дефект приложения.
  Merge/main/final handoff остаются отдельными открытыми этапами.

# Адресный checkpoint 15.10.2026 01:31 Саратов

- SOURCE/remote/HOT07b278bb clean MATCH, approved idle FF21:26:33;
  promoted85/pinsf822 сохранены. Test-only fixture не требует нового OCI.
- Отдельный normal AGENT run_xVvhlfYouMbeI_q-BHlIzVkk SUCCEEDED/version3:
  фактический Node.js стандартной библиотеки создал7000 ordinary files
  по4096B в собственном work и6data outputs по200000B плюс report-heavy.md.
  Ни GitHub, ни сеть, ни настройки, ни прежняя бизнес-задача не изменялись.
- Все9 собственных CLEAN artifacts прочитаны owner по exact revision до EOF;
  size/SHA256/fresh metadata version/revision/digest MATCH9/9, всего1207297B.
  Report6560B/SHA058f0a6c содержит фактическую команду и полное чтение файлов.
- Actual Pod59cfddd8/image5cc/input44136fa3/runtime eec3786f/executionabc91a15
  связан ранним ACK. Observer89970 после terminal stream EOF: NOT_CAPTURED,
  не failure и не итоговый diagnostic rejoin PASS. Exact Pod штатно удалён.
- Desktop/mobile screenshot просмотрены, Console0/Network0/API200,
  overflowfalse, connected, session200. Все22 основных Deployment Ready.
  Actual heavy publication PASS не доказывает historical primary repair;
  stage1/merge/main/final handoff остаются открытыми, QA65 не объявлен PASS.
