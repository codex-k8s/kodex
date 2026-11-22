---
id: OPS-DOC-KUBERNETES-SECRET-MATERIAL-2145
title: Защищённая подготовка данных Kubernetes Secret
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-15
---

# Защищённые данные Secret

Срез связан с #2145. `data` и `stringData` не передаются в PWA, обычный
integration invocation, prompt, RunnerInput, clipboard или журнал. Локальный
CLI читает private JSON-файл и загружает содержимое прямо в broker через TLS.
Публичные команды управляют лишь подготовкой и безопасной историей материала.
Они не изменяют Secret в Kubernetes и не предоставляют effect credit.

Для JSON Secret v1:

```bash
python3 -I -B tools/dev/managed-kubernetes-resource-material.py inspect --operation kubernetes.create --secret-file <private-secret-json>
chmod 600 <admission-file>
python3 -I -B tools/dev/managed-kubernetes-resource-material.py upload --operation kubernetes.create --secret-file <private-secret-json> --admission-file <admission-file>
```

`inspect` печатает только operation, namespace, name/generateName, secretType,
patchType, contentSHA256 и contentBytes. Для create/replace/apply/edit private
content — canonical JSON `{data?,stringData?}`. Остальные поля исходного Secret
не загружаются этим каналом: неизменяемый публичный запрос будущей операции
задаёт безопасную metadata/type отдельно. Ровно один из name/generateName
обязателен, generateName допустим только для create.

Для PATCH передаётся весь private native patch; публичный invocation body
остается пустым `{}` либо `[]`. Порядок соединения private/public patch не
придумывается:

```bash
python3 -I -B tools/dev/managed-kubernetes-resource-material.py inspect --operation kubernetes.patch --patch-file <private-patch-json> --patch-type JSON --namespace <namespace> --name <name> --secret-type Opaque
python3 -I -B tools/dev/managed-kubernetes-resource-material.py upload --operation kubernetes.patch --patch-file <private-patch-json> --patch-type JSON --namespace <namespace> --name <name> --secret-type Opaque --admission-file <admission-file>
```

MERGE и STRATEGIC используют свои native documents. No-op `{}`/`[]` допустим.
Owner/broker отдельно проверяют допустимость patch, schema и exact UID/RV;
публичная подготовка не отменяет этих проверок. Service-account-token Secret
закрыто запрещён.

Пользователь переносит safe metadata в PWA и скачивает исходный admission.
Он содержит одноразовый permit; только owner-local CLI читает этот private
файл. Нельзя помещать admission или значения Secret в argv/env/чат. CLI требует
owned regular file0600 без symlink/hardlink, проверяет CA checksum, exact SNI,
TLS1.3 и operator-pinned IP. Proxy environment игнорируется. После начала
одного POST нет redirect/reconnect/retry; raw exceptions/body не выводятся. Socket timeout
не считается абсолютным deadline: общий identity/material transport прерывает
I/O отдельным deadline timer даже при drip headers/body, затем cancel/join
завершает timer до закрытия соединения. Поздний ACK не превращается в успех;
локальное завершение не доказывает остановку удалённого действия.

| Переход | Actor и authority | Контракт и результат |
|---|---|---|
| Begin | Authenticated user через control-api-gateway; owner заново разрешает Connection/policy/current personal binding либо active SERVICE | POST `/api/v1/me/integration-connections/{connectionRef}/kubernetes-resource-materials` → public Begin RPC → owner repository; mutation key, explicit expectedVersion и expectedConnectionVersion. Только первый admission содержит permit/endpoint. |
| Get | Тот же проверенный actor/owner scope, не authority из materialRef | GET по ровно одному materialRef/Begin key → Get RPC → safe18-field view и ETag. Permit/private locator/hash не возвращаются. |
| Cancel | Тот же current owner; сначала адресное разрешение, затем OCC/idempotency | POST `.../{materialRef}/cancel`, If-Match текущей material version → Cancel RPC. Закрывает будущее использование, не переписывает прошлый UNKNOWN/effect. |
| Upload/observe | Одноразовый purpose-specific permit из первого admission; private broker TLS route | POST `/v1/kubernetes-resource-materials/{ref}/upload\|observe`. Safe view; observe не повторяет private write. |
| AVAILABLE | Факт сохранения private artifact | «Защищённый материал подготовлен; изменение Secret требует отдельного допуска». Secret effects требуют EACH и свежего owner Begin; private refs не расширяют доступ. |
| UNKNOWN/ACK_REJECTED/expiry | Авторитетная история владельца | Нет автоматической новой попытки. UNKNOWN и MATERIAL_OBSERVATION_ABSENT не доказывают отсутствие загрузки. Get не восстанавливает permit. |
| CONSUMED/revoke | Owner lifecycle | История сохраняется, повторное использование закрыто. Результат target effect читается отдельно. |

Публичные вызовы используют специализированные RPC и permissions
`platform.integrations.kubernetes.material.begin|get|cancel`; transport peer
только control-api-gateway. Actor/ROOT/subject/identity/target назначает owner.
При отсутствии optional owner repository операция возвращает Unavailable.
События эти public commands не публикуют; авторитетный read path — Get.

Permit/setup deadline90s отделён от immutable material use deadline15min от
Begin. Переход AVAILABLE меняет отображаемый deadline, не продлевает permit.
UI очищает permit на pagehide/logout/context change, отбрасывает поздние ответы
и после неизвестной записи допускает только чтение исходного selector.
Shared manager surface требует явный PROJECT_SHARED, exact BROKER_IDENTITY /
KUBERNETES_TARGET metadata и server-owned UPDATE; createdBy не выдаёт прав.

Публичная локальная проверка: `make test-managed-kubernetes-material-public`.
Она проверяет service/grpc/actual HTTP router, native process TLS upload и
identity CLI regression. PWA unit/browser и полный native owner ledger,
private upload/storage, policy108, install/render и сквозной target effect
квалифицируются полным #2145. Этот target сам по себе не доказывает готовность
всего047/108 и не выполняет внешние effects.

Проверены Context7 `/python/cpython`: `ssl.create_default_context`,
`CERT_REQUIRED`, `check_hostname`, `server_hostname`, `TLSVersion.TLSv1_3`.
