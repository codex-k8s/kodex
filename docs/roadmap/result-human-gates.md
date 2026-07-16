---
id: ROAD-MC-003
title: Human gates по типам результата
type: process
status: proposed
owner: manager
version: 0.1.0
updated: 2026-07-16
---

# Human gates по типам результата

## Принцип

Human gate ставится на законченный и проверяемый тип результата, а не на каждую внутреннюю задачу агента.

Примеры одного result gate:

- service boundaries и структура каталогов;
- OpenAPI/gRPC/AsyncAPI contracts;
- domain models/repositories/services/handlers;
- UI flow и набор экранов;
- integration capability/approval contract;
- deployment topology и runbook;
- backup/restore design;
- готовый feature slice с E2E.

## Обязательный цикл

```text
Manager defines result
  -> Worker produces version N
  -> Reviewer pass 1
  -> Worker fixes
  -> Reviewer pass 2
  -> Worker fixes
  -> optional Reviewer pass 3
  -> Manager readiness check
  -> Owner gate 1
      -> comments: Worker fixes -> Reviewer full pass -> Owner gate again
      -> preliminary OK: Reviewer final pass -> Owner final OK
  -> Reviewer merges/publishes
  -> Reviewer launches Improver
  -> Improver updates instructions/guides via separate reviewed result
```

Owner может явно сократить число review passes для низкорискового результата. По умолчанию manager планирует 2-3 прохода до первого gate.

## Responsibilities

### Manager

- определяет один result и acceptance criteria;
- выбирает workers/reviewers и distinct review purposes;
- не передает сырой или непроверенный результат owner;
- отслеживает child callbacks и blockers;
- не считает silence approval;
- после завершения запускает следующие независимые waves.

### Worker

- создает result и evidence;
- отвечает на каждое actionable замечание;
- не закрывает review thread без фактического исправления/обоснования;
- после owner feedback не меняет scope скрыто.

### Reviewer

- проверяет result против docs/criteria, а не вкусовых предпочтений;
- разделяет blocking и non-blocking findings;
- повторно проверяет весь затронутый contract после fix;
- merge выполняет только после final owner OK;
- после merge запускает improver через MCP.

### Owner

- принимает законченный result version;
- оставляет замечания в одном gate cycle;
- явно пишет OK/accept;
- может остановить/изменить scope.

### Improver

- собирает feedback цикла;
- ищет systemic cause;
- предлагает изменения instructions/guides/playbooks;
- не переписывает product decision без отдельного gate.

## Версионирование gate

Gate связан с result version/hash/commit/PR SHA. Существенное изменение после OK инвалидирует gate. Reviewer фиксирует final evidence и exact version перед merge.

## Параллельность

Manager может запускать несколько waves после принятия общего dependency result. Например, после принятия service template и contract rules разные domain services разрабатываются параллельно в отдельных threads.

Нельзя параллельно реализовывать несколько consumers нестабильного contract до его gate, если это приведет к массовой переделке.

## GitHub lifecycle для кодового результата

1. Issue фиксирует цель/result/criteria/dependencies.
2. Worker открывает draft PR.
3. Review cycles выполняются inline и summary comments.
4. Manager публикует owner-ready summary со ссылками/evidence.
5. Owner comments отрабатываются в том же PR, если scope не изменился принципиально.
6. Reviewer подтверждает resolved threads/checks/exact SHA.
7. Owner дает final OK.
8. Reviewer выполняет merge.
9. Reviewer через MCP запускает improver с ссылками на Issue/PR/reviews и периодом.
