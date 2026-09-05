---
id: GO-LIB-MAIL-POLICY-001
title: Общий контракт почтовых сетевых pins
type: library-readme
status: approved
owner: backend
version: 1.0.0
updated: 2026-09-05
---

# Общий контракт почтовых сетевых pins

Модуль используется CP producer #1046 и egress consumer #1029. Он извлечён
из проверенного egress unit без смены `egress-mail/v1`, digest или bootstrap
артефактов. Единственная реализация typed producer, validation и render
находится здесь; сервисный adapter сохраняет CLI и runtime limits/readiness.

`Produce(ctx, emailbridgeapi.Configuration, gatewayPolicyDigest, Resolver)`
возвращает `MailDocument`. Resolver принадлежит вызывающему сервису и возвращает
`Snapshot{Addresses []netip.Addr, ExpiresAt time.Time}` с полным DNS-ответом.
Библиотека проверяет exact source revision/digest, endpoint pairs, bounded
число destinations/addresses, свежесть и каждый адрес. Private/special/mixed
наборы закрыто отклоняются целиком. `NormalizeHostname` и `ValidateAddresses`
также используются общим egress DNS consumer, исключая разные правила pins.

`MailDocument.Validate/Digest` и `RenderFiles` создают immutable digest-named
ConfigMap, exact NetworkPolicy и Deployment patch. Выход не содержит mailbox
credential descriptors, scopes, messages или значения credentials. Создание
Kubernetes objects, DNS query/cache, tenant authority, publication state,
rollout/readback, telemetry и lifecycle остаются у владельцев сервисов.

В модуле нет goroutines, root context, логирования, Kubernetes/SQL/provider
клиентов или скрытых retries. Контекст DNS передаётся resolver. Ошибки содержат
только фиксированную диагностику; plaintext и upstream diagnostics не выводятся.
Изменение wire schema/digest требует согласованного producer/consumer migration;
извлечение сохраняет совместимость с существующим `egress-mail/v1`.

`PublicationAdmissionResources()` возвращает exact source VAP/Binding для
генератора deploy artifact и CP authoritative readback. `bash
tools/generate-mail-admission.sh` обновляет только generated JSON в egress base.
Admission ограничивает CP CREATE ConfigMap точными namespace/name/labels,
immutable mode и единственным bounded `mail-policy.json`. Иные identities
остаются под своим RBAC; первоначальный shipped seed может установить installer.
CEL проверяет эту структурную границу. Семантику JSON и digest проверяют
общий producer и strict egress loader. CP должен подтвердить actual spec,
готовность VAP и Binding до использования выданного права create.

Tests исполняют source CEL для положительного render и 16 отрицательных
вариантов, сверяют generated artifact и оба environment renders.
Kubernetes admission enforcement и CNI live остаются отдельной приёмкой.
Проверены Context7 `/cel-expr/cel-go` (Compile/Program/Eval) и
`/websites/kubernetes_io` (ValidatingAdmissionPolicy, failurePolicy и Binding).

Локальная проверка: `go test -race ./...`, `go vet ./...`; egress suite дополнительно
проверяет shared producer на настоящем source fixture, точный byte-for-byte
bootstrap render, consumer readiness, CNI и TLS/DNS negative paths. Живой DNS,
Kubernetes/CNI и provider acceptance здесь не заявляются.
