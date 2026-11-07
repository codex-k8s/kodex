---
id: OPS-DOC-NATIVE-INTEGRATION-EGRESS-2040
title: Опциональные точные маршруты native-интеграций
type: operations
status: draft
owner: backend
version: 1.0.0
updated: 2026-11-07
---

# Доступ native-интеграций к внутренним API

Источник: [#2040](https://github.com/codex-k8s/kodex/issues/2040), приёмка
[#1830](https://github.com/codex-k8s/kodex/issues/1830).
Unit включает egress-gateway, выбор transport в integration-gateway, общий
контракт, render/deploy, метрику и сквозную проверку двух процессов.
Миграций БД нет; Definition/Connection/Capability/Grant, личные credentials,
Human Gate и lifecycle внешних эффектов сохраняются.

## Совместимость и границы

По умолчанию дополнительных listener, Service и ConfigMap нет. Native
GitLab/Jira/Confluence используют прежний transport. Bundled/external OIDC,
Codex/OpenCode и выбранный модельный provider независимы от этого параметра.
OpenAPI, GitHub, Context7 и почтовые запросы не переводятся на новый маршрут.
Включённый профиль применяется ко всему native-каталогу, включая Server/DC,
catalog helpers, вложения, credential health и наблюдения за правами.

Новый listener8085 доступен только integration-gateway в `kodex-system`.
Набор upstream задаёт оператор установки через immutable Git-owned JSON;
Connection, модель, prompt и ответ API его не расширяют. Допуск сетевого
адреса не выдаёт прикладных прав и не подтверждает пригодность PAT.

Gateway сверяет CONNECT Host,443, TLS ClientHello/SNI и точный operator pin.
Только этот отдельный профиль допускает RFC1918/ULA; loopback, link-local,
metadata, multicast, mapped IPv6, неопределённые и широкие диапазоны запрещены.
Публичные адреса проходят прежнюю проверку специальных диапазонов.
DNS не заменяет pins: оператор обновляет их явным новым поколением после
проверки владельца назначения. Общая public-DNS policy остаётся прежней.

TLS проходит сквозь CONNECT; egress-gateway не получает PAT и не завершает TLS
API. integration-gateway проверяет сертификат, hostname и системный CA bundle,
сохраняет TLS1.3 и запрет redirects. Корневой CA при необходимости подключают
штатным проверенным bundle при старте процесса. `InsecureSkipVerify` не нужен.

Отдельный ConfigMap ограничен64KiB,64 именами и32 уникальными точными адресами
на имя. Schema, поля, duplicate keys, trailing JSON, generation, base digest
и digest документа проверяются до активации. Ошибка optional файла закрывает
native listener через readiness-only fallback с общим бюджетом соединений;
core listeners продолжают работать. Ошибка полной process configuration
по-прежнему отклоняется при старте.

## Входы оператора

Следующий пример полностью синтетический; его нельзя применять без собственной
проверки адресов. Имена и массивы pins отсортированы. `gateway_policy_digest`
берут из окончательного render **после** настройки OIDC и model provider.

```json
{
  "schema": "egress-native-integration/v1",
  "generation": 1,
  "gateway_policy_digest": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "destinations": [
    {"hostname":"gitlab.example.invalid","port":443,"address_cidrs":["10.77.0.4/32"]},
    {"hostname":"tracker.example.invalid","port":443,"address_cidrs":["fd77::4/128"]}
  ]
}
```

Обычную команду `tools/release/render-web-only.sh` дополняют двумя аргументами:

```sh
--native-integration-configuration /absolute/private/native-egress.json \
--native-integration-configuration-sha256 <sha256-точных-байтов-файла>
```

Это новые опциональные входы render. SHA файла защищает вход оператора; digest
typed документа и label поколения вычисляет renderer. Секретов в JSON нет.
Release lock и source SHA остаются обязательными обычными входами поставки.

Render добавляет:

- immutable `egress-gateway-native-<digest>` с `policy.json` и полным digest;
- Service `egress-gateway-native` с selector конкретного поколения;
- вход8085 только от exact namespace+pod peer integration-gateway и ответное
  egress-правило от integration-gateway к egress-gateway;
- точные /32,/128 upstream rules на TCP443;
- listener/file/digest env и read-only volume egress-gateway, а также
  `INTEGRATION_GATEWAY_NATIVE_PROXY_URL` с единственным фиксированным
  `http://egress-gateway-native.kodex-system.svc.cluster.local:8085`.

Два selector в одном NetworkPolicy peer означают AND. Renderer проверяет полный
**desired** union правил8085 для выбранных Pods и отклоняет дополнительный
широкий доступ. Kubernetes объединяет политики аддитивно: точные новые
upstream rules не сужают существующее исключение core gateway на TCP443.
Именно gateway обеспечивает допуск точного назначения; нельзя объявлять эти
правила полной L3-изоляцией upstream. Перед apply оператор также проверяет
полный **live** union, CNI, DNAT и отсутствие посторонних выбирающих политик.

## Установка и lifecycle

1. Закрепить совместимые immutable images обоих gateway в release lock.
   Весь candidate и входы остаются неизменными в течение прогона.
2. Сохранить preimage/backup и подтвердить target, отсутствие активных
   integration claims/turns и незавершённых внешних эффектов. Смена pins не
   отменяет уже совершённую запись во внешнем API.
3. Выполнить штатные `portable-release.sh` plan/preflight и fresh apply либо
   квалифицированный upgrade. `deploy-platform.sh` всегда проверяет native
   assets по полному render. Отдельный shell patch внутри Pod не нужен.
4. Foundation содержит immutable ConfigMap, правила и Service **до** Deployment.
   Новый selector исключает старое поколение из новых соединений, даже при
   maxUnavailable0. Дождаться endpoints нового поколения, завершения rollout и
   закрытия старых соединений. Стандартный bounded drain/cancel/join сохраняется.
5. Проверить точные image digests, projected digest, Pod generation,
   Service/EndpointSlice, полный live NetworkPolicy union и `/readyz` через8085.
   Затем выполнить обычную личную проверку ключа и управляемую операцию из роли.
   Зелёная сетевая готовность сама по себе не означает готовности интеграции.

Начальное включение изменяет набор assets и оба workload, поэтому не считается
произвольным `resume-workloads` прежнего bootstrap. Его exact-delta gate не
обходят. Обычный resume уже совпадающего render не меняет policy generation.

При ошибке proxy, файла, Host/SNI, CA либо pin нет fallback к8080, прямому HTTP
или другому ключу. GET сохраняет прежние ограниченные повторы; WRITE по-прежнему
выполняется один раз, UNKNOWN согласуется только штатным readback. Общие
лимиты соединений, deadline, shutdown, quotas и receipt semantics не меняются.

Отключение выполняют новым закреплённым render без профиля после quiescence и
совместимым rollout обоих gateway. Без native env новый процесс не слушает8085;
остаточный Service не получает endpoints. Устаревшие assets автоматически не
prune-ятся: удаление только точных прежних optional объектов планируют после
readback отсутствия endpoints/соединений. Возврат images требует совместимости
со схемой текущего candidate; этот unit не разрешает DB downgrade.

Метрика `kodex_egress_gateway_native_integration_ready` существует только при
включённом listener и отражает корректность проекции. Она не содержит host/IP,
пользователя, PAT и не заявляет доступность API. Стандартные closed outcome
метрики CONNECT/dial/tunnel отражают отказы без payload.

## Проверки и ручная приёмка

`make test-native-integration-egress` проверяет render обоих профилей и запускает
synthetic integration adapter→реальный CONNECT gateway→HTTPS API в отдельных
test binaries. Фиктивный pin отображается только test-only dialer на loopback;
такого dialer нет в поставляемом binary. Проверяются exact proxy, credential
binding, TLS CA/SNI, недоверенный сертификат, redirects и отсутствие fallback.
Тестовые процессы получают очищенное окружение.

Дополнительные Go tests проверяют strict document, generation, special ranges,
неизменяемость route, Host/SNI, shared capacity, invalid optional startup,
весь native catalog и личный credential probe. Прежние public OpenAPI и model
provider тесты остаются регрессией. Renderer тестирует additive broad rules и
подмену Service, env, mount, namespace, pins и base digest.

На установленном candidate отдельно подтвердить каждую нужную вертикаль
роль→tool→gateway→API→receipt/audit с персональным ключом и ограниченной областью.
Нужны READ и согласованный synthetic WRITE, pagination/401/403/timeout,
UNKNOWN reconciliation, restart и проверка отозванного ключа. Прямой HTTP smoke
и этот локальный тест не заменяют такую приёмку. До живой проверки строка
остаётся NOT RUN/BLOCKED, даже если PR слит и Pods готовы.

Через Context7 проверены официальные документы Kubernetes NetworkPolicy:
[AND selectors и additive policies](https://kubernetes.io/docs/concepts/services-networking/network-policies/).
При деплое всё равно проверяют фактическую версию API/CNI: наличие документации
latest не подтверждает сетевое поведение конкретного кластера.
