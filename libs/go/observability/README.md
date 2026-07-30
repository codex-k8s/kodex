# Observability runtime

Пакет содержит общий ограниченный Prometheus-профиль для Go gRPC
deployables. Имена операций задаются закрытой картой при composition;
неизвестный метод нормализуется в `unknown`. Пакет не принимает произвольные
labels из request или transport metadata.
