// Package redisstore implements cache.Store over a TLS-only Redis client with
// a locally loaded, size-bounded CA and exact server-name verification.
// RU: Пакет redisstore реализует cache.Store через Redis только с TLS,
// локально загруженным ограниченным CA и точной проверкой имени сервера.
package redisstore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"path/filepath"
	"time"

	"github.com/codex-k8s/kodex/libs/go/cache"
	"github.com/codex-k8s/kodex/libs/go/securefile"
	"github.com/redis/go-redis/v9"
)

// maximumCABytes bounds the CA material accepted by the secure file reader.
// RU: maximumCABytes ограничивает объём CA для безопасного чтения файла.
const maximumCABytes = 1 << 20

// Config supplies the exact TLS Redis endpoint, authentication, database,
// pool size and bounded operation timeouts; New validates mandatory fields.
// RU: Config задаёт точную конечную точку Redis с TLS, аутентификацию,
// базу, размер пула и тайм-ауты; New проверяет обязательные поля.
type Config struct {
	Address       string
	TLSServerName string
	CAFile        string
	Username      string
	Password      string
	Database      int
	PoolSize      int
	DialTimeout   time.Duration
	ReadTimeout   time.Duration
	WriteTimeout  time.Duration
}

// Store owns one Redis client and translates its operations to cache.Store;
// callers close it after workers using the cache have stopped.
// RU: Store владеет одним Redis-клиентом и реализует операции cache.Store;
// вызывающий код закрывает его после остановки использующих кэш workers.
type Store struct {
	client *redis.Client
}

// PoolStats exposes bounded Redis pool counters without disclosing addresses,
// authentication material or arbitrary key names.
// RU: PoolStats предоставляет счётчики пула Redis без адресов, реквизитов
// аутентификации и произвольных имён ключей.
type PoolStats struct {
	Hits     uint32
	Misses   uint32
	Timeouts uint32
	Total    uint32
	Idle     uint32
	Stale    uint32
}

// New validates configuration and a secure CA file, then constructs a Redis
// client with TLS1.3 and exact SNI; network readiness is checked separately.
// RU: New проверяет конфигурацию и безопасный файл CA, затем создаёт клиент
// Redis с TLS1.3 и точным SNI; доступность сети проверяется отдельно.
func New(config Config) (*Store, error) {
	if config.Address == "" || config.TLSServerName == "" ||
		!filepath.IsAbs(config.CAFile) ||
		config.Username == "" || config.Password == "" ||
		config.Database < 0 || config.PoolSize < 1 || config.PoolSize > 256 ||
		config.DialTimeout <= 0 || config.ReadTimeout <= 0 || config.WriteTimeout <= 0 {
		return nil, errors.New("Redis cache configuration is invalid")
	}
	pool, err := loadCertificatePool(config.CAFile)
	if err != nil {
		return nil, err
	}
	client := redis.NewClient(&redis.Options{
		Addr:         config.Address,
		Username:     config.Username,
		Password:     config.Password,
		DB:           config.Database,
		PoolSize:     config.PoolSize,
		DialTimeout:  config.DialTimeout,
		ReadTimeout:  config.ReadTimeout,
		WriteTimeout: config.WriteTimeout,
		TLSConfig: &tls.Config{
			MinVersion: tls.VersionTLS13,
			ServerName: config.TLSServerName,
			RootCAs:    pool,
		},
	})
	return &Store{client: client}, nil
}

// Get returns bytes for one cache key, translates Redis nil to ErrMiss and
// redacts backend failures rather than returning a value with an error.
// RU: Get возвращает байты одного ключа, превращает Redis nil в ErrMiss и
// скрывает подробности сбоя backend, не выдавая значение вместе с ошибкой.
func (store *Store) Get(ctx context.Context, key string) ([]byte, error) {
	raw, err := store.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return nil, cache.ErrMiss
	}
	if err != nil {
		return nil, errors.New("read Redis cache")
	}
	return raw, nil
}

// Set requires a positive TTL and writes one exact byte value; backend errors
// are returned without leaking Redis configuration or key contents.
// RU: Set требует положительный TTL и записывает точное байтовое значение;
// ошибка backend возвращается без раскрытия конфигурации и содержимого ключа.
func (store *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("Redis cache TTL is invalid")
	}
	if err := store.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return errors.New("write Redis cache")
	}
	return nil
}

// Delete removes one exact cache key and returns a redacted backend error;
// authoritative service data is untouched.
// RU: Delete удаляет один точный ключ кэша и возвращает скрытую ошибку backend;
// авторитетные данные сервиса не затрагиваются.
func (store *Store) Delete(ctx context.Context, key string) error {
	if err := store.client.Del(ctx, key).Err(); err != nil {
		return errors.New("delete Redis cache")
	}
	return nil
}

// Check pings Redis through the configured TLS client to test the actual
// transport path, returning a redacted failure on connection errors.
// RU: Check отправляет ping через настроенный TLS-клиент для проверки
// фактического соединения и скрывает детали ошибки подключения.
func (store *Store) Check(ctx context.Context) error {
	if err := store.client.Ping(ctx).Err(); err != nil {
		return errors.New("check Redis cache")
	}
	return nil
}

// Close releases the owned Redis client; callers must join its users first.
// RU: Close освобождает принадлежащий Store Redis-клиент; сначала вызывающий
// код должен дождаться завершения его пользователей.
func (store *Store) Close() error {
	return store.client.Close()
}

// PoolStats maps Redis client pool counters into a value-only snapshot for
// observability without exposing the client or its credentials.
// RU: PoolStats преобразует счётчики пула клиента в снимок для наблюдаемости
// без выдачи самого клиента и его реквизитов.
func (store *Store) PoolStats() PoolStats {
	stats := store.client.PoolStats()
	return PoolStats{
		Hits:     stats.Hits,
		Misses:   stats.Misses,
		Timeouts: stats.Timeouts,
		Total:    stats.TotalConns,
		Idle:     stats.IdleConns,
		Stale:    stats.StaleConns,
	}
}

// loadCertificatePool reads a bounded secure CA file and rejects material
// without at least one parseable PEM certificate.
// RU: loadCertificatePool читает безопасный ограниченный файл CA и отклоняет
// материал без хотя бы одного разбираемого PEM-сертификата.
func loadCertificatePool(path string) (*x509.CertPool, error) {
	raw, err := securefile.Read(path, maximumCABytes)
	if err != nil {
		return nil, errors.New("Redis CA file is unsafe")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("parse Redis CA file")
	}
	return pool, nil
}
