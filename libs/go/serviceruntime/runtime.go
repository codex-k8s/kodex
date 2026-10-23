package serviceruntime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Readiness atomically publishes a boolean/reason pair without performing I/O.
// Callers compute dependency policy and choose the reason text themselves.
// Readiness атомарно публикует пару готовность/причина без I/O. Политику
// зависимостей и текст причины выбирает вызывающий код.
type Readiness struct {
	snapshot atomic.Pointer[readinessSnapshot]
}

// readinessSnapshot is immutable after publication, allowing lock-free readers
// to observe status and reason from one consistent update.
// readinessSnapshot неизменяем после публикации, поэтому lock-free читатель
// видит статус и причину одного согласованного обновления.
type readinessSnapshot struct {
	ready  bool
	reason string
}

// NewReadiness publishes the initial not-ready/starting snapshot; no dependency
// check or background monitor is started by construction.
// NewReadiness публикует начальный not-ready/starting снимок; конструктор
// не запускает проверку зависимостей или фоновый monitor.
func NewReadiness() *Readiness {
	readiness := &Readiness{}
	readiness.snapshot.Store(&readinessSnapshot{reason: "starting"})
	return readiness
}

// Set replaces the snapshot through CAS, mapping an empty reason to unspecified.
// It returns true only for a changed boolean or reason, so callers can report
// transitions rather than logging every repeated dependency observation.
// Set заменяет снимок через CAS, превращая пустую причину в unspecified.
// True означает изменение boolean или причины, чтобы вызывающий код отмечал
// переходы, а не логировал каждое повторное наблюдение зависимости.
func (readiness *Readiness) Set(ready bool, reason string) bool {
	if reason == "" {
		reason = "unspecified"
	}
	for {
		current := readiness.snapshot.Load()
		if current != nil && current.ready == ready && current.reason == reason {
			return false
		}
		next := &readinessSnapshot{ready: ready, reason: reason}
		if readiness.snapshot.CompareAndSwap(current, next) {
			return true
		}
	}
}

// Ready reads one consistent snapshot; an uninitialized zero-value Readiness
// reports not-ready/starting instead of assuming successful initialization.
// Ready читает один согласованный снимок; zero-value Readiness возвращает
// not-ready/starting, не предполагая успешную инициализацию.
func (readiness *Readiness) Ready() (bool, string) {
	snapshot := readiness.snapshot.Load()
	if snapshot == nil {
		return false, "starting"
	}
	return snapshot.ready, snapshot.reason
}

// Worker runs background work under its caller-owned context. Implementations
// must observe cancellation and return so their group can finish joining.
// Worker выполняет фоновую работу с контекстом вызывающего кода. Реализация
// должна реагировать на отмену и возвращаться для завершения join группы.
type Worker func(context.Context) error

// WorkerGroup owns one cancellation scope, completion signal and joined result.
// Each worker may report one error into a buffer sized for the whole group;
// result is read only after done closes, establishing publication ordering.
// WorkerGroup владеет областью отмены, сигналом завершения и общей ошибкой.
// На worker предусмотрено одно сообщение в буфере; result читается только
// после закрытия done, что обеспечивает порядок публикации результата.
type WorkerGroup struct {
	cancel context.CancelFunc
	done   chan struct{}
	err    chan error
	result error
	once   sync.Once
}

// StartWorkers launches all workers under a shared child context and joins them
// in a collector. A non-cancellation error cancels peers; ordinary context.Canceled
// is not added to the result. Callers invoke it only after their startup barrier.
// StartWorkers запускает workers под общим дочерним контекстом и объединяет их
// завершение. Ошибка, кроме context.Canceled, отменяет соседей; штатная отмена
// не добавляется в результат. Запускать группу следует после startup barrier.
func StartWorkers(parent context.Context, workers ...Worker) *WorkerGroup {
	ctx, cancel := context.WithCancel(parent)
	group := &WorkerGroup{
		cancel: cancel,
		done:   make(chan struct{}),
		err:    make(chan error, len(workers)),
	}
	var wait sync.WaitGroup
	wait.Add(len(workers))
	for _, worker := range workers {
		go func(workerValue Worker) {
			defer wait.Done()
			if err := workerValue(ctx); err != nil && !errors.Is(err, context.Canceled) {
				group.err <- err
				cancel()
			}
		}(worker)
	}
	go func() {
		wait.Wait()
		close(group.err)
		for workerErr := range group.err {
			group.result = errors.Join(group.result, workerErr)
		}
		close(group.done)
	}()
	return group
}

// Stop makes repeated explicit stop requests idempotent through sync.Once;
// it signals cancellation but does not itself wait for worker completion.
// Stop делает повторные явные запросы остановки идемпотентными через sync.Once;
// он сигнализирует отмену, но сам не ожидает завершения workers.
func (group *WorkerGroup) Stop() {
	group.once.Do(group.cancel)
}

// Wait returns the joined worker result after completion, or the caller's
// context error when its wait expires. A timed-out wait does not cancel workers;
// lifecycle shutdown must call Stop independently before releasing dependencies.
// Wait возвращает общую ошибку после завершения или ошибку контекста ожидания.
// Истечение ожидания само не отменяет workers; shutdown должен отдельно вызвать
// Stop до закрытия зависимостей.
func (group *WorkerGroup) Wait(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("wait workers: %w", ctx.Err())
	case <-group.done:
		return group.result
	}
}

// ShutdownOperation names a cleanup callback and its positive timeout. Run must
// cooperate with context cancellation; a context does not preempt a stuck callback.
// ShutdownOperation задаёт имя cleanup, callback и положительный timeout.
// Run должен реагировать на отмену: контекст не прерывает зависший callback сам.
type ShutdownOperation struct {
	Name    string
	Timeout time.Duration
	Run     func(context.Context) error
}

// RunShutdown invokes cleanup sequentially with a fresh child context per valid
// operation, canceling each child afterward and joining errors without skipping
// later callbacks. Parent cancellation/deadlines still apply to every child;
// callers supply a live shutdown base rather than their canceled worker context.
// RunShutdown выполняет cleanup последовательно со свежим дочерним контекстом
// каждой допустимой операции, отменяет его и объединяет ошибки, не пропуская
// последующие callback. Отмена/deadline родителя сохраняются; следует передать
// живую shutdown-базу, а не отменённый контекст workers.
func RunShutdown(
	background context.Context,
	operations ...ShutdownOperation,
) error {
	var joined error
	for _, operation := range operations {
		if operation.Timeout <= 0 {
			joined = errors.Join(joined, fmt.Errorf("%s: invalid shutdown timeout", operation.Name))
			continue
		}
		ctx, cancel := context.WithTimeout(background, operation.Timeout)
		err := operation.Run(ctx)
		cancel()
		if err != nil {
			joined = errors.Join(joined, fmt.Errorf("%s: %w", operation.Name, err))
		}
	}
	return joined
}
