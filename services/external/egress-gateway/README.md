# Egress gateway

`egress-gateway` — самостоятельный platform deployable для ограниченного
исходящего HTTPS-трафика. Он принимает только bodyless HTTP `CONNECT` к
утверждённому `FQDN:443`, проверяет фактический TLS `ClientHello` и выполняет
внешний dial только к проверенному literal IP. Gateway не завершает TLS, не
имеет application credentials и не меняет проверку сертификата либо hostname
в TLS stack потребителя.

## Опубликованный runtime-контракт

| Параметр | Значение |
|---|---|
| Namespace | `mattercodex-system` |
| Service | `egress-gateway` |
| Полный Service DNS | `egress-gateway.mattercodex-system.svc.cluster.local` |
| CONNECT port | `8080/TCP`, имя `connect` |
| Technical Service | `egress-gateway-technical.mattercodex-system.svc.cluster.local`; публикует и not-ready Pod для закрытого readback |
| Technical port | `9090/TCP`, имя `metrics` |
| Endpoint Pod labels | `app.kubernetes.io/name=egress-gateway`, `app.kubernetes.io/component=platform-egress` |
| Liveness | `GET /livez` на technical port |
| Readiness | `GET /readyz` на technical port |
| Policy readback | `GET /policy` на technical port; только process/policy/resolver state, revision и SHA-256 digest |

Будущий consumer задаёт
`HTTPS_PROXY=http://egress-gateway.mattercodex-system.svc.cluster.local:8080`.
В `NO_PROXY` должны остаться `localhost`, loopback и внутренние зоны `.svc` и
`.svc.cluster.local`, чтобы внутренние service calls не направлялись наружу.
`NetworkPolicy` разрешает CONNECT не к объекту Service, а к указанным устойчивым
Pod labels в точном namespace и на точном порту.

Нулевой image digest в repository base — только явный render input pattern.
Перед rollout renderer обязан заменить его на построенный и допущенный exact
OCI digest; нулевое значение не является заявлением о существующем образе.

## Machine policy

Файл policy монтируется из immutable `ConfigMap`. Deployment задаёт ожидаемые
version и canonical SHA-256 digest независимо от файла. При загрузке gateway
строго отвергает неизвестные и повторяющиеся JSON-поля, неполную конфигурацию,
неверные bounds, несовпадение version либо digest. Runtime mutation отсутствует.
При таком отказе CONNECT listener не создаётся, `/readyz` возвращает `503`, а
ограниченный `/policy` readback показывает `policyState=INVALID` без ложной
loaded revision/digest. Некорректный resolver primitive аналогично оставляет
policy `ACTIVE`, resolver `INVALID` и трафик закрытым.

Активная revision разрешает только:

- `api.openai.com:443`;
- `auth.openai.com:443`;
- `chatgpt.com:443`;
- `github.com:443`.

Wildcard, suffix/pattern, IP literal и любой другой порт запрещены. Канонический
контракт находится в `contracts/egress/v1/egress-gateway-policy.schema.json`.

## Матрица угроз и сценариев

| Сценарий | Закрывающая граница | Проверяемый результат |
|---|---|---|
| Неутверждённый Pod обращается к gateway | CNI ingress: exact namespace и Pod labels consumer | Пакет не достигает listener; Service DNS не является authority |
| Hostile, conflicting либо body-bearing CONNECT | Строгий bounded parser request-line и headers | Reject до `200` и до внешнего dial |
| Допустимый CONNECT, но SNI отсутствует, malformed, duplicate, отличается или скрыт ECH | Bounded parser фактического TLS ClientHello | Tunnel закрыт, счётчик внешних dial не меняется |
| DNS NXDOMAIN, timeout, truncated без TCP recovery, loop, CNAME/answer overflow, mixed public/private либо private-only | Server-owned A/AAAA resolver с полной validation snapshot | Fail closed; unsafe snapshot не кэшируется |
| Public snapshot сменяется private после TTL | Повторный resolve после expiry и revalidation каждого cached address перед dial | Rebinding отклонён; dial получает только literal AddrPort |
| Caller пытается выбрать policy, version или destination | Immutable loaded policy и expected version/digest Deployment | Request не расширяет authority |
| Компрометация gateway | Нет secrets, SA token, RBAC, host access; egress только DNS и TCP/443 | Blast radius ограничен L7 policy и resource bounds |
| Slowloris, oversized input, half-open tunnel, connection flood | Header/ClientHello bounds, deadlines, global/per-source limits, cancel/join | Нет неограниченных goroutine и buffers |
| Policy partial, invalid или digest mismatch | Startup validation и readiness barrier | Process не готов и CONNECT listener не обслуживает трафик |
| Consumer пытается обойти gateway | Итоговая consumer NetworkPolicy без direct external HTTPS | Consumer достигает только gateway Pod labels:8080 |

## Матрица authority

| Данные или действие | Авторитетный владелец | Недоверенный сигнал |
|---|---|---|
| Schema, policy content, expected version/digest, Deployment, Service и labels | Platform/repository owner | Runtime request |
| Допустимый workload ingress | CNI `NetworkPolicy` | Service DNS и request fields |
| Желаемый destination | Только exact взаимное совпадение CONNECT authority, ClientHello SNI и policy | CONNECT authority и SNI по отдельности |
| DNS snapshot | Server-owned resolver после полной A/AAAA/CNAME/special-purpose validation | Внешний DNS answer |
| Dial target | Проверенный literal `netip.AddrPort` | Hostname |
| TLS peer, certificate и application auth | TLS stack consumer | Gateway |
| Readiness/readback | ACTIVE policy state, version/digest и resolver primitives | Caller parameters |
| Observability | Закрытые internal stage/outcome/reason | Hostname, IP, URL, SNI, headers и payload |

## Матрица состояния и lifecycle

| Объект | Переходы | Инвариант закрытого отказа |
|---|---|---|
| Process | `BOOTING -> READY | NOT_READY -> DRAINING -> STOPPED` | Readiness false до startup barrier и до начала drain |
| Policy | `UNLOADED -> VALIDATING -> ACTIVE`; ошибка -> `INVALID` | `INVALID` никогда не обслуживает CONNECT; замена только rollout |
| DNS cache | `MISS -> RESOLVING -> VALIDATED(until expiry) | REJECTED` | Stale и unsafe fallback отсутствуют |
| Connection | `ACCEPTED -> CONNECT_VALIDATED -> CLIENTHELLO_PENDING -> SNI_VALIDATED -> DNS_VALIDATED -> LITERAL_DIALED -> TUNNELING -> CLOSED` | Любой reject до `LITERAL_DIALED` гарантирует zero external connection |
| Shutdown | `READY -> DRAINING -> STOPPED` | Stop accept, cancel tunnels, join, затем независимый bounded cleanup |
| Rollback | Новый rollout ранее review-approved policy и image digest | Runtime mutation и mutable ConfigMap отсутствуют |

Gateway не хранит business state, не использует PostgreSQL, idempotency/OCC и
не публикует domain events. Connection attempt — только ephemeral bounded
process state; поэтому Proto, AsyncAPI и domain-event контракты неприменимы.

## DNS и TLS ограничения

Resolver выполняет явные A и AAAA запросы через настроенные IP-адреса DNS,
проверяет response ID/question/RCODE, CNAME chain, число и размер записей и
вычисляет bounded TTL из фактических DNS RR. UDP truncation требует успешного
повтора по TCP. Если хотя бы один адрес относится к private, loopback,
link-local, multicast, unspecified, IPv4-mapped, reserved, benchmarking,
documentation или другому IANA special-purpose prefix, отвергается весь набор.

После успешного CONNECT gateway bounded-буферизует исходные TLS records до
полного первого ClientHello, требует ровно один hostname SNI и отсутствие ECH,
затем побайтно передаёт уже прочитанные данные внешнему peer. Hostname никогда
не передаётся `net.Dialer` и не вызывает вторичное DNS-разрешение.

## Проверенные внешние спецификации

- [Go 1.26.5 `net`](https://pkg.go.dev/net),
  [`net/netip`](https://pkg.go.dev/net/netip) и
  [`crypto/tls`](https://pkg.go.dev/crypto/tls);
- [miekg/dns v1.1.72](https://pkg.go.dev/github.com/miekg/dns);
- [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/);
- [RFC 9110 CONNECT](https://www.rfc-editor.org/rfc/rfc9110.html#name-connect),
  [RFC 6066](https://www.rfc-editor.org/rfc/rfc6066.html),
  [RFC 8446](https://www.rfc-editor.org/rfc/rfc8446.html) и
  [RFC 9849 ECH](https://www.rfc-editor.org/rfc/rfc9849.html);
- IANA [IPv4](https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry.xhtml)
  и [IPv6](https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry.xhtml)
  Special-Purpose Address Registries.
