---
id: OPS-DOC-KUBERNETES-IDENTITY-EXPORT-2145
title: Локальная подготовка выбранного kubeconfig context
type: operations
status: approved
owner: developer
version: 1.1.0
updated: 2026-11-15
---

# Подготовка kubeconfig для owner-local загрузки

Локальная команда `export` относится к onboarding #2145. Её выполняет владелец
своего файла на своём компьютере. Ключи не передаются сотруднику Kodex, в его
workspace, API/PWA, argv/env или диагностический вывод.

```bash
python3 -I -B tools/dev/managed-kubernetes-identity.py export --kubectl <absolute-kubectl-path> --kubeconfig <private-source-file> --context 'source-context-name' --target-context 'canonical-context-from-owner' --output <new-private-file>
python3 -I -B tools/dev/managed-kubernetes-identity.py inspect --kubeconfig <new-private-file>
```

`--context` обязателен, current-context исходного файла не используется как
выбор по умолчанию. Только явный `--target-context` переименовывает name
единственного context и current-context результата. Cluster/user references,
server, trust и credentials этим параметром не меняются. Имя назначения должно
поступить из safe operator requirements перед Begin; оно не выдаёт authority.

Порядок пользовательского сценария: прочитать current KUBE requirements
(canonicalContext, target/version и Connection/policy) → export с явным
переименованием → inspect точных итоговых bytes → Begin с hash/size → скачать
admission → upload того же private файла. Canonical context в SetupView
подтверждает immutable snapshot после Begin. Получение имени только после
Begin не завершает first-user путь: позднее переименование меняет hash и
закрыто отклоняется. Public requirements projection и его PWA consumer
квалифицируются общим047; local export сам по себе не означает их готовность.

`inspect` выдаёт только SHA256 и размер. Это не доказательство правильного
формата, текущих RBAC/identity или готовности подключения. Broker отдельно
проверяет strict embedded shape, exact target/CA, authenticated subject и
актуальную eligibility.

Export использует один private source snapshot и два read-only вызова явно
заданного kubectl: minify/raw/json, затем minify/flatten/raw/json. Все arguments
кроме input/context и безопасного output format фиксированы. Env defaults,
KUBECONFIG, kuberc overrides и proxy environment не наследуются. REST client
для Kubernetes API не создаётся. Полученный stdout хранится только в памяти
CLI, затем в новом owner-private0600 файле; наружу он не печатается.

Первый snapshot сохраняет relative refs относительно исходного каталога.
После minify читаются максимум три известных refs: CA, client certificate и
client key. Каждый читается одним bounded descriptor с проверкой стабильности.
Private key требует owned regular0600/0400 файл без symlink/hardlink;
CA/client certificate могут быть public-readable и не writable для group/other.
Stage2 получает embedded copies: исходные файлы не открываются kubectl повторно
после проверки. Source и references не переписываются.

Выход допускает ровно один context/cluster/user и static token либо embedded
client certificate/key. Exec/auth-provider, impersonation, password/tokenFile,
proxy/insecure flags, extensions и прочие SDK features закрыто отклоняются.
Небезопасная настройка выбранного context не исполняется. Неиспользуемые
contexts удаляются minify; это не разрешает применять их credentials.

Общий operation budget15s охватывает оба вызова, stdout ограничен1MiB на вызов,
stderr64KiB. Любой непустой stderr или nonzero status закрывает export без
вывода исходной диагностики. Отказ/timeout завершает свою process group и
дожидается leader. Output создаётся только после полной проверки, с O_EXCL,
0600 и fsync; существующий файл не заменяется. Partial cleanup не удаляет
объект, который заменил созданный inode.

Публичная проверка использует синтетические token/certificate configs,
relative refs и явно выбранный local kubectl1.35.5:

```bash
KODEX_TEST_KUBECTL_BINARY=<absolute-qualified-kubectl-path> make test-managed-kubernetes-identity-export
```

Она включает actual kubectl/Python process → настоящий broker ParseCredential;
материалы fixture не выводятся. Live kubeconfig и cluster не используются.

Проверены Context7 `/kubernetes/kubectl` и точный source1.35.5:
[View.Run](https://github.com/kubernetes/kubernetes/blob/v1.35.5/staging/src/k8s.io/kubectl/pkg/cmd/config/view.go),
[MinifyConfig/FlattenConfig](https://github.com/kubernetes/kubernetes/blob/v1.35.5/staging/src/k8s.io/client-go/tools/clientcmd/api/helpers.go),
[kuberc disable](https://github.com/kubernetes/kubernetes/blob/v1.35.5/staging/src/k8s.io/kubectl/pkg/kuberc/kuberc.go).

## Сквозной consumer требований

`kubernetesSetupRequirements` приходит из существующих scoped Connection и
«Мои подключения»: `targetRef`, `targetRevision`, `canonicalContext`,
`connectionVersion`, `credentialPolicy`. Отсутствующие, неверные или не совпадающие
с текущими version/policy требования запрещают новый Begin. UI не подставляет
имя context из definition, текущей формы либо старого setup. Opaque context в
команде заключён в POSIX single quotes; shell metacharacters не исполняются.

| Шаг | Владелец и проверка | Результат и восстановление |
| --- | --- | --- |
| Read requirements | CP scoped visibility/current package/target revision; HTTP нормализует закрытую safe metadata | Новых полномочий и событий нет; повторное чтение существующего Connection |
| Export → inspect | Локальный владелец explicit source context, canonical rename, private snapshot/file guards | Новый0600 файл и только hash/size; без сетевого эффекта и без события |
| Begin | Existing dedicated owner RPC, verified actor, binding/Connection OCC, idempotency, текущий pinned target | Первичный admission либо reconcile-only; unknown → GET по исходному ключу, без повторного upload |
| Get / Cancel | Exact scoped setup + текущий setup ETag для Cancel | Immutable canonicalContext остаётся в readback; потеря requirements не блокирует адресованное чтение/отмену |
| Upload / Complete | CLI exact TLS admission → private owner upload lifecycle | COMPLETED означает TEST_REQUIRED; готовность identity/RBAC проверяет отдельный owner test |

Личный mount допускает только известный BROKER_IDENTITY/KUBERNETES_TARGET.
Сервисный mount дополнительно требует PROJECT_SHARED, доступную exact definition
и server-projected UPDATE; это лишь видимость UI, не замена server owner
проверке organization manager и явно выбранной service identity. TOKEN/SSH
и неизвестный kind не используют этот consumer. AVAILABLE материала SecretB
не относится к готовности identity.
