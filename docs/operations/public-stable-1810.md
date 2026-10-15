---
id: OPS-PUBLIC-STABLE-001
title: Штатная поставка Kodex и внешний доступ с SSO
type: operations
status: approved
owner: manager
version: 1.1.1
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

## Архиватор: изоляция попыток двух реплик

Источник состояния — `SessionArchiveTask` control-plane, не Kubernetes label.
`ClaimSessionArchiveTasks` использует workload identity/mTLS/application grant,
organization scope, `FOR UPDATE SKIP LOCKED`, новую attempt/generation и exact
lease/fence. Generated client `SessionArchiveOperations` и существующий renew
связывают controller с этим владельцем. Внешнего endpoint для cleanup нет.
Jobs и immutable ConfigMap — производная материализация, не новый источник
полномочий; каждый прежний input проверяется по полной owner-связке.

| Переход | Проверка и результат |
| --- | --- |
| startup / readiness второй реплики | Только read Kubernetes API и рабочий RPC; namespace-wide cleanup запрещён |
| claim SNAPSHOT / RESTORE / DELETE_OBJECT | Свежий exact renew; адресное завершение прежних attempt той же task/content generation; immutable input и UID/RV; затем новая Job |
| claim DELETE_PVC | Та же свежая authority; существующая exact PVC identity/deletion; Job не создаётся |
| retry / lease expiry | CP назначает следующую attempt; максимум5, прежняя lease не восстанавливается; новый writer ждёт foreground удаления прежней Job и отсутствия её Pods |
| renew / cancel / terminal / project purge | Прежний owner fence; отказ renew прекращает работу и удаляет только доказанно собственную материализацию; чужая task не затрагивается |
| complete / fail | Диагностика до cleanup; собственные Job/Pods joined до owner completion; CP сохраняет receipt/audit и существующие session/run события |
| unknown Create / заменённый UID | Не удалять объект по имени; сохранить input для проверки следующей attempt; ошибка не превращается в success |
| dead letter / orphan / несовпадающий input | Нет глобального sweep либо автоматического расширения authority; закрытый отказ, exact readback через CP task/session storage и Kubernetes metadata |
| cutover / rollback | До смены worker pin нет READY/CLAIMED archive tasks и managed Job/Pod; controller/worker используют один проверенный OCI; прежние данные не изменяются |

RBAC controller больше не выдаёт `deletecollection`; точечное чтение input
нужно для проверки predecessor. Worker сохраняет пустую Kubernetes роль.
Поддерживаемый путь не содержит второго decoder или legacy API.

Переход старого controller выполняется в отдельной фазе того же exact плана:
CAS → `replicas=0` → observed zero и отсутствие owned Pod → повторный owner
idle read → CAS только `Role.rules` из repo-owned manifest → compiled2.
RoleBinding, UID ресурсов и storage/pins проверяются до каждой фазы.
Новая роль не ставится при живом старом controller; возобновление writer не
происходит при оставшемся Pod, drift, unknown ответе или занятом owner graph.
Потерянный ответ patch фиксируется как `FAIL_OR_UNKNOWN`, не повторяется
по предположению. Отдельный readback устанавливает достигнутое состояние;
автоматическое восстановление старого hot startup cleanup запрещено.

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

## Проверенная публикация штатных приложений

На exact `1c653c314a2cf7cd4f8a9bd0198b52913a8f0180` добавлен отдельный
`tools/release/stable-image-publication.mjs` с plan/apply/readback и ограниченный
`publish-stable-image-worker.sh`. Bootstrap `seed ... --component all` не
используется: его тестовая подстановка runner под control-plane не является
поставкой приложения.

- PASS: 39 быстрых Node tests, 26 Python tests preserved/stable OCI,
  Bash syntax, ShellCheck и `git diff --check`.
- PASS PLAN ONLY: все 12 фактических OCI прошли проверку через новый CLI.
  Сверены архив целиком, каждый blob/descriptor, config/entrypoint/non-root
  user, точный Dockerfile target, Git source context hash и неизменность
  build inputs соответствующего компонента. Проверенные копии `0400` и
  owner-private планы сохранены вне Git; schema/receipt не объявляют
  подпись, SBOM или завершённый domain admission.
- Один первоначальный preflight FAIL закрыл доступ к изменяемому указателю
  инструмента. Использован уже существующий приватный pinned artifact той
  же установки; guard не ослаблялся, общий указатель не менялся.
- PASS LOCAL/SYNTHETIC: настоящий CLI и Bash worker прошли plan/apply/readback
  с fake Kubernetes/Docker. Проверены закрытый список компонентов, точный
  owner hash, drift registry, lost publication response, readback без нового
  import и завершение helper/port-forward до удаления временных данных.
  Sentinel не попал в plan, journal, stdout или argv.
- NOT RUN: фактический import в registry, node pull и любые PodSpec mutations.
  Планы связываются с clean executor SHA и после следующего коммита требуют
  повторного формирования перед owner-approved применением.

Публикация разрешает только 12 названий приложений и уникальный
source/digest-specific tag, не `runner`, `roles`, inputs или старый readiness
tag. Существующие repository pins не перезаписываются. Registry и writer
projection связываются с exact cluster/namespace/resource identities.
Worker использует отдельную mTLS/basic identity promotion registry,
строгую CA/SNI проверку, случайный loopback port-forward и pinned tools
image. Credentials не передаются в argv или env контейнера; helper читает
только private read-only input, не пишет HOME на host и не получает Docker
socket, kubeconfig либо другие credentials. До registry effect сохраняется
fsync intent; неопределённый результат разрешается отдельным readback.
CLI явно возвращает только `REGISTRY_PUBLICATION_ONLY`, `nodePull: NOT_RUN`.

### Обязательное продолжение node pull

Проверка рабочего consumer выявила текущий закрытый список в
`services/jobs/role-image-builder/cmd/registry-pull-authorizer/main.go`:
node identity и installer profile разрешают только runner, roles,
session-archive и role-image-builder. Поэтому публикации нового API/frontend
недостаточно: их скачивание узлом сейчас будет отклонено. Это не внешний
blocker и не повод отключить authorizer. Требуется точный список platform
repositories для существующей node identity, отрицательные GET/HEAD/WRITE и
чужие repository проверки, новый compiled authorizer и воспроизводимый
bootstrap самого registry-serving образа. Последующий actual node pull должен
проверять тот же штатный TLS/DNS/identity путь, не только наличие в node cache.
Probe, BuildKit/input profiles и права записи не расширяются этим решением.

### Материализация node pull для штатного профиля

Дополнение code-first плана: закрытый compiled список node repositories
покрывает все shipped Pod images, migration/optional jobs, существующие
admission/tools images и динамический `roles`. OCI build inputs (`dockerfile`,
`role-base-documents`, `role-image-inputs`) и evidence/admin не выдаются node
identity. Совпадение списка с release inventory проверяется тестом; новые
repositories не добавляются wildcard или env-параметром. Probe сохраняет
только прежний control-plane read, BuildKit/input profiles не меняются.

Путь: kubelet/containerd с существующими exact node mTLS/basic credentials →
public pull DNS/SAN и exact ingress → `registry-pull-authorizer` → внутренний
read-only registry. Только GET/HEAD manifest/blob; существующие certificate
generation, peer IP и application credential checks остаются до repository
решения. Это чтение immutable artifact, без DB mutation, task/grant, owner
state transition или domain event. Его авторитетное readback — manifest digest
и фактический serving `imageID` через CRI каждой ноды.

Для обычной поставки authorizer выделен в минимальный distroless target
`registry-pull-runtime` того же unit. В base он имеет собственный image ref,
command и прежние credentials/readiness/sidecar. Release lock требует exact
новый digest. Hot local profile явно использует свой admission bundle, где
тот же binary уже собирается; это отдельный dev profile, не fallback при
ошибке штатного pull. Никакого копирования исполняемого файла в live Pod.

Старый authorizer не разрешает скачивать новый registry-serving repository.
Его bootstrap поэтому требует отдельной проверяемой фазы: durable публикация
нового exact OCI, затем однократная материализация только этого проверенного
registry-serving digest в cache точных принадлежащих кластеру нод и rollout
проверенного PodSpec. Это не альтернативный путь для приложений. После rollout
`imagePullPolicy: Always` на exact node probes обязан доказать обычный
TLS/DNS/credential pull и для самого gateway, и для приложений; preload/Ready
не считается таким доказательством. Сам import, rollback и контроль этой фазы
ещё требуют исполняемого repo-owned адаптера, review и отдельного owner OK.

Текущий read-only Pod inventory также подтвердил сохраняемые compiled
`image-admission` и `image-admission-tools`. Их прежние exact artifacts должны
быть доступны через durable pull endpoint, а не только из node cache;
бизнесовые pins/grants не пересоздаются ради публикации. До доказательства
всего пути этапы 2/3/4/7 остаются незавершёнными.

## Собранный pull gateway и результаты проверок

На exact `69cded420aeb39511ce58123a45aed694aad3de9`:

- PASS: Go unit/race и `go vet` для pull-authorizer/nodepullidentity.
  Проверены signed node credential, GET/HEAD всех 23 shipped Pod repositories,
  прежние profile restrictions, отсутствие WRITE и namespace/selector escape.
  Список соответствует release inventory без build-only inputs.
- PASS: 41 Node tests, 27 Python OCI tests, ShellCheck трёх release scripts,
  Go toolchain contract и полный `web-only-release-test.sh`.
- PASS LOCAL OCI: новый `registry-pull-authorizer`, source тот же SHA,
  manifest `sha256:02e42ed073443c9afc11c4a16e2046e5f8f1368139d2bef23f591fd59b3cf9be`.
  Target `registry-pull-runtime` содержит только нужный binary, distroless
  base, UID/GID10001 и прежний command; tools/scanners туда не переносятся.
- PASS PLAN ONLY: реальный publication CLI проверил этот OCI/context/recipe
  и создал private план, hash
  `25321a5719f500dc98aa8a53d5baadf75a5e9289d8069c690addbec97f5157cc`.
- Проверены normal base/render binding и фактическое выражение local renderer:
  dev profile использует свой exact bundle, посторонние images не меняются.
- Проверены Context7 Go net/url docs; Go toolchain остаётся1.26.6.
- NOT RUN: публикация и bootstrap gateway на нодах, новый live authorizer,
  actual node pulls, двурепличный rollout и внешний SSO.

Найдены preserved OCI, совпадающие с работающими admission/tool containers:
`image-admission@sha256:c17d3c048de86a865864f138887f159feacb497107a99eb7e7ae7273e90ca8b2`
и
`image-admission-tools@sha256:20840aa452ad3b9b15601de041a6534d8c5ba9877e67e5f3e93d880b6d1a6fd3`.
Пока сверены index digests с live refs, это не полная проверка/публикация
сохранённых OCI. Их нельзя выдавать за новые build receipts: нужен отдельный
типизированный retained-artifact path с исходными exact digests и без
подмены build provenance либо domain admission.

Дополнительное live read-only чтение `session-archive` показало worker pin
`sha256:ca8eacdc22f2dc76a8080460659f2dd01afc9cdb61866bf7a123fae003460b49`,
отличный от нового controller OCI. До cutover требуется проверить и согласованно
материализовать controller/worker pin, связанный admission и idle/readback
границы; простой patch app image этого не доказывает. Default role pin
runtime-controller не изменён и не заменяется image контроллера.

Следующие обязательные работы: retained artifact publication, точный bootstrap
и activation pull gateway, worker/admission binding, frontend TLS cutover,
Go rollback/полная replica матрица, certificate/OIDC migration и финальные
review/owner gate с реальным rollout/browser readback. Main и live PodSpec
остаются исходными; частичные успехи не закрывают этапы общего checklist.

## Сохранённые служебные артефакты: отдельный тип доказательства

На exact `49cc03949cbb160682eef7410c672fa8435d22ce`:

- PASS: 45 Node tests, 30 Python tests, ShellCheck и `git diff --check`.
- PASS PLAN ONLY: оба сохранённых OCI полностью проверены через
  `stable-image-publication.mjs retained-plan`, с текущим Kubernetes
  Deployment → ReplicaSet → Ready Pod lineage и фактическим `imageID`.
  Сверены все blobs, descriptor graph, archive integrity, user/config и
  исходный cache tag; неподвязанные blobs не принимаются.
- План `image-admission` имеет hash
  `67c5a2180b8c2e49d7ce42c952c98bf082e106676685519fa1d2b19d94708c44`.
  План `image-admission-tools` имеет hash
  `9874038bdb08791982e0073967ed642cfb38419967eaad0a6c97b9ce724d7959`.
  Оба сохраняют уже записанные выше exact image digests.
- PASS LOCAL/SYNTHETIC: настоящий CLI и worker обработали оба retained
  компонента, publication/readback и отрицательные проверки serving drift,
  смешанных build/retained claims и старой schema.
- NOT RUN: registry import этих OCI и любые изменения в живой среде.

Новая private plan schema — `kodex.stable-image-publication/v2` с обязательным
`artifactProfile: built|retained`; decoder v1 и неявного fallback нет. Старые
локальные v1 планы были только preflight evidence и не применяются. Built
проверяет прежний Git build context; retained не содержит build receipt и
ссылается только на текущие разрешённые serving consumers. Эта проекция
не выдаёт новый domain admission и не меняет business/runtime pins. Реальный
consumer/repository, owner lineage и digest разрешаются из API, а не из
caller payload. Raw specs/env не попадают в evidence. До эффекта retained
план повторно проверяет Deployment bindings и свежий serving digest.

Повторное чтение обнаружило ещё необходимую транспортную часть: сохранённые
platform refs используют логическое имя registry, а текущий node runtime
настроен только на physical promoted-pull endpoint. Произвольная замена refs
во всех исторических immutable admission snapshots не выполняется. Требуется
code-first exact registry mirror с одной канонической TLS/auth конфигурацией,
без wildcard, второго credential source или default-endpoint fallback;
либо эквивалентный проверенный forward transition всех затронутых consumers.
Перезапуск/проверка нод относятся к отдельному owner-approved maintenance
stage. Ничего в node registry configuration сейчас не менялось.
Проверены Context7 и официальные
[K3s private registry docs](https://docs.k3s.io/installation/private-registry)
по exact mirror endpoint, auth/TLS, restart и отключению default fallback.

Также проверен preserved worker OCI
`sha256:ca8eacdc22f2dc76a8080460659f2dd01afc9cdb61866bf7a123fae003460b49`.
Его recorded cache input hash не совпал с повторным filesystem fingerprint
текущего main. Это не доказывает функциональную регрессию (в hash входят
метаданные файлов), но не позволяет объявить старый binary эквивалентным
текущим исходникам. Переиспользование этого OCI для controller пока не принято
как доказанное решение; controller/worker/admission coupling остаётся
открытым до проверки настоящего source closure либо согласованного transition.

## Архиватор: исправление опасного старта второй реплики

На exact `f10fe2e21e4e36f9dd42cf12f53a6a6d6710c93e`:

- PASS: Go unit всех пакетов session-archive, `go test -race` controller/app,
  `go vet ./...`; используется Go1.26.6.
- PASS: 47 Node tests, 30 Python OCI tests, полный
  `scripts/tests/web-only-release-test.sh`, `git diff --check`.
  ShellCheck завершился с кодом0; renderer сохраняет прежние информационные
  SC2016 для yq expressions, эти строки не менялись.
- PASS READ-ONLY LIVE: новый SQL oracle прочитал `openArchiveTasks=0`,
  `transitionalStorage=0`; managed archive Job/Pod отсутствовали. Это
  наблюдение до cutover, а не доказательство будущего maintenance окна.
- PASS LOCAL OCI: исправленный session-archive собран из этого source,
  digest `sha256:3d8e397f0d7029d65c72cf38d7bc16db304705b6dfa0053a7acbede3c5fa5f2c`.
  Он заменяет прежний локальный controller9b8a в плане поставки, не live pin.
  Первый вызов build CLI с неверным `--output` был отклонён до эффекта;
  повтор с документированным `--output-directory` успешно завершился.
- PASS PLAN ONLY: реальный publication preflight проверил новый OCI/context,
  hash `2404404d25373579baf9568d20d93092bf15695a79efeb82e93cca362d813265`.
- NOT RUN: registry import/node pull, новый RBAC и controller в кластере,
  actual restart/retry с worker Pods. Запуски настоящих агентов не выполнялись.

Устранён найденный дефект: startup больше не выполняет managed-label
DeleteCollection по всему namespace. RBAC не даёт `deletecollection`.
Retry адресно проверяет immutable input предыдущих attempts (максимум5),
полный owner tuple, deterministic name и exact Job identity. До удаления
подтверждается свежая owner lease; UID/RV preconditions и foreground deletion
с ожиданием Pods защищают от replacement и преждевременного запуска writer.
Собственный cleanup не удаляет неизвестный результат Create по одному имени.
Тесты охватывают две параллельные сессии/реплики, чужой input, потерю renew,
заменённый UID, оставшийся Pod и потерянный ответ Create с fenced retry.

Модель cutover теперь связывает `SESSION_ARCHIVE_WORKER_IMAGE` с тем же OCI,
что controller, и отвергает отсутствующий/двойной/динамический исходный pin.
Apply дополнительно проверяет archive tasks/storage/managed Job/Pod. Сам
переход от старого глобального startup cleanup всё ещё требует bounded
quiesce старого controller и согласованной установки RBAC; одна idle-проверка
не объявляется атомарной блокировкой новых claims. Исполняемый переход и
rollback остаются частью незавершённого этапа4. Пункты общего плана не закрыты.
Проверены Context7 Kubernetes foreground GC и ownerReference docs.

## Исполняемый quiesce и смена прав архиватора

На exact `caad97a4ede8dcfbe217e16e485ac46e4dcc2866`:

- PASS: 53 Node tests, 30 Python OCI tests, полный web-only release render
  и `git diff --check`. Go-код относительно проверенного `f10fe2e2` не менялся.
- PASS PLAN ONLY: реальный `stable-go-release.mjs plan` прочитал live
  Deployment/Role/RoleBinding и построил exact переход для уже собранного
  `session-archive@sha256:3d8e397f0d7029d65c72cf38d7bc16db304705b6dfa0053a7acbede3c5fa5f2c`.
  Plan hash `5df849f13120822c1250b7b62445c953e66dd321b3e0bafa57e291c42eb70e51`.
- OBSERVED READ-ONLY: `inspect` подтвердил `SOURCE_UNCHANGED`, `complete=false`.
  Replica count, Role и работающие Pod не менялись. Этот результат не является
  PASS rollout либо приёмкой двух реплик.
- PASS SYNTHETIC: настоящий CLI выполнил порядок quiesce → zero/Pod join →
  Role CAS → compiled2; новые права не применялись до исчезновения старых Pod.
  Потерянные quiesce/Role ответы не переотправлялись; inspect различил
  `QUIESCED` и `QUIESCED_ROLE_READY`, оба с `complete=false`.
- PASS NEGATIVE: drift RoleBinding/UID, чужие subject/namespace, wildcard
  permissions, foreign serving Pod и старая plan schema закрыто отклоняются.
- NOT RUN: эти phases в живом кластере; publication/node pull; публичные TLS/SSO.

Go plan теперь `kodex.stable-go-plan/v2`, без decoder прежней schema.
Desired Role читается из единственного repo-owned `runtime/rbac.yaml`;
в private plan сохраняются только commitments, а не сырой Deployment/env.
Binding не меняется: controller ServiceAccount остаётся прежним, worker не
получает Kubernetes полномочий. Подтверждение exact plan не заменяет review
и отдельное owner решение. Journal fsync предшествует каждому patch.

Прежний открытый quiesce/RBAC путь реализован и проверен локально; live
проверка, полный Go rollback, frontend/service TLS cutover, native registry
mirror/activation, replica matrix и certificate/OIDC остаются в работе.
Новый план не применён и перед поставкой потребует свежего drift/idle чтения.

## Native registry mirror: границы перехода

Источник — действующая конфигурация каждой точной Kodex node/container и
server-owned Kubernetes node/cluster UID, не credential из пользовательского
payload. Добавляется один exact logical mirror к уже настроенному physical
HTTPS endpoint. Его auth/TLS остаются единственным источником; копирования
credential под logical hostname или переписывания immutable admission refs нет.
K3s default endpoint fallback для настроенных mirrors отключается в native
config; unlisted registries не объявляются air-gapped. Прочие настройки,
сертификаты, private keys и registry roles не меняются.

| Фаза | Проверка и граница |
| --- | --- |
| plan | Только чтение node/container identity, private config hashes и TLS file hashes; plan без auth/raw YAML |
| apply preflight | Exact approved plan после review/owner gate, clean executor SHA, все nodes Ready, полный idle graph; сначала проверить все nodes |
| stage files | Exact before hashes/owner/mode, node-local private backup и intent; atomic rename двух известных files, без credential на host/argv/env |
| restart | Один exact Docker container за раз; следующие действия только после RuntimeReady/NetworkReady, API и свежего Ready heartbeat |
| lost response / partial write | Не повторять install/restart по предположению; inspect/readback и явный rollback по intent |
| rollback | До первого изменения проверить все exact before/after states и node-local backups; чужой drift не перезаписывать; последовательный restart |
| readback | Native config и generated exact mirror, отсутствие logical fallback/TLS skip; это ещё не actual fresh registry pull |

Ни один этап не меняет соседний кластер, DNS/firewall, kubeconfig, Kubernetes
Secrets или registry permission profile. Node restart и rollback — отдельное
owner-approved maintenance окно, не побочный эффект plan.

## Native registry mirror: реализация и preflight

На exact `334418d643cdf46f09b2378cdd00788a18ea5b67`:

- PASS: 61 Node tests без skipped; в их числе настоящий CLI с synthetic
  Docker/Kubernetes adapters и настоящий shell worker в disposable контейнере
  из уже имеющегося exact node image. K3s/кластер в fixture не запускается:
  только shell, `network=none`, read-only rootfs и отдельный временный bind.
  Временные контейнеры завершены и удалены; реальные node mounts не доступны.
- PASS: 30 Python OCI tests, полный web-only release render, ShellCheck нового
  worker без замечаний, `node --check` и `git diff --check`.
- PASS PLAN ONLY: настоящий read-only CLI проверил обе текущие Kodex nodes,
  Docker labels/UID/image, private file identity, canonical TLS/auth и source
  SHA. Plan hash
  `a5d24e11ef69181cfa4260b2f76a794a291810f35d436ef981d86f88161831d3`.
- PASS SYNTHETIC: последовательные install/restart/readback/rollback двух
  нод; wrong approval и file drift не меняют файлов; потерянный первый restart
  не запускает второй, rollback пропускает ещё не затронутую ноду.
- PASS COMPONENT: действительный shell worker выполнил install/restore,
  отказ при повторной установке/чужом drift и восстановление после оборванного
  payload до effects. Credential sentinels не попали в trace/output/journal.
- NOT RUN: изменения файлов/backup/restart на живых nodes, generated native
  mirror readback и fresh registry pull. Кластер и действующий main не менялись.

Публичные точки входа: `tools/release/stable-node-registry.mjs`
`plan|apply|readback|rollback`; exact context, physical host только для plan,
private plan/output и approved plan hash для apply/rollback. CLI сохраняет
только commitments; private YAML обрабатывается в памяти. Backup с original
bytes остаётся только внутри соответствующей node под root-owned0700,
intent и private0600; cert/key файлы не копируются и не вращаются.

Source guard связывает исполнение с clean exact SHA и tracked worker/model.
Монотонный журнал fsync предшествует effects. Worker использует exclusive
lock, private input pipe, before hash/owner/mode и atomic file rename.
Неизвестный ответ не повторяется автоматически. Перед каждым restart
проверяются idle graph и сохранённые promotion pins; затем ожидаются
RuntimeReady/NetworkReady, API и свежий Node Ready heartbeat. Native readback
проверяет exact physical endpoint, отсутствие logical fallback и TLS skip;
его результат явно `NATIVE_CONFIGURATION_ONLY`, `freshPull=NOT_RUN`.

Проверены Context7 K3s private-registry/agent docs. Pull gateway сохраняет
physical upstream и уже передаёт containerd `ns` query, не используя её
для выбора authority либо destination. Активация нового pull-authorizer,
publication и actual node pulls ещё требуют согласованного maintenance этапа.
Frontend TLS cutover, полный Go rollback, оставшаяся replica matrix и
certificate/OIDC также остаются незавершёнными. Чекбоксы общего плана не закрыты.

## Фронтенд: фазовая поставка compiled nginx

Переход использует один exact план: исходный Deployment/Service/три Ingress/
NetworkPolicy, существующие TLS references и текущий публичный runtime config.
UI получает новую immutable версию того же публичного config с согласованными
content revision/readiness/template pins; прежний mutable dev ConfigMap не
переписывается. Public issuer и client permissions на этой фазе не меняются.

| Фаза | Состояние и проверка |
| --- | --- |
| plan | Canonical nginx template, проверка server defaults через dry-run без persist; exact SSO middleware/TLS/peer selectors; public config schema без secrets |
| create dependencies | Только новый content-addressed runtime ConfigMap и exact mTLS ServersTransport; intent до запроса, чужой collision закрыто отклоняется |
| quiesce | CAS frontend до0, observed generation и отсутствие всех owned Pod; API direct route остаётся защищённым |
| activate | Exact ingress peer только8443; compiled2, без host mounts/dev env; readiness nginx подтверждает загрузку TLS |
| route switch | Service8443 и три точных Ingress backends; существующие SSO middleware/hosts/TLS не меняются; API/WS идут через nginx с проверкой API certificate |
| partial / unknown | Никакого автоматического повторения create/patch; наблюдаемая before/quiesced/after классификация, не ложный PASS |
| rollback | До effects проверить все UID/spec/annotation/reference pins; остановить только изменённый frontend, восстановить узкие обратные patches, удалить только собственные новые ресурсы по UID/RV |

Это короткое согласованное maintenance окно, не zero-downtime обещание.
Обратные patches содержат только изменённые поля, не целые Deployment/env.
Неизвестные дополнительные frontend env закрыто отклоняются до сохранения
recovery material; допустимы только существующие публичные dev knobs.

## Проверка compiled frontend и уточнение inventory

На exact `8ec0c0196355344bb3c236029450f6a8edfd16e5`:

- PASS: 73 Node-проверки поставки, включая реальный `nginx -t` из собранного
  OCI, без пропусков; 30 Python-проверок OCI и полный web-only render.
- PASS: compiled frontend собран из
  `ae33ac92ba71d9cb775b41e0a8706d6bbd193cb4`, manifest
  `sha256:f70b5d94bc852a35ab8161107e9ead827a1be17742f32e0d4b3f3e2ceaa0b5e5`.
  Реальный read-only publication preflight проверил OCI graph и Git build
  context; план публикации имеет commitment
  `55c2598449b63868d249be65cbd52d0173d468eb72fd0b0d8f39692fbcb2ca2a`.
- PASS: реальный frontend plan с Kubernetes server-side dry-run, без persist,
  commitment `4d76ad128ac7a7498ab407cc6b62e4fdaa4c94223f322cd25fc703881100eb98`.
  Проверены defaults, публичные TLS certificates/CA, текущие SSO middleware,
  exact ingress peers и согласованная immutable runtime revision.
- PASS: synthetic plan/apply/readback/rollback сохраняет исходный профиль;
  неизвестный create не повторяется, подмена UID нового ресурса запрещает
  rollback до первого write. Это не применение в рабочем кластере.
- PASS: фактический nginx запускает проверку конфигурации под UID101 с
  read-only root, без сети и capabilities, с одноразовыми тестовыми ключами.
  Первый запуск оснастки использовал config digest вместо Docker image ID;
  следующий обнаружил недостающий включаемый `public-assets.conf` в fixture.
  Оба исхода были FAIL; после исправления оснастки та же собранная конфигурация
  прошла проверку без изменения или ослабления nginx.
- Изменение nginx закрывает запись query/callback codes стандартным access
  log и снимает случайный default1MiB для artifact streaming; backend
  сохраняет авторитетные ограничения. Проверена официальная документация
  [NGINX command-line parameters](https://nginx.org/en/docs/switches.html)
  через Context7. Deprecated `rootCAsSecrets` заменён современным `rootCAs`;
  обе фактические Traefik CRD проверены read-only и поддерживают этот формат.

На exact `f2d9d5b1ae799483915738e2b4d7a77de11d0852` восемь inventory unit
tests PASS. Включены `kube-system` и `cert-manager`, которые не должны выпадать
из требования двух реплик совместимых controllers. Exact согласованные
`local-path-provisioner` и Grafana SQLite имеют отдельную категорию
`SINGLETON`, не ошибочный target2. Исключение не переносится на одноимённый
ресурс другой namespace/kind. Полная эксплуатационная матрица и бюджет
ресурсов ещё не завершены, checkbox2 остаётся открытым.

NOT RUN: frontend live apply/rollback, registry publication/node pull,
публичные TLS/SSO и browser новой поставки. Локальный Docker load служил только
одноразовой проверке nginx, не загрузке образа в Kubernetes nodes. Существующий
main и все hot Deployment сохранены; реальные AI/STT/device-code не запускались.

## Восстановление Go-поставки без возврата опасных прав

`stable-go-release.mjs` использует единственную schema `kodex.stable-go-plan/v3`,
без декодера прежних планов. План связывает clean executor SHA, cluster/app
namespace UID, каждый Deployment UID, before/after spec/labels, PVC/PV
commitments, узкие inverse operations и закрытый вид восстановления. Apply и
rollback требуют exact подтверждение плана после проектных review/owner gates.
Rollback дополнительно читает private apply journal с совпавшим intent и
сверяет сохранённые promoted pins по свежему owner idle read.

| Переход | Проверка и результат |
| --- | --- |
| plan | Удаляемые Go build env имеют закрытый публичный формат; source spool init совпадает с repo-owned renderer, неизвестные values не сохраняются |
| normal compiled runtime | Source workingDir и build HOME удаляются: в distroless нет `/workspace` и Go cache; init получает nonroot/read-only/drop ALL |
| rollback preflight | Весь набор UID/spec/labels/PVC/pins и archive RoleBinding сверяется до первого write; чужой drift закрывает весь запрос |
| ordinary service | CAS quiesce, join всех собственных Pods; inverse в памяти, исходные replicas/labels/spec; actual serving/readiness и повторный итоговый readback |
| archive after partial/full cutover | CAS quiesce/join; Role только сохраняется или сужается до repo-owned safe правил; исходный source profile возвращается строго с replicas0 |
| unknown response | Нет автоматической повторной отправки; fresh rollback наблюдает source/compiled/quiesced tuple; уже восстановленный source не патчится снова |
| archive forward recovery | Новый план из safe paused source и текущего narrowed Role; после отдельного допуска — тот же apply, исправленный compiled OCI, две готовые реплики |
| завершение recovery | `releaseComplete=false`, browser `NOT_RUN`; остановленный archive явно отражается `archivePaused=true`, не выдаётся за готовность |

Архиватор не возвращается к прежнему startup `deletecollection` и broad Role.
При его аварийном восстановлении архивирование временно недоступно, пока не
выполнен новый forward plan; это не альтернативный конечный профиль цели.
Исходные данные, provider credentials и существующие promoted pins не меняются.
Публичного API для этих операций нет: actor — оператор разрешённого кластера
после exact owner gate, transport — scoped Kubernetes CAS. Это deploy recovery,
не изменение доменных агрегатов; новые domain events не создаются, результат
читается через actual Kubernetes/owner readback и private intent journal.
