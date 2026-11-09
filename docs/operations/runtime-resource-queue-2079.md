---
id: OPS-RUNTIME-QUEUE-2079
title: Ресурсный допуск runtime и очередь запуска
type: runbook
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-09
---

# Ресурсный допуск runtime и очередь запуска

Issue: [#2079](https://github.com/codex-k8s/kodex/issues/2079). Связанные приёмки:
[#1830](https://github.com/codex-k8s/kodex/issues/1830),
[#1977](https://github.com/codex-k8s/kodex/issues/1977).

## Владелец и контракт

PostgreSQL control-plane остаётся владельцем очереди и выполнения. Это дополнение
существующего `ClaimExecution`, а не отдельная очередь в памяти контроллера.
Один лидер runtime-controller последовательно наблюдает ресурсы и публикует Pod;
параллельно работают уже запущенные trackers. Kubernetes остаётся окончательным
планировщиком. Наблюдение не резервирует ресурсы у других контроллеров и не
подменяет Kubernetes admission, affinity, storage topology либо quotas.

`RUNTIME_CONTROLLER_RESOURCE_AWARE_QUEUE=true` включён в стандартной конфигурации.
`false` возвращает прежний claim без ресурсного снимка. Старый клиент без нового
поля также сохраняет прежнее поведение. Нельзя включать новый controller перед
обновлением control-plane: старый Proto server игнорирует неизвестное поле.

Контроллеру нужен только дополнительный cluster-wide `list` для `nodes` и `pods`:
`runtime-controller-capacity-observer`. Доступ к Secrets не расширяется. Агенты
этот ClusterRole не получают. Причина ошибок Kubernetes не разрешает legacy
fallback при включённом ресурсном допуске.

Снимок ограничен 512 узлами, 32 страницами по 500 Pod, 15 секундами наблюдения и
20 секундами возраста на входе владельца. Имена узлов, Pod spec, env и credentials
не передаются в RPC: только числовые свободные/полные CPU millicores, байты памяти,
ephemeral storage и слоты Pod. Отрицательные/неограниченные значения запрещены.
Пустой список узлов означает отсутствие ёмкости, `nil` — совместимость старого
протокола. В ресурсном режиме один RPC допускает максимум одно выполнение и отмечает
до 16 ожидающих кандидатов. Ещё не отмеченные причины обходятся раньше уже
показанных: поздняя задача не остаётся ложно RUNNING за старой очередью.

Учёт использует `k8s.io/component-helpers/resource` той же версии, что client-go:
requests работающих Pod, последовательные и restartable init-контейнеры,
Pod-level requests и overhead. Назначенные незавершённые Pod учитываются во всех
пространствах, ещё не назначенные собственные runtime Pod — консервативно
раскладываются по пригодным узлам. Чужие Pending Pod не являются allocation;
их конкурентное размещение остаётся решением Kubernetes. Cordon, NotReady,
NoSchedule/NoExecute исключают узел. Служебные requests нового runtime выводятся
из того же `smallResources`, что Pod template. Ёмкость разных узлов не суммируется
для одного Pod. Instantaneous usage не подменяет requests.

`requests` и `limits` уже отдельно задаются политикой runtime environment. Эта
доработка не уменьшает их глобально и не переписывает пользовательские настройки.
Размеры выбираются по замерам активных turns и характеру инструментов. Idle
потребление не доказывает пиковый расход. Ограничение числа trackers и лимит
аккаунта модели продолжают действовать независимо от ресурсного допуска.

## Сквозной путь и жизненный цикл

| Сценарий | Actor и authority | Переход владельца | Результат и потребитель |
| --- | --- | --- | --- |
| Launch | Проверенная пользовательская identity, target permission и root initiator; обычный HTTP launch через gateway | Существующий Run/session и QUEUED execution node | Пользовательский запрос не назначает ресурсы узла, workload либо credentials |
| Опрос | Machine principal runtime-controller, штатные mTLS/JWS и `ClaimExecution` | Свежий ограниченный снимок проверяется до выбора | Отказ наблюдения не вызывает claim; очередь сохраняется в БД |
| Нет свободного места | Тот же защищённый RPC, resource policy принадлежит выбранной версии окружения | Узел остаётся QUEUED, без RR/lease/credentials. Root без активных execution nodes становится QUEUED; ещё не начавшийся root не получает `startedAt` | `NODE_STATE_CHANGED`, server-owned `queueReason=WAITING_RESOURCES`, GET/list/realtime |
| Запрос не помещается ни на один узел | Те же owner checks и полная ёмкость пригодных узлов | Без RR/lease; `REQUEST_EXCEEDS_CAPACITY` | Пользователь меняет окружение/ёмкость либо отменяет; подходящие независимые задачи проходят мимо такого кандидата |
| Освобождение места | Новый снимок, прежний node и root initiator; заново обычные session/grant/account/credential checks | Свежая RR и fenced lease, RUNNING, очистка queueReason, исходный turn | Обычная защищённая materialization и Pod; не retry и не новый внешний эффект |
| Повтор того же ожидания | Текущие owner state и версия | При неизменной причине не создаются новые события/receipts | Нет потока одинаковых событий на каждом poll |
| Cancel ожидающего | Обычные user permission/OCC/idempotency | Существующий cancel закрывает граф, QUEUED node больше не выбирается | GET/stream показывают CANCELLED, queueReason в terminal отсутствует |
| Restart/смена лидера | Kubernetes election и прежние execution fences | Ничего не переносится из локальной памяти как новая authority; новый лидер читает БД и Kubernetes | Ожидающая работа не теряется и не получает credentials до нового допуска |
| Deadline/отзыв до claim | Существующие owner deadline и grant eligibility checks | Штатное закрытие либо отказ eligibility, без обхода по ресурсному полю | Бюджет ожидания не продлевает общий deadline и не даёт fallback на чужую credential |
| Pod после допуска не подготовился | Исходный срок descriptor credentials; renew lease его не продлевает | Ограниченная остановка и fenced failure `RUNTIME_UNAVAILABLE`, summary `RUNTIME_STARTUP_TIMEOUT` | Без автоматического requeue/retry или обещания отменить уже начатый внешний эффект |
| Pod стал READY | Точное наблюдение принадлежащего выполнению Pod | Startup timer выключается; обычные tracker/deadline/cancel остаются | Прогресс `WORKLOAD_STARTING` меняется на `WORKLOAD_SCHEDULED` |

Root workflow с другими выполняющимися узлами сохраняет RUNNING: ожидающий этап
виден в графе. Открытый Human Gate не заменяется ресурсным QUEUED. Порядок одной
session, provider capacity и workflow dependency predicates сохранены.

Проекция credentials создаётся **после** ресурсного допуска. Её исходный срок не
продлевается ради Pending Pod. Startup timeout не даёт права повторить неизвестный
внешний эффект. Увеличение тайм-аутов, восстановление удалённого Secret вручную,
отключение cleanup и ослабление подписи/issuer/admission не являются recovery.

Прогретый системный Pod входит в занятые requests. Допуск консервативен: до
проверки полной warm compatibility учитывается место для холодного Pod. Это может
задержать warm turn в полностью заполненном кластере; виртуальное освобождение
занятого warm Pod не используется как недоказанный резерв для другого задания.

## Интерфейс

`Run.queueReason` и `RunDelta.queueReason` — optional read-only поля Proto/OpenAPI. Источник —
server-owned `runtimeQueueReason` в существующей presentation metadata, а не
текст модели. В списке и карточке показаны «В очереди» и объяснение ожидания либо
слишком большого запроса. После допуска маркер удаляется; terminal-проекция его
не возвращает. В графе каждого ожидающего execution node остаётся QUEUED и
локализованная `progressSummary`. Используется существующий readback/realtime,
без локального таймера, который выдумывает успешный запуск. Новый delta без
причины явно очищает прежнее значение; список также очищает optional поле при
авторитетном readback. RUNNING никогда не подменяется QUEUED старым текстом.

## Обновление, проверка и возврат

Новой миграции нет. Требуется совместимый набор immutable образов control-plane,
control-api-gateway, runtime-controller и staff-control-center из одной версии
контрактов; RoleImage input ABI не меняется. Адресный bootstrap-профиль
`component_image_profile.py` поддерживает также `control-api-gateway`, сохраняя
sidecars, сертификаты и прочие поля Deployment. Сначала обновляются control-plane и
gateway, затем UI и RBAC/controller. Не менять кандидат посреди приёмки.

1. Выполнить unit/controller tests, PostgreSQL `TestRuntimeResourceQueueComponent`,
   Proto/OpenAPI codegen, UI unit/typecheck/build и оба применимых render профиля.
2. На синтетической установке проверить два независимых turns при недостаточной
   одновременной ёмкости: очередь → освобождение → исходный turn. До допуска нет
   нового lease/credentials/Pod. Отдельно отменить ожидающее выполнение и проверить
   restart лидера. Не создавать новые внешние задачи/MR/страницы ради очереди.
3. Проверить слишком большой запрос и независимое меньшее задание, 401/отзыв
   credentials за время ожидания и отсутствие общего fallback.
4. Зафиксировать source SHA, четыре image digests, текущую схему и фактический
   browser/API readback. Unit/component PASS не равен установленной приёмке.

Для возврата сохранить exact image/config preimage. Схема не меняется. Старый
controller с новым control-plane продолжает legacy behavior. Это позволяет
вернуть controller/UI, сохранив нового владельца очереди. Перед возвратом самого
control-plane на старый протокол нужно через обычные owner-команды закончить или
отменить все ресурсные ожидания: старый claim не восстанавливает RUNNING у root,
переведённого новым владельцем в QUEUED. Затем явно выключить ресурсный режим
либо вернуть controller. Автоматический возврат старого CP при занятой очереди
не квалифицирован; прямой SQL не используется. Нельзя объявлять очередь
работающей, пока её server-side часть отсутствует. Обычное обновление образов не требует полного
дампа всех БД только из-за этой доработки.

Официальная документация по effective requests и Pending проверена через
Context7 для Kubernetes 1.32; actual helper API прочитан из pinned
`k8s.io/component-helpers v0.36.3`, совпадающего с существующим client-go.
