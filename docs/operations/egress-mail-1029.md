---
id: OPS-EGRESS-MAIL-1029
title: Почтовый CONNECT профиль
type: runbook
status: approved
owner: security
version: 1.0.0
updated: 2026-09-05
---

# Почтовый CONNECT профиль

Источник требования: #1029, выбранный владельцем отдельный EMAIL #1037.
`8080` остаётся общим HTTPS listener, `8081` принадлежит STT, `8082` принимает
только EMAIL через exact namespace/Pod selector. Caller headers не выбирают
профиль. Gateway не получает TLS private keys, application credentials или SA
token; он не завершает TLS и не повторяет прикладные операции.

## Контракт и полномочия

| Сценарий | Authority и владелец | Путь и результат |
|---|---|---|
| Конфигурация | Утверждённый оператором typed `email-bridge/v1` document, тот же source/revision/digest, что у EMAIL и CP | `cmd/mail-projection` проверяет schema, endpoints и trusted DNS; создаёт secretless immutable ConfigMap, Deployment patch и exact CNI pins |
| CONNECT | CNI допускает только Pod EMAIL в `kodex-system`; labels назначает deploy owner | Bodyless `CONNECT fqdn:port`, exact Host; `email-mail/email-bridge/email.transport`, digest/revision в response readback |
| Прикладная операция | EMAIL проверяет CP RuntimeWork authority, mailbox scope, operation, continuation/source/fence | SMTP/IMAP/POP исполняет EMAIL; CP RPC, gates, receipts и unknown-outcome не переносятся в egress |
| Implicit TLS | Mail policy + полный публичный DNS snapshot + literal pins | `465/995/993`: ClientHello SNI до dial, затем неизменённые TLS bytes |
| STARTTLS | Те же policy/DNS/pins; точные `587/110/143` | Серверное greeting проходит до TLS; обязательный upgrade и exact CA/hostname проверяет EMAIL, credentials до TLS запрещены |
| Readiness | Immutable policy digest, configured destinations и свежий DNS внутри pins | `GET /readyz` на `8082`: `204` либо `503`, те же profile/revision/digest headers; general/STT readiness независима |

IP literal в CONNECT, wildcard hostname, иные порты/режимы, private/mixed DNS,
новый IP вне pins, malformed/duplicate JSON и неверный digest закрыто
отклоняются. Gateway не фильтрует часть DNS-ответа до разрешённого subset.
Общая DNS-реализация сохраняет CNAME/TTL/bounds и повторную public IP проверку.
Все listeners разделяют общий лимит соединений и bounded cancel/join.
Метрика `kodex_egress_gateway_mail_ready` читает ту же TTL-aware readiness,
без hostname, mailbox ID, IP или source digest в labels. Общие counters
DNS/CONNECT/dial сохраняют закрытую кардинальность и не включают payload.

Mail readback дополнительно содержит
`X-Kodex-Egress-Configuration-Revision` (десятичная source revision) и
`X-Kodex-Egress-Configuration-Digest` (`emailbridgeapi.Digest(Configuration)`).
EMAIL сравнивает эти значения со своим immutable document до credentials;
revision/digest из request не являются источником полномочий.

## Проекция и смена конфигурации

`tools/render-egress-mail.sh staging|production <image-digest> <registry-fqdn>
<mailboxes.yaml> <trusted-resolv.conf>` выполняет только локальную генерацию.
Непустой source требует DNS-разрешения его утверждённых hosts; команда не
вызывает Kubernetes API, не устанавливает ресурсы и не подключается к mail
provider. Результат заменяет только mail ConfigMap/NetworkPolicy и точную
Deployment привязку, сохраняя остальные resources общего/STT render.

В base зафиксирован результат из существующего пустого EMAIL bootstrap.
Утверждённых live hosts нет: `destinations=[]`, CNI `egress=[]`, readiness mail
отрицательная. Пример конфигурации с fixtures не является production allowlist.

| Переход | Действие и readback |
|---|---|
| Создание | Из точного mailbox source получить все три артефакта; сверить configuration digest и gateway policy digest |
| Обновление | Выпустить новую immutable ConfigMap и Deployment patch; согласовать тот же source с EMAIL/CP; применение только по разрешению владельца |
| DNS rotation | Новые адреса вне pins закрывают CONNECT; заново произвести exact pins и выполнить согласованный rollout, не расширять до CIDR диапазона |
| Удаление mailbox | Новая source revision исключает его уникальные destinations; новые Pod не обслуживают старую policy; старые Pod дренируются в bounded shutdown |
| Ошибка/пустой source | Не выдавать readiness `204`, не делать dial; не переключаться на `8080` или direct outbound |
| Повтор подключения | Новое bounded transport connection; gateway не знает прикладной idempotency и никогда не переигрывает SMTP command |
| Shutdown | Stop accept, cancel tunnels, join workers и закрыть technical server в существующем общем бюджете |

Для полного отзыва старого поколения необходим фактический readback завершения
rollout; файл нового render не является доказательством применения. Смена
NetworkPolicy во время rolling update не делает старый Pod новым поколением.

## Проверки

- `go test -race ./...` и `go vet ./...` из service directory.
- `tools/verify-egress-gateway.sh`: оба environment render, STT и общая CNI
  граница, immutable rollout.
- `tools/verify-egress-mail.sh`: реальный producer и оба render из пустого
  EMAIL source, readonly immutable mount и отсутствие внешних разрешений.
- `internal/mailpolicy`: typed mailbox producer, digest/source binding,
  закрытые endpoints, private/mixed/rebinding, empty readiness.
- `internal/gateway` mail fixtures: все шесть transport pairs, server-first
  greeting, opaque bytes, exact literal dial и zero-dial при rebinding.
- Настоящие TLS fixtures проверяют encrypted roundtrip и отказ неизвестной CA
  для implicit/STARTTLS, не завершая TLS в gateway.

EMAIL-own перенос включает runtime env, deployable descriptor, исходящую
NetworkPolicy и render assertion одним checkpoint. Требуемое значение:
`EMAIL_BRIDGE_EGRESS_ADDRESS=egress-gateway.kodex-system.svc:8082`.
Этот unit не меняет EMAIL-owned metadata и не подменяет CP mailbox producer.
Live provider, CNI enforcement в кластере и применение: только после отдельного
разрешения владельца; локальный render не означает live PASS.

Проверены Context7 `/golang/go` для TLS API и официальные
[RFC 3207](https://www.rfc-editor.org/rfc/rfc3207.html) и
[RFC 8314](https://www.rfc-editor.org/rfc/rfc8314.html). STARTTLS не позволяет
требовать ClientHello до серверного приветствия; проверка TLS остаётся у bridge.
