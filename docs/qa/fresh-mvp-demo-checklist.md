---
id: QA-DOC-001
title: Проверка fresh web-only MVP перед демонстрацией
type: qa-checklist
status: approved
owner: qa
version: 1.0.0
updated: 2026-08-25
---

# Проверка fresh web-only MVP перед демонстрацией

Чеклист доказывает готовность чистой установки Kodex без legacy-данных и
Mattermost. Он разделяет безопасный Kubernetes readback до выпуска публичного
TLS-сертификата и финальный trusted E2E после отдельного допуска владельца.

## 1. Правила результата

- Для каждого пункта заполнить `результат: PASS|FAIL|NOT RUN` и ссылку либо
  путь к evidence. Checkbox отмечается только после заполнения обоих полей.
- `PASS` означает, что проверка действительно выполнена на exact SHA и дала
  ожидаемый результат. Отсутствие ошибки в другой проверке не является PASS.
- `FAIL` означает воспроизводимый дефект или неисправность обязательной
  оснастки. Для FAIL создать Issue с severity, шагами, expected/actual,
  безопасным evidence и ссылкой на этот пункт.
- `NOT RUN` означает, что проверка не выполнялась либо не было отдельного
  допуска, безопасной среды, credential или инструмента. Причина обязательна.
- В evidence запрещены значения Secret, cookies, bearer/refresh tokens,
  пароли, DSN, private keys, OIDC code/state, содержимое пользовательских
  файлов и полные URL с query/fragment.
- Kubernetes-команды до публичного TLS выполняются read-only. Запрещено
  создавать, изменять или удалять публичный `Certificate` ради проверки.
- Browser E2E запускается только после отдельного допуска владельца на
  синтетических данных fresh-установки. `--ignore-certificate-errors`, `-k`,
  `skipTLSVerify`, подмена Origin/CSRF и перехват HTTP/WebSocket запрещены.
- Необязательный Mattermost-профиль не входит в web-only MVP и получает
  `NOT RUN`, а не ложный PASS.

## 2. Паспорт прогона

- [ ] `META-01` Зафиксирован exact Git SHA: `________________`; результат:
      `________`; evidence: `________________`.
- [ ] `META-02` Зафиксированы release lock и SHA-256 неизменяемого render:
      `________________`; результат: `________`; evidence: `________________`.
- [ ] `META-03` Зафиксированы profile `web-only`, точный Kubernetes context,
      namespace `kodex-system` и публичный origin без query/credentials;
      результат: `________`; evidence: `________________`.
- [ ] `META-04` Зафиксированы UTC-время начала/окончания, оператор и ссылка на
      отдельный owner-допуск для live readback/E2E; результат: `________`;
      evidence: `________________`.
- [ ] `META-05` Зафиксированы версии `kubectl`, Node.js, npm, Playwright и
      Chromium; результат: `________`; evidence: `________________`.
- [ ] `META-06` Подтверждено, что установка fresh, тестовые имена используют
      уникальный `KODEX_E2E_RESOURCE_PREFIX`, а живые пользовательские данные
      отсутствуют; результат: `________`; evidence: `________________`.

## 3. Фаза A: pre-public-TLS readback

Эта фаза не открывает публичный origin и не инициирует ACME issuance. Она
может выполняться до внешнего времени сброса rate limit.

### 3.1 Kubernetes и release identity

- [ ] `K8S-01` Kubernetes API доступен в точном context, current context не
      отличается от паспорта; результат: `________`; evidence: `________________`.
- [ ] `K8S-02` Все schedulable node имеют `Ready=True`, отсутствует
      `MemoryPressure`, `DiskPressure`, `PIDPressure` и `NetworkUnavailable`;
      результат: `________`; evidence: `________________`.
- [ ] `K8S-03` Namespace `kodex-system` существует; отсутствуют старые
      MatterCodex/MyQRContact namespaces и workloads; результат: `________`;
      evidence: `________________`.
- [ ] `K8S-04` Все CRD из exact render существуют и имеют condition
      `Established=True`; результат: `________`; evidence: `________________`.
- [ ] `K8S-05` В render нет unresolved `__KODEX_*__`, `.invalid`, нулевых image
      digests, `Vault` и `SecretProviderClass`; результат: `________`;
      evidence: `________________`.
- [ ] `K8S-06` Каждый workload использует digest-bound image из release lock;
      tag-only, `latest` и неожиданные registry отсутствуют; результат:
      `________`; evidence: `________________`.
- [ ] `K8S-07` Release SHA/render digest доступны в deployment evidence и
      совпадают с паспортом; результат: `________`; evidence: `________________`.

### 3.2 Хранилище и Secret metadata

- [ ] `DATA-01` Все требуемые PVC имеют phase `Bound`, ожидаемые StorageClass,
      capacity и access mode; результат: `________`; evidence: `________________`.
- [ ] `DATA-02` PostgreSQL и NATS PVC не используют временное хранилище;
      результат: `________`; evidence: `________________`.
- [ ] `DATA-03` Обязательные installation Secrets существуют. Evidence содержит
      только имя, generation/resourceVersion и список ключей, но не `.data`;
      результат: `________`; evidence: `________________`.
- [ ] `DATA-04` K3s encryption at rest включено и installer material находится
      вне Git/render/logs с owner-only mode; результат: `________`; evidence:
      `________________`.
- [ ] `DATA-05` В Pod specs нет неожиданных Secret mounts/env, service-account
      token не монтируется туда, где он не требуется; результат: `________`;
      evidence: `________________`.

### 3.3 Внутренний trust и stateful dependencies

- [ ] `TRUST-01` Все внутренние `Certificate` exact render имеют `Ready=True`,
      а `Bundle` — `Synced=True`; публичный Control Center certificate на этом
      шаге исключён; результат: `________`; evidence: `________________`.
- [ ] `TRUST-02` Не используются plaintext fallback, `skipTLSVerify` или
      wildcard egress; результат: `________`; evidence: `________________`.
- [ ] `STATE-01` StatefulSet `kodex-postgresql` полностью готов, Pod проходит
      readiness/liveness и не перезапускается циклически; результат:
      `________`; evidence: `________________`.
- [ ] `STATE-02` StatefulSet `kodex-nats` полностью готов, JetStream readiness
      успешна и данные находятся на PVC; результат: `________`; evidence:
      `________________`.
- [ ] `STATE-03` PostgreSQL и NATS Service имеют готовые EndpointSlice без
      `notReadyAddresses`; результат: `________`; evidence: `________________`.

### 3.4 Migrations, bootstrap jobs и authority

- [ ] `JOB-01` `internal-rpc-authority-migrate` завершён с `succeeded=1`, без
      failed Pod; результат: `________`; evidence: `________________`.
- [ ] `JOB-02` `control-plane-migrate` завершён с `succeeded=1`, без failed Pod;
      результат: `________`; evidence: `________________`.
- [ ] `JOB-03` `kodex-postgresql-runtime-credentials` завершён с `succeeded=1`;
      результат: `________`; evidence: `________________`.
- [ ] `JOB-04` `control-plane-broker-bootstrap` завершён с `succeeded=1`;
      результат: `________`; evidence: `________________`.
- [ ] `JOB-05` `release-artifact-materializer` завершён с `succeeded=1`;
      результат: `________`; evidence: `________________`.
- [ ] `JOB-06` Для всех Jobs отсутствуют активные/failed attempts после
      terminal success; в безопасных логах нет secret values; результат:
      `________`; evidence: `________________`.
- [ ] `AUTH-01` `internal-rpc-authority-publisher` полностью готов; результат:
      `________`; evidence: `________________`.
- [ ] `AUTH-02` Все dynamic Secret projections имеют generation больше нуля,
      точный ожидаемый набор непустых keys; values не читались и не печатались;
      результат: `________`; evidence: `________________`.

### 3.5 Workloads, API readiness и observability

- [ ] `WORK-01` Каждый Deployment/StatefulSet/DaemonSet exact render полностью
      готов; каждый CronJob существует с ожидаемым schedule/suspend/concurrency
      policy; результат: `________`; evidence: `________________`.
- [ ] `WORK-02` Нет Pod в `CrashLoopBackOff`, `ImagePullBackOff`, `ErrImagePull`,
      `CreateContainerConfigError`, `OOMKilled`, `Evicted` или неизвестном
      waiting state; результат: `________`; evidence: `________________`.
- [ ] `WORK-03` Неожиданные restart counts и warning Events отсутствуют либо
      для каждого есть объяснение и Issue; результат: `________`; evidence:
      `________________`.
- [ ] `WORK-04` Все Service имеют ожидаемые готовые EndpointSlice; отсутствуют
      endpoints старой установки; результат: `________`; evidence:
      `________________`.
- [ ] `API-01` Readiness probe `control-api-gateway:/readyz` успешна на рабочем
      Pod и проверяет тот же dependency snapshot, что рабочий API; результат:
      `________`; evidence: `________________`.
- [ ] `API-02` Readiness/liveness staff-control-center успешны по HTTPS внутри
      Pod; runtime config содержит same-origin `/api/v1` и `/api/v1`, ожидаемый
      OIDC issuer/client и не содержит placeholder; результат: `________`;
      evidence: `________________`.
- [ ] `OBS-01` Prometheus и Alertmanager имеют available replica; Grafana и
      Headlamp workloads готовы, но публичный browser пока не открывался;
      результат: `________`; evidence: `________________`.
- [ ] `OBS-02` Prometheus targets обязательных Kodex workloads `UP`, alerts о
      недоступности core dependencies не firing; результат: `________`;
      evidence: `________________`.

## 4. Фаза B: выпуск и проверка trusted public TLS

Фаза начинается только после подтверждённого окончания внешнего ACME rate
limit. До этого запрещены повторные issuance/reissuance попытки.

- [ ] `TLS-01` Зафиксированы источник и точное время окончания rate limit;
      текущее UTC-время позже него; результат: `________`; evidence:
      `________________`.
- [ ] `TLS-02` DNS A/AAAA для Control Center и recovery SAN указывает только на
      ожидаемый ingress; отсутствует ошибочный IPv6; результат: `________`;
      evidence: `________________`.
- [ ] `TLS-03` После одной разрешённой apply-попытки публичный `Certificate`
      имеет `Ready=True`; повторная ручная ротация и удаление Secret не
      выполнялись; результат: `________`; evidence: `________________`.
- [ ] `TLS-04` SAN содержит точный публичный host и настроенный recovery host,
      issuer/chain ожидаемы, срок действия корректен; результат: `________`;
      evidence: `________________`.
- [ ] `TLS-05` Внешний TLS-клиент получает полную доверенную цепочку,
      hostname verification и `Verify return code: 0`; Traefik default
      certificate не выдаётся; результат: `________`; evidence:
      `________________`.
- [ ] `TLS-06` HTTPS работает без `-k` и browser security warnings; HTTP только
      перенаправляет на точный HTTPS origin; результат: `________`; evidence:
      `________________`.
- [ ] `TLS-07` Security headers включают HSTS, CSP, anti-framing и запрет MIME
      sniffing; mixed content отсутствует; результат: `________`; evidence:
      `________________`.

## 5. Фаза C: OIDC, session и API

- [ ] `OIDC-01` OIDC discovery по trusted HTTPS возвращает 200, точный issuer,
      authorization/token/JWKS endpoints без private host и plaintext URL;
      результат: `________`; evidence: `________________`.
- [ ] `OIDC-02` Неавторизованный вход в Control Center и management surfaces
      перенаправляется в ожидаемый Keycloak client; open redirect отсутствует;
      результат: `________`; evidence: `________________`.
- [ ] `OIDC-03` Synthetic owner входит через Authorization Code + PKCE и
      возвращается только на exact callback origin; результат: `________`;
      evidence: `________________`.
- [ ] `OIDC-04` После callback protocol state очищен; bearer/refresh token не
      остаётся в URL, localStorage или доступной JavaScript cookie; результат:
      `________`; evidence: `________________`.
- [ ] `SESSION-01` `POST /api/v1/session` создаёт owner session, после чего UI
      работает через host-only `Secure`/`HttpOnly`/`SameSite` cookie;
      результат: `________`; evidence: `________________`.
- [ ] `SESSION-02` Reload и новая вкладка с тем же storage state сохраняют
      сессию; подмена/просрочка cookie закрыто отклоняется; результат:
      `________`; evidence: `________________`.
- [ ] `SESSION-03` Mutation без CSRF, с неверным CSRF либо чужим Origin
      отклоняется и не меняет состояние; результат: `________`; evidence:
      `________________`.
- [ ] `SESSION-04` Logout вызывает `DELETE /api/v1/session`, очищает локальную
      session и защищённые API перестают возвращать business data; результат:
      `________`; evidence: `________________`.
- [ ] `API-03` Неавторизованные `/api/v1/bootstrap`, `/api/v1/projects` и
      `/api/v1/runs` не возвращают business data; результат: `________`;
      evidence: `________________`.
- [ ] `API-04` Авторизованные bootstrap, overview, platform capabilities,
      projects, runs, decisions и audit отвечают schema-compatible JSON без
      внутренних stack traces и secret material; результат: `________`;
      evidence: `________________`.
- [ ] `API-05` HTTP API и WebSocket используют только same-origin `/api/v1`;
      reconnect/catch-up не требует bearer в URL; результат: `________`;
      evidence: `________________`.
- [ ] `API-06` Ошибки имеют безопасный `Problem.code`; backend detail и
      персональные/секретные данные не показываются в UI; результат:
      `________`; evidence: `________________`.

## 6. Фаза D: Control Center desktop

### 6.1 Экраны и навигация

- [ ] `UI-D-01` `/onboarding`: готов Системный помощник, first-run завершается;
      результат: `________`; evidence: `________________`.
- [ ] `UI-D-02` `/assistant`: новый диалог, typed plan и применение только
      разрешённых изменений; результат: `________`; evidence: `________________`.
- [ ] `UI-D-03` `/`: overview показывает активные runs и ожидающие решения;
      результат: `________`; evidence: `________________`.
- [ ] `UI-D-04` `/projects` и `/projects/:projectRef`: список, создание и обзор
      синтетического не-IT Проекта; результат: `________`; evidence:
      `________________`.
- [ ] `UI-D-05` Agents list/profile: создание, draft, validation, publish,
      capability, image recipe и подготовка окружения; результат: `________`;
      evidence: `________________`.
- [ ] `UI-D-06` Workflows list/profile: создание, два дочерних сотрудника,
      validation, publish и launch; результат: `________`; evidence:
      `________________`.
- [ ] `UI-D-07` Runs global/project/detail: queue, live graph, timeline,
      artifacts, cancel, retry, continuation и lineage; результат: `________`;
      evidence: `________________`.
- [ ] `UI-D-08` Files: список, безопасная загрузка/скачивание synthetic файла и
      project binding; результат: `________`; evidence: `________________`.
- [ ] `UI-D-09` Automations: экран открывается, состояния empty/list/form
      согласованы с permissions; destructive запуск не выполняется; результат:
      `________`; evidence: `________________`.
- [ ] `UI-D-10` Integrations: core явно готов без connections, обязательность
      Mattermost отсутствует; результат: `________`; evidence: `________________`.
- [ ] `UI-D-11` Decisions: Human Gate отображается и разрешается ровно одним
      concurrent owner action; результат: `________`; evidence:
      `________________`.
- [ ] `UI-D-12` Administration/access/members/audit открываются согласно owner
      role; typed assistant action присутствует в audit; результат:
      `________`; evidence: `________________`.

### 6.2 Web-only lifecycle

- [ ] `FLOW-01` Помощник создаёт ровно один синтетический Проект без Git,
      Kubernetes, Mattermost и иных integrations; результат: `________`;
      evidence: `________________`.
- [ ] `FLOW-02` ИИ-сотрудник публикуется, environment готовится и direct Run
      завершается с безопасным downloadable artifact; результат: `________`;
      evidence: `________________`.
- [ ] `FLOW-03` Continuation создаёт следующий turn той же session и сохраняет
      контекст; результат: `________`; evidence: `________________`.
- [ ] `FLOW-04` Системный помощник typed action создаёт сотрудника и audit
      event; системного помощника нельзя удалить/архивировать; результат:
      `________`; evidence: `________________`.
- [ ] `FLOW-05` Nested workflow создаёт ожидаемые nodes/edges/callbacks без
      дублей; дочерние результаты возвращаются координатору; результат:
      `________`; evidence: `________________`.
- [ ] `FLOW-06` Human Gate выдерживает два конкурентных решения: один winner,
      один version conflict с актуальным state; результат: `________`;
      evidence: `________________`.
- [ ] `FLOW-07` Контролируемый offline показывает offline state, после возврата
      сети WebSocket выполняет reconnect/catch-up без дубликатов; результат:
      `________`; evidence: `________________`.
- [ ] `FLOW-08` Cancel закрывает исходный граф; retry создаёт новую attempt с
      ссылкой на предыдущую и не меняет исходную terminal attempt; результат:
      `________`; evidence: `________________`.
- [ ] `FLOW-09` Core runs остаются `SUCCEEDED` без integration connections;
      результат: `________`; evidence: `________________`.
- [ ] `FLOW-10` Mattermost profile не запускался для web-only MVP; результат:
      `NOT RUN`; причина/evidence: `optional profile outside Issue #846`.

## 7. Фаза E: mobile и responsive

- [ ] `UI-M-01` Pixel 7 viewport: shell/menu/navigation доступны с keyboard и
      корректными accessible names; результат: `________`; evidence:
      `________________`.
- [ ] `UI-M-02` Projects, assistant, run detail и Human Gate доступны без
      горизонтального overflow и перекрытия controls; результат: `________`;
      evidence: `________________`.
- [ ] `UI-M-03` На mobile graph заменён читаемым списком, node status и связи
      доступны; desktop canvas скрыт; результат: `________`; evidence:
      `________________`.
- [ ] `UI-M-04` Длинные русские названия, empty/loading/error/offline states не
      ломают layout; результат: `________`; evidence: `________________`.
- [ ] `UI-M-05` Основные команды доступны с touch target и не требуют hover;
      результат: `________`; evidence: `________________`.

## 8. Фаза F: console/network и утечки

- [ ] `ERR-01` Playwright auto-fixture не обнаружила uncaught `pageerror`;
      результат: `________`; evidence: `________________`.
- [ ] `ERR-02` Playwright auto-fixture не обнаружила неожиданный console error;
      контролируемый offline-интервал ограничен одним reconnect-сценарием;
      результат: `________`; evidence: `________________`.
- [ ] `ERR-03` Playwright auto-fixture не обнаружила неожиданный failed request
      или HTTP 5xx; результат: `________`; evidence: `________________`.
- [ ] `ERR-04` В browser network отсутствуют неожиданные 4xx, redirect loops,
      mixed content, CORS и CSP violations. Ожидаемые CSRF/version negatives
      связаны с конкретным сценарием; результат: `________`; evidence:
      `________________`.
- [ ] `ERR-05` HTML, JS config, WebSocket frames, downloaded artifacts,
      screenshots и HTML reporter не содержат cookies, tokens, passwords,
      private keys, DSN или raw provider output; результат: `________`;
      evidence: `________________`.
- [ ] `ERR-06` Auth setup не создаёт screenshot/trace/video; session state —
      regular non-symlink file с mode `0600`, каталог — `0700`; результат:
      `________`; evidence: `________________`.

## 9. Фаза G: Grafana и Headlamp через OAuth

- [ ] `MGMT-01` Прямой anonymous запрос к Grafana/Headlamp не получает
      защищённый UI и направляется через соответствующий OAuth2 Proxy;
      результат: `________`; evidence: `________________`.
- [ ] `MGMT-02` Grafana принимает synthetic owner с ролью `kodex-owner`,
      открывает home и обязательные Kodex dashboards без console/network
      errors; результат: `________`; evidence: `________________`.
- [ ] `MGMT-03` Synthetic пользователь без `kodex-owner` не проходит Grafana
      role gate; результат: `________`; evidence: `________________`.
- [ ] `MGMT-04` Headlamp принимает только Keycloak administrator с ролью
      `admin`; пользователь без неё получает отказ; результат: `________`;
      evidence: `________________`.
- [ ] `MGMT-05` Headlamp использует ServiceAccount/ClusterRoleBinding
      `cluster-admin`, показывает node/namespaces/workloads; Secret values в
      ходе QA не открываются; результат: `________`; evidence:
      `________________`.
- [ ] `MGMT-06` OAuth cookies Control Center, Grafana и Headlamp имеют разные
      имена, `Secure`/`HttpOnly` и не расширены на другие hosts; результат:
      `________`; evidence: `________________`.
- [ ] `MGMT-07` Ingress каждого surface содержит exact OAuth2 middleware;
      обход proxy через другой публичный route отсутствует; результат:
      `________`; evidence: `________________`.

## 10. Команды поддерживаемой browser-проверки

До owner-допуска выполняется только статическая проверка discovery/types:

```bash
cd services/staff/control-center
npm ci --ignore-scripts
npm run test:e2e:check
```

После допуска и успешного trusted TLS создаётся реальная OIDC session. Значения
credentials читаются без печати и не добавляются в shell history/evidence:

```bash
export KODEX_E2E_BASE_URL='https://<approved-fresh-origin>'
export KODEX_E2E_STORAGE_STATE="$PWD/.auth/owner.json"
export KODEX_E2E_CONFIRM_DISPOSABLE='I_UNDERSTAND_THIS_MUTATES_A_DISPOSABLE_INSTALLATION'
export KODEX_E2E_PROFILE='web-only'
export KODEX_E2E_RESOURCE_PREFIX='<unique-lowercase-slug>'

npm run test:e2e:auth
npm run test:e2e
```

`KODEX_E2E_OWNER_USERNAME` и `KODEX_E2E_OWNER_PASSWORD` обязательны, но их
значения не включаются в этот документ, reporter или evidence.

## 11. Финальный шлюз

- [ ] `FINAL-01` Все обязательные пункты имеют PASS; каждый FAIL связан с
      открытым blocking Issue; каждый NOT RUN обоснован и принят владельцем;
      результат: `________`; evidence: `________________`.
- [ ] `FINAL-02` Повторный Kubernetes readback после E2E не выявил failing Pod,
      failed Job, новых restarts, warning Events или degraded target;
      результат: `________`; evidence: `________________`.
- [ ] `FINAL-03` Все созданные synthetic refs перечислены; решение об их
      сохранении для демонстрации либо удалении записано владельцем; результат:
      `________`; evidence: `________________`.
- [ ] `FINAL-04` Итог связан с exact Git SHA, release lock, render SHA-256,
      Playwright HTML report и Issues дефектов; секреты не раскрыты; результат:
      `________`; evidence: `________________`.
- [ ] `FINAL-05` Владелец получил короткий демонстрационный маршрут:
      OIDC login → onboarding → Проект → сотрудник → Run/artifact → workflow →
      Human Gate → audit → Grafana/Headlamp; результат: `________`; evidence:
      `________________`.

## Проверенная документация

- `GOV-DOC-003` — классификация PASS/FAIL/NOT RUN и граница browser E2E.
- `RUN-MC-002` — порядок fresh installation и обязательный readback.
- Playwright `/microsoft/playwright/v1.61.0` через Context7 — automatic
  fixtures, fixture teardown и события `console`, `pageerror`,
  `requestfailed`, `response`.
