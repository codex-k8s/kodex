package cache

import (
	"context"
	"errors"
	"time"
)

// Store abstracts byte-oriented cache operations and a health check; callers
// remain responsible for key scope and value eligibility.
// RU: Store абстрагирует операции байтового кэша и проверку соединения;
// область ключа и допустимость значения остаются ответственностью вызывающего кода.
type Store interface {
	Get(context.Context, string) ([]byte, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
	Check(context.Context) error
	Close() error
}

// ErrMiss distinguishes an absent key from an infrastructure failure so a
// caller can fall back to its authoritative source.
// RU: ErrMiss отличает отсутствие ключа от ошибки инфраструктуры, позволяя
// вызывающему коду обратиться к авторитетному источнику.
var ErrMiss = errors.New("cache miss")

// Codec maps a service-owned value to cache bytes and back; validation of
// provenance and scope belongs in the concrete decoder supplied by the caller.
// RU: Codec преобразует принадлежащее сервису значение в байты кэша и обратно;
// проверка происхождения и области возлагается на конкретный декодер вызывающего кода.
type Codec[T any] interface {
	Marshal(T) ([]byte, error)
	Unmarshal([]byte) (T, error)
}

// Loader obtains a value from an authoritative source using the original
// request context when cache lookup fails or decoding is rejected.
// RU: Loader получает значение из авторитетного источника с исходным контекстом
// запроса при промахе кэша либо отказе декодера.
type Loader[T any] func(context.Context) (T, error)

// Source is the narrow authoritative read port used after cache miss, error
// or invalid bytes; implementations choose their own owner-scoped query.
// RU: Source является узким портом авторитетного чтения после промаха, ошибки
// либо неверных байтов кэша; реализация сама задаёт запрос в области владельца.
type Source[T any] interface {
	Get(context.Context) (T, error)
}

// Engine bounds cache I/O by timeout and TTL and falls back to Source when
// cache access or decoding fails. A successful decode is trusted as supplied;
// the concrete Codec must reject stale or wrongly scoped snapshots.
// RU: Engine ограничивает операции кэша тайм-аутом и TTL и переходит к Source
// при ошибке чтения либо декодирования. Успешный результат декодера принимается;
// конкретный Codec обязан отклонять устаревший либо чужой снимок.
type Engine[T any] struct {
	store   Store
	codec   Codec[T]
	timeout time.Duration
	ttl     time.Duration
}

// New rejects nil dependencies, cache timeouts outside 10ms–1s and TTLs
// outside (0,1min], then builds an engine without opening a connection.
// RU: New отклоняет пустые зависимости, тайм-ауты вне10мс–1с и TTL вне
// (0,1мин], затем создаёт механизм без открытия соединения.
func New[T any](store Store, codec Codec[T], timeout, ttl time.Duration) (*Engine[T], error) {
	if store == nil || codec == nil ||
		timeout < 10*time.Millisecond || timeout > time.Second ||
		ttl <= 0 || ttl > time.Minute {
		return nil, errors.New("cache engine configuration is invalid")
	}
	return &Engine[T]{store: store, codec: codec, timeout: timeout, ttl: ttl}, nil
}

// Load adapts a function loader to Source and delegates bounded cache-first
// reading to GetOrSet without changing fallback behavior.
// RU: Load превращает функцию загрузки в Source и передаёт чтение через кэш
// в GetOrSet, сохраняя поведение перехода к источнику.
func (engine *Engine[T]) Load(ctx context.Context, key string, source Loader[T]) (T, error) {
	return engine.GetOrSet(ctx, key, sourceAdapter[T]{load: source})
}

// GetOrSet first tries a bounded cache read and decoder, then uses the original
// context for an authoritative read on any cache failure. Encoding or cache
// write failure never replaces a successful authoritative result.
// RU: GetOrSet сначала пробует ограниченное чтение и декодер кэша, затем при
// любом сбое кэша читает источник с исходным контекстом. Ошибка кодирования
// либо записи кэша не заменяет успешный авторитетный результат.
func (engine *Engine[T]) GetOrSet(
	ctx context.Context,
	key string,
	source Source[T],
) (T, error) {
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
	value, sourceErr := source.Get(ctx)
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

// sourceAdapter wraps a function as a Source and rejects an absent loader.
// RU: sourceAdapter оборачивает функцию в Source и отклоняет пустой загрузчик.
type sourceAdapter[T any] struct {
	load Loader[T]
}

// Get calls the supplied loader with the original context or returns an input
// error without attempting to read any cache state.
// RU: Get вызывает заданный загрузчик с исходным контекстом либо возвращает
// ошибку входа, не пытаясь читать состояние кэша.
func (adapter sourceAdapter[T]) Get(ctx context.Context) (T, error) {
	var zero T
	if adapter.load == nil {
		return zero, errors.New("cache source is invalid")
	}
	return adapter.load(ctx)
}

// Store encodes a caller-validated projection and writes it under a nonempty
// key with the engine's bounded timeout and TTL; validation is not repeated.
// RU: Store кодирует уже проверенную вызывающим кодом проекцию и записывает
// её по непустому ключу с ограниченными тайм-аутом и TTL; проверка не повторяется.
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

// Invalidate deletes one exact nonempty cache key under a bounded context and
// returns the backend error; it does not change authoritative state.
// RU: Invalidate удаляет один точный непустой ключ кэша с ограниченным
// контекстом и возвращает ошибку backend; авторитетное состояние не меняется.
func (engine *Engine[T]) Invalidate(ctx context.Context, key string) error {
	if key == "" {
		return errors.New("cache invalidation key is invalid")
	}
	cacheCtx, cancel := context.WithTimeout(ctx, engine.timeout)
	defer cancel()
	return engine.store.Delete(cacheCtx, key)
}
