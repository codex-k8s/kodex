# grpcserver

`grpcserver` содержит общий серверный gRPC runtime для внутренних Go-сервисов `kodex`.

В модуль входит только инфраструктурная граница:
- сборка `grpc.Server` из типизированной конфигурации;
- recovery;
- проверка вызывающей стороны через расширяемый `Authenticator`;
- shared-token authenticator для внутреннего service-to-service контура;
- лимит активных unary RPC;
- deadline для unary RPC;
- keepalive, `MaxConcurrentStreams` и лимиты размера сообщений;
- базовые Prometheus-метрики unary RPC;
- OpenTelemetry `StatsHandler` для входящих RPC;
- W3C-проброс `tracecontext+baggage`;
- резервная лог-корреляция из активного OpenTelemetry span, если вызывающая сторона не передала legacy `x-kodex-trace-id`.

В модуль не входят доменные handlers, cast `proto <-> domain` и маппинг доменных ошибок.
