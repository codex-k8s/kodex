---
id: OPS-PROTOTYPE-HANDOFF-1031
title: Единый прототип перед построчным ревью
type: operations
status: approved
owner: manager
version: 1.1.0
updated: 2026-10-20
---

# Единый прототип перед построчным ревью

Связанные задачи: #1031/#1223/#1797. План не является актом приёмки.

## Решение владельца 20.10.2026

Выбран вариант 1: после фактических проверок пунктов 1–3 разрешено объединить
актуальные правки через PR в `main`, завершить связанные открытые PR и передать
единый SHA на последующее построчное ревью. Это ограниченное исключение из
порядка предварительного review/human gate. Оно не отменяет authority,
изоляцию и проверки затронутых сценариев, не разрешает известный fail-open,
прямой push в `main`, обход GitHub protection или подмену решений владельца
внутри платформы. Полное ревью и ручная приёмка остаются в бэклоге ниже.

## Выполняемый объём и критерии завершения

- [ ] **1. Единая версия.** Полезные правки сохранены в опубликованной истории;
  конкурирующие реализации сопоставлены и сведены в одну. Итоговый кандидат
  находится в `main`, связанные PR слиты либо закрыты как заменённые со ссылкой
  на сохранённые изменения. Есть точные SHA, source/image/migration readback.
- [x] **2. Основной сценарий.** Проверены модель, системный и проектный помощники,
  окружение, сотрудник, процесс, реальный ход модели, делегирование, Human Gate,
  результат/файл и продолжение. Пользовательские запросы не требуют знания ID.
  Опасные внешние действия за владельца не подтверждаются; безопасный
  проверочный gate не разрешает настоящую отправку письма или внешний git push.
- [x] **3. Устойчивость и изоляция.** Проверены независимые диалоги, лимит 10 и
  очередь 11-го, изменение/снижение лимита, Stop/retry, rejoin/reconnect,
  контролируемый restart собственных реплик, границы проектов/пользователей.
  Synthetic и live результаты разделены; unit/green Pod не заменяет живой путь.

## Консолидация

Исходный `main`: `8c19ffb76328ae559698fae2806be79ef0bd64ae`.
Цепочка: #1811 → #1815 → #1817 → #1820 → #1821.
В неё частично перенесён #1813: сверить оба commit, сохранив защиту смешанных
версий. Из #1808/#1809 сохранить одну полную реализацию лимитов/credits;
неизвестные upstream данные не подменять выдуманными финансовыми измерениями.
#1799 содержит только Git-cycle receipt, не доказательство готового Workflow.
Закрытие Issue определяется собственными критериями, а не слиянием PR.

## Оставшийся бэклог: пункты 4–11

| Пункт | Что требуется и критерий завершения | Задачи |
| --- | --- | --- |
| 4. Построчное ревью | Весь собственный backend/frontend/SQL/scripts/deploy/CI и сквозные контракты. Для generated — воспроизводимость, зависимостей — версии/SBOM/лицензии. Карта покрытия файлов и findings на точном итоговом SHA. | #1031/#1223 |
| 5. Security findings | Устранить находки, обновить сканы зависимостей/образов; проверить tenant/authority/secrets/egress/Human Gate отрицательными сценариями. Для каждого finding — исправление с проверкой либо явное ограниченное решение владельца о риске. Старые числа CVE не актуальный scan. Production-hardening #1729 не автоматический блокер trusted-cluster прототипа. | #1482/#1031 |
| 6. Полная почтовая цепочка | Карантин → AV → отдельный смысловой анализ → серверный допуск точной версии → черновик → подтверждение → send. Реальные A1/A2/A3, запрет send без решения, новое решение при изменении полей, отсутствие дубля. Подтверждённая отправка не доказывает всю цепочку. | #1812 |
| 7. Backup/restore | Активировать backup-controller, сохранить БД/файлы/необходимые настройки и ключи, восстановить в изолированный контур; проверить сессии/файлы/SSO, описать RPO/RTO. Dump/TOC недостаточно. | #1003 |
| 8. Установщик | Довести существующий installer: managed/existing зависимости, безопасный preview, повтор/обновление, необязательные GitHub и заранее подключённая модель. Проверить чистую установку и существующий Kubernetes без изменения чужих ресурсов. | #1791/#849/#776 |
| 9. Документация | Быстрый старт пользователя, администрирование, установка/обновление, backup/restore, диагностика и ограничения. Новый пользователь проходит сценарий без знания внутренних ID; планируемое отделено от доступного. | #1791/#1031 |
| 10. Итоговый baseline | Полный применимый Go/PostgreSQL/frontend/contracts/render/browser/security контур единого кандидата; команды и PASS/FAIL/NOT RUN на точном SHA. Адресный smoke пунктов 1–3 не общий PASS, отсутствие GitHub checks не CI PASS. | #1031/#1223 |
| 11. Приёмка владельца | Короткие сценарии со ссылками: onboarding, помощники, сотрудник, процесс, подтверждение, файлы, Stop/продолжение, корзина. Владелец проверяет стабильную версию; дефекты фиксируются и затронутые сценарии перепроверяются. Merge не заменяет приёмку. | #1031 |

## Журнал

20.10.2026 — PREPARED: девять PR открыты/Draft; `main` на исходном SHA.
Все 22 Deployment имеют 2/2 ready — только инфраструктурный readback.
Предварительное объединение #1809 с #1821 выявило десять конфликтующих файлов,
включая два generated: их нужно перегенерировать, не исправлять вручную.
Ветка кандидата создана отдельно от обслуживающих исходников.
Пункты 1–3 пока не объявляются PASS.

### Консолидация и адресные проверки 20.10

В #1822 объединяется полный вариант #1809, включающий историю #1808.
Разрешены конфликты Proto/runtime activity/callback/runner/WS; generated
источники получены штатными генераторами. Сохранены process-version observation,
immutable image/admission и purge исправления более новой цепочки.
Скрипт #1813 `mailbox-worker-pause.mjs` сохранён отдельно от уже перенесённого
почтового доменного исправления.

Финансовые миграции из ещё не развёрнутых веток перенесены в
20261020000100/20261020000200: прежний номер второй совпадал с применённой
миграцией renewal. Применённые renewal/purge/AV миграции не менялись.
Строгий callback decoder принимает существующий `provider_process`, но запрещает
его смешение с financial/message; отдельные HTTP и shared ABI регрессии добавлены.

Смена AGENT/MEMBERSHIP/PLATFORM_MEMBERSHIP требует authoritative resync с floor
точного события: полный FIFO не позволяет пропустить этот authority wake.
SYSTEM_ASSISTANT heartbeat сохраняет адресный delta/read путь, так как не меняет
eligibility проектных агентов финансового каталога. Новая локальная admission
по проценту лимита не вводится; notification только будит supported read и не
публикует произвольный upstream payload в переписку.

| Изменённый переход | Authority / результат / событие / потребитель |
| --- | --- |
| ClaimExecution | Прежний trusted workload и owner candidate transaction; exact node/session turn переводятся в RUNNING атомарно; прежний TURN_STARTED, без нового события или права |
| Financial progress | Тот же ReportExecutionProgress и exact lease/fence/generation, root/node/session/turn/attempt/revision/current credential; collector ordering и replay под root/account locks; PROVIDER_ACCOUNT_CHANGED только для принятого нового наблюдения в owner transaction |
| Cancel/terminal/retry | Прежняя owner-транзакция отзывает lease; финансовый replay не оживляет граф и не выдаёт новый runtime grant; authoritative Run/Graph и прежние terminal events |
| Authority wake / rejoin | Exact owner cursor не ниже исходного сигнала, PLATFORM_RESYNC_REQUIRED очищает financial cache и требует свежий snapshot; нет REST polling финансовой сводки |
| Applied configuration plan | Проверенный staff actor, version/OCC/idempotency, validation перед application; receipt и изменения одной транзакции; внешний effect не подтверждается за владельца |

PASS LOCAL на содержимом кандидата до merge commit: Proto lint/build/codegen,
AsyncAPI reproduction, frontend lint/typecheck/build,42 finance/realtime и120
assistant store/timeline tests; весь runtimecontract, runtime-controller и
WebSocket transport. Полный agent-runner повторяется после согласования старой
проверки подавления notification с новым wake-only поведением.

PASS DISPOSABLE: bootstrap, parallel lifecycle, project profiles и system-run
scope —173.196s; financial activity/semantic replay/terminal —4.229s после
исправления claim. До исправления последний тест был FAIL: узел RUNNING, exact
session turn оставался QUEUED. Проверка допуска не ослаблялась; исправлен producer
перехода. Полный выбранный PostgreSQL контур повторяется на новом содержимом.
Это не полный baseline пункта10 и не live acceptance всех пунктов2–3.

LIVE (обслуживающая версия до консолидации): через отдельный Playwright Chrome
подтверждены штатный SSO/API session200, bootstrap200, защищённый WebSocket,
onboarding с готовой моделью gpt-6.1-sol/medium. Обычным запросом без refs
системный помощник создал typed plan; validation/application200, отдельный проект
«Готовность прототипа» создан с реальной квитанцией. Никакие письма/GitHub writes
не выполнялись. Chrome MCP доступен для списка вкладок; результат проверки UI
через Playwright не выдаётся за MCP результат.

На `ac0cb4b8cebb3bb5c6c6c73f206d0373ff30c53d` завершены: полный agent-runner,
runtime-controller, WebSocket transport,120 assistant frontend tests и повтор
выбранного PostgreSQL контура — PASS,170.786s. Четыре OCI (control-plane,
control-api-gateway,runtime-controller,staff-control-center) собраны из exact
tracked archive этого SHA. Публикация/node pull/live на момент записи NOT RUN.

Квитанция #1799 сохранена без расширения её значения: это исторический
ограниченный Git-cycle, а не общий QA PASS. Для двух новых финансовых миграций
добавлен закрытый профиль `provider-financial-observations` с exact readback
20261020000200. Source inspector принимает также равнозначный HTTPS origin без
`.git`; чужой репозиторий, query-параметры и другой host остаются запрещены.

Миграция20261020000200 и публикация четырёх OCI — PASS LIVE. Первая доставка
source-only — FAIL: новые Pod получили StartError на отсутствующем `.env`
mountpoint; старые 2/2 реплики продолжали обслуживать запросы. Причина общая:
worktree создаёт `.git`-файл, несовместимый с directory mask. Исправлен именно
producer: новый application source — чистый локальный Git clone с публичным
origin и штатной подготовкой пустых mask mountpoints; ignored data не копируются.
15 regression tests подготовки — PASS LOCAL после исправления. Далее применяется
уже собранный compiled cutover CP/API; это не называется успешным hot-reload.

CP compiled cutover фактически запущен: две Ready реплики с digest сборки ac0.
Первый readback скрипта — FAIL `STABLE_FOREIGN_SERVING_POD`: общий app-name
захватил завершённые pod мигратора. Исправлен selector инвентаризации по
app-name и component из авторитетного Deployment; строгая ReplicaSet lineage
не ослабляется.14 тестов Go release — PASS LOCAL. Незавершённая часть API
получает отдельный новый план по фактическому состоянию, без повтора CP PATCH.

Итоговый live serving readback четырёх компонентов — PASS: source ac0cb4b8c,
по две Ready реплики, точные runtime imageID digest и spec fingerprints.
CP/API переведены в compiled; runtime-controller/PWA обновлены image-only с
сохранением source/provenance metadata и rollback. Ошибочный первый source-only
план остался FAIL, его результат не переписан. Новый API compiled план — PASS.
SSO/public routing сохранены, зависимости не переустанавливались.

Runner ac0 собран full profile, binary provenance проверен; manifest
`sha256:b0efb2cba0c73992493ca77eada27a9d340842e8449bc1000038f2a7f2a3b41a`,
публикация exact bytes в registry — PASS. Подготовлена новая immutable policy,
активация пока NOT RUN. Скрипты policy поддерживают compiled staging Deployment
и оба закрытых repo-owned имени reader; production или чужой image не допускаются.
20 policy tests — PASS LOCAL; расширенный reader assertion — отдельный PASS LOCAL.
Созданы11 собственных пустых проектных диалогов для следующей проверки лимита;
это не PASS десяти одновременных выполнений.

20.10: immutable runner policy и default b0 активированы штатными phased
скриптами, API снова 2/2. После перезапуска сервера отсутствовал точный registry
host route на двух узлах: исправление вынесено в проверяемый repo-owned helper.
HTTPS graph и CRI pull exact digest на обоих узлах — PASS; постоянное
восстановление этой настройки при bootstrap/restart относится к пункту8.

Реальный project turn выявил устаревший опубликованный default ENV pin.
Авторитетный readiness уже сообщал DEFAULT_ROLE_IMAGE_STALE, но AddTurn
создавал ход. Добавлена проверка до queue/interrupt; выбранные disposable
PostgreSQL project-profile/parallel-lifecycle проверки — PASS,36.267s.
Новая версия окружения опубликована через штатные draft/validation/impact API,
четыре собственных QA-потребителя переназначены, текущий ENV ready=true.
Исторические версии не менялись, admission не обходился.

Следующий canary и retry — FAIL ACTIVITY_DELIVERY до результата модели.
Добавлена закрытая диагностика progress callback (стадия и gRPC code, без
payload). Устранение этого дефекта и live пункты2–3 ещё не завершены.

Проверка обновления compiled CP выявила, что cutover-инструмент рассчитан только
на переход с hot-reload. Для уже compiled CP/API использован image-only механизм
с exact source/provenance и rollback; его закрытый реестр расширен на эти два
существующих приложения.13 unit tests механизма — PASS LOCAL.

ACTIVITY_DELIVERY установлен до точного producer defect: AddAssistantTurn
вставлял session_turns.state=COMPLETED до начала исполнения. Обычный claim
закономерно не переводил такой terminal turn в RUNNING; финансовый guard
закрыто отклонял событие. Новый ход теперь QUEUED; claim атомарно переводит
его в RUNNING, terminal закрывает прежним owner-путём. Исторические строки
не переписываются. Новый project-assistant financial component воспроизвёл
FAIL до изменения SQL, после исправления — PASS; выбранный project/parallel/
activity PostgreSQL контур целиком PASS,39.008s. Новый live ещё NOT RUN.

Run route после потери availability snapshot во время inflight read мог
пропустить восстановление: watcher не наблюдал завершение loading. Он теперь
реагирует на этот точный переход; ошибки403/404 по-прежнему останавливают
повторы, старый graph не считается свежим допуском.18 выбранных frontend
rejoin/mutation/refresh tests — PASS LOCAL. Доставка и live подтверждение
исправления пока NOT RUN.

CP8402 и PWAe7dab7 доставлены по две Ready реплики с exact digest/source.
Frontend lint/typecheck/build — PASS LOCAL. Реальный project assistant прошёл
прежний отказ финансового callback, получил текущее наблюдение лимита/кредитов
и создал план CREATE_WORKFLOW; validation/application200, процесс опубликован
через UI. Сценарий выполнения начат, итог ещё не принят.

Выявлена вторая гонка: waitTerminal вызывал финансовый RPC, который мог
потребить turn/completed; после возврата цикл всё равно ожидал следующее
сообщение. Regression test воспроизвёл FAIL context canceled при уже принятом
terminal, после повторной проверки terminal перед select — PASS. Полный
agent-runner — PASS LOCAL. Это не отменяет реальные terminal/lease guards;
доставка нового runner и повтор workflow остаются NOT RUN.

Runner b799/d6a2 активирован штатными policy/binding/default фазами. HTTPS
граф и CRI pull на двух узлах — PASS; API открыт2/2. Два собственных старых
QA-запуска отменены API, runtime-поды удалены. ENV проверочного проекта
опубликован как версия3, выбранные четыре привязки APPLIED, готовность=true.
Новый canary `run_ri1_m6XDtokSOFyLfFRmYofr` — SUCCEEDED; это live подтверждение
устранения зависания после terminal, не общий приёмочный PASS.

Повтор workflow `run_nRdrAVZydUIxyl80pymmn6Qq` дошёл до Human Gate, однако
чтение фактического result.md выявило FAIL: файл содержит диагностику, а не
требуемый report.md. Помощник назначил координатора всем этапам и поручил
делегировать отсутствующему в списке исполнителю. Подтверждение не выполнялось.
Добавлены проверка противоречивого self-only/required-delegate профиля и
versioned core-v54 с общей рекомендацией назначать фактического исполнителя,
не путать capability с delegationTargets и привязывать gate к результату.
Исторические core-v52/v53 bytes сохранены. Адресные systemassistant/workflow
unit tests — PASS LOCAL; новые доставка и основной сценарий ещё NOT RUN.

CP385415 доставлен2/2. PostgreSQL workflow component набор — PASS85.053s;
отдельный AssistantCoreInheritanceComponent — PASS10.604s. Живой read выявил
регрессию новой проверки: structurally valid self-only профиль стал возвращать
503 даже при открытии для исправления. Разделены структурная проверка чтения и
проверка исполнимости. Повреждённый JSON по-прежнему отклоняется; неисполнимый
профиль читается, но readiness BLOCKED/DEPENDENCY_UNAVAILABLE, публикация и
запуск запрещены. Добавлены scan/assistant-metadata unit и readiness component
регрессии. Unit — PASS LOCAL; доставка этой поправки и повтор live ещё NOT RUN.

На c6d6235b12a8da9a5106a2feb5323c5a95f3debf: WorkflowLaunchComponent —
PASS36.469s; BootstrapComponent/schedule_race_retry_prompt_snapshot_and_expiry,
включая readiness regression, — PASS4.673s. Ошибочный фильтр SchedulerComponent
не выбрал тестов и не считается PASS. CP c6d6235 доставлен2/2: live GET200,
прежняя неисполнимая публикация BLOCKED, исправленный draft прошёл VALIDATE и
PUBLISH обычными API (текущая версия6).

LIVE: при лимите2 два exact node RUNNING, третий QUEUED; затем одновременно
10 active provider executions и один QUEUED из11 отдельных сессий. Снижение
до2 сохранило10 активных; Stop одного дал CANCELLED только его графу,
active9, остальные продолжили работу, ожидающий остался QUEUED. Возврат10
допустил ожидающий. Retry выбранного отменённого запуска создал новую попытку2
через UI. Runtime binary SHA256 совпал с b799 provenance. Обновление CP прошло
при активных ходах без их отмены. Полная оставшаяся проверка пункта3 ещё NOT RUN.

Скриншот parallel-active выявил UX FAIL: sticky сводка лимитов перекрывала
заголовок в фиксированном flex workspace запуска. Для этого режима снят sticky;
обычные прокручиваемые экраны не меняются. Доставка и visual readback NOT RUN.

Основной повтор run_k75KLTB1A1r4SrWIJuHHxcDw: отдельный Автор получил
делегирование и создал report.md (art_mEG7OHc6xmd2UM7q28SXKodV,2274bytes,
версия1,CLEAN). Полный PREVIEW200 содержит заданные факты и неизвестность,
не утверждает общую готовность платформы. Точный внутренний Human Gate
gat_gcQd9C4DjGzKRRdQv7IA9jEM подтверждён с техническим комментарием без
внешних действий; root SUCCEEDED. Это не решение владельца о приёмке прототипа.

LIVE изоляция: разные session refs у11 диалогов, ответы2 и3 соответствуют
своим номерам; отдельное продолжение диалога3 вернуло3. ЧерновикA восстановился
после перехода вB, полеB перед вводом было пустым; черновики не отправлялись.
Активный sleep90 в диалоге8 отменён INTERRUPT_ACTIVE, замена завершена.
Оставшийся старый QUEUE ход выполнился после interrupt, но повторил ответ
«прервано» вместо своей задачи — FAIL семантики текущего submission. Добавлен
versioned continuation-task-projection-v2: последовательность истории не
определяет текущую задачу; exact Task/INPUT/history остаются прежними. Проверка
producer exact task после приоритетного interrupt добавлена в component suite.

После завершения workflow выявлен ещё FAIL: required input проверялся против
nil, поэтому continuation не предлагался. Read/list теперь используют input
соответствующего доступного Run, command наследует его только после свежей
authority той же сессии. Текущая схема повторно проверяется, а несовместимое
новое обязательное поле закрывает запуск. Preview использует те же значения.
Матрица: owner API/session turn → AddSessionTurn → доступный SUCCEEDED Run →
exact inherited input → прежний LaunchRun/OCC/receipt/audit/RUN_CREATED и
CONTINUES → текущий RuntimeRevision/runner; отказ атомарный, без нового события.
Unit continuation — PASS LOCAL; component/delivery/live ещё NOT RUN.

Frontend2b50: lint/typecheck,11 адресных tests и build — PASS LOCAL.
На старой переписке заметны повторные generic progress-карточки; это открытое
UX-замечание для пункта11, а не якобы исправленный результат.

На aed645bc0c2d0f69b1de6ff579393f95aa2714c6 unit continuation — PASS0.541s;
WorkflowLaunchComponent + AssistantParallelLifecycleComponent — PASS45.396s.
Новая проверка доказывает доступность required-input workflow в resumable
каталоге, exact наследование fields в claim, новую delegation/callback цепочку
и закрытый отказ при добавлении несовместимого обязательного поля. Первые
итерации новой оснастки не завершали delegated graph и затем не учитывали
штатный coordinator envelope; эти FAIL не выдавались за продуктовый PASS.

После технического APPROVE реальный root завершён, но финальное сообщение
координатора неверно утверждало отсутствие решения человека. Старый callback
текст сформирован до APPROVE, а свежая workflow-проекция не содержала состояния
gate. В versioned guidance-v2 добавлен bounded read последнего result gate
точного шага/текущего root (без integration invocation и комментариев).
Ref/state/decision проходят закрытую проверку и входят в новый immutable prompt
snapshot; это информация о решении, не полномочие и не send approval.
Исторические сообщения не переписаны. Доставка и повтор live NOT RUN.

Продолжение required-input процесса через UI создало новый root в той же
сессии, отдельное делегирование и новый report.md; root WAITING_HUMAN.
Live v2 result-gate проекция оказалась пустой: gate относится к дочернему
HUMAN_GATE node, а не к step node напрямую. Исправлен точный parent/root/org
join; новая disposable проверка проходит gate и проверяет APPROVED/APPROVE
в свежем coordinator snapshot — PASS8.288s. Доставка этой поправки NOT RUN.

Повтор QUEUE/INTERRUPT_ACTIVE после aed/b0 — FAIL: актуальные задачи доходят
до физического prompt и PROVIDER_INPUT_ACKNOWLEDGED с правильным SHA, но ответ
повторяет старое «прервано». Scoped read native rollout подтвердил текущий
USER task и старую developer-инструкцию с прежней задачей без свежей замены.
В точном upstream rust-v0.160.0 generic developerInstructions вставляется при
initial context и не обновляется cold resume. Renderer v2 сам по себе не
исправил live поведение. Новый путь использует native collaborationMode каждого
turn/start с полными текущими instructions/model/reasoning; thread binding
устойчивый, история не переписана, authority и sandbox не расширены.
Проверены OpenAI App Server docs и точный upstream source. Live нового runner
ещё NOT RUN; этот дефект не объявлен устранённым по одному unit test.

На68a9978ac полный agent-runner — PASS LOCAL. CP с corrected result-gate join
доставлен2/2, provenance/image readback — PASS; новый runner собран full profile,
точный binary SHA256=8f430680d3c144ea918c8eeaaa96bdaf356df1611059477c1a7cd9622215f27c.
Phased activation выполняется с сохранением exact admission; live пока NOT RUN.

Долгое пустое открытие continuation и recovering WS — отдельный FAIL. Включённый
owner run snapshot отклонял предыдущий root той же session: поддерживалась
только RETRY_OF lineage. Добавлена точная root-to-root CONTINUES проверка
owner → gateway → frontend; same-run attempt CONTINUES не расширяет eligibility.
Новые отрицательные проверки включают foreign session/state/node/edge/retry
и duplicate. Disposable required-input continuation с GetRunGraphSnapshot —
PASS7.550s; CP unit PASS0.064s, gateway WS PASS3.430s,32 snapshot frontend tests
PASS LOCAL. Authority/history atomicity не ослаблена; доставка/live NOT RUN.

Выявлен retained own Pod с остановленным role-runtime: окончательный отказ
CompleteExecution после отзыва lease очищал ресурсы только при deadline.
Cancel/interrupt без deadline теперь тоже закрывает только exact own runtime
и tracker; внешний отказ не повторяется и не становится receipt. Проверка и
доставка этой поправки пока NOT RUN.

Полный runtime-controller с новой non-deadline denial regression — PASS LOCAL:
app0.434s/callback8.082s/workload1.141s. Frontend lint и101 store/realtime/
snapshot tests — PASS LOCAL. Эти результаты относятся к следующему кандидату,
его compiled доставка и окончательный live повтор ещё не подтверждены.

0fb76f459: frontend lint/typecheck/build и101 выбранный test — PASS LOCAL;
CP/API/runtime-controller/PWA доставлены по2 Ready реплики с exact source/image
metadata. Runner68a/f672 phased policy/default открыт, HTTPS graph и CRI pull
на двух узлах — PASS. Первый node probe с бюджетом240s остался FAIL BUDGET_EXCEEDED;
повтор360s завершён, прежний FAIL не переписан. Retained own Pod удалён штатным
controller restart, без ручной правки runtime/БД.

Окружение собственного проекта явно опубликовано как версия5 на новом exact
bootstrap artifact; четыре выбранных consumer APPLIED, ready=true. Версия4 с
прежним artifact осталась DEFAULT_ROLE_IMAGE_STALE: исторические pins не менялись.
Для создания текущего штатного bootstrap artifact создан собственный сотрудник
«Контроль продолжения», без сети или внешних прав.

Live новый turn в прежней проблемной сессии начал выполнять текущий sleep25.
Однако native rollout показал, что custom collaborationMode instructions
сохранены только в turn_context: catalog предпочёл свой preset developer text.
Это не PASS полной доставки инструкций. Выбран поддерживаемый additionalContext
application path с UTF-8 chunks3000bytes и отдельной exact revision identity, чтобы
избежать provider truncation. Модель получает полный snapshot native developer
fragments; история не переписывается, runtime checks не расширяются.
Сборка и live этого последнего исправления ещё NOT RUN.

### Итог адресной проверки пунктов 2–3, 20.10.2026

Следующие результаты заменяют прежние NOT RUN только для явно названных
сценариев. Прежние неуспешные попытки остаются в журнале. Исходники runner:
`dc42e31aefdf68d0ce71f1e8d01146be5db74a9d`; остальные четыре обслуживающих
компонента: `0fb76f45977817bd8542b15a5edf2aff91f6a798`. Их каталоги не менялись
между этими SHA. Последующий коммит этого отчёта меняет только документацию.

| Компонент | Фактически обслуживаемый OCI digest |
| --- | --- |
| control-plane | `sha256:960ce6cb016792d240334697b4c5b896ea2769cebe500d83753dcf20841be4ad` |
| control-api-gateway | `sha256:bb86b5bb9e2b3786d616e4cb481c842744c81c83530314710dce8237dda83828` |
| runtime-controller | `sha256:d1cfe081d2691749eb17f36112c13df727780a8246c4b0d32aaa0bc396959538` |
| staff-control-center | `sha256:4cbfbf1c68b56e6061d44ae29f8bd249a0cdfea15c4a5ac01771b4e844f7a56c` |
| agent-runner | `sha256:abd525662c1f418db0181a5f30f59f547924affe22ae49f87e0ae6a1e5847d07` |

PASS LIVE: phased policy/default activation, exact HTTPS graph и CRI pull
нового runner на обоих узлах; все22 Deployment имеют по2 Ready реплики.
Зависимости не переустанавливались. У собственного проекта опубликовано
окружение версии6, ready=true; четыре выбранных потребителя переназначены
штатным API. Исторические версии и чужие окружения не переписывались.
Последняя применённая миграция: `20261020000200`.

PASS LOCAL: полный agent-runner на предшествующем содержимом; после изменения
единственного chunk budget на3000bytes повторены codex/app tests —7.928s.
Проверка собирает большой Unicode snapshot обратно без потери байтов.
Native additionalContext(kind=application) передаёт полный текущий snapshot
developer fragments, а не catalog preset. Порог взят из точного upstream
`rust-v0.160.0` (`a956835d020762cb2b570053af06f643a11c0ecc`), а не предположения
о размере контекста. Проверены Context7 `/openai/codex` и
[официальные App Server docs](https://learn.chatgpt.com/docs/app-server#threads).
Transport/sandbox/grant boundary не расширялись.

**Основной сценарий — PASS LIVE в согласованном объёме.** Системный помощник
по обычному запросу без ID создал проект, проектный — typed plans сотрудников
и процесса. Применение прошло validation/application API. Модель
gpt-6.1-sol/medium и авторизованный аккаунт готовы, текущий лимит аккаунта10.
Отдельная финальная проверка системного помощника:
`cnv_wSoX-2PYMCtVC9mWlyyttINU`, запуски
`run_XE1eLwD2lOzJ8i2G_GOzMzI_` и `run_L3T8u7x8P-__Q-LjcbEulJ2z`:
два последовательных ответа «СИСТЕМА ОДИН.» / «СИСТЕМА ДВА.» — COMPLETED.
Его ранее настроенный собственный образ и набор инструментов сохранены;
пересборка всех пользовательских образов при обновлении относится к пункту8.

Финальный процесс:
[открыть успешный запуск](https://platform.kodex.works/projects/prj_hSgglhuNMDtlxosaXCI37zan/runs/run_PHNxGe3fxlCygTOpsZXrwbNp).
Продолжение отправлено настоящей формой «Ход работы» обычным русским текстом,
без refs. Required input унаследован из точного успешного предшественника;
архивированная сессия восстановлена штатной RESTORE task, создан отдельный
дочерний запуск. `run_PHNxGe3fxlCygTOpsZXrwbNp` и
`run_kj3-3uhKB06gUNrEJVeKhMTB` — SUCCEEDED.
Gate `gat_5JCKomzr3uxzg7aIdgoBWytF` подтверждён через настоящий UI только
как внутренний технический результат, не как ручная приёмка прототипа и не
разрешение внешнего действия. Свежий callback корректно увидел APPROVE.

Результат: `art_xHCsxdalQL0ofZtua5n8h82g`, report.md версии3,2532bytes,
CLEAN, SHA256 `efd702fc1be140b7a32fa06eeeb5234e7b45e5778ce83f8a6f2bf14571b0b29f`.
Настоящий UI «Файлы и знания» → «Результаты» → карточка → «Открыть» выдаёт
защищённый preview200 с точными байтами. Текст явно не объявляет ручную
приёмку выполненной. Preview-права не расширялись.
В callback Pod проверены exact native instructions:31455bytes/11chunks,
SHA256 `17d26670c974baa70e1b56592569970b5b7cb965e0e76b8de7107d589f55328a`;
binary SHA256 `d546379265d776a59b04a982ae183328b9746a0f67ffaabbafac7685f0f39633`
совпал с provenance новой сборки.

**Параллельность — PASS LIVE.** При лимите2 два активных и третий QUEUED;
при10 — десять активных в разных сессиях и одиннадцатый QUEUED. Снижение до2
не отменило действующие10; Stop отменил только выбранный запуск. Возврат10
допустил очередь. Retry создал отдельную попытку. Ответы независимых диалогов
не смешались, draft не перенёсся в другой диалог; archive/restore штатным API
сохранил историю. Контролируемая замена2 реплик CP не отменила активные ходы.

Финальный повтор ранее дефектного queue/interrupt:
`cnv_VcXokm81X8Fdzr0vJpYZtwRS`: hold
`run_DXiIUdchLq6LUEoeeVZCiORQ` — CANCELLED; priority
`run_z-AuMb4NHGJqSU4CNxPXhuUT` — SUCCEEDED, ответ «ПРИОРИТЕТ»;
queue `run_1JHWDmQaiziSwdV00BYa4bUr` — SUCCEEDED, ответ «ОЧЕРЕДЬ».
Порядок priority → прежняя queue сохранён, устаревшее «прервано» не повторяется.
Native hold snapshot80598bytes/27chunks совпал с текущим input целиком.
Завершённые собственные turn Pod очищены контроллером, SYSTEM warm сохранён.

Continuation snapshot/rejoin — PASS LIVE: один GET вместо повторяющегося
Unavailable, оба WebSocket потока снова live после штатного refreshSession.
Чужой проект не получает список этих диалогов. Граница другого пользователя
проверена disposable PostgreSQL project-profile/system-scope/parallel tests;
вход вторым реальным SSO-пользователем NOT RUN и не выдаётся за live PASS.
Отрицательные continuation tests охватывают foreign session/state/node/edge,
retry/duplicate/cycles. Selected PostgreSQL continuation snapshot7.550s,
gateway WS3.430s, frontend101tests/lint/typecheck/build и полный
runtime-controller — PASS LOCAL/DISPOSABLE на указанных исходниках.

**Браузер и UX.** Chrome MCP list/evaluate доступны; screenshot/console/network
инструменты зависли, поэтому изображения и сетевой журнал получены разрешённым
Playwright Chrome, не выдаются за MCP screenshot. Просмотрены скриншоты
активного/успешного графа и файла: результат доступен, header не перекрывает
содержимое, обратная связь графа не скрыта узлами. Финальные отдельные интервалы:
процесс20 и preview14 успешных API-ответов,0 console/page errors и0 HTTP errors.
Начальный SSO setup имел3 ожидаемых401 и одно предупреждение MIME модуля;
причина последнего не установлена, весь сеанс не объявляется безошибочным.
Недавние логи CP/API/controller:0 error/panic в выбранном10-минутном окне.

Остаток UX в пункте11: мелкие узлы даже после Fit, избыточные служебные progress
карточки, внутренние locator-ссылки в тексте модели вместо постоянной ссылки UI.
Файл открывается штатной карточкой; эти замечания не скрываются общим PASS.
Полный baseline, security review, реальная почта и ручная приёмка NOT RUN
в рамках этого завершения — остаются пунктами4–11, не объявляются готовыми.

**Передача кода.** Все последующие изменения только документальные. #1822
содержит цепочку #1811/#1815/#1817/#1820/#1821 и полный #1809 с #1808;
четыре почтовых payload файла #1813 совпадают с его head, адаптированная
реализация сохраняет более строгие проверки; квитанция #1799 сохранена точно.
На момент фиксации этого документа пункт1 намеренно ещё не отмечен: слияние
и отсутствие открытых PR проверяются после этой записи. Авторитетная квитанция
с итоговым main SHA, readback и состояниями всех заменённых PR публикуется в
[#1822](https://github.com/codex-k8s/kodex/pull/1822) после нормального merge.
Отсутствие GitHub check runs не является CI PASS. Секреты в отчёт не включены.
