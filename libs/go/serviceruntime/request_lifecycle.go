package serviceruntime

import (
	"context"
	"sync"
)

// requestCancellation retains only an owned context cancellation function; it never interprets or separately projects actor, authorization, payload or remote-operation state.
// requestCancellation сохраняет лишь owned context cancellation function, не интерпретируя и не проецируя отдельно actor, authorization, payload либо remote-operation state.
type requestCancellation struct{ cancel context.CancelFunc }

// RequestLifecycle links synchronous requests to a caller-owned application context and positively joins their releases and its own cancellation callback. It never closes dependencies, retries work or claims remote quiescence.
// RequestLifecycle связывает synchronous requests с caller-owned application context и подтверждает join их releases и собственного cancellation callback. Dependencies не закрываются, work не повторяется, remote quiescence не заявляется.
type RequestLifecycle struct {
	mutex        sync.Mutex
	parent       context.Context
	gate         *ActivityGate
	requests     map[*requestCancellation]struct{}
	stopParent   func() bool
	parentDone   chan struct{}
	parentJoined sync.Once
}

// NewRequestLifecycle requires an explicit application context and bounded local concurrency. Construction starts no work or I/O; the standard context callback runs only when the parent is canceled and remains owned until Close/Wait joins it.
// NewRequestLifecycle требует explicit application context и bounded local concurrency. Construction не запускает work либо I/O; standard context callback выполняется лишь при отмене parent и остаётся owned до Close/Wait join.
func NewRequestLifecycle(parent context.Context, maximum int) (*RequestLifecycle, error) {
	if parent == nil {
		return nil, ErrActivityGateLifecycle
	}
	if parent.Err() != nil {
		return nil, ErrActivityGateClosed
	}
	gate, err := NewActivityGate(maximum)
	if err != nil {
		return nil, err
	}
	lifecycle := &RequestLifecycle{parent: parent, gate: gate, requests: make(map[*requestCancellation]struct{}), parentDone: make(chan struct{})}
	lifecycle.stopParent = context.AfterFunc(parent, func() { lifecycle.closeAdmission(); lifecycle.parentCompleted() })
	return lifecycle, nil
}

// Begin preserves request values and cancellation while applying the earlier application deadline. Its idempotent release belongs to the caller and must run only after the entire synchronous handler and its owned cleanup joined.
// Begin сохраняет request values и cancellation, применяя более ранний application deadline. Idempotent release принадлежит caller и вызывается лишь после join всего synchronous handler и его owned cleanup.
func (lifecycle *RequestLifecycle) Begin(request context.Context) (context.Context, func(), error) {
	if lifecycle == nil || lifecycle.parent == nil || request == nil {
		return nil, nil, ErrActivityGateLifecycle
	}
	if err := request.Err(); err != nil {
		return nil, nil, err
	}
	var linked context.Context
	var cancel context.CancelFunc
	if deadline, ok := lifecycle.parent.Deadline(); ok {
		linked, cancel = context.WithDeadline(request, deadline)
	} else {
		linked, cancel = context.WithCancel(request)
	}
	lifecycle.mutex.Lock()
	if lifecycle.parent.Err() != nil {
		lifecycle.mutex.Unlock()
		cancel()
		return nil, nil, ErrActivityGateClosed
	}
	release, err := lifecycle.gate.Enter()
	if err != nil {
		lifecycle.mutex.Unlock()
		cancel()
		return nil, nil, err
	}
	entry := &requestCancellation{cancel: cancel}
	lifecycle.requests[entry] = struct{}{}
	lifecycle.mutex.Unlock()
	var once sync.Once
	return linked, func() {
		once.Do(func() {
			cancel()
			lifecycle.mutex.Lock()
			delete(lifecycle.requests, entry)
			lifecycle.mutex.Unlock()
			release()
		})
	}, nil
}

// closeAdmission synchronously prevents new work before canceling every admitted request; cancellation handles are copied under lock and invoked outside it, without waiting for a handler while holding the registry mutex.
// closeAdmission синхронно запрещает новую work до отмены каждого admitted request; cancellation handles копируются под lock и вызываются вне него без ожидания handler под registry mutex.
func (lifecycle *RequestLifecycle) closeAdmission() {
	lifecycle.mutex.Lock()
	_ = lifecycle.gate.Close()
	cancels := make([]context.CancelFunc, 0, len(lifecycle.requests))
	for entry := range lifecycle.requests {
		cancels = append(cancels, entry.cancel)
	}
	lifecycle.mutex.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
}

// parentCompleted records actual callback completion or its successful removal, never merely a cancellation request or expired shutdown budget.
// parentCompleted фиксирует actual callback completion либо successful removal, а не только cancellation request либо expired shutdown budget.
func (lifecycle *RequestLifecycle) parentCompleted() {
	lifecycle.parentJoined.Do(func() { close(lifecycle.parentDone) })
}

// StopAdmission synchronously closes admission and cancels owned request contexts without waiting for their handlers. Callback removal is joined when possible; only Wait or Joined can prove actual local completion.
// StopAdmission синхронно закрывает admission и отменяет owned request contexts без ожидания handlers. Callback removal присоединяется при возможности; лишь Wait либо Joined доказывают actual local completion.
func (lifecycle *RequestLifecycle) StopAdmission() error {
	if lifecycle == nil || lifecycle.parent == nil {
		return ErrActivityGateLifecycle
	}
	lifecycle.closeAdmission()
	if lifecycle.stopParent() {
		lifecycle.parentCompleted()
	}
	return nil
}

// Close closes admission, synchronously cancels admitted request contexts and then waits for positive join under the supplied shutdown budget. A timeout leaves Joined false and cannot authorize dependency closure.
// Close закрывает admission, синхронно отменяет admitted request contexts, затем ожидает positive join в supplied shutdown budget. Timeout оставляет Joined false и не разрешает dependency closure.
func (lifecycle *RequestLifecycle) Close(ctx context.Context) error {
	if lifecycle == nil || lifecycle.parent == nil || ctx == nil {
		return ErrActivityGateLifecycle
	}
	if err := lifecycle.StopAdmission(); err != nil {
		return err
	}
	return lifecycle.Wait(ctx)
}

// Wait joins the existing activity gate and the owned parent callback without spawning another waiter; it cannot cancel requests or turn an exhausted budget into successful cleanup.
// Wait ожидает existing activity gate и owned parent callback без дополнительного waiter; requests не отменяются, exhausted budget не превращается в successful cleanup.
func (lifecycle *RequestLifecycle) Wait(ctx context.Context) error {
	if lifecycle == nil || lifecycle.parent == nil || ctx == nil {
		return ErrActivityGateLifecycle
	}
	if err := lifecycle.gate.Wait(ctx); err != nil {
		return err
	}
	select {
	case <-lifecycle.parentDone:
		return nil
	default:
	}
	select {
	case <-lifecycle.parentDone:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Closed reports local admission refusal including a canceled parent; it grants no evidence of handler join or remote effect completion.
// Closed сообщает local admission refusal, включая canceled parent, но не evidence handler join либо remote effect completion.
func (lifecycle *RequestLifecycle) Closed() bool {
	return lifecycle == nil || lifecycle.parent == nil || lifecycle.parent.Err() != nil || lifecycle.gate.Closed()
}

// Joined reports only explicit local activity and callback join, preserving false after timeout even though request contexts were canceled.
// Joined сообщает лишь explicit local activity и callback join, сохраняя false после timeout также при canceled request contexts.
func (lifecycle *RequestLifecycle) Joined() bool {
	if lifecycle == nil || lifecycle.parent == nil || !lifecycle.gate.Joined() {
		return false
	}
	select {
	case <-lifecycle.parentDone:
		return true
	default:
		return false
	}
}
