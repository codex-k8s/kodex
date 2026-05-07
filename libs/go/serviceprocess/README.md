# serviceprocess

`serviceprocess` содержит общий код уровня процесса для Go-сервисов платформы:

- запуск HTTP health/metrics и gRPC серверов;
- graceful shutdown;
- readiness mux;
- подключение необязательного `platform-event-log`;
- запуск outbox dispatcher.

Библиотека не содержит доменной логики и не владеет конфигурацией конкретного сервиса.
