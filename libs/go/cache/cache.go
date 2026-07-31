package cache

import (
	"context"
	"errors"
	"time"
)

// Store — provider-neutral TTL key/value storage.
type Store interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
	Check(context.Context) error
	Close() error
}

// ErrMiss отличает отсутствие записи от инфраструктурной ошибки.
var ErrMiss = errors.New("cache miss")

// Codec преобразует service-owned domain snapshot в bounded wire form.
type Codec[T any] interface {
	Marshal(T) ([]byte, error)
	Unmarshal([]byte) (T, error)
}

// Loader читает authoritative source.
type Loader[T any] func(context.Context) (T, error)

// Engine реализует fail-open-to-source read-through без права выдавать stale
// состояние при ошибке PostgreSQL.
type Engine[T any] struct {
	store   Store
	codec   Codec[T]
	timeout time.Duration
	ttl     time.Duration
}

// New создаёт bounded engine.
func New[T any](store Store, codec Codec[T], timeout, ttl time.Duration) (*Engine[T], error) {
	if store == nil || codec == nil ||
		timeout < 10*time.Millisecond || timeout > time.Second ||
		ttl <= 0 || ttl > time.Minute {
		return nil, errors.New("cache engine configuration is invalid")
	}
	return &Engine[T]{store: store, codec: codec, timeout: timeout, ttl: ttl}, nil
}

// Load сначала проверяет cache, затем authoritative source.
func (engine *Engine[T]) Load(ctx context.Context, key string, source Loader[T]) (T, error) {
	var zero T
	if key == "" || source == nil {
		return zero, errors.New("cache load input is invalid")
	}
	cacheCtx, cancelCache := context.WithTimeout(ctx, engine.timeout)
	raw, err := engine.store.Get(cacheCtx, key)
	cancelCache()
	if err == nil {
		value, decodeErr := engine.codec.Unmarshal(raw)
		if decodeErr == nil {
			return value, nil
		}
	}
	value, sourceErr := source(ctx)
	if sourceErr != nil {
		return zero, sourceErr
	}
	encoded, encodeErr := engine.codec.Marshal(value)
	if encodeErr == nil {
		writeCtx, cancelWrite := context.WithTimeout(ctx, engine.timeout)
		_ = engine.store.Set(writeCtx, key, encoded, engine.ttl)
		cancelWrite()
	}
	return value, nil
}

// Store сохраняет уже проверенную caller projection с bounded timeout/TTL.
func (engine *Engine[T]) Store(ctx context.Context, key string, value T) error {
	if key == "" {
		return errors.New("cache store key is invalid")
	}
	encoded, err := engine.codec.Marshal(value)
	if err != nil {
		return err
	}
	cacheCtx, cancel := context.WithTimeout(ctx, engine.timeout)
	defer cancel()
	return engine.store.Set(cacheCtx, key, encoded, engine.ttl)
}

// Invalidate удаляет подозрительную или устаревшую запись best-effort caller policy.
func (engine *Engine[T]) Invalidate(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("cache invalidation key is invalid")
	}
	cacheCtx, cancel := context.WithTimeout(ctx, engine.timeout)
	defer cancel()
	return engine.store.Delete(cacheCtx, key)
}
