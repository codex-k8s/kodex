package httpserver

import (
	"context"
	"errors"
	"net/http"

	"github.com/codex-k8s/kodex/libs/go/serviceruntime"
)

const (
	// requestGateUnavailable is a fixed transport diagnostic without request path, identity or payload.
	// requestGateUnavailable задаёт fixed transport diagnostic без request path, identity либо payload.
	requestGateUnavailable = "HTTP request admission is unavailable"
)

// ErrRequestGateLifecycle rejects invalid construction or waiting before admission has closed; it never means handlers joined.
// ErrRequestGateLifecycle отклоняет invalid construction либо ожидание до closure admission и никогда не означает joined handlers.
var ErrRequestGateLifecycle = errors.New("HTTP request gate lifecycle is invalid")

// RequestGate bounds synchronous owned HTTP handlers and provides a positive join signal after admission closes. It grants no domain permission, agent budget or remote-operation quiescence.
// RequestGate ограничивает synchronous owned HTTP handlers и даёт positive join signal после closure admission. Domain permission, agent budget либо remote-operation quiescence он не выдаёт.
type RequestGate struct {
	handler    http.Handler
	activities *serviceruntime.ActivityGate
}

// NewRequestGate validates a fixed handler and finite upload/request concurrency; it starts no listener, timer or goroutine.
// NewRequestGate проверяет fixed handler и finite upload/request concurrency; listener, timer либо goroutine не запускаются.
func NewRequestGate(handler http.Handler, maximum int) (*RequestGate, error) {
	if handler == nil {
		return nil, ErrRequestGateLifecycle
	}
	activities, err := serviceruntime.NewActivityGate(maximum)
	if err != nil {
		return nil, ErrRequestGateLifecycle
	}
	return &RequestGate{handler: handler, activities: activities}, nil
}

// ServeHTTP reserves one local request slot before calling its handler and always releases it, including panic unwinding. Closure and overload reject before domain I/O; payloads and paths never enter the fixed response.
// ServeHTTP резервирует local request slot до handler и освобождает его также при panic unwinding. Closure и overload отклоняются до domain I/O; payloads и paths не входят в fixed response.
func (gate *RequestGate) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if gate == nil {
		http.Error(writer, requestGateUnavailable, http.StatusServiceUnavailable)
		return
	}
	release, err := gate.activities.Enter()
	if err != nil {
		status := http.StatusServiceUnavailable
		if errors.Is(err, serviceruntime.ErrActivityGateLimit) {
			status = http.StatusTooManyRequests
		}
		http.Error(writer, requestGateUnavailable, status)
		return
	}
	defer release()
	gate.handler.ServeHTTP(writer, request)
}

// Close atomically prevents new owned work and publishes join only after all previously admitted handlers exit; it does not cancel handlers or infer their remote outcomes.
// Close атомарно запрещает новую owned work и публикует join лишь после выхода всех ранее допущенных handlers; handlers не отменяются, remote outcome не выводится.
func (gate *RequestGate) Close() error {
	if gate == nil || gate.activities.Close() != nil {
		return ErrRequestGateLifecycle
	}
	return nil
}

// Wait observes the existing join channel without spawning a waiter goroutine. An expired context returns its error and cannot produce positive join or authorize dependency closure.
// Wait наблюдает existing join channel без waiter goroutine. Истёкший context возвращает ошибку и не даёт positive join либо разрешения закрывать dependencies.
func (gate *RequestGate) Wait(ctx context.Context) error {
	if gate == nil {
		return ErrRequestGateLifecycle
	}
	err := gate.activities.Wait(ctx)
	if errors.Is(err, serviceruntime.ErrActivityGateLifecycle) {
		return ErrRequestGateLifecycle
	}
	return err
}

// Closed reports only admission closure; it must never be used as evidence that handlers or remote effects joined.
// Closed сообщает лишь closure admission; это не evidence joined handlers либо remote effects.
func (gate *RequestGate) Closed() bool {
	return gate == nil || gate.activities.Closed()
}

// Joined returns true only for a closed gate whose last owned handler has exited; an idle open gate is not joined because it can still accept work.
// Joined возвращает true лишь для closed gate после выхода последнего owned handler; idle open gate не joined, поскольку ещё принимает work.
func (gate *RequestGate) Joined() bool {
	return gate != nil && gate.activities.Joined()
}
