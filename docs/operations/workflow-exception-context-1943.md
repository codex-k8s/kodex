---
id: OPS-WORKFLOW-EXCEPTION-CONTEXT-1943
title: Контекст менеджера серверного workflow
type: operation
status: approved
owner: architect
version: 1.0.0
updated: 2026-10-27
---

# Контекст менеджера серверного workflow

Issue [#1943](https://github.com/codex-k8s/kodex/issues/1943), эпик #1836.
Manager exception разбирает уже возникшее отклонение. Он получает данные,
а не право принять результат, изменить DAG, одобрить повтор или совершить WRITE.
Возврат к более раннему автору и повторные рецензии этим unit не реализуются.

## Сквозной контракт

| Переход | Авторитетный источник и результат |
| --- | --- |
| Result Gate → REQUEST_CHANGES | Existing owner command сначала сохраняет decision/version/comment в exact Gate. Episode указывает logical origin, final physical node, business attempt и immutable predecessor inputs. |
| PENDING_MANAGER → задача | Owner SELECT связывает org/root/project/pinned SERVER version и текущий last episode/version с exact final node. Source decision берётся только из единственного WAITING_FOR → HUMAN_GATE с этим parent и CHANGES_REQUESTED/REQUEST_CHANGES, без native integration invocation. Payload comment и соседний Gate не подходят. |
| Задача → manager | Закрытая поясняющая форма `kodex.workflow-exception-context.v1` содержит exact сохранённый comment, причину и public producer/predecessor metadata. Отсутствие owner decision для RESULT_INVALID либо materialization failure остаётся отсутствием, не UNKNOWN или придуманным Gate. |
| Manager claim → frozen RR | Прежний claim назначает capabilities и полный RR/lease/generation. Новый INVOKER helper принимает только текущий managerNodeId/ref последнего MANAGER_QUEUED episode, root actor, published coordinator и точный source step/attempt. BusinessAttempt0 не получает workflow_inputs, capture или accepted journal. |
| RR → каталог | Existing SERVER catalog использует старую assigned-step ветку UNION exact exception pins. Source closed capture может быть ещё не принят: это диагностические данные. Dependency ветка принимает лишь exact accepted receipt, принадлежащий published DependsOn source шага и сохранённым episode inputs. Не открываются другие outputs того же root/проекта. |
| Каталог → чтение | Existing lease/catalog/ref/digest/generation fences, canonical project/agent/run/artifact view+download, immutable content и exact effective CLEAN остаются обязательны. Дополнительный coordinator fence в read_catalog пропускает только exact active exception-manager helper. |
| Complete/cancel/expiry/revoke | Existing owner envelope закрывает прежний lease/manager/root. Context helper перестаёт выдавать membership после завершения manager либо смены episode. Задача, сохранённые решения и файл остаются историческими данными; они не возобновляют execution. |

Комментарий не усекается. Задача проверяется прежним
`ValidAssistantTurnContent`: максимум 32768 Unicode codepoints, корректный UTF-8,
без NUL. Producer summary ограничена 1500 codepoints; содержимое файлов в задачу
не копируется. Переполнение возвращает closed configuration failure в existing
savepoint/gate path, а SQL fault не маскируется под отсутствие конфигурации.

## Свежесть и историческая ревизия

`runtime_file_visible_entries` связывает точные frozen ref/revision/digest/size/type
с `artifact_history` и immutable content. После AV migration20261016000100
`artifact_history.scan_state` уже вычисляется через
`artifact_effective_scan(revision.id)`. Другая CLEAN revision не разрешает
QUARANTINED entry. Поэтому view не заменяется и не ослабляется.

Новая owner-approved revision того же artifact не становится source capture.
Existing old CLEAN revision читается только по прежнему exact pin и текущим ACL.
Иной artifact, revision, project, manager node или episode не наследует право чтения.
Runtime completion не принимает заражённый scan, а AV receipt immutable:
штатного перехода уже frozen CLEAN receipt в QUARANTINED через пользовательский
rescan API нет. Проверки не имитируют его как обычное действие владельца.

## Миграции, поставка и восстановление

Принятый порядок: 011 legacy rework → **012 workflow exception context** →
013 upstream rework → SSH270001 → Application270002. Этот профиль012 не принимает013.
Новые FK/columns и purge edges не добавляются; сохраняется текущий011 graph105/286.
012 читает Goose ledger как migrator до SET ROLE owner, требует exact011, проверяет
прежний body hash и добавляет два SQL STABLE SECURITY INVOKER helpers. PUBLIC EXECUTE
отозван в той же транзакции. UNION replacement сохраняет OID/ACL и обычный reader.

Отдельный immutable release profile `workflow-exception-context` имеет свой
011 precondition и 012 postcondition SQL, exact source/metadata/ACL helpers и все
унаследованные schema/purge/retention/authority checks. Старые010/011 validators не
принимают012. В штатной disposable оснастке current012 выбирает новый verifier;
старые schema regression проверяют свои инварианты через текущий012 snapshot.

Перед DDL необходим existing closed maintenance: активная работа должна быть
завершена/отменена каноническими командами, затем check/stop/apply/inspect/resume
с immutable Job/source/image/SQL pins. Не обходить active counters ради обновления.
После штатного resume старого CP доставить новый immutable CP всем репликам и
подтвердить точный image/source/readiness до новых manager turns. Legacy задачи и
уже созданные manager task/RR не переписываются; исправленный контекст получает
свежий manager. Общий RC/API/PWA/runner file-catalog wire не меняется, новых прав нет.
DDL forward-only; после012 восстанавливать совместимый CP, не повторять DDL и не
объявлять старый metadata-only task исправленным после одного rollout.

## Поддерживаемая проверка

`make test-workflow-exception-context-postgres` — штатный disposable PostgreSQL
entrypoint, 300s плюс bounded cleanup. Сценарии создают настоящие два шага,
source/dependency files, exact result Gate/REQUEST_CHANGES и manager claim.
Manifest, metadata, preview и bytes читаются через реальные runtime-controller
principals. Есть owner replay, RESULT_INVALID без выдуманного решения,
current ACL revoke, cancel/completion, actual lease expiry/reclaim и отдельная
owner-approved revision через project assistant proposal/validate/apply.
SQL не создаёт готовые Gate, grant, capture, episode или accepted journal.

012 schema negatives меняют только DDL отдельной clone: PUBLIC EXECUTE,
SECURITY DEFINER, search_path, function body, лишний overload и утрата UNION.
Они исполняют тот же поставляемый postcondition SQL. Old SERVER/legacy schema
negatives и recovery/retry snapshot suites включены в этот bounded target.

Local unit/compile/vet, native и live результаты публикуются отдельно на exact SHA.
Наличие исходников оснастки не означает её PASS. Live ручная проверка после
разрешённой поставки: REQUEST_CHANGES с конкретным комментарием; свежий manager
читает точные результаты и перечисляет уже сообщённые замечания без просьбы
повторить текст. Отдельно подтвердить revoke/cancel и отсутствие расширения прав.
