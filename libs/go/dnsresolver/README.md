---
id: GO-LIB-DNS-RESOLVER-001
title: Общий DNS resolver для сетевых pins
type: technical-guide
status: approved
owner: developer
version: 1.0.0
updated: 2026-09-05
---

# DNS resolver

Источник #1029, consumer #1046. Модуль содержит прежнюю реализацию egress
resolver и её тесты; сервисные импорты удалены. CP mail publisher и egress
используют один A/AAAA/CNAME/TTL и public-address алгоритм. Общая проверка IP
и нормализация hostname принадлежат `libs/go/mailpolicy`; snapshot реализует
его `Resolver` port без преобразования expiry.

`New(Config, []netip.AddrPort, Exchanger, Observer)` проверяет те же bounds,
что и machine policy, и только literal resolver addresses с портом 53.
`LoadSystemServers(path)` ограниченно читает server-owned resolver configuration;
client payload не назначает этот путь. Production передаёт `nil` Exchanger:
используется miekg/dns с propagated context и отдельными query deadlines.
Observer получает закрытые outcome/reason, без hostname, IP или payload.

`Resolve(ctx, hostname)` возвращает полный проверенный snapshot с `ExpiresAt`:
UDP truncation повторяется через TCP, malformed/mixed/private/CNAME-loop/overflow
ответ закрыто отклоняется. Внутренний cache bounded; stale fallback отсутствует.
Нижний TTL ограничивает только кэширование: короткий положительный DNS TTL
не продлевается и snapshot не кэшируется. Отменённый context и истёкший за время
запроса snapshot не выдаются. Старые in-flight snapshots остаются immutable.

Публичная проверка: `bash tools/verify-egress-gateway.sh`; узкий контур из модуля:
`go test -race -timeout 90s ./...` и `go vet ./...`. Fixtures используют fake
Exchanger и loopback, не обращаются к публичным DNS или live provider.
Проверена Context7 `/miekg/dns`: [ExchangeContext и deadline](https://github.com/miekg/dns/blob/master/_autodocs/api-reference/client.md).
