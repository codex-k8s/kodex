# eventconsumer

`eventconsumer` is the shared runtime for reading domain events from `platform-event-log`.

It owns no business state. A service registers handlers by `event_type` and `schema_version`; the runner claims a batch through `eventlog.Store`, calls the typed handler with a deadline, and advances the shared checkpoint only after the event was idempotently acknowledged or safely poisoned.

The current `platform-event-log` schema stores the append-only stream, per-consumer checkpoint, and durable retry attempt for the currently blocked sequence. Persistent business failed/poison diagnostics must be written by the owner service domain model when the service has one. Runtime checkpoint diagnostics, logs, and hooks must stay bounded: event type, schema version, sequence, status, safe code and short summary only.
