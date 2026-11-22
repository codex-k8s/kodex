package serviceruntime

import (
	"context"
	"errors"
	"sync"
)

var (
	// ErrActivityGateLifecycle rejects invalid construction or waiting before explicit admission closure; it never proves that work joined.
	// ErrActivityGateLifecycle отклоняет invalid construction либо ожидание до explicit admission closure и не доказывает joined work.
	ErrActivityGateLifecycle = errors.New("activity gate lifecycle is invalid")
	// ErrActivityGateClosed rejects new local work after admission closes, independently from domain authority or the outcome of previously admitted work.
	// ErrActivityGateClosed отклоняет новую local work после admission closure независимо от domain authority либо исхода ранее допущенной work.
	ErrActivityGateClosed = errors.New("activity admission is closed")
	// ErrActivityGateLimit rejects local overload without reserving a slot or implying that any business operation started.
	// ErrActivityGateLimit отклоняет local overload без резервирования slot либо утверждения о старте business operation.
	ErrActivityGateLimit = errors.New("activity admission capacity is exhausted")
)

// ActivityGate bounds synchronous caller-owned activities and publishes positive join only after explicit closure and the last release. It owns no context, goroutine, retry, authority, remote outcome or dependency.
// ActivityGate ограничивает synchronous caller-owned activities и публикует positive join лишь после explicit closure и последнего release. Context, goroutine, retry, authority, remote outcome либо dependency ему не принадлежат.
type ActivityGate struct {
	mutex           sync.Mutex
	maximum, active int
	closed          bool
	drained         chan struct{}
}

// NewActivityGate validates a finite local concurrency limit without starting work; its slots are neither agent budgets nor permissions to perform effects.
// NewActivityGate проверяет конечный local concurrency limit без запуска work; slots не являются agent budgets либо permissions на effects.
func NewActivityGate(maximum int) (*ActivityGate, error) {
	if maximum < 1 || maximum > 128 {
		return nil, ErrActivityGateLifecycle
	}
	return &ActivityGate{maximum: maximum, drained: make(chan struct{})}, nil
}

// Enter reserves one local activity and returns its idempotent release. The caller must defer release until all owned activity work and cancellation callbacks really exit; closure alone never releases its slot.
// Enter резервирует local activity и возвращает idempotent release. Caller обязан defer release до actual выхода всей owned activity work и cancellation callbacks; closure сама не освобождает slot.
func (gate *ActivityGate) Enter() (func(), error) {
	if gate == nil || gate.drained == nil {
		return nil, ErrActivityGateLifecycle
	}
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	if gate.closed {
		return nil, ErrActivityGateClosed
	}
	if gate.active >= gate.maximum {
		return nil, ErrActivityGateLimit
	}
	gate.active++
	var once sync.Once
	return func() {
		once.Do(func() {
			gate.mutex.Lock()
			defer gate.mutex.Unlock()
			gate.active--
			if gate.closed && gate.active == 0 {
				close(gate.drained)
			}
		})
	}, nil
}

// Close prevents new activities and records join only when every existing activity released; it does not cancel or claim to stop their external effects.
// Close запрещает новые activities и фиксирует join лишь после release всех existing activities; отмена либо остановка external effects не заявляются.
func (gate *ActivityGate) Close() error {
	if gate == nil || gate.drained == nil {
		return ErrActivityGateLifecycle
	}
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	if !gate.closed {
		gate.closed = true
		if gate.active == 0 {
			close(gate.drained)
		}
	}
	return nil
}

// Wait observes the existing positive join channel without a waiter goroutine; timeout preserves an unjoined result, while an already recorded join remains true even if the observation context is canceled.
// Wait наблюдает existing positive join channel без waiter goroutine; timeout сохраняет unjoined result, а ранее recorded join остаётся true также при canceled observation context.
func (gate *ActivityGate) Wait(ctx context.Context) error {
	if gate == nil || gate.drained == nil || ctx == nil {
		return ErrActivityGateLifecycle
	}
	gate.mutex.Lock()
	closed := gate.closed
	gate.mutex.Unlock()
	if !closed {
		return ErrActivityGateLifecycle
	}
	select {
	case <-gate.drained:
		return nil
	default:
	}
	select {
	case <-gate.drained:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Closed exposes admission state only; it is not evidence that handlers, goroutines or remote work joined.
// Closed раскрывает лишь admission state, не evidence joined handlers, goroutines либо remote work.
func (gate *ActivityGate) Closed() bool {
	if gate == nil || gate.drained == nil {
		return true
	}
	gate.mutex.Lock()
	defer gate.mutex.Unlock()
	return gate.closed
}

// Joined is true only after explicit closure and release of every owned activity; an idle open gate can still admit work and is therefore not joined.
// Joined true лишь после explicit closure и release каждой owned activity; idle open gate ещё допускает work, поэтому не joined.
func (gate *ActivityGate) Joined() bool {
	if gate == nil || gate.drained == nil || !gate.Closed() {
		return false
	}
	select {
	case <-gate.drained:
		return true
	default:
		return false
	}
}
