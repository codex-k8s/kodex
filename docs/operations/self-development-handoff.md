---
id: OPS-DOC-SELFDEV-002
title: Точка продолжения самонастройки и dogfooding Kodex
type: operations
status: approved
owner: manager
version: 1.1.0
updated: 2026-10-05
---

# Текущее состояние

05.10.2026 владелец явно возобновил работу после переноса хранилища.
Перенос проверен; цель снова выполняется. Последний опубликованный checkpoint:
`8104899c21a13615aa01e1b1f1f9e0d912f5cade`. Новый полный runner и четыре
supply-chain image собраны; fresh render и supply-chain apply/readback
полностью завершены 05.10.2026 в17:59:24 UTC. Control plane, gateway и
контроллеры Ready. Core repair secret-broker PASS18:18:45 UTC: доставлен
exact native CLI0.160.0, без ослабления pin или startup barrier. Каталог
провайдера штатно восстановился; прежний native create conversation HTTP412
был закрытым отказом из-за expiry каталога, а не cached frontend version.
Native SYSTEM применил новый typed plan через UI, recipe v9/generation7;
его build завершён. Полный отчёт READY, первый admission REJECTED по двум
HIGH npm findings. OWNER UI принял exact риск для локального QA/dogfooding;
вторая attempt ACCEPTED, собственный artifact PROMOTED. Начат SYSTEM40
для назначения образа окружению. Inventory37/38 VERIFIED; npm PROBE_FAILED
исправлен в новом base image. SYSTEM40 environment revision22 опубликован.
SYSTEM41 typed plan штатно создал generation8 на exact rebuilt base85b5;
build COMPLETED, обязательные инструменты38/38 VERIFIED. Первый admission
REJECTED по двум прежним HIGH findings, теперь точно локализованным в bundled
pnpm11.11.0, а не обновлённом npm12.2.0. Для exact нового image/report через
OWNER UI принято новое локальное решение риска; attempt2 ACCEPTED, generation8
PROMOTED18:41 UTC. Запущен native SYSTEM42 для полного профиля. Новый
environment/instructions/config publish и actual provider prompt proof NOT RUN.
Ниже перечисленные старые checkpoints относятся к истории, а не к текущему HEAD.

- Issue: [#1797](https://github.com/codex-k8s/kodex/issues/1797).
- Draft PR: [#1798](https://github.com/codex-k8s/kodex/pull/1798), не слит.
- Ветка: `kodex-agent/issue-1797-self-development-bootstrap`.
- Исторический checkpoint реализации до CEL repair: `99f397c56a7dd97831011d70be73e82eb8999afd`.
- Исторический checkpoint завершения сборки и паузы: `1cde8237c535e19bc0cdcd742c64ce6a8f6251b3`.
- После этого checkpoint исправлена CEL-типизация admission policies;
  точный source и результаты адресных проверок фиксируются в журнале.
- Полное задание: [65 разделов QA](../qa/full-qa-task.md).
- Checklist и подробный хронологический журнал: [самонастройка](self-development-dogfooding.md).

# Исправлено в исходниках

Это перечень реализации, а не утверждение о полной живой приёмке.
Для каждого этапа точные SHA, PASS/FAIL/NOT RUN и ограничения находятся в журнале.

- Системная и проектная конфигурация помощников, typed self-configuration,
  отдельные диалоги и управление Stop/queue/interrupt.
- Переписка с промежуточными ответами и компактными вызовами инструментов,
  scoped realtime/history/rejoin, отделение служебных событий от сообщений.
- Управляемые MCP/Context7 profiles, immutable runtime pins и настройки
  ApprovalPolicy из допустимого набора с серверной проверкой.
- Диагностика и восстановление образов: технические ошибки, expiry,
  provenance/ABI/evidence, история сборок и интерфейс выбора образа.
- Типизированный полный отчёт уязвимостей и интерфейс принятия риска
  администратором для точных image/report/policy. Новый подписанный admission
  не переписывает прежнее evidence и не обходит integrity/provenance/signature.
- Runtime toolchain и Chromium, ограниченный параллелизм сборки,
  защищённый импорт и фиксация OCI digest на каждой ноде.
- Безопасный maintenance barrier: завершённые init/Job и старые Evicted Pods
  проверяются по точному происхождению и двум native snapshots.
  Большие Kubernetes inventory передаются потоком, без ограничения argv.
- Адресная очистка устаревших кэшей с сохранением current/restore pins;
  дополнительные удаления после запроса паузы не выполнялись.
- Строгая нормализация явного пустого scanner fix state, проверенная на полном
  native отчёте; технический scan failure не подменяется решением о риске.
- Tool rows по38px с конкретными именами каталогов, раскрытием деталей и
  клавиатурной навигацией; screenshot на текущем hot reload проверен.

## Текущая точка продолжения

Предыдущий native recipe v8/generation6 завершил build, но его
admission scan завершился технической ошибкой прежнего projection handler;
artifact штатно завершён FAILED/ADMISSION_LEASE_EXPIRED; claim/lease очищены,
attempt terminal snapshot совпадает, один owner expiry receipt подтверждён.
Прежний SSA отказ16:09:52 UTC сохранён в журнале. Exact repo-owned restore
CHECK/server dry-run/APPLY завершён16:24:29 UTC: canonical script восстановлен
с field manager `kodex-local-dev`, без force-conflicts и удаления managedFields.
Новые worker images собраны на source `b5fe1bec` и импортированы на обе ноды;
Go inputs между `b5fe1bec` и `6efc5104` не менялись. Свежий render `6efc5104`
и полный ordinary supply-chain apply/readback PASS16:30:31 UTC подтвердили
активацию. Builder после двух startup restart с ErrMaterialization сам
восстановился до Ready16:29:02 UTC; точная подпричина UNKNOWN.

Повторный ROLE_ENVIRONMENTS отказ устранён bounded whole-transaction retry
только для PostgreSQL40001: новая транзакция повторно проверяет authority,
lease и fence. Адресные unit и SYSTEM/PROJECT PostgreSQL component PASS.
В native QA_CATALOG_RENEW_FIX_02 пять реальных tool events успешны, в том
числе три последовательных ROLE_ENVIRONMENTS; скриншот и Console проверены.
Устаревшие ожидания component fixture обновлены до утверждённого graph
100nodes/253edges без изменения production guard или applied migration.

Предыдущий admission recovery завершён: exact native Pod DNS и TCP к CP
проверены; actual client trusted profile и отсутствие proxy подтверждены.
Serving CP использует plaintext gRPC, поэтому TLS mismatch исключён.
Три native callback подтвердили REFUSED через закрытый потоковый classifier.
Двухсекундная dev-диагностика дала штатный terminal receipt и очистку workspace;
источник временного TCP отказа окончательно не доказан. Production bounded
WaitForReady только Fail/Expire уже реализован и проверен disposable TCP,
без повтора полученной server error; новый worker binary теперь активирован.
Ручного изменения SQL-состояния, claim/grant или ослабления NetworkPolicy
не было; terminal выполнен штатной owner-транзакцией.

Native SYSTEM39 в диалоге `cnv_h4JZw1FWPxVrsK_gxSx5gWgr` завершён:
шесть read tool events SUCCESS и один propose. Единственный typed plan
обновления recipe подтверждён через UI16:39:37 UTC; отдельный REQUEST_BUILD
и ручная подмена состояния не использовались. Авторитетный результат:
recipe v9/generation7, specSHA256
`742bdccb9ea4c2d831a8d135c1f90199fe8671490c54be3b034e18e256b31a61`;
Прежний digest `FROM` с префиксом `72b27` сохранён. Build
`imgbld_391ktSUxZEzhsxVdJjm97i0r`, attempt1, COMPLETED/version12/100%
подтверждён16:40:12 UTC. Новый artifact `imgart_-PQ2z3H-QfPi7dAYxUgBMsHm`
получил полный READY отчёт и REJECTED/version3/admissionRevision1.
Report SHA256 `c503f02a94e7003090e9171f01807da946c7e96e41f83d996244df6cb4025b96`,
4640 matches/2938 advisories/blocking2. OWNER UI16:48:41 UTC принял риск
для exact image/report/policy. Прежний REJECTED snapshot сохранён, отдельная
attempt2 `imgadm_qzaTBu3oOljYWt2PD7iWimEH` ACCEPTED16:50:49 UTC.
После OWNER UI promotion artifact достиг ACCEPTED/PROMOTED/version10,
recipe version10/promotedImageReady=true; exact digest `1c82da82` сохранён.
Inventory37/38 VERIFIED; npm PROBE_FAILED — открытое замечание.

Далее: typed SYSTEM environment → новый turn и tool/prompt proof →
SYSTEM создаёт проект и PROJECT → шесть ролей, grants и реальный Workflow.
Основной checklist2–15/6.1 остаётся открытым до фактических доказательств.

# Исторические проверки и сохранённые FAIL

- PASS: на source `99f397c5` каноническая supply-chain сборка четырёх компонентов
  с `build-jobs=4` и импортом exact digests на обе ноды.
- PASS: свежий render того же source. Это не serving/live acceptance.
- Исторический PASS: maintenance barrier подтверждён; пять Deployment оставлены
  с `spec/status replicas=0`, активных работ нет, promoted pins сохранены.
- FAIL: частичный apply остановлен на CEL warning существующей
  `kodex-image-admission-controller-workspaces`, generation/observedGeneration 2.
  `spec.validations[4].expression` обращается к полю `resources.requests`,
  которое type checker не распознал для PersistentVolumeClaim.
  Проверка отсутствия warnings не обходилась.
- Уточнение по свежему readback 05.10.2026 12:58 UTC: migration
  `20261005000200_image_admission_risk_decisions.sql` уже применена,
  `goose_db_version.is_applied=true`. Job `control-plane-migrate-85012127fec2`
  завершился при предыдущем apply в 09:25 UTC. Прежняя трактовка отказа как
  preflight «до любых эффектов» была неверной: отказ случился после частичного
  apply, на проверке компиляции политики. Serving CP/gateway и контроллеры
  на тот момент не возобновлены; полная активация и её readback тогда NOT RUN.
- NOT RUN: живое принятие риска, повторный admission/promotion и полный dogfooding.
- Открытое замечание безопасности ещё не закрыто; подтверждение устранения NOT RUN.
- Общее историческое форматирование журнала имеет отдельный FAIL;
  это не ошибка сборки приложения и не объявлено PASS.

  05.10.2026 13:52 UTC — предыдущие FAIL сохранены выше как история. На clean
  source `b40f278477cf977e058a90c8bcd163550e35fa4e` исправлены Quantity/PVC,
  RBAC union, init/main list и обработка optional пустого typeChecking.
  Typed CEL, восемь compiler gate tests, deploy selection и выполненные cutover
  tests PASS; один optional disposable PostgreSQL test NOT RUN.
  Canonical all/build-jobs4/import, свежий render и supply-chain apply/readback PASS.
  Все девять VAP имеют свежие generation/observedGeneration и ноль warnings.
  Все пять Deployment и session-archive имеют desired/ready/updated/available=1;
  archive восстановился без ручного restart. Source mounts и адресные host/Pod
  hashes сверены, фактические Go executables проверены отдельно от Air launcher.
  У hot binaries нет vcs.revision: exact source SHA не выводится из annotation.
  Chrome: SSO, bootstrap/session200, подключённый realtime, Console без ошибок;
  скриншот переписки просмотрен. Новый SYSTEM turn отправлен штатным UI.
  Полная native самонастройка, решение о риске и dogfooding ещё не завершены.

Maintenance503 устранён штатной активацией после успешной компиляции политик.
Готовность Deployment и вход не объявляются завершением пользовательских
сценариев: последующие этапы выполняются через помощников и штатный UI.

## Последний checkpoint активации: 16:24–16:40 UTC

- PASS: restore на прежнем ConfigMap UID
  `cc73eb9b-f263-496a-b383-05070e5f845f`, RV456244;
  dataSHA256 `dc297e676f88545be3e5e5091113a72c97f6c06d7f45e087a0c70824bb334457`;
  canonical scriptSHA256 `3d61890702c0157e944823a7282bb662865fdd7c333de05daf84840138db5e55`.
- PASS: fresh render source `6efc5104`, suffix `wn8P5b`, fingerprint
  `b16b308610835de8977b54d183d310ebb7cdcbcab941e2d212296059383b1ce6`;
  обычный supply-chain apply/readback полностью завершён16:30:31 UTC.
- PASS: все пять Deployment desired/ready/updated/available=1. Worker image
  digest префиксы: builder `120c7`, admission `9907`, tools `137c9`, authority
  `710a22`; это сокращённые отпечатки, не замена exact release pins.
- PASS: host/CP mounted `client.go` SHA256
  `14e74b94ee2c7518281fa39bb31da1d7fb7405b822dbb0cef80a08d0f6ab15de`
  совпадает16:32 UTC. Runtime source annotation CP соответствует `6efc5104`;
  это не доказательство SHA работающего binary.
- PASS: owner READ16:31 UTC до нового build — pendingAdmissions0,
  promotedArtifactCount19, published pins префикса `28e8bf55` неизменны.
- PASS: native SYSTEM39, один подтверждённый UI план обновления recipe и build
  COMPLETED/version12/100%; не полный admission или dogfooding acceptance.
- NOT RUN: новый полный report/admission/risk/promotion, runtime tool/prompt
  proof и остальные пользовательские сценарии; checklist2–15/6.1 открыт.

# Оставшиеся действия

- [x] Сверить GitHub HEAD, рабочую ветку, сохранённые данные и фактическое
      состояние Deployment/работ/образов. Старое evidence не объявлять свежим.
- [x] Исправить CEL-типизацию workspace/runtime policy через код и адресные
      negative tests, не ослабляя PVC/RBAC/контейнерные ограничения.
      Живая компиляция и активация остаются отдельным следующим пунктом.
- [x] Получить fresh source/render; штатным repo-owned apply выполнить forward
      migration и обновление CRD/policy/network/CP/gateway. После полного readback
      штатно возобновить контроллеры. Не применять старый render вслепую.
- [ ] Проверить Chrome: скриншот, вёрстку/UX, Console, relevant Network,
      WebSocket и логи backend. Чужие вкладки не закрывать.
- [ ] Для следующего исправленного image повторить полный report, точное
      решение ADMIN/OWNER при необходимости, подписанный admission и promotion.
      Предыдущий build `imgbld_391ktSUxZEzhsxVdJjm97i0r` уже прошёл этот путь;
      остаётся npm PROBE_FAILED, исправление и новый OCI пока не активированы.
- [ ] Через SYSTEM выполнить самонастройку, Context7/web/repository/prompt proof,
      создание проекта `Kodex | Dev` и отдельного Project Assistant.
- [ ] Через PROJECT создать шесть ролей, окружения, grants, файлы и SOFTWARE_CHANGE;
      проверить реальные ходы каждой роли, делегирование и оба Human Gate режима.
- [ ] После bootstrap acceptance слить только bootstrap PR, обновиться на main,
      повторно проверить deployment и созданные ресурсы.
- [ ] Силами команды Kodex выполнить реальную Issue #1796 либо следующую подходящую,
      внутренние reviews/fixes/re-review и итоговый отчёт.
- [ ] Передать финальный dogfooding PR в `READY_FOR_HUMAN_REVIEW` владельцу;
      не выполнять merge/auto-merge/owner approve этого PR.

Пункты 2–15 и 6.1 основного checklist остаются открытыми: наличие кода и
адресных тестов не заменяет предусмотренное живое доказательство.

Изолированные экспериментальные worktrees не являются новым источником
принятого кода. Их не удалять при обслуживании до сверки с историей ветки;
промежуточные варианты не применять поверх текущего checkpoint автоматически.

05.10.2026 17:12 UTC: поверх pushed `d29e4f63` интегрированы адресные frontend
исправления каталога и восстановленного имени образа, а также non-root npm fix.
Native40 UI теперь показывает собственный образ и 41 VERIFIED программу без
ошибки каталога. В17:26 UTC окружение revision22 опубликовано, binding version2
совпадает с `renvv_quVjHbEqDeaw63wj1HTjyc_U`; draft PUBLISHED/version3.
Новые runtime receipt
и corrected image проверять только после canonical rebuild; прежний immutable
gen7 не содержит будущего protected binary. Полный checklist остаётся открыт.
