---
id: OPS-MVP-COMPLETION-20261016
title: Завершение прототипа после демонстрации
type: operations
status: approved
owner: manager
version: 1.0.0
updated: 2026-10-16
---

# Завершение прототипа

План утверждён владельцем16.10.2026. Основная приёмка: #1031/#1223;
публичная установка: #1810/PR#1811; почтовый дефект: #1812/PR#1813;
сквозной сценарий агентов: #1797; установка: #1791.

## Этапы

- [x] Восстановить ядро, внешний HTTPS/SSO и закрепить восстановление после
  перезапуска. Не менять данные, сертификаты и соседние маршруты.
- [ ] Согласовать исходники, развёрнутые образы и release evidence; сохранить
  уже применённый почтовый фикс и публичную конфигурацию.
- [x] Вернуть выборочный hot reload application code, сохранив внешний вход;
  доказать реально смонтированные исходники и работающую версию.
- [ ] Завершить реальный пользовательский сценарий: проект, окружение,
  сотрудник, выполнение, файлы, продолжение, делегирование и Human Gate.
  В dogfooding — один цикл комплексного ревью, без повторных автоматических
  циклов и без подтверждения внешних действий за владельца.
- [ ] Подтвердить Stop/retry/queue, параллельность, rejoin/restart и изоляцию
  пользователей/проектов. Исторические результаты не выдавать за свежую проверку.
- [ ] Закрыть серверную границу допуска файлов: карантин, точные байты,
  обязательные проверки и fail-closed. Формат PDF не доказывает антивирусный допуск.
- [ ] Подтвердить сохранность сессий и реальное восстановление резервной копии
  в изолированное место. PostgreSQL/NATS/S3 остаются одиночными экземплярами.
- [ ] Проверить повторяемую установку/обновление и onboarding; выполнить
  итоговую применимую проверку и передать владельцу рабочий MVP с честными
  PASS/FAIL/NOT RUN и актуальными задачами.

## Режим работы

Владелец разрешил автономное исполнение утверждённого плана в существующем
контуре. Прямой push в main, подмена решений владельца, ручное изменение
статусов БД и ослабление TLS/SSO/сетевых ограничений не допускаются.
Реальная отправка письма требует отдельного решения в платформе.
Chrome MCP: собственная вкладка, получение списка вкладок каждые10 минут,
визуальная проверка изменяемых экранов, Console и relevant Network.
Приватные параметры установки и значения реквизитов в журнал не включаются.

## Журнал

### 16.10: исходное состояние

- Основной checkout main `8c19ffb76328ae559698fae2806be79ef0bd64ae`, чистый.
- Четыре компонента control-plane/runtime-controller/secret-broker/session-archive
  недоступны после перезапуска; exact Kubernetes API egress содержит прежний
  адрес узла, не совпадающий с текущим ready EndpointSlice.
- Публичные корень и bootstrap возвращают HTTP502. Установленные relay socket
  units включены в автозапуск, но inactive. Маршруты и сертификаты не изменялись.
- PASS: repo-owned kubernetes-api-egress.sh validate/apply для четырёх exact
  workload selectors. Добавлены только Service IP/порт и ready API endpoint/порт;
  wildcard egress отсутствует. Это оперативное восстановление; автоматическое
  обновление после следующей смены адреса пока NOT RUN/не реализовано.
- Первая реплика control-plane и runtime-controller восстановились без смены
  образов и данных; полный readiness и внешний вход пока NOT RUN.

### Восстановление публичного relay

В PR#1811 добавлен режим `edge-release.mjs recover`: требуется точный hash
исходного owner-approved плана и успешный apply journal. До запуска socket
проверяются cluster/node/CA identity, protected commitment, все resource UID,
полные unit-файлы и enabled state. Ресурсы, маршруты, unit-файлы и сертификаты
не перезаписываются. После эффекта выполняется обычный exact readback.
Результаты тестов и живого применения фиксируются следующим checkpoint.

### 16.10: восстановление API и подготовка exact peer rebind

- PASS: все четыре затронутых Deployment восстановились до2/2 без смены
  образов, удаления Pod и изменения данных. Отдельный readback четырёх
  текущих exact API policies PASS.
- PASS LOCAL на `0728f2f18b8d972b979118eb537f0bd995889eed`:
  восемь edge-release tests, включая восстановление inactive sockets и
  отрицательные проверки replaced UID/изменённых unit-файлов.
- FAIL/CLOSED: живой recover остановился до первого эффекта на
  `EDGE_PEER_NODE_DRIFT`. После перезапуска прежние exact Docker identities
  внешнего ingress получили другие IP. Исторические разрешения не расширялись.
- Добавлен forward-only `edge-rebind.mjs plan|apply|readback`. Новый приватный
  план выводит адреса из прежних Docker IDs и общего прежнего bridge gateway;
  связывает текущий protected snapshot соседнего контура, resource UID receipts,
  CA и полные before/after unit bodies. Не меняет Kubernetes resources, TLS,
  маршруты и исходный apply journal. Старые адреса удаляются из четырёх units,
  новые разрешаются только как точные /32. Привилегированный worker проверяет
  все preimage до записи и атомарно заменяет только закрытые четыре имени.
- PASS LOCAL на рабочем дереве этого checkpoint: пять targeted rebind/worker
  tests, включая чужой Docker ID, остановленный узел, другой bridge/network,
  публичный/loopback/повторный IP, symlink/hardlink/path traversal, точное
  возобновление и сохранение0644 при атомарной замене.
- PASS: read-only rebind plan. Apply/readback/public HTTPS пока NOT RUN.

### 16.10: публичный вход восстановлен

- PASS на executor `e2e1735e420784b998cc1b0b67d544c4726d35c0`: rebind apply и
  отдельный readback. Все четыре relay units соответствуют новому плану,
  sockets active/enabled, прежние node/container identities и CA сохранены.
  Protected snapshot соседних ресурсов неизменен относительно нового плана.
- PASS: публичный корень302 в SSO, OIDC discovery200, bootstrap без сессии401.
- PASS: отдельный штатный вход владельца и четыре последовательных reload
  в собственном Playwright Chrome с проверкой TLS. Bootstrap/session/ticket200,
  пять WebSocket соединений, проекты видны, состояние «Подключено».
  Два начальных401 до SPA login ожидаемы. Ошибок JavaScript и MIME в этом
  прогоне нет; прежний плавающий MIME-дефект не объявляется окончательно закрытым.
- Chrome MCP: собственная вкладка видит штатную форму SSO; одноразовый
  защищённый ввод не завершился и истёк без выдачи credentials. Зависший вызов
  прерван; браузерная авторизованная проверка выше относится к Playwright,
  а не к MCP. Ни trace/HAR, ни значения credentials не сохранялись.
- Для coherent dev source переносится mail fix `1989291611ee807c3078af32a9593737b6800aa6`
  в текущую PR-ветку. В конфликте import helper сохранены защита exact digest
  alias от перезаписи и прежний registry-pull-authorizer; добавлен только
  closed repository control-plane. Прямого push/merge в main нет.

### Подготовка двухрепличного Go hot reload

- Добавлены repo-owned restore-hot-go plan/apply/readback для control-plane
  и API gateway. Сохраняются две реплики, текущая RollingUpdate strategy,
  node selector, application env, sidecars/init, PVC/Secret/config references.
  Исторический inverse используется только для dev toolchain/source/cache;
  итоговое намерение заново проверяется моделью до apply.
- Air config и выходной binary теперь могут использовать exact Pod UID:
  две реплики не пишут в один runtime directory, Go build cache остаётся общим.
- PASS LOCAL: четыре model/instance tests, включая отказ при foreign UID,
  writable source, подмене application env/sidecar/storage и path injection.
- Первый cache contract запуск FAIL: default host Go1.27.1 не соответствует
  закреплённому toolchain. Повтор с уже установленным Go1.26.6 PASS:14 cache
  tests и EMAIL entrypoint/Secret bootstrap contracts. Глобальный Go не менялся.
- Disposable PostgreSQL на source `ccbda6697a6d5c086ae4309d13d18e0f393d8641`:
  migrations/repeated up PASS; email configuration/authorization regression,
  watermark и immutable credentials PASS. Весь выбранный email contour FAIL:
  email receipt reconciliation fixture получает conflict при LaunchRun.
  Этот FAIL не скрыт и должен быть устранён до итоговой приёмки.
- Hot reload apply и serving source proof пока NOT RUN. Публичный frontend
  пока продолжает отдавать compiled bundle через прежний Nginx.

### 16.10: Go hot reload активирован

- PASS: source `eac653967423592b14678d53a104571125b456c1` содержит публичную
  конфигурацию и почтовый fix. Подготовлен отдельный tracked-only clone с
  пустыми private mountpoints; исходный main не изменялся.
- PASS: repo-owned dry-run/plan/apply и отдельный spec readback для CP/API
  gateway; оба завершили rolling replacement с desired/updated/available2.
  Runtime controller, secret broker, archive и frontend не менялись.
- PASS: две CP реплики читают один exact SQL digest исправления, имеют пустой
  masked .env и разные exact Pod UID/Air runtime directories. Монтирование
  source read-only; ключи и Git metadata скрыты.
- Источник дальнейших правок теперь один: checkout, реально подключённый к Pod,
  а не первоначальный worktree PR. Та же branch/PR; начальный source SHA указан
  выше. Новые checkpoint записывают текущую revision и actual readback.
- Найдено несоответствие служебной root metadata: template уже hot-reload,
  Deployment label ещё stable. Вторая ревизия restore plan синхронизирует
  только собственный marker с CAS и поддерживает последующие source checkpoints
  без повторного применения исторического inverse. Старые v1 планы не декодируются
  новым исполнителем; их уже полученное evidence остаётся историческим.

### Предотвращение повторения потери связности

- Подтверждена первичная причина inactive relay после загрузки: systemd явно
  удалил start двух socket jobs из-за ordering cycle
  socket → network-online → basic → sockets.target → socket.
- Исправлена factory генерация новых socket units: DefaultDependencies=no,
  явный shutdown ordering и multi-user.target. Для установленного состояния
  подготовлен отдельный immutable forward boot plan с точными before/after;
  исходные журналы установки не переписываются.
- Добавлен операторский API reconciler: closed set из пяти base policies и
  четырёх созданных recovery policies, exact cluster/namespace/policy UID,
  ready control-plane EndpointSlice и JSON Patch с UID/RV/full-spec precondition.
  Он удаляет устаревший API IP, сохраняя все остальные ingress/egress rules.
- Добавлена периодическая сверка public relay: прежние exact Docker IDs и
  network IDs, canonical полные unit preimage, только текущие exact /32.
  Частично заменённые canonical файлы после crash не требуют подмены истории;
  неизвестные unit body, дополнительный drop-in или другой node/network закрывают
  восстановление. Никаких изменений Kubernetes ресурсов соседнего контура.
- Установка выполняется от штатного оператора через отдельные user timers,
  без переноса credentials в агента. Операторский linger обеспечивает startup
  после reboot без интерактивного входа. Внешний relay использует прежний
  ограниченный worker для четырёх exact root unit filenames.
- PASS LOCAL на рабочем дереве checkpoint:12 model/worker tests. Live boot
  transition, timers и фактический host reboot пока NOT RUN.

### 16.10: восстановление закреплено; исправлена оснастка почтового regression

- PASS на executor `36190190b3b98928d8eb0f376d1f41d9e9d3edc8`: forward boot
  apply и отдельный readback; оба root relay socket active/enabled,
  DefaultDependencies=no, shutdown ordering, включение в multi-user.target.
- PASS: установлены два независимых operator user timers с периодом60s;
  первый API проход обновил пять base policies, следующий — changed0.
  Relay controller прошёл два последовательных цикла без изменения canonical
  units. Linger включён только для текущего оператора. Значения credentials
  и копии kubeconfig не создавались. Runtime агентам права не выдавались.
- PASS: systemd dependency verification для двух sockets; выданы только
  посторонние предупреждения об устаревшем CPUAccounting в системных XFS units.
  PASS: публичный корень302 в SSO и API401 без авторизации. Полный host reboot
  NOT RUN: он затронул бы соседний контур и не нужен для проверки кода исправления.
- PASS LOCAL на executor36190190b:8 edge-release CLI tests. Вместе с12
  targeted model/worker tests подтверждены closed drift/UID/unit/path guards;
  это не blanket full MVP acceptance.
- Причина FAIL email receipt fixture: адресный запуск пытался LaunchRun до
  свежего наблюдения каталога модели, которое ранее создавал соседний сценарий.
  Исправлена только оснастка: явный штатный seed owner catalog tasks;
  production eligibility и policy не ослаблялись.
- PASS на рабочем дереве этого checkpoint с Go1.26.6: адресный receipt test,
  затем весь `TestBootstrapComponent/email` на fresh disposable PostgreSQL:
  reconciliation, watermark, exact authorization/mailbox Human Gate и immutable
  credentials. Fresh migrations, repeated up и read-only worker/runner queries PASS.
- PASS: штатный fresh owner SSO/SPA login после исправлений, bootstrap200 и
  provider accounts200; account AUTHORIZED/enabled. Это ещё не доказательство
  нового модельного выполнения или полной готовности провайдера.

### 16.10: изоляция диалогов и серверный AV-допуск

- PASS LOCAL на `bd17c576e5f3dc562783e3eeab6a4b6578ca0784`: fresh disposable
  PostgreSQL — `TestAssistantParallelLifecycleComponent`,
  `TestProjectAssistantProfilesComponent`, `TestAssistantSystemRunProjectScopeComponent`.
  Это синтетическая lifecycle/authority проверка, не реальные ИИ-запуски.
- Для следующего полного backend unit создан #1814 и план
  `OPS-ARTIFACT-ANTIVIRUS-1814`. Изменения изолированы от живого hot source.
  Все upload/avatar/runtime/prepared paths получают серверный потоковый AV;
  evidence хранится в той же immutable object receipt, без нового FK-графа.
- PASS LOCAL на рабочем дереве AV checkpoint: весь unit-набор Control Plane,
  web-only render, потоковый scanner protocol, closed scanner rollout model
  и migration CLI с точным новым профилем. Изменённых внешних OpenAPI/Proto/
  AsyncAPI контрактов нет; новое evidence не принимается из transport payload.
- PASS LOCAL на том же рабочем дереве: fresh PostgreSQL upload/quarantine,
  missing/mismatched proof, parallel preview, immutable revisions, prepared
  content stage/replay/expiry/purge, avatar lifecycle, file bindings, skills,
  nested delegation, Human Gate и session archive. В тестах использован явно
  синтетический scanner, который не считается живым AV-доказательством.
- PASS LOCAL: upgrade из прежней схемы с сохранением aggregate IDs, байтовых
  receipts, bindings, grants и raw historical scan state. Эффективный scan
  старого format-only CLEAN — QUARANTINED. Retention удаляет точные версии.
- Исправлены test-only зависимости от соседнего model-catalog seed и общая
  singleton-БД avatar fixture: адресные проверки теперь независимы. В avatar
  producer исправлен readback количества колонок immutable revision.
  Production eligibility и scanner failure policy не ослаблялись.
- NOT RUN на этом checkpoint: новый AV rollout, реальные clean/EICAR controls
  в обоих экземплярах, новый browser upload и реальный модельный сценарий.
  До них MVP checkbox допуска файлов остаётся открытым.
