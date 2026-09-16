---
id: GUIDE-DOC-LOCAL-DEV-001
title: Локальный запуск Kodex
type: guide
status: approved
owner: manager
version: 1.0.0
updated: 2026-09-16
---

# Локальный запуск

Точные параметры доступны через `./dev.sh identity --help` и `--help`
соответствующего helper. Перед запуском проверяются локальный kubeconfig,
принадлежность существующих ресурсов и приватный каталог состояния.
Удалённый hot reload описан отдельно в `docs/runbooks/remote-hot-reload.md`.

## Отдельный этап SSO

Из корня клона после безопасной загрузки `KODEX_LOCAL_OWNER_USERNAME`,
`KODEX_LOCAL_OWNER_EMAIL` и `KODEX_LOCAL_OWNER_PASSWORD`:

```bash
./dev.sh identity \
  --kubeconfig /home/s/.kube/kodex-dev-local \
  --context default \
  --state-directory /home/s/.local/state/kodex-local-trusted \
  --management-surfaces control-center
```

Этап использует штатные bootstrap, генерацию материалов и настройку
Keycloak. Он не требует provider authorization или сборки runner, не
разворачивает прикладные сервисы и не доказывает вход через Control Center.
Результат этого этапа нельзя обозначать как успешную приёмку приложения.

`--management-surfaces control-center` исключает чтение и запись Secrets
Grafana/Headlamp и настройку их OIDC clients. Режим `all` сохранён для
полного контура; его нельзя применять к общим ресурсам без проверки
принадлежности. Параметр поддерживается также командами
`materialize-identity-secrets.sh` и `configure-keycloak.sh`.

После успешной генерации файлов сохраняется отдельный
`generated-material-contract-revision.json`. Он разрешает продолжить
bootstrap с теми же материалами, но не означает завершения настройки
NATS или успешного развёртывания. Несовпадение контракта не приводит к
удалению namespace или материалов: требуется отдельное решение о восстановлении.

## Локальный HTTPS и системное доверие

После bootstrap локального CA, этапа SSO и общего render можно отдельно
выпустить публичный сертификат Control Center и установить существующий CA
в системное хранилище этой машины:

```bash
python3 tools/dev/bootstrap-local-https.py \
  --kubeconfig /home/s/.kube/kodex-dev-local --context default \
  --render /home/s/.local/state/kodex-local-trusted/render-trusted.yaml \
  --ca-file /home/s/.local/state/kodex-local-trusted/kodex-local-ca.crt
```

Helper разрешает только loopback Kubernetes API, проверяет локальные labels,
совпадение CA с кластером и точные публичные ресурсы из render. Он применяет
только сертификат и три ingress Control Center без force-conflicts; SSO уже
должен иметь готовый сертификат. Через `sudo -n` устанавливается только
публичный CA в `/usr/local/share/ca-certificates/kodex-local-ca.crt` и
обновляется системное доверие. Другой существующий CA не перезаписывается.
Chrome получает доверие через NSS на общем этапе `bootstrap-cluster.sh`;
после изменения доверия уже открытый браузер может потребовать перезапуска.

Сертификаты обслуживают `control.127.0.0.1.nip.io` и
`sso.127.0.0.1.nip.io`. Успешный TLS не означает готовности приложения:
до развёртывания backend Control Center может отвечать HTTP 404.
Документация cert-manager по SelfSigned/CA issuer, Certificate и Ingress
проверена через Context7; публичный ключ CA доверяется только локально.

## Исходники hot reload

`render-local.sh --security-profile trusted-cluster --host-uid … --host-gid …`
проверяет non-root identity и read-only source mounts. Helper
`local_hot_reload.py prepare-source-mask` создаёт только отсутствующие
`.agents`/`.kodex-dev`, проверяет существующие `.git` и `.env*` без чтения
содержимого и отклоняет симлинки. Права существующих каталогов не меняются.
Кэш должен находиться вне клона. Успешный render не заменяет проверку
фактических Pod, владельца cache marker и hot reload в браузере.

## Материалы trusted-cluster

`tools/install/materialize-secrets.sh` принимает явные
`--security-profile trusted-cluster --render <общий-render>` и проверяет
профиль render до записи Secrets. Authority projections, bootstrap roots,
authority telemetry trust и runtime execution client TLS не создаются.
Существующие authority-ресурсы не удаляются. Остальные материалы, включая
OIDC, PostgreSQL, NATS и внешнюю цепочку сборки образов, сохраняются.

Для первоначального запуска без provider account доступен
`--provider-mode deferred`, только с явным `trusted-cluster` и без
`--provider-auth-file`. Это не создаёт фиктивную авторизацию и не доказывает
работоспособность запуска моделей. По умолчанию остаются `protected` и
`configured`, требующие настоящий provider authorization file.
Secrets нового профиля получают ownership labels; существующий Secret без
совпадающих labels закрыто отклоняется, а конфликты записи не форсируются.

## Поэтапное развёртывание данных

После загрузки материалов первый этап запускается через штатный deploy:

```bash
KUBECONFIG=/home/s/.kube/kodex-dev-local bash tools/dev/deploy-local.sh \
  --context default --mode apply --security-profile trusted-cluster --stage data \
  --render /home/s/.local/state/kodex-local-trusted/render-trusted.yaml \
  --state-directory /home/s/.local/state/kodex-local-trusted
```

Этап проверяет loopback API, профиль, source/cache mounts и текущий UID/GID.
Он создаёт параметры admission до bindings, затем остальные базовые ресурсы
и четыре StatefulSet: PostgreSQL Control Plane, PostgreSQL email-bridge,
NATS и SeaweedFS. Конфликты не форсируются; существующие ресурсы требуют
локальных ownership labels либо подтверждённого Apply от `kodex-local-dev`
вместе с label `trusted-cluster`. Jobs, PVC и прежние ресурсы не удаляются.
`--mode readback` проверяет этот же этап без повторного apply.

Успех `--stage data` не означает запуска миграций, приложения или готовности
MVP. Полный deploy нового профиля пока закрыто отклоняется; прежний
`protected --stage full` сохранён отдельно.

Следующий этап — тот же вызов с `--stage migrate`: versioned S3 probe,
миграции Control Plane и email-bridge, runtime DB credentials и broker
bootstrap. Job получает суффикс digest своего манифеста; повтор того же
входа ожидает существующую Job, не удаляя её. Изменившийся вход создаёт
новую Job, forward-only миграции повторно проверяют текущее состояние БД.
Runtime DB bootstrap в `trusted-cluster` не ожидает authority roles/schema.

`--stage network --mode apply` применяет только NetworkPolicy из проверенного
render и не перезапускает StatefulSet. Это позволяет доставить точную
недостающую связь уже ожидающей Job. Сам по себе успешный apply не доказывает
отрицательную сетевую проверку или готовность приложения.

`--stage core` запускает восемь основных Deployments. STT подключается отдельно
через `--stage core --workload stt-tts-service` после готовности Control Plane,
secret-broker и egress-gateway; отсутствие STT не задерживает начальный UI
bootstrap. Эта команда использует тот же render, проверку ownership и rollout
timeout, не выполняет платный provider smoke. Readiness не заменяет ручную
проверку voice flow. Для отдельного
обновления допускается `--workload <имя>` из закрытого списка core; остальные
Deployments не применяются повторно. Для init установки CLI secret-broker
профиль использует exact runner digest, уже импортированный локальным
helper. Runtime execution и RoleImage по-прежнему используют свой admission
и promoted image flow.

Текущий Control Plane требует настоящий provider account при начальном
bootstrap организации. Поэтому `--provider-mode deferred` позволяет поднять
данные и UI, но пока не обеспечивает запуск Control Plane и вход в продукт.
Для полного bootstrap владелец указывает `KODEX_LOCAL_PROVIDER_AUTH_FILE` —
путь к приватному auth.json в локальной `.env`; токены не копируются в чат
или Git. `dev.sh` поддерживает это имя, прежнее `KODEX_DEV_PROVIDER_AUTH_FILE`
оставлено для совместимости. Не следует считать доступность оболочки UI
подтверждением login/API/project сценариев.

Air закреплён на `v1.67.4` в `components.lock.json`. Prime размещает бинарь
и receipt в versioned путях `go-tools/air-<version>`: обновление инструмента
не перезаписывает исполняемый файл уже работающих Pod. Runner проверяет
версию и SHA256 из render. Совместимость с прежним `go-tools/air` разрешена
только для старого pin `v1.63.4`, до обновления соответствующего Deployment.
Это обновление инструмента требует rollout Pod, но не сборки application
image; обычные изменения Go/Vue по-прежнему обслуживают Air/Vite.

Причина обновления — локально подтверждённый `close of closed channel` при
`rerun=true` в Air v1.63.4. Сверены Context7 `/air-verse/air`, upstream
release v1.67.4 и код `runner/engine.go`. Отдельно проверяются изменение,
возврат исходника и неизменный restart count после reload; краткая проверка
не заменяет отложенный длительный soak.
