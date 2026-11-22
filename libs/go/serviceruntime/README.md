# `serviceruntime`

Общий жизненный цикл процессов для исполняемых компонентов Go Kodex.

Публичный API предоставляет атомарную готовность, группу фоновых обработчиков
с явными отменой и ожиданием и последовательность операций завершения с
независимыми ограниченными контекстами. Библиотека не читает конфигурацию, не
создаёт корневой контекст, не
открывает внешние зависимости и не логирует ошибки.

## Дескрипторы дочернего процесса

`NewCommandPipes(command, label)` — конструктор трёх пар `os.Pipe` для ещё
не запущенной ненулевой `exec.Cmd`. Он назначает прямые файловые дескрипторы
stdin/stdout/stderr, не вызывает `Start`, не создаёт goroutine и не выбирает
протокол, context, deadline или правила остановки. `label` — доверенная
диагностическая метка вызывающего кода, не пользовательский текст или секрет.
Ошибки сохраняют имя потока, но не включают исходную ошибку ОС.

При неполном выделении все полученные дескрипторы закрываются. После успеха
вызывающий код владеет шестью концами: child ends закрывает в своей точке
после `Start`, parent ends — после завершения и ожидания readers.
`Cmd.Wait` не владеет этими отдельно созданными handles.
`CommandPipes.Close` служит общей best-effort очисткой, а не остановкой
процесса или доказательством join. Наблюдаемость, сигнализация и повтор
запуска остаются у потребителей. Новые потребители используют конструктор;
совместимых запасных реализаций подготовки каналов нет.

## Initial connectivity probe

`WaitStartupProbe(ctx, probe, retryable, probeBudget, delay)` исполняет только
переданную readiness-проверку. Два production consumer — control-plane и
internal-rpc-authority — сохраняют собственную PostgreSQL/TLS/config policy.
Повтор допускается лишь при исходном caller deadline и положительном сервисном
предикате; библиотека не выбирает ошибку для retry и не продлевает deadline.
Нет goroutine или владения pool. Consumer закрывает rejected pool и не запускает
workers; при успехе передаёт pool своему Run. `StartupProbeFailure` содержит
приватные Cause/Budget отдельно, не реализует Error и не печатает значения.
Это не повтор business command, SQL transaction, bootstrap либо миграции.

## Локальная admission и положительный join

`NewActivityGate(maximum)` задаёт1..128 одновременно допущенных synchronous
activities, не запуская goroutine, context либо I/O. `Enter` возвращает
idempotent release; caller откладывает его до завершения всей своей work,
включая cancellation callbacks. Закрытая admission и overload имеют разные
фиксированные ошибки. `Close` запрещает новые входы, `Wait(ctx)` подтверждает
выход всех ранее допущенных activities. Timeout не освобождает slots и не
доказывает join; idle open gate тоже не joined. Nil/zero gate закрыто
отклоняется без panic.

Gate не владеет transport/server, не назначает authority и не заменяет
runtime/agent/effect budgets. Отмена контекстов принадлежит consumer.
`httpserver.RequestGate` использует это ядро для HTTP handlers; SSH execution
service должен использовать тот же контракт для собственного caller-owned lifecycle.
Зависимости закрываются только после положительного join. Неуспешный
shutdown требует явного отказа процесса без ложного утверждения quiescence.

`NewRequestLifecycle(appCtx, maximum)` добавляет caller-owned application
cancellation поверх этого ядра для synchronous RPC/domain handlers. `Begin`
возвращает linked request context и idempotent release. Значения берутся из
incoming request, не из application context; применяется более ранний deadline.
`StopAdmission()` закрывает admission и синхронно отменяет admitted contexts
без ожидания handlers; этот hook вызывается перед bounded transport shutdown.
Он не доказывает join. `Close(ctx)` сначала выполняет тот же stop,
затем ждёт реальные releases и завершение/снятие собственного parent callback.
`Wait(ctx)` и `Joined()` не подменяют join одним `context.Done` или server Stop.
Parent context не отменяется библиотекой. Canceled constructor, nil contexts и
zero lifecycle закрыто отклоняются. Дополнительная waiter goroutine не создаётся.

Consumer вызывает release после своих synchronous operations и cleanup;
остановка транспорта/дочерних workers остаётся его обязанностью. Timeout
не разрешает закрыть dependencies, которыми ещё пользуется unjoined handler.
Callback `context.AfterFunc` также явно joined: успешный stop означает снятие,
а false сам по себе не означает completion. Проверены актуальные документы
Go context через Context7 и race/cancel/deadline/ambient-value regression cases.

`WorkerGroup.Joined()` читает только существующий collector `done`: ошибка
завершившегося worker сохраняется отдельно от факта local join. `Stop()`, отмена
контекста и timeout `Wait` не разрешают закрывать зависимости, пока `Joined()`
ложен. Это не доказательство остановки удалённого процесса.
Проверка: `timeout 120s go test -race -count=1 -p=1 ./...` из каталога модуля.
