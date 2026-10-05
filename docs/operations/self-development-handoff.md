---
id: OPS-DOC-SELFDEV-002
title: Точка продолжения самонастройки и dogfooding Kodex
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-05
---

# Текущее состояние

05.10.2026 владелец явно возобновил работу после переноса хранилища.
Перенос проверен; цель снова выполняется. CEL-политики исправлены, штатный
apply/readback завершён на source `b40f278477cf977e058a90c8bcd163550e35fa4e`.
Пять компонентов и архив сессий восстановлены; выполняется native QA.
Текущий запушенный checkpoint кода: `b5fe1bec30e2bc4ef09f207e18803a6195c374e3`.
Поверх него устраняется узкий SSA ownership conflict диагностического script;
точный следующий SHA и live readback фиксируются в журнале.
Ниже перечисленные старые checkpoints относятся к истории, а не к текущему HEAD.

- Issue: [#1797](https://github.com/codex-k8s/kodex/issues/1797).
- Draft PR: [#1798](https://github.com/codex-k8s/kodex/pull/1798), не слит.
- Ветка: `kodex-agent/issue-1797-self-development-bootstrap`.
- Последняя фиксация кода и технических документов: `99f397c56a7dd97831011d70be73e82eb8999afd`.
- Checkpoint завершения сборки и паузы: `1cde8237c535e19bc0cdcd742c64ce6a8f6251b3`.
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

Native SYSTEM typed plan применён; recipe v8/generation6, build завершён.
Admission scan завершился технической ошибкой прежнего projection handler;
artifact штатно завершён FAILED/ADMISSION_LEASE_EXPIRED; claim/lease очищены,
attempt terminal snapshot совпадает, один owner expiry receipt подтверждён.
Подготовленные новые worker images собраны и импортированы на обе ноды
на sourceb5fe1bec, но полная активация пока не завершена: ordinary SSA
отказал на ownership одного диагностического script. Admission controller
оставлен остановленным; CP/gateway/frontend доступны. Готовится exact CAS
возврат canonical script с правильным field manager без force-conflicts,
после чего штатный supply-chain apply/readback повторяется.

Повторный ROLE_ENVIRONMENTS отказ устранён bounded whole-transaction retry
только для PostgreSQL40001: новая транзакция повторно проверяет authority,
lease и fence. Адресные unit и SYSTEM/PROJECT PostgreSQL component PASS.
В native QA_CATALOG_RENEW_FIX_02 пять реальных tool events успешны, в том
числе три последовательных ROLE_ENVIRONMENTS; скриншот и Console проверены.
Устаревшие ожидания component fixture обновлены до утверждённого graph
100nodes/253edges без изменения production guard или applied migration.

Admission recovery остаётся открытым: exact native Pod DNS и TCP к CP
проверены; actual client trusted profile и отсутствие proxy подтверждены.
Serving CP использует plaintext gRPC, поэтому TLS mismatch исключён.
Три native callback подтвердили REFUSED через закрытый потоковый classifier.
Двухсекундная dev-диагностика дала штатный terminal receipt и очистку workspace;
источник временного TCP отказа окончательно не доказан. Production bounded
WaitForReady только Fail/Expire уже реализован и проверен disposable TCP,
без повтора полученной server error; новый worker binary ещё не активирован.
Секреты, claim, grant, NetworkPolicy и SQL-состояние не изменялись.

Далее: штатный terminal callback → activation проверенных worker images →
fresh native build/полный report/owner risk decision/подписанный admission/
promotion → typed SYSTEM environment → новый turn и tool/prompt proof →
SYSTEM создаёт проект и PROJECT → шесть ролей, grants и реальный Workflow.
Основной checklist2–15/6.1 остаётся открытым до фактических доказательств.

# Последние фактические проверки

- PASS: на source `99f397c5` каноническая supply-chain сборка четырёх компонентов
  с `build-jobs=4` и импортом exact digests на обе ноды.
- PASS: свежий render того же source. Это не serving/live acceptance.
- PASS: maintenance barrier ранее подтверждён; пять Deployment оставлены
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
  не возобновлены; полная активация и её readback ещё NOT RUN.
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
- [ ] Штатно собрать новый native образ для evidence v5; проверить отчёт,
      решение ADMIN/OWNER, повторный admission и promotion. Checkbox 6.1 пока открыт.
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
