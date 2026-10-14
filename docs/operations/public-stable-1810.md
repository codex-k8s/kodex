---
id: OPS-PUBLIC-STABLE-001
title: Штатная поставка Kodex и внешний доступ с SSO
type: operations
status: approved
owner: manager
version: 1.0.0
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
