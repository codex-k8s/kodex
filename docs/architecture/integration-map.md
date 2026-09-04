---
id: ARCH-MC-005
title: Карта интеграций
type: architecture
status: approved
owner: architect
version: 1.5.0
updated: 2026-09-04
---

# Карта интеграций

## Типы интеграций

### Локальные инструменты окружения роли

Docker image роли предоставляет локальное ПО, необходимое для обработки данных:
языковые runtime, офисные пакеты, OCR, браузер, компилятор, CLI и другие
инструменты без внешних полномочий. Наличие бинарника в image не выдаёт агенту
доступ к внешней системе.

Примеры:

- локальные инструменты языка и сборки;
- обработка PDF, изображений и офисных документов;
- браузер для разрешённого публичного контента без учётных данных;
- CLI для локального преобразования и проверки файлов.

External token, kubeconfig, provider credential, registry credential и
пользовательский secret в role Pod не монтируются. Authenticated доступ к
внешней системе всегда требует `IntegrationConnection`, exact capability и
`IntegrationGrant`; согласование определяется server-owned risk policy.

### Управляемая интеграция MCP

Агент получает только MCP endpoint с областью одной сессии. Шлюз интеграций владеет учетными данными и выполняет инструмент после проверки прав, риска и согласования.

Примеры:

- запись/проведение документа в 1С;
- отправка договора или письма клиенту;
- изменение сделки CRM;
- финансовая операция;
- изменение промышленного кластера Kubernetes;
- изменение настроек Kodex.

## Материализация MCP в runtime

Перед каждым turn объект `RuntimeRevision` собирает разрешённые Agent привязки
MCP, версии capabilities/grants и bounded policy. Provider adapter обязан
материализовать их до запуска или возобновления provider session и не может
добавить tool, отсутствующий в revision.

Первый Codex adapter записывает каждую привязку отдельной именованной секцией
`[mcp_servers.<stable_name>]` в сгенерированный `config.toml` перед app-server
`thread/start` либо `thread/resume`. Это adapter detail: Codex thread ID и TOML
не являются core domain fields.

Для секции задаются транспорт, endpoint или команда, `required`, разрешенные инструменты, безопасные ссылки на заголовки из переменных окружения, `startup_timeout_sec` и `tool_timeout_sec`. Значения секретов в TOML не записываются. `tool_timeout_sec` устанавливается в максимальное разрешенное установленной версией Codex и политикой платформы значение, чтобы обычные долгие операции не обрывались преждевременно.

Тайм-аут не является механизмом многодневного ожидания решения человека. Такие действия используют долговечное продолжение сессии, описанное ниже.

### Интеграция чтения и контекста

Интеграция предоставляет ограниченный поиск и чтение данных. Выдача ограничивается областью, пагинацией и бюджетом контекста.

## Пакет интеграции

Версионируемый пакет YAML содержит:

```yaml
apiVersion: integrations.kodex.io/v1
kind: IntegrationPackage
metadata:
  key: synthetic
  version: 1.0.0
  origin: SHIPPED
spec:
  name: Synthetic HTTP
  description: Детерминированный HTTP journal для проверки lifecycle.
  category: testing
  adapter: SYNTHETIC_HTTP
  configurationFields:
    - key: journal
      type: STRING
      required: true
      maximumLength: 120
  capabilities:
    - key: synthetic.journal.read
      operation: synthetic.journal.read
      risk: READ
      approvalPolicy: NONE
      resourceScope:
        kind: SYNTHETIC_JOURNAL
        connectionFields: [journal]
      inputFields: []
```

Канонический контракт находится в
`contracts/integrations/v1/integration-package.schema.json`; новые adapter и
resource kind добавляются только вместе с закрытой реализацией loader/gateway.
Пакет не содержит значений секретов. Поля проходят строгую проверку schema и
семантических инвариантов, а canonical representation получает SHA-256 digest.
Декларативные MCP definitions дополняются разделением организаций,
авторизацией, typed grants, аудитом и server-owned Human Gates, доступными в
Control Center независимо от interaction adapters.

В MVP встроенные `IntegrationDefinition` остаются частью bootstrap catalog и не
зависят от магазина пакетов. В будущей модели `ARCH-MC-012` пакет приложения
может доставить одну или несколько definitions как типизированную capability.
Установка такого пакета не создаёт `IntegrationConnection`, не заполняет
секреты и не выдаёт grant автоматически: connection lifecycle, проверка
capability и исполнение effect остаются в существующей интеграционной границе.

## Карта внешних систем

| Система | Роль | Базовый режим |
| --- | --- | --- |
| Mattermost | Optional inbound, notifications, result mirror и Human Gate decisions | Typed interaction adapter |
| OpenAI Codex | Первый поставщик среды выполнения агента | Адаптер поставщика и device-code авторизация |
| OpenAI Audio Transcriptions | Русское STT для Control Center | `stt-tts-service`, `gpt-transcribe`, exact HTTPS через egress-gateway |
| GitHub | Репозитории, Issues, PR и рецензирование | Типизированный управляемый MCP adapter по exact grant |
| Kubernetes | Среда платформы и целевых проектов | Kubernetes платформы — внутренний runtime boundary; целевые кластеры — типизированный MCP adapter по exact grant |
| Электронная почта | Прием и исходящая коммуникация | Управляемый MCP |
| CRM/1С | Бизнес-операции | Управляемый MCP с согласованиями |
| OCI registry | Образы платформы и ролей | Адаптер цепочки поставки |

## Исходящий HTTPS-транспорт platform gateway

Provider clients role runtime и typed adapters `integration-gateway`, которым
нужен изменяемый SaaS address set, используют
`egress-gateway.kodex-system.svc.cluster.local:8080` как HTTP proxy.
Этот же exact URL поддерживает только bodyless `GET /readyz` без query для
совместимости management readiness: `204` требует фактически ACTIVE policy и
validated resolver; `503` означает закрытый отказ. Technical readback остаётся
на отдельном monitoring-only Service `:9090`, consumer к нему не допускается.
`NO_PROXY` сохраняет внутренние `.svc` и `.svc.cluster.local` calls внутри
кластера. Consumer `NetworkPolicy` разрешает только точные endpoint Pod labels
`app.kubernetes.io/name=egress-gateway` и
`app.kubernetes.io/component=platform-egress` на `8080/TCP`; объект Service и
request fields не являются authority.

Gateway допускает только exact policy FQDN на `443`, требует совпадения
CONNECT authority и фактического ClientHello SNI, запрещает ECH и выполняет
server-owned A/AAAA resolution с TTL/CNAME/special-purpose validation. TLS
остаётся end-to-end, поэтому application credentials и проверка сертификата
не переходят к gateway.

STT adapter допускает только `POST https://api.openai.com/v1/audio/transcriptions`
с end-to-end TLS. Multipart содержит проверенный файл, server-pinned
`model=gpt-transcribe` и `language=ru`; произвольные provider-параметры из
browser отсутствуют. API key выдаёт `secret-broker` краткоживущей projection
для exact actor/tenant/config/account generation и не получает
`egress-gateway`.

## Контракт согласования

Каждая возможность имеет класс риска:

- `read` - согласование обычно не нужно;
- `write` - определяется политикой;
- `external_communication` - согласование по шаблону или получателю;
- `destructive` - обязательное согласование;
- `financial` - обязательное согласование и дополнительные метаданные аудита;
- `platform_admin` - обязательное согласование либо явно разрешенный аварийный профиль.

Согласование показывает инициатора, цель, инструмент, безопасные аргументы, риск, срок действия и ожидаемый эффект. Секретные аргументы маскируются до сохранения запроса.

## Долговечное ожидание согласования

1. MCP-вызов создает `ToolInvocation` и при необходимости `ApprovalRequest` в одной транзакции.
2. Если решение нельзя получить в пределах обычного хода, MCP немедленно возвращает структурированный `pending` с непрозрачным идентификатором вызова.
3. Runner завершает ход и переводит сессию в `waiting_approval`; до реализации
   session archive/restore в #1002 session PVC сохраняется и не удаляется.
4. Решение человека и выполнение операции сохраняются идемпотентно независимо от наличия pod.
5. Событие outbox ставит ход продолжения в очередь той же сессии и той же учетной записи поставщика.
6. Контроллер среды выполнения использует сохранённый session PVC, заново
   генерирует актуальный provider config, возобновляет exact server-owned
   provider session и передаёт доверенный структурированный результат исходного
   вызова инструмента. Codex adapter использует `config.toml` и app-server
   `thread/resume`. Восстановление после удаления PVC появится только в #1002.
7. Отклонение, истечение срока и ошибка продолжают сессию тем же способом и не теряются при перезапуске сервисов.

Открытый сетевой запрос и живой pod не удерживаются с пятницы до понедельника. Корреляция выполняется по неизменяемому идентификатору вызова и хешу аргументов; подменить результат другого вызова нельзя.
