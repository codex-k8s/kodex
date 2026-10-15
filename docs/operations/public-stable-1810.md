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
данные и конфигурацию не менять. Инфраструктурные singleton без безопасного
HA сохраняются в одном экземпляре, в том числе storage provisioner и Grafana
с существующей SQLite; смена storage backend и миграция БД ради второй
реплики не требуются. Одноразовые Job и exact runtime executions
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
| Grafana | 1 Pod, инфраструктурное исключение | Сохранить существующие SQLite, PVC и SSO; не запускать двух писателей и не мигрировать БД ради второй реплики |
| Alertmanager | 2 Pod | Настройка cluster peers и отдельных PVC через авторитетный operator, не ручной patch его StatefulSet |
| PostgreSQL, NATS, S3, Redis, Prometheus TSDB | Без репликации | Не менять данные, volumes или количество Pod хранилищ |
| Runtime execution, Job, CronJob, DaemonSet | По своему контракту | Не превращать один execution/одноразовую task в две работы |
| Ingress и certificate controllers Kodex | 2 сервисные реплики | Проверить их собственный leader/storage protocol; не масштабировать соседние controllers |

Inventory не выдаёт неизвестный StatefulSet за исключённую БД: Grafana явно
остаётся singleton по решению владельца, Alertmanager требует storage review. Discovery
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

## Следующий пакет: исполняемый Go cutover и локальная сборка

Добавлен `tools/release/stable-go-release.mjs`: приватные plan/apply/readback,
exact cluster/resource identity, проверка drift до первого write и перед
каждым PATCH, свежий owner idle read через существующий read-only SQL,
сохранение promoted pins, журнал намерения до PATCH и контроль двух
принадлежащих Deployment готовых Pod с фактическими imageID. Env/spec не
сохраняются в плане и не передаются в argv; Secret references сохраняются.
Apply требует отдельного owner решения и exact plan SHA, сам аргумент не
заменяет проектное review. Rollback этого пути пока не реализован.

Модель теперь закрыто проверяет реальный PVC protocol перед Go cutover;
local RWO получает exact node pin, RWOP отклоняется. Init artifact spool
переходит на тот же compiled controller, исходное хранилище не теряется.
Старые host source annotations снимаются, normal Pod не помечается hot reload.

На дереве после `c1e6909dcf2990591a8ca6670d388cfb3823dadf`: 22 Node tests,
включая synthetic CLI plan/apply/readback и отказ без PATCH при неверном
подтверждении либо занятости, PASS. Это disposable test, не живой cutover.
Server dry-run четырёх Go Deployment до добавления PVC guard прошёл PASS;
он использовал тестовые image references, ничего не применял и не доказывает
admission/publication/node pull новых образов.

`tools/release/build-stable-local.sh` собирает выбранный compiled компонент
из credential-free `git archive` точного чистого SHA через существующий
BuildKit. Он не пересоздаёт builder, не импортирует и не публикует образы,
не трогает Pods. Receipt содержит source/context/archive/manifest digests;
admission, publication и node pull явно NOT RUN до отдельных проверок.
Синтаксис/ShellCheck PASS; реальная OCI сборка этого нового пути ещё NOT RUN.

## Реальные OCI сборки и исправление фронтенда

Локальная compiled OCI сборка нового пути выполнена. Это не registry
publication, admission, node pull либо развёртывание.

| Компонент | Exact source SHA | Manifest SHA256 |
| --- | --- | --- |
| control-plane | `2087c69d4d3a3fb7aba8736c3c6bc7ab2c9f6411` | `850912011101bd8c6a8af8f2f07c01da24e9bc3fd5831e23f8a15cf790dd71c9` |
| session-archive | `2087c69d4d3a3fb7aba8736c3c6bc7ab2c9f6411` | `9b8a8155ccb400ba98600aae8e95b2845ee70541b27a4bfeeeaf370339697399` |
| integration-synthetic | `2087c69d4d3a3fb7aba8736c3c6bc7ab2c9f6411` | `08e8079b14e29bd94bbad52a709a6f6e1fd4a733528dd72a02a7a3b862cf38ac` |
| runtime-controller | `2e07f737b824b79451ba5a8408c4f7744e33471c` | `31e7e50f84cc506b99f87a31abe0dbf272d59ba935871e176f6e2be3c0abf3a9` |
| control-api-gateway | `9e54c6e8bcfee675559c6a16f2ed6d1141d3a4ba` | `58f064ba63be2e9658c9641b673ca4ba18dc8eed45ec73f49911ae8a42c061b5` |
| staff-control-center | `9e54c6e8bcfee675559c6a16f2ed6d1141d3a4ba` | `79db365eed79750907d4558a04ea46431ed90ef1eed74f85e76d5952b00374cc` |
| secret-broker | `d093118e0843a2e9ed0217538e9b6795d83cd04b` | `a83a43835cd5cc05ce0c5cb65c29f3ab894ad6c4c5f97cbe8c5c414ae948a571` |
| egress-gateway | `d093118e0843a2e9ed0217538e9b6795d83cd04b` | `aa4f231a94c2a5cbcd75f258fca459946a8bd843403e4b0337fef13b67eefa38` |
| integration-gateway | `c61e42bacf94ffafdcabbd166f4895a670bab272` | `9b5364df61072ca82b03104407a329e3b01d66d38753290f2c436077ed1621a4` |
| email-bridge | `c61e42bacf94ffafdcabbd166f4895a670bab272` | `2ad6093e18498bb9477ca7a8301df6f4f9a0f32be51ce7dca0d17dddd6298239` |

Первый frontend build завершился FAIL: Dockerfile не копировал актуальные
test/config dependencies TypeScript. Следующий выявил неполный
Dockerfile-specific allowlist. Исправлены оба источника: configs, E2E helpers
и их настоящие declarations доступны build stage, секреты/локальные файлы
по-прежнему исключены. Ничего из tools/e2e не копируется в итоговый nginx.
Третий build на указанном SHA прошёл PASS с обычным `npm run build`; typecheck
не отключался. Добавлен регрессионный тест контекста и модель nginx cutover,
сохраняющая current runtime ConfigMap. Переключение8080→8443 требует отдельной
Service/transport-фазы; zero-downtime для него пока не заявляется.

## Две реплики identity

В repo-owned identity manifest теперь две Keycloak реплики, cache `ispn`,
discovery `jdbc-ping`, embedded mTLS включён явно, PodDisruptionBudget
`minAvailable: 1`. Cache и failure-detection ports доступны только peer Pod
с обоими identity labels в том же namespace, в ingress и egress. База остаётся
одной репликой, её manifest не изменялся. Два render/network unit tests PASS;
live cache cluster, replacement/login и upgrade ещё NOT RUN.

Проверены официальные [Keycloak cache docs](https://www.keycloak.org/server/caching)
и [Docker COPY docs](https://docs.docker.com/reference/dockerfile/#copy).

Повтор на exact `d093118e0843a2e9ed0217538e9b6795d83cd04b`: 27 быстрых
проверок и полный `web-only-release-test.sh` PASS после изменения identity
manifests. GitHub Draft #1811 exact head подтверждён; рабочий main по-прежнему
`8c19ffb76328ae559698fae2806be79ef0bd64ae`, чистый. Live контроль трёх основных
Deployment подтвердил исходные1/1; новые replica settings ещё не применялись.

## Уточнение системного singleton перед применением

При дополнительном read-only inventory Kubernetes infrastructure найден
`local-path-provisioner` версии `v0.0.36`, одна реплика. В
[точном upstream source](https://github.com/rancher/local-path-provisioner/blob/v0.0.36/main.go#L281-L290)
выбор лидера отключён. Он выполняет создание/удаление локальных PV; простое
удвоение controller не является поддерживаемым HA и создаёт риск гонок
операций с диском. Изменений этого controller или его PVC не было.

Для буквального требования двух реплик всех постоянно работающих компонентов
нужно отдельное решение владельца:

1. Рекомендуется оставить cluster storage controller одной репликой как
   исключение для инфраструктуры хранения, наряду с исключёнными данными;
   обычные прикладные сервисы и поддерживаемые HA controllers — по две.
2. Расширить работу до HA storage controller: отдельная поддерживаемая
   реализация/изменение leader election, воспроизводимые create/delete/failover
   проверки и доказательство сохранности существующих PV. Без этой работы
   две Ready реплики не объявляются готовым HA storage.

Владелец выбрал вариант 1: подобные инфраструктурные singleton остаются в
одном экземпляре. `local-path-provisioner` сохраняется с одной репликой;
отдельная реализация HA storage controller не входит в эту работу. Обычные
прикладные сервисы и поддерживаемые HA controllers остаются в двухрепличном
плане. Это не означает репликацию локальных данных или отказоустойчивость
сервера целиком.

Повторное read-only чтение обнаружило 50 PVC класса `local-path`, все `Bound`:
37 в `kodex-runtime`, 9 в `kodex-system`, 3 в `observability`, 1 в `identity`.
Provisioner нужен для автоматического выделения и удаления локальных томов,
в том числе состояния новых сессий агентов; сам `/workspace` сейчас `emptyDir`,
а `/workspace/.kodex/state` — PVC. Он не обслуживает поток чтения/записи
уже подключённых томов. Изменений ресурсов или данных при проверке не было.
Причина ожидания решения снята; применение по-прежнему требует предусмотренных
review и owner gates. Цель не завершена, обязательные этапы не отмечены.
Уже собраны десять локальных OCI, ещё без публикации.

Подготовка public edge дополнительно связывает исходную CA с exact cluster и
ConfigMap UID; private keys не читаются и не копируются. Модель теперь имеет
два независимых relay процесса и два endpoint вместо нового singleton.
Она покрыта model tests. Результаты нового плана и исполняемого edge workflow
зафиксированы ниже; live apply остаётся NOT RUN.

## Завершение локальных сборок и исполняемый публичный маршрут

На source `80438038f991d152455d7753f061bbac7e4903e8` успешно собраны ещё
два OCI, с точными receipt и SHA-256 архива:

| Компонент | OCI manifest digest |
| --- | --- |
| `automation-scheduler` | `sha256:f79299556a9e7bdd292bd0a72d7f67b9ee3dbb493c9f70c80f7ab9e9357475b0` |
| `artifact-retention` | `sha256:0b59a91ad5700aaab9cd910bac65043ae75b996f75ab867b4d7048fff9d38706` |

Всего локально собраны 12 разных compiled OCI для 13 текущих hot Deployment:
два egress Deployment используют один образ. Это завершение локальной сборки,
а не этапа 3 целиком: registry publication, release provenance/admission,
node pull и реальное serving imageID ещё NOT RUN.

На exact `58d69f543c60429b1735bc91acb5ee9d4ac22361`:

- PASS: 35 быстрых Node tests, `git diff --check` и полный
  `scripts/tests/web-only-release-test.sh` с закреплённым Go toolchain.
- PASS: реальный read-only `edge-plan.mjs` создал v2 plan для двух разных
  кластеров, exact Docker node identities и текущей публичной CA. В плане
  6 добавочных Kubernetes ресурсов и 4 unit-файла; hash
  `84483d1711b2579a73d7e42e865aacdcf74681f80bfe00225c0d5ab0813bee3f`.
- PASS: `systemd-analyze verify` новых четырёх units. Два предупреждения
  относятся к уже установленным системным units и устаревшему CPUAccounting,
  а не к новым файлам Kodex.
- PASS LOCAL/SYNTHETIC: `edge-release.mjs` apply/readback/rollback с fake
  Kubernetes/host adapters. Проверены exact owner confirmation, сохранение
  чужого baseline, два socket listeners, lost create response без повтора,
  отказ удаления подменённого UID до первого DELETE и удаление только своих
  ресурсов. Реальный `kubectl` отдельно отправил DELETE с UID/resourceVersion
  preconditions в одноразовый loopback HTTP fixture, не в кластер.
- Исправлены ошибки первой версии оснастки: тестовый Stats wrapper терял
  методы prototype, а actual raw DELETE добавляет штатный `timeout` query.
  Первый запуск был FAIL; повтор после исправлений — PASS на указанном SHA.
- NOT RUN: установка units в `/etc`, создание public ingress ресурсов,
  публичный certificate, новый OIDC issuer и browser SSO. Рабочий main и
  application PodSpec не менялись.

`edge-release.mjs` повторно рендерит все артефакты из плана, проверяет CA и
cluster/node identities, записывает intent в fsync-журнал до эффекта. Unit
создаётся с noclobber, root ownership и сохранением на диск; чужой файл не
перезаписывается. Kubernetes объекты создаются через `create`, не универсальный
`apply`. Известные API defaults не разрешают добавить Service selector,
ExternalName, иной route, root CA, finalizer или ownerReference. Rollback
проверяет весь набор до удаления и передаёт серверу UID/resourceVersion
preconditions. Неопределённый create не повторяется автоматически; его точный
intent может быть разрешён при rollback. Частично записанный unit с неверным
содержимым закрыто останавливает операцию и требует отдельного разбора.

Пример команд после обязательных review и отдельного owner OK на exact plan:

```bash
node infra/public-ingress/edge-release.mjs apply \
  --context <edge-context> --plan /private/edge/plan.json \
  --approved-plan-sha <approved-sha256> --output /private/edge-apply.jsonl
node infra/public-ingress/edge-release.mjs readback \
  --context <edge-context> --plan /private/edge/plan.json \
  --journal /private/edge-apply.jsonl --output /private/edge-readback.jsonl
node infra/public-ingress/edge-release.mjs rollback \
  --context <edge-context> --plan /private/edge/plan.json \
  --approved-plan-sha <approved-sha256> --journal /private/edge-apply.jsonl \
  --output /private/edge-rollback.jsonl
```

CLI подтверждает только `EDGE_RESOURCES_ONLY`, не успешный публичный TLS,
SSO или итоговую приёмку. Аргумент hash не заменяет процесс review/owner gate.
Остаются публикация образов, штатный frontend/Service TLS cutover, полный
Go rollback и двухрепличный профиль, certificate/OIDC/client migration,
review/owner gate, live cutover и внешняя browser-приёмка. Цель активна;
обязательные этапы не отмечаются по частичным локальным доказательствам.
