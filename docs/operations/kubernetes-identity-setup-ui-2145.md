---
id: OPS-KUBERNETES-SETUP-UI-2145
title: Локальная настройка Kubernetes identity через PWA
type: operations
status: approved
owner: developer
version: 1.0.0
updated: 2026-11-15
---

# Поток пользователя

Связано с [#2145](https://github.com/codex-k8s/kodex/issues/2145).
`PersonalConnectionsPanel` подключает отдельный сценарий только для известного
`BROKER_IDENTITY/KUBERNETES_TARGET` и `INITIATOR_REQUIRED`. TOKEN, SSH и неизвестные
metadata не попадают в этот сценарий. Обычный пользователь не получает управление
общей SERVICE identity.

Владелец запускает локально
`python3 tools/dev/managed-kubernetes-identity.py inspect --kubeconfig <local-path>`
и переносит только SHA256/размер. Begin отправляет их с текущей Connection version
и явным `expectedVersion=0` при отсутствии личной binding. Subject, target и
identity назначает CP; PWA их не выбирает. Первый admission можно скачать для
CLI. Permit не отображается и не хранится в reactive view, URL, cookies или Web
Storage. После pagehide/logout/смены context он удаляется из памяти PWA.

Скачанный admission содержит одноразовый секрет. Пользователь ограничивает права
файла командой `chmod 600 <admission-file>`, затем запускает
`python3 tools/dev/managed-kubernetes-identity.py upload --admission-file <admission-file> --kubeconfig <local-path>`.
Kubeconfig не читается браузером; CLI передаёт его прямо broker по выбранному
owner протоколу. Наличие инструкции не означает квалификацию CLI/broker deployment:
они входят в полный unit047, принадлежащий catalog.

# Recovery и полномочия

Потерянный Begin сверяется GET по исходному Begin idempotency key. Повреждённый
успешный response также не разрешает автоматическую повторную запись. GET не
восстанавливает permit. Утрата файла требует чтения и явной отмены текущего setup,
а не второго upload. Cancel передаёт ETag точной версии setup. После unknown
Cancel допускается только GET исходного setup; чтение другого завершённого setup
не снимает неопределённость первой записи.

После COMPLETED UI показывает требование отдельной проверки Connection, затем
обновляет metadata по действию пользователя. Ни identity ref/revision, ни статус
setup не превращаются в READY, verified capabilities или grant. После любого
terminal setup новые Begin требуют обновить Connection metadata. READ статуса
не создаёт effect, domain event или grant; авторитетный read path — Get setup.
Owner lifetime и immutable context ограждают поздние responses после logout,
pagehide и смены version. Отмена браузерного запроса не объявляет серверную работу
отменённой.

# Подключение общей SERVICE поверхности

Компонент `KubernetesIdentitySetupPanel` принимает `KubernetesSetupContext`:
`connectionRef`, `connectionVersion`, `policy`, `expectedVersion`, `available`,
`enabled`. Для manager-поверхности catalog подключает его внутри существующего
`canManage(currentConnection)` и exact KUBE-purpose guard в
`IntegrationConnectionsPanel`. Нужно передать `policy=PROJECT_SHARED` и текущую
Connection version одновременно в `connectionVersion` и `expectedVersion`;
binding metadata общей identity равны нулю. `changed` вызывает свежий owner read
подключения. Значения не выводятся из `created_by`, локального selector либо
предполагаемых organization прав. Этот manager mounting не включён в отдельный
PWA contribution; он должен быть сведён в полный047 до заявления его готовности.

# Проверки

Focused unit suites находятся рядом с feature. Browser suite
`e2e/kubernetes-identity-setup.synthetic.spec.ts` использует существующую
`personal-connections.html` с настоящим panel и generated client; transport
синтетический. Она проверяет скачивание точного admission, отсутствие permit в DOM,
mobile/desktop, recovery исходного ключа, Cancel ETag и TEST_REQUIRED. Это не
доставка047/108, не живой upload и не проверка Kubernetes RBAC.

Проверены Context7 `/websites/vuejs`:
[watch cleanup](https://vuejs.org/guide/essentials/watchers) и
[reactivity core](https://vuejs.org/api/reactivity-core).
