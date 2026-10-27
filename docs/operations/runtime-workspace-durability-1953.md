---
id: OPS-DOC-RUNTIME-WORKSPACE-1953
title: Устойчивое рабочее состояние сотрудника
type: operation-contract
status: approved
owner: developer
version: 1.0.0
updated: 2026-10-27
---

# Граница профиля

[#1953](https://github.com/codex-k8s/kodex/issues/1953) задаёт сохранение
неопубликованного рабочего дерева при остановке сотрудника. История модели,
устойчивый work и принятые CLEAN-артефакты имеют разные контракты. Сохранение
work не принимает бизнес-результат и не выдаёт новых полномочий.

Выбранный профиль `SESSION_WORKSPACE_V2` повторно использует существующий
session PVC20GiB. Только exact writable generation монтируется как
`/workspace/work`; private parent, другая generation и другие сессии не доступны
через альтернативный путь `.kodex/state`. Кэши и временные результаты сборки
отделены от сохраняемого дерева. Прежние квоты work, количества файлов,
памяти, выполнения и archive tasks не увеличиваются.

Этот документ фиксирует согласованный контракт. Наличие типов, миграции или
локального теста само по себе не подтверждает доставку либо полную готовность.

## Authority и жизненный цикл

| Переход | Авторитетное действие |
| --- | --- |
| Create | CP создаёт пустую work binding exact organization/project/session/agent. Совпадение имён либо latest-by-agent не выбирает прежние файлы |
| Claim/renew | Один current writer через owner CAS и точные execution pins. RWO и наличие Pod не являются single-writer proof |
| Materialize | Только owner-selected generation и exact PVC UID; role видит только свой tree, не корень хранилища |
| Complete | Сначала process Wait, reader drain и отсутствие process group; затем immutable checkpoint и отдельный owner ACK. Business receipt не переписывается |
| Cancel/timeout | Немедленное закрытие execution grants; work остаётся. Graceful Pod deletion сохраняет положительный owned quiescence proof до исчезновения Pod |
| Неизвестный writer | BLOCKED: нет archive/delete/rejoin по одному lease expiry, force-delete либо PodNotFound |
| Technical continuation | Exact server-owned CONTINUES, свежий lease и inputs; без повторения внешнего эффекта |
| Child retry | Actual retry lineage и fresh actor/project/agent eligibility. Новый самостоятельный root не наследует полномочия старого родителя |
| Workflow retry/rework | Exact predecessor/version/step/agent и approved route; новые Sessions получают копию собственного frozen checkpoint, не общий writable tree |
| Replay/late ACK | Прежний immutable owner outcome. Старый writer не меняет current generation и не становится вновь разрешённым |
| Archive/restore | Проверка всех exact-version parts и digest до owner ACK; model не запускается по частично восстановленному дереву |
| Delete/purge | Durable destructive intent исключает новый writer; exact UID/versions, отсутствие consumers и authoritative readback. Unknown блокирует удаление |

## Архив и ограничение ресурсов

Legacy archive v1 сохраняет прежние bytes и reader. Закрытый v2 work manifest
содержит только scope, generation, writer commitments, digest/размер дерева и
ограниченный список immutable data parts. Secret lease fence, provider auth,
input/context и чужие файлы в архив не входят. Отсутствующий Codex session ID
не выдумывается для work, появившегося до запуска модели.

Canonical stream ограничен прежними work byte/file quotas. Частей не больше32,
каждая не больше64MiB, manifest/task не больше64KiB. Путь и overhead одной записи
ограничены; превышение либо неподдержанное дерево закрыто отклоняется с
сохранением PVC. Decode/restore отклоняет неизвестные и повторные поля, лишние
bytes, traversal, links, special files и несовпадение exact object commitments.
Восстановленный work остаётся недоверенным внутри прежнего sandbox и не
обходит quarantine или artifact/VFS eligibility.

V1 receipt при наличии work не разрешает удаление всей PVC. Перед activation
нужно завершить либо отозвать старые destructive archive jobs и подтвердить
их exact UID readback. После v2 записей восстановление требует совместимого
forward reader; удаление metadata ради старого бинарного rollback запрещено.

Backup фиксирует точное поколение подтверждённого checkpoint со всеми parts.
Активное неподтверждённое дерево не выдаётся за полный backup исходников.
Restore не возвращает старые leases, credentials или RuntimeRevision authority.
Старый work на уже удалённом emptyDir без receipt остаётся отсутствующим либо
неизвестным, а rollout не переименовывается в восстановленный исходник.

Forward migration `20261026001150` располагается после01100 и до01200;
применённые миграции не меняются. Catalog/purge proof и rollout относятся к
точному отдельному checkpoint, без allow-missing и без обхода остановленных
native-путей других задач. Проверки operator, native, backup/restore и реальная
приёмка сотрудником обязательны до заявления готовности.
