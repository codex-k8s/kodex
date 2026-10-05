---
id: OPS-DOC-SELFDEV-002
title: Точка продолжения самонастройки и dogfooding Kodex
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-05
---

# Состояние на паузе

Работа приостановлена владельцем. Этот документ не разрешает её возобновление.
Кластер, агентские запуски, сборки и QA не запускать до явного «продолжай».

- Issue: [#1797](https://github.com/codex-k8s/kodex/issues/1797).
- Draft PR: [#1798](https://github.com/codex-k8s/kodex/pull/1798), не слит.
- Ветка: `kodex-agent/issue-1797-self-development-bootstrap`.
- Последняя фиксация кода и технических документов: `99f397c56a7dd97831011d70be73e82eb8999afd`.
- Checkpoint завершения сборки и паузы: `1cde8237c535e19bc0cdcd742c64ce6a8f6251b3`.
- Последующие изменения этой фиксации касаются только документации.
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

# Последние фактические проверки

- PASS: на source `99f397c5` каноническая supply-chain сборка четырёх компонентов
  с `build-jobs=4` и импортом exact digests на обе ноды.
- PASS: свежий render того же source. Это не serving/live acceptance.
- PASS: maintenance barrier ранее подтверждён; пять Deployment оставлены
  с `spec/status replicas=0`, активных работ нет, promoted pins сохранены.
- FAIL: apply остановлен в preflight на CEL warning существующей
  `kodex-image-admission-controller-workspaces`, generation/observedGeneration 2.
  `spec.validations[4].expression` обращается к полю `resources.requests`,
  которое type checker не распознал для PersistentVolumeClaim.
  Проверка отсутствия warnings не обходилась.
- NOT RUN: новая migration `20261005000200_image_admission_risk_decisions.sql`,
  serving policy 89 и новые CRD/network/CP/gateway; контроллеры не возобновлены.
- NOT RUN: живое принятие риска, повторный admission/promotion и полный dogfooding.
- Открытое замечание безопасности ещё не закрыто; подтверждение устранения NOT RUN.
- Общее историческое форматирование журнала имеет отдельный FAIL;
  это не ошибка сборки приложения и не объявлено PASS.

Сейчас остановлены control-plane, control-api-gateway, image-admission,
role-image-builder и runtime-controller. API возвращает `503`; это ожидаемое
следствие maintenance, а не доказательство работоспособности нового кода.
Все дочерние агенты и собственные build/render/apply процессы завершены.

# Что делать после возобновления

- [ ] Сверить GitHub HEAD, рабочую ветку, сохранённые данные и фактическое
      состояние Deployment/работ/образов. Старое evidence не объявлять свежим.
- [ ] Исправить CEL-типизацию workspace policy через код и адресные negative tests,
      не ослабляя PVC constraints и gate отсутствия compiler warnings.
- [ ] Получить fresh source/render; штатным repo-owned apply выполнить forward
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
