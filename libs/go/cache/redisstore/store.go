// Package redisstore реализует cache.Store поверх go-redis.
package redisstore

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/codex-k8s/matter-codex/libs/go/cache"
	"github.com/redis/go-redis/v9"
)

const maximumCABytes = 1 << 20

// Config задаёт TLS-only Redis endpoint.
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

// Store владеет Redis client.
type Store struct {
	client *redis.Client
}

type PoolStats struct {
	Hits     uint32
	Misses   uint32
	Timeouts uint32
	Total    uint32
	Idle     uint32
	Stale    uint32
}

// New создаёт TLS-only Redis store.
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

// Get читает ограниченный байтовый снимок.
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

// Set сохраняет снимок с обязательным TTL.
func (store *Store) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return errors.New("Redis cache TTL is invalid")
	}
	if err := store.client.Set(ctx, key, value, ttl).Err(); err != nil {
		return errors.New("write Redis cache")
	}
	return nil
}

// Delete инвалидирует точный ключ.
func (store *Store) Delete(ctx context.Context, key string) error {
	if err := store.client.Del(ctx, key).Err(); err != nil {
		return errors.New("delete Redis cache")
	}
	return nil
}

// Check проверяет фактическое TLS соединение.
func (store *Store) Check(ctx context.Context) error {
	if err := store.client.Ping(ctx).Err(); err != nil {
		return errors.New("check Redis cache")
	}
	return nil
}

// Close закрывает client.
func (store *Store) Close() error {
	return store.client.Close()
}

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

func loadCertificatePool(path string) (*x509.CertPool, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 ||
		info.Size() > maximumCABytes || info.Mode().Perm()&0o007 != 0 {
		return nil, errors.New("Redis CA file is unsafe")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("read Redis CA file")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(raw) {
		return nil, errors.New("parse Redis CA file")
	}
	return pool, nil
}
