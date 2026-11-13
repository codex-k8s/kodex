---
id: RUN-MC-002
title: Чистое развертывание Kodex
type: runbook
status: approved
owner: sre
version: 2.4.15
updated: 2026-11-13
---

# Чистое развертывание Kodex

Runbook предназначен для новой установки без переносимых данных. Полный reset
bare-metal узла уничтожает все Kubernetes workloads, PVC и containerd state.
Для production reset обязательно отдельное решение владельца.

Опциональные внешние OIDC и TLS termination, независимые режимы и локальный
CI/CD delivery описаны в [отдельном профиле](portable-install.md). Прежние
режимы этого runbook сохраняют defaults и собственное управление TLS/IdP.

Перед допуском новой установки выполнить `make test-install-contract` на exact
candidate. Узкий `make test-install-workload-startup` исполняет настоящий startup
helper с synthetic manifests, закрытыми stubs и пустым PATH: startup consumers,
control-plane/provider, materializer и registry/BuildKit gates проверяются вместе
с остановкой после каждого обязательного отказа. К кластеру, сети и live credentials
эта оснастка не обращается. PASS оснастки не заменяет fresh install, immutable
images, bootstrap receipts и browser/runtime приёмку по #1977.

## 1. Два поддержанных сценария

### Пустой bare-metal сервер

`KODEX_INSTALL_MODE=bare-metal` устанавливает и настраивает:

1. системные пакеты и firewall;
2. k3s с encryption at rest;
3. cert-manager и trust-manager;
4. Keycloak;
5. выбранные management surfaces: Grafana/Prometheus/Alertmanager и, только
   при явном профиле `all`, Headlamp;
6. локальный OCI registry;
7. GitHub Actions Runner Controller и rootless BuildKit sidecar;
8. PostgreSQL, NATS и control-plane deployables в `kodex-system`, а runtime
   workloads и session-archive workers в `kodex-runtime`.

S3-compatible storage является обязательной зависимостью web-first MVP.
Production использует заранее созданные внешний HTTPS endpoint и bucket;
встроенный object storage не разворачивается.

При установке компонента `secrets` установщик требует S3 env и материализует
Secret `kodex-external-s3` в `kodex-system` с keys `endpoint`, `region`,
`bucket`, `access-key`, `secret-key` и credential-only Secret с тем же именем
и keys `access-key`, `secret-key` в `kodex-runtime`; значения не входят в Git
или render. При установке только компонента `platform` оператор заранее создаёт
оба exact Secret, а установщик выполняет закрытый readback. Control-plane
получает metadata через `secretKeyRef`, credentials через read-only files и
остаётся not ready, если Secret, bucket либо authenticated `HeadBucket`
недоступны. Session-archive worker получает только credential projection в
своём namespace. Bucket создаётся оператором до запуска platform.

Служебный bootstrap registry принадлежит namespace `kodex-infra`; application
registry и workloads Kodex принадлежат `kodex-system`. Смешивать эти границы
или возвращать исторические release namespaces запрещено.

### Готовый Kubernetes

Для новой пустой БД authority renderer получает явный
`--authority-registry-configuration <json>` с содержимым
`{"schema":"kodex.authority-registry/v1","source_revision":1}`.
Историческая ревизия каталога в исходниках не является первой ревизией новой
установки: publisher закрыто отклоняет её до публикации. Этот input сохраняется
вместе с release lock и применяется при каждом воспроизведении render; после
первой публикации ревизия меняется только штатным протоколом ротации.
Если bootstrap Jobs уже завершились, а publisher отказал до первого durable
effect, используется проверяемое
[продолжение initial authority bootstrap](portable-install.md#registry-authority-при-первой-установке-и-после-частичного-bootstrap).
Повторный запуск миграций или удаление нового состояния для такого исправления
не требуется.

`KODEX_INSTALL_MODE=existing-kubernetes` не обновляет ОС и не устанавливает
k3s. Оператор передаёт exact kubeconfig/context и выбирает компоненты. Для
установки только приложения инфраструктурные зависимости должны уже
удовлетворять тем же контрактам DNS, TLS, OIDC, HTTPS OCI registry и runner
labels `kodex-build`/`kodex-deploy`. Exact registry write identity передаётся
через `KODEX_RELEASE_REGISTRY_USERNAME` и
`KODEX_RELEASE_REGISTRY_PASSWORD`; установщик не создаёт пользователя во
внешнем registry и не ослабляет его TLS.

Admin host, с которого запускается профиль `existing-kubernetes`, должен иметь
`curl`, `dig` из пакета `dnsutils`, `python3` и GNU `timeout`: они выполняют
fail-closed DNS/HTTP-01 preflight до создания публичного `Certificate`.
Bare-metal installer устанавливает `dnsutils` и остальные необходимые утилиты
сам и проверяет их наличие при readback.

Для release build ingress задаётся тремя независимыми параметрами:
`KODEX_INGRESS_NAMESPACE`, `KODEX_INGRESS_POD_NAME` и
`KODEX_INGRESS_SERVICE_NAME`. Service обязан иметь единственный HTTPS port
`443`, выбирать Pod с label `app.kubernetes.io/name=<pod-name>` и иметь готовый
EndpointSlice. Envoy принимает CONNECT только для точного публичного registry
authority, но направляет raw TLS tunnel на этот внутренний Service. Поэтому
клиент продолжает проверять публичные hostname и сертификат, а single-node
установка не зависит от pod-to-public-node hairpin NAT.

Текущий релиз изолируется в namespace `kodex-system`. Произвольное имя
namespace пока закрыто отклоняется, чтобы manifests и security policy не
создавали ложную изоляцию.

После полного завершения платформенного bootstrap и перед открытием Control Center
установить отдельную dependency
[proxy session store](../operations/proxy-session-store-1383.md), затем выполнить
`infra/management-surfaces/bootstrap.sh --mode apply-surfaces` с выбранным
профилем как минимум `control-center`. Одних PWA/API Deployment и Ingress
недостаточно: маршруты ссылаются на управляемые OAuth2 Proxy middleware.
Для локальной чистой установки используется explicit UID-bound fresh profile
session store; смена environment labels для обхода staging gate запрещена.
OIDC connect address содержит порт **Service** (у bundled SSO —443),
`--oidc-target-port` — порт **Pod** (8443). SNI остаётся публичным hostname,
проверка CA обязательна. Portable release использует OAuth2 chain без
hot-reload retry; readback допускает только эти две канонические цепочки.
API portable release идёт через TLS Service `staff-control-center` и его
проверяемый nginx upstream к gateway; hot-reload использует непосредственно TLS
Service `control-api-gateway`. В обоих случаях `/api/v1` сохраняет точный auth
middleware без browser error redirect, priority200 и порт `https`. Произвольный
backend либо plaintext не допускаются.
Initial-resume выполняется до установки management Deployment: его guard
проверяет точный набор платформенных компонентов. Runtime должен быть пустым;
допускается только первоначальный `system-assistant-warm` в Pending с exact
runner images, полным набором waiting statuses, без запусков/restarts контейнеров
и при нулевых `runs`/`session_turns`. Этот Pod не удаляется и не считается готовым:
публикация release artifacts ещё должна обеспечить его первый pull/start.
Работающий warm Pod, бизнес-история, неизвестный Pod или неполный status
закрывают initial-resume; это не maintenance/Stop/retry действующей сессии.

### Ограничение Go-памяти проверки образа

Если фактический scan завершился `OOMKilled` при контейнерном лимите 2 GiB,
canonical renderer принимает `--image-admission-memory-profile bounded-2g`.
Он включает существующий component `image-admission-memory-budget`: только
ключ `scan-go-memory-limit-mib=768` в ConfigMap `kodex-image-admission`.
Wrapper передаёт `GOMEMLIMIT=768MiB` Syft/Grype и проверяет конечный cgroup v2
limit с резервом не меньше25%. Профиль `default` сохраняет прежнее поведение.
Резерв выбран после повторного OOM при1536MiB; реальная проверка768MiB также
завершилась OOM на полном RoleImage. Это мягкий предел Go runtime, а не гарантия
отсутствия OOM. Увеличенный контейнерный бюджет описан ниже в разделе
«Память полного SBOM»; сканирование, подписи и правила допуска сохраняются.

В уже установленном окружении сначала завершить текущие admission attempts,
сохранить terminal status, получить render и адресный plan для exact ConfigMap,
проверить UID/resourceVersion, применить и перечитать только этот ресурс.
Новую попытку создаёт штатная `REQUEST_BUILD`; старый FAILED не переписывается.
Нужны новые SBOM/vulnerability/signature/receipt и promotion до запуска образа.
Компонент и правила merge проверены по официальной документации Kustomize
через Context7; `make test-scan-memory-budget` исполняет wrapper и Kustomize.

### Подготовка полного runner без изменения кластера

`tools/dev/build-local-runner.sh --build-only --image-profile full
--source-root <clean-exact-worktree> --state-directory <private-directory>`
собирает OCI и проверяет exact source/binary provenance тем же verifier, что
обычная доставка. Режим требует существующий builder `kodex-local-dev`,
не пересоздаёт builder/network, не импортирует образ в Kubernetes и не записывает
`agent-runner-image` как установленный pin. Manifest и provenance остаются в
private cache; последующий import использует прежние exact-manifest guards.
Полный runner ещё не является допущенным RoleImage: supply-chain admission,
материализация среды, pod readback и реальный workspace path проверяются отдельно.
Если runtime использует provider sandbox projection, сначала согласовать её
с новой image policy при остановленных новых исполнениях. Затем
`tools/release/default-runner-transition.mjs` проверяет live source/projection,
binding и controller через общий `provider_sandbox_profile.py --check-live`.
Переход меняет default двух приложений с UID/resourceVersion guards; admission
остаётся закрытым до завершения rollout. Успешный readback не обновляет ранее
опубликованные среды проектов: для них нужны новые RoleImage и явная привязка.

Отдельное исправление authority можно собрать точным source SHA через
`tools/release/build-stable-local.sh --component internal-rpc-authority
--source-sha <sha> --output-directory <new-private-directory>`. Штатный
`stable-image-publication.mjs plan/apply/readback` проверяет OCI, nonroot
`65532:65532`, verifier entrypoint и exact registry boundary. Это публикация
образа; rollout существующих workload и readback остаются отдельными шагами.

Вспомогательные authority/admission/builder образы готовятся отдельно через
`tools/dev/build-local-image-supply-chain.sh --build-only --component all
--source-root <clean-exact-worktree> --state-directory <private-directory>`.
Этот режим требует прежний builder, не вызывает Kubernetes и сохраняет
`*-oci-image` и `build-only.json` с `import=NOT_RUN`; прежние указатели установленных
образов не меняются. Полный supply-chain admission проверяется после установки.

Установщик передаёт каждому дочернему `kubectl` явные kubeconfig/context через
`tools/install/target_kubectl.py` в приватном временном PATH. Он не вызывает
`config use-context` и не изменяет конфигурацию хоста. Для отдельных repo helpers
тот же launcher можно разместить как `kubectl` в приватном PATH и задать
`KODEX_TARGET_KUBECTL`, `KODEX_TARGET_KUBECONFIG`, `KODEX_TARGET_CONTEXT`.
Подмена target flags и изменение host config закрыто отклоняются; обычные операции
с ресурсами сохраняют собственные ownership guards соответствующего helper.

Bootstrap identity, NATS и trust-manager создаёт namespace Kodex через
`tools/install/ensure_namespace.py`: метка владельца устанавливается в том же
create, существующий чужой namespace не присваивается установке. Если ранний
bootstrap этой же новой установки уже создал namespace без метки, оператор
проверяет creation receipt и передаёт его exact UID через `--created-uid`.
Helper допускает только четыре namespace платформы, проверяет UID/resourceVersion
перед patch и UID после записи; прочие labels сохраняются. Обычный повторный
запуск не требует этого параметра и не изменяет уже подтверждённое владение.
Это исправление metadata новой установки, а не импорт старых данных.

### Локальный hot-reload

Для immutable candidate на локальном кластере S3 можно подготовить из того же
`deploy/k8s/base/local-object-storage`, закрепив SeaweedFS/AWS CLI по
`tools/dev/components.lock.json`. `tools/install/local_object_storage_tls.py storage`
принимает этот render, публичную CA и exact ClusterIssuer; создаёт Certificate
для `seaweedfs-s3.kodex-system.svc.cluster.local`, включает штатный TLS на 8333
без второго HTTP listener и переводит probes/bucket bootstrap на проверяемый HTTPS.
Credentials создаются заново в private state и материализуются как immutable
`kodex-external-s3`: полный набор metadata/credentials/S3 identity в `kodex-system`,
только `access-key`/`secret-key` в `kodex-runtime`. Перед первым apply проверяется
отсутствие всех точных объектов, cluster/namespace UID и сохраняется hash render.
Повторный apply использует те же credentials; их молчаливая регенерация запрещена.

Фаза `consumers` того же helper применяется к полному release render до его
закрепления: control-plane, retention, backup-controller и session-archive получают
публичную CA; `SESSION_ARCHIVE_OBJECT_STORAGE_CA_CONFIG_MAP` доставляет её каждому
временному archive worker штатным controller. Ни один `ALLOW_INSECURE_LOCAL` не
включается. Сценарий: bootstrap Job создаёт versioned buckets и проверяет точные
write/read/delete receipts → control-plane владеет storage metadata и task/grant →
session-archive создаёт exact worker в `kodex-runtime` → worker обращается к S3
с той же CA/hostname и возвращает owner-bound receipt. NetworkPolicy связывает
оба namespace с точными pod selectors. TLS/readiness не заменяют приёмку native
checkpoint, workspace PVC и восстановления; эти проверки выполняются после
установки полного RoleImage.

Проверены Context7 `/seaweedfs/seaweedfs` и исходники
[SeaweedFS 4.41](https://github.com/seaweedfs/seaweedfs/blob/4.41/weed/command/s3.go):
сертификат и ключ при `s3.port.https=0` обслуживают TLS на основном S3 port.
Проверки преобразования: `python3 -m unittest discover -s tools/install
-p test_local_object_storage_tls.py`; отрицательные случаи включают отсутствующий
consumer, повторный ресурс и некорректную CA.

`dev.sh up` добавляет к web-only render SeaweedFS release 4.41 в
`kodex-system`: один StatefulSet с PVC, внутренний S3 Service на TCP/8333,
deny-by-default policy и bucket bootstrap Job. `tools/dev/deploy-local.sh`
создаёт immutable `kodex-external-s3` из случайных credential files и никогда
не печатает значения. Job создаёт `kodex-artifacts` до migrations и запуска
control-plane; повторный apply выполняет exact Secret/job/readiness readback.
Завершённый `seaweedfs-bucket-bootstrap` сохраняется без TTL: полный readback
установки требует его фактическое состояние, а общий receipt миграций этот
Job не заменяет. Срок самого исполнения остаётся ограничен 900 секундами;
завершённый Job не выполняет повторных S3-действий. Удаление Job/PVC при полной
очистке относится к отдельному адресному lifecycle установки.

Если Job прежнего render уже удалён TTL-controller, повторный readback закрыто
отклоняется. Сначала подтвердить точные cluster/namespace/PVC и наличие всех
четырёх Kodex-owned bucket с включённым versioning через read-only проверку;
не создавать пустые bucket вместо потерянных данных. Затем допустимо адресно
восстановить **только** этот bootstrap Job из текущего TLS render, сохранив
остальные ресурсы и credentials. Его ограниченная проверка создаёт собственный
`_kodex-bootstrap/<pod-name>-write-readback.txt`, сверяет полученную версию и
удаляет именно её; прежние объекты, версии, native checkpoints и архивы не
удаляются. После Complete повторный apply/readback должен сохранять Job UID
и не запускать Pod заново. Ручное удаление завершённого Job снова лишит
readback подтверждения; оно не является способом очистки новых сессий.

Перед первым `dev.sh up` оператор передаёт утверждённый каталог OpenCode
и приватный regular файл API key (0600, текущий владелец, без symlink/hardlink):

```bash
KODEX_MODEL_PROVIDER_CONFIGURATION_FILE=/private/providers.json \
KODEX_MODEL_PROVIDER_API_KEY_FILE=/private/provider-api-key \
./dev.sh up --kubeconfig /private/kubeconfig --context approved-local
```

Файл имеет единственный формат `kodex.model-provider-catalog/v1` по
`contracts/model-provider/v1/catalog.schema.json`: `managed_by=git`, одна
`source_revision`, явные `default_provider`/`default_model` и от 1 до 16
`providers`. Каждый элемент — закрытый `kodex.model-provider/v1` с той же
source revision. Default provider — точный `opencode-<provider_id>`, а не имя
модели. Одинаковый alias у разных providers допустим; общий hostname требует
одинаковых address pins. Отдельный старый descriptor вместо каталога отвергается.
Флаг `--model-provider-default-model` и одноимённый env, если заданы, служат только
проверкой совпадения с каталогом и не переопределяют его. Первичный API key
принадлежит исключительно default provider; остальные accounts создаются через
штатные UI/API, с собственными credentials и проверкой доступности моделей.
Изменение каталога требует drain и остановки старых consumers перед применением
совместимого immutable render; hot reload маршрутов не поддерживается.

Локальный bootstrap использует тот же `tools/install/model-provider.py`, что
`install.sh`: проверяет exact provider/model/endpoint, создаёт typed credential
и отдельный секретный cache namespace key. Render доставляет immutable
конфигурацию control-plane, secret-broker и egress-gateway с проверяемым digest.
Codex CLI, ChatGPT auth snapshots и автоматический импорт прежних аккаунтов
не поддерживаются. Дополнительные аккаунты создаются через штатный UI/API.
Повторный запуск с тем же API key сохраняет namespace; другая конфигурация или
ключ требуют явного управления credential, а не молчаливой перезаписи bootstrap.
В `trusted-cluster` допустим отложенный ввод credential; это не означает
готовность системного помощника. `protected` требует файл ключа до изменений.

Локальные `credentials.env` и `access-identities.env` содержат только закрытый
набор строк `KEY="value"` с JSON-экранированием внутри значения. Их создаёт
`tools/dev/local_credentials.py`; shell подключает только код loader, а не файл
данных. Reader проверяет владельца, приватный каталог, regular файл без
symlink/hardlink, размер, полный набор и уникальность ключей. Повторное применение
сверяет переданные owner credentials без перезаписи. Старый shell-формат не
исполняется и не импортируется: для чистой установки нужен новый приватный state.
Проверки без credentials и кластера: `python3 -m unittest discover -s tools/dev
-p test_local_credentials.py`, `bash scripts/tests/full-local-e2e-entrypoint-test.sh`
и `bash scripts/tests/remote-disposable-cluster-contract-test.sh`. Через Context7
`/python/cpython` проверены `os.open`/`dir_fd`/`O_NOFOLLOW`, `fstat` и публикация
`os.link` в [официальной документации](https://docs.python.org/3/library/os.html).

`dev.sh down` содержит удаление фиксированных application namespaces, включая
`identity`, и не доказывает их исключительное владение. В разделяемом кластере
эту команду не применять: сначала нужен адресный inventory/plan с UID и
доказательством принадлежности каждого ресурса. Разрешение очистить Kodex не
разрешает удалять общий IdP, чужие workloads или данные.

Для согласованной полной очистки локального k3d доступен адресный путь
`installation_inventory.py → installation_cleanup.py → installation_apply.py`
из `tools/dev`. Он не заменяет разрешение владельца на уничтожение данных.
До применения требуется committed source, exact kubeconfig/context, cluster UID
из `kube-system`, node UID и приватный каталог0700 для inventory/plan/journal.
Публичные fixtures используют только synthetic identities; фактический inventory
и журнал не публикуются в Issue/PR.

Если инфраструктурный namespace нужно сохранить, у plan CLI есть повторяемый
`--preserve-namespace <exact-name>`. Имя должно присутствовать в полном inventory;
дубли, неизвестные имена и пути отклоняются. Это только сужение области удаления:
объекты, PVC и Helm-ресурсы выбранного namespace не становятся delete candidates.
План закрепляет отсортированный `preservedNamespaces` своим digest. Apply и
повторная сверка потребляют именно этот список, без отдельного override.
Полный inventory не фильтруется: сохранённые consumers продолжают блокировать
удаление общих Secrets, Services и других зависимостей. Unknown ownership и
чужие children не обходятся этой опцией. Без опции прежний план и digest не меняются.

Inventory вызывается с явными `--kubectl`, `--kubeconfig`, `--context`,
`--with-k3d-storage` и `--output`. Он читает metadata всех API ресурсов без
Secret data/last-applied; для закрытых типов извлекает только ссылки на
зависимости, состояния Pod и image IDs. Для каждого local-path PV проверяются
claim UID, exact каталог, affinity, node UID, k3d container ID, device/inode и
отсутствие symlink. Неизвестное происхождение, неполный discovery, чужой consumer,
чужой дочерний объект или пересечение foreign hostPath запрещают зависимую очистку.

Plan принимает `--inventory`, SHA256 фактических bytes через
`--inventory-sha256`, `--context`, `--cluster-uid`, повторяемый `--node-uid`
и `--output`. Он сохраняет ownership proofs и source SHA; `OWNERSHIP_CLASSIFIED`
означает классификацию, а не выполненную очистку. Имена namespace сами по себе
не доказывают принадлежность, third-party CRD и общие cluster capabilities
сохраняются.

Apply получает те же client/config/context, `--inventory`,
`--inventory-sha256`, `--plan`, `--plan-sha256`, `--journal` и одну `--phase`.
Без `--execute` выполняется только preflight. Фазы идут в порядке `admission`,
`producers`, `workloads`, `pods`, `claims`, `objects`, `namespaces`.
При DiskPressure перед `claims` доступна дополнительная фаза `storage`: после
отсутствия workload/Pod и положительного CRI join она удаляет только exact
local-path каталоги исходных PV. UID/claim/affinity, node/container ID,
canonical path, device/inode и отсутствие вложенных mount points проверяются
заново. Внутренние symlink не обходятся; корневой symlink отклоняется. PVC/PV
ещё сохраняют доказательство принадлежности, а после удаления данных проходят
обычную фазу `claims` и штатный reclaimer. Каждый erase имеет отдельный fsync intent
и readback отсутствия каталога. Эта необратимая фаза разрешена только при полном
уничтожении старых данных, а не при обновлении с сохранением сессий.
Сертификаты удаляются в `objects`: недоступный общий webhook не должен блокировать
независимую остановку workloads и освобождение диска Kodex. Общий webhook при этом
не отключается, его failure policy и finalizers не изменяются.
Каждая фаза заново проверяет target и ownership, имеет бюджет600 секунд и
ожидает авторитетного отсутствия удалённых UID. Все удаления адресные, с UID/RV
preconditions и `Orphan`; force-delete, снятие finalizers, wildcard delete и
глобальный prune отсутствуют. Перед каждым эффектом журнал fsync фиксирует intent.
Повторный запуск той же фазы с тем же журналом проверяет отсутствие/оставшееся
удаление; replacement UID не удаляется. Снятие ownerReferences допускается только
для exact parent UID с записанным intent. Остальной ownership drift закрыто
останавливает применение.

При исправлении исполнителя во время незавершённой очистки исходные plan/inventory
и журнал сохраняются. После квалификации нового committed descendant явно
передаётся `--executor-sha`; exact HEAD и отсутствие изменений кода проверяются,
новый SHA записывается в preflight. Это не меняет digest/область исходного плана
и не разрешает другие ресурсы. По умолчанию executor SHA равен source SHA плана.

Для уже осиротевшего после прерывания ingress-shim Certificate допускается только
узкий read-only reconciliation: в журнале должны быть удаления exact старых
Certificate и использующих его Ingress, совпасть namespace, имя TLS Secret и полный
закрытый набор issuer/output references. Новый объект должен иметь producer
`cert-manager-ingress-shim` и не иметь чужого ownerRef. После нового доказательства
удаление использует его новый UID/RV; один старый intent такого права не даёт.
Связанный CSR/Secret/Event проходит обычную UID/producer closure. Механизм
[ingress-shim](https://cert-manager.io/docs/usage/ingress/) проверен через
Context7 `/websites/cert-manager_io`; воспроизводимый сценарий replacement включён
в `make test-installation-cleanup`.

Перед claims проверяется завершение containers через CRI exact k3d nodes.
Перед namespace дополнительно требуется отсутствие PV и физических каталогов
всех исходных томов. Недоступный узел, зависший finalizer или reclaimer при
DiskPressure оставляет фазу незавершённой; отсутствие Pod в API не заменяет
положительный writer join. Этот путь не удаляет файлы хоста вне этих PV, image cache и внешние
S3/БД: для них нужны отдельные доказательства ownership и адресные операции.
Возврат после согласованного уничтожения данных — новая совместимая установка;
legacy backup/reader и выдуманный goose down не применяются.

Старые локальные node images очищаются отдельно через
`tools/dev/installation_images.py plan|apply`. Нужны исходный cleanup plan с
SHA256, exact client/context, приватные image plan/journal и SHA256 статического
helper, собранного из `tools/dev/installation-runtime` на чистом committed HEAD.
Build info helper должен подтверждать тот же SHA. Plan копирует проверенные
bytes в новый private task directory каждого exact k3d container и проверяет
readback хеша; Kubernetes workloads при этом не создаются.
Удаляются только references canonical components в локальной области Kodex;
чужие aliases того же digest и используемые образы сохраняются.
Promoted registry определяется по exact UID существующих immutable
`kodex-image-admission-policy-*`, а не по переданному URL. Его aliases и bare CRI
image references удаляются только при наличии ещё существующего local reference
того же target. Alias удаляется первым, local witness — последним; неизвестный
registry или consumer сохраняет всю группу. Повторное применение заново сверяет
policy UID/host/repository. Очистку образов поэтому завершают до фазы `objects`.
Apply использует
typed containerd API через локальный Unix socket, namespace `k8s.io` и
`DeleteImageRequest.Target.Digest`. Server-side precondition защищает от замены
tag между проверкой и удалением. Синхронный GC следует штатному графу ссылок
containerd; blobs, snapshots, leases и общие base images напрямую не удаляются.
После каждого удаления проверяется отсутствие reference, после узла — сохранность
невыбранных references. План имеет бюджет900 секунд; lost ACK разрешается readback.
Проверены Context7 `/containerd/containerd/v2.2.1` и source
[containerd2.2.3 image store](https://github.com/containerd/containerd/blob/v2.2.3/core/metadata/images.go):
digest проверяется внутри metadata transaction. Native API client закреплён на
`github.com/containerd/containerd/api v1.10.0`; поддержка exact node runtime
проверяется отдельно от fixtures.

Безопасная локальная проверка: `make test-installation-cleanup` (бюджет60 секунд,
synthetic API и filesystem fixtures, ожидается exit0). Она входит в
`make test-install-contract` и не доказывает фактическую очистку кластера.
Через Context7 `/websites/kubernetes_io` проверены
[Orphan deletion](https://kubernetes.io/docs/tasks/administer-cluster/use-cascading-deletion/)
и DeleteOptions UID/resourceVersion preconditions. Совместимость с конкретным
API server подтверждается readback применения, отдельно от unit fixtures.

Локальный профиль по умолчанию использует
`--management-surfaces control-center-grafana`: Grafana устанавливается, а
Headlamp, его OAuth2 Proxy, Keycloak client, Secret, Ingress, NetworkPolicy и
`cluster-admin` binding не создаются. Headlamp включается только явным
`--management-surfaces all`. Для уже существующего k3d/k3s-кластера можно
независимо выбрать владение общими capabilities:

```bash
./dev.sh up \
  --cert-manager-mode managed \
  --ingress-mode existing \
  --api-endpoint-mode existing \
  --provider-sandbox-mode disabled \
  --security-profile trusted-cluster
```

`disabled` для provider sandbox допустим только в локальном
`trusted-cluster`; release-профиль такой режим не принимает. Эти параметры не
устанавливают k3s на хосте и не изменяют уже выбранный ingress controller.

При первом `dev.sh up` локальный профиль создаёт host-only dummy-интерфейс
`kodex-api0` с адресом `10.254.254.1/32` и настраивает k3s
`advertise-address` на него. Поэтому точный Kubernetes API destination в
`NetworkPolicy` не зависит от DHCP, Wi-Fi или VPN. Публичные локальные адреса
Control Center и SSO при этом остаются на `127.0.0.1.nip.io`; loopback нельзя
использовать как API endpoint из Pod, поскольку внутри Pod он указывает на сам
Pod. При доказанном конфликте адрес можно переопределить через
`KODEX_DEV_KUBERNETES_API_ADDRESS`, выбрав другой приватный IPv4 `/32`.

## 2. Подготовка `.kodex-env`

```bash
cp .kodex-env.example .kodex-env
chmod 0600 .kodex-env
```

Файл заполняется владельцем и не коммитится. Он содержит только входные
параметры конкретной установки:

- режим, kubeconfig/context, публичный IPv4, точный непривилегированный
  `KODEX_HOST_OPERATOR_USER` с настроенным SSH-ключом и необязательный точный
  глобальный IPv6 адрес bare-metal сервера;
- DNS Control Center, SSO, выбранных management surfaces и registry;
- необязательный отдельный DNS SAN для восстановления Control Center TLS после
  внешнего ACME duplicate-certificate rate limit;
- режим публичного TLS `KODEX_PUBLIC_TLS_MODE=deferred|enabled`;
- точные comma-separated списки разрешённых IPv4/IPv6 ingress для public TLS
  preflight и bounded DNS/HTTP timeouts;
- exact connect address, TLS server name и Kubernetes selector OIDC workload;
- ACME email, ingress workload selector и имя ingress Service;
- постоянного Keycloak administrator и первого owner;
- профиль `KODEX_MANAGEMENT_SURFACES=all|control-center-grafana|control-center`;
- необязательный приватный `KODEX_KEYCLOAK_SMTP_CONFIG_FILE`;
- owner PAT и ARC PAT GitHub;
- registry write identity для existing-Kubernetes без bundled registry;
- Codex `auth.json` как base64 либо путь к файлу;
- режим внешних tracing/Sentry exporters, обязательные external S3 параметры и
  необязательный Sentry DSN.

На доверенном admin host или self-hosted runner файл можно собрать напрямую из
GitHub Variables/Secrets, предварительно передав их в environment шага:

```bash
./tools/install/write-env-file.sh --output .kodex-env
```

Секретный файл нельзя загружать в GitHub artifact. Рекомендуемое разделение:

- Variables: mode, DNS, namespace/context, ingress/OIDC selectors, ACME email;
- Secrets: Keycloak/owner initial passwords, GitHub PAT, registry password,
  OpenAI auth, Sentry DSN и обязательные S3 credentials;
- локальный material: `.kodex-material`, CA/private keys и recovery state.

Bundled MVP по умолчанию использует `KODEX_DISABLE_OBSERVABILITY=true`. Это
отключает только внешние OTel tracing и Sentry exporters: Prometheus endpoints,
`PodMonitor`, `ServiceMonitor`, `PrometheusRule`, Grafana и Alertmanager остаются
рабочими. Значение `false` допустимо только после отдельной установки совместимых
`otel-collector.observability.svc` и `sentry-relay.observability.svc` и требует
непустой `KODEX_SENTRY_DSN`; эти telemetry-компоненты не входят в bundled MVP.

Установщик принимает только `KODEX_*` assignments, запрещает shell evaluation
и требует mode `0600`. Значения никогда не печатаются.

### SMTP Keycloak

SMTP включается только при заданном абсолютном пути
`KODEX_KEYCLOAK_SMTP_CONFIG_FILE`. JSON и отдельный файл пароля должны
принадлежать запускающему пользователю и иметь mode `0600`; password нельзя
помещать в `.kodex-env`, JSON или Git. Без этого параметра Keycloak работает
без исходящей почты.

Пример безопасной структуры:

```json
{
  "version": 1,
  "host": "smtp.example.com",
  "port": 587,
  "security": "starttls",
  "from": "owner@example.com",
  "replyTo": "support@example.com",
  "authentication": {
    "mode": "password",
    "username": "owner@example.com",
    "passwordFile": "/run/user/1000/kodex-smtp-password"
  }
}
```

Установщик передаёт нормализованный realm update в Keycloak через stdin,
сверяет не секретные SMTP-поля и только факт наличия password. Значение пароля
не попадает в argv, логи, Git или readback.

`KODEX_CONTROL_TLS_RECOVERY_HOST` обычно остается пустым. Если удостоверяющий
центр временно запретил повторную выдачу для точного набора identifiers после
нескольких чистых переустановок, владелец может указать отдельное lowercase DNS
имя, заранее направленное на тот же ingress. Оно добавляется только в SAN
сертификата Control Center: публичный host, ingress и OIDC redirect не меняются.
После успешной выдачи значение нельзя удалять до согласованной плановой ротации,
поскольку изменение `dnsNames` само инициирует reissuance. Удаление TLS Secret
для ручной ротации запрещено; используется штатный lifecycle cert-manager.

`KODEX_PUBLIC_TLS_MODE=enabled` является каноническим режимом и применяется по
умолчанию. Режим `deferred` нужен только для явно согласованного окна ACME: он
удаляет принадлежащий платформе публичный `Certificate` Control Center и его
незавершенные ACME-потомки, исключает этот Certificate из preflight/apply и не
ослабляет внутренние mTLS/JWS, OIDC или TLS-проверки. До переключения обратно в
`enabled` публичный endpoint не считается готовым и browser E2E с доверенным TLS
имеет результат `NOT RUN`. HTTP fallback и `skipTLSVerify` запрещены.

Перед переключением в `enabled` владелец отдельно подтверждает допустимое окно
выдачи. `KODEX_PUBLIC_TLS_ALLOWED_IPV4_ADDRESSES` и
`KODEX_PUBLIC_TLS_ALLOWED_IPV6_ADDRESSES` содержат comma-separated exact IP без
CIDR, wildcard, пробелов и scope zone. Их объединение должно в точности
совпадать с полным A/AAAA snapshot всех SAN из exact render. Пустой IPv6-список
допустим только при отсутствии AAAA у каждого SAN. Неиспользуемые запасные IP
запрещены, чтобы старый адрес нельзя было молча оставить разрешённым.

До server-side dry-run публичного `Certificate` и повторно непосредственно
перед apply установщик:

1. проверяет, что exact `ClusterIssuer` из render имеет `Ready=True`;
2. выполняет bounded A и AAAA lookup каждого SAN;
3. закрыто отклоняет неизвестный адрес или расхождение snapshot с allowlist;
4. через каждый опубликованный IPv4/IPv6 выполняет bounded HTTP-запрос на port
   80 с exact `Host` соответствующего SAN и принимает только HTTP 1xx-4xx.

`KODEX_PUBLIC_TLS_DNS_TIMEOUT_SECONDS` и
`KODEX_PUBLIC_TLS_HTTP_TIMEOUT_SECONDS` задаются в диапазоне 1-99 секунд;
значение по умолчанию — 10. Проверка не создаёт solver, `CertificateRequest`,
`Order` или `Challenge`. Если применённый публичный `Certificate` не достигает
`Ready=True` за bounded wait, установщик удаляет только объект с exact именем и
owner-метками Kodex, затем ожидает garbage collection его ACME-потомков. Чужой
одноимённый объект закрыто останавливает cleanup. После успешного выпуска
выполняются trusted HTTPS/OIDC/browser readback.

## 3. Secret model

`tools/install/generate-material.sh` один раз создаёт `.kodex-material/`:

- installation CA и workload certificates;
- PostgreSQL bootstrap/static role passwords и DSN;
- NATS operator/account/user credentials;
- internal RPC authority roots/signers;
- registry identities и Cosign key;
- identity/OAuth2 secrets.

`.kodex-material/` не коммитится, имеет mode `0700` и является частью
зашифрованного owner backup. Повторный запуск переиспользует material и не
ротирует identity молча.

Cookie secrets трёх OAuth2 Proxy содержат ровно 32 случайных ASCII-байта.
Materialization проверяет длину до изменения Kubernetes и закрыто отклоняет
старый либо повреждённый material вместо запуска CrashLooping proxy.

`tools/install/materialize-secrets.sh` создаёт Kubernetes Secrets. Static
Secrets принадлежат installer field manager. Динамические key-delivery Secrets
создаёт и обновляет только `internal-rpc-authority-publisher`; installer создаёт
для них пустой generation `0`, но не перезаписывает опубликованное значение.

Provider credential на чистой установке материализуется как bootstrap revision
`runtime-provider-openai-default-r1` с immutable `auth.json` и `auth.sha256`.
Перед созданием metadata ConfigMap installer сверяет фактический digest, UID и
`resourceVersion`. После штатной авторизации или ротации metadata указывает на
новую immutable revision; повторный запуск installer проверяет и сохраняет эту
активную revision, не сравнивая её с изменившимся bootstrap-файлом и не
возвращая указатель на `r1`. Если пересоздание installation material удалило
metadata ConfigMap, installer восстанавливает его только по единственной
revision с точным digest актуального owner-private `auth.json`. Старый mutable
`r1`, созданный версией installer до
этого инварианта, удаляется только при подтверждённом field manager
`kodex-install` и создаётся заново. Изменение любой уже immutable revision
закрыто отклоняется: ротация credentials всегда создаёт новую revision.

Platform preflight и readback повторяют проверку immutable, digest и metadata.
Повреждённый либо рассинхронизированный provider Secret останавливает deploy до
создания runtime workloads и не переводит системного помощника в ложное
состояние готовности.

Session PVC системного помощника и обычных runtime создаются без фиксированного
имени `StorageClass`: Kubernetes назначает default class выбранной установки.
Финальный release readback ждёт `Bound` PVC, `Ready` Pod
`system-assistant-warm` и успешный `/assistant/readyz` leader-реплики
runtime-controller. Отсутствующий default `StorageClass` либо неготовый
системный помощник останавливает релиз до сообщения об успешном завершении.

Kubernetes Secrets защищаются k3s encryption at rest. Значения запрещены в
GitHub artifacts, render, logs, Issue/PR и ConfigMap.

Secret `kodex-external-s3` не выдаётся browser, gateway, agent runtime Pod или
agent. Специализированный session-archive worker получает в `kodex-runtime`
только `access-key` и `secret-key`; endpoint, region и bucket передаёт
контроллер из своей проверенной конфигурации. Production endpoint обязан быть
HTTPS без `skipTLSVerify`; local HTTP разрешён только для точного Service DNS
`seaweedfs-s3.kodex-system.svc.cluster.local`.

## 4. Preflight и reset

```bash
sudo ./tools/install/reset-host.sh \
  --confirm-destroy DESTROY-KODEX-HOST
```

Reset применяется только для согласованного пустого узла. Он запускает штатный
k3s kill-all, снимает mounts, удаляет Kubernetes state и проверяет отсутствие
старых firewall chains. Таблица `inet kodex_fw` удаляется отдельно, а
`nftables.service` отключается: её прежняя `FORWARD policy drop` выполнялась до
Kubernetes/UFW и могла блокировать DNAT до ingress pod после переустановки.

Перед установкой DNS всех публичных hosts должен указывать на exact разрешённые
ingress IP, а порты 22, 80 и 443 быть доступны. Опубликованный AAAA допустим
только при фактически работающем внешнем IPv6 HTTP path; иначе запись удаляется
до выпуска сертификата. Другие входящие порты firewall закрывает. UFW
каждый раз строится с нуля: incoming и routed по умолчанию запрещены, pod
forwarding ограничен CIDR `10.42.0.0/16`, служебный трафик -
`10.43.0.0/16`, а внешний DNAT до pod разрешён только для HTTP/HTTPS.
Host-компонент завершается только после одновременной готовности Kubernetes API
и хотя бы одного node; краткое состояние `NotReady` во время запуска Flannel
обрабатывается bounded wait, а не считается ошибкой установки.

Bare-metal профиль K3s/Traefik остается IPv4-only. Если публичные DNS-имена
имеют только `A` записи, `KODEX_SERVER_PUBLIC_IPV6_ADDRESS` оставляют пустым:
IPv6 bridge не создается, а повторный apply удаляет только ранее созданные
Kodex unit-файлы. Наличие неизвестного unit или drop-in с тем же именем
останавливает операцию до ручного разбора и не приводит к перезаписи.

Если хотя бы один публичный host имеет `AAAA`, владелец задает
`KODEX_SERVER_PUBLIC_IPV6_ADDRESS` как точный глобальный IPv6 адрес, уже
назначенный интерфейсу сервера. Host installer создает две socket-activated
пары `kodex-ipv6-ingress-bridge-{80,443}.{socket,service}`. Сокеты слушают
только `[$KODEX_SERVER_PUBLIC_IPV6_ADDRESS]:80|443`, а
`systemd-socket-proxyd` передает raw TCP на `127.0.0.1:80|443`. Bridge не
завершает TLS, не использует wildcard bind и не открывает дополнительные
порты. Число одновременных соединений ограничено, service sandboxed, а unit
files имеют явный owner marker.

`Type=notify` сохраняется: sandbox разрешает `AF_UNIX` для локального
`sd_notify` наряду с `AF_INET/AF_INET6` для proxy. Это не добавляет сетевых
listeners или destinations. Без notification socket процесс не сообщает READY
и systemd завершает запуск по timeout (#1103).

`preflight` закрыто отклоняет синтаксически неверный, неглобальный либо не
назначенный host-интерфейсу IPv6 адрес. `apply` идемпотентно сверяет ownership перед
записью и включает только socket units. `readback` сравнивает точное содержимое
и права unit-файлов, состояния `enabled/active/listening` обоих сокетов и
выполняет локальный HTTP-запрос через IPv6 bridge до Traefik. Проверка
публичного TLS и DNS выполняется отдельным ACME preflight после готовности
всего ingress.

После прежнего timeout повторяется тот же repo-owned `apply`: после остановки
proxy сбрасывается failed state только четырёх управляемых units, затем
перезапускаются sockets. Сброс выполняется до нового запуска; readback не очищает
ошибки и повторно проверяет состояние после HTTP probe. Новый service failure
останавливает операцию. Не следует заменять `Type=notify`, сбрасывать все failed
units хоста или править units вручную.

## 5. Установка

Полный неинтерактивный bare-metal запуск:

```bash
sudo ./install.sh --non-interactive --components all
```

Интерактивный запуск без `--components` предлагает каждый компонент отдельно.
Явный выбор для существующего Kubernetes:

```bash
./install.sh \
  --components secrets,platform
```

В этом режиме cert-manager, OIDC, HTTPS registry и self-hosted runners уже
существуют и соответствуют входным параметрам `.kodex-env`. Если их нет,
выбираются нужные инфраструктурные компоненты явно; bundled ARC требует
bundled registry, а bundled management surfaces устанавливаются вместе с
bundled identity.

BuildKit не является host daemon. Он запускается rootless sidecar только в
ephemeral ARC build runner. Build и render выполняются на exact SHA в GitHub
Actions; установщик скачивает digest-bound render и применяет его локально к
целевому context.

## 6. Порядок платформы

`tools/install/deploy-platform.sh` соблюдает порядок:

1. в `enabled` — fail-closed exact ClusterIssuer, DNS A/AAAA и внешний HTTP-01
   preflight до публичного `Certificate`;
2. owner-checked retirement stale image-admission binding после fresh namespace
   reset, если её namespaced parameters отсутствуют;
3. phase-aware server-side dry-run CRD и уже известных API без persistence;
4. повторный public TLS preflight, CRD и foundation; в режиме `deferred`
   публичный Certificate исключается;
5. Certificate/Bundle readback;
6. PostgreSQL, NATS и проверка обязательного внешнего S3 Secret/bucket;
7. authority и control-plane migrations;
8. static PostgreSQL role reconciliation;
9. broker bootstrap;
10. `internal-rpc-authority-publisher`;
11. readback всех dynamic Secret projections;
12. остальные Deployments/CronJobs;
13. release artifact materialization, включая exact `control-plane` sentinel
    для authenticated pull-registry readback; транспортный отказ readback
    повторяется ограниченно, а несовпадение digest немедленно закрывает выпуск;
14. rollout, Job и failing Pod readback.

Нельзя запускать workloads до готовности authority projections или считать
GitHub render фактическим deployment.

Preflight повторяет семантику actual apply: mutable ресурсы проверяются через
server-side apply с тем же field manager, а удаляемые перед заменой immutable
ConfigMap и Job проверяются отдельным server-side create с временным именем.
Сам preflight остаётся read-only. Предшествующая фаза `prepare-preflight`
удаляет только cluster-scoped image-admission binding, для которой одновременно
отсутствуют namespaced parameters и labels/spec в точности совпадают с exact
render; неизвестное расхождение закрыто останавливает выпуск. Apply в той же
операции восстанавливает parameters и fail-closed binding из exact render.
Существующий seed Secret не переотправляется через admission policy. При apply
изменившийся immutable ConfigMap удаляется только после проверки platform-owned
labels и затем воссоздаётся из exact render. Такой же owner-checked replacement
выполняется для immutable `ImageAdmissionPolicyParameters` после установления
его CRD и до применения foundation.

## 7. Восстановление и rollback

- Повторный `install.sh` переиспользует owner material и идемпотентно сверяет
  infrastructure.
- Application rollback выполняется новым exact release старого Git SHA.
- Перед dispatch каждого exact release `release-platform.sh` сверяет
  неизменяемые owner actor, workflow ref, job и runner hook, затем разрешает в
  GitHub Variable `KODEX_WORKFLOW_SHA` и обоих ARC gate ровно текущий clean
  `main` SHA. Обновление Kubernetes защищено `resourceVersion`; оба слоя
  проходят exact readback, включая ожидание projected volume уже существующих
  runner Pod. Ручное редактирование variable или `kodex-runner-owner-gate`
  запрещено.
- Потеря `.kodex-material` при существующих данных является инцидентом, а не
  поводом генерировать новые roots поверх работающей установки.
- PostgreSQL/PVC backup и внешняя копия `.kodex-material` хранятся раздельно и
  в зашифрованном виде.

## 8. Обязательный readback

Установка завершена только если:

- Kubernetes API и node Ready;
- при заданном `KODEX_SERVER_PUBLIC_IPV6_ADDRESS` exact IPv6 bridge для 80/443
  enabled/active/listening, а локальный IPv6 HTTP-запрос достигает Traefik;
- при пустом `KODEX_SERVER_PUBLIC_IPV6_ADDRESS` owner-managed bridge units
  отсутствуют и не активны;
- cert-manager/trust-manager/Keycloak/ARC готовы;
- Traefik подключается к Keycloak с exact public SNI через привязанный к
  `Service` `ServersTransport`, а публичный OIDC discovery отвечает без TLS
  fallback;
- Control Center и выбранные management surfaces закрыты OAuth2 Proxy;
- при профиле `all` Headlamp пропускает только Keycloak `admin` и использует
  `cluster-admin`; при остальных профилях его workload и binding отсутствуют;
- все platform StatefulSet/Deployment готовы, migration Jobs успешны;
- production control-plane прошёл authenticated `HeadBucket` внешнего S3, а
  local SeaweedFS, S3 EndpointSlice и `seaweedfs-bucket-bootstrap` готовы;
- отсутствуют `CrashLoopBackOff`, `ImagePullBackOff`, `ErrImagePull` и
  `CreateContainerConfigError`;
- API/OIDC и browser E2E пройдены без раскрытия credentials.


## Обновление offline базы admission и credential files

Admission tools содержат закреплённый официальный Grype archive с SHA256.
Новая сборка останавливается после истечения допустимых720h; нельзя отключать
`GRYPE_DB_VALIDATE_AGE`, увеличивать срок ради запуска или включать runtime
auto-update. Перед выпуском сверить metadata из официального
`https://grype.anchore.io/databases/v6/latest.json`, скачать exact archive,
проверить SHA256 и обновить оба Dockerfile, fixture contract и каталог зависимостей.
`grype db status -o json` в собранном образе от штатного UID без сети должен
подтвердить schema/built/valid. Это проверка базы, а не успешный vulnerability scan
каждого выпускаемого образа. Проверена официальная документация Grype через
Context7: checksum archive, offline import и ограничение возраста.

Bootstrap registry/material generator и reconciliation передают пароль и
Docker auth в jq через private files/JSON stdin, а не `--arg` со значением.
Установщик проецирует S3 keys и Sentry DSN через `kubectl --from-file`; значения
не появляются в argv. Временные файлы имеют0600 внутри0700. При обновлении
node registry сохраняется прежнее удаление завершающих LF из credential files.
Это не меняет auth scopes, CA/TLS policy или существующие secret refs.


### Доставка устойчивого workspace

Для полной приёмки файлов canonical renderer получает
`--session-workspace-profile SESSION_WORKSPACE_V2`. Закрытый transform проверяет
ABI5 владельца и immutable images, exact envFrom и отсутствие shadow env.
Он назначает профиль одновременно control-plane/session-archive и согласованный
`local-path`/`20Gi` runtime-controller/session-archive; маркер Pod template
обеспечивает rollout после изменения ConfigMap. Default `disabled` не меняет
render и не служит командой удаления уже существующего V2 состояния.

Перед переходом установленного стенда подтверждаются current schema041,
совместимые CP/RC/runner/archive images и отсутствие активных runs/archive tasks.
Применение: ConfigMap consumers, затем archive controller и runtime-controller,
последним control-plane owner; каждый Deployment проверяется по UID/spec и
readiness. Только после этого разрешается model claim тестового AGENT/WORKFLOW.
Manifest не заменяет проверку реального PREPARE_WORKSPACE_VOLUME, owner ACK,
PVC/S3 checkpoint и restore. Матрица lifecycle сохранена в
[runtime-workspace-durability-1953](../operations/runtime-workspace-durability-1953.md).
Отключение флага не разрешает fallback, потерю файлов или удаление bindings.


### Память полного SBOM

Полный OpenCode RoleImage на локальном стенде исчерпал прежние2Gi при scan Syft
даже с мягкими1536/768MiB: наблюдался рост RSS до1785MiB перед OOM, без dirty
page tail. Мягкий Go limit не ограничивает обязательный live heap. Renderer
использует один bounded профиль scan для protected/trusted-cluster:
requests CPU1/memory2Gi, limits CPU4/memory16Gi, tmp32Gi. Прочие фазы сохраняют
прежние128Mi/1Gi; CEL проверяет точные ресурсы каждой фазы. Это не разрешение
понизить severity gate, пропустить cataloger либо принять неполный SBOM.

Сначала завершаются старые attempts и сохраняется их failure evidence, затем
доставляется immutable admission-controller с новым renderer и совместимая CEL
policy. При `observability-profile=disabled` также пересобираются производный
immutable renderer, его ConfigMap/mount и CEL opt-in штатным
`observability_profile.admission_profile`: digest исходного renderer должен
совпасть с новым образом. Один image patch оставит старую закреплённую копию и
закрыто остановит создание Jobs. Новый scan создаётся owner-командой после
readback; live успех должен быть подтверждён отдельно.
