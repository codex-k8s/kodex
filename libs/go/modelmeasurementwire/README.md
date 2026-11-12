# Преобразование измерений модели

Модуль переводит `runtimecontract.ModelMeasurement` в control-plane Proto и обратно
для runtime-controller и control-plane. `ToProto` и `FromProto` сохраняют наличие
необязательных значений и создают независимые копии. Это только wire adapter:
валидация остаётся у `ModelMeasurement.Validate`, authority — у владельца lease.

Отсутствующие значения не становятся нулевыми измерениями, неизвестные варианты
не получают значения по умолчанию. Caller обязан отклонить неверную проекцию до
использования. Модуль не выполняет I/O, не создаёт retries, metrics, goroutines
или lifecycle и не назначает actor/session/turn. Совместимость определяется
совместной поставкой Proto и runtime contract; legacy decoder отсутствует.
