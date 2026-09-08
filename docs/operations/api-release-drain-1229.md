---
id: OPS-DOC-1229
title: Завершение API при независимом релизе
type: operations
status: approved
owner: SRE
version: 1.0.0
updated: 2026-09-08
---

# Завершение API при независимом релизе

Задача #1229, проверки #1223. Профиль предназначен для штатного RollingUpdate,
не обещает сохранение запросов после SIGKILL, потери узла или превышения
объявленного бюджета запроса. Freshness, grants и revocation state не меняются.

## Причина и правило

Прежний API передавал отменяемый сигналом процесса контекст в HTTP BaseContext.
Поэтому Shutdown не мог дождаться уже принятого запроса: его downstream context
отменялся заранее. Кроме того, listener и issuer останавливались без окна
распространения EndpointSlice, а Air принудительно завершал процесс через 2 с
при штатном HTTP shutdown budget 20 с. Это связанные дефекты lifecycle;
одна потеря readiness сама по себе не доказывает ошибку ротации.

Контекст принятого HTTP-запроса сохраняет отмену клиента и штатные deadlines,
но переживает сигнал процесса в пределах HTTP shutdown budget. Истечение
бюджета принудительно закрывает соединения. Затем отменяются оставшиеся request
contexts, включая hijacked WebSocket, и закрываются downstream клиенты.

## Матрица завершения

| Фаза | Действие | Полномочия и ограничения |
| --- | --- | --- |
| До релиза | SRE фиксирует UID/spec, доступные replicas и точные app sources | Выданная runtime identity; никаких полномочий из request payload |
| Terminating Pod | Kubernetes исключает endpoint; preStop приложения 10 с | Старый listener и issuer ещё обслуживают действительные запросы |
| HTTP shutdown | Новые соединения больше не принимаются; принятые завершаются за 20 с | Auth/expiry/revoke продолжают действовать, access не продлевается |
| Превышение бюджета | Соединения закрываются принудительно | Неизвестный исход mutation не повторяется автоматически |
| Остановка issuer | Его preStop 40 с покрывает 10+20 с приложения с запасом | Подписи, ключи и durable state не заменяются |
| Cleanup | Каждый обязательный shutdown/flush имеет собственный budget | Pod grace 120 с; Air kill delay 90 с не обрывает штатный cleanup через 2 с |

`GET /api/v1/session` сохраняет существующий путь browser SSO/session boundary →
авторитетная session family → типизированный ответ. Периодический
`PUT /api/v1/session` использует прежний защищённый refresh и cookie readback.
Новых RPC, бизнес-переходов, domain events или источников authority этот профиль
не вводит; откат приложения не откатывает историю полномочий.

## Совместимость и однократная миграция

Используется Kubernetes `LifecycleHandler.sleep`: он выполняется kubelet и не
требует shell/sleep в distroless образе. Поддерживаемый профиль — Kubernetes и
kubelets 1.34+, где действие стабильно. Скрипт проверяет это до PATCH.
Источник: [официальный реестр feature gates](https://kubernetes.io/docs/reference/command-line-tools-reference/feature-gates/).
Порядок preStop/SIGTERM проверен через Context7 по
[Container Lifecycle Hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/).

На уже работающем disposable staging отдельно от обычного application release:

```bash
node tools/release/control-api-drain-profile.mjs \
  --context staging --evidence /private/api-drain-profile.jsonl \
  --confirm APPLY-STAGING-API-DRAIN
```

Меняются только два собственных preStop, Pod grace и аннотация профиля API.
Неизвестные существующие hooks или другой shutdown budget закрыто отклоняются.
Перед PATCH создаётся приватный durable journal; неизвестный исход не повторяется.
После перехода обычный `scoped-release.mjs` обновляет только приложение.

## Проверки перед приёмкой

- [ ] Локальные HTTP lifecycle tests: завершение принятого запроса, отмена клиента, принудительное закрытие по timeout.
- [ ] Kustomize render содержит preStop 10/40 с и grace 120 с.
- [ ] Однократная миграция профиля завершена и выполнен exact readback.
- [ ] Повторные одиночные релизы API, группа API/PWA и адресный rollback не меняют соседей или security sources.
- [ ] Авторизованные HTTP-пробы включают успешный natural refresh и не скрывают 5xx повторами.

Пробы запускаются `tools/dev/release-http-acceptance.mjs --expected-sha <tool-sha>
--seconds 1200` с выданными `KODEX_E2E_*` env и свежим private storage state.
Тот же storage state не используется другим процессом одновременно. Поля
журнала ограничены временем, закрытыми endpoint/method/status/content-type и
агрегатами. Bodies, cookies и token values не сохраняются. Неопределённый
результат refresh останавливает проверку без повторного PUT. Ошибка плановой
HTTP-пробы остаётся FAIL независимо от последующих успешных запросов.
