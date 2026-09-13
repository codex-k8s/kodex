---
id: OPS-DOC-1470
title: Независимость служебной идентичности и восстановление readers
type: operations
status: approved
owner: manager
version: 1.1.0
updated: 2026-09-13
---

# Решение владельца и этап восстановления

Владелец выбрал вариант 2 в #1470: обычный внутренний RPC проверяет mTLS,
стабильную идентичность сервиса и локальное разрешение метода независимо от
Pod/Git SHA. Task grants, actor/org/project, Human Gates, replay, secret/file
scope и revoke сохраняются. Новый профиль вводится явным совместимым переходом;
ошибка текущего verifier не включает его автоматически. Реализация перехода
остаётся в #1470. Этот документ не объявляет её выполненной.

Исходная policy 77 содержит 394 bindings: 376 к Control Plane, 16 к Secret
Broker и два к STT. 297 bindings имеют caller control-api-gateway. Числа
являются инвентаризацией исходников, не результатами live QA. Каждый binding
нужно связать с проверкой actor, владельцем данных и task lifecycle; отсутствие
Pod grant не разрешает подставить сертификат вместо пользовательских прав.

После MVP отдельно учитываются адресная изоляция/отзыв реплики (#1471),
bounded emergency revoke стабильного сертификата (#1527) и Firefox/WebKit
(#1472). Отделение startup обычного клиента от optional proof dependencies
ведётся в #1528. Ускоренный staging-профиль до #1527 ограничивает допуск
точным сроком
проверенного сертификата; срочный отзыв выполняется ротацией trust material.
Пользовательский, task/attempt, Human Gate и предметный revoke этим решением не
ослабляются. Разработка продолжается через hot reload, одним агентом, пакетами;
каждый новый дефект получает bug Issue до следующего пакета.

## Совместимый переход обычных RPC

Control Plane сначала начинает одновременно читать прежний подписанный профиль
и явный `service-v1`. Новый reader проверяет mTLS peer, canonical SPIFFE ID,
точную пару caller + full method из встроенной target-owned policy и срок
сертификата. Для browser-запроса он затем проверяет OIDC credential и разрешает
actor/org/project у владельца состояния. Для служебного запроса он разрешает
system actor и поколение workload у владельца состояния. Runtime
materialization и STT delegation не понижаются до обычного service actor.

После readback Control Plane клиенты переводятся независимо. Ошибка `service-v1`
не повторяется через legacy. Прежний proof resolver и локальный issuer временно
сохраняются только для task delegation и отдельных защищённых materializer
вызовов; их startup dependency переносится в #1528. Откат отдельного клиента к
legacy допустим, пока dual reader работает;
откат Control Plane после перевода клиентов требует сначала вернуть клиентов.

## Восстановление существующего инцидента #1469

Старые readers отвергают документ целиком после expiry PREVIOUS. Проверенные
readers e11a306ce4184becace76502346c9f89459495f1 позволяют загрузить CURRENT,
но не разрешают использовать просроченный ключ. Требование здорового target
обычного sidecar rollout не даёт исправить уже неготовый Deployment.

`tools/release/authority-reader-recovery.mjs` предоставляет отдельный
forward-only путь только для текущих operator/owner operation IDs, exact
publisher spec/registry UID/data digest, exact source и воспроизводимых
issuer/verifier binary hashes. Immutable профиль закреплён на образе
`registry.local.kodex/kodex/internal-rpc-authority@sha256:d645561e5ae7cc223bbb0bf1f523eb8bf4c245a249ba3a5807e609867a4185f9`.
Скрипт не меняет grant, ключи, срок PREVIOUS, registry или application source.
Обычный rollout по-прежнему требует здоровые replicas.

План не более двух разных Deployment. Manifest `{version:1,targets:[...]}`
использует source/image targets из `authority-sidecar-rollout.mjs`. Для source
задаются `source.path`, exact `source.revision`, `roles`; для image — `image`.
Secret Broker с source issuer и image verifier проходит двумя отдельными
планами одного Deployment. Старые receipts не перезаписываются.

```bash
node tools/release/authority-reader-recovery.mjs plan --context default \
  --manifest "$MANIFEST" --capability "$CAPABILITY" \
  --rotation-plan "$ROTATION_PLAN" --output "$PLAN"
node tools/release/authority-reader-recovery.mjs apply --context default \
  --plan "$PLAN" --evidence "$EVIDENCE" \
  --confirm RECOVER-STAGING-AUTHORITY-READERS
node tools/release/authority-reader-recovery.mjs observe --context default \
  --plan "$PLAN" --evidence "$EVIDENCE"
```

Все файлы оператора приватные, вне source. Plan закрепляет UID/resourceVersion
и полный spec; перед каждым PATCH повторяется boundary, затем JSON Patch CAS.
Drift не форсируется. INTENT fsync предшествует PATCH, UNKNOWN разрешается
readback без повторной мутации. Независимый второй target может обновиться
после отказа первого. Observe не создаёт новый rotation intent или Job.

`AFTER` означает только применённый spec. Готовность target и фактические
binary hashes проверяются отдельно; восстановление API/OIDC и завершение
прежней rotation требуют последующих функциональных проверок. Исторические
ошибки HTTP сохраняются. Rollback к несовместимым readers не предусмотрен.

Ручная проверка: после восстановления войти через Chrome, открыть проект,
запустить работу и получить результат. Отдельно повторить независимый релиз
приложения с сохранением security spec соседей. Наличие этого runbook не
является PASS перечисленных сценариев.

Проверены документы Kubernetes через Context7 `/websites/kubernetes_io`:
JSON Patch `test` и условные изменения с resourceVersion. Новых библиотек,
production-действий и раскрытых секретов нет.
