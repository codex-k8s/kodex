---
id: OPS-SERVER-ROOT-DEADLINE-1935
title: Истечение часов серверного workflow
type: operation
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-27
---

# Часы серверного workflow

Issue [#1935](https://github.com/codex-k8s/kodex/issues/1935). Доставка и native
GREEN подтверждаются отдельно на точном SHA; этот документ не объявляет их выполненными.

Источником root clock служит опубликованный immutable workflow version.
SERVER_DAG_V1 root не исполняет coordinator initial: его anchor после assignment
может находиться в child run, а оставшийся anchor ещё не имеет turn/RR.
Поэтому root-self clock связан с существующим ROOT_PROCESS того же org/run/root,
с parent NULL и exact workflow/project/version. Для обычного прежнего root и
для физических child clocks сохраняется выбор AGENT_EXECUTION. Состояние owner
Gate не приостанавливает и не сбрасывает эти часы.

| Переход | Авторитетное поведение |
| --- | --- |
| Первый claim | Root/step clocks стартуют однократно; queued ожидание до первого claim не сокращает срок. |
| Root overdue, есть PLANNED successors | Runtime claim tick закрывает root FAILED/RUNTIME_TIMEOUT; ROOT_PROCESS FAILED, неисполняемые anchors CANCELLED с прежними materialization/turn/RR. |
| Все anchors уже в child runs | Отсутствие AGENT_EXECUTION в root не скрывает root clock; тот же root процесс остаётся владельцем timeout. |
| Ранний physical step clock | Terminal attribution остаётся у настоящего AGENT_EXECUTION этого child; root закрывается тем же полным envelope. |
| Поздний owner APPROVE | Fresh authority и replay предшествуют проверке; истёкший execution закрывается атомарно, команда возвращает Forbidden, новая attempt не создаётся. |
| Execution раньше Gate expiry | Сохраняется RUNTIME_TIMEOUT; Gate CANCELLED, а не ложный owner REJECT или OWNER_GATE_EXPIRED. |
| Gate раньше execution | Прежний OWNER_GATE_EXPIRED и server decision NULL сохраняются. |
| Recovery manager завершён | Root timeout закрывает pending recovery episode как CANCELLED, сохраняя reason/provenance/capture и завершённый manager. |
| Cancel / owner REJECT / другой terminal | PLANNED anchor не получает fabricated execution; прежние штатные terminal пути и immutable история сохраняются. |
| WRITE уже начат | Прежний terminal SQL сохраняет uncertain external WRITE как UNKNOWN_OUTCOME; timeout не доказывает отсутствие внешнего эффекта и не разрешает повтор. |
| Terminal replay / повторный tick | Нет новых событий, lease или продолжения; clocks и сохранённые артефакты не меняются. |
| Следующий независимый root | Тот же native claim после closure выдаёт только свежий назначенный execution; отменять просроченный root вручную не нужно. |
| Legacy / system assistant | SERVER root-specific predicate не расширяет их eligibility и не меняет clock policy. |

Карта: runtime-controller poll → RuntimeWorkService.ClaimExecution → owner scope
и ClaimExecution transaction → reconcileExecutionDeadlines/expired SQL →
failRuntimeGraph. Для owner Gate используются executionDeadlineCommand/owner SQL
и earlierGateExecutionDeadline/earlier SQL. Actor и org выводятся из доверенного
transport/owner scope; rootRef payload сам по себе не выдаёт authority.
Строки блокируются прежним owner protocol, immutable clocks и SQL CHECK не меняются.
Закрытие состояния, audit/events и terminal deliveries фиксируются одной owner
транзакцией. Ошибка SQL не превращается в успешный skip.

Новая native оснастка TestServerWorkflowRootDeadlineComponent создаёт workflow
через Create/Validate/Publish/Launch/Claim/Complete, использует настоящий clock,
читает proof только SELECT и не готовит business states SQL. Она охватывает
recovery с PLANNED successor, root без физических AGENT nodes, позднее owner
решение и ранний execution против Gate expiry. Каждый сценарий проверяет
последующий независимый claim, сохранение custody/часов и отсутствие replay effects.

Публичная bounded точка проверки (disposable PostgreSQL слушает только loopback):

```bash
GOMAXPROCS=2 KUBECONFIG=/dev/null KODEX_TEST_POSTGRES_NETWORK=host \
  timeout --kill-after=10s 240s bash scripts/tests/control-plane-postgres-test.sh \
  '^TestServerWorkflowRootDeadlineComponent$'
```

Сопутствующие legacy deadline и system-assistant terminal-storage сценарии
выполняются через ту же штатную точку с явным фильтром. Их результаты не
подменяют новую SERVER матрицу. При FAIL сохраняются точные SHA, причина и
cleanup; автоматического повтора с другим названием или новым key нет.

Для приложения исправления нужна обычная immutable доставка CP image; новых
миграций, ослабления constraints, ручного изменения deadline или инфраструктуры
нет. До application rollout текущий просроченный root не исправляется запросом
GET. После rollout его закрывает штатный следующий claim tick. История и файлы
проверяются через owner readback. Rollback сохраняет schema010, но старый binary
может снова заблокировать closure другого SERVER root с planned successors;
поэтому исправление доставляется вперёд, без переписывания данных.
