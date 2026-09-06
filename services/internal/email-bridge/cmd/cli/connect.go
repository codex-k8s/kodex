package main

import (
	"context"
	"errors"
	"net"
	"syscall"
	"time"
)

const databaseRetryDelay = time.Second

type connectionWaitFailure struct{ cause, budget error }

func (*connectionWaitFailure) Error() string     { return "database connection wait exhausted" }
func (e *connectionWaitFailure) Unwrap() []error { return []error{e.cause, e.budget} }

// Ожидание относится только к Ping; миграция вызывается единожды после успеха
// и использует остаток исходного бюджета, без перезапуска SQL.
func withReadyDatabase(ctx context.Context, ping func(context.Context) error, apply func(context.Context) error, delay time.Duration) error {
	var last error
	for {
		if err := ctx.Err(); err != nil {
			return failure(stageDatabaseConnect, &connectionWaitFailure{cause: last, budget: err})
		}
		err := ping(ctx)
		if err == nil {
			if err = ctx.Err(); err != nil {
				return failure(stageDatabaseConnect, &connectionWaitFailure{cause: last, budget: err})
			}
			return apply(ctx)
		}
		if ctx.Err() != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
			if last == nil {
				last = err
			}
			return failure(stageDatabaseConnect, &connectionWaitFailure{cause: last, budget: ctx.Err()})
		}
		// Классификатор сначала исключает TLS/auth/configuration даже у joined error.
		if !retryableConnection(err) {
			return failure(stageDatabaseConnect, err)
		}
		last = err
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return failure(stageDatabaseConnect, &connectionWaitFailure{cause: last, budget: ctx.Err()})
		case <-timer.C:
		}
	}
}

func retryableConnection(err error) bool {
	// Общая диагностика знает permanent SQL/TLS/configuration до сетевых wrappers.
	switch failureClass(err) {
	case "dns_temporary", "dns_timeout", "connection_refused", "connection_reset", "host_unreachable", "network_unreachable", "network_timeout":
		return true
	default:
		return false
	}
}

func networkFailureClass(err error) string {
	var dns *net.DNSError
	if errors.As(err, &dns) {
		if dns.IsNotFound {
			return "dns_not_found"
		}
		if dns.IsTimeout {
			return "dns_timeout"
		}
		if dns.IsTemporary {
			return "dns_temporary"
		}
		return "dns"
	}
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, syscall.EHOSTUNREACH):
		return "host_unreachable"
	case errors.Is(err, syscall.ENETUNREACH):
		return "network_unreachable"
	case errors.Is(err, syscall.ETIMEDOUT):
		return "network_timeout"
	}
	var network net.Error
	if errors.As(err, &network) {
		return "network"
	}
	return "unknown"
}
