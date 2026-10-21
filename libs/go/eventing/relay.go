package eventing

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"
)

// Publisher is the broker-neutral boundary for delivering one immutable
// envelope and checking the broker before the relay is declared ready.
// RU: Publisher отделяет брокер от доставки одного неизменяемого конверта и
// проверяет брокер до объявления готовности ретранслятора.
type Publisher interface {
	// Publish returns a durable broker receipt or an error for one envelope.
	// RU: Publish возвращает устойчивое подтверждение брокера либо ошибку для конверта.
	Publish(context.Context, Envelope) (PublishReceipt, error)
	// Check verifies that the configured broker contract is usable.
	// RU: Check проверяет пригодность настроенного контракта брокера.
	Check(context.Context) error
	// Close releases the publisher after relay work has stopped.
	// RU: Close освобождает отправителя после остановки работы ретранслятора.
	Close() error
}

// PublishReceipt records the broker stream, sequence and duplicate flag used
// when finalizing the exact outbox event without copying its payload.
// RU: PublishReceipt хранит поток, последовательность и признак повтора брокера
// для завершения точного outbox-события без копирования его содержимого.
type PublishReceipt struct {
	Stream    string
	Sequence  uint64
	Duplicate bool
}

// ClaimedEvent binds one immutable envelope to its store-issued lease token
// and attempt count so finalization can reject another worker's claim.
// RU: ClaimedEvent связывает неизменяемый конверт с выданным хранилищем токеном
// аренды и числом попыток, чтобы завершение отвергало чужой захват.
type ClaimedEvent struct {
	Envelope   Envelope
	LeaseToken string
	Attempts   uint32
}

// OutboxStore owns durable claim and finalization of outbox events; the relay
// never changes the business aggregate or constructs a replacement envelope.
// RU: OutboxStore владеет устойчивым захватом и завершением outbox-событий;
// ретранслятор не меняет бизнес-агрегат и не создаёт новый конверт.
type OutboxStore interface {
	// Check verifies the store's readiness contract before claiming events.
	// RU: Check проверяет готовность хранилища до захвата событий.
	Check(context.Context) error
	// Claim leases a bounded batch to one instance for the requested duration.
	// RU: Claim арендует ограниченный пакет одному экземпляру на заданный срок.
	Claim(context.Context, string, int, time.Duration) ([]ClaimedEvent, error)
	// MarkPublished finalizes a matching lease with its broker receipt.
	// RU: MarkPublished завершает совпадающую аренду с подтверждением брокера.
	MarkPublished(context.Context, string, string, PublishReceipt) error
	// MarkFailed schedules a matching lease for retry or terminal failure.
	// RU: MarkFailed назначает совпадающей аренде повтор либо окончательный отказ.
	MarkFailed(context.Context, string, string, bool, time.Duration) error
}

// RelayConfig bounds batch size, polling, leases, publish and finalization
// deadlines, retry count and exponential backoff for one relay instance.
// RU: RelayConfig ограничивает пакет, опрос, аренду, сроки публикации и
// завершения, число попыток и экспоненциальную задержку экземпляра.
type RelayConfig struct {
	InstanceID      string
	BatchSize       int
	PollInterval    time.Duration
	LeaseDuration   time.Duration
	PublishTimeout  time.Duration
	FinalizeTimeout time.Duration
	MaxAttempts     uint32
	InitialBackoff  time.Duration
	MaximumBackoff  time.Duration
}

// Relay coordinates at-least-once outbox delivery through separate store and
// publisher ports; it owns counters but not the producer's business state.
// RU: Relay координирует доставку outbox как минимум один раз через отдельные
// порты хранилища и брокера; счётчики принадлежат ему, бизнес-состояние — нет.
type Relay struct {
	config    RelayConfig
	store     OutboxStore
	publisher Publisher
	claimed   atomic.Uint64
	published atomic.Uint64
	failed    atomic.Uint64
}

// RelayStats exposes cumulative local claims and successfully finalized
// publish/failure outcomes; it is not an authoritative outbox ledger.
// RU: RelayStats показывает накопленные локальные захваты и успешно
// завершённые исходы публикации/ошибки, но не заменяет журнал outbox.
type RelayStats struct {
	Claimed   uint64
	Published uint64
	Failed    uint64
}

// NewRelay rejects missing dependencies and unsafe timing or retry bounds;
// it creates an idle relay without claiming or publishing any event.
// RU: NewRelay отклоняет отсутствующие зависимости и небезопасные границы
// времени либо повторов; созданный ретранслятор ещё не захватывает события.
func NewRelay(config RelayConfig, store OutboxStore, publisher Publisher) (*Relay, error) {
	if config.InstanceID == "" || len(config.InstanceID) > 128 ||
		config.BatchSize < 1 || config.BatchSize > 128 ||
		config.PollInterval < 50*time.Millisecond ||
		config.LeaseDuration < time.Second ||
		config.PublishTimeout <= 0 || config.FinalizeTimeout <= 0 ||
		config.PublishTimeout+config.FinalizeTimeout >= config.LeaseDuration ||
		config.MaxAttempts < 1 || config.MaxAttempts > 100 ||
		config.InitialBackoff <= 0 || config.MaximumBackoff < config.InitialBackoff ||
		store == nil || publisher == nil {
		return nil, errors.New("event relay configuration is invalid")
	}
	return &Relay{config: config, store: store, publisher: publisher}, nil
}

// Check probes both the durable outbox and broker contract, returning the
// first wrapped failure rather than treating either dependency as optional.
// RU: Check проверяет устойчивый outbox и контракт брокера, возвращая первую
// ошибку с контекстом, не считая ни одну зависимость необязательной.
func (relay *Relay) Check(ctx context.Context) error {
	if err := relay.store.Check(ctx); err != nil {
		return fmt.Errorf("check outbox: %w", err)
	}
	if err := relay.publisher.Check(ctx); err != nil {
		return fmt.Errorf("check publisher: %w", err)
	}
	return nil
}

// Run starts one immediate cycle, then polls at the configured interval until
// lifecycle cancellation or a claim/finalization error stops the loop.
// RU: Run сразу выполняет первый цикл, затем опрашивает с заданным интервалом
// до отмены жизненного цикла либо ошибки захвата/завершения.
func (relay *Relay) Run(lifecycle, finalizeParent context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-lifecycle.Done():
			return lifecycle.Err()
		case <-timer.C:
			if err := relay.cycle(lifecycle, finalizeParent); err != nil {
				return err
			}
			timer.Reset(relay.config.PollInterval)
		}
	}
}

// cycle claims a bounded batch, publishes each envelope under the lifecycle
// deadline and finalizes its exact lease under a separate bounded context.
// RU: cycle захватывает ограниченный пакет, публикует конверты со сроком
// жизненного цикла и завершает точную аренду под отдельным ограниченным контекстом.
func (relay *Relay) cycle(lifecycle, finalizeParent context.Context) error {
	claimed, err := relay.store.Claim(
		lifecycle,
		relay.config.InstanceID,
		relay.config.BatchSize,
		relay.config.LeaseDuration,
	)
	if err != nil {
		return fmt.Errorf("claim outbox: %w", err)
	}
	relay.claimed.Add(uint64(len(claimed)))
	for _, item := range claimed {
		publishCtx, cancelPublish := context.WithTimeout(lifecycle, relay.config.PublishTimeout)
		publishReceipt, publishErr := relay.publisher.Publish(
			publishCtx,
			item.Envelope,
		)
		cancelPublish()

		finalizeCtx, cancelFinalize := context.WithTimeout(
			finalizeParent,
			relay.config.FinalizeTimeout,
		)
		if publishErr == nil {
			err = relay.store.MarkPublished(
				finalizeCtx,
				item.Envelope.EventID,
				item.LeaseToken,
				publishReceipt,
			)
			if err == nil {
				relay.published.Add(1)
			}
		} else {
			err = relay.store.MarkFailed(
				finalizeCtx,
				item.Envelope.EventID,
				item.LeaseToken,
				true,
				relay.backoff(item.Attempts),
			)
			if err == nil {
				relay.failed.Add(1)
			}
		}
		cancelFinalize()
		if err != nil {
			return fmt.Errorf("finalize outbox: %w", err)
		}
	}
	return nil
}

// Stats reads local atomic counters without contacting the broker or store;
// a claim can be counted before its final publish/failure outcome.
// RU: Stats читает локальные атомарные счётчики без обращения к брокеру или
// хранилищу; захват может быть учтён до окончательного исхода доставки.
func (relay *Relay) Stats() RelayStats {
	return RelayStats{
		Claimed:   relay.claimed.Load(),
		Published: relay.published.Load(),
		Failed:    relay.failed.Load(),
	}
}

// backoff doubles the configured initial delay for each recorded attempt and
// caps it at MaximumBackoff without exceeding the configured duration.
// RU: backoff удваивает начальную задержку за каждую учтённую попытку и
// ограничивает её MaximumBackoff, не выходя за заданный срок.
func (relay *Relay) backoff(attempts uint32) time.Duration {
	backoff := relay.config.InitialBackoff
	for current := uint32(0); current < attempts; current++ {
		if backoff >= relay.config.MaximumBackoff/2 {
			return relay.config.MaximumBackoff
		}
		backoff *= 2
	}
	if backoff > relay.config.MaximumBackoff {
		return relay.config.MaximumBackoff
	}
	return backoff
}
