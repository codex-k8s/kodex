---
id: OPS-PUBLIC-STABLE-001
title: Штатная поставка Kodex и внешний доступ с SSO
type: operations
status: approved
owner: manager
version: 1.1.0
updated: 2026-10-15
---

# Цель и решения владельца

Issue [#1810](https://github.com/codex-k8s/kodex/issues/1810), начальный
main `8c19ffb76328ae559698fae2806be79ef0bd64ae`.

Kodex переходит с hot reload на штатные собранные immutable images.
Постоянные сервисы получают две реплики; новая внешняя точка входа —
`https://platform.kodex.works`, с обязательным oauth2-proxy/Keycloak SSO.
Уточнение владельца: PostgreSQL, NATS и S3 не реплицировать, их существующие
данные и конфигурацию не менять. Одноразовые Job и exact runtime executions
не дублировать. Совместное размещение не даёт права пересоздать соседний
кластер или его маршруты.

Для внешнего входа рекомендуется отдельный exact Host/SNI-маршрут на уже
работающем внешнем ingress; текущий владелец публичных портов не меняется.
Kodex TLS, SSO и backend остаются отдельными. Связность между ingress и
Kodex проверяется до выбора exact upstream; общий доступ между сетями,
TLS skip, wildcard route/egress и копирование чужих секретов запрещены.

После проверки текущего ingress выбран Host routing с публичным TLS на
внешнем ingress и повторным HTTPS до отдельного Kodex ingress. Публичные
сертификаты выпускает существующий ACME resolver только для новых Kodex
hostnames; private keys не копируются между кластерами. Вверх доставляется
только публичная CA для проверки exact upstream SNI. Это не требует менять
глобальный HTTP redirect или challenge handler соседнего приложения.

# Этапы

- [x] **1. Read-only preflight DNS/портов/соседнего входа.** Публичный
  hostname Kodex резолвится на этот сервер; соседний HTTPS отдаёт302.
  Public80/443 принадлежат внешнему ingress соседнего кластера, Kodex
  обслуживается отдельным loopback ingress. На новом hostname сейчас
  certificate hostname mismatch, штатная точка входа ещё не настроена.
- [ ] **2. Зафиксировать точный состав релиза и матрицу реплик.** Inventory
  Deployment/StatefulSet/Job/PVC/images/host mounts, общий бюджет ресурсов,
  leader/claim/inbox semantics, preservation digests. БД/NATS/S3 исключены.
- [ ] **3. Собрать и проверить штатные immutable images.** Exact source,
  provenance/digests, необходимые registry/node pull boundaries; без Air/Vite,
  source mounts и незаполненных image pins в итоговом PodSpec.
- [ ] **4. Реализовать cutover и двухрепличный профиль.** Repo-owned
  plan/apply/readback/rollback с exact drift/idle guards; rolling/drain,
  leader handoff, отсутствие двойных execution/effects, readiness на рабочем
  пути. PKI, active grants, runtime profiles и promoted pins сохраняются.
- [ ] **5. Подготовить публичные ingress/TLS/OIDC/SSO.** Exact маршруты
  платформы и OIDC, публичный доверенный certificate и renewal, согласованные
  issuer/callback/logout/origin/cookie настройки. API/WebSocket защищены;
  OAuth callback/realm assets не открывают обход к приложению.
- [ ] **6. Применимые тесты, render и проектные review/owner gate.** Scope
  этой задачи не наследует исключение bootstrap1797 автоматически. Нельзя
  считать план первоначального запроса подтверждением готового PR.
- [ ] **7. Выполнить тот же cutover и контрольное чтение.** Exact images,
  отсутствие hot reload, две ready/available сервисные реплики, сохранённые
  data/credentials/pins, работоспособность при смене одной реплики.
- [ ] **8. Browser и внешняя приёмка.** Clean-session redirect в Keycloak,
  login/logout, UI/API/WebSocket/rejoin, Console/Network/backend, desktop/mobile;
  прежние соседние маршруты/TLS/SSO не изменились. Отчёт на exact SHA.

# Матрица жизненного цикла поставки

| Фаза | Источник полномочий/состояния | Обязательное доказательство |
| --- | --- | --- |
| preflight/plan | Решение владельца, exact контекст и live inventory | Нет writes, совпавшие source/resources/pins и baseline соседнего входа |
| build | Проверенный source SHA, версия recipe/toolchain и registry identity | Immutable OCI и provenance, без новых runtime grants |
| drain/idle | Владелец доменных leases/tasks, а не локальное предположение | Отсутствие незавершённой работы либо штатный bounded drain/join |
| apply | Проверенный exact план после review и owner gate | CAS/drift, сохранённые data/credentials; нет wildcard patches |
| два сервисных Pod | Существующие leader/owner claim/fence и durable inbox | Один победитель effect; незапущенный Pod не считается READY |
| leader expiry/replacement | Прежний owner lease и монотонное поколение | Старый grant/fence не оживает, новый leader не повторяет effects |
| terminal/cancel/retry | Авторитетные существующие owner commands | Полный граф закрывается атомарно, Job/execution не размножаются |
| rollback | Exact предыдущие безопасные image/config pins | Не откатывает applied migrations, данные или authority generation |
| итоговый readback | Реальные Pod/imageID/endpoints и owner/browser reads | SHA/digests/replicas/SSO и baseline соседнего сервиса совпали |

Stateful зависимости остаются текущими; это двухрепличная поставка сервисов,
не заявление о HA всего сервера, диска, БД, broker или S3.

# Начальный журнал

15.10.2026 02:35 Саратов: read-only DNS/ports/ingress inventory PASS,
соседний anonymous HTTPS302. Новый hostname TLS FAIL (hostname mismatch),
не исправлен и не скрыт через skipTLSVerify. OAuth proxy уже имеет две
реплики; Go/Vue hot workloads преимущественно имеют одну. Постоянные
PostgreSQL/NATS/session-store/S3 по одному; их данные не трогались.
`sso.kodex.works` также резолвится; окончательный issuer/certificate ещё
не выбран и не настроен. Context7: официальные oauth2-proxy Redis/session
и Traefik HostSNI/passthrough docs проверены без передачи приватных доменов.

Реализация, сборка, live cutover, certificates и внешняя browser-проверка
пока NOT RUN. Рабочий main и действующие Pod не менялись.

# Матрица компонентов и оставшихся ограничений

| Область | Штатная цель | Условие перед применением |
| --- | --- | --- |
| Go приложения и фоновые контроллеры | 2 compiled Pod | Exact image/provenance, сохранённые Secret references, CAS, idle/drain и существующие leases/claims |
| `session-archive` | 2 compiled Pod | Сохранить аргумент `controller`; не подменять его одноразовым execution |
| `integration-synthetic` | 2 compiled Pod текущего стенда | Отдельный существующий fixture adapter в target `synthetic`, не внутри рабочего gateway |
| Фронтенд | 2 nginx Pod из сборки | Удалить Vite/source mounts; согласовать штатные TLS/Service/ServersTransport и runtime ConfigMap |
| Keycloak | 2 Pod | Вместо local cache — поддерживаемый кластерный cache, exact peer network rules; PostgreSQL остаётся одним |
| oauth2-proxy | 2 Pod | Сохранить Redis/TLS session store, согласовать issuer/callback/logout/cookies с внешними hostnames |
| Registry | 2 Pod на роль | Общее существующее хранилище, exact node для local RWO, общий HTTP upload secret; pull/push/promotion/evidence identities не смешиваются |
| BuildKit и image workers | 2 Pod | Изолированные scratch/cache и существующие атомарные claim/fence; новый Pod не дублирует задачу |
| Grafana | 2 Pod | Нельзя независимо запускать два SQLite; сначала поддерживаемая общая БД с сохранением данных и SSO |
| Alertmanager | 2 Pod | Настройка cluster peers и отдельных PVC через авторитетный operator, не ручной patch его StatefulSet |
| PostgreSQL, NATS, S3, Redis, Prometheus TSDB | Без репликации | Не менять данные, volumes или количество Pod хранилищ |
| Runtime execution, Job, CronJob, DaemonSet | По своему контракту | Не превращать один execution/одноразовую task в две работы |
| Ingress и certificate controllers Kodex | 2 сервисные реплики | Проверить их собственный leader/storage protocol; не масштабировать соседние controllers |

Inventory не выдаёт неизвестный StatefulSet за исключённую БД: Grafana и
Alertmanager относятся к сервисам с обязательной storage review. Discovery
основного списка покрывает четыре платформенных namespace; cluster ingress
и certificate controllers дополнительно проверены read-only, но полный
release/fence matrix ещё не завершён. Пункт 2 остаётся незакрытым.

# Журнал реализации до первого применения

15.10.2026: main `8c19ffb76328ae559698fae2806be79ef0bd64ae` не изменялся.
Новый код находится в отдельной ветке PR [#1811](https://github.com/codex-k8s/kodex/pull/1811).

- PASS: read-only inventory 141 ресурсов; 13 платформенных Deployment с hot
  commands, две oauth proxy deployment уже имеют по две реплики. Это не
  доказательство готовности новой поставки.
- PASS: 18 Node unit tests моделей inventory/cutover/public edge и реального
  ограниченного Bash build pool. Проверены отказ на unknown binary, wrong
  source command, placeholder digest, shared dev mounts, single-Pod PVC,
  unsafe registry storage, wildcard routing, public listener и broad peers.
- PASS: Go toolchain contract с repo-pinned Go1.26.6; `bash -n`, `shellcheck`
  release builder и `git diff --check`. Первый запуск с системным Go1.27.1
  завершился FAIL; повтор выполнен с правильным toolchain и без чужого GOROOT.
- Реализован отдельный immutable Docker target существующего synthetic
  adapter. Рабочий integration gateway этот binary не содержит.
- Сборка релиза имеет бюджет 1..4, по умолчанию 2. Обычные buildctl процессы
  ограничены пулом; отказ дожидается batch и запрещает публикацию release lock.
  Runner и зависящий от него role base сохраняют порядок.
- Public edge model/plan создаёт только добавочные exact Host resources и
  приватный loopback relay с exact peers. Existing ingress Deployment,
  публичные80/443, firewall и глобальные redirect/ACME параметры не меняются.
- Chrome MCP подключён, собственная вкладка обновлена без несохранённого
  ввода; чужие вкладки не затрагивались.
- NOT RUN: build/push новых OCI, live apply, две рабочие реплики, публичный
  certificate, новый OIDC login/logout и внешняя browser-приёмка.

Эти проверки относятся к изменённому дереву после `d4c77e18c28d7393e3f54bd6f83c08b99a66c0bb`;
после коммита точный проверенный SHA фиксируется отдельной записью. Модели и
план не объявляются завершёнными executable apply/rollback.

## Контрольное чтение на `213ba8f33c6a68a5697954193575b3dc3f12f7fa`

- PASS: тот же набор 18 unit tests, pinned Go toolchain contract,
  `web-only-release-test.sh`, Bash syntax, ShellCheck и diff whitespace.
- PASS: push/readback GitHub — Draft #1811 содержит этот exact SHA,
  main остаётся `8c19ffb76328ae559698fae2806be79ef0bd64ae`.
- PASS: read-only public edge plan на фактическом внешнем cluster; четыре
  добавочных ресурса и два systemd unit. `systemd-analyze verify` завершился0;
  сообщения об устаревшей CPUAccounting относятся к существующим сторонним
  system units, не к создаваемому relay.
- NOT RUN: установка relay, создание CA projection, новые маршруты и ACME,
  SSO cutover, новые образы и масштабирование. Private plan остаётся только
  локальным: topology и чужие resource details не добавляются в репозиторий.
