package ratelimit

import (
	"sync"
	"time"
)

// Config sets the fixed-window request quota, bounded subject registry and
// separate global/per-subject concurrency ceilings for each transport.
// Config задаёт квоту запросов в фиксированном окне, ограниченный реестр
// субъектов и отдельные глобальные/персональные пределы параллелизма.
type Config struct {
	Window                         time.Duration
	Limit                          uint32
	MaximumKeys                    int
	PreAuthConcurrency             int
	GlobalHTTPConcurrency          int
	PerSubjectHTTPConcurrency      int
	GlobalWebSocketConcurrency     int
	PerSubjectWebSocketConcurrency int
}

// bucket stores one subject's request window, last use and currently held HTTP
// and WebSocket slots; all access is protected by Limiter.mu.
// bucket хранит окно запросов субъекта, время последнего использования и
// занятые HTTP/WebSocket-слоты; доступ защищён Limiter.mu.
type bucket struct {
	started time.Time
	count   uint32
	seen    time.Time
	http    int
	ws      int
}

// Limiter combines per-subject fixed-window counts with nonblocking global
// semaphores and fails closed when its bounded bucket registry is full.
// Limiter объединяет счётчики субъектов в фиксированных окнах с глобальными
// неблокирующими семафорами и отклоняет запрос при заполненном реестре.
type Limiter struct {
	mu      sync.Mutex
	config  Config
	buckets map[string]bucket
	preauth chan struct{}
	http    chan struct{}
	ws      chan struct{}
	now     func() time.Time
}

// New allocates an empty bounded registry and transport semaphores; invalid
// configuration remains fail-closed at each admission method.
// New создаёт пустой ограниченный реестр и семафоры транспортов; при
// недействительной конфигурации каждый метод допуска закрыто отказывает.
func New(config Config) *Limiter {
	return &Limiter{
		config: config, buckets: make(map[string]bucket, config.MaximumKeys),
		preauth: make(chan struct{}, config.PreAuthConcurrency),
		http:    make(chan struct{}, config.GlobalHTTPConcurrency),
		ws:      make(chan struct{}, config.GlobalWebSocketConcurrency), now: time.Now,
	}
}

// valid requires positive quotas and per-subject concurrency strictly below
// each matching global limit before admitting any request.
// valid требует положительные квоты и предел субъекта строго ниже
// соответствующего глобального предела до допуска любого запроса.
func (limiter *Limiter) valid() bool {
	return limiter != nil && limiter.config.Window >= time.Second && limiter.config.Limit > 0 && limiter.config.MaximumKeys > 0 &&
		limiter.config.PreAuthConcurrency > 0 && limiter.config.GlobalHTTPConcurrency > 0 && limiter.config.PerSubjectHTTPConcurrency > 0 &&
		limiter.config.PerSubjectHTTPConcurrency < limiter.config.GlobalHTTPConcurrency && limiter.config.GlobalWebSocketConcurrency > 0 &&
		limiter.config.PerSubjectWebSocketConcurrency > 0 && limiter.config.PerSubjectWebSocketConcurrency < limiter.config.GlobalWebSocketConcurrency
}

// Allow counts a subject's request in its fixed window, resetting an expired
// window and refusing unknown subjects if no inactive bucket can be evicted.
// Allow учитывает запрос субъекта в фиксированном окне, сбрасывает истёкшее
// окно и отклоняет нового субъекта, если нет вытесняемой неактивной записи.
func (limiter *Limiter) Allow(key string) bool {
	if !limiter.valid() || key == "" {
		return false
	}
	now := limiter.now()
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	value, ok := limiter.bucketLocked(key, now)
	if !ok {
		return false
	}
	if now.Sub(value.started) >= limiter.config.Window {
		value.started, value.count = now, 0
	}
	value.seen = now
	if value.count >= limiter.config.Limit {
		limiter.buckets[key] = value
		return false
	}
	value.count++
	limiter.buckets[key] = value
	return true
}

// AcquirePreAuth reserves a global pre-authentication slot without waiting and
// returns an idempotent release callback on success.
// AcquirePreAuth неблокирующе резервирует глобальный слот до аутентификации и
// при успехе возвращает идемпотентную функцию освобождения.
func (limiter *Limiter) AcquirePreAuth() (func(), bool) {
	if !limiter.valid() {
		return func() {}, false
	}
	return acquireChannel(limiter.preauth)
}

// AcquireHTTP reserves both global and subject HTTP concurrency for a nonempty
// key, releasing the global slot if subject admission fails.
// AcquireHTTP резервирует глобальный и персональный HTTP-параллелизм для
// непустого ключа и освобождает глобальный слот при отказе субъекту.
func (limiter *Limiter) AcquireHTTP(key string) (func(), bool) {
	return limiter.acquireSubject(key, false)
}

// AcquireWebSocket reserves the distinct WebSocket global/subject limits and
// returns the same once-only release contract as HTTP admission.
// AcquireWebSocket резервирует отдельные глобальный и персональный пределы
// WebSocket и возвращает тот же одноразовый контракт освобождения, что HTTP.
func (limiter *Limiter) AcquireWebSocket(key string) (func(), bool) {
	return limiter.acquireSubject(key, true)
}

// acquireSubject checks the matching global semaphore before the locked
// subject bucket, then returns a once-only callback that releases both slots.
// acquireSubject сначала проверяет глобальный семафор, затем защищённую запись
// субъекта и возвращает функцию однократного освобождения обоих слотов.
func (limiter *Limiter) acquireSubject(key string, websocket bool) (func(), bool) {
	if !limiter.valid() || key == "" {
		return func() {}, false
	}
	global := limiter.http
	if websocket {
		global = limiter.ws
	}
	releaseGlobal, ok := acquireChannel(global)
	if !ok {
		return func() {}, false
	}
	now := limiter.now()
	limiter.mu.Lock()
	value, ok := limiter.bucketLocked(key, now)
	if ok {
		if websocket {
			ok = value.ws < limiter.config.PerSubjectWebSocketConcurrency
			if ok {
				value.ws++
			}
		} else {
			ok = value.http < limiter.config.PerSubjectHTTPConcurrency
			if ok {
				value.http++
			}
		}
		value.seen = now
		limiter.buckets[key] = value
	}
	limiter.mu.Unlock()
	if !ok {
		releaseGlobal()
		return func() {}, false
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			limiter.mu.Lock()
			entry, exists := limiter.buckets[key]
			if exists {
				if websocket && entry.ws > 0 {
					entry.ws--
				}
				if !websocket && entry.http > 0 {
					entry.http--
				}
				entry.seen = limiter.now()
				limiter.buckets[key] = entry
			}
			limiter.mu.Unlock()
			releaseGlobal()
		})
	}, true
}

// bucketLocked returns an existing bucket or evicts the oldest expired idle
// entry; active or recent subjects are never evicted to admit a new key.
// bucketLocked возвращает запись либо вытесняет самую старую истёкшую
// неактивную запись; занятые и недавние субъекты не вытесняются.
func (limiter *Limiter) bucketLocked(key string, now time.Time) (bucket, bool) {
	if value, ok := limiter.buckets[key]; ok {
		return value, true
	}
	if len(limiter.buckets) >= limiter.config.MaximumKeys {
		var oldestKey string
		var oldest time.Time
		for candidate, entry := range limiter.buckets {
			if entry.http != 0 || entry.ws != 0 || now.Sub(entry.seen) < limiter.config.Window {
				continue
			}
			if oldestKey == "" || entry.seen.Before(oldest) || (entry.seen.Equal(oldest) && candidate < oldestKey) {
				oldestKey, oldest = candidate, entry.seen
			}
		}
		if oldestKey == "" {
			return bucket{}, false
		}
		delete(limiter.buckets, oldestKey)
	}
	return bucket{started: now, seen: now}, true
}

// acquireChannel performs a nonblocking semaphore send and returns an
// idempotent release callback; a full channel denies admission.
// acquireChannel неблокирующе занимает семафор и возвращает идемпотентное
// освобождение; заполненный канал означает отказ в допуске.
func acquireChannel(channel chan struct{}) (func(), bool) {
	select {
	case channel <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-channel }) }, true
	default:
		return func() {}, false
	}
}
