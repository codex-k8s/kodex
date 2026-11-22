---
id: OPS-DOC-KUBERNETES-CHILD-GATE-2145
title: Решение Human Gate для одного действия Kubernetes
type: operations
status: approved
owner: developer
version: 1.0.1
updated: 2026-11-15
---

# Решение для одного дочернего действия

Контракт #2145 использует существующий OwnerGate. Новый Gate не является новым
планировщиком, grant либо разрешением на весь Kubernetes invocation. Связь
`owner_gates.kubernetes_child_id → execution child → plan → original invocation`
назначает сервер. Gate node принадлежит исходному run/root и имеет parent
исходного invocation node (включая активный AGENT_EXECUTION). Actor, project, connection, target и grant не
принимаются от решающего клиента.

Публичное `OwnerGate.kubernetesChild` содержит только сохранённые plan/child refs,
operation, plan/input digests, target ref/revision, точный GVR/address и безопасные
`intendedName`/`intendedGenerateName` для CREATE. Collection observation CREATE не
выдумывает имя существующего объекта или его UID/RV. Поле `preview` показывает типизированные отправленные изменения, а не состояние провайдера до изменения. Raw body, Secret data,
protected material, identity locator, lease fence и готовый execution input не
попадают в Gate, UI либо событие. Hash связывает решение с исходным действием,
но не является permission или effect credit.

Значения preview получает только actor с текущим `gate.resolve` после существующей
проверки видимости Gate. `GetOwnerGate` допускает до512KiB, List — до16KiB;
события сохраняют digest и `NOT_AUTHORIZED`, без отправленных значений даже при последующем actor-scoped readback пользователем с правом решения. Параметр
disclosure и бюджет задаёт сервер, публичный request их не содержит.

Один shared builder проверяет исходный сохранённый canonical input тем же
operation parser. Он показывает известные builtin replicas, images, selectors,
resource limits, RBAC verbs/resources и типизированные options/argv. Это не
универсальный dump body: environment, annotations, configuration data,
неподтверждённые custom fields, scripts и чувствительные значения скрыты.
Heuristic credential markers — дополнительная защита; неизвестные поля скрыты
независимо от markers. Вложенный object вместо известного scalar не раскрывается.
Secret показывает только metadata и owner-bound `secretMaterialRef`; protected
bytes не читаются. Каждый пропуск виден через closed limitations, budget truncation
не называется полным diff. Digest остаётся SHA256 всего исходного input, включая
скрытые поля. Значения preview не принимаются обратно при Resolve и не используются
для исполнения.

| Переход | Источник полномочий и preconditions | Изменение и результат |
| --- | --- | --- |
| Создание OPEN | Broker Begin с исходной реальной binding; owner проверяет current source/actor/grant, immutable plan и exact child | Существующий Gate/node/edge, OWNER_GATE_OPENED; попытка не допускается |
| Чтение | Existing actor-scoped Get/List/readback с exact Gate scope; значения только при current gate.resolve | Безопасное описание immutable child и submitted preview, разрешённые решения и OCC; эффект и событие отсутствуют |
| APPROVE | Existing gate.resolve, exact Gate ETag/idempotency; fresh source→Connection/binding→invocation→Gate locks, тот же live claim/generation и current child grant | APPROVED и terminal HumanGate node; один OWNER_GATE_RESOLVED; parent invocation остаётся RUNNING с исходной lease; ни READY, ни continuation, ни нового effect credit |
| Следующий Begin | Исходный gateway/broker предъявляет реальную binding и exact child; весь current owner predicate повторяется | Только этот child получает однократный effect attempt; решение другого child не подходит |
| REJECT / CANCEL | Те же ownership/OCC/current claim checks; решение только исходного Gate | Один OWNER_GATE_RESOLVED для выбранного Gate, выбранный Gate/node terminal; original invocation REJECTED/CANCELLED, его lease/fence/instance закрываются; существующий dependency trigger закрывает зависимые attempts/streams/остальные OPEN Gates |
| Результат отказа | Existing integration continuation от terminal invocation | Actor получает исход отказа; это не отмена всего run и не повтор execution attempt |
| Истечение / revoke / replacement | Текущий lease, root, grant и source predicate перестают выполняться | Решение не создаёт новую lease/attempt; canonical owner terminal closure закрывает зависимый граф; новая попытка требует нового invocation/Gate |
| Повтор mutation | Existing scoped idempotency receipt плюс проверка полномочий | Прежний ответ без повторных Gate/effect/continuation writes; новый key со старым ETag отклоняется |

APPROVE не исполняет запрос Kubernetes. Он не меняет root initiator и не
подставляет credentials решающего пользователя. При закрытии сохраняются
immutable accepted receipts и факты уже начатых удалённых действий; cancel не
доказывает, что удалённый эффект остановился. Потерянный ACK восстанавливается
авторитетным чтением Gate и invocation, без повторной отправки эффекта.

Проверка того же original claim не восстанавливает raw fence из его digest.
Owner пересчитывает исходный operation snapshot по current selected identity,
configuration, subject, registry/target/scope pins и сохранённым generation /
effect_fence_digest. Stored plan.binding_digest остаётся immutable. Это чтение
не формирует broker binding и не выдаёт execution credit.

Для отображения и штатного решения нужны совместимые CP, control-api-gateway и
PWA из полного047 unit. Частичная декларация публичного поля либо успешный
отдельный UI тест не означает готовности owner producer, broker consumer,
полного schema proof, policy108 или установленного окружения.

Для sibling Gates, закрытых dependency trigger, отдельное событие решения человека
не создаётся: человек решил только выбранный Gate. Их authoritative read path —
existing Get/ListOwnerGates и текущий граф run. Выбранный Gate публикует ровно один
OWNER_GATE_RESOLVED в той же транзакции. Retained SENT receipts не переписываются,
CLAIMED notices закрываются как UNKNOWN_OUTCOME; отмена записи не является proof
удалённой остановки.

Для PWA проверены актуальные рекомендации Vue через Context7
`/websites/vuejs`: [text interpolation](https://vuejs.org/guide/best-practices/security)
экранирует отправленные строки. Preview выводится через text interpolation;
`v-html` и исполнение JSON из preview не применяются.
