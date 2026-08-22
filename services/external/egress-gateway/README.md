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
| CONNECT port | `8080/TCP`, имя `connect`; bodyless `CONNECT` и compatibility `GET /readyz` |
| Technical Service | `egress-gateway-technical.mattercodex-system.svc.cluster.local`; публикует и not-ready Pod для закрытого readback |
| Technical port | `9090/TCP`, имя `metrics` |
| Endpoint Pod labels | `app.kubernetes.io/name=egress-gateway`, `app.kubernetes.io/component=platform-egress` |
| Liveness | `GET /livez` на technical port |
| Compatibility readiness | bodyless `GET /readyz` без query на `8080`: `204` только при effective `ACTIVE/READY`, иначе `503`; другие non-CONNECT routes закрыты |
| Technical readiness | `GET /readyz` на technical port |
| Policy readback | `GET /policy` на technical port; только process/policy/resolver state, revision и SHA-256 digest |

Будущий consumer задаёт
`HTTPS_PROXY=http://egress-gateway.mattercodex-system.svc.cluster.local:8080`.
В `NO_PROXY` должны остаться `localhost`, loopback и внутренние зоны `.svc` и
`.svc.cluster.local`, чтобы внутренние service calls не направлялись наружу.
`NetworkPolicy` разрешает CONNECT не к объекту Service, а к указанным устойчивым
Pod labels в точном namespace и на точном порту.

### Контракт rebase PR #243

После merge этого prerequisite сохранённая developer session PR #243
перебазируется на новый `main`, удаляет локальные
`cmd/management-egress-proxy/main.go` и
`deploy/k8s/base/integration-gateway/management-egress-proxy.yaml`, их
Kustomize/OCI target и сохраняет management lifecycle у `integration-gateway`.
Единый `ManagementEgressProxyURL` получает значение
`http://egress-gateway.mattercodex-system.svc.cluster.local:8080`: provider/Git
clients используют его как `HTTPS_PROXY`, а существующий `GET <url>/readyz`
получает совместимый effective readiness на том же порту. `NO_PROXY` сохраняет
внутренние `.svc`/`.svc.cluster.local` calls; consumer policy не открывает
`9090` и не получает direct external `443`.

Нулевой image digest в repository base — только явный render input pattern.
Принадлежащие unit overlays находятся в
`deploy/k8s/overlays/{staging,production}/egress-gateway`. Перед rollout
`tools/render-egress-gateway.sh` обязан заменить ровно один image input на
построенный и допущенный exact OCI digest из exact node-reachable registry;
нулевое значение не является заявлением о существующем образе.

## Machine policy

Файл policy монтируется из отдельного immutable content-addressed `ConfigMap`.
Kustomize hash входит в имя объекта и автоматически переключает точную ссылку
Deployment при новом содержимом. Deployment задаёт ожидаемые version и
canonical SHA-256 digest независимо от файла. Digest вычисляет тот же Go
canonicalizer `cmd/policy-digest`, который использует runtime. При загрузке gateway
строго отвергает неизвестные и повторяющиеся JSON-поля, неполную конфигурацию,
неверные bounds, несовпадение version либо digest. Runtime mutation отсутствует.
При таком отказе порт `8080` обслуживает только compatibility `/readyz=503`,
любой CONNECT закрыто отклоняется, а ограниченный `/policy` readback показывает
`policyState=INVALID` без ложной
loaded revision/digest. Некорректный resolver primitive аналогично оставляет
policy `ACTIVE`, resolver `INVALID` и трафик закрытым.

Активная revision разрешает только:

- `api.openai.com:443`;
- `auth.openai.com:443`;
- `chatgpt.com:443`;
- `github.com:443`.

Wildcard, suffix/pattern, IP literal, uppercase/trailing-dot alias и любой
другой порт запрещены. Schema принимает только lowercase ASCII FQDN без
завершающей точки; канонический контракт находится в
`contracts/egress/v1/egress-gateway-policy.schema.json`.

## Матрица угроз и сценариев

| Сценарий | Закрывающая граница | Проверяемый результат |
|---|---|---|
| Неутверждённый Pod обращается к gateway | CNI ingress: exact namespace и Pod labels consumer | Пакет не достигает listener; Service DNS не является authority |
| Hostile, conflicting либо body-bearing CONNECT | Строгий bounded parser request-line и headers | Reject до `200` и до внешнего dial |
| Допустимый CONNECT, но SNI отсутствует, malformed, duplicate, отличается или скрыт ECH | Bounded parser фактического TLS ClientHello | Tunnel закрыт, счётчик внешних dial не меняется |
| DNS NXDOMAIN, timeout, truncated без TCP recovery, loop, CNAME/answer overflow, mixed public/private либо private-only | Server-owned A/AAAA resolver с полной validation snapshot | Fail closed; unsafe snapshot не кэшируется |
| Public snapshot сменяется private после TTL | Повторный resolve после expiry и revalidation каждого cached address перед dial | Rebinding отклонён; dial получает только literal AddrPort |
| Caller пытается выбрать policy, version или destination | Immutable loaded policy и expected version/digest Deployment | Request не расширяет authority |
| Компрометация gateway | Нет application secrets, SA token, RBAC, host access; restricted runtime и resource bounds | Скомпрометированный процесс сохраняет сетевой доступ к достижимому TCP/443; дополнительное L3/L4 ограничение вынесено в [Issue #248](https://github.com/codex-k8s/matter-codex/issues/248) |
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
| Shutdown | `READY -> DRAINING -> STOPPED` | Stop accept, cancel tunnels до `20s`, join worker `5s`, technical cleanup `5s`; Pod grace `45s` оставляет `15s` margin |
| Rollback | Выбор ранее review-approved environment render, policy object и image digest | Runtime mutation, delete/recreate и изменение существующего immutable ConfigMap отсутствуют |

Gateway не хранит business state, не использует PostgreSQL, idempotency/OCC и
не публикует domain events. Connection attempt — только ephemeral bounded
process state; поэтому Proto, AsyncAPI и domain-event контракты неприменимы.

## DNS и TLS ограничения

Resolver выполняет явные A и AAAA запросы через настроенные IP-адреса DNS,
проверяет response ID/question/RCODE, CNAME chain, число и размер записей и
вычисляет bounded TTL из фактических DNS RR. UDP truncation требует успешного
повтора по TCP. IPv6 допускается default-deny только внутри выделенного
global-unicast `2000::/3` с дополнительным explicit special-purpose deny. Если
хотя бы один адрес относится к private, loopback,
link-local, multicast, unspecified, IPv4-mapped, reserved, benchmarking,
documentation или другому IANA special-purpose prefix, отвергается весь набор.

После успешного CONNECT gateway bounded-буферизует исходные TLS records до
полного первого ClientHello, требует ровно один hostname SNI и отсутствие ECH,
затем побайтно передаёт уже прочитанные данные внешнему peer. Hostname никогда
не передаётся `net.Dialer` и не вызывает вторичное DNS-разрешение.

## Проверенные внешние спецификации

- [Go 1.26.6 `net`](https://pkg.go.dev/net),
  [`net/netip`](https://pkg.go.dev/net/netip) и
  [`crypto/tls`](https://pkg.go.dev/crypto/tls);
- [miekg/dns v1.1.72](https://pkg.go.dev/github.com/miekg/dns);
- [`env/v11` v11.4.1](https://pkg.go.dev/github.com/caarlos0/env/v11);
- [Kubernetes NetworkPolicy](https://kubernetes.io/docs/concepts/services-networking/network-policies/);
- [RFC 9110 CONNECT](https://www.rfc-editor.org/rfc/rfc9110.html#name-connect),
  [RFC 6066](https://www.rfc-editor.org/rfc/rfc6066.html),
  [RFC 8446](https://www.rfc-editor.org/rfc/rfc8446.html) и
  [RFC 9849 ECH](https://www.rfc-editor.org/rfc/rfc9849.html);
- IANA [IPv4](https://www.iana.org/assignments/iana-ipv4-special-registry/iana-ipv4-special-registry.xhtml)
  и [IPv6](https://www.iana.org/assignments/iana-ipv6-special-registry/iana-ipv6-special-registry.xhtml)
  Special-Purpose Address Registries, а также
  [IPv6 Address Space](https://www.iana.org/assignments/ipv6-address-space/ipv6-address-space.xhtml).
